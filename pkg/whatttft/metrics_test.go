package whatttft

import (
	"math"
	"testing"
	"time"
)

// TestCalculateDerivedMetricsCompleteTimeline verifies every v0.1 duration metric is computed from event pairs.
func TestCalculateDerivedMetricsCompleteTimeline(t *testing.T) {
	completionTokens := 10
	timeline := Timeline{EventsNS: map[EventName]int64{
		EventRequestStart:      0,
		EventDNSStart:          ns(1 * time.Millisecond),
		EventDNSDone:           ns(2 * time.Millisecond),
		EventConnectStart:      ns(2 * time.Millisecond),
		EventConnectDone:       ns(5 * time.Millisecond),
		EventTLSStart:          ns(5 * time.Millisecond),
		EventTLSDone:           ns(8 * time.Millisecond),
		EventGotConn:           ns(9 * time.Millisecond),
		EventWroteRequest:      ns(12 * time.Millisecond),
		EventFirstResponseByte: ns(20 * time.Millisecond),
		EventHeadersReceived:   ns(21 * time.Millisecond),
		EventFirstSSEEvent:     ns(22 * time.Millisecond),
		EventFirstOutputDelta:  ns(25 * time.Millisecond),
		EventLastOutputDelta:   ns(45 * time.Millisecond),
		EventBodyEOF:           ns(50 * time.Millisecond),
	}}

	metrics := CalculateDerivedMetrics(timeline, &completionTokens)

	assertMetric(t, "http_ttfb_ms", metrics.HTTPTTFBMS, 20)
	assertMetric(t, "headers_latency_ms", metrics.HeadersLatencyMS, 21)
	assertMetric(t, "first_event_ms", metrics.FirstEventMS, 22)
	assertMetric(t, "ttft_delta_ms", metrics.TTFTDeltaMS, 25)
	assertMetric(t, "e2e_delta_ms", metrics.E2EDeltaMS, 45)
	assertMetric(t, "stream_total_ms", metrics.StreamTotalMS, 50)
	assertMetric(t, "generation_delta_ms", metrics.GenerationDeltaMS, 20)
	assertMetric(t, "server_wait_to_first_byte_ms", metrics.ServerWaitToFirstByteMS, 8)
	assertMetric(t, "stream_protocol_to_first_output_ms", metrics.StreamProtocolToFirstOutputMS, 5)
	assertMetric(t, "dns_ms", metrics.DNSMS, 1)
	assertMetric(t, "tcp_connect_ms", metrics.TCPConnectMS, 3)
	assertMetric(t, "tls_ms", metrics.TLSMS, 3)
	assertMetric(t, "request_write_ms", metrics.RequestWriteMS, 3)
	assertMetric(t, "e2e_output_tps", metrics.E2EOutputTPS, 222.22222222222223)
	assertMetric(t, "generation_delta_output_tps", metrics.GenerationDeltaOutputTPS, 450)
}

// TestCalculateDerivedMetricsMissingEvents verifies metrics are nil when either endpoint event is absent.
func TestCalculateDerivedMetricsMissingEvents(t *testing.T) {
	completionTokens := 4
	metrics := CalculateDerivedMetrics(Timeline{EventsNS: map[EventName]int64{
		EventFirstResponseByte: ns(10 * time.Millisecond),
		EventLastOutputDelta:   ns(20 * time.Millisecond),
	}}, &completionTokens)

	assertNilMetric(t, "http_ttfb_ms", metrics.HTTPTTFBMS)
	assertNilMetric(t, "e2e_output_tps", metrics.E2EOutputTPS)
	assertNilMetric(t, "generation_delta_output_tps", metrics.GenerationDeltaOutputTPS)

	metrics = CalculateDerivedMetrics(Timeline{EventsNS: map[EventName]int64{
		EventRequestStart:     0,
		EventFirstOutputDelta: ns(15 * time.Millisecond),
	}}, nil)

	assertMetric(t, "ttft_delta_ms", metrics.TTFTDeltaMS, 15)
	assertNilMetric(t, "e2e_delta_ms", metrics.E2EDeltaMS)
	assertNilMetric(t, "generation_delta_ms", metrics.GenerationDeltaMS)
	assertNilMetric(t, "e2e_output_tps", metrics.E2EOutputTPS)
	assertNilMetric(t, "generation_delta_output_tps", metrics.GenerationDeltaOutputTPS)
}

// TestCalculateDerivedMetricsZeroDurations verifies observed zero-duration metrics are not treated as missing.
func TestCalculateDerivedMetricsZeroDurations(t *testing.T) {
	metrics := CalculateDerivedMetrics(Timeline{EventsNS: map[EventName]int64{
		EventRequestStart:      0,
		EventFirstResponseByte: 0,
		EventHeadersReceived:   0,
		EventFirstSSEEvent:     0,
		EventFirstOutputDelta:  0,
		EventLastOutputDelta:   0,
		EventBodyEOF:           0,
		EventDNSStart:          0,
		EventDNSDone:           0,
		EventConnectStart:      0,
		EventConnectDone:       0,
		EventTLSStart:          0,
		EventTLSDone:           0,
		EventGotConn:           0,
		EventWroteRequest:      0,
	}}, nil)

	assertMetric(t, "http_ttfb_ms", metrics.HTTPTTFBMS, 0)
	assertMetric(t, "headers_latency_ms", metrics.HeadersLatencyMS, 0)
	assertMetric(t, "first_event_ms", metrics.FirstEventMS, 0)
	assertMetric(t, "ttft_delta_ms", metrics.TTFTDeltaMS, 0)
	assertMetric(t, "e2e_delta_ms", metrics.E2EDeltaMS, 0)
	assertMetric(t, "stream_total_ms", metrics.StreamTotalMS, 0)
	assertMetric(t, "generation_delta_ms", metrics.GenerationDeltaMS, 0)
	assertMetric(t, "server_wait_to_first_byte_ms", metrics.ServerWaitToFirstByteMS, 0)
	assertMetric(t, "stream_protocol_to_first_output_ms", metrics.StreamProtocolToFirstOutputMS, 0)
	assertMetric(t, "dns_ms", metrics.DNSMS, 0)
	assertMetric(t, "tcp_connect_ms", metrics.TCPConnectMS, 0)
	assertMetric(t, "tls_ms", metrics.TLSMS, 0)
	assertMetric(t, "request_write_ms", metrics.RequestWriteMS, 0)
}

// TestCalculateDerivedMetricsZeroCompletionTokens verifies known zero output tokens produce zero E2E throughput when duration is positive and no post-first-delta TPS.
func TestCalculateDerivedMetricsZeroCompletionTokens(t *testing.T) {
	completionTokens := 0
	metrics := CalculateDerivedMetrics(Timeline{EventsNS: map[EventName]int64{
		EventRequestStart:     0,
		EventFirstOutputDelta: ns(5 * time.Millisecond),
		EventLastOutputDelta:  ns(10 * time.Millisecond),
	}}, &completionTokens)

	assertMetric(t, "e2e_output_tps", metrics.E2EOutputTPS, 0)
	assertNilMetric(t, "generation_delta_output_tps", metrics.GenerationDeltaOutputTPS)
}

// TestCalculateDerivedMetricsGenerationDeltaOutputTPSNilCases verifies post-first-delta TPS is omitted when it would be misleading.
func TestCalculateDerivedMetricsGenerationDeltaOutputTPSNilCases(t *testing.T) {
	tests := map[string]struct {
		tokens *int
		events map[EventName]int64
	}{
		"missing tokens": {
			events: map[EventName]int64{
				EventFirstOutputDelta: 0,
				EventLastOutputDelta:  ns(10 * time.Millisecond),
			},
		},
		"one token": {
			tokens: intPointerForMetrics(1),
			events: map[EventName]int64{
				EventFirstOutputDelta: 0,
				EventLastOutputDelta:  ns(10 * time.Millisecond),
			},
		},
		"missing first output": {
			tokens: intPointerForMetrics(2),
			events: map[EventName]int64{
				EventLastOutputDelta: ns(10 * time.Millisecond),
			},
		},
		"missing last output": {
			tokens: intPointerForMetrics(2),
			events: map[EventName]int64{
				EventFirstOutputDelta: 0,
			},
		},
		"zero generation duration": {
			tokens: intPointerForMetrics(2),
			events: map[EventName]int64{
				EventFirstOutputDelta: 0,
				EventLastOutputDelta:  0,
			},
		},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			metrics := CalculateDerivedMetrics(Timeline{EventsNS: tc.events}, tc.tokens)
			assertNilMetric(t, "generation_delta_output_tps", metrics.GenerationDeltaOutputTPS)
		})
	}
}

func intPointerForMetrics(value int) *int {
	return &value
}

func assertMetric(t *testing.T, name string, got *float64, want float64) {
	t.Helper()

	if got == nil {
		t.Fatalf("%s = nil, want %.12g", name, want)
	}
	if math.Abs(*got-want) > 1e-9 {
		t.Fatalf("%s = %.12g, want %.12g", name, *got, want)
	}
}

func assertNilMetric(t *testing.T, name string, got *float64) {
	t.Helper()

	if got != nil {
		t.Fatalf("%s = %.12g, want nil", name, *got)
	}
}

func ns(duration time.Duration) int64 {
	return duration.Nanoseconds()
}
