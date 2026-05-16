package whatttft

import (
	"encoding/json"
	"strings"
	"testing"
)

// TestRequestRecordJSONShape verifies the stable JSON field names for request records.
func TestRequestRecordJSONShape(t *testing.T) {
	promptTokens := 12
	cacheHit := false
	ttfbMS := 123.45

	rec := RequestRecord{
		RequestID:      "req-001",
		Provider:       "openai",
		Model:          "test-model",
		ScenarioName:   "short",
		Warmup:         false,
		Attempt:        1,
		CacheMode:      CacheBust,
		ConnectionMode: WarmConnections,
		PromptHash:     strings.Repeat("a", 64),
		PromptTokens:   &promptTokens,
		Cache: CacheRecord{
			Hit:                &cacheHit,
			PromptCachedTokens: intPtr(0),
		},
		HTTP: HTTPRecord{
			StatusCode:          200,
			Status:              "200 OK",
			Protocol:            "HTTP/2.0",
			GotConn:             true,
			ConnReused:          true,
			ConnWasIdle:         true,
			ConnIdleTimeNS:      15,
			CompressionDisabled: true,
		},
		Timeline: Timeline{
			BaseWallUnixNano: 1700000000000000000,
			EventsNS: map[EventName]int64{
				EventRequestStart:      0,
				EventFirstResponseByte: 123450000,
				EventFirstOutputDelta:  200000000,
			},
		},
		Derived: DerivedMetrics{
			HTTPTTFBMS: &ttfbMS,
		},
	}

	data, err := json.Marshal(rec)
	if err != nil {
		t.Fatalf("marshal request record: %v", err)
	}

	if strings.Contains(string(data), "completion_tokens") {
		t.Fatalf("nil completion token count should be omitted: %s", data)
	}

	var got RequestRecord
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("unmarshal request record: %v", err)
	}

	if got.CacheMode != CacheBust {
		t.Fatalf("cache mode = %q, want %q", got.CacheMode, CacheBust)
	}
	if got.ConnectionMode != WarmConnections {
		t.Fatalf("connection mode = %q, want %q", got.ConnectionMode, WarmConnections)
	}
	if got.Timeline.EventsNS[EventFirstOutputDelta] != 200000000 {
		t.Fatalf("first output delta ns = %d, want 200000000", got.Timeline.EventsNS[EventFirstOutputDelta])
	}
	if got.Derived.HTTPTTFBMS == nil || *got.Derived.HTTPTTFBMS != ttfbMS {
		t.Fatalf("http_ttfb_ms = %v, want %v", got.Derived.HTTPTTFBMS, ttfbMS)
	}
	if got.Cache.Hit == nil || *got.Cache.Hit {
		t.Fatalf("cache hit = %v, want pointer to false", got.Cache.Hit)
	}
}

// TestChunkRecordJSONShape verifies the stable JSON field names for chunk records.
func TestChunkRecordJSONShape(t *testing.T) {
	rec := ChunkRecord{
		RequestID:    "req-001",
		Index:        2,
		AtNS:         300000000,
		SSEDataBytes: 42,
		Content:      "hello",
		Role:         "assistant",
		Empty:        false,
		UsageChunk:   false,
	}

	data, err := json.Marshal(rec)
	if err != nil {
		t.Fatalf("marshal chunk record: %v", err)
	}

	var got ChunkRecord
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("unmarshal chunk record: %v", err)
	}

	if got.Content != "hello" {
		t.Fatalf("content = %q, want hello", got.Content)
	}
	if got.SSEDataBytes != 42 {
		t.Fatalf("sse data bytes = %d, want 42", got.SSEDataBytes)
	}
}

func intPtr(value int) *int {
	return &value
}
