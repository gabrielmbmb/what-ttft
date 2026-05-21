package charts

import (
	"math"
	"strings"
	"testing"
)

// TestHistogramEmpty verifies empty input renders an unavailable state.
func TestHistogramEmpty(t *testing.T) {
	got := Histogram(nil, 5, 40)
	if !strings.Contains(got, "(no values)") {
		t.Fatalf("empty histogram = %q, want no-values marker", got)
	}
}

// TestHistogramKnownBins verifies deterministic bin counts for known values.
func TestHistogramKnownBins(t *testing.T) {
	got := Histogram([]float64{0, 5, 10, 15}, 2, 36)
	if !strings.Contains(got, "    0.0-    7.5") || !strings.Contains(got, "    7.5-   15.0") {
		t.Fatalf("histogram missing expected ranges:\n%s", got)
	}
	if strings.Count(got, " 2") < 2 {
		t.Fatalf("histogram missing two bins with count 2:\n%s", got)
	}
}

// TestHistogramOneValue verifies one value renders as one populated bin.
func TestHistogramOneValue(t *testing.T) {
	got := Histogram([]float64{42}, 10, 20)
	if !strings.Contains(got, "   42.0-   42.0") || !strings.Contains(got, "# 1") {
		t.Fatalf("one-value histogram unexpected:\n%s", got)
	}
}

// TestHistogramSkipsNonFiniteAndTinyWidth verifies non-finite values are omitted and tiny widths still render.
func TestHistogramSkipsNonFiniteAndTinyWidth(t *testing.T) {
	got := Histogram([]float64{1, math.NaN(), math.Inf(1), 2}, 2, 1)
	if !strings.Contains(got, "# 1") {
		t.Fatalf("tiny histogram missing bar/count:\n%s", got)
	}
}
