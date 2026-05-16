package stats

import (
	"math"
	"testing"
)

// TestSummarizeEmpty verifies empty inputs produce a zero-count distribution with nil statistics.
func TestSummarizeEmpty(t *testing.T) {
	dist := Summarize(nil)

	if dist.Count != 0 {
		t.Fatalf("count = %d, want 0", dist.Count)
	}
	if dist.Min != nil || dist.Mean != nil || dist.P50 != nil || dist.P90 != nil || dist.P95 != nil || dist.P99 != nil || dist.Max != nil || dist.StdDev != nil {
		t.Fatalf("empty distribution should have nil stats: %#v", dist)
	}
}

// TestSummarizeNearestRankPercentiles verifies deterministic nearest-rank percentile behavior.
func TestSummarizeNearestRankPercentiles(t *testing.T) {
	dist := Summarize([]float64{5, 1, 2, 4, 3})

	assertStat(t, "min", dist.Min, 1)
	assertStat(t, "mean", dist.Mean, 3)
	assertStat(t, "p50", dist.P50, 3)
	assertStat(t, "p90", dist.P90, 5)
	assertStat(t, "p95", dist.P95, 5)
	assertStat(t, "p99", dist.P99, 5)
	assertStat(t, "max", dist.Max, 5)
	assertStat(t, "stddev", dist.StdDev, math.Sqrt(2))
}

// TestSummarizeNearestRankEvenCount verifies p50 uses nearest-rank rather than interpolation.
func TestSummarizeNearestRankEvenCount(t *testing.T) {
	dist := Summarize([]float64{1, 2, 3, 4})

	assertStat(t, "p50", dist.P50, 2)
	assertStat(t, "p90", dist.P90, 4)
}

func assertStat(t *testing.T, name string, got *float64, want float64) {
	t.Helper()

	if got == nil {
		t.Fatalf("%s = nil, want %.12g", name, want)
	}
	if math.Abs(*got-want) > 1e-12 {
		t.Fatalf("%s = %.12g, want %.12g", name, *got, want)
	}
}
