package charts

import (
	"fmt"
	"math"
	"strings"

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
