package whatttft

import (
	"math"
	"testing"
	"time"
)

// TestSummarizeExcludesWarmupAndAggregatesMeasuredRequests verifies counts and metrics ignore warmup records by default.
func TestSummarizeExcludesWarmupAndAggregatesMeasuredRequests(t *testing.T) {
	providerProcessingMS := 3.0
	records := []RequestRecord{
		summaryRecord(summaryRecordConfig{warmup: true, cacheMode: CacheBust, ttftMS: 999, completionTokens: 100}),
		summaryRecord(summaryRecordConfig{cacheMode: CacheBust, ttftMS: 10, completionTokens: 10, cachedTokens: summaryIntPointer(0), providerProcessingMS: &providerProcessingMS, protocol: "HTTP/2.0", reused: true, bodyEOFMS: 100}),
		summaryErrorRecord(CacheBust, "http_status", httpStatusTooManyRequests),
	}

	summary := Summarize(records)

	if summary.TotalRequests != 3 {
		t.Fatalf("total requests = %d, want 3", summary.TotalRequests)
	}
	if summary.WarmupRequests != 1 {
		t.Fatalf("warmup requests = %d, want 1", summary.WarmupRequests)
	}
	if summary.MeasuredRequests != 2 {
		t.Fatalf("measured requests = %d, want 2", summary.MeasuredRequests)
	}
	if summary.SuccessfulRequests != 1 {
		t.Fatalf("successful requests = %d, want 1", summary.SuccessfulRequests)
	}
	if summary.ErrorRequests != 1 {
		t.Fatalf("error requests = %d, want 1", summary.ErrorRequests)
	}
	if summary.ErrorCategories["http_status"] != 1 {
		t.Fatalf("error categories = %#v, want http_status count 1", summary.ErrorCategories)
	}
	if summary.ErrorStatusCodes["429"] != 1 {
		t.Fatalf("error status codes = %#v, want 429 count 1", summary.ErrorStatusCodes)
	}

	if len(summary.Groups) != 1 {
		t.Fatalf("group count = %d, want 1", len(summary.Groups))
	}
	group := summary.Groups[0]
	if group.MeasuredRequests != 2 || group.SuccessfulRequests != 1 || group.ErrorRequests != 1 {
		t.Fatalf("group counts = measured %d successful %d errors %d", group.MeasuredRequests, group.SuccessfulRequests, group.ErrorRequests)
	}
	assertSummaryDistribution(t, "ttft_delta_ms", group.Metrics.TTFTDeltaMS, 1, 10)
	assertSummaryDistribution(t, "provider_processing_ms", group.Metrics.ProviderProcessingMS, 1, 3)
	assertSummaryDistribution(t, "server_wait_minus_provider_processing_ms", group.Metrics.ServerWaitMinusProviderProcessingMS, 1, 2)
	assertSummaryDistribution(t, "generation_delta_output_tps", group.Metrics.GenerationDeltaOutputTPS, 1, 112.5)
	if group.Metrics.TTFTDeltaMS.P50 != nil && *group.Metrics.TTFTDeltaMS.P50 == 999 {
		t.Fatal("warmup TTFT leaked into measured distribution")
	}
	if group.TotalCompletionTokens != 10 {
		t.Fatalf("total completion tokens = %d, want 10", group.TotalCompletionTokens)
	}
	if group.CompletionTokenRecords != 1 {
		t.Fatalf("completion token records = %d, want 1", group.CompletionTokenRecords)
	}
	assertFloatPointer(t, "system_tps", group.SystemTPS, 100)
	assertFloatPointer(t, "rps", group.RPS, 10)
	if group.Cache.CacheMode != CacheBust {
		t.Fatalf("cache mode = %q, want %q", group.Cache.CacheMode, CacheBust)
	}
	if group.Cache.CacheHitCount != 0 {
		t.Fatalf("cache hit count = %d, want 0", group.Cache.CacheHitCount)
	}
	assertSummaryDistribution(t, "cached_prompt_tokens", group.Cache.CachedPromptTokens, 1, 0)
	if group.Connection.ReusedConnectionCount != 1 {
		t.Fatalf("reused connection count = %d, want 1", group.Connection.ReusedConnectionCount)
	}
	if group.Connection.ProtocolCounts["HTTP/2.0"] != 1 {
		t.Fatalf("protocol counts = %#v, want HTTP/2.0 count 1", group.Connection.ProtocolCounts)
	}
}

// TestSummarizeSeparatesTargetIDs verifies different configured target IDs are never combined.
func TestSummarizeSeparatesTargetIDs(t *testing.T) {
	summary := Summarize([]RequestRecord{
		summaryRecord(summaryRecordConfig{cacheMode: CacheBust, targetID: "target-a", targetName: "Target A", ttftMS: 10, completionTokens: 1, bodyEOFMS: 50}),
		summaryRecord(summaryRecordConfig{cacheMode: CacheBust, targetID: "target-b", targetName: "Target B", ttftMS: 20, completionTokens: 1, bodyEOFMS: 50}),
	})

	if len(summary.Groups) != 2 {
		t.Fatalf("group count = %d, want 2", len(summary.Groups))
	}
	if summary.Groups[0].TargetID != "target-a" || summary.Groups[0].TargetName != "Target A" {
		t.Fatalf("first group target = id %q name %q, want target-a/Target A", summary.Groups[0].TargetID, summary.Groups[0].TargetName)
	}
	if summary.Groups[1].TargetID != "target-b" || summary.Groups[1].TargetName != "Target B" {
		t.Fatalf("second group target = id %q name %q, want target-b/Target B", summary.Groups[1].TargetID, summary.Groups[1].TargetName)
	}
}

// TestSummarizeSeparatesServiceTiers verifies different requested provider service tiers are never combined.
func TestSummarizeSeparatesServiceTiers(t *testing.T) {
	summary := Summarize([]RequestRecord{
		summaryRecord(summaryRecordConfig{cacheMode: CacheBust, serviceTier: "default", observedServiceTier: "default", ttftMS: 10, completionTokens: 1, bodyEOFMS: 50}),
		summaryRecord(summaryRecordConfig{cacheMode: CacheBust, serviceTier: "priority", observedServiceTier: "priority", ttftMS: 20, completionTokens: 1, bodyEOFMS: 50}),
	})

	if len(summary.Groups) != 2 {
		t.Fatalf("group count = %d, want 2", len(summary.Groups))
	}
	if summary.Groups[0].RequestedServiceTier != "default" {
		t.Fatalf("first group service tier = %q, want default", summary.Groups[0].RequestedServiceTier)
	}
	if summary.Groups[1].RequestedServiceTier != "priority" {
		t.Fatalf("second group service tier = %q, want priority", summary.Groups[1].RequestedServiceTier)
	}
	if summary.Groups[0].ObservedServiceTierCounts["default"] != 1 {
		t.Fatalf("observed default tiers = %#v", summary.Groups[0].ObservedServiceTierCounts)
	}
	if summary.Groups[1].ObservedServiceTierCounts["priority"] != 1 {
		t.Fatalf("observed priority tiers = %#v", summary.Groups[1].ObservedServiceTierCounts)
	}
}

// TestSummarizeSeparatesCacheModes verifies mixed cache modes are never combined into one metric group.
func TestSummarizeSeparatesCacheModes(t *testing.T) {
	summary := Summarize([]RequestRecord{
		summaryRecord(summaryRecordConfig{cacheMode: CacheReuse, ttftMS: 20, completionTokens: 1, bodyEOFMS: 50}),
		summaryRecord(summaryRecordConfig{cacheMode: CacheBust, ttftMS: 100, completionTokens: 1, bodyEOFMS: 50}),
	})

	if len(summary.Groups) != 2 {
		t.Fatalf("group count = %d, want 2", len(summary.Groups))
	}
	if summary.Groups[0].CacheMode != CacheBust {
		t.Fatalf("first group cache mode = %q, want %q", summary.Groups[0].CacheMode, CacheBust)
	}
	if summary.Groups[1].CacheMode != CacheReuse {
		t.Fatalf("second group cache mode = %q, want %q", summary.Groups[1].CacheMode, CacheReuse)
	}
	assertSummaryDistribution(t, "cache-bust ttft", summary.Groups[0].Metrics.TTFTDeltaMS, 1, 100)
	assertSummaryDistribution(t, "cache-reuse ttft", summary.Groups[1].Metrics.TTFTDeltaMS, 1, 20)
}

// TestSummarizeRequiresCompleteSystemThroughputData verifies system TPS is omitted when token usage is incomplete.
func TestSummarizeRequiresCompleteSystemThroughputData(t *testing.T) {
	recordWithTokens := summaryRecord(summaryRecordConfig{cacheMode: CacheReuse, ttftMS: 20, completionTokens: 1, bodyEOFMS: 50})
	recordWithoutTokens := summaryRecord(summaryRecordConfig{cacheMode: CacheReuse, ttftMS: 30, bodyEOFMS: 100})
	recordWithoutTokens.CompletionTokens = nil

	summary := Summarize([]RequestRecord{recordWithTokens, recordWithoutTokens})
	if len(summary.Groups) != 1 {
		t.Fatalf("group count = %d, want 1", len(summary.Groups))
	}
	group := summary.Groups[0]
	if group.CompletionTokenRecords != 1 {
		t.Fatalf("completion token records = %d, want 1", group.CompletionTokenRecords)
	}
	if group.SystemTPS != nil {
		t.Fatalf("system_tps = %v, want nil with incomplete token data", *group.SystemTPS)
	}
	if group.RPS == nil {
		t.Fatal("rps should still be available with complete timing data")
	}
}

const httpStatusTooManyRequests = 429

type summaryRecordConfig struct {
	warmup               bool
	cacheMode            CacheMode
	ttftMS               float64
	completionTokens     int
	cachedTokens         *int
	providerProcessingMS *float64
	protocol             string
	reused               bool
	bodyEOFMS            float64
	serviceTier          string
	observedServiceTier  string
	targetID             string
	targetName           string
}

func summaryRecord(cfg summaryRecordConfig) RequestRecord {
	bodyEOFMS := cfg.bodyEOFMS
	if bodyEOFMS == 0 {
		bodyEOFMS = 100
	}
	lastOutputMS := bodyEOFMS - 10
	if lastOutputMS < cfg.ttftMS {
		lastOutputMS = cfg.ttftMS
	}
	completionTokens := cfg.completionTokens

	return RequestRecord{
		RequestID:            "req-summary",
		TargetID:             cfg.targetID,
		TargetName:           cfg.targetName,
		Provider:             "provider",
		Model:                "model",
		ScenarioName:         "scenario",
		Warmup:               cfg.warmup,
		CacheMode:            cfg.cacheMode,
		ConnectionMode:       WarmConnections,
		RequestedServiceTier: cfg.serviceTier,
		ObservedServiceTier:  cfg.observedServiceTier,
		CompletionTokens:     &completionTokens,
		Cache: CacheRecord{
			PromptCachedTokens: cfg.cachedTokens,
		},
		HTTP: HTTPRecord{
			StatusCode:           200,
			Protocol:             cfg.protocol,
			ProviderProcessingMS: cfg.providerProcessingMS,
			RequestedServiceTier: cfg.serviceTier,
			ObservedServiceTier:  cfg.observedServiceTier,
			ConnReused:           cfg.reused,
		},
		Timeline: Timeline{
			BaseWallUnixNano: int64(time.Second),
			EventsNS: map[EventName]int64{
				EventRequestStart:      0,
				EventFirstResponseByte: msToNS(cfg.ttftMS / 2),
				EventHeadersReceived:   msToNS(cfg.ttftMS / 2),
				EventFirstSSEEvent:     msToNS(cfg.ttftMS / 2),
				EventFirstOutputDelta:  msToNS(cfg.ttftMS),
				EventLastOutputDelta:   msToNS(lastOutputMS),
				EventBodyEOF:           msToNS(bodyEOFMS),
			},
		},
		Derived: DerivedMetrics{
			TTFTDeltaMS:                   summaryFloatPointer(cfg.ttftMS),
			HTTPTTFBMS:                    summaryFloatPointer(cfg.ttftMS / 2),
			HeadersLatencyMS:              summaryFloatPointer(cfg.ttftMS / 2),
			FirstEventMS:                  summaryFloatPointer(cfg.ttftMS / 2),
			E2EDeltaMS:                    summaryFloatPointer(lastOutputMS),
			StreamTotalMS:                 summaryFloatPointer(bodyEOFMS),
			GenerationDeltaMS:             summaryFloatPointer(lastOutputMS - cfg.ttftMS),
			ServerWaitToFirstByteMS:       summaryFloatPointer(cfg.ttftMS / 2),
			StreamProtocolToFirstOutputMS: summaryFloatPointer(cfg.ttftMS / 2),
			E2EOutputTPS:                  summaryFloatPointer(float64(cfg.completionTokens) / (lastOutputMS / 1000)),
			GenerationDeltaOutputTPS:      summaryGenerationTPS(cfg.completionTokens, lastOutputMS-cfg.ttftMS),
		},
	}
}

func summaryErrorRecord(cacheMode CacheMode, category string, statusCode int) RequestRecord {
	return RequestRecord{
		RequestID:      "req-error",
		Provider:       "provider",
		Model:          "model",
		ScenarioName:   "scenario",
		CacheMode:      cacheMode,
		ConnectionMode: WarmConnections,
		HTTP:           HTTPRecord{StatusCode: statusCode},
		Error: &ErrorRecord{
			Category:   category,
			Message:    "rate limited",
			StatusCode: statusCode,
		},
	}
}

func assertSummaryDistribution(t *testing.T, name string, distribution Distribution, count int, p50 float64) {
	t.Helper()

	if distribution.Count != count {
		t.Fatalf("%s count = %d, want %d", name, distribution.Count, count)
	}
	assertFloatPointer(t, name+" p50", distribution.P50, p50)
}

func assertFloatPointer(t *testing.T, name string, got *float64, want float64) {
	t.Helper()

	if got == nil {
		t.Fatalf("%s = nil, want %.12g", name, want)
	}
	if math.Abs(*got-want) > 1e-9 {
		t.Fatalf("%s = %.12g, want %.12g", name, *got, want)
	}
}

func summaryGenerationTPS(completionTokens int, generationMS float64) *float64 {
	if completionTokens <= 1 || generationMS <= 0 {
		return nil
	}

	return summaryFloatPointer(float64(completionTokens-1) / (generationMS / 1000))
}

func summaryFloatPointer(value float64) *float64 {
	return &value
}

func summaryIntPointer(value int) *int {
	return &value
}

func msToNS(milliseconds float64) int64 {
	return int64(milliseconds * float64(time.Millisecond))
}
