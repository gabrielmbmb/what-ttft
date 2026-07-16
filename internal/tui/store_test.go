package tui

import (
	"reflect"
	"testing"

	"github.com/gabrielmbmb/what-ttft/pkg/whatttft"
)

// TestLiveStoreRequestLifecycle verifies request events update active and completed progress counts.
func TestLiveStoreRequestLifecycle(t *testing.T) {
	store := newLiveStore()
	store.applyEvent(whatttft.RunEvent{
		Kind:             whatttft.EventRunStarted,
		BenchmarkName:    "bench",
		TotalRequests:    3,
		WarmupRequests:   1,
		MeasuredRequests: 2,
	})
	store.applyEvent(whatttft.RunEvent{Kind: whatttft.EventRequestScheduled, RequestID: "req-1"})
	store.applyEvent(whatttft.RunEvent{Kind: whatttft.EventRequestDispatched, RequestID: "req-1"})

	progress := store.progress()
	if progress.Total != 3 || progress.Warmup != 1 || progress.Measured != 2 || progress.Active != 1 {
		t.Fatalf("progress after dispatch = %#v, want total/warmup/measured/active 3/1/2/1", progress)
	}

	record := whatttft.RequestRecord{RequestID: "req-1", Timeline: whatttft.Timeline{EventsNS: map[whatttft.EventName]int64{whatttft.EventRequestStart: 0}}}
	store.applyEvent(whatttft.RunEvent{
		Kind:               whatttft.EventRequestFinished,
		RequestID:          "req-1",
		CompletedRequests:  1,
		SuccessfulRequests: 1,
		Record:             &record,
	})

	progress = store.progress()
	if progress.Active != 0 || progress.Completed != 1 || progress.Successful != 1 {
		t.Fatalf("progress after finish = %#v, want active/completed/success 0/1/1", progress)
	}
	if len(store.recordOrder) != 1 || store.recordOrder[0] != "req-1" {
		t.Fatalf("record order = %#v, want req-1", store.recordOrder)
	}
}

// TestLiveStoreCopiesRecords verifies stored request records are isolated from caller mutation.
func TestLiveStoreCopiesRecords(t *testing.T) {
	store := newLiveStore()
	record := whatttft.RequestRecord{
		RequestID: "req-1",
		Timeline:  whatttft.Timeline{EventsNS: map[whatttft.EventName]int64{whatttft.EventFirstOutputDelta: 100}},
		Cache:     whatttft.CacheRecord{Extra: map[string]any{"safe": "value"}},
	}
	store.applyEvent(whatttft.RunEvent{Kind: whatttft.EventRequestFinished, RequestID: "req-1", Record: &record})

	record.Timeline.EventsNS[whatttft.EventFirstOutputDelta] = 999
	record.Cache.Extra["safe"] = "mutated"

	stored := store.records["req-1"]
	if got := stored.Timeline.EventsNS[whatttft.EventFirstOutputDelta]; got != 100 {
		t.Fatalf("stored first output delta = %d, want copied value 100", got)
	}
	if got := stored.Cache.Extra["safe"]; got != "value" {
		t.Fatalf("stored cache extra = %#v, want copied value", got)
	}
}

// TestLiveStoreCopiesSummaryMaps verifies summary maps are isolated from caller mutation.
func TestLiveStoreCopiesSummaryMaps(t *testing.T) {
	store := newLiveStore()
	summary := whatttft.RunSummary{
		ErrorCategories: map[string]int{"provider": 1},
		Groups: []whatttft.SummaryGroup{{
			TargetID:                  "target-a",
			ObservedServiceTierCounts: map[string]int{"default": 1},
			Connection:                whatttft.ConnectionSummary{ProtocolCounts: map[string]int{"HTTP/2.0": 1}},
		}},
	}
	store.applyEvent(whatttft.RunEvent{Kind: whatttft.EventSummaryUpdated, Summary: &summary})

	summary.ErrorCategories["provider"] = 99
	summary.Groups[0].ObservedServiceTierCounts["default"] = 99
	summary.Groups[0].Connection.ProtocolCounts["HTTP/2.0"] = 99

	if got := store.summary.ErrorCategories["provider"]; got != 1 {
		t.Fatalf("stored summary error category = %d, want 1", got)
	}
	if got := store.summary.Groups[0].ObservedServiceTierCounts["default"]; got != 1 {
		t.Fatalf("stored observed tier count = %d, want 1", got)
	}
	if got := store.summary.Groups[0].Connection.ProtocolCounts["HTTP/2.0"]; got != 1 {
		t.Fatalf("stored protocol count = %d, want 1", got)
	}
}

// TestLiveStoreGroupsMatchSummarize verifies live groups are recomputed from completed records.
func TestLiveStoreGroupsMatchSummarize(t *testing.T) {
	store := newLiveStore()
	records := []whatttft.RequestRecord{
		tuiTestRecord("req-1", "target-b", 30, 100, nil),
		tuiTestRecord("req-2", "target-a", 20, 90, nil),
	}
	for _, record := range records {
		store.applyEvent(whatttft.RunEvent{Kind: whatttft.EventRequestFinished, RequestID: record.RequestID, Record: &record})
	}

	got := store.Groups()
	want := whatttft.Summarize(records).Groups
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("groups = %#v, want %#v", got, want)
	}
}

// TestLiveStoreAggregateSummaryNotClobberedByEventSummary verifies a per-target event summary does
// not overwrite the cross-target aggregate the dashboard derives from all records. This guards the
// interleaved-run case where request_finished events carry no summary and the store must keep every
// target's group.
func TestLiveStoreAggregateSummaryNotClobberedByEventSummary(t *testing.T) {
	store := newLiveStore()
	records := []whatttft.RequestRecord{
		tuiTestRecord("req-1", "target-a", 20, 90, nil),
		tuiTestRecord("req-2", "target-b", 30, 100, nil),
	}
	for _, record := range records {
		store.applyEvent(whatttft.RunEvent{Kind: whatttft.EventRequestFinished, RequestID: record.RequestID, Record: &record})
	}

	// A later event carrying only target-a's summary must not shrink the aggregate to one group.
	singleTargetSummary := whatttft.Summarize(records[:1])
	store.applyEvent(whatttft.RunEvent{Kind: whatttft.EventSummaryUpdated, Summary: &singleTargetSummary})

	if got := len(store.Groups()); got != 2 {
		t.Fatalf("groups = %d, want 2 (aggregate over all records, not the per-target event summary)", got)
	}
}

// TestLiveStoreTargetOrderFromEvent verifies the dashboard reflects the benchmark's target order
// (e.g. interleaved) from the started event instead of always showing serial.
func TestLiveStoreTargetOrderFromEvent(t *testing.T) {
	interleaved := newLiveStore()
	interleaved.applyEvent(whatttft.RunEvent{Kind: whatttft.EventBenchmarkStarted, BenchmarkName: "bench", TargetOrder: whatttft.InterleavedTargetOrder})
	if interleaved.targetOrder != string(whatttft.InterleavedTargetOrder) {
		t.Fatalf("target order = %q, want interleaved", interleaved.targetOrder)
	}

	defaulted := newLiveStore()
	defaulted.applyEvent(whatttft.RunEvent{Kind: whatttft.EventBenchmarkStarted, BenchmarkName: "bench"})
	if defaulted.targetOrder != string(whatttft.SerialTargetOrder) {
		t.Fatalf("default target order = %q, want serial", defaulted.targetOrder)
	}
}

// TestLiveStoreMetricRows verifies core metric rows are calculated over successful measured records.
func TestLiveStoreMetricRows(t *testing.T) {
	store := newLiveStore()
	for _, record := range []whatttft.RequestRecord{
		tuiTestRecord("req-1", "target-a", 10, 100, nil),
		tuiTestRecord("req-2", "target-a", 20, 200, nil),
		tuiTestRecord("req-3", "target-a", 999, 999, &whatttft.ErrorRecord{Category: "provider"}),
	} {
		store.applyEvent(whatttft.RunEvent{Kind: whatttft.EventRequestFinished, RequestID: record.RequestID, Record: &record})
	}

	rows := store.MetricRows()
	if len(rows) == 0 || rows[3].Name != metricTTFTDeltaMS || rows[3].Count != 2 || rows[3].P50 == nil || *rows[3].P50 != 10 {
		t.Fatalf("metric rows = %#v, want ttft count 2 p50 10", rows)
	}
}

// TestLiveStoreSlowestRequests verifies slowest request rows include TTFT, E2E, and stream-total metrics.
func TestLiveStoreSlowestRequests(t *testing.T) {
	store := newLiveStore()
	for _, record := range []whatttft.RequestRecord{
		tuiTestRecord("req-1", "target-a", 10, 100, nil),
		tuiTestRecord("req-2", "target-a", 50, 200, nil),
	} {
		store.applyEvent(whatttft.RunEvent{Kind: whatttft.EventRequestFinished, RequestID: record.RequestID, Record: &record})
	}

	slowest := store.SlowestRequests(2)
	if len(slowest) != 2 || slowest[0].RequestID != "req-2" || slowest[0].MetricName != metricStreamTotalMS || slowest[0].ValueMS != 220 {
		t.Fatalf("slowest = %#v, want req-2 stream_total_ms 220 first", slowest)
	}
}

// TestLiveStoreStatusCounts verifies status-code and error-category counts are tracked for measured records.
func TestLiveStoreStatusCounts(t *testing.T) {
	store := newLiveStore()
	errorRecord := tuiTestRecord("req-1", "target-a", 10, 100, &whatttft.ErrorRecord{Category: "http_status"})
	errorRecord.HTTP.StatusCode = 429
	successRecord := tuiTestRecord("req-2", "target-a", 20, 200, nil)
	successRecord.HTTP.StatusCode = 200
	warmupRecord := tuiTestRecord("req-3", "target-a", 30, 300, &whatttft.ErrorRecord{Category: "warmup"})
	warmupRecord.Warmup = true
	for _, record := range []whatttft.RequestRecord{errorRecord, successRecord, warmupRecord} {
		store.applyEvent(whatttft.RunEvent{Kind: whatttft.EventRequestFinished, RequestID: record.RequestID, Record: &record})
	}

	counts := store.StatusCounts()
	if counts.ErrorCategories["http_status"] != 1 || counts.StatusCodes["429"] != 1 || counts.StatusCodes["200"] != 1 {
		t.Fatalf("status counts = %#v, want http_status=1 status 429/200=1", counts)
	}
	if _, ok := counts.ErrorCategories["warmup"]; ok {
		t.Fatalf("status counts included warmup error: %#v", counts)
	}
}

// TestLiveStoreBenchmarkTargetsTrackStatus verifies benchmark target rows transition through pending, running, and finished states.
func TestLiveStoreBenchmarkTargetsTrackStatus(t *testing.T) {
	store := newLiveStore()
	store.applyEvent(whatttft.RunEvent{Kind: whatttft.EventBenchmarkStarted, BenchmarkName: "bench", Targets: []whatttft.RunEventTarget{
		{TargetID: "target-a", TargetName: "Target A", Provider: "openai", Model: "gpt-a", TotalRequests: 1, MeasuredRequests: 1},
		{TargetID: "target-b", TargetName: "Target B", Provider: "openai", Model: "gpt-b", TotalRequests: 1, MeasuredRequests: 1},
	}, TotalRequests: 2, MeasuredRequests: 2})

	rows := store.TargetRows()
	if len(rows) != 2 || rows[0].Status != targetStatusPending || rows[1].Status != targetStatusPending {
		t.Fatalf("initial target rows = %#v, want two pending rows", rows)
	}
	store.applyEvent(whatttft.RunEvent{Kind: whatttft.EventTargetStarted, TargetID: "target-a", TargetName: "Target A", Provider: "openai", Model: "gpt-a", TotalRequests: 1, MeasuredRequests: 1})
	if progress := store.Progress(); progress.Total != 2 || progress.Measured != 2 {
		t.Fatalf("benchmark progress after target start = %#v, want global total/measured 2/2", progress)
	}
	rows = store.TargetRows()
	if rows[0].Status != targetStatusRunning || rows[1].Status != targetStatusPending {
		t.Fatalf("started target rows = %#v, want target-a running target-b pending", rows)
	}
	record := tuiTestRecord("target-a-req-000000", "target-a", 10, 100, nil)
	store.applyEvent(whatttft.RunEvent{Kind: whatttft.EventRequestFinished, TargetID: "target-a", RequestID: record.RequestID, CompletedRequests: 1, SuccessfulRequests: 1, Record: &record})
	store.applyEvent(whatttft.RunEvent{Kind: whatttft.EventTargetFinished, TargetID: "target-a", TargetName: "Target A"})
	rows = store.TargetRows()
	if rows[0].Status != targetStatusFinished || rows[0].Completed != 1 || rows[0].Successful != 1 {
		t.Fatalf("finished target row = %#v, want target-a finished completed/successful 1", rows[0])
	}
}

// TestLiveStoreTargetVisibilityFiltersComparisonSeries verifies hidden benchmark targets are omitted from comparison chart series without changing stable style indexes.
func TestLiveStoreTargetVisibilityFiltersComparisonSeries(t *testing.T) {
	store := newLiveStore()
	store.applyEvent(whatttft.RunEvent{Kind: whatttft.EventBenchmarkStarted, BenchmarkName: "bench", Targets: []whatttft.RunEventTarget{
		{TargetID: "target-a", Model: "gpt-a"},
		{TargetID: "target-b", Model: "gpt-b"},
	}})
	for _, record := range []whatttft.RequestRecord{
		tuiTestRecord("target-a-req-000000", "target-a", 10, 100, nil),
		tuiTestRecord("target-b-req-000000", "target-b", 90, 200, nil),
	} {
		store.applyEvent(whatttft.RunEvent{Kind: whatttft.EventRequestFinished, TargetID: record.TargetID, RequestID: record.RequestID, Record: &record})
	}

	store.selectTarget(1)
	store.toggleSelectedTargetVisibility()
	if store.targetVisible("target-b") || store.visibleTargetCount() != 1 {
		t.Fatalf("target visibility after hide = b:%t count:%d, want hidden/count 1", store.targetVisible("target-b"), store.visibleTargetCount())
	}
	series := benchMetricSeries(store, metricTTFTDeltaMS)
	if len(series) != 1 || series[0].Label != "gpt-a" || series[0].StyleIndex != 0 {
		t.Fatalf("filtered series = %#v, want only gpt-a with stable style index 0", series)
	}

	store.selectTarget(-1)
	store.toggleSelectedTargetVisibility()
	if !store.targetVisible("target-a") || store.visibleTargetCount() != 1 {
		t.Fatalf("last visible target was hidden: a:%t count:%d", store.targetVisible("target-a"), store.visibleTargetCount())
	}
	store.showAllTargets()
	if !store.targetVisible("target-a") || !store.targetVisible("target-b") || store.visibleTargetCount() != 2 {
		t.Fatalf("show all visibility = a:%t b:%t count:%d, want all visible", store.targetVisible("target-a"), store.targetVisible("target-b"), store.visibleTargetCount())
	}
}

// TestLiveStoreSelectedTargetStoreFiltersRecords verifies selected target detail charts use only that target's records.
func TestLiveStoreSelectedTargetStoreFiltersRecords(t *testing.T) {
	store := newLiveStore()
	store.applyEvent(whatttft.RunEvent{Kind: whatttft.EventBenchmarkStarted, BenchmarkName: "bench", Targets: []whatttft.RunEventTarget{{TargetID: "target-a"}, {TargetID: "target-b"}}})
	for _, record := range []whatttft.RequestRecord{
		tuiTestRecord("target-a-req-000000", "target-a", 10, 100, nil),
		tuiTestRecord("target-b-req-000000", "target-b", 90, 200, nil),
	} {
		store.applyEvent(whatttft.RunEvent{Kind: whatttft.EventRequestFinished, TargetID: record.TargetID, RequestID: record.RequestID, Record: &record})
	}
	store.selectTarget(1)
	selected := store.selectedTargetStore()
	values := selected.RunSeries(metricTTFTDeltaMS)
	if len(values) != 1 || values[0] != 90 {
		t.Fatalf("selected target TTFT values = %#v, want only target-b value 90", values)
	}
}

// TestBenchSeriesLabelsDisambiguateDuplicateModels verifies legends stay readable when targets share a model ID.
func TestBenchSeriesLabelsDisambiguateDuplicateModels(t *testing.T) {
	labels := benchSeriesLabels([]targetRow{
		{ID: "gpt-5.5-priority", Name: "GPT-5.5 Priority", Model: "gpt-5.5", RequestedServiceTier: "priority"},
		{ID: "gpt-5.5-default", Name: "GPT-5.5 Default", Model: "gpt-5.5", RequestedServiceTier: "default"},
	})
	if labels["gpt-5.5-priority"] != "GPT-5.5 Priority" || labels["gpt-5.5-default"] != "GPT-5.5 Default" {
		t.Fatalf("duplicate model legend labels = %#v, want target names", labels)
	}
}

// TestLiveStoreCurrentTargetFallbacks verifies target labels degrade gracefully when IDs or names are missing.
func TestLiveStoreCurrentTargetFallbacks(t *testing.T) {
	store := newLiveStore()
	if got := store.currentTarget(); got != "-" {
		t.Fatalf("empty current target = %q, want -", got)
	}
	store.applyEvent(whatttft.RunEvent{Kind: whatttft.EventTargetStarted, TargetID: "target-a"})
	if got := store.currentTarget(); got != "target-a" {
		t.Fatalf("id current target = %q, want target-a", got)
	}
	store.applyEvent(whatttft.RunEvent{Kind: whatttft.EventTargetStarted, TargetName: "Target A"})
	if got := store.currentTarget(); got != "target-a (Target A)" {
		t.Fatalf("named current target = %q, want target-a (Target A)", got)
	}
}

func tuiTestRecord(requestID string, targetID string, ttftMS float64, e2eMS float64, err *whatttft.ErrorRecord) whatttft.RequestRecord {
	completionTokens := 4
	streamTotalMS := e2eMS + 20
	return whatttft.RequestRecord{
		RequestID:        requestID,
		TargetID:         targetID,
		Provider:         "openai",
		Model:            "gpt-test",
		ScenarioName:     "short",
		CacheMode:        whatttft.CacheReuse,
		ConnectionMode:   whatttft.WarmConnections,
		PromptHash:       "hash-" + requestID,
		CompletionTokens: &completionTokens,
		OutputDeltaCount: 3,
		Timeline: whatttft.Timeline{BaseWallUnixNano: 1_000_000_000, EventsNS: map[whatttft.EventName]int64{
			whatttft.EventRequestStart:      0,
			whatttft.EventFirstOutputDelta:  int64(ttftMS * 1_000_000),
			whatttft.EventLastOutputDelta:   int64(e2eMS * 1_000_000),
			whatttft.EventBodyEOF:           int64(streamTotalMS * 1_000_000),
			whatttft.EventFirstResponseByte: 5_000_000,
			whatttft.EventWroteRequest:      1_000_000,
		}},
		Derived: whatttft.DerivedMetrics{
			HTTPTTFBMS:              tuiFloatPointer(5),
			TTFTDeltaMS:             &ttftMS,
			E2EDeltaMS:              &e2eMS,
			StreamTotalMS:           &streamTotalMS,
			ServerWaitToFirstByteMS: tuiFloatPointer(4),
			E2EOutputTPS:            tuiFloatPointer(40),
		},
		Error: err,
	}
}

func tuiFloatPointer(value float64) *float64 {
	return &value
}
