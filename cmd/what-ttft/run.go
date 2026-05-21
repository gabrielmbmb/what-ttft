package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"math"
	"os"
	"strconv"
	"strings"
	"sync/atomic"
	"time"

	"github.com/gabrielmbmb/what-ttft/internal/eventbus"
	"github.com/gabrielmbmb/what-ttft/internal/httptracecap"
	"github.com/gabrielmbmb/what-ttft/internal/report"
	"github.com/gabrielmbmb/what-ttft/internal/tui"
	"github.com/gabrielmbmb/what-ttft/pkg/provider/openai"
	"github.com/gabrielmbmb/what-ttft/pkg/whatttft"
)

const (
	adHocScenarioName   = "ad-hoc"
	interruptedExitCode = 130
)

type commandExecution struct {
	Result          *whatttft.RunResult
	BenchmarkResult *whatttft.BenchmarkResult
	Metadata        report.RunMetadata
	OutputDir       string
	Err             error
	ReportErr       error
	Canceled        bool
	Partial         bool
}

type sequencedCommandObserver struct {
	downstream whatttft.RunObserver
	sequence   atomic.Int64
}

func newSequencedCommandObserver(observer whatttft.RunObserver) whatttft.RunObserver {
	if observer == nil {
		return nil
	}

	return &sequencedCommandObserver{downstream: observer}
}

func (o *sequencedCommandObserver) OnRunEvent(ctx context.Context, event whatttft.RunEvent) {
	if o == nil || o.downstream == nil {
		return
	}

	event.Sequence = o.sequence.Add(1)
	if event.WallUnixNano == 0 {
		event.WallUnixNano = time.Now().UnixNano()
	}
	o.downstream.OnRunEvent(ctx, event.Clone())
}

func runCommand(args []string, stdout io.Writer, stderr io.Writer) int {
	return runCommandContext(context.Background(), args, stdout, stderr)
}

func runCommandContext(ctx context.Context, args []string, stdout io.Writer, stderr io.Writer) int {
	if ctx == nil {
		ctx = context.Background()
	}

	cfg, _, err := parseRunFlags(args, stderr)
	if err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return 0
		}
		writeFormatted(stderr, "%v\n", err)
		return 2
	}

	cfg.outputDir = report.ResolveOutputDir(cfg.outputDir, outputDirMetadata(cfg), time.Now())
	if preflightErr := report.ValidateOutputDir(cfg.outputDir, cfg.overwrite); preflightErr != nil {
		writeFormatted(stderr, "output directory check: %v\n", preflightErr)
		return 1
	}

	execute := func(execCtx context.Context, observer whatttft.RunObserver) commandExecution {
		return executeRunCommand(execCtx, cfg, args, observer)
	}
	if cfg.tui {
		return runTUILauncher(ctx, runTUILaunchRequest{
			Config:  cfg,
			Args:    append([]string(nil), args...),
			Stdout:  stdout,
			Stderr:  stderr,
			Execute: execute,
		})
	}

	execution := execute(ctx, nil)
	return finishRunCommand(stdout, stderr, cfg, execution)
}

type runTUILaunchRequest struct {
	Config  runCLIConfig
	Args    []string
	Stdout  io.Writer
	Stderr  io.Writer
	Execute func(context.Context, whatttft.RunObserver) commandExecution
}

type runTUILaunchFunc func(context.Context, runTUILaunchRequest) int

var runTUILauncher runTUILaunchFunc = defaultRunTUILauncher

func defaultRunTUILauncher(ctx context.Context, request runTUILaunchRequest) int {
	if ctx == nil {
		ctx = context.Background()
	}

	runCtx, cancel := context.WithCancel(ctx)
	defer cancel()

	events := make(chan whatttft.RunEvent, 1024)
	tuiSink := tui.NewEventSink(events)
	bus := eventbus.New([]eventbus.Sink{tuiSink}, eventbus.Options{})
	executionCh := make(chan commandExecution, 1)
	go func() {
		execution := request.Execute(runCtx, bus)
		if closeErr := bus.Close(context.Background()); closeErr != nil && execution.ReportErr == nil {
			execution.ReportErr = fmt.Errorf("close event bus: %w", closeErr)
		}
		executionCh <- execution
	}()

	tuiErr := tui.Run(runCtx, tui.RunOptions{
		Events: events,
		Cancel: cancel,
		Output: request.Stdout,
	})
	if tuiErr != nil {
		cancel()
		execution := <-executionCh
		writeFormatted(request.Stderr, "run TUI failed: %v\n", tuiErr)
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

	return finishRunCommand(request.Stdout, request.Stderr, request.Config, execution)
}

func executeRunCommand(ctx context.Context, cfg runCLIConfig, args []string, observer whatttft.RunObserver) commandExecution {
	if ctx == nil {
		ctx = context.Background()
	}

	sequencedObserver := newSequencedCommandObserver(observer)
	result, metadata, err := executeRun(ctx, cfg, args, sequencedObserver)
	execution := commandExecution{
		Result:   result,
		Metadata: metadata,
		Err:      err,
		Canceled: isContextError(err),
		Partial:  isContextError(err) && hasRunRecords(result),
	}
	if shouldWriteReports(err, result) {
		outputDir, reportErr := writeCommandReports(ctx, cfg.outputDir, cfg.overwrite, cfg.saveChunks, metadata, result, sequencedObserver)
		execution.OutputDir = outputDir
		execution.ReportErr = reportErr
	}

	return execution
}

func finishRunCommand(stdout io.Writer, stderr io.Writer, cfg runCLIConfig, execution commandExecution) int {
	if execution.ReportErr != nil {
		writeFormatted(stderr, "write reports: %v\n", execution.ReportErr)
		return 1
	}
	if execution.Err != nil {
		if execution.Canceled {
			if execution.Partial && execution.OutputDir != "" {
				printRunSummary(stdout, cfg, execution.Result)
				writeFormatted(stdout, "\nrun canceled; wrote partial results to %s\n", execution.OutputDir)
			} else {
				writeFormatted(stderr, "run canceled: %v\n", execution.Err)
			}
			return interruptedExitCode
		}

		writeFormatted(stderr, "run failed: %v\n", execution.Err)
		return 1
	}

	printRunSummary(stdout, cfg, execution.Result)
	writeFormatted(stdout, "\nwrote results to %s\n", execution.OutputDir)
	return 0
}

func parseRunFlags(args []string, stderr io.Writer) (runCLIConfig, *flag.FlagSet, error) {
	cfg := runCLIConfig{}
	flagSet := newFlagSet("what-ttft run", stderr)
	flagSet.Usage = func() { printRunUsage(flagSet.Output()) }

	flagSet.StringVar(&cfg.provider, "provider", "openai", "provider to benchmark (openai for v0.1)")
	flagSet.StringVar(&cfg.openAIAPI, "openai-api", string(openai.ResponsesAPI), "OpenAI API surface: responses|chat-completions")
	flagSet.StringVar(&cfg.baseURL, "base-url", openai.DefaultBaseURL, "provider API base URL")
	flagSet.StringVar(&cfg.apiKeyEnv, "api-key-env", "OPENAI_API_KEY", "environment variable containing the API key")
	flagSet.StringVar(&cfg.apiKey, "api-key", "", "API key for local testing; never printed")
	flagSet.StringVar(&cfg.model, "model", "", "model identifier")
	flagSet.StringVar(&cfg.prompt, "prompt", "", "user prompt")
	flagSet.StringVar(&cfg.systemPrompt, "system-prompt", "", "optional system prompt")
	flagSet.IntVar(&cfg.samples, "samples", 50, "measured request count")
	flagSet.IntVar(&cfg.warmup, "warmup", 5, "warmup request count")
	flagSet.IntVar(&cfg.concurrency, "concurrency", 1, "fixed concurrency")
	flagSet.StringVar(&cfg.cacheMode, "cache-mode", string(whatttft.CacheBust), "cache-bust|cache-reuse|provider-explicit-cache|unknown")
	flagSet.StringVar(&cfg.connectionMode, "connection-mode", string(whatttft.WarmConnections), "warm|cold")
	flagSet.StringVar(&cfg.reasoningEffort, "reasoning-effort", "", "optional reasoning/thinking effort: none|minimal|low|medium|high|xhigh")
	flagSet.StringVar(&cfg.reasoningEffort, "thinking-effort", "", "alias for --reasoning-effort")
	flagSet.StringVar(&cfg.serviceTier, "service-tier", "", "optional OpenAI service tier: auto|default|flex|scale|priority")
	flagSet.IntVar(&cfg.maxOutputTokens, "max-output-tokens", 64, "maximum output tokens")
	flagSet.Var(&cfg.temperature, "temperature", "optional sampling temperature, e.g. 0")
	flagSet.Var(&cfg.topP, "top-p", "optional nucleus sampling value, e.g. 1")
	flagSet.DurationVar(&cfg.timeout, "timeout", 120*time.Second, "whole-request HTTP client timeout")
	flagSet.StringVar(&cfg.outputDir, "out", "", "output directory; defaults to a generated directory under runs/")
	flagSet.BoolVar(&cfg.saveChunks, "save-chunks", false, "write chunks.jsonl with generated content")
	flagSet.BoolVar(&cfg.includeUsage, "include-usage", true, "request stream usage chunks when supported")
	flagSet.BoolVar(&cfg.legacyMaxTokens, "legacy-max-tokens", false, "send max_tokens instead of max_completion_tokens")
	flagSet.BoolVar(&cfg.overwrite, "overwrite", false, "overwrite non-empty output directory")
	flagSet.BoolVar(&cfg.tui, "tui", false, "show the live terminal dashboard when available")

	if err := flagSet.Parse(args); err != nil {
		return runCLIConfig{}, flagSet, err
	}
	if flagSet.NArg() != 0 {
		return runCLIConfig{}, flagSet, fmt.Errorf("unexpected positional arguments: %s", strings.Join(flagSet.Args(), " "))
	}
	if err := cfg.validate(); err != nil {
		return runCLIConfig{}, flagSet, err
	}

	return cfg, flagSet, nil
}

func printRunUsage(output io.Writer) {
	writeText(output, `Usage:
  what-ttft run [flags]

Required flags:
  --provider openai
  --model MODEL
  --prompt PROMPT

Common flags:
  --openai-api responses|chat-completions
  --base-url URL
  --api-key-env ENV
  --api-key KEY
  --samples N
  --warmup N
  --concurrency N
  --cache-mode cache-bust|cache-reuse|provider-explicit-cache|unknown
  --connection-mode warm|cold
  --reasoning-effort none|minimal|low|medium|high|xhigh
  --thinking-effort none|minimal|low|medium|high|xhigh
  --service-tier auto|default|flex|scale|priority
  --max-output-tokens N
  --temperature FLOAT
  --top-p FLOAT
  --timeout DURATION
  --out DIR
  --save-chunks
  --include-usage
  --legacy-max-tokens
  --tui
`)
}

func executeRun(ctx context.Context, cfg runCLIConfig, args []string, observer whatttft.RunObserver) (*whatttft.RunResult, report.RunMetadata, error) {
	cacheMode := whatttft.CacheMode(cfg.cacheMode)
	connectionMode := whatttft.ConnectionMode(cfg.connectionMode)
	scenario := whatttft.Scenario{
		Name:            adHocScenarioName,
		Prompt:          cfg.prompt,
		SystemPrompt:    cfg.systemPrompt,
		MaxOutputTokens: cfg.maxOutputTokens,
		ReasoningEffort: cfg.reasoningEffort,
	}
	if cfg.temperature.set {
		scenario.Temperature = &cfg.temperature.value
	}
	if cfg.topP.set {
		scenario.TopP = &cfg.topP.value
	}

	runConfig := whatttft.RunConfig{
		Scenario:         scenario,
		WarmupRequests:   cfg.warmup,
		MeasuredRequests: cfg.samples,
		Concurrency:      cfg.concurrency,
		CacheMode:        cacheMode,
		ConnectionMode:   connectionMode,
		OutputDir:        cfg.outputDir,
		SaveChunks:       cfg.saveChunks,
	}

	apiKey, err := resolveAPIKey(cfg)
	if err != nil {
		return nil, report.RunMetadata{}, err
	}
	client := httptracecap.NewHTTPClient(httptracecap.TransportConfig{
		ConnectionMode: connectionMode,
		Timeout:        cfg.timeout,
	})
	provider := openai.New(openai.Config{
		API:                openai.API(cfg.openAIAPI),
		BaseURL:            cfg.baseURL,
		APIKey:             apiKey,
		APIKeyEnv:          "",
		Model:              cfg.model,
		ServiceTier:        openai.ServiceTier(cfg.serviceTier),
		UseLegacyMaxTokens: cfg.legacyMaxTokens,
		IncludeUsage:       cfg.includeUsage,
		HTTPClient:         client,
	})

	start := time.Now()
	result, err := whatttft.NewRunnerWithOptions(provider, runConfig, whatttft.RunnerOptions{Observer: observer}).Run(ctx)
	end := time.Now()
	metadata := report.RunMetadata{
		Provider:             provider.Name(),
		Model:                provider.Model(),
		BaseURL:              cfg.baseURL,
		ProviderAPI:          cfg.openAIAPI,
		RequestedServiceTier: cfg.serviceTier,
		Scenario:             scenario,
		RunConfig:            runConfig,
		WallStartUnixNano:    start.UnixNano(),
		WallEndUnixNano:      end.UnixNano(),
		Args:                 redactArgs(args),
	}
	if err != nil {
		return result, metadata, err
	}

	return result, metadata, nil
}

func writeCommandReports(
	ctx context.Context,
	outputDir string,
	overwrite bool,
	saveChunks bool,
	metadata report.RunMetadata,
	result *whatttft.RunResult,
	observer whatttft.RunObserver,
) (string, error) {
	emitReportWriteEvent(ctx, observer, whatttft.EventReportWriteStarted, metadata, result, outputDir, nil)
	writtenDir, err := report.WriteRun(report.WriteOptions{
		OutputDir:  outputDir,
		Overwrite:  overwrite,
		SaveChunks: saveChunks,
		Run:        metadata,
		Result:     result,
	})
	if err != nil {
		emitReportWriteEvent(ctx, observer, whatttft.EventReportWriteFailed, metadata, result, outputDir, err)
		return "", err
	}

	emitReportWriteEvent(ctx, observer, whatttft.EventReportWriteFinished, metadata, result, writtenDir, nil)
	return writtenDir, nil
}

func emitReportWriteEvent(
	ctx context.Context,
	observer whatttft.RunObserver,
	kind whatttft.RunEventKind,
	metadata report.RunMetadata,
	result *whatttft.RunResult,
	outputDir string,
	err error,
) {
	if observer == nil {
		return
	}

	event := whatttft.RunEvent{
		Kind:                 kind,
		BenchmarkName:        metadata.BenchmarkName,
		Provider:             metadata.Provider,
		Model:                metadata.Model,
		ScenarioName:         firstNonEmptyString(metadata.Scenario.Name, metadata.RunConfig.Scenario.Name),
		CacheMode:            metadata.RunConfig.CacheMode,
		ConnectionMode:       metadata.RunConfig.ConnectionMode,
		RequestedServiceTier: metadata.RequestedServiceTier,
		OutputDir:            outputDir,
		TotalRequests:        metadata.RunConfig.WarmupRequests + metadata.RunConfig.MeasuredRequests,
		WarmupRequests:       metadata.RunConfig.WarmupRequests,
		MeasuredRequests:     metadata.RunConfig.MeasuredRequests,
	}
	if result != nil {
		event.CompletedRequests = len(result.Records)
		event.SuccessfulRequests = result.Summary.SuccessfulRequests
		event.ErrorRequests = result.Summary.ErrorRequests
		event.Summary = &result.Summary
	}
	if err != nil {
		event.Error = &whatttft.RunEventError{Category: "report_write", Message: err.Error()}
	}
	observer.OnRunEvent(ctx, event)
}

func isContextError(err error) bool {
	return errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded)
}

func hasRunRecords(result *whatttft.RunResult) bool {
	return result != nil && len(result.Records) > 0
}

func shouldWriteReports(err error, result *whatttft.RunResult) bool {
	if !hasRunRecords(result) {
		return false
	}

	return err == nil || isContextError(err)
}

func outputDirMetadata(cfg runCLIConfig) report.RunMetadata {
	scenario := whatttft.Scenario{Name: adHocScenarioName}

	return report.RunMetadata{
		Provider:             cfg.provider,
		Model:                cfg.model,
		ProviderAPI:          cfg.openAIAPI,
		RequestedServiceTier: cfg.serviceTier,
		Scenario:             scenario,
		RunConfig: whatttft.RunConfig{
			Scenario:       scenario,
			CacheMode:      whatttft.CacheMode(cfg.cacheMode),
			ConnectionMode: whatttft.ConnectionMode(cfg.connectionMode),
		},
	}
}

func printRunSummary(output io.Writer, cfg runCLIConfig, result *whatttft.RunResult) {
	writeFormatted(
		output,
		"provider=%s api=%s model=%s scenario=%s samples=%d warmup=%d concurrency=%d cache=%s connection=%s service_tier=%s successful=%d errors=%d\n\n",
		cfg.provider,
		cfg.openAIAPI,
		cfg.model,
		adHocScenarioName,
		cfg.samples,
		cfg.warmup,
		cfg.concurrency,
		cfg.cacheMode,
		cfg.connectionMode,
		firstNonEmptyString(cfg.serviceTier, "unset"),
		result.Summary.SuccessfulRequests,
		result.Summary.ErrorRequests,
	)
	writeLine(output, "metric                                      p50      p95      p99      mean")

	var metrics whatttft.MetricDistributions
	var systemTPS *float64
	var rps *float64
	if len(result.Summary.Groups) > 0 {
		group := result.Summary.Groups[0]
		metrics = group.Metrics
		systemTPS = group.SystemTPS
		rps = group.RPS
	}
	printMetricLine(output, "http_ttfb_ms", metrics.HTTPTTFBMS)
	printMetricLine(output, "provider_processing_ms", metrics.ProviderProcessingMS)
	printMetricLine(output, "server_wait_minus_provider_processing_ms", metrics.ServerWaitMinusProviderProcessingMS)
	printMetricLine(output, "ttft_delta_ms", metrics.TTFTDeltaMS)
	printMetricLine(output, "e2e_delta_ms", metrics.E2EDeltaMS)
	printMetricLine(output, "e2e_output_tps", metrics.E2EOutputTPS)
	printMetricLine(output, "generation_delta_output_tps", metrics.GenerationDeltaOutputTPS)
	writeFormatted(output, "\nsystem_tps=%s rps=%s\n", formatCLIOptionalFloat(systemTPS), formatCLIOptionalFloat(rps))
}

func printMetricLine(output io.Writer, name string, distribution whatttft.Distribution) {
	writeFormatted(
		output,
		"%-42s %-8s %-8s %-8s %-8s\n",
		name,
		formatCLIOptionalFloat(distribution.P50),
		formatCLIOptionalFloat(distribution.P95),
		formatCLIOptionalFloat(distribution.P99),
		formatCLIOptionalFloat(distribution.Mean),
	)
}

func formatCLIOptionalFloat(value *float64) string {
	if value == nil {
		return "-"
	}

	return fmt.Sprintf("%.1f", *value)
}

func firstNonEmptyString(value string, fallback string) string {
	if value != "" {
		return value
	}

	return fallback
}

func resolveAPIKey(cfg runCLIConfig) (string, error) {
	if cfg.apiKey != "" {
		return cfg.apiKey, nil
	}
	if cfg.apiKeyEnv == "" {
		return "", errors.New("--api-key-env or --api-key is required")
	}

	apiKey := os.Getenv(cfg.apiKeyEnv)
	if apiKey == "" {
		return "", fmt.Errorf("API key environment variable %s is empty; set it or pass --api-key", cfg.apiKeyEnv)
	}

	return apiKey, nil
}

func redactArgs(args []string) []string {
	redacted := append([]string(nil), args...)
	for index := 0; index < len(redacted); index++ {
		arg := redacted[index]
		if arg == "--api-key" {
			if index+1 < len(redacted) {
				redacted[index+1] = "[REDACTED]"
			}
			continue
		}
		if strings.HasPrefix(arg, "--api-key=") {
			redacted[index] = "--api-key=[REDACTED]"
		}
	}

	return redacted
}

type runCLIConfig struct {
	provider        string
	openAIAPI       string
	baseURL         string
	apiKeyEnv       string
	apiKey          string
	model           string
	prompt          string
	systemPrompt    string
	samples         int
	warmup          int
	concurrency     int
	cacheMode       string
	connectionMode  string
	reasoningEffort string
	serviceTier     string
	maxOutputTokens int
	temperature     optionalFloat
	topP            optionalFloat
	timeout         time.Duration
	outputDir       string
	saveChunks      bool
	includeUsage    bool
	legacyMaxTokens bool
	overwrite       bool
	tui             bool
}

func (c runCLIConfig) validate() error {
	if c.provider != "openai" {
		return fmt.Errorf("unsupported provider %q", c.provider)
	}
	if c.model == "" {
		return errors.New("--model is required")
	}
	if c.prompt == "" {
		return errors.New("--prompt is required")
	}
	if c.apiKey == "" && c.apiKeyEnv == "" {
		return errors.New("--api-key-env or --api-key is required")
	}
	if c.samples < 0 {
		return errors.New("--samples must be non-negative")
	}
	if c.warmup < 0 {
		return errors.New("--warmup must be non-negative")
	}
	if c.samples+c.warmup == 0 {
		return errors.New("at least one sample or warmup request is required")
	}
	if c.concurrency < 1 {
		return errors.New("--concurrency must be at least 1")
	}
	if c.maxOutputTokens < 0 {
		return errors.New("--max-output-tokens must be non-negative")
	}
	if c.timeout < 0 {
		return errors.New("--timeout must be non-negative")
	}
	if !validCacheMode(whatttft.CacheMode(c.cacheMode)) {
		return fmt.Errorf("unsupported cache mode %q", c.cacheMode)
	}
	if !validConnectionMode(whatttft.ConnectionMode(c.connectionMode)) {
		return fmt.Errorf("unsupported connection mode %q", c.connectionMode)
	}
	if !validReasoningEffort(c.reasoningEffort) {
		return fmt.Errorf("unsupported reasoning effort %q", c.reasoningEffort)
	}
	if !validOpenAIAPI(openai.API(c.openAIAPI)) {
		return fmt.Errorf("unsupported OpenAI API %q", c.openAIAPI)
	}
	if !validServiceTier(openai.ServiceTier(c.serviceTier)) {
		return fmt.Errorf("unsupported service tier %q", c.serviceTier)
	}

	return nil
}

func validCacheMode(mode whatttft.CacheMode) bool {
	switch mode {
	case whatttft.CacheBust, whatttft.CacheReuse, whatttft.ProviderExplicitCache, whatttft.CacheUnknown:
		return true
	default:
		return false
	}
}

func validConnectionMode(mode whatttft.ConnectionMode) bool {
	switch mode {
	case whatttft.WarmConnections, whatttft.ColdConnections:
		return true
	default:
		return false
	}
}

func validOpenAIAPI(api openai.API) bool {
	switch api {
	case openai.ResponsesAPI, openai.ChatCompletionsAPI:
		return true
	default:
		return false
	}
}

func validServiceTier(tier openai.ServiceTier) bool {
	switch tier {
	case "", openai.ServiceTierAuto, openai.ServiceTierDefault, openai.ServiceTierFlex, openai.ServiceTierScale, openai.ServiceTierPriority:
		return true
	default:
		return false
	}
}

func validReasoningEffort(effort string) bool {
	switch effort {
	case "", "none", "minimal", "low", "medium", "high", "xhigh":
		return true
	default:
		return false
	}
}

type optionalFloat struct {
	value float64
	set   bool
}

func (f *optionalFloat) Set(value string) error {
	parsed, err := parseFiniteFloat(value)
	if err != nil {
		return err
	}

	f.value = parsed
	f.set = true
	return nil
}

func (f *optionalFloat) String() string {
	if f == nil || !f.set {
		return ""
	}

	return fmt.Sprintf("%g", f.value)
}

func parseFiniteFloat(value string) (float64, error) {
	parsed, err := strconv.ParseFloat(value, 64)
	if err != nil {
		return 0, fmt.Errorf("parse float %q: %w", value, err)
	}
	if math.IsInf(parsed, 0) || math.IsNaN(parsed) {
		return 0, fmt.Errorf("float %q is not finite", value)
	}

	return parsed, nil
}
