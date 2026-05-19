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
	return &Runner{provider: provider, cfg: cfg}
}

// Run executes the configured warmup phase followed by the measured phase.
func (r *Runner) Run(ctx context.Context) (*RunResult, error) {
	if ctx == nil {
		ctx = context.Background()
	}

	cfg, err := normalizeRunConfig(r.cfg)
	if err != nil {
		return nil, err
	}
	if r.provider == nil {
		return nil, errors.New("provider is required")
	}

	if cfg.Concurrency > 1 {
		return r.runConcurrent(ctx, cfg)
	}

	return r.runSequential(ctx, cfg)
}

func (r *Runner) runSequential(ctx context.Context, cfg RunConfig) (*RunResult, error) {
	total := cfg.WarmupRequests + cfg.MeasuredRequests
	result := newRunResult(total, cfg.SaveChunks)

	for attempt := 0; attempt < total; attempt++ {
		if err := ctx.Err(); err != nil {
			result.Summary = Summarize(result.Records)
			return result, err
		}

		warmup := attempt < cfg.WarmupRequests
		record, chunks := r.runOne(ctx, cfg, attempt, warmup, newScheduledRecorder())
		appendRunOutput(result, record, chunks, cfg.SaveChunks)

		if err := ctx.Err(); err != nil {
			result.Summary = Summarize(result.Records)
			return result, err
		}
	}

	result.Summary = Summarize(result.Records)
	return result, nil
}

func (r *Runner) runOne(ctx context.Context, cfg RunConfig, attempt int, warmup bool, recorder *Recorder) (RequestRecord, []ChunkRecord) {
	promptPlan := BuildPromptPlan(cfg, attempt, warmup)
	requestID := fmt.Sprintf("req-%06d", attempt)
	observer := newRequestObserver(requestID, recorder, cfg.SaveChunks)

	err := r.provider.StreamChat(ctx, ProviderRequest{
		RequestID: requestID,
		Scenario:  cfg.Scenario,
		Prompt:    promptPlan.Prompt,
		Warmup:    warmup,
	}, observer)

	timeline := recorder.Timeline()
	usage := observer.usageSnapshot()
	httpRecord := observer.httpSnapshot()
	record := RequestRecord{
		RequestID:            requestID,
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
		Cache:                observer.cacheSnapshot(),
		HTTP:                 httpRecord,
		Timeline:             timeline,
	}
	record.Derived = CalculateDerivedMetrics(timeline, record.CompletionTokens)
	if err != nil {
		record.Error = newErrorRecord(err, recorder.ElapsedNS())
	}

	return record, observer.chunkSnapshot()
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
	mu         sync.Mutex
	requestID  string
	recorder   *Recorder
	saveChunks bool
	chunks     []ChunkRecord
	usage      ProviderUsage
	cache      CacheRecord
	http       HTTPRecord
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
	if delta.Visible && delta.Text != "" {
		o.recorder.MarkFirst(EventFirstOutputDelta)
		o.recorder.MarkLast(EventLastOutputDelta)
	}

	if !o.saveChunks {
		return
	}

	o.mu.Lock()
	defer o.mu.Unlock()

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

func (o *requestObserver) chunkSnapshot() []ChunkRecord {
	o.mu.Lock()
	defer o.mu.Unlock()

	return append([]ChunkRecord(nil), o.chunks...)
}
