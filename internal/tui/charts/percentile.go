package charts

import (
	"fmt"
	"math"
	"strings"

	"github.com/NimbleMarkets/ntcharts/v2/barchart"
	"github.com/gabrielmbmb/what-ttft/pkg/whatttft"
)

// PercentileGroup contains one group's latency percentiles for PercentileBars.
type PercentileGroup struct {
	// Label is the non-secret display label for the group; empty values are rendered as "group".
	Label string

	// P50 is the p50 latency in milliseconds, or nil when unavailable.
	P50 *float64

	// P90 is the p90 latency in milliseconds, or nil when unavailable.
	P90 *float64

	// P95 is the p95 latency in milliseconds, or nil when unavailable.
	P95 *float64

	// P99 is the p99 latency in milliseconds, or nil when unavailable.
	P99 *float64
}

// PercentileOptions configures RenderPercentileChart output.
type PercentileOptions struct {
	// Width is the target rendered width in terminal cells; values less than one render an empty string.
	Width int

	// Height is the target rendered height in terminal rows; values less than one render an empty string.
	Height int

	// Title is the non-secret chart title; empty defaults to "percentiles".
	Title string

	// Unit is the value unit label, such as "ms"; empty omits the unit annotation.
	Unit string

	// EmptyLabel is the no-data explanation; empty defaults to "no percentile groups available".
	EmptyLabel string
}

// PercentileGroupsFromSummary builds TTFT percentile groups from summary groups in their existing order.
func PercentileGroupsFromSummary(groups []whatttft.SummaryGroup) []PercentileGroup {
	percentiles := make([]PercentileGroup, 0, len(groups))
	for _, group := range groups {
		percentiles = append(percentiles, PercentileGroup{
			Label: groupLabel(group),
			P50:   group.Metrics.TTFTDeltaMS.P50,
			P90:   group.Metrics.TTFTDeltaMS.P90,
			P95:   group.Metrics.TTFTDeltaMS.P95,
			P99:   group.Metrics.TTFTDeltaMS.P99,
		})
	}
	return percentiles
}

// RenderPercentileChart renders p99 percentile comparison bars with p50/p90/p95/p99 values retained in labels.
func RenderPercentileChart(groups []PercentileGroup, opts PercentileOptions, theme Theme) string {
	if opts.Width <= 0 || opts.Height <= 0 {
		return ""
	}
	title := chartTitle(percentileTitle(opts.Title), opts.Unit)
	if len(groups) == 0 {
		empty := opts.EmptyLabel
		if strings.TrimSpace(empty) == "" {
			empty = "no percentile groups available"
		}
		return fitChartText(strings.Join([]string{title, empty}, "\n"), opts.Width, opts.Height)
	}
	if opts.Width < 48 || opts.Height < 6 {
		return fitChartText(PercentileBars(groups, opts.Width), opts.Width, opts.Height)
	}

	barData, maxValue := percentileBarData(groups, theme)
	if len(barData) == 0 || maxValue <= 0 {
		return fitChartText(PercentileBars(groups, opts.Width), opts.Width, opts.Height)
	}
	chartHeight := opts.Height - 2
	if chartHeight < 3 {
		return fitChartText(PercentileBars(groups, opts.Width), opts.Width, opts.Height)
	}
	chart := barchart.New(
		opts.Width,
		chartHeight,
		barchart.WithHorizontalBars(),
		barchart.WithStyles(theme.Axis, theme.Label),
		barchart.WithMaxValue(maxValue),
		barchart.WithNoAutoMaxValue(),
		barchart.WithDataSet(barData),
	)
	chart.Draw()
	return fitChartText(strings.Join([]string{title + "  bar=p99", chart.View()}, "\n"), opts.Width, opts.Height)
}

// PercentileBars renders p50/p90/p95/p99 millisecond percentiles with scaled p99 bars.
func PercentileBars(groups []PercentileGroup, width int) string {
	if len(groups) == 0 {
		return "percentiles ms\n(no groups)"
	}

	maxValue := 0.0
	for _, group := range groups {
		for _, value := range []*float64{group.P50, group.P90, group.P95, group.P99} {
			if value != nil && finiteFloat(*value) && *value > maxValue {
				maxValue = *value
			}
		}
	}
	barWidth := width - 54
	if barWidth < 1 {
		barWidth = 1
	}

	var builder strings.Builder
	builder.WriteString("percentiles ms\n")
	builder.WriteString("target                 p50     p90     p95     p99     bar\n")
	for index, group := range groups {
		label := truncate(group.Label, 20)
		bar := "-"
		if group.P99 != nil && finiteFloat(*group.P99) && maxValue > 0 {
			bar = strings.Repeat("█", scaledFloatWidth(*group.P99, maxValue, barWidth))
		}
		fmt.Fprintf(&builder, "%-20s %7s %7s %7s %7s %s", label, formatOptional(group.P50), formatOptional(group.P90), formatOptional(group.P95), formatOptional(group.P99), bar)
		if index != len(groups)-1 {
			builder.WriteByte('\n')
		}
	}

	return builder.String()
}

func percentileTitle(title string) string {
	if strings.TrimSpace(title) == "" {
		return "percentiles"
	}
	return title
}

func percentileBarData(groups []PercentileGroup, theme Theme) ([]barchart.BarData, float64) {
	data := make([]barchart.BarData, 0, len(groups))
	maxValue := 0.0
	for _, group := range groups {
		if group.P99 == nil || !finiteFloat(*group.P99) {
			continue
		}
		if *group.P99 > maxValue {
			maxValue = *group.P99
		}
		data = append(data, barchart.BarData{
			Label: truncate(group.Label, 18),
			Values: []barchart.BarValue{{
				Name:  fmt.Sprintf("p50=%s p90=%s p95=%s p99=%s", formatOptional(group.P50), formatOptional(group.P90), formatOptional(group.P95), formatOptional(group.P99)),
				Value: *group.P99,
				Style: theme.Series,
			}},
		})
	}
	return data, maxValue
}

func scaledFloatWidth(value float64, maxValue float64, width int) int {
	if value <= 0 || maxValue <= 0 || width <= 0 {
		return 0
	}
	result := int(value / maxValue * float64(width))
	if result < 1 {
		return 1
	}
	if result > width {
		return width
	}
	return result
}

func formatOptional(value *float64) string {
	if value == nil || !finiteFloat(*value) {
		return "-"
	}
	return fmt.Sprintf("%.1f", *value)
}

func finiteFloat(value float64) bool {
	return !math.IsNaN(value) && !math.IsInf(value, 0)
}

func truncate(value string, width int) string {
	if strings.TrimSpace(value) == "" {
		value = "group"
	}
	if width <= 0 || len(value) <= width {
		return value
	}
	if width == 1 {
		return "…"
	}
	return value[:width-1] + "…"
}

func groupLabel(group whatttft.SummaryGroup) string {
	if group.TargetID != "" {
		return group.TargetID
	}
	if group.TargetName != "" {
		return group.TargetName
	}
	if group.Model != "" {
		return group.Model
	}
	return group.Provider
}
