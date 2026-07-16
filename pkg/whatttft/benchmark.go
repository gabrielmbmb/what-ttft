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

	// InterleavedTargetOrder records that all targets were executed together under one shared concurrency budget, interleaving requests round-robin across targets so every target runs in the same time window; a target's effective concurrency is not fixed because idle capacity is shared with targets that still have pending requests.
	InterleavedTargetOrder TargetOrder = "interleaved"
)

// ValidTargetOrder reports whether order is a supported target scheduling strategy or empty (which defaults to serial).
func ValidTargetOrder(order TargetOrder) bool {
	switch order {
	case "", SerialTargetOrder, InterleavedTargetOrder:
		return true
	default:
		return false
	}
}

// BenchmarkConfig configures a multi-target benchmark run.
type BenchmarkConfig struct {
	// Name is an optional benchmark label used by callers and reports; empty means no benchmark name was supplied and it must not contain secrets.
	Name string

	// Targets is the ordered list of providers/models to benchmark; it must contain at least one target and the order is preserved for serial execution.
	Targets []BenchmarkTarget

	// TargetOrder selects how targets are scheduled; empty defaults to SerialTargetOrder, and InterleavedTargetOrder runs all targets together under one shared concurrency budget.
	TargetOrder TargetOrder
}

// BenchmarkTarget configures one target in a multi-target benchmark.
type BenchmarkTarget struct {
	// ID is the stable benchmark target identifier used for grouping and request ID prefixes; it is sanitized before use, must be unique after sanitization, and must not contain secrets.
	ID string

	// Name is an optional human-readable target label copied to request records and summaries; empty means no separate label was supplied and it must not contain secrets.
	Name string

	// Provider is the provider adapter used for this target; nil is invalid and no network requests are sent until all targets pass preflight validation.
	Provider Provider

	// ProviderAPI is the non-secret provider API surface label for display, such as "responses" or "chat-completions"; empty means unavailable.
	ProviderAPI string

	// RequestedServiceTier is the non-secret service tier requested for display, such as "default" or "priority"; empty means unset or unavailable.
	RequestedServiceTier string

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
	cfg    BenchmarkConfig
	events *eventEmitter
}

// BenchmarkOptions configures optional multi-target benchmark behavior that is not part of the serialized benchmark scenario.
type BenchmarkOptions struct {
	// Observer receives live benchmark events; nil disables live events and does not change canonical result records.
	Observer RunObserver
}

// NewBenchmarkRunner creates a runner for a multi-target benchmark configuration.
func NewBenchmarkRunner(cfg BenchmarkConfig) *BenchmarkRunner {
	return NewBenchmarkRunnerWithOptions(cfg, BenchmarkOptions{})
}

// NewBenchmarkRunnerWithOptions creates a runner for a multi-target benchmark configuration with optional live-event observation.
func NewBenchmarkRunnerWithOptions(cfg BenchmarkConfig, options BenchmarkOptions) *BenchmarkRunner {
	return &BenchmarkRunner{cfg: cfg, events: newEventEmitter(options.Observer)}
}

// Run executes all benchmark targets serially and returns combined records, chunks, and summaries.
func (r *BenchmarkRunner) Run(ctx context.Context) (*BenchmarkResult, error) {
	return r.run(ctx)
}

// RunBenchmark executes cfg across all targets serially and returns combined records, chunks, and summaries.
func RunBenchmark(ctx context.Context, cfg BenchmarkConfig) (*BenchmarkResult, error) {
	return RunBenchmarkWithOptions(ctx, cfg, BenchmarkOptions{})
}

// RunBenchmarkWithOptions executes cfg across all targets serially with optional live-event observation.
func RunBenchmarkWithOptions(ctx context.Context, cfg BenchmarkConfig, options BenchmarkOptions) (*BenchmarkResult, error) {
	return NewBenchmarkRunnerWithOptions(cfg, options).Run(ctx)
}

func (r *BenchmarkRunner) run(ctx context.Context) (*BenchmarkResult, error) {
	if ctx == nil {
		ctx = context.Background()
	}

	order := r.cfg.TargetOrder
	if order == "" {
		order = SerialTargetOrder
	}
	if !ValidTargetOrder(order) {
		err := fmt.Errorf("unsupported target order %q", r.cfg.TargetOrder)
		r.emitBenchmarkError(ctx, EventBenchmarkFailed, err, nil)
		return nil, err
	}

	targets, err := normalizeBenchmarkConfig(r.cfg)
	if err != nil {
		r.emitBenchmarkError(ctx, EventBenchmarkFailed, err, nil)
		return nil, err
	}

	result := &BenchmarkResult{TargetOrder: order}
	r.emit(ctx, r.baseBenchmarkEvent(EventBenchmarkStarted, targets, result))

	var runErr error
	if order == InterleavedTargetOrder {
		runErr = r.runInterleaved(ctx, targets, result)
	} else {
		runErr = r.runSerial(ctx, targets, result)
	}

	result.Summary = Summarize(result.Records)
	if runErr != nil {
		kind := EventBenchmarkFailed
		if errors.Is(runErr, context.Canceled) || errors.Is(runErr, context.DeadlineExceeded) {
			kind = EventBenchmarkCanceled
		}
		r.emitBenchmarkError(ctx, kind, runErr, result)
		return result, runErr
	}

	finished := r.baseBenchmarkEvent(EventBenchmarkFinished, targets, result)
	finished.Summary = &result.Summary
	r.emit(ctx, finished)
	return result, nil
}

// runSerial executes targets one after another, each with its own runner and concurrency.
func (r *BenchmarkRunner) runSerial(ctx context.Context, targets []normalizedBenchmarkTarget, result *BenchmarkResult) error {
	for _, target := range targets {
		if err := ctx.Err(); err != nil {
			return err
		}

		r.emit(ctx, benchmarkTargetEvent(EventTargetStarted, r.cfg.Name, target, result))
		targetResult, runErr := newRunnerWithEmitter(target.provider, target.config, r.events).Run(ctx)
		if targetResult != nil {
			result.Records = append(result.Records, targetResult.Records...)
			result.Chunks = append(result.Chunks, targetResult.Chunks...)
			result.Summary = Summarize(result.Records)
		}
		if runErr != nil {
			targetFailed := benchmarkTargetEvent(EventTargetFailed, r.cfg.Name, target, result)
			targetFailed.Error = runEventErrorFromError(runErr)
			r.emit(ctx, targetFailed)
			return runErr
		}
		r.emit(ctx, benchmarkTargetEvent(EventTargetFinished, r.cfg.Name, target, result))
	}

	return nil
}

func (r *BenchmarkRunner) emit(ctx context.Context, event RunEvent) {
	if r == nil || r.events == nil {
		return
	}
	r.events.emit(ctx, event)
}

func (r *BenchmarkRunner) emitBenchmarkError(ctx context.Context, kind RunEventKind, err error, result *BenchmarkResult) {
	event := RunEvent{
		Kind:          kind,
		BenchmarkName: r.cfg.Name,
		Error:         runEventErrorFromError(err),
	}
	if result != nil {
		event.CompletedRequests = len(result.Records)
		event.SuccessfulRequests = result.Summary.SuccessfulRequests
		event.ErrorRequests = result.Summary.ErrorRequests
		event.Summary = &result.Summary
	}
	r.emit(ctx, event)
}

func (r *BenchmarkRunner) baseBenchmarkEvent(kind RunEventKind, targets []normalizedBenchmarkTarget, result *BenchmarkResult) RunEvent {
	totalWarmup := 0
	totalMeasured := 0
	saveChunks := false
	for _, target := range targets {
		totalWarmup += target.config.WarmupRequests
		totalMeasured += target.config.MeasuredRequests
		saveChunks = saveChunks || target.config.SaveChunks
	}
	event := RunEvent{
		Kind:             kind,
		BenchmarkName:    r.cfg.Name,
		Targets:          benchmarkEventTargets(targets),
		SaveChunks:       saveChunks,
		TotalRequests:    totalWarmup + totalMeasured,
		WarmupRequests:   totalWarmup,
		MeasuredRequests: totalMeasured,
	}
	if result != nil {
		event.TargetOrder = result.TargetOrder
		event.CompletedRequests = len(result.Records)
		event.SuccessfulRequests = result.Summary.SuccessfulRequests
		event.ErrorRequests = result.Summary.ErrorRequests
	}

	return event
}

func benchmarkEventTargets(targets []normalizedBenchmarkTarget) []RunEventTarget {
	eventTargets := make([]RunEventTarget, 0, len(targets))
	for _, target := range targets {
		eventTarget := RunEventTarget{
			TargetID:             target.config.TargetID,
			TargetName:           target.config.TargetName,
			ProviderAPI:          target.providerAPI,
			RequestedServiceTier: target.requestedServiceTier,
			ScenarioName:         target.config.Scenario.Name,
			CacheMode:            target.config.CacheMode,
			ConnectionMode:       target.config.ConnectionMode,
			TotalRequests:        target.config.WarmupRequests + target.config.MeasuredRequests,
			WarmupRequests:       target.config.WarmupRequests,
			MeasuredRequests:     target.config.MeasuredRequests,
			Concurrency:          target.config.Concurrency,
		}
		if target.provider != nil {
			eventTarget.Provider = target.provider.Name()
			eventTarget.Model = target.provider.Model()
		}
		eventTargets = append(eventTargets, eventTarget)
	}
	return eventTargets
}

func benchmarkTargetEvent(kind RunEventKind, benchmarkName string, target normalizedBenchmarkTarget, result *BenchmarkResult) RunEvent {
	event := RunEvent{
		Kind:                 kind,
		BenchmarkName:        benchmarkName,
		TargetID:             target.config.TargetID,
		TargetName:           target.config.TargetName,
		ProviderAPI:          target.providerAPI,
		ScenarioName:         target.config.Scenario.Name,
		CacheMode:            target.config.CacheMode,
		ConnectionMode:       target.config.ConnectionMode,
		RequestedServiceTier: target.requestedServiceTier,
		SaveChunks:           target.config.SaveChunks,
		TotalRequests:        target.config.WarmupRequests + target.config.MeasuredRequests,
		WarmupRequests:       target.config.WarmupRequests,
		MeasuredRequests:     target.config.MeasuredRequests,
		Concurrency:          target.config.Concurrency,
	}
	if target.provider != nil {
		event.Provider = target.provider.Name()
		event.Model = target.provider.Model()
	}
	if result != nil {
		event.CompletedRequests = len(result.Records)
		event.SuccessfulRequests = result.Summary.SuccessfulRequests
		event.ErrorRequests = result.Summary.ErrorRequests
		event.Summary = &result.Summary
	}

	return event
}

type normalizedBenchmarkTarget struct {
	provider             Provider
	providerAPI          string
	requestedServiceTier string
	config               RunConfig
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
			provider:             target.Provider,
			providerAPI:          target.ProviderAPI,
			requestedServiceTier: target.RequestedServiceTier,
			config:               runConfig,
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
