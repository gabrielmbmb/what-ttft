package tui

import (
	"fmt"
	"strings"
	"testing"
)

// TestBenchMetricsTableHighlightsBestValues verifies latency minima and throughput maxima use the emphasized good-value style.
func TestBenchMetricsTableHighlightsBestValues(t *testing.T) {
	first := benchModelMetricRow{
		Target: targetRow{ID: "target-a", Total: 1, Completed: 1, Successful: 1, Visible: true},
		Label:  "model-a",
		TTFT:   metricRow{P50: testFloat64(10), P95: testFloat64(20)},
		E2E:    metricRow{P50: testFloat64(100), P95: testFloat64(120)},
		TPS:    metricRow{Mean: testFloat64(5)},
	}
	second := benchModelMetricRow{
		Target: targetRow{ID: "target-b", Total: 1, Completed: 1, Successful: 1, Visible: true},
		Label:  "model-b",
		TTFT:   metricRow{P50: testFloat64(30), P95: testFloat64(40)},
		E2E:    metricRow{P50: testFloat64(200), P95: testFloat64(220)},
		TPS:    metricRow{Mean: testFloat64(9)},
	}
	rows := []benchModelMetricRow{first, second}
	theme := newTheme(false)
	best := bestBenchMetricValues(rows)
	firstLine := benchMetricsTableLine(first, true, 140, best, theme)
	secondLine := benchMetricsTableLine(second, false, 140, best, theme)

	for _, value := range []float64{10, 20, 100, 120} {
		want := theme.render(roleGood, fmt.Sprintf("%8s", formatMetricValue(testFloat64(value))))
		if !strings.Contains(firstLine, want) {
			t.Fatalf("lower best value %.1f was not highlighted:\n%s", value, firstLine)
		}
	}
	wantTPS := theme.render(roleGood, fmt.Sprintf("%8s", formatMetricValue(testFloat64(9))))
	if !strings.Contains(secondLine, wantTPS) {
		t.Fatalf("higher best TPS was not highlighted:\n%s", secondLine)
	}
	if strings.Contains(firstLine, theme.render(roleGood, fmt.Sprintf("%8s", "5.0"))) {
		t.Fatalf("lower TPS was incorrectly highlighted:\n%s", firstLine)
	}
	if strings.Contains(secondLine, theme.render(roleGood, fmt.Sprintf("%8s", "30.0"))) {
		t.Fatalf("higher latency was incorrectly highlighted:\n%s", secondLine)
	}
}

// TestBenchMetricsTableHighlightsTiedBestValues verifies equally best models are all emphasized.
func TestBenchMetricsTableHighlightsTiedBestValues(t *testing.T) {
	rows := []benchModelMetricRow{
		{TTFT: metricRow{P50: testFloat64(10)}},
		{TTFT: metricRow{P50: testFloat64(10)}},
	}
	best := bestBenchMetricValues(rows)
	theme := newTheme(false)
	want := theme.render(roleGood, fmt.Sprintf("%7s", "10.0"))
	for index, row := range rows {
		line := benchMetricsTableLine(row, false, 100, best, theme)
		if !strings.Contains(line, want) {
			t.Fatalf("tied row %d was not highlighted: %q", index, line)
		}
	}
}

func testFloat64(value float64) *float64 {
	return &value
}
