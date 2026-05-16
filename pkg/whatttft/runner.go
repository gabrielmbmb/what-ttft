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

	// Summary contains aggregate counts over Records, with success and error counts excluding warmup requests.
	Summary RunSummary `json:"summary"`
}

// RunSummary contains initial aggregate counts for a run.
type RunSummary struct {
	// TotalRequests is the count of all request records, including warmup and measured attempts; units are requests.
	TotalRequests int `json:"total_requests"`

	// WarmupRequests is the count of warmup request records excluded from default success/error summaries; units are requests.
	WarmupRequests int `json:"warmup_requests"`

	// MeasuredRequests is the count of non-warmup request records included in default summaries; units are requests.
	MeasuredRequests int `json:"measured_requests"`

	// SuccessfulRequests is the count of measured request records without an error; units are requests and warmup successes are excluded.
	SuccessfulRequests int `json:"successful_requests"`

	// ErrorRequests is the count of measured request records with an error; units are requests and warmup errors are excluded.
	ErrorRequests int `json:"error_requests"`
}

// NewRunner creates a Runner for provider and cfg.
func NewRunner(provider Provider, cfg RunConfig) *Runner {
	return &Runner{provider: provider, cfg: cfg}
}

// Run executes the configured warmup phase followed by the measured phase sequentially.
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

	total := cfg.WarmupRequests + cfg.MeasuredRequests
	result := &RunResult{Records: make([]RequestRecord, 0, total)}
	if cfg.SaveChunks {
		result.Chunks = make([]ChunkRecord, 0)
	}

	for attempt := 0; attempt < total; attempt++ {
		if err := ctx.Err(); err != nil {
			result.Summary = summarizeRun(result.Records)
			return result, err
		}

		warmup := attempt < cfg.WarmupRequests
		record, chunks := r.runOne(ctx, cfg, attempt, warmup)
		result.Records = append(result.Records, record)
		if cfg.SaveChunks {
			result.Chunks = append(result.Chunks, chunks...)
		}

		if err := ctx.Err(); err != nil {
			result.Summary = summarizeRun(result.Records)
			return result, err
		}
	}

	result.Summary = summarizeRun(result.Records)
	return result, nil
}

func (r *Runner) runOne(ctx context.Context, cfg RunConfig, attempt int, warmup bool) (RequestRecord, []ChunkRecord) {
	recorder := NewRecorder(nil)
	recorder.Mark(EventScheduledAt)

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
	record := RequestRecord{
		RequestID:        requestID,
		Provider:         r.provider.Name(),
		Model:            r.provider.Model(),
		ScenarioName:     cfg.Scenario.Name,
		Warmup:           warmup,
		Attempt:          attempt,
		CacheMode:        promptPlan.CacheMode,
		ConnectionMode:   cfg.ConnectionMode,
		PromptHash:       promptPlan.PromptHash,
		PromptTokens:     usage.PromptTokens,
		CompletionTokens: usage.CompletionTokens,
		TotalTokens:      usage.TotalTokens,
		Cache:            observer.cacheSnapshot(),
		HTTP:             observer.httpSnapshot(),
		Timeline:         timeline,
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
	if cfg.Concurrency > 1 {
		return RunConfig{}, errors.New("fixed-concurrency runner is not implemented yet")
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

func summarizeRun(records []RequestRecord) RunSummary {
	summary := RunSummary{TotalRequests: len(records)}
	for _, record := range records {
		if record.Warmup {
			summary.WarmupRequests++
			continue
		}

		summary.MeasuredRequests++
		if record.Error == nil {
			summary.SuccessfulRequests++
		} else {
			summary.ErrorRequests++
		}
	}

	return summary
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
