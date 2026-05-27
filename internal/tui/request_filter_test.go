package tui

import (
	"reflect"
	"testing"
)

// TestRequestFiltersCoverRequiredDimensions verifies each required request filter dimension selects the expected rows.
func TestRequestFiltersCoverRequiredDimensions(t *testing.T) {
	rows := requestFilterTestRows()
	tests := []struct {
		name  string
		query string
		want  []string
	}{
		{name: "target", query: "target:target-a", want: []string{"target-a-req-000001", "target-a-req-000003"}},
		{name: "model", query: "model:gpt-b", want: []string{"target-b-req-000002"}},
		{name: "provider api", query: "api:responses", want: []string{"target-a-req-000001", "target-a-req-000003"}},
		{name: "warmup phase", query: "phase:warmup", want: []string{"target-b-req-000002"}},
		{name: "measured phase alias", query: "warmup:false", want: []string{"target-a-req-000001", "target-a-req-000003"}},
		{name: "success outcome", query: "outcome:success", want: []string{"target-a-req-000001"}},
		{name: "failed outcome", query: "outcome:error", want: []string{"target-b-req-000002", "target-a-req-000003"}},
		{name: "http status", query: "status:429", want: []string{"target-b-req-000002"}},
		{name: "http status class", query: "status:5xx", want: []string{"target-a-req-000003"}},
		{name: "error category", query: "error:rate_limit", want: []string{"target-b-req-000002"}},
		{name: "cache hit", query: "cache:hit", want: []string{"target-a-req-000001"}},
		{name: "cache miss", query: "cache:miss", want: []string{"target-b-req-000002"}},
		{name: "cache unknown", query: "cache:unknown", want: []string{"target-a-req-000003"}},
		{name: "request id", query: "id:000003", want: []string{"target-a-req-000003"}},
		{name: "bare substring", query: "provider", want: []string{"target-a-req-000003"}},
		{name: "ttft threshold", query: "ttft_delta_ms>200", want: []string{"target-b-req-000002"}},
		{name: "e2e threshold", query: "e2e_delta_ms>=900", want: []string{"target-b-req-000002", "target-a-req-000003"}},
		{name: "stream threshold", query: "stream_total_ms<1000", want: []string{"target-a-req-000001"}},
		{name: "ttfb threshold", query: "http_ttfb_ms<=20", want: []string{"target-a-req-000001"}},
		{name: "output tps threshold", query: "e2e_output_tps>50", want: []string{"target-a-req-000003"}},
		{name: "generation tps threshold", query: "generation_delta_output_tps>=12", want: []string{"target-a-req-000001", "target-a-req-000003"}},
		{name: "completion tokens threshold", query: "tokens>=10", want: []string{"target-a-req-000001", "target-a-req-000003"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			filters, _, err := parseRequestFilterQuery(tt.query)
			if err != nil {
				t.Fatalf("parse %q: %v", tt.query, err)
			}
			if got := matchingRequestIDs(rows, filters); !reflect.DeepEqual(got, tt.want) {
				t.Fatalf("filter %q request IDs = %#v, want %#v", tt.query, got, tt.want)
			}
		})
	}
}

// TestRequestFilterParserRejectsInvalidDrafts verifies invalid structured filters are rejected before application.
func TestRequestFilterParserRejectsInvalidDrafts(t *testing.T) {
	for _, query := range []string{"unknown:value", "status:not-a-status", "ttft_delta_ms>slow", "sort:nope"} {
		t.Run(query, func(t *testing.T) {
			if _, _, err := parseRequestFilterQuery(query); err == nil {
				t.Fatalf("parse %q unexpectedly succeeded", query)
			}
		})
	}
}

// TestRequestSortsCoverRequiredOrders verifies each required request sort produces deterministic row order.
func TestRequestSortsCoverRequiredOrders(t *testing.T) {
	rows := []requestRow{
		requestFilterRow("request-1", "target-b", 1, "gpt-b", requestOutcomeOK, "200", 100, 700, 900, 20),
		requestFilterRow("request-2", "target-a", 0, "gpt-a", requestOutcomeError, "500", 300, 200, 500, 5),
		requestFilterRow("request-3", "target-a", 0, "gpt-c", requestOutcomeError, "429", 0, 900, 400, 80),
	}
	rows[2].TTFTMS = nil
	tests := []struct {
		name string
		sort requestSort
		want []string
	}{
		{name: "completion order", sort: requestSortCompletionOrder, want: []string{"request-1", "request-2", "request-3"}},
		{name: "slowest ttft", sort: requestSortSlowestTTFT, want: []string{"request-2", "request-1", "request-3"}},
		{name: "slowest e2e", sort: requestSortSlowestE2E, want: []string{"request-3", "request-1", "request-2"}},
		{name: "slowest stream", sort: requestSortSlowestStream, want: []string{"request-1", "request-2", "request-3"}},
		{name: "highest tps", sort: requestSortHighestTPS, want: []string{"request-3", "request-1", "request-2"}},
		{name: "lowest tps", sort: requestSortLowestTPS, want: []string{"request-2", "request-1", "request-3"}},
		{name: "errors first", sort: requestSortErrorsFirst, want: []string{"request-2", "request-3", "request-1"}},
		{name: "target model order", sort: requestSortTargetOrder, want: []string{"request-2", "request-3", "request-1"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := requestIDs(sortRequestRows(rows, tt.sort)); !reflect.DeepEqual(got, tt.want) {
				t.Fatalf("sort %s request IDs = %#v, want %#v", tt.sort, got, tt.want)
			}
		})
	}
}

func matchingRequestIDs(rows []requestRow, filters requestFilters) []string {
	var ids []string
	for _, row := range rows {
		if filters.matches(row) {
			ids = append(ids, row.RequestID)
		}
	}
	return ids
}

func requestIDs(rows []requestRow) []string {
	ids := make([]string, 0, len(rows))
	for _, row := range rows {
		ids = append(ids, row.RequestID)
	}
	return ids
}

func requestFilterTestRows() []requestRow {
	rows := []requestRow{
		requestFilterRow("target-a-req-000001", "target-a", 0, "gpt-a", requestOutcomeOK, "200", 100, 500, 600, 30),
		requestFilterRow("target-b-req-000002", "target-b", 1, "gpt-b", requestOutcomeError, "429", 300, 900, 1000, 5),
		requestFilterRow("target-a-req-000003", "target-a", 0, "gpt-c", requestOutcomeError, "503", 0, 1200, 1300, 80),
	}
	rows[0].ProviderAPI = "responses"
	rows[0].Phase = requestPhaseMeasured
	rows[0].ErrorCategory = "-"
	rows[0].CacheState = requestCacheHit
	rows[0].CompletionTokens = intPointer(10)
	rows[0].GenerationTPS = tuiFloatPointer(12)

	rows[1].ProviderAPI = "chat-completions"
	rows[1].Phase = requestPhaseWarmup
	rows[1].ErrorCategory = "rate_limit"
	rows[1].CacheState = requestCacheMiss
	rows[1].CompletionTokens = intPointer(2)
	rows[1].GenerationTPS = tuiFloatPointer(4)

	rows[2].ProviderAPI = "responses"
	rows[2].Phase = requestPhaseMeasured
	rows[2].ErrorCategory = "provider"
	rows[2].CacheState = requestCacheUnknown
	rows[2].TTFTMS = nil
	rows[2].TTFBMS = tuiFloatPointer(60)
	rows[2].CompletionTokens = intPointer(20)
	rows[2].GenerationTPS = tuiFloatPointer(60)
	return rows
}

func requestFilterRow(requestID string, targetID string, targetOrdinal int, model string, outcome string, status string, ttft float64, e2e float64, stream float64, tps float64) requestRow {
	return requestRow{
		RequestID:     requestID,
		Ordinal:       len(requestID),
		TargetID:      targetID,
		TargetOrdinal: targetOrdinal,
		Provider:      "openai",
		ProviderAPI:   "responses",
		Model:         model,
		Phase:         requestPhaseMeasured,
		Outcome:       outcome,
		HTTPStatus:    status,
		ErrorCategory: "-",
		TTFTMS:        tuiFloatPointer(ttft),
		E2EMS:         tuiFloatPointer(e2e),
		StreamTotalMS: tuiFloatPointer(stream),
		TTFBMS:        tuiFloatPointer(ttft / 5),
		E2EOutputTPS:  tuiFloatPointer(tps),
		CacheState:    requestCacheUnknown,
	}
}

func intPointer(value int) *int {
	return &value
}
