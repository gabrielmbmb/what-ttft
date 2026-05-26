package charts

import (
	"math"
	"strings"
	"testing"
)

// TestRenderSeriesChartIncludesMetricSemantics verifies the ntcharts adapter preserves metric names and units.
func TestRenderSeriesChartIncludesMetricSemantics(t *testing.T) {
	got := RenderSeriesChart([]float64{10, 20, 15}, SeriesChartOptions{Width: 48, Height: 10, Title: "ttft_delta_ms", Unit: "ms"}, PlainTheme())
	for _, want := range []string{"ttft_delta_ms (ms)", "x=request order", "y=ms", "latest=15.0 ms"} {
		if !strings.Contains(got, want) {
			t.Fatalf("series chart missing %q:\n%s", want, got)
		}
	}
}

// TestRenderSeriesChartChangesWithValues verifies live chart output changes when observed values change.
func TestRenderSeriesChartChangesWithValues(t *testing.T) {
	first := RenderSeriesChart([]float64{10, 20, 15}, SeriesChartOptions{Width: 48, Height: 10, Title: "ttft_delta_ms", Unit: "ms"}, PlainTheme())
	second := RenderSeriesChart([]float64{10, 20, 30}, SeriesChartOptions{Width: 48, Height: 10, Title: "ttft_delta_ms", Unit: "ms"}, PlainTheme())
	if first == second {
		t.Fatalf("series chart did not change after values changed:\n%s", first)
	}
	if !strings.Contains(second, "latest=30.0 ms") {
		t.Fatalf("updated chart missing latest value:\n%s", second)
	}
}

// TestRenderMultiSeriesChartIncludesSeriesLabels verifies multi-model comparisons keep labels and shared-axis semantics.
func TestRenderMultiSeriesChartIncludesSeriesLabels(t *testing.T) {
	got := RenderMultiSeriesChart([]NamedSeries{
		{Label: "gpt-a", Values: []float64{10, 20, 15}},
		{Label: "gpt-b", Values: []float64{30, 25, 35}},
	}, SeriesChartOptions{Width: 72, Height: 12, Title: "ttft_delta_ms", Unit: "ms"}, PlainTheme())
	for _, want := range []string{"ttft_delta_ms (ms)", "legend:", "● gpt-a", "◆ gpt-b", "series=2", "latest=15.0 ms", "latest=35.0 ms", "x=request order per target"} {
		if !strings.Contains(got, want) {
			t.Fatalf("multi-series chart missing %q:\n%s", want, got)
		}
	}
}

// TestRenderSeriesChartEmptyAndNonFinite verifies unavailable/non-finite inputs render an explicit empty state.
func TestRenderSeriesChartEmptyAndNonFinite(t *testing.T) {
	got := RenderSeriesChart([]float64{math.NaN(), math.Inf(1)}, SeriesChartOptions{Width: 48, Height: 6, Title: "e2e_delta_ms", Unit: "ms", EmptyLabel: "waiting for first successful measured request"}, PlainTheme())
	if !strings.Contains(got, "waiting for first successful measured request") {
		t.Fatalf("empty series chart missing explicit empty state:\n%s", got)
	}
	if strings.Contains(got, "NaN") || strings.Contains(got, "Inf") {
		t.Fatalf("series chart leaked non-finite values:\n%s", got)
	}
}

// TestRenderSeriesChartTinyFallback verifies tiny dimensions use the compact fallback instead of overflowing.
func TestRenderSeriesChartTinyFallback(t *testing.T) {
	got := RenderSeriesChart([]float64{1, 2, 3}, SeriesChartOptions{Width: 12, Height: 3, Title: "tiny", Unit: "ms"}, PlainTheme())
	if !strings.Contains(got, "tiny (ms)") || !strings.Contains(got, "▁") {
		t.Fatalf("tiny series chart missing compact title/sparkline:\n%s", got)
	}
}
