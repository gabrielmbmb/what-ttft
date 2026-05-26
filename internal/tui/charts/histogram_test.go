package charts

import (
	"math"
	"strings"
	"testing"
)

// TestRenderHistogramChartUsesSemanticLabels verifies the ntcharts adapter labels units and bins.
func TestRenderHistogramChartUsesSemanticLabels(t *testing.T) {
	got := RenderHistogramChart([]float64{10, 20, 30, 40}, HistogramOptions{Width: 80, Height: 8, Bins: 2, Title: "TTFT distribution", Unit: "ms"}, PlainTheme())
	for _, want := range []string{"TTFT distribution (ms)", "bins=2", "n=4", "min=10.0", "max=40.0", "10-25", "25-40"} {
		if !strings.Contains(got, want) {
			t.Fatalf("histogram chart missing %q:\n%s", want, got)
		}
	}
}

// TestRenderHistogramChartHeightEqualBinsShowsBars verifies a focused TTFT panel with one row per bin is not blank.
func TestRenderHistogramChartHeightEqualBinsShowsBars(t *testing.T) {
	got := RenderHistogramChart([]float64{659.9, 931.3, 7673.8, 2988.9, 3820.6, 1200, 2500, 6000}, HistogramOptions{Width: 100, Height: 10, Bins: 8, Title: "TTFT distribution", Unit: "ms"}, PlainTheme())
	if !strings.Contains(got, "660-1537") || !strings.Contains(got, "█") {
		t.Fatalf("histogram chart should show bin labels and bars when height equals bins:\n%s", got)
	}
}

// TestRenderMultiHistogramChartIncludesSeriesLabels verifies benchmark distributions retain model labels.
func TestRenderMultiHistogramChartIncludesSeriesLabels(t *testing.T) {
	got := RenderMultiHistogramChart([]NamedSeries{
		{Label: "gpt-a", Values: []float64{10, 20}},
		{Label: "gpt-b", Values: []float64{30, 40}},
	}, HistogramOptions{Width: 80, Height: 8, Bins: 2, Title: "TTFT distribution", Unit: "ms"}, PlainTheme())
	for _, want := range []string{"TTFT distribution (ms)", "bins=2", "n=4", "series=2", "legend:", "● gpt-a", "◆ gpt-b"} {
		if !strings.Contains(got, want) {
			t.Fatalf("multi-histogram chart missing %q:\n%s", want, got)
		}
	}
}

// TestRenderHistogramChartSkipsNonFinite verifies the ntcharts adapter does not show NaN or Inf.
func TestRenderHistogramChartSkipsNonFinite(t *testing.T) {
	got := RenderHistogramChart([]float64{10, math.NaN(), math.Inf(1), 20}, HistogramOptions{Width: 48, Height: 8, Bins: 2, Title: "TTFT distribution", Unit: "ms"}, PlainTheme())
	if strings.Contains(got, "NaN") || strings.Contains(got, "Inf") {
		t.Fatalf("histogram chart leaked non-finite values:\n%s", got)
	}
}

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
