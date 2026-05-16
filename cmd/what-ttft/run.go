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
	"time"

	"github.com/gabrielmbmb/what-ttft/internal/httptracecap"
	"github.com/gabrielmbmb/what-ttft/internal/report"
	"github.com/gabrielmbmb/what-ttft/pkg/provider/openai"
	"github.com/gabrielmbmb/what-ttft/pkg/whatttft"
)

const defaultOpenAIBaseURL = "https://api.openai.com/v1"

func runCommand(args []string, stdout io.Writer, stderr io.Writer) int {
	cfg, _, err := parseRunFlags(args, stderr)
	if err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return 0
		}
		writeFormatted(stderr, "%v\n", err)
		return 2
	}

	result, metadata, err := executeRun(context.Background(), cfg, args)
	if err != nil {
		writeFormatted(stderr, "run failed: %v\n", err)
		return 1
	}

	if err := report.WriteRun(report.WriteOptions{
		OutputDir:  cfg.outputDir,
		Overwrite:  cfg.overwrite,
		SaveChunks: cfg.saveChunks,
		Run:        metadata,
		Result:     result,
	}); err != nil {
		writeFormatted(stderr, "write reports: %v\n", err)
		return 1
	}

	printRunSummary(stdout, cfg, result)
	writeFormatted(stdout, "\nwrote results to %s\n", cfg.outputDir)
	return 0
}

func parseRunFlags(args []string, stderr io.Writer) (runCLIConfig, *flag.FlagSet, error) {
	cfg := runCLIConfig{}
	flagSet := newFlagSet("what-ttft run", stderr)
	flagSet.Usage = func() { printRunUsage(flagSet.Output()) }

	flagSet.StringVar(&cfg.provider, "provider", "openai", "provider to benchmark (openai for v0.1)")
	flagSet.StringVar(&cfg.baseURL, "base-url", defaultOpenAIBaseURL, "provider API base URL")
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
	flagSet.IntVar(&cfg.maxOutputTokens, "max-output-tokens", 64, "maximum output tokens")
	flagSet.Var(&cfg.temperature, "temperature", "optional sampling temperature, e.g. 0")
	flagSet.Var(&cfg.topP, "top-p", "optional nucleus sampling value, e.g. 1")
	flagSet.DurationVar(&cfg.timeout, "timeout", 120*time.Second, "whole-request HTTP client timeout")
	flagSet.StringVar(&cfg.outputDir, "out", "", "output directory")
	flagSet.BoolVar(&cfg.saveChunks, "save-chunks", false, "write chunks.jsonl with generated content")
	flagSet.BoolVar(&cfg.includeUsage, "include-usage", true, "request stream usage chunks when supported")
	flagSet.BoolVar(&cfg.legacyMaxTokens, "legacy-max-tokens", false, "send max_tokens instead of max_completion_tokens")
	flagSet.BoolVar(&cfg.overwrite, "overwrite", false, "overwrite non-empty output directory")

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
  --out DIR

Common flags:
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
  --max-output-tokens N
  --temperature FLOAT
  --top-p FLOAT
  --timeout DURATION
  --save-chunks
  --include-usage
  --legacy-max-tokens
`)
}

func executeRun(ctx context.Context, cfg runCLIConfig, args []string) (*whatttft.RunResult, report.RunMetadata, error) {
	cacheMode := whatttft.CacheMode(cfg.cacheMode)
	connectionMode := whatttft.ConnectionMode(cfg.connectionMode)
	scenario := whatttft.Scenario{
		Name:            "ad-hoc",
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

	apiKey := cfg.apiKey
	if apiKey == "" && cfg.apiKeyEnv != "" {
		apiKey = os.Getenv(cfg.apiKeyEnv)
	}
	client := httptracecap.NewHTTPClient(httptracecap.TransportConfig{
		ConnectionMode: connectionMode,
		Timeout:        cfg.timeout,
	})
	provider := openai.New(openai.Config{
		BaseURL:            cfg.baseURL,
		APIKey:             apiKey,
		APIKeyEnv:          "",
		Model:              cfg.model,
		UseLegacyMaxTokens: cfg.legacyMaxTokens,
		IncludeUsage:       cfg.includeUsage,
		HTTPClient:         client,
	})

	start := time.Now()
	result, err := whatttft.NewRunner(provider, runConfig).Run(ctx)
	end := time.Now()
	metadata := report.RunMetadata{
		Provider:          provider.Name(),
		Model:             provider.Model(),
		BaseURL:           cfg.baseURL,
		Scenario:          scenario,
		RunConfig:         runConfig,
		WallStartUnixNano: start.UnixNano(),
		WallEndUnixNano:   end.UnixNano(),
		Args:              redactArgs(args),
	}
	if err != nil {
		return result, metadata, err
	}

	return result, metadata, nil
}

func printRunSummary(output io.Writer, cfg runCLIConfig, result *whatttft.RunResult) {
	writeFormatted(
		output,
		"provider=%s model=%s scenario=ad-hoc samples=%d warmup=%d concurrency=%d cache=%s connection=%s\n\n",
		cfg.provider,
		cfg.model,
		cfg.samples,
		cfg.warmup,
		cfg.concurrency,
		cfg.cacheMode,
		cfg.connectionMode,
	)
	writeLine(output, "metric                p50      p95      p99      mean")

	var metrics whatttft.MetricDistributions
	if len(result.Summary.Groups) > 0 {
		metrics = result.Summary.Groups[0].Metrics
	}
	printMetricLine(output, "http_ttfb_ms", metrics.HTTPTTFBMS)
	printMetricLine(output, "ttft_delta_ms", metrics.TTFTDeltaMS)
	printMetricLine(output, "e2e_delta_ms", metrics.E2EDeltaMS)
}

func printMetricLine(output io.Writer, name string, distribution whatttft.Distribution) {
	writeFormatted(
		output,
		"%-20s %-8s %-8s %-8s %-8s\n",
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
	maxOutputTokens int
	temperature     optionalFloat
	topP            optionalFloat
	timeout         time.Duration
	outputDir       string
	saveChunks      bool
	includeUsage    bool
	legacyMaxTokens bool
	overwrite       bool
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
	if c.outputDir == "" {
		return errors.New("--out is required")
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
