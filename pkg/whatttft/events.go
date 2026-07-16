package whatttft

import (
	"context"
	"sync/atomic"
	"time"
)

// RunEventKind identifies the kind of lifecycle, progress, or reporting event emitted by a benchmark runner.
type RunEventKind string

const (
	// EventBenchmarkStarted is emitted after a multi-target benchmark passes preflight validation and before the first target starts.
	EventBenchmarkStarted RunEventKind = "benchmark_started"

	// EventBenchmarkFinished is emitted after a multi-target benchmark completes and its combined in-memory summary is final.
	EventBenchmarkFinished RunEventKind = "benchmark_finished"

	// EventBenchmarkCanceled is emitted when a multi-target benchmark stops because its context was canceled; partial records may be available.
	EventBenchmarkCanceled RunEventKind = "benchmark_canceled"

	// EventBenchmarkFailed is emitted for a multi-target benchmark-level failure that is not a per-request provider error.
	EventBenchmarkFailed RunEventKind = "benchmark_failed"

	// EventRunStarted is emitted after a single-target run passes validation and before its first request is scheduled.
	EventRunStarted RunEventKind = "run_started"

	// EventRunFinished is emitted after a single-target run completes and its in-memory summary is final.
	EventRunFinished RunEventKind = "run_finished"

	// EventRunCanceled is emitted when a single-target run stops because its context was canceled; partial records may be available.
	EventRunCanceled RunEventKind = "run_canceled"

	// EventRunFailed is emitted for a single-target run-level failure that is not a per-request provider error.
	EventRunFailed RunEventKind = "run_failed"

	// EventTargetStarted is emitted before a multi-target benchmark starts executing one configured target.
	EventTargetStarted RunEventKind = "target_started"

	// EventTargetFinished is emitted after a multi-target benchmark finishes executing one configured target and appends its results.
	EventTargetFinished RunEventKind = "target_finished"

	// EventTargetFailed is emitted for a target-level failure that prevents or interrupts one configured target outside normal per-request provider errors.
	EventTargetFailed RunEventKind = "target_failed"

	// EventPhaseStarted is emitted when a warmup or measured phase starts for a target run.
	EventPhaseStarted RunEventKind = "phase_started"

	// EventPhaseFinished is emitted when a warmup or measured phase finishes or is interrupted for a target run.
	EventPhaseFinished RunEventKind = "phase_finished"

	// EventRequestScheduled is emitted after the runner records scheduled_at and before the request is sent to a worker or executed sequentially.
	EventRequestScheduled RunEventKind = "request_scheduled"

	// EventRequestDispatched is emitted immediately before provider.StreamChat is invoked; it is not the HTTP request_start timeline event.
	EventRequestDispatched RunEventKind = "request_dispatched"

	// EventRequestFinished is emitted after a request record is complete, including any request-level provider error captured in the record.
	EventRequestFinished RunEventKind = "request_finished"

	// EventSummaryUpdated is emitted when a live summary snapshot over currently completed request records is available.
	EventSummaryUpdated RunEventKind = "summary_updated"

	// EventReportWriteStarted is emitted by CLI/reporting code immediately before canonical report files are written.
	EventReportWriteStarted RunEventKind = "report_write_started"

	// EventReportWriteFinished is emitted by CLI/reporting code after canonical report files are written successfully.
	EventReportWriteFinished RunEventKind = "report_write_finished"

	// EventReportWriteFailed is emitted by CLI/reporting code when canonical report writing fails.
	EventReportWriteFailed RunEventKind = "report_write_failed"
)

// RunPhase identifies whether a runner event belongs to warmup or measured requests.
type RunPhase string

const (
	// PhaseWarmup identifies requests intentionally excluded from default measured summaries.
	PhaseWarmup RunPhase = "warmup"

	// PhaseMeasured identifies requests included in default measured summaries.
	PhaseMeasured RunPhase = "measured"
)

// RunEventError is a bounded, redacted error payload for benchmark lifecycle events.
type RunEventError struct {
	// Category is a stable redacted error grouping label such as "context", "validation", or "report_write"; empty means uncategorized.
	Category string `json:"category"`

	// Message is a bounded human-readable error message with secrets redacted; empty means no message was recorded.
	Message string `json:"message"`

	// Retryable is true when the event producer believes the error is transient; false means non-retryable or unknown.
	Retryable bool `json:"retryable"`
}

// RunEventTarget is non-secret benchmark target metadata carried by benchmark-level events.
type RunEventTarget struct {
	// TargetID is the stable sanitized target identifier; empty is invalid for benchmark target metadata and contains no secrets.
	TargetID string `json:"target_id"`

	// TargetName is the optional human-readable target label; empty means unnamed and contains no secrets.
	TargetName string `json:"target_name,omitempty"`

	// Provider is the normalized provider name, such as "openai"; empty means unavailable and contains no secrets.
	Provider string `json:"provider,omitempty"`

	// ProviderAPI is the non-secret provider API surface label, such as "responses"; empty means unavailable.
	ProviderAPI string `json:"provider_api,omitempty"`

	// RequestedServiceTier is the non-secret service tier requested for this target; empty means unset or unavailable.
	RequestedServiceTier string `json:"requested_service_tier,omitempty"`

	// Model is the provider model identifier; empty means unavailable and must not contain API keys or credentials.
	Model string `json:"model,omitempty"`

	// ScenarioName is the scenario grouping label; empty means unnamed or unavailable and contains no secrets.
	ScenarioName string `json:"scenario_name,omitempty"`

	// CacheMode is the requested prompt/KV cache behavior for this target; empty means unavailable.
	CacheMode CacheMode `json:"cache_mode,omitempty"`

	// ConnectionMode is the requested HTTP connection reuse behavior for this target; empty means unavailable.
	ConnectionMode ConnectionMode `json:"connection_mode,omitempty"`

	// TotalRequests is the total planned request count for this target; zero means unavailable or no requests.
	TotalRequests int `json:"total_requests,omitempty"`

	// WarmupRequests is the planned warmup request count for this target; zero means none or unavailable.
	WarmupRequests int `json:"warmup_requests,omitempty"`

	// MeasuredRequests is the planned measured request count for this target; zero means none or unavailable.
	MeasuredRequests int `json:"measured_requests,omitempty"`

	// Concurrency is the configured maximum in-flight request count for this target; zero means unavailable.
	Concurrency int `json:"concurrency,omitempty"`
}

// RunEvent is live benchmark telemetry for TUIs, event logs, and external consumers.
type RunEvent struct {
	// Sequence is a process-local monotonically increasing event sequence number; zero means unassigned, and values are not stable across reruns.
	Sequence int64 `json:"sequence"`

	// Kind identifies the event lifecycle or progress category; empty is invalid for produced events.
	Kind RunEventKind `json:"kind"`

	// WallUnixNano is the wall-clock Unix nanosecond timestamp for event display and ordering; zero means unavailable and this value must not be used for benchmark latency math.
	WallUnixNano int64 `json:"wall_unix_nano"`

	// BenchmarkName is the optional multi-target benchmark label; empty means no benchmark label was supplied and it must not contain secrets.
	BenchmarkName string `json:"benchmark_name,omitempty"`

	// TargetOrder is the multi-target scheduling strategy for the benchmark, such as serial or interleaved; empty means unavailable or a single-target run.
	TargetOrder TargetOrder `json:"target_order,omitempty"`

	// TargetID is the stable sanitized benchmark target identifier associated with the event; empty means no target dimension applies and it must not contain secrets.
	TargetID string `json:"target_id,omitempty"`

	// TargetName is the optional human-readable target label associated with the event; empty means no target label was supplied and it must not contain secrets.
	TargetName string `json:"target_name,omitempty"`

	// Targets contains non-secret benchmark target metadata on benchmark-level events; nil means no target list was attached.
	Targets []RunEventTarget `json:"targets,omitempty"`

	// Provider is the normalized provider name associated with the event, such as "openai"; empty means unavailable and it must not contain secrets.
	Provider string `json:"provider,omitempty"`

	// ProviderAPI is the non-secret provider API surface label associated with the event, such as "responses"; empty means unavailable.
	ProviderAPI string `json:"provider_api,omitempty"`

	// Model is the provider model identifier associated with the event; empty means unavailable and it must not contain API keys or credentials.
	Model string `json:"model,omitempty"`

	// ScenarioName is the benchmark scenario label associated with the event; empty means unnamed or unavailable and it must not contain secrets.
	ScenarioName string `json:"scenario_name,omitempty"`

	// CacheMode is the requested prompt/KV cache behavior for the event context; empty means unavailable and summaries must not mix different non-empty values.
	CacheMode CacheMode `json:"cache_mode,omitempty"`

	// ConnectionMode is the requested HTTP connection reuse behavior for the event context; empty means unavailable and summaries must not mix different non-empty values.
	ConnectionMode ConnectionMode `json:"connection_mode,omitempty"`

	// RequestedServiceTier is the provider service tier requested for this event context, such as OpenAI default or priority; empty means unset or unavailable and it is not secret.
	RequestedServiceTier string `json:"requested_service_tier,omitempty"`

	// Phase identifies whether this event belongs to the warmup or measured phase; empty means the event is not phase-specific.
	Phase RunPhase `json:"phase,omitempty"`

	// Attempt is the zero-based request attempt index for request-specific events; nil means the event is not tied to one request attempt.
	Attempt *int `json:"attempt,omitempty"`

	// Warmup reports whether the request-specific event belongs to a warmup request; nil means the event is not tied to one request or the phase is unavailable.
	Warmup *bool `json:"warmup,omitempty"`

	// RequestID is the stable request identifier for request-specific events; empty means the event is not tied to one request and it must not contain secrets.
	RequestID string `json:"request_id,omitempty"`

	// TotalRequests is the total count of requests planned for the event context; zero means unavailable or no requests.
	TotalRequests int `json:"total_requests,omitempty"`

	// WarmupRequests is the total count of planned warmup requests for the event context; zero means none or unavailable.
	WarmupRequests int `json:"warmup_requests,omitempty"`

	// MeasuredRequests is the total count of planned measured requests for the event context; zero means none or unavailable.
	MeasuredRequests int `json:"measured_requests,omitempty"`

	// CompletedRequests is the count of request records completed when the event was emitted; zero means none or unavailable.
	CompletedRequests int `json:"completed_requests,omitempty"`

	// SuccessfulRequests is the count of completed measured request records without errors when the event was emitted; zero means none or unavailable.
	SuccessfulRequests int `json:"successful_requests,omitempty"`

	// ErrorRequests is the count of completed measured request records with errors when the event was emitted; zero means none or unavailable.
	ErrorRequests int `json:"error_requests,omitempty"`

	// ActiveRequests is the count of requests believed to be in flight when the event was emitted; zero means none or unavailable.
	ActiveRequests int `json:"active_requests,omitempty"`

	// Concurrency is the configured maximum in-flight request count for the event context; zero means unavailable.
	Concurrency int `json:"concurrency,omitempty"`

	// Record is a completed request record snapshot for request_finished events; nil means no completed record is attached, and generated chunk content remains controlled by SaveChunks/report files.
	Record *RequestRecord `json:"record,omitempty"`

	// Summary is a live summary snapshot over records known when the event was emitted; nil means no summary is attached and final summary.json remains authoritative.
	Summary *RunSummary `json:"summary,omitempty"`

	// Error is a bounded, redacted lifecycle error payload for failed or canceled events; nil means no lifecycle error is attached.
	Error *RunEventError `json:"error,omitempty"`

	// OutputDir is the filesystem directory for canonical report files; empty means unavailable and the path may reveal local names but must not contain secrets by construction.
	OutputDir string `json:"output_dir,omitempty"`

	// SaveChunks is true when the run explicitly opted in to writing generated output chunks to chunks.jsonl; false means generated content is not available through live events or report loading.
	SaveChunks bool `json:"save_chunks,omitempty"`

	// Message is optional bounded redacted human-readable event context; empty means no message was supplied.
	Message string `json:"message,omitempty"`
}

// Clone returns a defensive copy of event for asynchronous observers and sinks.
func (event RunEvent) Clone() RunEvent {
	cloned := event
	cloned.Attempt = cloneIntPointer(event.Attempt)
	cloned.Warmup = cloneBoolPointer(event.Warmup)
	if event.Targets != nil {
		cloned.Targets = append([]RunEventTarget(nil), event.Targets...)
	}
	if event.Record != nil {
		record := cloneRequestRecord(*event.Record)
		cloned.Record = &record
	}
	if event.Summary != nil {
		summary := cloneRunSummary(*event.Summary)
		cloned.Summary = &summary
	}
	if event.Error != nil {
		eventError := *event.Error
		cloned.Error = &eventError
	}

	return cloned
}

// RunObserver receives live benchmark events from runners or CLI/reporting code.
type RunObserver interface {
	// OnRunEvent records or forwards event; implementations must redact secrets and should avoid blocking benchmark hot paths.
	OnRunEvent(context.Context, RunEvent)
}

// RunObserverFunc adapts a function into a RunObserver.
type RunObserverFunc func(context.Context, RunEvent)

// OnRunEvent calls fn with event when fn is non-nil.
func (fn RunObserverFunc) OnRunEvent(ctx context.Context, event RunEvent) {
	if fn == nil {
		return
	}
	fn(ctx, event)
}

func notifyRunObserver(ctx context.Context, observer RunObserver, event RunEvent) {
	if observer == nil {
		return
	}
	observer.OnRunEvent(ctx, event)
}

type eventEmitter struct {
	observer RunObserver
	sequence *atomic.Int64
}

func newEventEmitter(observer RunObserver) *eventEmitter {
	return &eventEmitter{observer: observer, sequence: &atomic.Int64{}}
}

func (e *eventEmitter) emit(ctx context.Context, event RunEvent) {
	if e == nil || e.observer == nil {
		return
	}
	if event.Sequence == 0 {
		event.Sequence = e.sequence.Add(1)
	}
	if event.WallUnixNano == 0 {
		event.WallUnixNano = time.Now().UnixNano()
	}
	notifyRunObserver(ctx, e.observer, event.Clone())
}

func cloneRequestRecord(record RequestRecord) RequestRecord {
	cloned := record
	cloned.PromptTokens = cloneIntPointer(record.PromptTokens)
	cloned.CompletionTokens = cloneIntPointer(record.CompletionTokens)
	cloned.TotalTokens = cloneIntPointer(record.TotalTokens)
	cloned.Cache = cloneCacheRecord(record.Cache)
	cloned.HTTP = cloneHTTPRecord(record.HTTP)
	cloned.Timeline = cloneTimeline(record.Timeline)
	cloned.Derived = cloneDerivedMetrics(record.Derived)
	if record.Error != nil {
		errorRecord := *record.Error
		cloned.Error = &errorRecord
	}

	return cloned
}

func cloneCacheRecord(record CacheRecord) CacheRecord {
	cloned := record
	cloned.Hit = cloneBoolPointer(record.Hit)
	cloned.PromptCachedTokens = cloneIntPointer(record.PromptCachedTokens)
	cloned.CacheReadTokens = cloneIntPointer(record.CacheReadTokens)
	cloned.CacheCreationTokens = cloneIntPointer(record.CacheCreationTokens)
	cloned.CacheTTLSeconds = cloneInt64Pointer(record.CacheTTLSeconds)
	cloned.Extra = cloneAnyMap(record.Extra)

	return cloned
}

func cloneHTTPRecord(record HTTPRecord) HTTPRecord {
	cloned := record
	cloned.ProviderProcessingMS = cloneFloat64Pointer(record.ProviderProcessingMS)

	return cloned
}

func cloneTimeline(timeline Timeline) Timeline {
	cloned := timeline
	if timeline.EventsNS != nil {
		cloned.EventsNS = make(map[EventName]int64, len(timeline.EventsNS))
		for key, value := range timeline.EventsNS {
			cloned.EventsNS[key] = value
		}
	}

	return cloned
}

func cloneDerivedMetrics(metrics DerivedMetrics) DerivedMetrics {
	return DerivedMetrics{
		HTTPTTFBMS:                    cloneFloat64Pointer(metrics.HTTPTTFBMS),
		HeadersLatencyMS:              cloneFloat64Pointer(metrics.HeadersLatencyMS),
		FirstEventMS:                  cloneFloat64Pointer(metrics.FirstEventMS),
		TTFTDeltaMS:                   cloneFloat64Pointer(metrics.TTFTDeltaMS),
		E2EDeltaMS:                    cloneFloat64Pointer(metrics.E2EDeltaMS),
		StreamTotalMS:                 cloneFloat64Pointer(metrics.StreamTotalMS),
		GenerationDeltaMS:             cloneFloat64Pointer(metrics.GenerationDeltaMS),
		E2EOutputTPS:                  cloneFloat64Pointer(metrics.E2EOutputTPS),
		GenerationDeltaOutputTPS:      cloneFloat64Pointer(metrics.GenerationDeltaOutputTPS),
		ServerWaitToFirstByteMS:       cloneFloat64Pointer(metrics.ServerWaitToFirstByteMS),
		StreamProtocolToFirstOutputMS: cloneFloat64Pointer(metrics.StreamProtocolToFirstOutputMS),
		DNSMS:                         cloneFloat64Pointer(metrics.DNSMS),
		TCPConnectMS:                  cloneFloat64Pointer(metrics.TCPConnectMS),
		TLSMS:                         cloneFloat64Pointer(metrics.TLSMS),
		RequestWriteMS:                cloneFloat64Pointer(metrics.RequestWriteMS),
	}
}

func cloneRunSummary(summary RunSummary) RunSummary {
	cloned := summary
	cloned.ErrorCategories = cloneStringIntMap(summary.ErrorCategories)
	cloned.ErrorStatusCodes = cloneStringIntMap(summary.ErrorStatusCodes)
	if summary.Groups != nil {
		cloned.Groups = make([]SummaryGroup, len(summary.Groups))
		for index, group := range summary.Groups {
			cloned.Groups[index] = cloneSummaryGroup(group)
		}
	}

	return cloned
}

func cloneSummaryGroup(group SummaryGroup) SummaryGroup {
	cloned := group
	cloned.ObservedServiceTierCounts = cloneStringIntMap(group.ObservedServiceTierCounts)
	cloned.ErrorCategories = cloneStringIntMap(group.ErrorCategories)
	cloned.ErrorStatusCodes = cloneStringIntMap(group.ErrorStatusCodes)
	cloned.Metrics = cloneMetricDistributions(group.Metrics)
	cloned.SystemTPS = cloneFloat64Pointer(group.SystemTPS)
	cloned.RPS = cloneFloat64Pointer(group.RPS)
	cloned.Cache = cloneCacheSummary(group.Cache)
	cloned.Connection = cloneConnectionSummary(group.Connection)

	return cloned
}

func cloneMetricDistributions(metrics MetricDistributions) MetricDistributions {
	return MetricDistributions{
		HTTPTTFBMS:                          cloneDistribution(metrics.HTTPTTFBMS),
		HeadersLatencyMS:                    cloneDistribution(metrics.HeadersLatencyMS),
		FirstEventMS:                        cloneDistribution(metrics.FirstEventMS),
		TTFTDeltaMS:                         cloneDistribution(metrics.TTFTDeltaMS),
		E2EDeltaMS:                          cloneDistribution(metrics.E2EDeltaMS),
		StreamTotalMS:                       cloneDistribution(metrics.StreamTotalMS),
		GenerationDeltaMS:                   cloneDistribution(metrics.GenerationDeltaMS),
		ProviderProcessingMS:                cloneDistribution(metrics.ProviderProcessingMS),
		ServerWaitToFirstByteMS:             cloneDistribution(metrics.ServerWaitToFirstByteMS),
		ServerWaitMinusProviderProcessingMS: cloneDistribution(metrics.ServerWaitMinusProviderProcessingMS),
		StreamProtocolToFirstOutputMS:       cloneDistribution(metrics.StreamProtocolToFirstOutputMS),
		DNSMS:                               cloneDistribution(metrics.DNSMS),
		TCPConnectMS:                        cloneDistribution(metrics.TCPConnectMS),
		TLSMS:                               cloneDistribution(metrics.TLSMS),
		RequestWriteMS:                      cloneDistribution(metrics.RequestWriteMS),
		CompletionTokens:                    cloneDistribution(metrics.CompletionTokens),
		E2EOutputTPS:                        cloneDistribution(metrics.E2EOutputTPS),
		GenerationDeltaOutputTPS:            cloneDistribution(metrics.GenerationDeltaOutputTPS),
	}
}

func cloneCacheSummary(summary CacheSummary) CacheSummary {
	cloned := summary
	cloned.CachedPromptTokens = cloneDistribution(summary.CachedPromptTokens)

	return cloned
}

func cloneConnectionSummary(summary ConnectionSummary) ConnectionSummary {
	cloned := summary
	cloned.ProtocolCounts = cloneStringIntMap(summary.ProtocolCounts)

	return cloned
}

func cloneDistribution(distribution Distribution) Distribution {
	return Distribution{
		Count:  distribution.Count,
		Min:    cloneFloat64Pointer(distribution.Min),
		Mean:   cloneFloat64Pointer(distribution.Mean),
		P50:    cloneFloat64Pointer(distribution.P50),
		P90:    cloneFloat64Pointer(distribution.P90),
		P95:    cloneFloat64Pointer(distribution.P95),
		P99:    cloneFloat64Pointer(distribution.P99),
		Max:    cloneFloat64Pointer(distribution.Max),
		StdDev: cloneFloat64Pointer(distribution.StdDev),
	}
}

func cloneStringIntMap(values map[string]int) map[string]int {
	if values == nil {
		return nil
	}

	cloned := make(map[string]int, len(values))
	for key, value := range values {
		cloned[key] = value
	}

	return cloned
}

func cloneAnyMap(values map[string]any) map[string]any {
	if values == nil {
		return nil
	}

	cloned := make(map[string]any, len(values))
	for key, value := range values {
		cloned[key] = cloneAny(value)
	}

	return cloned
}

func cloneAny(value any) any {
	switch typed := value.(type) {
	case map[string]any:
		return cloneAnyMap(typed)
	case []any:
		cloned := make([]any, len(typed))
		for index, item := range typed {
			cloned[index] = cloneAny(item)
		}
		return cloned
	case []string:
		return append([]string(nil), typed...)
	case []int:
		return append([]int(nil), typed...)
	case []float64:
		return append([]float64(nil), typed...)
	default:
		return value
	}
}

func cloneIntPointer(value *int) *int {
	if value == nil {
		return nil
	}
	cloned := *value

	return &cloned
}

func cloneInt64Pointer(value *int64) *int64 {
	if value == nil {
		return nil
	}
	cloned := *value

	return &cloned
}

func cloneFloat64Pointer(value *float64) *float64 {
	if value == nil {
		return nil
	}
	cloned := *value

	return &cloned
}

func cloneBoolPointer(value *bool) *bool {
	if value == nil {
		return nil
	}
	cloned := *value

	return &cloned
}
