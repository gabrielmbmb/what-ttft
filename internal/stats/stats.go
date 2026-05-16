// Package stats computes deterministic summary statistics for benchmark metrics.
package stats

import (
	"math"
	"sort"
)

// Distribution summarizes a finite set of float64 values using nearest-rank percentiles.
type Distribution struct {
	// Count is the number of values included in this distribution; zero means all other fields are nil.
	Count int `json:"count"`

	// Min is the smallest observed value, or nil when Count is zero.
	Min *float64 `json:"min,omitempty"`

	// Mean is the arithmetic mean over all observed values, or nil when Count is zero.
	Mean *float64 `json:"mean,omitempty"`

	// P50 is the nearest-rank 50th percentile over observed values, or nil when Count is zero.
	P50 *float64 `json:"p50,omitempty"`

	// P90 is the nearest-rank 90th percentile over observed values, or nil when Count is zero.
	P90 *float64 `json:"p90,omitempty"`

	// P95 is the nearest-rank 95th percentile over observed values, or nil when Count is zero.
	P95 *float64 `json:"p95,omitempty"`

	// P99 is the nearest-rank 99th percentile over observed values, or nil when Count is zero.
	P99 *float64 `json:"p99,omitempty"`

	// Max is the largest observed value, or nil when Count is zero.
	Max *float64 `json:"max,omitempty"`

	// StdDev is the population standard deviation over observed values, or nil when Count is zero.
	StdDev *float64 `json:"stddev,omitempty"`
}

// Summarize calculates a Distribution from values using nearest-rank percentiles.
func Summarize(values []float64) Distribution {
	if len(values) == 0 {
		return Distribution{}
	}

	sorted := append([]float64(nil), values...)
	sort.Float64s(sorted)

	count := len(sorted)
	minValue := sorted[0]
	maxValue := sorted[count-1]
	meanValue := mean(sorted)
	stddevValue := populationStdDev(sorted, meanValue)
	p50Value := nearestRank(sorted, 50)
	p90Value := nearestRank(sorted, 90)
	p95Value := nearestRank(sorted, 95)
	p99Value := nearestRank(sorted, 99)

	return Distribution{
		Count:  count,
		Min:    &minValue,
		Mean:   &meanValue,
		P50:    &p50Value,
		P90:    &p90Value,
		P95:    &p95Value,
		P99:    &p99Value,
		Max:    &maxValue,
		StdDev: &stddevValue,
	}
}

func mean(values []float64) float64 {
	var total float64
	for _, value := range values {
		total += value
	}

	return total / float64(len(values))
}

func populationStdDev(values []float64, meanValue float64) float64 {
	var sumSquares float64
	for _, value := range values {
		delta := value - meanValue
		sumSquares += delta * delta
	}

	return math.Sqrt(sumSquares / float64(len(values)))
}

func nearestRank(sortedValues []float64, percentile float64) float64 {
	if len(sortedValues) == 0 {
		return math.NaN()
	}

	rank := int(math.Ceil(percentile / 100 * float64(len(sortedValues))))
	if rank < 1 {
		rank = 1
	}
	if rank > len(sortedValues) {
		rank = len(sortedValues)
	}

	return sortedValues[rank-1]
}
