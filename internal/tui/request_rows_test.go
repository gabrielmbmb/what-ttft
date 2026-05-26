package tui

import (
	"reflect"
	"testing"

	"github.com/gabrielmbmb/what-ttft/pkg/whatttft"
)

// TestRequestRowsCaptureRequestDiagnostics verifies row construction preserves identifying diagnostics without generated content.
func TestRequestRowsCaptureRequestDiagnostics(t *testing.T) {
	cachedTokens := 12
	promptTokens := 100
	totalTokens := 104
	cacheMissTokens := 0
	store := newLiveStore()
	store.applyEvent(whatttft.RunEvent{Kind: whatttft.EventBenchmarkStarted, BenchmarkName: "bench", ProviderAPI: "responses", Targets: []whatttft.RunEventTarget{
		{TargetID: "target-a", TargetName: "Target A", Provider: "openai", ProviderAPI: "responses", RequestedServiceTier: "default", Model: "gpt-a"},
		{TargetID: "target-b", TargetName: "Target B", Provider: "openai", ProviderAPI: "chat-completions", RequestedServiceTier: "priority", Model: "gpt-b"},
	}})

	success := tuiTestRecord("target-a-req-000000", "target-a", 10, 100, nil)
	success.TargetName = "Target A"
	success.Model = "gpt-a"
	success.PromptTokens = &promptTokens
	success.TotalTokens = &totalTokens
	success.Cache.PromptCachedTokens = &cachedTokens
	success.HTTP.StatusCode = 200
	success.HTTP.Protocol = "HTTP/2.0"
	success.HTTP.GotConn = true
	success.HTTP.ConnReused = true
	success.HTTP.RequestedServiceTier = "default"

	failed := tuiTestRecord("target-b-req-000000", "target-b", 0, 0, &whatttft.ErrorRecord{Category: "http_status", StatusCode: 429, BodySnippet: "SECRET_API_KEY body"})
	failed.Model = "gpt-b"
	failed.Derived.TTFTDeltaMS = nil
	failed.Derived.E2EDeltaMS = nil
	failed.HTTP.StatusCode = 429
	failed.HTTP.GotConn = true
	failed.HTTP.ConnReused = false
	failed.OutputDeltaCount = 0

	warmup := tuiTestRecord("target-a-req-000001", "target-a", 20, 120, nil)
	warmup.Warmup = true
	warmup.Model = "gpt-a"
	warmup.Cache.PromptCachedTokens = &cacheMissTokens
	warmup.OutputDeltaCount = 0

	for _, record := range []whatttft.RequestRecord{success, failed, warmup} {
		store.applyEvent(whatttft.RunEvent{Kind: whatttft.EventRequestFinished, TargetID: record.TargetID, RequestID: record.RequestID, Record: &record})
	}

	rows := store.requestRows()
	if len(rows) != 3 {
		t.Fatalf("request rows = %d, want 3", len(rows))
	}
	assertRequestRowBasics(t, rows[0], requestRow{
		Ordinal:       0,
		RequestID:     "target-a-req-000000",
		Phase:         requestPhaseMeasured,
		TargetID:      "target-a",
		TargetName:    "Target A",
		TargetOrdinal: 0,
		Provider:      "openai",
		ProviderAPI:   "responses",
		Model:         "gpt-a",
		ServiceTier:   "default",
		Outcome:       requestOutcomeOK,
		HTTPStatus:    "200",
		ErrorCategory: "-",
		CacheState:    requestCacheHit,
		Conn:          requestConnReused,
		Protocol:      "HTTP/2.0",
		OutputState:   requestOutputDisabled,
	})
	if rows[0].PromptTokens == nil || *rows[0].PromptTokens != 100 || rows[0].CompletionTokens == nil || *rows[0].CompletionTokens != 4 || rows[0].CachedTokens == nil || *rows[0].CachedTokens != 12 {
		t.Fatalf("success token/cache pointers = prompt:%v completion:%v cached:%v", rows[0].PromptTokens, rows[0].CompletionTokens, rows[0].CachedTokens)
	}
	if rows[0].TTFTMS == nil || *rows[0].TTFTMS != 10 || rows[0].E2EOutputTPS == nil || *rows[0].E2EOutputTPS != 40 {
		t.Fatalf("success metric pointers = ttft:%v tps:%v", rows[0].TTFTMS, rows[0].E2EOutputTPS)
	}

	assertRequestRowBasics(t, rows[1], requestRow{
		Ordinal:       1,
		RequestID:     "target-b-req-000000",
		Phase:         requestPhaseMeasured,
		TargetID:      "target-b",
		TargetName:    "Target B",
		TargetOrdinal: 1,
		Provider:      "openai",
		ProviderAPI:   "chat-completions",
		Model:         "gpt-b",
		ServiceTier:   "priority",
		Outcome:       requestOutcomeError,
		HTTPStatus:    "429",
		ErrorCategory: "http_status",
		CacheState:    requestCacheUnknown,
		Conn:          requestConnNew,
		Protocol:      "-",
		OutputState:   requestOutputEmpty,
	})
	if rows[1].TTFTMS != nil || rows[1].E2EMS != nil {
		t.Fatalf("failed row TTFT/E2E = %v/%v, want nil/nil", rows[1].TTFTMS, rows[1].E2EMS)
	}

	if rows[2].Phase != requestPhaseWarmup || rows[2].CacheState != requestCacheMiss || rows[2].OutputState != requestOutputEmpty {
		t.Fatalf("warmup row phase/cache/output = %q/%q/%q, want warmup/miss/empty", rows[2].Phase, rows[2].CacheState, rows[2].OutputState)
	}
}

// TestRequestRowsSortingDoesNotMutateCanonicalOrder verifies row sorting works on copies and leaves store order unchanged.
func TestRequestRowsSortingDoesNotMutateCanonicalOrder(t *testing.T) {
	store := newLiveStore()
	store.applyEvent(whatttft.RunEvent{Kind: whatttft.EventBenchmarkStarted, Targets: []whatttft.RunEventTarget{{TargetID: "target-a"}, {TargetID: "target-b"}}})
	for _, record := range []whatttft.RequestRecord{
		tuiTestRecord("target-b-req-000000", "target-b", 20, 100, nil),
		tuiTestRecord("target-a-req-000000", "target-a", 10, 100, nil),
	} {
		store.applyEvent(whatttft.RunEvent{Kind: whatttft.EventRequestFinished, TargetID: record.TargetID, RequestID: record.RequestID, Record: &record})
	}

	rows := store.requestRows()
	sorted := sortRequestRows(rows, requestSortTargetOrder)
	if got := []string{sorted[0].RequestID, sorted[1].RequestID}; !reflect.DeepEqual(got, []string{"target-a-req-000000", "target-b-req-000000"}) {
		t.Fatalf("target-order sorted request IDs = %#v, want target-a then target-b", got)
	}
	if got := []string{rows[0].RequestID, rows[1].RequestID}; !reflect.DeepEqual(got, []string{"target-b-req-000000", "target-a-req-000000"}) {
		t.Fatalf("input rows mutated by sorting: %#v", got)
	}
	if got := append([]string(nil), store.recordOrder...); !reflect.DeepEqual(got, []string{"target-b-req-000000", "target-a-req-000000"}) {
		t.Fatalf("store record order mutated by sorting: %#v", got)
	}

	sorted[0].RequestID = "mutated"
	if store.records["target-a-req-000000"].RequestID != "target-a-req-000000" {
		t.Fatalf("mutating sorted row changed canonical record: %#v", store.records["target-a-req-000000"])
	}
}

func assertRequestRowBasics(t *testing.T, got requestRow, want requestRow) {
	t.Helper()
	if got.Ordinal != want.Ordinal || got.RequestID != want.RequestID || got.Phase != want.Phase || got.TargetID != want.TargetID || got.TargetName != want.TargetName || got.TargetOrdinal != want.TargetOrdinal || got.Provider != want.Provider || got.ProviderAPI != want.ProviderAPI || got.Model != want.Model || got.ServiceTier != want.ServiceTier || got.Outcome != want.Outcome || got.HTTPStatus != want.HTTPStatus || got.ErrorCategory != want.ErrorCategory || got.CacheState != want.CacheState || got.Conn != want.Conn || got.Protocol != want.Protocol || got.OutputState != want.OutputState {
		t.Fatalf("request row basics = %#v, want %#v", got, want)
	}
}
