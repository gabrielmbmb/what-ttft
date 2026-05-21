package charts

import (
	"fmt"
	"math"
	"strings"
)

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
	counts := make([]int, bins)
	if minValue == maxValue {
		counts[0] = len(values)
	} else {
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
	}

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
