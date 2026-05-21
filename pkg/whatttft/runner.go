package whatttft

import (
	"context"
	"errors"
	"fmt"
	"sync"
)

// Runner executes benchmark requests for one provider and run configuration.
type Runner struct {
	provider Provider
	cfg      RunConfig
	events   *eventEmitter
}

// RunnerOptions configures optional runner behavior that is not part of the serialized benchmark scenario.
type RunnerOptions struct {
	// Observer receives live benchmark events; nil disables live events and does not change canonical result records.
	Observer RunObserver
}

// RunResult contains raw request records, optional chunks, and a measured-request summary for one run.
type RunResult struct {
	// Records contains one record per attempted request, including warmup requests and failed requests; order matches attempt order.
	Records []RequestRecord `json:"records"`

	// Chunks contains optional per-request output chunks captured only when RunConfig.SaveChunks is true; values may include sensitive generated content.
	Chunks []ChunkRecord `json:"chunks,omitempty"`

	// Summary contains aggregate counts and grouped measured-request statistics over Records, with success and error counts excluding warmup requests.
	Summary RunSummary `json:"summary"`
}

// NewRunner creates a Runner for provider and cfg.
func NewRunner(provider Provider, cfg RunConfig) *Runner {
	return NewRunnerWithOptions(provider, cfg, RunnerOptions{})
}

// NewRunnerWithOptions creates a Runner for provider and cfg with optional live-event observation.
func NewRunnerWithOptions(provider Provider, cfg RunConfig, options RunnerOptions) *Runner {
	return newRunnerWithEmitter(provider, cfg, newEventEmitter(options.Observer))
}

func newRunnerWithEmitter(provider Provider, cfg RunConfig, emitter *eventEmitter) *Runner {
	return &Runner{provider: provider, cfg: cfg, events: emitter}
}

// Run executes the configured warmup phase followed by the measured phase.
func (r *Runner) Run(ctx context.Context) (*RunResult, error) {
	if ctx == nil {
		ctx = context.Background()
	}

	cfg, err := normalizeRunConfig(r.cfg)
	if err != nil {
		r.emitRunError(ctx, EventRunFailed, r.cfg, err)
		return nil, err
	}
	if r.provider == nil {
		providerErr := errors.New("provider is required")
		r.emitRunError(ctx, EventRunFailed, cfg, providerErr)
		return nil, providerErr
	}

	r.emit(ctx, r.baseRunEvent(cfg, EventRunStarted))

	var result *RunResult
	if cfg.Concurrency > 1 {
		result, err = r.runConcurrent(ctx, cfg)
	} else {
		result, err = r.runSequential(ctx, cfg)
	}
	if err != nil {
		kind := EventRunFailed
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			kind = EventRunCanceled
		}
		event := r.baseRunEvent(cfg, kind)
		event.Error = runEventErrorFromError(err)
		if result != nil {
			event.CompletedRequests = len(result.Records)
			event.SuccessfulRequests = result.Summary.SuccessfulRequests
			event.ErrorRequests = result.Summary.ErrorRequests
			event.Summary = &result.Summary
		}
		r.emit(ctx, event)
		return result, err
	}

	event := r.baseRunEvent(cfg, EventRunFinished)
	if result != nil {
		event.CompletedRequests = len(result.Records)
		event.SuccessfulRequests = result.Summary.SuccessfulRequests
		event.ErrorRequests = result.Summary.ErrorRequests
		event.Summary = &result.Summary
	}
	r.emit(ctx, event)

	return result, nil
}

func (r *Runner) runSequential(ctx context.Context, cfg RunConfig) (*RunResult, error) {
	total := cfg.WarmupRequests + cfg.MeasuredRequests
	result := newRunResult(total, cfg.SaveChunks)

	if err := r.runSequentialPhase(ctx, cfg, result, 0, cfg.WarmupRequests, true); err != nil {
		result.Summary = Summarize(result.Records)
		return result, err
	}
	if err := r.runSequentialPhase(ctx, cfg, result, cfg.WarmupRequests, cfg.MeasuredRequests, false); err != nil {
		result.Summary = Summarize(result.Records)
		return result, err
	}

	result.Summary = Summarize(result.Records)
	r.emitSummaryUpdated(ctx, cfg, result)
	return result, nil
}

func (r *Runner) runSequentialPhase(
	ctx context.Context,
	cfg RunConfig,
	result *RunResult,
	startAttempt int,
	count int,
	warmup bool,
) error {
	if count == 0 {
		return nil
	}

	r.emitPhaseEvent(ctx, cfg, EventPhaseStarted, warmup, len(result.Records), nil)
	defer func() {
		r.emitPhaseEvent(ctx, cfg, EventPhaseFinished, warmup, len(result.Records), &result.Summary)
	}()

	for offset := range count {
		if err := ctx.Err(); err != nil {
			return err
		}

		attempt := startAttempt + offset
		recorder := newScheduledRecorder()
		r.emitRequestScheduled(ctx, cfg, attempt, warmup)
		record, chunks := r.runOne(ctx, cfg, attempt, warmup, recorder)
		appendRunOutput(result, record, chunks, cfg.SaveChunks)
		result.Summary = Summarize(result.Records)
		r.emitRequestFinished(ctx, cfg, record, len(result.Records), &result.Summary)
		r.emitSummaryUpdated(ctx, cfg, result)

		if err := ctx.Err(); err != nil {
			return err
		}
	}

	return nil
}

func (r *Runner) runOne(ctx context.Context, cfg RunConfig, attempt int, warmup bool, recorder *Recorder) (RequestRecord, []ChunkRecord) {
	promptPlan := BuildPromptPlan(cfg, attempt, warmup)
	requestID := requestIDForAttempt(cfg, attempt)
	observer := newRequestObserver(requestID, recorder, cfg.SaveChunks)

	r.emitRequestDispatched(ctx, cfg, attempt, warmup, requestID)
	err := r.provider.StreamChat(ctx, ProviderRequest{
		RequestID: requestID,
		Scenario:  cfg.Scenario,
		Prompt:    promptPlan.Prompt,
		Warmup:    warmup,
	}, observer)

	timeline := recorder.Timeline()
	usage := observer.usageSnapshot()
	httpRecord := observer.httpSnapshot()
	outputDeltaCount := observer.visibleOutputDeltaCountSnapshot()
	record := RequestRecord{
		RequestID:            requestID,
		TargetID:             cfg.TargetID,
		TargetName:           cfg.TargetName,
		Provider:             r.provider.Name(),
		Model:                r.provider.Model(),
		ScenarioName:         cfg.Scenario.Name,
		Warmup:               warmup,
		Attempt:              attempt,
		CacheMode:            promptPlan.CacheMode,
		ConnectionMode:       cfg.ConnectionMode,
		RequestedServiceTier: httpRecord.RequestedServiceTier,
		ObservedServiceTier:  httpRecord.ObservedServiceTier,
		PromptHash:           promptPlan.PromptHash,
		PromptTokens:         usage.PromptTokens,
		CompletionTokens:     usage.CompletionTokens,
		TotalTokens:          usage.TotalTokens,
		OutputDeltaCount:     outputDeltaCount,
		Cache:                observer.cacheSnapshot(),
		HTTP:                 httpRecord,
		Timeline:             timeline,
	}
	record.Derived = CalculateDerivedMetricsWithOutputDeltaCount(timeline, record.CompletionTokens, outputDeltaCount)
	if err != nil {
		record.Error = newErrorRecord(err, recorder.ElapsedNS())
	}

	return record, observer.chunkSnapshot()
}

func (r *Runner) baseRunEvent(cfg RunConfig, kind RunEventKind) RunEvent {
	event := RunEvent{
		Kind:             kind,
		TargetID:         cfg.TargetID,
		TargetName:       cfg.TargetName,
		ScenarioName:     cfg.Scenario.Name,
		CacheMode:        cfg.CacheMode,
		ConnectionMode:   cfg.ConnectionMode,
		TotalRequests:    cfg.WarmupRequests + cfg.MeasuredRequests,
		WarmupRequests:   cfg.WarmupRequests,
		MeasuredRequests: cfg.MeasuredRequests,
		Concurrency:      cfg.Concurrency,
	}
	if r.provider != nil {
		event.Provider = r.provider.Name()
		event.Model = r.provider.Model()
	}

	return event
}

func (r *Runner) emit(ctx context.Context, event RunEvent) {
	if r == nil || r.events == nil {
		return
	}
	r.events.emit(ctx, event)
}

func (r *Runner) emitRunError(ctx context.Context, kind RunEventKind, cfg RunConfig, err error) {
	event := r.baseRunEvent(cfg, kind)
	event.Error = runEventErrorFromError(err)
	r.emit(ctx, event)
}

func (r *Runner) emitPhaseEvent(ctx context.Context, cfg RunConfig, kind RunEventKind, warmup bool, completed int, summary *RunSummary) {
	event := r.baseRunEvent(cfg, kind)
	event.Phase = phaseForWarmup(warmup)
	event.Warmup = runEventBoolPointer(warmup)
	event.CompletedRequests = completed
	if summary != nil {
		event.SuccessfulRequests = summary.SuccessfulRequests
		event.ErrorRequests = summary.ErrorRequests
		event.Summary = summary
	}
	r.emit(ctx, event)
}

func (r *Runner) emitRequestScheduled(ctx context.Context, cfg RunConfig, attempt int, warmup bool) {
	event := r.baseRunEvent(cfg, EventRequestScheduled)
	event.Phase = phaseForWarmup(warmup)
	event.Attempt = runEventIntPointer(attempt)
	event.Warmup = runEventBoolPointer(warmup)
	event.RequestID = requestIDForAttempt(cfg, attempt)
	r.emit(ctx, event)
}

func (r *Runner) emitRequestDispatched(ctx context.Context, cfg RunConfig, attempt int, warmup bool, requestID string) {
	event := r.baseRunEvent(cfg, EventRequestDispatched)
	event.Phase = phaseForWarmup(warmup)
	event.Attempt = runEventIntPointer(attempt)
	event.Warmup = runEventBoolPointer(warmup)
	event.RequestID = requestID
	r.emit(ctx, event)
}

func (r *Runner) emitRequestFinished(ctx context.Context, cfg RunConfig, record RequestRecord, completed int, summary *RunSummary) {
	event := r.baseRunEvent(cfg, EventRequestFinished)
	event.TargetID = record.TargetID
	event.TargetName = record.TargetName
	event.Provider = record.Provider
	event.Model = record.Model
	event.ScenarioName = record.ScenarioName
	event.CacheMode = record.CacheMode
	event.ConnectionMode = record.ConnectionMode
	event.RequestedServiceTier = record.RequestedServiceTier
	event.Phase = phaseForWarmup(record.Warmup)
	event.Attempt = runEventIntPointer(record.Attempt)
	event.Warmup = runEventBoolPointer(record.Warmup)
	event.RequestID = record.RequestID
	event.CompletedRequests = completed
	event.Record = &record
	if summary != nil {
		event.SuccessfulRequests = summary.SuccessfulRequests
		event.ErrorRequests = summary.ErrorRequests
		event.Summary = summary
	}
	r.emit(ctx, event)
}

func (r *Runner) emitSummaryUpdated(ctx context.Context, cfg RunConfig, result *RunResult) {
	if result == nil {
		return
	}
	event := r.baseRunEvent(cfg, EventSummaryUpdated)
	event.CompletedRequests = len(result.Records)
	event.SuccessfulRequests = result.Summary.SuccessfulRequests
	event.ErrorRequests = result.Summary.ErrorRequests
	event.Summary = &result.Summary
	r.emit(ctx, event)
}

func phaseForWarmup(warmup bool) RunPhase {
	if warmup {
		return PhaseWarmup
	}

	return PhaseMeasured
}

func runEventIntPointer(value int) *int {
	return &value
}

func runEventBoolPointer(value bool) *bool {
	return &value
}

func runEventErrorFromError(err error) *RunEventError {
	if err == nil {
		return nil
	}
	category := "run"
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		category = "context"
	}

	return &RunEventError{Category: category, Message: err.Error()}
}

func requestIDForAttempt(cfg RunConfig, attempt int) string {
	if cfg.RequestIDPrefix != "" {
		return fmt.Sprintf("%sreq-%06d", cfg.RequestIDPrefix, attempt)
	}

	return fmt.Sprintf("req-%06d", attempt)
}

func normalizeRunConfig(cfg RunConfig) (RunConfig, error) {
	if cfg.WarmupRequests < 0 {
		return RunConfig{}, errors.New("warmup requests must be non-negative")
	}
	if cfg.MeasuredRequests < 0 {
		return RunConfig{}, errors.New("measured requests must be non-negative")
	}
	if cfg.WarmupRequests+cfg.MeasuredRequests == 0 {
		return RunConfig{}, errors.New("at least one request is required")
	}
	if cfg.Concurrency < 0 {
		return RunConfig{}, errors.New("concurrency must be non-negative")
	}
	if cfg.Concurrency == 0 {
		cfg.Concurrency = 1
	}

	if cfg.CacheMode == "" {
		cfg.CacheMode = CacheBust
	}
	switch cfg.CacheMode {
	case CacheBust, CacheReuse, ProviderExplicitCache, CacheUnknown:
	default:
		return RunConfig{}, fmt.Errorf("unsupported cache mode %q", cfg.CacheMode)
	}

	if cfg.ConnectionMode == "" {
		cfg.ConnectionMode = WarmConnections
	}
	switch cfg.ConnectionMode {
	case WarmConnections, ColdConnections:
	default:
		return RunConfig{}, fmt.Errorf("unsupported connection mode %q", cfg.ConnectionMode)
	}

	return cfg, nil
}

func newRunResult(total int, saveChunks bool) *RunResult {
	result := &RunResult{Records: make([]RequestRecord, 0, total)}
	if saveChunks {
		result.Chunks = make([]ChunkRecord, 0)
	}

	return result
}

func appendRunOutput(result *RunResult, record RequestRecord, chunks []ChunkRecord, saveChunks bool) {
	result.Records = append(result.Records, record)
	if saveChunks {
		result.Chunks = append(result.Chunks, chunks...)
	}
}

func newScheduledRecorder() *Recorder {
	recorder := NewRecorder(nil)
	recorder.Mark(EventScheduledAt)
	return recorder
}

func newErrorRecord(err error, atNS int64) *ErrorRecord {
	category := "provider_error"
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		category = "context"
	}

	return &ErrorRecord{
		Category: category,
		Message:  err.Error(),
		AtNS:     atNS,
	}
}

type requestObserver struct {
	mu                  sync.Mutex
	requestID           string
	recorder            *Recorder
	saveChunks          bool
	chunks              []ChunkRecord
	visibleOutputDeltas int
	usage               ProviderUsage
	cache               CacheRecord
	http                HTTPRecord
}

func newRequestObserver(requestID string, recorder *Recorder, saveChunks bool) *requestObserver {
	return &requestObserver{
		requestID:  requestID,
		recorder:   recorder,
		saveChunks: saveChunks,
	}
}

func (o *requestObserver) Mark(name EventName) {
	o.recorder.Mark(name)
}

func (o *requestObserver) MarkFirst(name EventName) {
	o.recorder.MarkFirst(name)
}

func (o *requestObserver) MarkLast(name EventName) {
	o.recorder.MarkLast(name)
}

func (o *requestObserver) OnStreamEvent(StreamEvent) {}

func (o *requestObserver) OnOutputDelta(delta OutputDelta) {
	atNS := o.recorder.ElapsedNS()
	visible := delta.Visible && delta.Text != ""
	if visible {
		o.recorder.MarkFirst(EventFirstOutputDelta)
		o.recorder.MarkLast(EventLastOutputDelta)
	}

	o.mu.Lock()
	defer o.mu.Unlock()

	if visible {
		o.visibleOutputDeltas++
	}
	if !o.saveChunks {
		return
	}

	index := len(o.chunks)
	o.chunks = append(o.chunks, ChunkRecord{
		RequestID:    o.requestID,
		Index:        index,
		AtNS:         atNS,
		Content:      delta.Text,
		Role:         delta.Role,
		FinishReason: delta.FinishReason,
		Empty:        delta.Text == "",
		UsageChunk:   false,
		SSEDataBytes: 0,
	})
}

func (o *requestObserver) OnToken(TokenEvent) {}

func (o *requestObserver) OnUsage(usage ProviderUsage) {
	o.mu.Lock()
	defer o.mu.Unlock()

	o.usage = usage
	if o.saveChunks {
		o.chunks = append(o.chunks, ChunkRecord{
			RequestID:  o.requestID,
			Index:      len(o.chunks),
			AtNS:       o.recorder.ElapsedNS(),
			Empty:      true,
			UsageChunk: true,
		})
	}
}

func (o *requestObserver) OnCache(cache CacheRecord) {
	o.mu.Lock()
	defer o.mu.Unlock()

	o.cache = cache
}

func (o *requestObserver) OnFinish(FinishEvent) {}

func (o *requestObserver) OnHTTP(record HTTPRecord) {
	o.mu.Lock()
	defer o.mu.Unlock()

	o.http = record
}

func (o *requestObserver) usageSnapshot() ProviderUsage {
	o.mu.Lock()
	defer o.mu.Unlock()

	return o.usage
}

func (o *requestObserver) cacheSnapshot() CacheRecord {
	o.mu.Lock()
	defer o.mu.Unlock()

	return o.cache
}

func (o *requestObserver) httpSnapshot() HTTPRecord {
	o.mu.Lock()
	defer o.mu.Unlock()

	return o.http
}

func (o *requestObserver) visibleOutputDeltaCountSnapshot() int {
	o.mu.Lock()
	defer o.mu.Unlock()

	return o.visibleOutputDeltas
}

func (o *requestObserver) chunkSnapshot() []ChunkRecord {
	o.mu.Lock()
	defer o.mu.Unlock()

	return append([]ChunkRecord(nil), o.chunks...)
}
