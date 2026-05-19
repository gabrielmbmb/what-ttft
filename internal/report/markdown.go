package report

import (
	"fmt"
	"sort"
	"strings"

	"github.com/gabrielmbmb/what-ttft/pkg/whatttft"
)

// MarkdownSummary renders a concise Markdown summary focused on p50/p95/p99 metric distributions.
func MarkdownSummary(summary whatttft.RunSummary) string {
	return MarkdownSummaryWithMetadata(summary, RunMetadata{})
}

// MarkdownSummaryWithMetadata renders a Markdown summary with optional multi-target metadata for comparison tables.
func MarkdownSummaryWithMetadata(summary whatttft.RunSummary, metadata RunMetadata) string {
	var builder strings.Builder
	builder.WriteString("# what-ttft summary\n\n")
	fmt.Fprintf(&builder, "total_requests: %d\n", summary.TotalRequests)
	fmt.Fprintf(&builder, "warmup_requests: %d\n", summary.WarmupRequests)
	fmt.Fprintf(&builder, "measured_requests: %d\n", summary.MeasuredRequests)
	fmt.Fprintf(&builder, "successful_requests: %d\n", summary.SuccessfulRequests)
	fmt.Fprintf(&builder, "error_requests: %d\n\n", summary.ErrorRequests)

	if len(summary.Groups) == 0 {
		builder.WriteString("No measured request groups.\n")
		return builder.String()
	}

	if shouldWriteComparisonTable(summary, metadata) {
		writeComparisonMarkdown(&builder, summary, metadata)
	}

	for _, group := range summary.Groups {
		writeGroupMarkdown(&builder, group)
	}

	return builder.String()
}

func shouldWriteComparisonTable(summary whatttft.RunSummary, metadata RunMetadata) bool {
	if len(metadata.Targets) > 0 {
		return true
	}
	for _, group := range summary.Groups {
		if group.TargetID != "" {
			return true
		}
	}

	return false
}

func writeComparisonMarkdown(builder *strings.Builder, summary whatttft.RunSummary, metadata RunMetadata) {
	builder.WriteString("## Target comparison\n\n")
	builder.WriteString("| target | provider | api | requested tier | observed tier | model | ok | err | ttft p50 ms | ttft p95 ms | e2e p50 ms | e2e p95 ms | e2e tps mean | generation tps mean | system tps | rps |\n")
	builder.WriteString("|---|---|---|---|---|---|---:|---:|---:|---:|---:|---:|---:|---:|---:|---:|\n")

	groups := groupsByTarget(summary.Groups)
	if len(metadata.Targets) > 0 {
		for _, target := range metadata.Targets {
			writeComparisonRow(builder, comparisonRowForTarget(target, groups[target.TargetID]))
		}
		builder.WriteString("\n")
		return
	}

	for _, group := range summary.Groups {
		if group.TargetID == "" {
			continue
		}
		writeComparisonRow(builder, comparisonRowForGroup(group))
	}
	builder.WriteString("\n")
}

type comparisonRow struct {
	target             string
	provider           string
	api                string
	requestedTier      string
	observedTier       string
	model              string
	successfulRequests int
	errorRequests      int
	ttftP50            *float64
	ttftP95            *float64
	e2eP50             *float64
	e2eP95             *float64
	e2eTPSMean         *float64
	generationTPSMean  *float64
	systemTPS          *float64
	rps                *float64
}

func comparisonRowForTarget(target RunTargetMetadata, group *whatttft.SummaryGroup) comparisonRow {
	row := comparisonRow{
		target:        firstNonEmpty(target.TargetID, "unknown"),
		provider:      target.Provider,
		api:           target.ProviderAPI,
		requestedTier: firstNonEmpty(target.RequestedServiceTier, "unset"),
		observedTier:  firstNonEmpty(target.ObservedServiceTier, formatStringIntMap(target.ObservedServiceTierCounts), ""),
		model:         target.Model,
	}
	if group == nil {
		return row
	}

	row.successfulRequests = group.SuccessfulRequests
	row.errorRequests = group.ErrorRequests
	row.ttftP50 = group.Metrics.TTFTDeltaMS.P50
	row.ttftP95 = group.Metrics.TTFTDeltaMS.P95
	row.e2eP50 = group.Metrics.E2EDeltaMS.P50
	row.e2eP95 = group.Metrics.E2EDeltaMS.P95
	row.e2eTPSMean = group.Metrics.E2EOutputTPS.Mean
	row.generationTPSMean = group.Metrics.GenerationDeltaOutputTPS.Mean
	row.systemTPS = group.SystemTPS
	row.rps = group.RPS
	if row.observedTier == "" {
		row.observedTier = formatStringIntMap(group.ObservedServiceTierCounts)
	}

	return row
}

func comparisonRowForGroup(group whatttft.SummaryGroup) comparisonRow {
	return comparisonRow{
		target:             firstNonEmpty(group.TargetID, "unknown"),
		provider:           group.Provider,
		requestedTier:      firstNonEmpty(group.RequestedServiceTier, "unset"),
		observedTier:       formatStringIntMap(group.ObservedServiceTierCounts),
		model:              group.Model,
		successfulRequests: group.SuccessfulRequests,
		errorRequests:      group.ErrorRequests,
		ttftP50:            group.Metrics.TTFTDeltaMS.P50,
		ttftP95:            group.Metrics.TTFTDeltaMS.P95,
		e2eP50:             group.Metrics.E2EDeltaMS.P50,
		e2eP95:             group.Metrics.E2EDeltaMS.P95,
		e2eTPSMean:         group.Metrics.E2EOutputTPS.Mean,
		generationTPSMean:  group.Metrics.GenerationDeltaOutputTPS.Mean,
		systemTPS:          group.SystemTPS,
		rps:                group.RPS,
	}
}

func writeComparisonRow(builder *strings.Builder, row comparisonRow) {
	fmt.Fprintf(
		builder,
		"| %s | %s | %s | %s | %s | %s | %d | %d | %s | %s | %s | %s | %s | %s | %s | %s |\n",
		row.target,
		row.provider,
		row.api,
		row.requestedTier,
		firstNonEmpty(row.observedTier, ""),
		row.model,
		row.successfulRequests,
		row.errorRequests,
		formatOptionalFloat(row.ttftP50),
		formatOptionalFloat(row.ttftP95),
		formatOptionalFloat(row.e2eP50),
		formatOptionalFloat(row.e2eP95),
		formatOptionalFloat(row.e2eTPSMean),
		formatOptionalFloat(row.generationTPSMean),
		formatOptionalFloat(row.systemTPS),
		formatOptionalFloat(row.rps),
	)
}

func groupsByTarget(groups []whatttft.SummaryGroup) map[string]*whatttft.SummaryGroup {
	byTarget := make(map[string]*whatttft.SummaryGroup, len(groups))
	for index := range groups {
		if groups[index].TargetID == "" {
			continue
		}
		byTarget[groups[index].TargetID] = &groups[index]
	}

	return byTarget
}

func writeGroupMarkdown(builder *strings.Builder, group whatttft.SummaryGroup) {
	fmt.Fprintf(builder, "## %sprovider=%s model=%s scenario=%s cache=%s connection=%s service_tier=%s\n\n", targetHeadingPrefix(group), group.Provider, group.Model, group.ScenarioName, group.CacheMode, group.ConnectionMode, firstNonEmpty(group.RequestedServiceTier, "unset"))
	fmt.Fprintf(builder, "measured=%d successful=%d errors=%d\n", group.MeasuredRequests, group.SuccessfulRequests, group.ErrorRequests)
	if len(group.ObservedServiceTierCounts) > 0 {
		fmt.Fprintf(builder, "observed_service_tiers=%s\n", formatStringIntMap(group.ObservedServiceTierCounts))
	}
	builder.WriteString("\n")
	builder.WriteString("| metric | count | mean | p50 | p95 | p99 | max |\n")
	builder.WriteString("|---|---:|---:|---:|---:|---:|---:|\n")

	for _, row := range metricRows(group.Metrics) {
		writeMetricRow(builder, row.name, row.distribution)
	}
	builder.WriteString("\n")

	if group.SystemTPS != nil || group.RPS != nil || group.TotalCompletionTokens > 0 {
		fmt.Fprintf(builder, "total_completion_tokens: %d\n", group.TotalCompletionTokens)
		fmt.Fprintf(builder, "system_tps: %s\n", formatOptionalFloat(group.SystemTPS))
		fmt.Fprintf(builder, "rps: %s\n\n", formatOptionalFloat(group.RPS))
	}
}

func targetHeadingPrefix(group whatttft.SummaryGroup) string {
	if group.TargetID == "" {
		return ""
	}
	if group.TargetName == "" {
		return fmt.Sprintf("target=%s ", group.TargetID)
	}

	return fmt.Sprintf("target=%s target_name=%s ", group.TargetID, group.TargetName)
}

type metricRow struct {
	name         string
	distribution whatttft.Distribution
}

func metricRows(metrics whatttft.MetricDistributions) []metricRow {
	return []metricRow{
		{name: "http_ttfb_ms", distribution: metrics.HTTPTTFBMS},
		{name: "headers_latency_ms", distribution: metrics.HeadersLatencyMS},
		{name: "first_event_ms", distribution: metrics.FirstEventMS},
		{name: "ttft_delta_ms", distribution: metrics.TTFTDeltaMS},
		{name: "e2e_delta_ms", distribution: metrics.E2EDeltaMS},
		{name: "stream_total_ms", distribution: metrics.StreamTotalMS},
		{name: "generation_delta_ms", distribution: metrics.GenerationDeltaMS},
		{name: "provider_processing_ms", distribution: metrics.ProviderProcessingMS},
		{name: "server_wait_to_first_byte_ms", distribution: metrics.ServerWaitToFirstByteMS},
		{name: "server_wait_minus_provider_processing_ms", distribution: metrics.ServerWaitMinusProviderProcessingMS},
		{name: "stream_protocol_to_first_output_ms", distribution: metrics.StreamProtocolToFirstOutputMS},
		{name: "dns_ms", distribution: metrics.DNSMS},
		{name: "tcp_connect_ms", distribution: metrics.TCPConnectMS},
		{name: "tls_ms", distribution: metrics.TLSMS},
		{name: "request_write_ms", distribution: metrics.RequestWriteMS},
		{name: "e2e_output_tps", distribution: metrics.E2EOutputTPS},
		{name: "generation_delta_output_tps", distribution: metrics.GenerationDeltaOutputTPS},
	}
}

func writeMetricRow(builder *strings.Builder, name string, distribution whatttft.Distribution) {
	fmt.Fprintf(
		builder,
		"| %s | %d | %s | %s | %s | %s | %s |\n",
		name,
		distribution.Count,
		formatOptionalFloat(distribution.Mean),
		formatOptionalFloat(distribution.P50),
		formatOptionalFloat(distribution.P95),
		formatOptionalFloat(distribution.P99),
		formatOptionalFloat(distribution.Max),
	)
}

func formatOptionalFloat(value *float64) string {
	if value == nil {
		return ""
	}

	return fmt.Sprintf("%.3f", *value)
}

func formatStringIntMap(values map[string]int) string {
	if len(values) == 0 {
		return ""
	}

	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)

	parts := make([]string, 0, len(keys))
	for _, key := range keys {
		parts = append(parts, fmt.Sprintf("%s:%d", key, values[key]))
	}

	return strings.Join(parts, ",")
}
