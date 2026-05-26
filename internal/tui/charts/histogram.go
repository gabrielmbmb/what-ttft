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

	chartHeight := opts.Height - 3
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
	content := strings.Join([]string{
		title + fmt.Sprintf("  bins=%d  n=%d  min=%.1f  max=%.1f", bins, len(values), minValue, maxValue),
		chart.View(),
		histogramXAxis(maxCount, opts.Width, histogramMaxLabelWidth(barData)),
		histogramLegendLine(),
	}, "\n")
	return fitChartText(content, opts.Width, opts.Height)
}

// RenderMultiHistogramChart renders multiple labeled distributions as a stacked histogram with shared bins.
func RenderMultiHistogramChart(series []NamedSeries, opts HistogramOptions, theme Theme) string {
	if opts.Width <= 0 || opts.Height <= 0 {
		return ""
	}

	series = cleanNamedSeries(series)
	title := chartTitle(histogramTitle(opts.Title), opts.Unit)
	if len(series) == 0 {
		return fitChartText(strings.Join([]string{title, seriesEmptyLabel(opts.EmptyLabel)}, "\n"), opts.Width, opts.Height)
	}

	values := flattenSeriesValues(series)
	bins := opts.Bins
	if bins <= 0 {
		bins = histogramDefaultBins(opts.Height)
	}
	if bins > len(values) {
		bins = len(values)
	}
	if opts.Width < minimumHistogramChartWidth || opts.Height < minimumHistogramChartHeight {
		return renderCompactMultiHistogram(series, opts, title)
	}

	chartHeight := opts.Height - 3
	if chartHeight < 3 {
		return renderCompactMultiHistogram(series, opts, title)
	}
	if bins > chartHeight {
		bins = chartHeight
	}

	barData, maxCount := multiHistogramBarData(series, bins, theme)
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

	content := strings.Join([]string{
		title + fmt.Sprintf("  bins=%d  n=%d  series=%d", bins, len(values), len(series)),
		chart.View(),
		histogramXAxis(maxCount, opts.Width, histogramMaxLabelWidth(barData)),
		multiHistogramLegendLine(series, opts.Width, theme),
	}, "\n")
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

func renderCompactMultiHistogram(series []NamedSeries, opts HistogramOptions, title string) string {
	lines := []string{title}
	for _, item := range series {
		minValue, maxValue := minMax(item.Values)
		lines = append(lines, fmt.Sprintf("%-14s n=%d min=%.1f max=%.1f", truncateChartLabel(item.Label, 14), len(item.Values), minValue, maxValue))
	}
	lines = append(lines, fmt.Sprintf("series=%d", len(series)))
	return fitChartText(strings.Join(lines, "\n"), opts.Width, opts.Height)
}

func multiHistogramBarData(series []NamedSeries, bins int, theme Theme) ([]barchart.BarData, int) {
	values := flattenSeriesValues(series)
	minValue, maxValue := minMax(values)
	seriesCounts := make([][]int, 0, len(series))
	for _, item := range series {
		seriesCounts = append(seriesCounts, histogramCounts(item.Values, bins, minValue, maxValue))
	}

	maxCount := 0
	data := make([]barchart.BarData, 0, bins)
	for binIndex := range bins {
		low, high := histogramRange(minValue, maxValue, bins, binIndex)
		barValues := make([]barchart.BarValue, 0, len(series))
		total := 0
		for seriesIndex, counts := range seriesCounts {
			count := counts[binIndex]
			if count == 0 {
				continue
			}
			total += count
			styleIndex := resolvedSeriesStyleIndex(series[seriesIndex], seriesIndex)
			barValues = append(barValues, barchart.BarValue{
				Name:  truncateChartLabel(series[seriesIndex].Label, 18),
				Value: float64(count),
				Style: theme.seriesStyle(styleIndex),
			})
		}
		if total > maxCount {
			maxCount = total
		}
		data = append(data, barchart.BarData{Label: fmt.Sprintf("%.0f-%.0f", low, high), Values: barValues})
	}
	if maxCount < 1 {
		maxCount = 1
	}
	return data, maxCount
}

func multiHistogramLegendLine(series []NamedSeries, width int, theme Theme) string {
	parts := []string{"x=request count"}
	labelWidth := legendLabelWidth(width, len(series), 5)
	for index, item := range series {
		styleIndex := resolvedSeriesStyleIndex(item, index)
		marker := theme.seriesStyle(styleIndex).Render(string(seriesMarker(styleIndex)))
		parts = append(parts, fmt.Sprintf("%s %s n=%d", marker, truncateChartLabel(item.Label, labelWidth), len(item.Values)))
	}
	return "legend: " + strings.Join(parts, "  |  ")
}

func histogramLegendLine() string {
	return "legend: x=request count"
}

func histogramXAxis(maxCount int, width int, labelWidth int) string {
	if width <= 0 {
		return ""
	}
	if maxCount < 0 {
		maxCount = 0
	}

	prefixWidth := labelWidth + 1
	if prefixWidth < 0 {
		prefixWidth = 0
	}
	if prefixWidth >= width {
		return truncateChartLine(fmt.Sprintf("0-%d", maxCount), width)
	}

	prefix := strings.Repeat(" ", prefixWidth)
	graphWidth := width - prefixWidth
	leftLabel := "0"
	rightLabel := fmt.Sprintf("%d", maxCount)
	if graphWidth <= len(leftLabel)+len(rightLabel)+1 {
		return truncateChartLine(prefix+leftLabel+"-"+rightLabel, width)
	}

	if maxCount > 1 {
		midLabel := fmt.Sprintf("%d", maxCount/2)
		if graphWidth >= len(leftLabel)+len(midLabel)+len(rightLabel)+4 {
			remaining := graphWidth - len(leftLabel) - len(midLabel) - len(rightLabel)
			leftFill := remaining / 2
			rightFill := remaining - leftFill
			return prefix + leftLabel + strings.Repeat("─", leftFill) + midLabel + strings.Repeat("─", rightFill) + rightLabel
		}
	}

	fillWidth := graphWidth - len(leftLabel) - len(rightLabel)
	return prefix + leftLabel + strings.Repeat("─", fillWidth) + rightLabel
}

func histogramMaxLabelWidth(data []barchart.BarData) int {
	width := 0
	for _, item := range data {
		if len(item.Label) > width {
			width = len(item.Label)
		}
	}
	return width
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
