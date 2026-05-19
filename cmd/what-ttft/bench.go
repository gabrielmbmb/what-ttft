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
	"github.com/gabrielmbmb/what-ttft/internal/httptracecap"
	"github.com/gabrielmbmb/what-ttft/internal/report"
	"github.com/gabrielmbmb/what-ttft/pkg/provider/openai"
	"github.com/gabrielmbmb/what-ttft/pkg/whatttft"
)

func benchCommand(args []string, stdout io.Writer, stderr io.Writer) int {
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

	benchmarkConfig, err := buildBenchmarkConfig(plan)
	if err != nil {
		writeFormatted(stderr, "%v\n", err)
		return 1
	}

	start := time.Now()
	benchmarkResult, err := whatttft.RunBenchmark(context.Background(), benchmarkConfig)
	end := time.Now()
	metadata = benchOutputMetadata(plan, cliCfg, args, start.UnixNano(), end.UnixNano())
	metadata.RunConfig.OutputDir = outputDir
	if err != nil {
		writeFormatted(stderr, "benchmark failed: %v\n", err)
		return 1
	}

	writtenDir, err := report.WriteRun(report.WriteOptions{
		OutputDir:  outputDir,
		Overwrite:  cliCfg.overwrite,
		SaveChunks: plan.Run.SaveChunks,
		Run:        metadata,
		Result:     benchmarkResult.RunResult(),
	})
	if err != nil {
		writeFormatted(stderr, "write reports: %v\n", err)
		return 1
	}

	printBenchSummary(stdout, plan, benchmarkResult)
	writeFormatted(stdout, "\nwrote results to %s\n", writtenDir)
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
			ID:       target.ID,
			Name:     target.Name,
			Provider: provider,
			Config:   plan.RunConfigForTarget(target),
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
	writeLine(output, "target              api               tier       model               ok   err  ttft_p50  ttft_p95  e2e_p50   e2e_p95   e2e_tps_mean  gen_tps_mean  system_tps  rps")
	groups := benchGroupsByTargetID(result.Summary.Groups)
	for _, target := range plan.Targets {
		group := groups[target.ID]
		if group == nil {
			writeFormatted(
				output,
				"%-19s %-17s %-10s %-19s %-4d %-4d %-9s %-9s %-9s %-9s %-13s %-13s %-11s %s\n",
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
			)
			continue
		}

		writeFormatted(
			output,
			"%-19s %-17s %-10s %-19s %-4d %-4d %-9s %-9s %-9s %-9s %-13s %-13s %-11s %s\n",
			target.ID,
			target.OpenAI.API,
			firstNonEmptyString(string(target.OpenAI.ServiceTier), "unset"),
			target.OpenAI.Model,
			group.SuccessfulRequests,
			group.ErrorRequests,
			formatCLIOptionalFloat(group.Metrics.TTFTDeltaMS.P50),
			formatCLIOptionalFloat(group.Metrics.TTFTDeltaMS.P95),
			formatCLIOptionalFloat(group.Metrics.E2EDeltaMS.P50),
			formatCLIOptionalFloat(group.Metrics.E2EDeltaMS.P95),
			formatCLIOptionalFloat(group.Metrics.E2EOutputTPS.Mean),
			formatCLIOptionalFloat(group.Metrics.GenerationDeltaOutputTPS.Mean),
			formatCLIOptionalFloat(group.SystemTPS),
			formatCLIOptionalFloat(group.RPS),
		)
	}
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
