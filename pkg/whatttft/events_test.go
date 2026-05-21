package whatttft

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
)

// TestRunEventRepresentativeJSONRoundTrips verifies representative live event shapes are JSON-stable.
func TestRunEventRepresentativeJSONRoundTrips(t *testing.T) {
	attempt := 0
	warmup := false
	completionTokens := 8
	ttftMS := 123.4
	summaryP50 := 123.4
	record := RequestRecord{
		RequestID:            "target-a-req-000000",
		TargetID:             "target-a",
		TargetName:           "Target A",
		Provider:             "openai",
		Model:                "gpt-test",
		ScenarioName:         "short",
		Attempt:              0,
		CacheMode:            CacheBust,
		ConnectionMode:       WarmConnections,
		RequestedServiceTier: "priority",
		ObservedServiceTier:  "priority",
		PromptHash:           strings.Repeat("a", 64),
		CompletionTokens:     &completionTokens,
		Timeline: Timeline{EventsNS: map[EventName]int64{
			EventRequestStart:     0,
			EventFirstOutputDelta: 123400000,
		}},
		Derived: DerivedMetrics{TTFTDeltaMS: &ttftMS},
	}
	summary := RunSummary{
		TotalRequests:      1,
		MeasuredRequests:   1,
		SuccessfulRequests: 1,
		Groups: []SummaryGroup{{
			TargetID:             "target-a",
			TargetName:           "Target A",
			Provider:             "openai",
			Model:                "gpt-test",
			ScenarioName:         "short",
			CacheMode:            CacheBust,
			ConnectionMode:       WarmConnections,
			RequestedServiceTier: "priority",
			MeasuredRequests:     1,
			SuccessfulRequests:   1,
			Metrics: MetricDistributions{
				TTFTDeltaMS: Distribution{Count: 1, P50: &summaryP50},
			},
		}},
	}

	tests := []RunEvent{
		{
			Sequence:         1,
			Kind:             EventBenchmarkStarted,
			WallUnixNano:     1700000000000000000,
			BenchmarkName:    "model-compare",
			TotalRequests:    2,
			MeasuredRequests: 2,
		},
		{
			Sequence:         2,
			Kind:             EventTargetStarted,
			WallUnixNano:     1700000000000000001,
			BenchmarkName:    "model-compare",
			TargetID:         "target-a",
			TargetName:       "Target A",
			Provider:         "openai",
			Model:            "gpt-test",
			ScenarioName:     "short",
			CacheMode:        CacheBust,
			ConnectionMode:   WarmConnections,
			TotalRequests:    1,
			MeasuredRequests: 1,
		},
		{
			Sequence:             3,
			Kind:                 EventRequestFinished,
			WallUnixNano:         1700000000000000002,
			TargetID:             "target-a",
			Provider:             "openai",
			Model:                "gpt-test",
			ScenarioName:         "short",
			CacheMode:            CacheBust,
			ConnectionMode:       WarmConnections,
			RequestedServiceTier: "priority",
			Phase:                PhaseMeasured,
			Attempt:              &attempt,
			Warmup:               &warmup,
			RequestID:            "target-a-req-000000",
			CompletedRequests:    1,
			SuccessfulRequests:   1,
			Record:               &record,
		},
		{
			Sequence:           4,
			Kind:               EventSummaryUpdated,
			WallUnixNano:       1700000000000000003,
			CompletedRequests:  1,
			SuccessfulRequests: 1,
			Summary:            &summary,
		},
		{
			Sequence:     5,
			Kind:         EventReportWriteFailed,
			WallUnixNano: 1700000000000000004,
			OutputDir:    "runs/failure",
			Error: &RunEventError{
				Category:  "report_write",
				Message:   "write summary: permission denied",
				Retryable: false,
			},
		},
	}

	for _, original := range tests {
		data, err := json.Marshal(original)
		if err != nil {
			t.Fatalf("marshal %s event: %v", original.Kind, err)
		}
		if strings.Contains(string(data), "sk-test") || strings.Contains(strings.ToLower(string(data)), "authorization") {
			t.Fatalf("event JSON contains a secret-looking value: %s", data)
		}

		var got RunEvent
		if err := json.Unmarshal(data, &got); err != nil {
			t.Fatalf("unmarshal %s event: %v", original.Kind, err)
		}
		if got.Kind != original.Kind {
			t.Fatalf("kind = %q, want %q in JSON %s", got.Kind, original.Kind, data)
		}
		if got.Sequence != original.Sequence {
			t.Fatalf("sequence = %d, want %d", got.Sequence, original.Sequence)
		}
		if original.TargetID != "" && got.TargetID != original.TargetID {
			t.Fatalf("target_id = %q, want %q", got.TargetID, original.TargetID)
		}
		if original.Record != nil {
			if got.Record == nil {
				t.Fatalf("record missing after JSON round trip: %s", data)
			}
			if got.Attempt == nil || *got.Attempt != 0 {
				t.Fatalf("attempt = %v, want pointer to zero", got.Attempt)
			}
			if got.Warmup == nil || *got.Warmup {
				t.Fatalf("warmup = %v, want pointer to false", got.Warmup)
			}
			if got.Record.RequestID != original.Record.RequestID {
				t.Fatalf("record request_id = %q, want %q", got.Record.RequestID, original.Record.RequestID)
			}
		}
		if original.Summary != nil {
			if got.Summary == nil || got.Summary.SuccessfulRequests != original.Summary.SuccessfulRequests {
				t.Fatalf("summary = %#v, want successful requests %d", got.Summary, original.Summary.SuccessfulRequests)
			}
		}
		if original.Error != nil {
			if got.Error == nil || got.Error.Category != original.Error.Category {
				t.Fatalf("error = %#v, want category %q", got.Error, original.Error.Category)
			}
		}
	}
}

// TestRunObserverFuncAndNilObserver verifies observer helper behavior for function and nil observers.
func TestRunObserverFuncAndNilObserver(t *testing.T) {
	ctx := context.Background()
	notifyRunObserver(ctx, nil, RunEvent{Kind: EventRunStarted})

	var nilFunc RunObserverFunc
	notifyRunObserver(ctx, nilFunc, RunEvent{Kind: EventRunStarted})

	var calls int
	var got RunEvent
	observer := RunObserverFunc(func(_ context.Context, event RunEvent) {
		calls++
		got = event
	})
	notifyRunObserver(ctx, observer, RunEvent{Sequence: 7, Kind: EventRunFinished})

	if calls != 1 {
		t.Fatalf("observer calls = %d, want 1", calls)
	}
	if got.Sequence != 7 || got.Kind != EventRunFinished {
		t.Fatalf("event = %#v, want sequence 7 run_finished", got)
	}
}

// TestRunEventCloneDefensiveCopy verifies Clone protects asynchronous consumers from later event mutation.
func TestRunEventCloneDefensiveCopy(t *testing.T) {
	attempt := 0
	warmup := false
	promptTokens := 12
	cacheHit := true
	cachedTokens := 4
	providerMS := 9.5
	ttftMS := 123.4
	p50 := 123.4
	p95 := 200.0
	systemTPS := 30.0
	eventError := &RunEventError{Category: "report_write", Message: "original", Retryable: false}

	event := RunEvent{
		Sequence:          9,
		Kind:              EventRequestFinished,
		WallUnixNano:      1700000000000000000,
		TargetID:          "target-a",
		Attempt:           &attempt,
		Warmup:            &warmup,
		RequestID:         "target-a-req-000000",
		CompletedRequests: 1,
		Record: &RequestRecord{
			RequestID:      "target-a-req-000000",
			TargetID:       "target-a",
			Provider:       "openai",
			Model:          "gpt-test",
			ScenarioName:   "short",
			CacheMode:      CacheBust,
			ConnectionMode: WarmConnections,
			PromptHash:     strings.Repeat("b", 64),
			PromptTokens:   &promptTokens,
			Cache: CacheRecord{
				Hit:                &cacheHit,
				PromptCachedTokens: &cachedTokens,
				Extra: map[string]any{
					"string": "original",
					"nested": map[string]any{"value": "original"},
					"slice":  []any{"original"},
				},
			},
			HTTP: HTTPRecord{ProviderProcessingMS: &providerMS},
			Timeline: Timeline{EventsNS: map[EventName]int64{
				EventFirstOutputDelta: 123400000,
			}},
			Derived: DerivedMetrics{TTFTDeltaMS: &ttftMS},
		},
		Summary: &RunSummary{
			TotalRequests:      1,
			MeasuredRequests:   1,
			SuccessfulRequests: 1,
			ErrorCategories:    map[string]int{"provider_error": 1},
			Groups: []SummaryGroup{{
				TargetID:                  "target-a",
				Provider:                  "openai",
				Model:                     "gpt-test",
				ScenarioName:              "short",
				CacheMode:                 CacheBust,
				ConnectionMode:            WarmConnections,
				MeasuredRequests:          1,
				SuccessfulRequests:        1,
				ErrorCategories:           map[string]int{"provider_error": 1},
				ObservedServiceTierCounts: map[string]int{"priority": 1},
				Metrics: MetricDistributions{
					TTFTDeltaMS: Distribution{Count: 1, P50: &p50, P95: &p95},
				},
				SystemTPS: &systemTPS,
				Cache: CacheSummary{
					CacheMode:          CacheBust,
					CachedPromptTokens: Distribution{Count: 1, P50: &p50},
				},
				Connection: ConnectionSummary{ProtocolCounts: map[string]int{"HTTP/2.0": 1}},
			}},
		},
		Error: eventError,
	}

	cloned := event.Clone()

	*event.Attempt = 5
	*event.Warmup = true
	*event.Record.PromptTokens = 99
	*event.Record.Cache.Hit = false
	*event.Record.Cache.PromptCachedTokens = 99
	event.Record.Cache.Extra["string"] = "mutated"
	nestedOriginal, ok := event.Record.Cache.Extra["nested"].(map[string]any)
	if !ok {
		t.Fatalf("original nested extra has type %T, want map[string]any", event.Record.Cache.Extra["nested"])
	}
	nestedOriginal["value"] = "mutated"
	sliceOriginal, ok := event.Record.Cache.Extra["slice"].([]any)
	if !ok {
		t.Fatalf("original slice extra has type %T, want []any", event.Record.Cache.Extra["slice"])
	}
	sliceOriginal[0] = "mutated"
	*event.Record.HTTP.ProviderProcessingMS = 99
	event.Record.Timeline.EventsNS[EventFirstOutputDelta] = 999
	*event.Record.Derived.TTFTDeltaMS = 99
	event.Summary.ErrorCategories["provider_error"] = 99
	event.Summary.Groups[0].ObservedServiceTierCounts["priority"] = 99
	*event.Summary.Groups[0].Metrics.TTFTDeltaMS.P50 = 99
	*event.Summary.Groups[0].SystemTPS = 99
	*event.Summary.Groups[0].Cache.CachedPromptTokens.P50 = 99
	event.Summary.Groups[0].Connection.ProtocolCounts["HTTP/2.0"] = 99
	event.Error.Message = "mutated"

	if cloned.Attempt == nil || *cloned.Attempt != 0 {
		t.Fatalf("cloned attempt = %v, want pointer to zero", cloned.Attempt)
	}
	if cloned.Warmup == nil || *cloned.Warmup {
		t.Fatalf("cloned warmup = %v, want pointer to false", cloned.Warmup)
	}
	if cloned.Record == event.Record {
		t.Fatal("cloned record reuses original record pointer")
	}
	if cloned.Record.PromptTokens == nil || *cloned.Record.PromptTokens != 12 {
		t.Fatalf("cloned prompt tokens = %v, want 12", cloned.Record.PromptTokens)
	}
	if cloned.Record.Cache.Hit == nil || !*cloned.Record.Cache.Hit {
		t.Fatalf("cloned cache hit = %v, want true", cloned.Record.Cache.Hit)
	}
	if cloned.Record.Cache.PromptCachedTokens == nil || *cloned.Record.Cache.PromptCachedTokens != 4 {
		t.Fatalf("cloned cached tokens = %v, want 4", cloned.Record.Cache.PromptCachedTokens)
	}
	if cloned.Record.Cache.Extra["string"] != "original" {
		t.Fatalf("cloned extra string = %v, want original", cloned.Record.Cache.Extra["string"])
	}
	nestedClone, ok := cloned.Record.Cache.Extra["nested"].(map[string]any)
	if !ok {
		t.Fatalf("cloned nested extra has type %T, want map[string]any", cloned.Record.Cache.Extra["nested"])
	}
	if nestedClone["value"] != "original" {
		t.Fatalf("cloned nested extra = %#v, want original", nestedClone)
	}
	sliceClone, ok := cloned.Record.Cache.Extra["slice"].([]any)
	if !ok {
		t.Fatalf("cloned slice extra has type %T, want []any", cloned.Record.Cache.Extra["slice"])
	}
	if sliceClone[0] != "original" {
		t.Fatalf("cloned extra slice = %#v, want original", sliceClone)
	}
	if cloned.Record.HTTP.ProviderProcessingMS == nil || *cloned.Record.HTTP.ProviderProcessingMS != 9.5 {
		t.Fatalf("cloned provider processing = %v, want 9.5", cloned.Record.HTTP.ProviderProcessingMS)
	}
	if cloned.Record.Timeline.EventsNS[EventFirstOutputDelta] != 123400000 {
		t.Fatalf("cloned first output delta = %d, want 123400000", cloned.Record.Timeline.EventsNS[EventFirstOutputDelta])
	}
	if cloned.Record.Derived.TTFTDeltaMS == nil || *cloned.Record.Derived.TTFTDeltaMS != 123.4 {
		t.Fatalf("cloned ttft = %v, want 123.4", cloned.Record.Derived.TTFTDeltaMS)
	}
	if cloned.Summary == event.Summary {
		t.Fatal("cloned summary reuses original summary pointer")
	}
	if cloned.Summary.ErrorCategories["provider_error"] != 1 {
		t.Fatalf("cloned summary error categories = %#v, want provider_error count 1", cloned.Summary.ErrorCategories)
	}
	if cloned.Summary.Groups[0].ObservedServiceTierCounts["priority"] != 1 {
		t.Fatalf("cloned observed tiers = %#v, want priority count 1", cloned.Summary.Groups[0].ObservedServiceTierCounts)
	}
	if cloned.Summary.Groups[0].Metrics.TTFTDeltaMS.P50 == nil || *cloned.Summary.Groups[0].Metrics.TTFTDeltaMS.P50 != 123.4 {
		t.Fatalf("cloned summary p50 = %v, want 123.4", cloned.Summary.Groups[0].Metrics.TTFTDeltaMS.P50)
	}
	if cloned.Summary.Groups[0].SystemTPS == nil || *cloned.Summary.Groups[0].SystemTPS != 30 {
		t.Fatalf("cloned system tps = %v, want 30", cloned.Summary.Groups[0].SystemTPS)
	}
	if cloned.Summary.Groups[0].Cache.CachedPromptTokens.P50 == nil || *cloned.Summary.Groups[0].Cache.CachedPromptTokens.P50 != 123.4 {
		t.Fatalf("cloned cached prompt p50 = %v, want 123.4", cloned.Summary.Groups[0].Cache.CachedPromptTokens.P50)
	}
	if cloned.Summary.Groups[0].Connection.ProtocolCounts["HTTP/2.0"] != 1 {
		t.Fatalf("cloned protocol counts = %#v, want HTTP/2.0 count 1", cloned.Summary.Groups[0].Connection.ProtocolCounts)
	}
	if cloned.Error == event.Error {
		t.Fatal("cloned error reuses original error pointer")
	}
	if cloned.Error.Message != "original" {
		t.Fatalf("cloned error message = %q, want original", cloned.Error.Message)
	}
}
