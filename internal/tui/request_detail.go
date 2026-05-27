package tui

import (
	"fmt"
	"strings"

	"github.com/gabrielmbmb/what-ttft/internal/tui/charts"
	"github.com/gabrielmbmb/what-ttft/pkg/whatttft"
)

type requestDetailSection int

const (
	requestDetailSectionIdentity requestDetailSection = iota
	requestDetailSectionOutcome
	requestDetailSectionLatency
	requestDetailSectionTimeline
	requestDetailSectionTransport
	requestDetailSectionUsageCache
	requestDetailSectionOutput
	requestDetailSectionCount
)

func (s *requestExplorerState) moveDetailSection(delta int) {
	count := int(requestDetailSectionCount)
	if count <= 0 {
		s.DetailSection = requestDetailSectionIdentity
		return
	}
	index := int(s.DetailSection.normalize()) + delta
	for index < 0 {
		index += count
	}
	index %= count
	s.DetailSection = requestDetailSection(index)
}

func (section requestDetailSection) normalize() requestDetailSection {
	if section < requestDetailSectionIdentity || section >= requestDetailSectionCount {
		return requestDetailSectionIdentity
	}
	return section
}

func (section requestDetailSection) label() string {
	switch section.normalize() {
	case requestDetailSectionOutcome:
		return "outcome"
	case requestDetailSectionLatency:
		return "latency"
	case requestDetailSectionTimeline:
		return "timeline"
	case requestDetailSectionTransport:
		return "transport"
	case requestDetailSectionUsageCache:
		return "usage/cache"
	case requestDetailSectionOutput:
		return "output"
	default:
		return "identity"
	}
}

func renderRequestDetail(store liveStore, state requestExplorerState, width int, height int, theme tuiTheme) string {
	rows, totalRows, hiddenRows := requestExplorerRows(store, state)
	if len(rows) == 0 {
		body := renderRequestExplorerNoMatches(state, totalRows, hiddenRows)
		return panel("Request detail", fitToBox(body, panelInnerWidth(width), panelInnerHeight(height)), width, height, theme, roleAccent)
	}
	selected := requestRowIndex(rows, state.CursorRequestID)
	if selected < 0 {
		selected = 0
	}
	row := rows[selected]
	record, ok := requestRecordByID(store.completedRecords(), row.RequestID)
	if !ok {
		return panel("Request detail", "selected request record is unavailable\nesc request list", width, height, theme, roleAccent)
	}

	section := state.DetailSection.normalize()
	bodyWidth := panelInnerWidth(width)
	bodyHeight := panelInnerHeight(height)
	lines := []string{
		fmt.Sprintf("request=%s  row=%d/%d  section=%s %d/%d", row.RequestID, selected+1, len(rows), section.label(), int(section)+1, int(requestDetailSectionCount)),
	}
	lines = append(lines, requestDetailSectionLines(row, record, section, bodyWidth, bodyHeight-len(lines)-1, theme)...)
	lines = append(lines, "keys: esc request list  [/] section  ↑/↓ request  o output")
	return panel("Request detail · "+section.label(), fitToBox(strings.Join(lines, "\n"), bodyWidth, bodyHeight), width, height, theme, roleAccent)
}

func requestRecordByID(records []whatttft.RequestRecord, requestID string) (whatttft.RequestRecord, bool) {
	for _, record := range records {
		if record.RequestID == requestID {
			return record, true
		}
	}
	return whatttft.RequestRecord{}, false
}

func requestDetailSectionLines(row requestRow, record whatttft.RequestRecord, section requestDetailSection, width int, height int, theme tuiTheme) []string {
	switch section.normalize() {
	case requestDetailSectionOutcome:
		return requestDetailOutcomeLines(row, record)
	case requestDetailSectionLatency:
		return requestDetailLatencyLines(record)
	case requestDetailSectionTimeline:
		return requestDetailTimelineLines(record, width, height, theme)
	case requestDetailSectionTransport:
		return requestDetailTransportLines(record)
	case requestDetailSectionUsageCache:
		return requestDetailUsageCacheLines(row, record)
	case requestDetailSectionOutput:
		return requestDetailOutputLines(row)
	default:
		return requestDetailIdentityLines(row, record)
	}
}

func requestDetailIdentityLines(row requestRow, record whatttft.RequestRecord) []string {
	return []string{
		fmt.Sprintf("request_id=%s  attempt=%d  ordinal=%d  phase=%s  warmup=%t", row.RequestID, row.Attempt, row.Ordinal+1, row.Phase, record.Warmup),
		fmt.Sprintf("target_id=%s  target_name=%s", requestDetailValue(row.TargetID), requestDetailValue(row.TargetName)),
		fmt.Sprintf("provider=%s  provider_api=%s  model=%s  scenario=%s", requestDetailValue(row.Provider), requestDetailValue(row.ProviderAPI), requestDetailValue(row.Model), requestDetailValue(record.ScenarioName)),
		fmt.Sprintf("cache_mode=%s  connection_mode=%s", requestDetailValue(string(record.CacheMode)), requestDetailValue(string(record.ConnectionMode))),
		fmt.Sprintf("requested_service_tier=%s  observed_service_tier=%s", requestDetailValue(firstNonEmpty(record.RequestedServiceTier, record.HTTP.RequestedServiceTier)), requestDetailValue(firstNonEmpty(record.ObservedServiceTier, record.HTTP.ObservedServiceTier))),
	}
}

func requestDetailOutcomeLines(row requestRow, record whatttft.RequestRecord) []string {
	lines := []string{
		fmt.Sprintf("outcome=%s  finish_reason=%s", row.Outcome, requestDetailValue(row.FinishReason)),
		fmt.Sprintf("http_status=%s  http_text=%s", row.HTTPStatus, requestDetailValue(record.HTTP.Status)),
	}
	if record.Error == nil {
		return append(lines, "error=-")
	}
	lines = append(lines,
		fmt.Sprintf("error_category=%s  retryable=%t  error_at_ms=%s", requestDetailValue(record.Error.Category), record.Error.Retryable, formatNSAsMS(record.Error.AtNS)),
	)
	if record.Error.Message != "" {
		lines = append(lines, "error_message="+requestDetailRedacted(record.Error.Message))
	}
	if record.Error.BodySnippet != "" {
		lines = append(lines, "body_snippet="+requestDetailRedacted(record.Error.BodySnippet))
	}
	return lines
}

func requestDetailLatencyLines(record whatttft.RequestRecord) []string {
	return []string{
		fmt.Sprintf("HTTP TTFB (http_ttfb_ms)=%s  headers_latency_ms=%s", formatMetricValue(record.Derived.HTTPTTFBMS), formatMetricValue(record.Derived.HeadersLatencyMS)),
		fmt.Sprintf("first_event_ms=%s  TTFT delta (ttft_delta_ms)=%s", formatMetricValue(record.Derived.FirstEventMS), formatMetricValue(record.Derived.TTFTDeltaMS)),
		fmt.Sprintf("E2E delta (e2e_delta_ms)=%s  stream_total_ms=%s", formatMetricValue(record.Derived.E2EDeltaMS), formatMetricValue(record.Derived.StreamTotalMS)),
		fmt.Sprintf("generation_delta_ms=%s  stream_protocol_to_first_output_ms=%s", formatMetricValue(record.Derived.GenerationDeltaMS), formatMetricValue(record.Derived.StreamProtocolToFirstOutputMS)),
		fmt.Sprintf("e2e_output_tps=%s  generation_delta_output_tps=%s", formatMetricValue(record.Derived.E2EOutputTPS), formatMetricValue(record.Derived.GenerationDeltaOutputTPS)),
		fmt.Sprintf("provider_processing_ms=%s  server_wait_to_first_byte_ms=%s", formatMetricValue(record.HTTP.ProviderProcessingMS), formatMetricValue(record.Derived.ServerWaitToFirstByteMS)),
	}
}

func requestDetailTimelineLines(record whatttft.RequestRecord, width int, height int, theme tuiTheme) []string {
	lines := []string{
		requestDetailEventLine(record, whatttft.EventRequestStart, whatttft.EventFirstResponseByte, whatttft.EventHeadersReceived),
		requestDetailEventLine(record, whatttft.EventFirstSSEEvent, whatttft.EventFirstOutputDelta, whatttft.EventLastOutputDelta),
		requestDetailEventLine(record, whatttft.EventDone, whatttft.EventBodyEOF),
	}
	if height > 6 && width > 20 {
		chartHeight := height - len(lines)
		if chartHeight > 8 {
			chartHeight = 8
		}
		if chartHeight >= 3 {
			chart := charts.RenderWaterfallChart(record, charts.WaterfallOptions{Width: width, Height: chartHeight, Title: "waterfall ms", EmptyLabel: "waterfall unavailable: timeline events missing"}, theme.chartTheme(roleChartWaterfall))
			lines = append(lines, strings.Split(chart, "\n")...)
		}
	}
	return lines
}

func requestDetailTransportLines(record whatttft.RequestRecord) []string {
	return []string{
		fmt.Sprintf("dns_ms=%s  tcp_connect_ms=%s  tls_ms=%s  request_write_ms=%s", formatMetricValue(record.Derived.DNSMS), formatMetricValue(record.Derived.TCPConnectMS), formatMetricValue(record.Derived.TLSMS), formatMetricValue(record.Derived.RequestWriteMS)),
		fmt.Sprintf("server_wait_to_first_byte_ms=%s  stream_protocol_to_first_output_ms=%s", formatMetricValue(record.Derived.ServerWaitToFirstByteMS), formatMetricValue(record.Derived.StreamProtocolToFirstOutputMS)),
		fmt.Sprintf("network=%s  remote_addr=%s  protocol=%s  tls_version=%s", requestDetailValue(record.HTTP.Network), requestDetailValue(record.HTTP.RemoteAddr), requestDetailValue(record.HTTP.Protocol), requestDetailValue(record.HTTP.TLSVersion)),
		fmt.Sprintf("got_conn=%t  conn=%s  was_idle=%t  idle_ms=%s", record.HTTP.GotConn, requestRowConn(record), record.HTTP.ConnWasIdle, formatNSAsMS(record.HTTP.ConnIdleTimeNS)),
		fmt.Sprintf("compression_disabled=%t  dns_addrs=%d", record.HTTP.CompressionDisabled, record.HTTP.DNSAddrs),
		fmt.Sprintf("dns_error=%s  connect_error=%s  tls_error=%s  write_error=%s", requestDetailRedacted(record.HTTP.DNSError), requestDetailRedacted(record.HTTP.ConnectError), requestDetailRedacted(record.HTTP.TLSError), requestDetailRedacted(record.HTTP.WriteError)),
	}
}

func requestDetailUsageCacheLines(row requestRow, record whatttft.RequestRecord) []string {
	return []string{
		fmt.Sprintf("prompt_tokens=%s  completion_tokens=%s  total_tokens=%s  output_delta_count=%d", formatIntPointer(row.PromptTokens), formatIntPointer(row.CompletionTokens), formatIntPointer(row.TotalTokens), record.OutputDeltaCount),
		fmt.Sprintf("cache=%s  cached_tokens=%s  cache_read_tokens=%s  cache_creation_tokens=%s", row.CacheState, formatIntPointer(record.Cache.PromptCachedTokens), formatIntPointer(record.Cache.CacheReadTokens), formatIntPointer(record.Cache.CacheCreationTokens)),
		fmt.Sprintf("cache_hit=%s  cache_ttl_seconds=%s  cache_id=%s", formatBoolPointer(record.Cache.Hit), formatInt64Pointer(record.Cache.CacheTTLSeconds), requestDetailRedacted(record.Cache.CacheID)),
		fmt.Sprintf("requested_service_tier=%s  observed_service_tier=%s", requestDetailValue(firstNonEmpty(record.RequestedServiceTier, record.HTTP.RequestedServiceTier)), requestDetailValue(firstNonEmpty(record.ObservedServiceTier, record.HTTP.ObservedServiceTier))),
	}
}

func requestDetailOutputLines(row requestRow) []string {
	lines := []string{"output_state=" + row.OutputState}
	switch row.OutputState {
	case requestOutputDisabled:
		lines = append(lines, "output_preview=unavailable; rerun with --save-chunks to write chunks.jsonl and enable request output inspection")
	case requestOutputEmpty:
		lines = append(lines, "output_preview=empty or unavailable for this request")
	default:
		lines = append(lines, "output_preview=pending v0.4 chunks.jsonl loader")
	}
	return lines
}

func requestDetailEventLine(record whatttft.RequestRecord, names ...whatttft.EventName) string {
	parts := make([]string, 0, len(names))
	for _, name := range names {
		value, ok := record.Timeline.EventsNS[name]
		if !ok {
			parts = append(parts, string(name)+"=-")
			continue
		}
		parts = append(parts, fmt.Sprintf("%s=%s", name, formatNSAsMS(value)))
	}
	return strings.Join(parts, "  ")
}

func requestDetailValue(value string) string {
	value = safeInline(value)
	if value == "" {
		return "-"
	}
	return value
}

func requestDetailRedacted(value string) string {
	value = safeInline(value)
	if value == "" {
		return "-"
	}
	lower := strings.ToLower(value)
	for _, marker := range []string{"authorization", "bearer", "api_key", "apikey", "api key", "secret", "token", "cookie", "signed"} {
		if strings.Contains(lower, marker) {
			return "[redacted]"
		}
	}
	return value
}

func formatBoolPointer(value *bool) string {
	if value == nil {
		return "-"
	}
	return fmt.Sprintf("%t", *value)
}

func formatInt64Pointer(value *int64) string {
	if value == nil {
		return "-"
	}
	return fmt.Sprintf("%d", *value)
}

func formatNSAsMS(value int64) string {
	return fmt.Sprintf("%.1f", float64(value)/1_000_000)
}
