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
