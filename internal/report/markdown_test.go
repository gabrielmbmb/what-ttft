package report

import (
	"strings"
	"testing"

	"github.com/gabrielmbmb/what-ttft/pkg/whatttft"
)

// TestMarkdownSummaryIncludesKeyMetricNames verifies the Markdown report contains the core metric table.
func TestMarkdownSummaryIncludesKeyMetricNames(t *testing.T) {
	mean := 100.0
	p50 := 90.0
	p95 := 150.0
	p99 := 200.0
	maxValue := 210.0
	summary := whatttft.RunSummary{
		TotalRequests:      3,
		WarmupRequests:     1,
		MeasuredRequests:   2,
		SuccessfulRequests: 2,
		Groups: []whatttft.SummaryGroup{
			{
				Provider:           "openai",
				Model:              "gpt-test",
				ScenarioName:       "short",
				CacheMode:          whatttft.CacheBust,
				ConnectionMode:     whatttft.WarmConnections,
				MeasuredRequests:   2,
				SuccessfulRequests: 2,
				Metrics: whatttft.MetricDistributions{
					TTFTDeltaMS: whatttft.Distribution{Count: 2, Mean: &mean, P50: &p50, P95: &p95, P99: &p99, Max: &maxValue},
				},
				TotalCompletionTokens: 4,
				SystemTPS:             &mean,
				RPS:                   &p50,
			},
		},
	}

	markdown := MarkdownSummary(summary)
	for _, want := range []string{
		"# what-ttft summary",
		"total_requests: 3",
		"provider=openai model=gpt-test scenario=short cache=cache-bust connection=warm",
		"| metric | count | mean | p50 | p95 | p99 | max |",
		"ttft_delta_ms",
		"http_ttfb_ms",
		"provider_processing_ms",
		"server_wait_minus_provider_processing_ms",
		"e2e_delta_ms",
		"e2e_output_tps",
		"generation_delta_output_tps",
		"system_tps: 100.000",
		"rps: 90.000",
	} {
		if !strings.Contains(markdown, want) {
			t.Fatalf("markdown missing %q:\n%s", want, markdown)
		}
	}
}

// TestMarkdownSummaryIncludesTargetHeading verifies target IDs and names appear in group headings.
func TestMarkdownSummaryIncludesTargetHeading(t *testing.T) {
	markdown := MarkdownSummary(whatttft.RunSummary{Groups: []whatttft.SummaryGroup{{
		TargetID:       "target-a",
		TargetName:     "Target A",
		Provider:       "openai",
		Model:          "gpt-test",
		ScenarioName:   "short",
		CacheMode:      whatttft.CacheReuse,
		ConnectionMode: whatttft.WarmConnections,
	}}})

	if !strings.Contains(markdown, "target=target-a target_name=Target A provider=openai") {
		t.Fatalf("markdown missing target heading:\n%s", markdown)
	}
}

// TestMarkdownSummaryHandlesEmptyGroups verifies an empty summary still renders useful counts.
func TestMarkdownSummaryHandlesEmptyGroups(t *testing.T) {
	markdown := MarkdownSummary(whatttft.RunSummary{TotalRequests: 1, WarmupRequests: 1})

	if !strings.Contains(markdown, "No measured request groups.") {
		t.Fatalf("markdown missing empty-group note:\n%s", markdown)
	}
	if !strings.Contains(markdown, "warmup_requests: 1") {
		t.Fatalf("markdown missing warmup count:\n%s", markdown)
	}
}
