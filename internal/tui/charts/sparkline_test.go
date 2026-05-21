package charts

import (
	"math"
	"testing"
)

// TestSparklineHandlesEmptyAndTinyWidths verifies unavailable data degrades to an empty string.
func TestSparklineHandlesEmptyAndTinyWidths(t *testing.T) {
	if got := Sparkline(nil, 10); got != "" {
		t.Fatalf("empty sparkline = %q, want empty", got)
	}
	if got := Sparkline([]float64{1, 2, 3}, 0); got != "" {
		t.Fatalf("zero-width sparkline = %q, want empty", got)
	}
}

// TestSparklineRendersKnownValues verifies deterministic sparkline output for a simple increasing sequence.
func TestSparklineRendersKnownValues(t *testing.T) {
	got := Sparkline([]float64{1, 2, 3, 4}, 10)
	if got != "▁▃▆█" {
		t.Fatalf("sparkline = %q, want ▁▃▆█", got)
	}
}

// TestSparklineDownsamplesAndSkipsNonFinite verifies width limits and NaN/Inf filtering.
func TestSparklineDownsamplesAndSkipsNonFinite(t *testing.T) {
	got := Sparkline([]float64{1, math.NaN(), 2, math.Inf(1), 3, 4}, 2)
	if got != "▁█" {
		t.Fatalf("downsampled sparkline = %q, want ▁█", got)
	}
}

// TestSparklineOneValue verifies a single finite value renders as one low block.
func TestSparklineOneValue(t *testing.T) {
	if got := Sparkline([]float64{42}, 10); got != "▁" {
		t.Fatalf("single-value sparkline = %q, want ▁", got)
	}
}
