package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/gabrielmbmb/what-ttft/internal/configfile"
	"github.com/gabrielmbmb/what-ttft/internal/eventbus"
	"github.com/gabrielmbmb/what-ttft/internal/httptracecap"
	"github.com/gabrielmbmb/what-ttft/internal/report"
	"github.com/gabrielmbmb/what-ttft/internal/tui"
	"github.com/gabrielmbmb/what-ttft/pkg/provider/openai"
	"github.com/gabrielmbmb/what-ttft/pkg/whatttft"
)

func benchCommand(args []string, stdout io.Writer, stderr io.Writer) int {
	return benchCommandContext(context.Background(), args, stdout, stderr)
}

func benchCommandContext(ctx context.Context, args []string, stdout io.Writer, stderr io.Writer) int {
	if ctx == nil {
		ctx = context.Background()
	}

	cliCfg, _, err := parseBenchFlags(args, stderr)
	if err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return 0
		}
		writeFormatted(stderr, "%v\n", err)
		return 2
	}

	plan, err := configfile.LoadFile(cliCfg.configPath)
	if err != nil {
		writeFormatted(stderr, "config: %v\n", err)
		return 2
	}
	if overrideErr := applyBenchOverrides(plan, cliCfg); overrideErr != nil {
		writeFormatted(stderr, "%v\n", overrideErr)
		return 2
	}

	metadata := benchOutputMetadata(plan, cliCfg, args, 0, 0)
	outputDir := report.ResolveOutputDir(cliCfg.outputDir, metadata, time.Now())
	cliCfg.outputDir = outputDir
	if cliCfg.dryRun {
		if envErr := validateBenchmarkAPIKeyEnvs(plan); envErr != nil {
			writeFormatted(stderr, "%v\n", envErr)
			return 1
		}
		printBenchDryRun(stdout, plan, outputDir)
		return 0
	}
	if preflightErr := report.ValidateOutputDir(outputDir, cliCfg.overwrite); preflightErr != nil {
		writeFormatted(stderr, "output directory check: %v\n", preflightErr)
		return 1
	}

	execute := func(execCtx context.Context, observer whatttft.RunObserver) commandExecution {
		return executeBenchCommand(execCtx, plan, cliCfg, args, observer)
	}
	if cliCfg.tui {
		return benchTUILauncher(ctx, benchTUILaunchRequest{
			Plan:    plan,
			Config:  cliCfg,
			Args:    append([]string(nil), args...),
			Stdout:  stdout,
			Stderr:  stderr,
			Execute: execute,
		})
	}

	execution := execute(ctx, nil)
	return finishBenchCommand(stdout, stderr, plan, execution)
}

type benchTUILaunchRequest struct {
	Plan    *configfile.Config
	Config  benchCLIConfig
	Args    []string
	Stdout  io.Writer
	Stderr  io.Writer
	Execute func(context.Context, whatttft.RunObserver) commandExecution
}

type benchTUILaunchFunc func(context.Context, benchTUILaunchRequest) int

var benchTUILauncher benchTUILaunchFunc = defaultBenchTUILauncher

func defaultBenchTUILauncher(ctx context.Context, request benchTUILaunchRequest) int {
	if ctx == nil {
		ctx = context.Background()
	}

	benchCtx, cancel := context.WithCancel(ctx)
	defer cancel()

	events := make(chan whatttft.RunEvent, 1024)
	tuiSink := tui.NewEventSink(events)
	bus := eventbus.New([]eventbus.Sink{tuiSink}, eventbus.Options{})
	executionCh := make(chan commandExecution, 1)
	busCloseCtx := context.WithoutCancel(benchCtx)
	go func() {
		execution := request.Execute(benchCtx, bus)
		if closeErr := bus.Close(busCloseCtx); closeErr != nil && execution.ReportErr == nil {
			execution.ReportErr = fmt.Errorf("close event bus: %w", closeErr)
		}
		executionCh <- execution
	}()

	tuiErr := tui.Run(benchCtx, tui.RunOptions{
		Events: events,
		Cancel: cancel,
		Output: request.Stdout,
	})
	if tuiErr != nil {
		cancel()
		execution := <-executionCh
		writeFormatted(request.Stderr, "bench TUI failed: %v\n", tuiErr)
		if execution.ReportErr != nil {
			writeFormatted(request.Stderr, "write reports: %v\n", execution.ReportErr)
		}
		return 1
	}

	var execution commandExecution
	select {
	case execution = <-executionCh:
	default:
		cancel()
		execution = <-executionCh
	}

	return finishBenchCommand(request.Stdout, request.Stderr, request.Plan, execution)
}

func executeBenchCommand(ctx context.Context, plan *configfile.Config, cliCfg benchCLIConfig, args []string, observer whatttft.RunObserver) commandExecution {
	if ctx == nil {
		ctx = context.Background()
	}

	sequencedObserver := newSequencedCommandObserver(observer)
	benchmarkResult, metadata, err := executeBench(ctx, plan, cliCfg, args, sequencedObserver)
	var runResult *whatttft.RunResult
	if benchmarkResult != nil {
		runResult = benchmarkResult.RunResult()
	}
	execution := commandExecution{
		Result:          runResult,
		BenchmarkResult: benchmarkResult,
		Metadata:        metadata,
		Err:             err,
		Canceled:        isContextError(err),
		Partial:         isContextError(err) && hasRunRecords(runResult),
	}
	if shouldWriteReports(err, runResult) {
		outputDir, reportErr := writeCommandReports(ctx, cliCfg.outputDir, cliCfg.overwrite, plan.Run.SaveChunks, metadata, runResult, sequencedObserver)
		execution.OutputDir = outputDir
		execution.ReportErr = reportErr
	}

	return execution
}

func executeBench(ctx context.Context, plan *configfile.Config, cliCfg benchCLIConfig, args []string, observer whatttft.RunObserver) (*whatttft.BenchmarkResult, report.RunMetadata, error) {
	start := time.Now()
	benchmarkConfig, err := buildBenchmarkConfig(plan)
	if err != nil {
		end := time.Now()
		metadata := benchOutputMetadata(plan, cliCfg, args, start.UnixNano(), end.UnixNano())
		metadata.RunConfig.OutputDir = cliCfg.outputDir
		return nil, metadata, err
	}

	benchmarkResult, err := whatttft.RunBenchmarkWithOptions(ctx, benchmarkConfig, whatttft.BenchmarkOptions{Observer: observer})
	end := time.Now()
	metadata := benchOutputMetadata(plan, cliCfg, args, start.UnixNano(), end.UnixNano())
	metadata.RunConfig.OutputDir = cliCfg.outputDir

	return benchmarkResult, metadata, err
}

func finishBenchCommand(stdout io.Writer, stderr io.Writer, plan *configfile.Config, execution commandExecution) int {
	if execution.ReportErr != nil {
		writeFormatted(stderr, "write reports: %v\n", execution.ReportErr)
		return 1
	}
	if execution.Err != nil {
		if execution.Canceled {
			if execution.Partial && execution.OutputDir != "" && execution.BenchmarkResult != nil {
				printBenchSummary(stdout, plan, execution.BenchmarkResult)
				writeFormatted(stdout, "\nbenchmark canceled; wrote partial results to %s\n", execution.OutputDir)
			} else {
				writeFormatted(stderr, "benchmark canceled: %v\n", execution.Err)
			}
			return interruptedExitCode
		}

		writeFormatted(stderr, "benchmark failed: %v\n", execution.Err)
		return 1
	}

	printBenchSummary(stdout, plan, execution.BenchmarkResult)
	writeFormatted(stdout, "\nwrote results to %s\n", execution.OutputDir)
	return 0
}

func parseBenchFlags(args []string, stderr io.Writer) (benchCLIConfig, *flag.FlagSet, error) {
	cfg := benchCLIConfig{}
	flagSet := newFlagSet("what-ttft bench", stderr)
	flagSet.Usage = func() { printBenchUsage(flagSet.Output()) }

	flagSet.StringVar(&cfg.configPath, "config", "", "YAML benchmark config path")
	flagSet.StringVar(&cfg.outputDir, "out", "", "output directory; defaults to a generated directory under runs/")
	flagSet.BoolVar(&cfg.overwrite, "overwrite", false, "overwrite non-empty output directory")
	flagSet.BoolVar(&cfg.dryRun, "dry-run", false, "parse, validate, and print the normalized plan without sending requests")
	flagSet.Var(&cfg.saveChunks, "save-chunks", "optional override for run.save_chunks")
	flagSet.Var(&cfg.samples, "samples", "optional override for run.samples")
	flagSet.Var(&cfg.warmup, "warmup", "optional override for run.warmup")
	flagSet.Var(&cfg.concurrency, "concurrency", "optional override for run.concurrency")
	flagSet.Var(&cfg.timeout, "timeout", "optional override for run.timeout, e.g. 120s")
	flagSet.Var(&cfg.serviceTier, "service-tier", "optional override for every OpenAI target service_tier: auto|default|flex|scale|priority")
	flagSet.BoolVar(&cfg.tui, "tui", false, "show the live terminal dashboard when available")

	if err := flagSet.Parse(args); err != nil {
		return benchCLIConfig{}, flagSet, err
	}
	if flagSet.NArg() != 0 {
		return benchCLIConfig{}, flagSet, fmt.Errorf("unexpected positional arguments: %s", strings.Join(flagSet.Args(), " "))
	}
	if strings.TrimSpace(cfg.configPath) == "" {
		return benchCLIConfig{}, flagSet, errors.New("--config is required")
	}

	return cfg, flagSet, nil
}

func printBenchUsage(output io.Writer) {
	writeText(output, `Usage:
  what-ttft bench --config benchmark.yaml [flags]

Required flags:
  --config PATH

Common flags:
  --out DIR
  --overwrite
  --dry-run
  --save-chunks[=true|false]
  --samples N
  --warmup N
  --concurrency N
  --timeout DURATION
  --service-tier auto|default|flex|scale|priority
  --tui
`)
}

func applyBenchOverrides(plan *configfile.Config, cliCfg benchCLIConfig) error {
	if cliCfg.samples.set {
		plan.Run.Samples = cliCfg.samples.value
	}
	if cliCfg.warmup.set {
		plan.Run.Warmup = cliCfg.warmup.value
	}
	if cliCfg.concurrency.set {
		plan.Run.Concurrency = cliCfg.concurrency.value
	}
	if cliCfg.timeout.set {
		plan.Run.Timeout = cliCfg.timeout.value
	}
	if cliCfg.saveChunks.set {
		plan.Run.SaveChunks = cliCfg.saveChunks.value
	}
	if cliCfg.serviceTier.set {
		tier := openai.ServiceTier(cliCfg.serviceTier.value)
		if !validServiceTier(tier) || tier == "" {
			return fmt.Errorf("unsupported service tier override %q", cliCfg.serviceTier.value)
		}
		for index := range plan.Targets {
			plan.Targets[index].OpenAI.ServiceTier = tier
		}
	}

	return validateBenchPlan(plan)
}

func validateBenchPlan(plan *configfile.Config) error {
	if plan.Run.Samples < 0 {
		return errors.New("--samples override must be non-negative")
	}
	if plan.Run.Warmup < 0 {
		return errors.New("--warmup override must be non-negative")
	}
	if plan.Run.Samples+plan.Run.Warmup == 0 {
		return errors.New("run.samples + run.warmup must be greater than zero")
	}
	if plan.Run.Concurrency < 1 {
		return errors.New("--concurrency override must be at least 1")
	}
	if plan.Run.Timeout < 0 {
		return errors.New("--timeout override must be non-negative")
	}

	return nil
}

func buildBenchmarkConfig(plan *configfile.Config) (whatttft.BenchmarkConfig, error) {
	targets := make([]whatttft.BenchmarkTarget, 0, len(plan.Targets))
	for _, target := range plan.Targets {
		apiKey := os.Getenv(target.OpenAI.APIKeyEnv)
		if apiKey == "" {
			return whatttft.BenchmarkConfig{}, fmt.Errorf("target %s API key environment variable %s is empty", target.ID, target.OpenAI.APIKeyEnv)
		}

		client := httptracecap.NewHTTPClient(httptracecap.TransportConfig{
			ConnectionMode: plan.Run.ConnectionMode,
			Timeout:        plan.Run.Timeout,
		})
		provider := openai.New(openai.Config{
			API:                target.OpenAI.API,
			BaseURL:            target.OpenAI.BaseURL,
			APIKey:             apiKey,
			Model:              target.OpenAI.Model,
			ServiceTier:        target.OpenAI.ServiceTier,
			UseLegacyMaxTokens: target.OpenAI.LegacyMaxTokens,
			IncludeUsage:       target.OpenAI.IncludeUsage,
			HTTPClient:         client,
		})
		targets = append(targets, whatttft.BenchmarkTarget{
			ID:                   target.ID,
			Name:                 target.Name,
			Provider:             provider,
			ProviderAPI:          string(target.OpenAI.API),
			RequestedServiceTier: string(target.OpenAI.ServiceTier),
			Config:               plan.RunConfigForTarget(target),
		})
	}

	return whatttft.BenchmarkConfig{Name: plan.Name, Targets: targets}, nil
}

func validateBenchmarkAPIKeyEnvs(plan *configfile.Config) error {
	for _, target := range plan.Targets {
		if os.Getenv(target.OpenAI.APIKeyEnv) == "" {
			return fmt.Errorf("target %s API key environment variable %s is empty", target.ID, target.OpenAI.APIKeyEnv)
		}
	}

	return nil
}

func benchOutputMetadata(plan *configfile.Config, cliCfg benchCLIConfig, args []string, wallStart int64, wallEnd int64) report.RunMetadata {
	commonRunConfig := whatttft.RunConfig{
		Scenario:         plan.Scenario,
		WarmupRequests:   plan.Run.Warmup,
		MeasuredRequests: plan.Run.Samples,
		Concurrency:      plan.Run.Concurrency,
		CacheMode:        plan.Run.CacheMode,
		ConnectionMode:   plan.Run.ConnectionMode,
		OutputDir:        cliCfg.outputDir,
		SaveChunks:       plan.Run.SaveChunks,
	}

	return report.RunMetadata{
		BenchmarkName:        plan.Name,
		ConfigPath:           cliCfg.configPath,
		ConfigSHA256:         benchConfigSHA256(cliCfg.configPath),
		TargetOrder:          string(whatttft.SerialTargetOrder),
		Targets:              benchTargetMetadata(plan),
		Provider:             commonTargetProvider(plan),
		Model:                commonTargetModel(plan),
		BaseURL:              commonTargetBaseURL(plan),
		ProviderAPI:          commonTargetAPI(plan),
		RequestedServiceTier: commonTargetServiceTier(plan),
		Scenario:             plan.Scenario,
		RunConfig:            commonRunConfig,
		WallStartUnixNano:    wallStart,
		WallEndUnixNano:      wallEnd,
		Args:                 redactArgs(args),
	}
}

func benchConfigSHA256(path string) string {
	if strings.TrimSpace(path) == "" {
		return ""
	}

	//nolint:gosec // Benchmark config paths are caller-provided CLI inputs by design.
	data, err := os.ReadFile(path)
	if err != nil {
		return ""
	}

	return configfile.SHA256Hex(data)
}

func benchTargetMetadata(plan *configfile.Config) []report.RunTargetMetadata {
	targets := make([]report.RunTargetMetadata, 0, len(plan.Targets))
	for _, target := range plan.Targets {
		targets = append(targets, report.RunTargetMetadata{
			TargetID:             target.ID,
			TargetName:           target.Name,
			Provider:             target.Provider,
			ProviderAPI:          string(target.OpenAI.API),
			RequestedServiceTier: string(target.OpenAI.ServiceTier),
			Model:                target.OpenAI.Model,
			BaseURL:              target.OpenAI.BaseURL,
			APIKeyEnv:            target.OpenAI.APIKeyEnv,
			IncludeUsage:         target.OpenAI.IncludeUsage,
			LegacyMaxTokens:      target.OpenAI.LegacyMaxTokens,
		})
	}

	return targets
}

func commonTargetProvider(plan *configfile.Config) string {
	if len(plan.Targets) == 0 {
		return "benchmark"
	}

	provider := plan.Targets[0].Provider
	for _, target := range plan.Targets[1:] {
		if target.Provider != provider {
			return "mixed"
		}
	}

	return provider
}

func commonTargetModel(plan *configfile.Config) string {
	if len(plan.Targets) == 1 {
		return plan.Targets[0].OpenAI.Model
	}
	if strings.TrimSpace(plan.Name) != "" {
		return plan.Name
	}

	return "multi-target"
}

func commonTargetBaseURL(plan *configfile.Config) string {
	if len(plan.Targets) == 0 {
		return ""
	}

	baseURL := plan.Targets[0].OpenAI.BaseURL
	for _, target := range plan.Targets[1:] {
		if target.OpenAI.BaseURL != baseURL {
			return ""
		}
	}

	return baseURL
}

func commonTargetAPI(plan *configfile.Config) string {
	if len(plan.Targets) == 0 {
		return ""
	}

	api := plan.Targets[0].OpenAI.API
	for _, target := range plan.Targets[1:] {
		if target.OpenAI.API != api {
			return "mixed"
		}
	}

	return string(api)
}

func commonTargetServiceTier(plan *configfile.Config) string {
	if len(plan.Targets) == 0 {
		return ""
	}

	tier := plan.Targets[0].OpenAI.ServiceTier
	for _, target := range plan.Targets[1:] {
		if target.OpenAI.ServiceTier != tier {
			return "mixed"
		}
	}

	return string(tier)
}

func printBenchDryRun(output io.Writer, plan *configfile.Config, outputDir string) {
	writeFormatted(
		output,
		"dry run: no requests sent\nbenchmark=%s scenario=%s targets=%d samples=%d warmup=%d concurrency=%d cache=%s connection=%s timeout=%s save_chunks=%t output=%s\n",
		firstNonEmptyString(plan.Name, "unnamed"),
		firstNonEmptyString(plan.Scenario.Name, "unnamed"),
		len(plan.Targets),
		plan.Run.Samples,
		plan.Run.Warmup,
		plan.Run.Concurrency,
		plan.Run.CacheMode,
		plan.Run.ConnectionMode,
		plan.Run.Timeout,
		plan.Run.SaveChunks,
		outputDir,
	)
	writeLine(output, "target              api               tier       model               base_url                           api_key_env")
	for _, target := range plan.Targets {
		writeFormatted(
			output,
			"%-19s %-17s %-10s %-19s %-34s %s\n",
			target.ID,
			target.OpenAI.API,
			firstNonEmptyString(string(target.OpenAI.ServiceTier), "unset"),
			target.OpenAI.Model,
			target.OpenAI.RedactedBaseURL(),
			target.OpenAI.APIKeyEnv,
		)
	}
}

func printBenchSummary(output io.Writer, plan *configfile.Config, result *whatttft.BenchmarkResult) {
	writeFormatted(
		output,
		"benchmark=%s scenario=%s targets=%d samples=%d warmup=%d concurrency=%d cache=%s connection=%s successful=%d errors=%d\n\n",
		firstNonEmptyString(plan.Name, "unnamed"),
		firstNonEmptyString(plan.Scenario.Name, "unnamed"),
		len(plan.Targets),
		plan.Run.Samples,
		plan.Run.Warmup,
		plan.Run.Concurrency,
		plan.Run.CacheMode,
		plan.Run.ConnectionMode,
		result.Summary.SuccessfulRequests,
		result.Summary.ErrorRequests,
	)
	writeLine(output, "target              api               tier       model               ok   err  completion_tokens_total  completion_token_records  ttft_p50  ttft_p95  e2e_p50   e2e_p95   e2e_output_tps_mean  generation_delta_output_tps_mean  generation_delta_output_tps_count  system_tps  rps")
	groups := benchGroupsByTargetID(result.Summary.Groups)
	for _, target := range plan.Targets {
		group := groups[target.ID]
		if group == nil {
			writeFormatted(
				output,
				"%-19s %-17s %-10s %-19s %-4d %-4d %-24s %-24s %-9s %-9s %-9s %-9s %-21s %-34s %-34s %-11s %s\n",
				target.ID,
				target.OpenAI.API,
				firstNonEmptyString(string(target.OpenAI.ServiceTier), "unset"),
				target.OpenAI.Model,
				0,
				0,
				"-",
				"-",
				"-",
				"-",
				"-",
				"-",
				"-",
				"-",
				"-",
				"-",
				"-",
			)
			continue
		}

		writeFormatted(
			output,
			"%-19s %-17s %-10s %-19s %-4d %-4d %-24d %-24s %-9s %-9s %-9s %-9s %-21s %-34s %-34s %-11s %s\n",
			target.ID,
			target.OpenAI.API,
			firstNonEmptyString(string(target.OpenAI.ServiceTier), "unset"),
			target.OpenAI.Model,
			group.SuccessfulRequests,
			group.ErrorRequests,
			group.TotalCompletionTokens,
			formatCLIUsageRecordCount(group.CompletionTokenRecords, group.SuccessfulRequests),
			formatCLIOptionalFloat(group.Metrics.TTFTDeltaMS.P50),
			formatCLIOptionalFloat(group.Metrics.TTFTDeltaMS.P95),
			formatCLIOptionalFloat(group.Metrics.E2EDeltaMS.P50),
			formatCLIOptionalFloat(group.Metrics.E2EDeltaMS.P95),
			formatCLIOptionalFloat(group.Metrics.E2EOutputTPS.Mean),
			formatCLIOptionalFloat(group.Metrics.GenerationDeltaOutputTPS.Mean),
			formatCLIDistributionCount(group.Metrics.GenerationDeltaOutputTPS, group.SuccessfulRequests),
			formatCLIOptionalFloat(group.SystemTPS),
			formatCLIOptionalFloat(group.RPS),
		)
	}
}

func formatCLIUsageRecordCount(count int, denominator int) string {
	if denominator <= 0 {
		return "0/0"
	}

	return fmt.Sprintf("%d/%d", count, denominator)
}

func formatCLIDistributionCount(distribution whatttft.Distribution, denominator int) string {
	return formatCLIUsageRecordCount(distribution.Count, denominator)
}

func benchGroupsByTargetID(groups []whatttft.SummaryGroup) map[string]*whatttft.SummaryGroup {
	byTarget := make(map[string]*whatttft.SummaryGroup, len(groups))
	for index := range groups {
		group := &groups[index]
		byTarget[group.TargetID] = group
	}

	return byTarget
}

type benchCLIConfig struct {
	configPath  string
	outputDir   string
	overwrite   bool
	dryRun      bool
	saveChunks  optionalBool
	samples     optionalInt
	warmup      optionalInt
	concurrency optionalInt
	timeout     optionalDuration
	serviceTier optionalString
	tui         bool
}

type optionalInt struct {
	value int
	set   bool
}

func (i *optionalInt) Set(value string) error {
	parsed, err := strconv.Atoi(value)
	if err != nil {
		return err
	}

	i.value = parsed
	i.set = true
	return nil
}

func (i *optionalInt) String() string {
	if i == nil || !i.set {
		return ""
	}

	return fmt.Sprintf("%d", i.value)
}

type optionalDuration struct {
	value time.Duration
	set   bool
}

func (d *optionalDuration) Set(value string) error {
	parsed, err := time.ParseDuration(value)
	if err != nil {
		return fmt.Errorf("parse duration %q: %w", value, err)
	}

	d.value = parsed
	d.set = true
	return nil
}

func (d *optionalDuration) String() string {
	if d == nil || !d.set {
		return ""
	}

	return d.value.String()
}

type optionalBool struct {
	value bool
	set   bool
}

func (b *optionalBool) Set(value string) error {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "1", "t", "true", "y", "yes", "on":
		b.value = true
	case "0", "f", "false", "n", "no", "off":
		b.value = false
	default:
		return fmt.Errorf("parse bool %q", value)
	}
	b.set = true
	return nil
}

func (b *optionalBool) String() string {
	if b == nil || !b.set {
		return ""
	}

	return fmt.Sprintf("%t", b.value)
}

func (b *optionalBool) IsBoolFlag() bool {
	return true
}

type optionalString struct {
	value string
	set   bool
}

func (s *optionalString) Set(value string) error {
	s.value = strings.TrimSpace(value)
	s.set = true
	return nil
}

func (s *optionalString) String() string {
	if s == nil || !s.set {
		return ""
	}

	return s.value
}
