package charts

import (
	"fmt"
	"math"
	"strings"

	"github.com/NimbleMarkets/ntcharts/v2/barchart"
)

const (
	minimumHistogramChartWidth  = 30
	minimumHistogramChartHeight = 6
)

// HistogramOptions configures RenderHistogramChart output.
type HistogramOptions struct {
	// Width is the target rendered width in terminal cells; values less than one render an empty string.
	Width int

	// Height is the target rendered height in terminal rows; values less than one render an empty string.
	Height int

	// Bins is the requested histogram bin count; values less than one default to a small chart-dependent count.
	Bins int

	// Title is the non-secret chart title; empty defaults to "histogram".
	Title string

	// Unit is the value unit label, such as "ms"; empty omits the unit annotation.
	Unit string

	// EmptyLabel is the no-data explanation; empty defaults to "waiting for first successful measured request".
	EmptyLabel string
}

// RenderHistogramChart renders a distribution using ntcharts bar-chart primitives when dimensions allow.
func RenderHistogramChart(values []float64, opts HistogramOptions, theme Theme) string {
	if opts.Width <= 0 || opts.Height <= 0 {
		return ""
	}

	values = finiteValues(values)
	title := chartTitle(histogramTitle(opts.Title), opts.Unit)
	if len(values) == 0 {
		return fitChartText(strings.Join([]string{title, seriesEmptyLabel(opts.EmptyLabel)}, "\n"), opts.Width, opts.Height)
	}
	bins := opts.Bins
	if bins <= 0 {
		bins = histogramDefaultBins(opts.Height)
	}
	if bins > len(values) {
		bins = len(values)
	}
	if opts.Width < minimumHistogramChartWidth || opts.Height < minimumHistogramChartHeight {
		return fitChartText(Histogram(values, bins, opts.Width), opts.Width, opts.Height)
	}

	chartHeight := opts.Height - 2
	if chartHeight < 3 {
		return fitChartText(Histogram(values, bins, opts.Width), opts.Width, opts.Height)
	}
	if bins > chartHeight {
		bins = chartHeight
	}

	barData, maxCount := histogramBarData(values, bins, theme)
	chart := barchart.New(
		opts.Width,
		chartHeight,
		barchart.WithHorizontalBars(),
		barchart.WithBarGap(0),
		barchart.WithStyles(theme.Axis, theme.Label),
		barchart.WithMaxValue(float64(maxCount)),
		barchart.WithNoAutoMaxValue(),
		barchart.WithDataSet(barData),
	)
	// Recompute the horizontal label origin after data is loaded; ntcharts initializes it before WithDataSet runs.
	chart.SetHorizontal(true)
	chart.Draw()

	minValue, maxValue := minMax(values)
	content := strings.Join([]string{title + fmt.Sprintf("  bins=%d  n=%d  min=%.1f  max=%.1f", bins, len(values), minValue, maxValue), chart.View()}, "\n")
	return fitChartText(content, opts.Width, opts.Height)
}

// Histogram renders a deterministic histogram for finite millisecond values.
func Histogram(values []float64, bins int, width int) string {
	values = finiteValues(values)
	if len(values) == 0 {
		return "histogram ms\n(no values)"
	}
	if bins <= 0 {
		bins = 1
	}
	if bins > len(values) {
		bins = len(values)
	}

	minValue, maxValue := minMax(values)
	counts := histogramCounts(values, bins, minValue, maxValue)

	maxCount := 0
	for _, count := range counts {
		if count > maxCount {
			maxCount = count
		}
	}
	barWidth := width - 26
	if barWidth < 1 {
		barWidth = 1
	}

	var builder strings.Builder
	builder.WriteString("histogram ms\n")
	for index, count := range counts {
		low, high := histogramRange(minValue, maxValue, bins, index)
		filled := scaledWidth(count, maxCount, barWidth)
		fmt.Fprintf(&builder, "%7.1f-%7.1f | %-*s %d", low, high, barWidth, strings.Repeat("#", filled), count)
		if index != len(counts)-1 {
			builder.WriteByte('\n')
		}
	}

	return builder.String()
}

func histogramTitle(title string) string {
	if strings.TrimSpace(title) == "" {
		return "histogram"
	}
	return title
}

func histogramDefaultBins(height int) int {
	if height < 8 {
		return 3
	}
	if height < 14 {
		return 5
	}
	return 8
}

func histogramBarData(values []float64, bins int, theme Theme) ([]barchart.BarData, int) {
	minValue, maxValue := minMax(values)
	counts := histogramCounts(values, bins, minValue, maxValue)
	maxCount := 0
	data := make([]barchart.BarData, 0, len(counts))
	for index, count := range counts {
		if count > maxCount {
			maxCount = count
		}
		low, high := histogramRange(minValue, maxValue, bins, index)
		data = append(data, barchart.BarData{
			Label: fmt.Sprintf("%.0f-%.0f", low, high),
			Values: []barchart.BarValue{{
				Name:  "count",
				Value: float64(count),
				Style: theme.Series,
			}},
		})
	}
	if maxCount < 1 {
		maxCount = 1
	}
	return data, maxCount
}

func histogramCounts(values []float64, bins int, minValue float64, maxValue float64) []int {
	counts := make([]int, bins)
	if minValue == maxValue {
		counts[0] = len(values)
		return counts
	}
	span := maxValue - minValue
	for _, value := range values {
		index := int(math.Floor((value - minValue) / span * float64(bins)))
		if index >= bins {
			index = bins - 1
		}
		if index < 0 {
			index = 0
		}
		counts[index]++
	}
	return counts
}

func histogramRange(minValue float64, maxValue float64, bins int, index int) (float64, float64) {
	if minValue == maxValue {
		return minValue, maxValue
	}
	span := (maxValue - minValue) / float64(bins)
	low := minValue + float64(index)*span
	return low, low + span
}

func scaledWidth(value int, maxValue int, width int) int {
	if value <= 0 || maxValue <= 0 || width <= 0 {
		return 0
	}
	if value == maxValue {
		return width
	}
	result := int(math.Round(float64(value) / float64(maxValue) * float64(width)))
	if result < 1 {
		return 1
	}
	return result
}
