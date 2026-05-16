package report

import (
	"fmt"
	"strings"

	"github.com/gabrielmbmb/what-ttft/pkg/whatttft"
)

// MarkdownSummary renders a concise Markdown summary focused on p50/p95/p99 metric distributions.
func MarkdownSummary(summary whatttft.RunSummary) string {
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

	for _, group := range summary.Groups {
		writeGroupMarkdown(&builder, group)
	}

	return builder.String()
}

func writeGroupMarkdown(builder *strings.Builder, group whatttft.SummaryGroup) {
	fmt.Fprintf(builder, "## provider=%s model=%s scenario=%s cache=%s connection=%s\n\n", group.Provider, group.Model, group.ScenarioName, group.CacheMode, group.ConnectionMode)
	fmt.Fprintf(builder, "measured=%d successful=%d errors=%d\n\n", group.MeasuredRequests, group.SuccessfulRequests, group.ErrorRequests)
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
		{name: "server_wait_to_first_byte_ms", distribution: metrics.ServerWaitToFirstByteMS},
		{name: "stream_protocol_to_first_output_ms", distribution: metrics.StreamProtocolToFirstOutputMS},
		{name: "dns_ms", distribution: metrics.DNSMS},
		{name: "tcp_connect_ms", distribution: metrics.TCPConnectMS},
		{name: "tls_ms", distribution: metrics.TLSMS},
		{name: "request_write_ms", distribution: metrics.RequestWriteMS},
		{name: "e2e_output_tps", distribution: metrics.E2EOutputTPS},
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
