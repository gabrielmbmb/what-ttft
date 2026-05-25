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

func renderCompactSeriesChart(values []float64, opts SeriesChartOptions, title string) string {
	lineWidth := opts.Width
	if lineWidth < 1 {
		lineWidth = 1
	}
	content := strings.Join([]string{title, Sparkline(values, lineWidth), seriesStatsLine(values, opts.Unit)}, "\n")
	return fitChartText(content, opts.Width, opts.Height)
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
