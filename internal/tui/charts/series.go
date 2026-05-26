package charts

import (
	"fmt"
	"math"
	"strings"

	"github.com/NimbleMarkets/ntcharts/v2/canvas"
	"github.com/NimbleMarkets/ntcharts/v2/canvas/runes"
	"github.com/NimbleMarkets/ntcharts/v2/linechart"

	"charm.land/lipgloss/v2"
)

const (
	minimumSeriesChartWidth  = 24
	minimumSeriesChartHeight = 6
)

// SeriesChartOptions configures RenderSeriesChart output.
type SeriesChartOptions struct {
	// Width is the target rendered width in terminal cells; values less than one render an empty string.
	Width int

	// Height is the target rendered height in terminal rows; values less than one render an empty string.
	Height int

	// Title is the non-secret chart title; empty defaults to "series".
	Title string

	// Unit is the y-axis unit label, such as "ms"; empty omits the unit annotation.
	Unit string

	// EmptyLabel is the no-data explanation; empty defaults to a measured-request waiting message.
	EmptyLabel string
}

// NamedSeries contains one non-secret labeled metric series for comparison charts.
type NamedSeries struct {
	// Label is the non-secret display label for the series; empty labels are rendered as "series-N".
	Label string

	// Values contains observed metric values in request order; non-finite values are ignored and values are not mutated.
	Values []float64

	// StyleIndex is the stable model/target color and marker index; zero is valid and UseStyleIndex=false means the input order is used.
	StyleIndex int

	// UseStyleIndex reports whether StyleIndex was explicitly set by the caller to keep colors stable when other series have no data.
	UseStyleIndex bool
}

// RenderSeriesChart renders a request-order metric series using ntcharts linechart primitives.
func RenderSeriesChart(values []float64, opts SeriesChartOptions, theme Theme) string {
	if opts.Width <= 0 || opts.Height <= 0 {
		return ""
	}

	values = finiteValues(values)
	title := chartTitle(opts.Title, opts.Unit)
	if len(values) == 0 {
		return fitChartText(strings.Join([]string{title, seriesEmptyLabel(opts.EmptyLabel)}, "\n"), opts.Width, opts.Height)
	}
	if opts.Width < minimumSeriesChartWidth || opts.Height < minimumSeriesChartHeight {
		return renderCompactSeriesChart(values, opts, title)
	}

	chartHeight := opts.Height - 3
	if chartHeight < 3 {
		return renderCompactSeriesChart(values, opts, title)
	}

	minValue, maxValue := paddedRange(values)
	xMax := math.Max(float64(len(values)), 2)
	chart := linechart.New(
		opts.Width,
		chartHeight,
		1,
		xMax,
		minValue,
		maxValue,
		linechart.WithStyles(theme.Axis, theme.Label, theme.Series),
		linechart.WithXYSteps(chartXStep(opts.Width), chartYStep(chartHeight)),
		linechart.WithXLabelFormatter(func(_ int, value float64) string { return fmt.Sprintf("%.0f", value) }),
		linechart.WithYLabelFormatter(func(_ int, value float64) string { return fmt.Sprintf("%.0f", value) }),
	)
	chart.DrawXYAxisAndLabel()
	if len(values) == 1 {
		chart.DrawRuneWithStyle(canvas.Float64Point{X: 1, Y: values[0]}, '•', theme.Series)
	} else {
		for index := 0; index < len(values)-1; index++ {
			from := canvas.Float64Point{X: float64(index + 1), Y: values[index]}
			to := canvas.Float64Point{X: float64(index + 2), Y: values[index+1]}
			chart.DrawBrailleLineWithStyle(from, to, theme.Series)
			chart.DrawLineWithStyle(from, to, runes.ArcLineStyle, theme.SecondarySeries)
		}
	}

	footer := seriesStatsLine(values, opts.Unit)
	content := strings.Join([]string{title, chart.View(), footer}, "\n")
	return fitChartText(content, opts.Width, opts.Height)
}

// RenderMultiSeriesChart renders multiple request-order metric series on shared axes.
func RenderMultiSeriesChart(series []NamedSeries, opts SeriesChartOptions, theme Theme) string {
	if opts.Width <= 0 || opts.Height <= 0 {
		return ""
	}

	series = cleanNamedSeries(series)
	title := chartTitle(opts.Title, opts.Unit)
	if len(series) == 0 {
		return fitChartText(strings.Join([]string{title, seriesEmptyLabel(opts.EmptyLabel)}, "\n"), opts.Width, opts.Height)
	}
	if opts.Width < minimumSeriesChartWidth || opts.Height < minimumSeriesChartHeight {
		return renderCompactMultiSeriesChart(series, opts, title)
	}

	chartHeight := opts.Height - 4
	if chartHeight < 3 {
		return renderCompactMultiSeriesChart(series, opts, title)
	}

	values := flattenSeriesValues(series)
	minValue, maxValue := paddedRange(values)
	xMax := math.Max(float64(maxSeriesLength(series)), 2)
	chart := linechart.New(
		opts.Width,
		chartHeight,
		1,
		xMax,
		minValue,
		maxValue,
		linechart.WithStyles(theme.Axis, theme.Label, theme.Series),
		linechart.WithXYSteps(chartXStep(opts.Width), chartYStep(chartHeight)),
		linechart.WithXLabelFormatter(func(_ int, value float64) string { return fmt.Sprintf("%.0f", value) }),
		linechart.WithYLabelFormatter(func(_ int, value float64) string { return fmt.Sprintf("%.0f", value) }),
	)
	chart.DrawXYAxisAndLabel()
	for seriesIndex, item := range series {
		styleIndex := resolvedSeriesStyleIndex(item, seriesIndex)
		style := theme.seriesStyle(styleIndex)
		marker := seriesMarker(styleIndex)
		if len(item.Values) == 1 {
			chart.DrawRuneWithStyle(canvas.Float64Point{X: 1, Y: item.Values[0]}, marker, style)
			continue
		}
		for index := 0; index < len(item.Values)-1; index++ {
			from := canvas.Float64Point{X: float64(index + 1), Y: item.Values[index]}
			to := canvas.Float64Point{X: float64(index + 2), Y: item.Values[index+1]}
			if styleIndex%2 == 0 {
				chart.DrawBrailleLineWithStyle(from, to, style)
			} else {
				chart.DrawLineWithStyle(from, to, runes.ArcLineStyle, style)
			}
		}
		chart.DrawRuneWithStyle(canvas.Float64Point{X: float64(len(item.Values)), Y: item.Values[len(item.Values)-1]}, marker, style)
	}

	legend := multiSeriesLegendLine(series, opts.Unit, opts.Width, theme)
	footer := fmt.Sprintf("x=request order per target  y=%s  series=%d", unitOrValue(opts.Unit), len(series))
	content := strings.Join([]string{title, chart.View(), legend, footer}, "\n")
	return fitChartText(content, opts.Width, opts.Height)
}

func renderCompactSeriesChart(values []float64, opts SeriesChartOptions, title string) string {
	lineWidth := opts.Width
	if lineWidth < 1 {
		lineWidth = 1
	}
	content := strings.Join([]string{title, Sparkline(values, lineWidth), seriesStatsLine(values, opts.Unit)}, "\n")
	return fitChartText(content, opts.Width, opts.Height)
}

func renderCompactMultiSeriesChart(series []NamedSeries, opts SeriesChartOptions, title string) string {
	lines := []string{title}
	lineWidth := opts.Width - 28
	if lineWidth < 4 {
		lineWidth = opts.Width
	}
	unitSuffix := unitSuffix(opts.Unit)
	for _, item := range series {
		latest := item.Values[len(item.Values)-1]
		lines = append(lines, fmt.Sprintf("%-14s %s latest=%.1f%s", truncateChartLabel(item.Label, 14), Sparkline(item.Values, lineWidth), latest, unitSuffix))
	}
	lines = append(lines, fmt.Sprintf("x=request order per target  series=%d", len(series)))
	return fitChartText(strings.Join(lines, "\n"), opts.Width, opts.Height)
}

func cleanNamedSeries(series []NamedSeries) []NamedSeries {
	cleaned := make([]NamedSeries, 0, len(series))
	for index, item := range series {
		values := finiteValues(item.Values)
		if len(values) == 0 {
			continue
		}
		label := strings.TrimSpace(item.Label)
		if label == "" {
			label = fmt.Sprintf("series-%d", index+1)
		}
		styleIndex := index
		if item.UseStyleIndex {
			styleIndex = item.StyleIndex
		}
		cleaned = append(cleaned, NamedSeries{Label: label, Values: values, StyleIndex: styleIndex, UseStyleIndex: true})
	}
	return cleaned
}

func flattenSeriesValues(series []NamedSeries) []float64 {
	count := 0
	for _, item := range series {
		count += len(item.Values)
	}
	values := make([]float64, 0, count)
	for _, item := range series {
		values = append(values, item.Values...)
	}
	return values
}

func maxSeriesLength(series []NamedSeries) int {
	maxLength := 0
	for _, item := range series {
		if len(item.Values) > maxLength {
			maxLength = len(item.Values)
		}
	}
	return maxLength
}

func (theme Theme) seriesStyle(index int) lipgloss.Style {
	if index < 0 {
		index = 0
	}
	if len(theme.Palette) > 0 {
		if index < len(theme.Palette) {
			return theme.Palette[index]
		}
		return generatedSeriesStyle(index)
	}
	if index%3 == 1 {
		return theme.SecondarySeries
	}
	if index%3 == 2 {
		return theme.Muted
	}
	return theme.Series
}

func generatedSeriesStyle(index int) lipgloss.Style {
	// Use a deterministic walk through the 256-color cube after the fixed palette is exhausted.
	color := 16 + ((index * 37) % 216)
	return lipgloss.NewStyle().Foreground(lipgloss.Color(fmt.Sprintf("%d", color)))
}

func seriesMarker(index int) rune {
	markers := []rune{'●', '◆', '▲', '■', '✦', '✚', '✕', '○'}
	return markers[index%len(markers)]
}

func resolvedSeriesStyleIndex(item NamedSeries, fallback int) int {
	if item.UseStyleIndex {
		return item.StyleIndex
	}
	return fallback
}

func multiSeriesLegendLine(series []NamedSeries, unit string, width int, theme Theme) string {
	parts := make([]string, 0, len(series))
	unitSuffix := unitSuffix(unit)
	labelWidth := legendLabelWidth(width, len(series), 18+len(unitSuffix))
	for index, item := range series {
		styleIndex := resolvedSeriesStyleIndex(item, index)
		latest := item.Values[len(item.Values)-1]
		marker := theme.seriesStyle(styleIndex).Render(string(seriesMarker(styleIndex)))
		parts = append(parts, fmt.Sprintf("%s %s latest=%.1f%s", marker, truncateChartLabel(item.Label, labelWidth), latest, unitSuffix))
	}
	return "legend: " + strings.Join(parts, "  |  ")
}

func legendLabelWidth(width int, seriesCount int, valueWidth int) int {
	if seriesCount <= 0 {
		return 12
	}
	separatorWidth := 5 * (seriesCount - 1)
	available := width - len("legend: ") - separatorWidth - seriesCount*(2+valueWidth)
	labelWidth := available / seriesCount
	if labelWidth < 8 {
		return 8
	}
	if labelWidth > 32 {
		return 32
	}
	return labelWidth
}

func unitSuffix(unit string) string {
	if strings.TrimSpace(unit) == "" {
		return ""
	}
	return " " + unit
}

func truncateChartLabel(value string, width int) string {
	if strings.TrimSpace(value) == "" {
		value = "series"
	}
	if width <= 0 {
		return ""
	}
	for lipgloss.Width(value) > width {
		runes := []rune(value)
		if len(runes) <= 1 {
			return string(runes[:min(len(runes), width)])
		}
		value = string(runes[:len(runes)-1])
	}
	return value
}

func chartTitle(title string, unit string) string {
	if strings.TrimSpace(title) == "" {
		title = "series"
	}
	if strings.TrimSpace(unit) == "" {
		return title
	}
	return title + " (" + unit + ")"
}

func seriesEmptyLabel(label string) string {
	if strings.TrimSpace(label) == "" {
		return "waiting for first successful measured request"
	}
	return label
}

func paddedRange(values []float64) (float64, float64) {
	minValue, maxValue := minMax(values)
	if minValue == maxValue {
		padding := math.Max(math.Abs(minValue)*0.05, 1)
		return minValue - padding, maxValue + padding
	}
	padding := (maxValue - minValue) * 0.08
	return minValue - padding, maxValue + padding
}

func chartXStep(width int) int {
	step := width / 4
	if step < 1 {
		return 1
	}
	return step
}

func chartYStep(height int) int {
	step := height / 3
	if step < 1 {
		return 1
	}
	return step
}

func seriesStatsLine(values []float64, unit string) string {
	minValue, maxValue := minMax(values)
	latest := values[len(values)-1]
	unitSuffix := ""
	if strings.TrimSpace(unit) != "" {
		unitSuffix = " " + unit
	}
	return fmt.Sprintf("x=request order  y=%s  latest=%.1f%s  min=%.1f%s  max=%.1f%s", unitOrValue(unit), latest, unitSuffix, minValue, unitSuffix, maxValue, unitSuffix)
}

func unitOrValue(unit string) string {
	if strings.TrimSpace(unit) == "" {
		return "value"
	}
	return unit
}

func fitChartText(content string, width int, height int) string {
	if width <= 0 || height <= 0 {
		return ""
	}
	inputLines := strings.Split(strings.ReplaceAll(content, "\r\n", "\n"), "\n")
	lines := make([]string, height)
	for index := range height {
		line := ""
		if index < len(inputLines) {
			line = inputLines[index]
		}
		line = truncateChartLine(line, width)
		lines[index] = padChartLine(line, width)
	}
	return strings.Join(lines, "\n")
}

func truncateChartLine(line string, width int) string {
	for lipgloss.Width(line) > width {
		runes := []rune(line)
		if len(runes) == 0 {
			return ""
		}
		line = string(runes[:len(runes)-1])
	}
	return line
}

func padChartLine(line string, width int) string {
	visible := lipgloss.Width(line)
	if visible >= width {
		return line
	}
	return line + strings.Repeat(" ", width-visible)
}
