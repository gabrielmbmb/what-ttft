package charts

import (
	"strings"
	"testing"

	"github.com/gabrielmbmb/what-ttft/pkg/whatttft"
)

// TestTargetTableEmpty verifies empty summary groups render an unavailable state.
func TestTargetTableEmpty(t *testing.T) {
	got := TargetTable(nil, 80)
	if !strings.Contains(got, "(no target groups)") {
		t.Fatalf("empty target table = %q, want no target groups", got)
	}
}

// TestTargetTableValuesAndStableOrder verifies comparison rows include metrics and sort by target key.
func TestTargetTableValuesAndStableOrder(t *testing.T) {
	p50 := 10.0
	e2e := 100.0
	tps := 20.0
	groups := []whatttft.SummaryGroup{
		{TargetID: "target-b", Model: "gpt-b", SuccessfulRequests: 1, Metrics: whatttft.MetricDistributions{TTFTDeltaMS: whatttft.Distribution{P50: &p50}}},
		{TargetID: "target-a", Model: "gpt-a", SuccessfulRequests: 2, ErrorRequests: 1, Metrics: whatttft.MetricDistributions{E2EDeltaMS: whatttft.Distribution{P50: &e2e}, E2EOutputTPS: whatttft.Distribution{Mean: &tps}, GenerationDeltaOutputTPS: whatttft.Distribution{Count: 1, Mean: &tps}}, SystemTPS: &tps, RPS: &p50},
	}
	got := TargetTable(groups, 120)
	if !strings.Contains(got, "target-a") || !strings.Contains(got, "gpt-a") || !strings.Contains(got, "tokens_total") || !strings.Contains(got, "token_recs") || !strings.Contains(got, "100.0") || !strings.Contains(got, "20.0") || !strings.Contains(got, "1/2") || !strings.Contains(got, "e2e_output_tps") {
		t.Fatalf("target table missing expected values:\n%s", got)
	}
	if strings.Index(got, "target-a") > strings.Index(got, "target-b") {
		t.Fatalf("target table order is not stable/sorted:\n%s", got)
	}
}

// TestTargetTableMissingTPS verifies unavailable TPS values render as dashes.
func TestTargetTableMissingTPS(t *testing.T) {
	got := TargetTable([]whatttft.SummaryGroup{{TargetID: "target-a", Model: "gpt-a"}}, 60)
	if strings.Count(got, "-") < 4 {
		t.Fatalf("target table missing unavailable markers:\n%s", got)
	}
}
