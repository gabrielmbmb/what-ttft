package whatttft

import "time"

const (
	minimumGenerationDeltaOutputTPSDeltas = 3
	minimumGenerationDeltaOutputTPSWindow = 50 * time.Millisecond
)

// CalculateDerivedMetrics computes standardized request metrics from monotonic-relative timeline events, omitting generation_delta_output_tps because the visible output delta count is unknown.
func CalculateDerivedMetrics(timeline Timeline, completionTokens *int) DerivedMetrics {
	return calculateDerivedMetrics(timeline, completionTokens, 0)
}

// CalculateDerivedMetricsWithOutputDeltaCount computes standardized request metrics and post-first-delta throughput when the visible output delta count is known.
func CalculateDerivedMetricsWithOutputDeltaCount(timeline Timeline, completionTokens *int, outputDeltaCount int) DerivedMetrics {
	return calculateDerivedMetrics(timeline, completionTokens, outputDeltaCount)
}

func calculateDerivedMetrics(timeline Timeline, completionTokens *int, outputDeltaCount int) DerivedMetrics {
	metrics := DerivedMetrics{
		HTTPTTFBMS:                    durationMS(timeline, EventRequestStart, EventFirstResponseByte),
		HeadersLatencyMS:              durationMS(timeline, EventRequestStart, EventHeadersReceived),
		FirstEventMS:                  durationMS(timeline, EventRequestStart, EventFirstSSEEvent),
		TTFTDeltaMS:                   durationMS(timeline, EventRequestStart, EventFirstOutputDelta),
		E2EDeltaMS:                    durationMS(timeline, EventRequestStart, EventLastOutputDelta),
		StreamTotalMS:                 durationMS(timeline, EventRequestStart, EventBodyEOF),
		GenerationDeltaMS:             durationMS(timeline, EventFirstOutputDelta, EventLastOutputDelta),
		ServerWaitToFirstByteMS:       durationMS(timeline, EventWroteRequest, EventFirstResponseByte),
		StreamProtocolToFirstOutputMS: durationMS(timeline, EventFirstResponseByte, EventFirstOutputDelta),
		DNSMS:                         durationMS(timeline, EventDNSStart, EventDNSDone),
		TCPConnectMS:                  durationMS(timeline, EventConnectStart, EventConnectDone),
		TLSMS:                         durationMS(timeline, EventTLSStart, EventTLSDone),
		RequestWriteMS:                durationMS(timeline, EventGotConn, EventWroteRequest),
	}
	metrics.E2EOutputTPS = e2eOutputTPS(timeline, completionTokens)
	metrics.GenerationDeltaOutputTPS = generationDeltaOutputTPS(timeline, completionTokens, outputDeltaCount)

	return metrics
}

func durationMS(timeline Timeline, start EventName, end EventName) *float64 {
	startNS, ok := eventNS(timeline, start)
	if !ok {
		return nil
	}
	endNS, ok := eventNS(timeline, end)
	if !ok {
		return nil
	}

	value := float64(endNS-startNS) / float64(time.Millisecond)
	return &value
}

func e2eOutputTPS(timeline Timeline, completionTokens *int) *float64 {
	if completionTokens == nil {
		return nil
	}

	startNS, ok := eventNS(timeline, EventRequestStart)
	if !ok {
		return nil
	}
	endNS, ok := eventNS(timeline, EventLastOutputDelta)
	if !ok {
		return nil
	}

	return tokensPerSecond(*completionTokens, endNS-startNS)
}

func generationDeltaOutputTPS(timeline Timeline, completionTokens *int, outputDeltaCount int) *float64 {
	if completionTokens == nil || *completionTokens <= 1 || outputDeltaCount < minimumGenerationDeltaOutputTPSDeltas {
		return nil
	}

	startNS, ok := eventNS(timeline, EventFirstOutputDelta)
	if !ok {
		return nil
	}
	endNS, ok := eventNS(timeline, EventLastOutputDelta)
	if !ok {
		return nil
	}

	if endNS-startNS < minimumGenerationDeltaOutputTPSWindow.Nanoseconds() {
		return nil
	}

	return tokensPerSecond(*completionTokens-1, endNS-startNS)
}

func tokensPerSecond(tokens int, durationNS int64) *float64 {
	if durationNS <= 0 {
		return nil
	}

	value := float64(tokens) / (float64(durationNS) / float64(time.Second))
	return &value
}

func eventNS(timeline Timeline, name EventName) (int64, bool) {
	if timeline.EventsNS == nil {
		return 0, false
	}

	value, ok := timeline.EventsNS[name]
	return value, ok
}
