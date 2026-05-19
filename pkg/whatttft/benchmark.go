package whatttft

import (
	"context"
	"errors"
	"fmt"
	"strings"
)

// TargetOrder describes how a multi-target benchmark scheduled target execution.
type TargetOrder string

const (
	// SerialTargetOrder records that targets were executed one after another in the configured order, so time-of-day and provider-load drift can affect comparisons.
	SerialTargetOrder TargetOrder = "serial"
)

// BenchmarkConfig configures a multi-target benchmark run.
type BenchmarkConfig struct {
	// Name is an optional benchmark label used by callers and reports; empty means no benchmark name was supplied and it must not contain secrets.
	Name string

	// Targets is the ordered list of providers/models to benchmark; it must contain at least one target and the order is preserved for serial execution.
	Targets []BenchmarkTarget
}

// BenchmarkTarget configures one target in a multi-target benchmark.
type BenchmarkTarget struct {
	// ID is the stable benchmark target identifier used for grouping and request ID prefixes; it is sanitized before use, must be unique after sanitization, and must not contain secrets.
	ID string

	// Name is an optional human-readable target label copied to request records and summaries; empty means no separate label was supplied and it must not contain secrets.
	Name string

	// Provider is the provider adapter used for this target; nil is invalid and no network requests are sent until all targets pass preflight validation.
	Provider Provider

	// Config is the single-target run configuration for this target; TargetID, TargetName, and RequestIDPrefix are overwritten from ID and Name during benchmark execution.
	Config RunConfig
}

// BenchmarkResult contains combined raw records, optional chunks, and summary data for a multi-target benchmark.
type BenchmarkResult struct {
	// Records contains one record per attempted request across all targets, including warmup and failed requests; order follows serial target order and then per-target attempt order.
	Records []RequestRecord `json:"records"`

	// Chunks contains optional per-request output chunks captured only when the corresponding target RunConfig.SaveChunks is true; values may include sensitive generated content.
	Chunks []ChunkRecord `json:"chunks,omitempty"`

	// Summary contains aggregate counts and grouped measured-request statistics over Records, with groups split by target ID, provider, model, scenario, cache mode, connection mode, and requested service tier.
	Summary RunSummary `json:"summary"`

	// TargetOrder records the target scheduling strategy used for this benchmark; v0.2 uses serial, and empty means unavailable.
	TargetOrder TargetOrder `json:"target_order"`
}

// RunResult returns a single-run-shaped copy of the benchmark result for report writers that accept RunResult.
func (r *BenchmarkResult) RunResult() *RunResult {
	if r == nil {
		return nil
	}

	return &RunResult{
		Records: append([]RequestRecord(nil), r.Records...),
		Chunks:  append([]ChunkRecord(nil), r.Chunks...),
		Summary: r.Summary,
	}
}

// BenchmarkRunner executes a multi-target benchmark in target order.
type BenchmarkRunner struct {
	cfg BenchmarkConfig
}

// NewBenchmarkRunner creates a runner for a multi-target benchmark configuration.
func NewBenchmarkRunner(cfg BenchmarkConfig) *BenchmarkRunner {
	return &BenchmarkRunner{cfg: cfg}
}

// Run executes all benchmark targets serially and returns combined records, chunks, and summaries.
func (r *BenchmarkRunner) Run(ctx context.Context) (*BenchmarkResult, error) {
	return RunBenchmark(ctx, r.cfg)
}

// RunBenchmark executes cfg across all targets serially and returns combined records, chunks, and summaries.
func RunBenchmark(ctx context.Context, cfg BenchmarkConfig) (*BenchmarkResult, error) {
	if ctx == nil {
		ctx = context.Background()
	}

	targets, err := normalizeBenchmarkConfig(cfg)
	if err != nil {
		return nil, err
	}

	result := &BenchmarkResult{TargetOrder: SerialTargetOrder}
	for _, target := range targets {
		if err := ctx.Err(); err != nil {
			result.Summary = Summarize(result.Records)
			return result, err
		}

		targetResult, runErr := NewRunner(target.provider, target.config).Run(ctx)
		if targetResult != nil {
			result.Records = append(result.Records, targetResult.Records...)
			result.Chunks = append(result.Chunks, targetResult.Chunks...)
		}
		if runErr != nil {
			result.Summary = Summarize(result.Records)
			return result, runErr
		}
	}

	result.Summary = Summarize(result.Records)
	return result, nil
}

type normalizedBenchmarkTarget struct {
	provider Provider
	config   RunConfig
}

func normalizeBenchmarkConfig(cfg BenchmarkConfig) ([]normalizedBenchmarkTarget, error) {
	if len(cfg.Targets) == 0 {
		return nil, errors.New("at least one benchmark target is required")
	}

	targets := make([]normalizedBenchmarkTarget, 0, len(cfg.Targets))
	seenIDs := make(map[string]int, len(cfg.Targets))
	for index, target := range cfg.Targets {
		path := fmt.Sprintf("targets[%d]", index)
		if target.Provider == nil {
			return nil, fmt.Errorf("%s.provider is required", path)
		}

		rawID := firstNonEmptyBenchmarkString(target.ID, target.Config.TargetID)
		id := sanitizeBenchmarkTargetID(rawID)
		if id == "" {
			return nil, fmt.Errorf("%s.id is required", path)
		}
		if previous, exists := seenIDs[id]; exists {
			return nil, fmt.Errorf("%s.id %q duplicates targets[%d].id after sanitization", path, id, previous)
		}
		seenIDs[id] = index

		runConfig, err := normalizeRunConfig(target.Config)
		if err != nil {
			return nil, fmt.Errorf("%s.config: %w", path, err)
		}
		runConfig.TargetID = id
		runConfig.TargetName = firstNonEmptyBenchmarkString(target.Name, target.Config.TargetName)
		runConfig.RequestIDPrefix = id + "-"

		targets = append(targets, normalizedBenchmarkTarget{
			provider: target.Provider,
			config:   runConfig,
		})
	}

	return targets, nil
}

func sanitizeBenchmarkTargetID(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	var builder strings.Builder
	lastSeparator := false
	for _, char := range value {
		if benchmarkTargetIDChar(char) {
			builder.WriteRune(char)
			lastSeparator = false
			continue
		}
		if !lastSeparator {
			builder.WriteByte('-')
			lastSeparator = true
		}
	}

	sanitized := strings.Trim(builder.String(), "-")
	if len(sanitized) > 80 {
		sanitized = strings.Trim(sanitized[:80], "-")
	}

	return sanitized
}

func benchmarkTargetIDChar(char rune) bool {
	return char >= 'a' && char <= 'z' || char >= '0' && char <= '9' || char == '-' || char == '_' || char == '.'
}

func firstNonEmptyBenchmarkString(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}

	return ""
}
