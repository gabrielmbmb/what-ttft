package charts

import (
	"math"
	"strings"
	"testing"

	"github.com/gabrielmbmb/what-ttft/pkg/whatttft"
)

// TestPercentileBarsEmpty verifies empty input renders an unavailable state.
func TestPercentileBarsEmpty(t *testing.T) {
	got := PercentileBars(nil, 80)
	if !strings.Contains(got, "(no groups)") {
		t.Fatalf("empty percentile bars = %q, want no-groups marker", got)
	}
}

// TestPercentileBarsKnownValues verifies deterministic labels, values, and bars.
func TestPercentileBarsKnownValues(t *testing.T) {
	p50 := 10.0
	p90 := 20.0
	p95 := 30.0
	p99 := 40.0
	got := PercentileBars([]PercentileGroup{{Label: "target-a", P50: &p50, P90: &p90, P95: &p95, P99: &p99}}, 60)
	for _, want := range []string{"target-a", "10.0", "20.0", "30.0", "40.0", "█"} {
		if !strings.Contains(got, want) {
			t.Fatalf("percentile bars missing %q:\n%s", want, got)
		}
	}
}

// TestPercentileBarsMissingValues verifies nil percentiles render as unavailable rather than zero.
func TestPercentileBarsMissingValues(t *testing.T) {
	got := PercentileBars([]PercentileGroup{{Label: "target-a"}}, 20)
	if strings.Count(got, "-") < 4 {
		t.Fatalf("percentile bars missing unavailable markers:\n%s", got)
	}
}

// TestPercentileBarsSkipsNonFinite verifies NaN and Inf values render as unavailable.
func TestPercentileBarsSkipsNonFinite(t *testing.T) {
	nan := math.NaN()
	inf := math.Inf(1)
	got := PercentileBars([]PercentileGroup{{Label: "target-a", P50: &nan, P99: &inf}}, 80)
	if strings.Contains(got, "NaN") || strings.Contains(got, "+Inf") || strings.Contains(got, "Inf") {
		t.Fatalf("percentile bars leaked non-finite values:\n%s", got)
	}
}

// TestPercentileGroupsFromSummary verifies summary groups convert to TTFT percentile groups.
func TestPercentileGroupsFromSummary(t *testing.T) {
	p50 := 12.0
	groups := PercentileGroupsFromSummary([]whatttft.SummaryGroup{{TargetID: "target-a", Metrics: whatttft.MetricDistributions{TTFTDeltaMS: whatttft.Distribution{P50: &p50}}}})
	if len(groups) != 1 || groups[0].Label != "target-a" || groups[0].P50 == nil || *groups[0].P50 != 12 {
		t.Fatalf("percentile groups = %#v, want target-a p50=12", groups)
	}
}
