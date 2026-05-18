package whatttft

import (
	"fmt"
	"sort"

	"github.com/gabrielmbmb/what-ttft/internal/stats"
)

// Distribution summarizes a finite set of metric values using nearest-rank percentiles.
type Distribution struct {
	// Count is the number of observed values included in this distribution; zero means all pointer fields are nil.
	Count int `json:"count"`

	// Min is the smallest observed value in the metric's documented units, or nil when Count is zero.
	Min *float64 `json:"min,omitempty"`

	// Mean is the arithmetic mean in the metric's documented units, or nil when Count is zero.
	Mean *float64 `json:"mean,omitempty"`

	// P50 is the nearest-rank 50th percentile in the metric's documented units, or nil when Count is zero.
	P50 *float64 `json:"p50,omitempty"`

	// P90 is the nearest-rank 90th percentile in the metric's documented units, or nil when Count is zero.
	P90 *float64 `json:"p90,omitempty"`

	// P95 is the nearest-rank 95th percentile in the metric's documented units, or nil when Count is zero.
	P95 *float64 `json:"p95,omitempty"`

	// P99 is the nearest-rank 99th percentile in the metric's documented units, or nil when Count is zero.
	P99 *float64 `json:"p99,omitempty"`

	// Max is the largest observed value in the metric's documented units, or nil when Count is zero.
	Max *float64 `json:"max,omitempty"`

	// StdDev is the population standard deviation in the metric's documented units, or nil when Count is zero.
	StdDev *float64 `json:"stddev,omitempty"`
}

// RunSummary contains aggregate counts and grouped measured-request statistics for a run.
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

	// ErrorCategories maps redacted error category labels to measured request counts; nil means no measured errors were observed.
	ErrorCategories map[string]int `json:"error_categories,omitempty"`

	// ErrorStatusCodes maps HTTP status-code strings to measured error counts; nil means no measured HTTP status errors were observed.
	ErrorStatusCodes map[string]int `json:"error_status_codes,omitempty"`

	// Groups contains measured-request summaries split by provider, model, scenario, cache mode, and connection mode; nil means no measured records were present.
	Groups []SummaryGroup `json:"groups,omitempty"`
}

// SummaryGroup contains aggregate statistics for one measured-request comparison group.
type SummaryGroup struct {
	// Provider is the normalized provider name for this group; it contains no secrets and empty means unspecified.
	Provider string `json:"provider"`

	// Model is the provider model identifier for this group; it contains no secrets unless a provider embeds sensitive deployment names.
	Model string `json:"model"`

	// ScenarioName is the scenario label for this group; empty means the scenario was unnamed.
	ScenarioName string `json:"scenario_name"`

	// CacheMode is the requested prompt/KV cache behavior for this group; groups never mix cache modes.
	CacheMode CacheMode `json:"cache_mode"`

	// ConnectionMode is the requested HTTP connection reuse behavior for this group; groups never mix connection modes.
	ConnectionMode ConnectionMode `json:"connection_mode"`

	// MeasuredRequests is the count of measured requests in this group; units are requests and warmups are excluded.
	MeasuredRequests int `json:"measured_requests"`

	// SuccessfulRequests is the count of measured requests in this group without an error; units are requests.
	SuccessfulRequests int `json:"successful_requests"`

	// ErrorRequests is the count of measured requests in this group with an error; units are requests.
	ErrorRequests int `json:"error_requests"`

	// ErrorCategories maps redacted error category labels to measured request counts in this group; nil means no group errors were observed.
	ErrorCategories map[string]int `json:"error_categories,omitempty"`

	// ErrorStatusCodes maps HTTP status-code strings to measured error counts in this group; nil means no group HTTP status errors were observed.
	ErrorStatusCodes map[string]int `json:"error_status_codes,omitempty"`

	// Metrics contains latency and throughput distributions over successful measured requests only; empty distributions have Count zero.
	Metrics MetricDistributions `json:"metrics"`

	// CompletionTokenRecords is the count of successful measured requests with provider-reported completion token counts; units are requests.
	CompletionTokenRecords int `json:"completion_token_records"`

	// TotalCompletionTokens is the sum of provider-reported completion tokens over successful measured requests with usage data; units are tokens.
	TotalCompletionTokens int `json:"total_completion_tokens"`

	// SystemTPS is total completion tokens divided by the successful measured response window in tokens/second, or nil when usage or timing is incomplete.
	SystemTPS *float64 `json:"system_tps,omitempty"`

	// RPS is successful measured requests divided by the successful measured response window in requests/second, or nil when timing is incomplete.
	RPS *float64 `json:"rps,omitempty"`

	// Cache summarizes observed prompt/KV cache metadata over successful measured requests in this group.
	Cache CacheSummary `json:"cache"`

	// Connection summarizes observed HTTP connection metadata over successful measured requests in this group.
	Connection ConnectionSummary `json:"connection"`
}

// MetricDistributions contains named request metric distributions for a summary group.
type MetricDistributions struct {
	// HTTPTTFBMS summarizes http_ttfb_ms over successful measured requests; units are milliseconds and Count zero means no values were observed.
	HTTPTTFBMS Distribution `json:"http_ttfb_ms"`

	// HeadersLatencyMS summarizes headers_latency_ms over successful measured requests; units are milliseconds and Count zero means no values were observed.
	HeadersLatencyMS Distribution `json:"headers_latency_ms"`

	// FirstEventMS summarizes first_event_ms over successful measured requests; units are milliseconds and Count zero means no values were observed.
	FirstEventMS Distribution `json:"first_event_ms"`

	// TTFTDeltaMS summarizes ttft_delta_ms over successful measured requests; units are milliseconds and Count zero means no values were observed.
	TTFTDeltaMS Distribution `json:"ttft_delta_ms"`

	// E2EDeltaMS summarizes e2e_delta_ms over successful measured requests; units are milliseconds and Count zero means no values were observed.
	E2EDeltaMS Distribution `json:"e2e_delta_ms"`

	// StreamTotalMS summarizes stream_total_ms over successful measured requests; units are milliseconds and Count zero means no values were observed.
	StreamTotalMS Distribution `json:"stream_total_ms"`

	// GenerationDeltaMS summarizes generation_delta_ms over successful measured requests; units are milliseconds and Count zero means no values were observed.
	GenerationDeltaMS Distribution `json:"generation_delta_ms"`

	// ProviderProcessingMS summarizes provider_processing_ms over successful measured requests; units are milliseconds, values are provider-reported, and Count zero means no values were observed.
	ProviderProcessingMS Distribution `json:"provider_processing_ms"`

	// ServerWaitToFirstByteMS summarizes server_wait_to_first_byte_ms over successful measured requests; units are milliseconds and Count zero means no values were observed.
	ServerWaitToFirstByteMS Distribution `json:"server_wait_to_first_byte_ms"`

	// ServerWaitMinusProviderProcessingMS summarizes server_wait_to_first_byte_ms minus provider_processing_ms over successful measured requests; units are milliseconds, values are diagnostic residuals that can be negative when provider and client timing definitions differ, and Count zero means no values were observed.
	ServerWaitMinusProviderProcessingMS Distribution `json:"server_wait_minus_provider_processing_ms"`

	// StreamProtocolToFirstOutputMS summarizes stream_protocol_to_first_output_ms over successful measured requests; units are milliseconds and Count zero means no values were observed.
	StreamProtocolToFirstOutputMS Distribution `json:"stream_protocol_to_first_output_ms"`

	// DNSMS summarizes dns_ms over successful measured requests; units are milliseconds and Count zero means no values were observed.
	DNSMS Distribution `json:"dns_ms"`

	// TCPConnectMS summarizes tcp_connect_ms over successful measured requests; units are milliseconds and Count zero means no values were observed.
	TCPConnectMS Distribution `json:"tcp_connect_ms"`

	// TLSMS summarizes tls_ms over successful measured requests; units are milliseconds and Count zero means no values were observed.
	TLSMS Distribution `json:"tls_ms"`

	// RequestWriteMS summarizes request_write_ms over successful measured requests; units are milliseconds and Count zero means no values were observed.
	RequestWriteMS Distribution `json:"request_write_ms"`

	// E2EOutputTPS summarizes e2e_output_tps over successful measured requests; units are tokens/second and Count zero means no values were observed.
	E2EOutputTPS Distribution `json:"e2e_output_tps"`
}

// CacheSummary contains observed prompt/KV cache aggregate metadata for one summary group.
type CacheSummary struct {
	// CacheMode is the requested prompt/KV cache mode for this group; groups never mix cache modes.
	CacheMode CacheMode `json:"cache_mode"`

	// CachedPromptTokens summarizes provider-reported cached prompt token counts; units are tokens and Count zero means unavailable.
	CachedPromptTokens Distribution `json:"cached_prompt_tokens"`

	// CacheHitCount is the count of successful measured requests with provider-reported cached prompt tokens greater than zero; units are requests.
	CacheHitCount int `json:"cache_hit_count"`
}

// ConnectionSummary contains observed HTTP connection aggregate metadata for one summary group.
type ConnectionSummary struct {
	// ReusedConnectionCount is the count of successful measured requests that reused an HTTP connection; units are requests.
	ReusedConnectionCount int `json:"reused_connection_count"`

	// ProtocolCounts maps observed HTTP protocol strings, such as HTTP/2.0, to successful measured request counts; nil means no protocols were observed.
	ProtocolCounts map[string]int `json:"protocol_counts,omitempty"`
}

// Summarize aggregates request records into measured-request summaries using nearest-rank percentiles.
func Summarize(records []RequestRecord) RunSummary {
	builders := make(map[string]*summaryGroupBuilder)
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
			incrementStringMap(&summary.ErrorCategories, record.Error.Category)
			incrementStatusMap(&summary.ErrorStatusCodes, errorStatusCode(record))
		}

		key := groupKey(record)
		builder := builders[key]
		if builder == nil {
			builder = newSummaryGroupBuilder(record)
			builders[key] = builder
		}
		builder.add(record)
	}

	summary.Groups = buildSummaryGroups(builders)
	return summary
}

type metricValueSet struct {
	httpTTFB                    []float64
	headersLatency              []float64
	firstEvent                  []float64
	ttftDelta                   []float64
	e2eDelta                    []float64
	streamTotal                 []float64
	generationDelta             []float64
	providerProcessing          []float64
	serverWaitToFirstByte       []float64
	serverWaitMinusProvider     []float64
	streamProtocolToFirstOutput []float64
	dns                         []float64
	tcpConnect                  []float64
	tls                         []float64
	requestWrite                []float64
	e2eOutputTPS                []float64
}

type summaryGroupBuilder struct {
	group                SummaryGroup
	metrics              metricValueSet
	cachedPromptTokens   []float64
	completionTokenTotal int
	completionTokenCount int
	earliestStartNS      int64
	latestResponseNS     int64
	hasWindow            bool
}

func newSummaryGroupBuilder(record RequestRecord) *summaryGroupBuilder {
	return &summaryGroupBuilder{group: SummaryGroup{
		Provider:       record.Provider,
		Model:          record.Model,
		ScenarioName:   record.ScenarioName,
		CacheMode:      record.CacheMode,
		ConnectionMode: record.ConnectionMode,
		Cache:          CacheSummary{CacheMode: record.CacheMode},
	}}
}

func (b *summaryGroupBuilder) add(record RequestRecord) {
	b.group.MeasuredRequests++
	if record.Error != nil {
		b.group.ErrorRequests++
		incrementStringMap(&b.group.ErrorCategories, record.Error.Category)
		incrementStatusMap(&b.group.ErrorStatusCodes, errorStatusCode(record))
		return
	}

	b.group.SuccessfulRequests++
	b.addMetrics(record)
	b.addUsage(record)
	b.addCache(record.Cache)
	b.addConnection(record.HTTP)
	b.addWindow(record)
}

func (b *summaryGroupBuilder) addMetrics(record RequestRecord) {
	metrics := record.Derived
	appendMetric(&b.metrics.httpTTFB, metrics.HTTPTTFBMS)
	appendMetric(&b.metrics.headersLatency, metrics.HeadersLatencyMS)
	appendMetric(&b.metrics.firstEvent, metrics.FirstEventMS)
	appendMetric(&b.metrics.ttftDelta, metrics.TTFTDeltaMS)
	appendMetric(&b.metrics.e2eDelta, metrics.E2EDeltaMS)
	appendMetric(&b.metrics.streamTotal, metrics.StreamTotalMS)
	appendMetric(&b.metrics.generationDelta, metrics.GenerationDeltaMS)
	appendMetric(&b.metrics.providerProcessing, record.HTTP.ProviderProcessingMS)
	appendMetric(&b.metrics.serverWaitToFirstByte, metrics.ServerWaitToFirstByteMS)
	appendServerWaitResidual(&b.metrics.serverWaitMinusProvider, metrics.ServerWaitToFirstByteMS, record.HTTP.ProviderProcessingMS)
	appendMetric(&b.metrics.streamProtocolToFirstOutput, metrics.StreamProtocolToFirstOutputMS)
	appendMetric(&b.metrics.dns, metrics.DNSMS)
	appendMetric(&b.metrics.tcpConnect, metrics.TCPConnectMS)
	appendMetric(&b.metrics.tls, metrics.TLSMS)
	appendMetric(&b.metrics.requestWrite, metrics.RequestWriteMS)
	appendMetric(&b.metrics.e2eOutputTPS, metrics.E2EOutputTPS)
}

func (b *summaryGroupBuilder) addUsage(record RequestRecord) {
	if record.CompletionTokens == nil {
		return
	}

	b.completionTokenCount++
	b.completionTokenTotal += *record.CompletionTokens
}

func (b *summaryGroupBuilder) addCache(cache CacheRecord) {
	if cache.PromptCachedTokens == nil {
		return
	}

	b.cachedPromptTokens = append(b.cachedPromptTokens, float64(*cache.PromptCachedTokens))
	if *cache.PromptCachedTokens > 0 {
		b.group.Cache.CacheHitCount++
	}
}

func (b *summaryGroupBuilder) addConnection(record HTTPRecord) {
	if record.ConnReused {
		b.group.Connection.ReusedConnectionCount++
	}
	if record.Protocol != "" {
		incrementStringMap(&b.group.Connection.ProtocolCounts, record.Protocol)
	}
}

func (b *summaryGroupBuilder) addWindow(record RequestRecord) {
	startNS, endNS, ok := responseWindowEndpoints(record)
	if !ok {
		return
	}
	if !b.hasWindow || startNS < b.earliestStartNS {
		b.earliestStartNS = startNS
	}
	if !b.hasWindow || endNS > b.latestResponseNS {
		b.latestResponseNS = endNS
	}
	b.hasWindow = true
}

func (b *summaryGroupBuilder) build() SummaryGroup {
	b.group.Metrics = MetricDistributions{
		HTTPTTFBMS:                          summarizeValues(b.metrics.httpTTFB),
		HeadersLatencyMS:                    summarizeValues(b.metrics.headersLatency),
		FirstEventMS:                        summarizeValues(b.metrics.firstEvent),
		TTFTDeltaMS:                         summarizeValues(b.metrics.ttftDelta),
		E2EDeltaMS:                          summarizeValues(b.metrics.e2eDelta),
		StreamTotalMS:                       summarizeValues(b.metrics.streamTotal),
		GenerationDeltaMS:                   summarizeValues(b.metrics.generationDelta),
		ProviderProcessingMS:                summarizeValues(b.metrics.providerProcessing),
		ServerWaitToFirstByteMS:             summarizeValues(b.metrics.serverWaitToFirstByte),
		ServerWaitMinusProviderProcessingMS: summarizeValues(b.metrics.serverWaitMinusProvider),
		StreamProtocolToFirstOutputMS:       summarizeValues(b.metrics.streamProtocolToFirstOutput),
		DNSMS:                               summarizeValues(b.metrics.dns),
		TCPConnectMS:                        summarizeValues(b.metrics.tcpConnect),
		TLSMS:                               summarizeValues(b.metrics.tls),
		RequestWriteMS:                      summarizeValues(b.metrics.requestWrite),
		E2EOutputTPS:                        summarizeValues(b.metrics.e2eOutputTPS),
	}
	b.group.Cache.CachedPromptTokens = summarizeValues(b.cachedPromptTokens)
	b.group.CompletionTokenRecords = b.completionTokenCount
	b.group.TotalCompletionTokens = b.completionTokenTotal
	b.group.RPS = b.rps()
	b.group.SystemTPS = b.systemTPS()

	return b.group
}

func (b *summaryGroupBuilder) rps() *float64 {
	windowSeconds, ok := b.windowSeconds()
	if !ok || b.group.SuccessfulRequests == 0 {
		return nil
	}

	value := float64(b.group.SuccessfulRequests) / windowSeconds
	return &value
}

func (b *summaryGroupBuilder) systemTPS() *float64 {
	windowSeconds, ok := b.windowSeconds()
	if !ok || b.completionTokenCount != b.group.SuccessfulRequests {
		return nil
	}

	value := float64(b.completionTokenTotal) / windowSeconds
	return &value
}

func (b *summaryGroupBuilder) windowSeconds() (float64, bool) {
	if !b.hasWindow {
		return 0, false
	}

	windowNS := b.latestResponseNS - b.earliestStartNS
	if windowNS <= 0 {
		return 0, false
	}

	return float64(windowNS) / 1e9, true
}

func buildSummaryGroups(builders map[string]*summaryGroupBuilder) []SummaryGroup {
	keys := make([]string, 0, len(builders))
	for key := range builders {
		keys = append(keys, key)
	}
	sort.Strings(keys)

	groups := make([]SummaryGroup, 0, len(keys))
	for _, key := range keys {
		groups = append(groups, builders[key].build())
	}

	return groups
}

func summarizeValues(values []float64) Distribution {
	return distributionFromStats(stats.Summarize(values))
}

func distributionFromStats(distribution stats.Distribution) Distribution {
	return Distribution{
		Count:  distribution.Count,
		Min:    distribution.Min,
		Mean:   distribution.Mean,
		P50:    distribution.P50,
		P90:    distribution.P90,
		P95:    distribution.P95,
		P99:    distribution.P99,
		Max:    distribution.Max,
		StdDev: distribution.StdDev,
	}
}

func appendMetric(values *[]float64, value *float64) {
	if value != nil {
		*values = append(*values, *value)
	}
}

func appendServerWaitResidual(values *[]float64, serverWaitMS *float64, providerProcessingMS *float64) {
	if serverWaitMS == nil || providerProcessingMS == nil {
		return
	}

	*values = append(*values, *serverWaitMS-*providerProcessingMS)
}

func incrementStringMap(counts *map[string]int, key string) {
	if key == "" {
		key = "unknown"
	}
	if *counts == nil {
		*counts = make(map[string]int)
	}
	(*counts)[key]++
}

func incrementStatusMap(counts *map[string]int, statusCode int) {
	if statusCode == 0 {
		return
	}

	incrementStringMap(counts, fmt.Sprintf("%d", statusCode))
}

func errorStatusCode(record RequestRecord) int {
	if record.Error != nil && record.Error.StatusCode != 0 {
		return record.Error.StatusCode
	}

	return record.HTTP.StatusCode
}

func groupKey(record RequestRecord) string {
	return record.Provider + "\x00" + record.Model + "\x00" + record.ScenarioName + "\x00" + string(record.CacheMode) + "\x00" + string(record.ConnectionMode)
}

func responseWindowEndpoints(record RequestRecord) (int64, int64, bool) {
	if record.Timeline.BaseWallUnixNano == 0 {
		return 0, 0, false
	}

	endNS, ok := eventNS(record.Timeline, EventBodyEOF)
	if !ok {
		endNS, ok = eventNS(record.Timeline, EventLastOutputDelta)
	}
	if !ok {
		return 0, 0, false
	}

	return record.Timeline.BaseWallUnixNano, record.Timeline.BaseWallUnixNano + endNS, true
}
