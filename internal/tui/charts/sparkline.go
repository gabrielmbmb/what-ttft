package charts

import (
	"math"
	"strings"
)

var sparklineRunes = []rune("▁▂▃▄▅▆▇█")

// Sparkline renders finite values as a compact left-to-right trend line using at most width cells.
func Sparkline(values []float64, width int) string {
	values = finiteValues(values)
	if width <= 0 || len(values) == 0 {
		return ""
	}
	if len(values) > width {
		values = downsample(values, width)
	}

	minValue, maxValue := minMax(values)
	if minValue == maxValue {
		return strings.Repeat(string(sparklineRunes[0]), len(values))
	}

	var builder strings.Builder
	for _, value := range values {
		normalized := (value - minValue) / (maxValue - minValue)
		index := int(math.Round(normalized * float64(len(sparklineRunes)-1)))
		if index < 0 {
			index = 0
		}
		if index >= len(sparklineRunes) {
			index = len(sparklineRunes) - 1
		}
		builder.WriteRune(sparklineRunes[index])
	}

	return builder.String()
}

func downsample(values []float64, width int) []float64 {
	if width <= 0 || len(values) <= width {
		return values
	}

	sampled := make([]float64, width)
	for index := range width {
		start := int(math.Floor(float64(index) * float64(len(values)) / float64(width)))
		end := int(math.Floor(float64(index+1) * float64(len(values)) / float64(width)))
		if end <= start {
			end = start + 1
		}
		if end > len(values) {
			end = len(values)
		}
		var sum float64
		for _, value := range values[start:end] {
			sum += value
		}
		sampled[index] = sum / float64(end-start)
	}

	return sampled
}

func finiteValues(values []float64) []float64 {
	finite := make([]float64, 0, len(values))
	for _, value := range values {
		if math.IsNaN(value) || math.IsInf(value, 0) {
			continue
		}
		finite = append(finite, value)
	}
	return finite
}

func minMax(values []float64) (float64, float64) {
	minValue := values[0]
	maxValue := values[0]
	for _, value := range values[1:] {
		if value < minValue {
			minValue = value
		}
		if value > maxValue {
			maxValue = value
		}
	}
	return minValue, maxValue
}
