package whatttft

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
)

// TestRunBenchmarkExecutesTargetsSerially verifies two targets produce combined records, chunks, and grouped summaries.
func TestRunBenchmarkExecutesTargetsSerially(t *testing.T) {
	providerA := &benchmarkModelProvider{fakeProvider: &fakeProvider{completionTokens: 2}, model: "model-a"}
	providerB := &benchmarkModelProvider{fakeProvider: &fakeProvider{completionTokens: 3}, model: "model-b"}

	result, err := RunBenchmark(context.Background(), BenchmarkConfig{
		Name: "two-targets",
		Targets: []BenchmarkTarget{
			{
				ID:       "target-a",
				Name:     "Target A",
				Provider: providerA,
				Config: RunConfig{
					Scenario:         Scenario{Name: "short", Prompt: "Say hello."},
					WarmupRequests:   1,
					MeasuredRequests: 2,
					CacheMode:        CacheReuse,
					ConnectionMode:   WarmConnections,
					SaveChunks:       true,
				},
			},
			{
				ID:       "target-b",
				Name:     "Target B",
				Provider: providerB,
				Config: RunConfig{
					Scenario:         Scenario{Name: "short", Prompt: "Say hello."},
					WarmupRequests:   1,
					MeasuredRequests: 2,
					CacheMode:        CacheReuse,
					ConnectionMode:   WarmConnections,
					SaveChunks:       true,
				},
			},
		},
	})
	if err != nil {
		t.Fatalf("run benchmark: %v", err)
	}
	if result.TargetOrder != SerialTargetOrder {
		t.Fatalf("target order = %q, want serial", result.TargetOrder)
	}
	if len(result.Records) != 6 {
		t.Fatalf("records = %d, want 6", len(result.Records))
	}
	if result.Summary.TotalRequests != 6 || result.Summary.WarmupRequests != 2 || result.Summary.MeasuredRequests != 4 {
		t.Fatalf("summary counts = total %d warmup %d measured %d", result.Summary.TotalRequests, result.Summary.WarmupRequests, result.Summary.MeasuredRequests)
	}
	if result.Summary.SuccessfulRequests != 4 || result.Summary.ErrorRequests != 0 {
		t.Fatalf("success/error = %d/%d, want 4/0", result.Summary.SuccessfulRequests, result.Summary.ErrorRequests)
	}
	if len(result.Summary.Groups) != 2 {
		t.Fatalf("summary groups = %d, want 2", len(result.Summary.Groups))
	}

	groups := benchmarkGroupsByTarget(result.Summary.Groups)
	for _, targetID := range []string{"target-a", "target-b"} {
		group := groups[targetID]
		if group == nil {
			t.Fatalf("missing summary group for %s", targetID)
		}
		if group.MeasuredRequests != 2 || group.SuccessfulRequests != 2 {
			t.Fatalf("group %s measured/success = %d/%d, want 2/2", targetID, group.MeasuredRequests, group.SuccessfulRequests)
		}
	}
	if groups["target-a"].Model != "model-a" || groups["target-b"].Model != "model-b" {
		t.Fatalf("group models = %q/%q, want model-a/model-b", groups["target-a"].Model, groups["target-b"].Model)
	}

	seenRequestIDs := make(map[string]bool, len(result.Records))
	for index, record := range result.Records {
		if seenRequestIDs[record.RequestID] {
			t.Fatalf("duplicate request ID %q", record.RequestID)
		}
		seenRequestIDs[record.RequestID] = true

		wantTarget := "target-a"
		wantModel := "model-a"
		if index >= 3 {
			wantTarget = "target-b"
			wantModel = "model-b"
		}
		if record.TargetID != wantTarget {
			t.Fatalf("record %d target ID = %q, want %q", index, record.TargetID, wantTarget)
		}
		if record.Model != wantModel {
			t.Fatalf("record %d model = %q, want %q", index, record.Model, wantModel)
		}
		wantRequestID := fmt.Sprintf("%s-req-%06d", wantTarget, index%3)
		if record.RequestID != wantRequestID {
			t.Fatalf("record %d request ID = %q, want %q", index, record.RequestID, wantRequestID)
		}
		if record.Warmup != (index%3 == 0) {
			t.Fatalf("record %d warmup = %t, want first request per target warm", index, record.Warmup)
		}
	}
	if len(result.Chunks) == 0 {
		t.Fatal("expected chunks from both targets")
	}
	for _, chunk := range result.Chunks {
		if !seenRequestIDs[chunk.RequestID] {
			t.Fatalf("chunk request ID %q does not join to any request", chunk.RequestID)
		}
	}

	if len(providerA.callsSnapshot()) != 3 || len(providerB.callsSnapshot()) != 3 {
		t.Fatalf("provider calls = %d/%d, want 3/3", len(providerA.callsSnapshot()), len(providerB.callsSnapshot()))
	}
}

// TestRunBenchmarkWithOptionsEmitsBenchmarkAndTargetEvents verifies multi-target event ordering and target identity propagation.
func TestRunBenchmarkWithOptionsEmitsBenchmarkAndTargetEvents(t *testing.T) {
	recorder := &eventRecorder{}
	providerA := &benchmarkModelProvider{fakeProvider: &fakeProvider{completionTokens: 1}, model: "model-a"}
	providerB := &benchmarkModelProvider{fakeProvider: &fakeProvider{completionTokens: 1}, model: "model-b"}

	result, err := RunBenchmarkWithOptions(context.Background(), BenchmarkConfig{
		Name: "event-benchmark",
		Targets: []BenchmarkTarget{
			{ID: "target-a", Name: "Target A", Provider: providerA, Config: benchmarkMeasuredConfig()},
			{ID: "target-b", Name: "Target B", Provider: providerB, Config: benchmarkMeasuredConfig()},
		},
	}, BenchmarkOptions{Observer: recorder})
	if err != nil {
		t.Fatalf("run benchmark: %v", err)
	}
	if len(result.Records) != 2 {
		t.Fatalf("records = %d, want 2", len(result.Records))
	}

	events := recorder.snapshot()
	if len(events) == 0 || events[0].Kind != EventBenchmarkStarted {
		t.Fatalf("first event = %#v, want benchmark_started", events)
	}
	if countEvents(events, EventBenchmarkFinished) != 1 {
		t.Fatalf("benchmark_finished count = %d, want 1", countEvents(events, EventBenchmarkFinished))
	}
	if countEvents(events, EventTargetStarted) != 2 || countEvents(events, EventTargetFinished) != 2 {
		t.Fatalf("target event counts start/finish = %d/%d", countEvents(events, EventTargetStarted), countEvents(events, EventTargetFinished))
	}
	requestEvents := eventsByKind(events, EventRequestFinished)
	if len(requestEvents) != 2 {
		t.Fatalf("request_finished events = %d, want 2", len(requestEvents))
	}
	seenTargets := map[string]string{}
	for _, event := range requestEvents {
		if event.TargetID == "" || event.Record == nil {
			t.Fatalf("request event missing target or record: %#v", event)
		}
		seenTargets[event.TargetID] = event.Record.RequestID
		if event.Record.TargetID != event.TargetID {
			t.Fatalf("record target %q does not match event target %q", event.Record.TargetID, event.TargetID)
		}
	}
	if seenTargets["target-a"] != "target-a-req-000000" || seenTargets["target-b"] != "target-b-req-000000" {
		t.Fatalf("request events by target = %#v, want prefixed request IDs", seenTargets)
	}
	finished := eventsByKind(events, EventBenchmarkFinished)[0]
	if finished.BenchmarkName != "event-benchmark" || finished.CompletedRequests != 2 || finished.Summary == nil || finished.Summary.SuccessfulRequests != 2 {
		t.Fatalf("benchmark_finished = %#v", finished)
	}
	for index, event := range events {
		if event.Sequence == 0 {
			t.Fatalf("event %d has zero sequence: %#v", index, event)
		}
		if event.WallUnixNano == 0 {
			t.Fatalf("event %d has zero wall time: %#v", index, event)
		}
	}
}

// TestRunBenchmarkWithOptionsEmitsCanceledEvent verifies benchmark cancellation is represented in events with partial summary data.
func TestRunBenchmarkWithOptionsEmitsCanceledEvent(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	recorder := &eventRecorder{}
	providerA := &benchmarkModelProvider{fakeProvider: &fakeProvider{completionTokens: 1, afterCall: cancel}, model: "model-a"}
	providerB := &benchmarkModelProvider{fakeProvider: &fakeProvider{completionTokens: 1}, model: "model-b"}

	result, err := RunBenchmarkWithOptions(ctx, BenchmarkConfig{Targets: []BenchmarkTarget{
		{ID: "target-a", Provider: providerA, Config: RunConfig{Scenario: Scenario{Prompt: "hello"}, MeasuredRequests: 5, CacheMode: CacheReuse}},
		{ID: "target-b", Provider: providerB, Config: benchmarkMeasuredConfig()},
	}}, BenchmarkOptions{Observer: recorder})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("run error = %v, want context.Canceled", err)
	}
	if result == nil || len(result.Records) != 1 {
		t.Fatalf("partial result = %#v, want one record", result)
	}

	events := recorder.snapshot()
	if countEvents(events, EventBenchmarkCanceled) != 1 {
		t.Fatalf("benchmark_canceled count = %d, want 1 in %#v", countEvents(events, EventBenchmarkCanceled), eventKinds(events))
	}
	if countEvents(events, EventBenchmarkFinished) != 0 {
		t.Fatalf("benchmark_finished count = %d, want 0", countEvents(events, EventBenchmarkFinished))
	}
	canceled := eventsByKind(events, EventBenchmarkCanceled)[0]
	if canceled.Error == nil || canceled.Error.Category != "context" {
		t.Fatalf("canceled error = %#v, want context", canceled.Error)
	}
	if canceled.CompletedRequests != 1 || canceled.Summary == nil || canceled.Summary.MeasuredRequests != 1 {
		t.Fatalf("canceled event = completed %d summary %#v", canceled.CompletedRequests, canceled.Summary)
	}
}

// TestRunBenchmarkGroupsSameModelByTargetID verifies target IDs split otherwise identical provider/model summaries.
func TestRunBenchmarkGroupsSameModelByTargetID(t *testing.T) {
	providerA := &benchmarkModelProvider{fakeProvider: &fakeProvider{completionTokens: 1}, model: "same-model"}
	providerB := &benchmarkModelProvider{fakeProvider: &fakeProvider{completionTokens: 1}, model: "same-model"}

	result, err := NewBenchmarkRunner(BenchmarkConfig{Targets: []BenchmarkTarget{
		{ID: "region-a", Provider: providerA, Config: benchmarkMeasuredConfig()},
		{ID: "region-b", Provider: providerB, Config: benchmarkMeasuredConfig()},
	}}).Run(context.Background())
	if err != nil {
		t.Fatalf("run benchmark: %v", err)
	}
	if len(result.Summary.Groups) != 2 {
		t.Fatalf("summary groups = %d, want 2", len(result.Summary.Groups))
	}
	groups := benchmarkGroupsByTarget(result.Summary.Groups)
	if groups["region-a"] == nil || groups["region-b"] == nil {
		t.Fatalf("groups by target = %#v, want region-a and region-b", groups)
	}
}

// TestRunBenchmarkContinuesAfterTargetRequestErrors verifies request-level failures do not abort later targets.
func TestRunBenchmarkContinuesAfterTargetRequestErrors(t *testing.T) {
	providerA := &benchmarkModelProvider{
		fakeProvider: &fakeProvider{
			completionTokens: 1,
			errorsByAttempt:  map[int]error{0: errors.New("synthetic target failure")},
		},
		model: "model-a",
	}
	providerB := &benchmarkModelProvider{fakeProvider: &fakeProvider{completionTokens: 1}, model: "model-b"}

	result, err := RunBenchmark(context.Background(), BenchmarkConfig{Targets: []BenchmarkTarget{
		{ID: "target-a", Provider: providerA, Config: benchmarkMeasuredConfig()},
		{ID: "target-b", Provider: providerB, Config: benchmarkMeasuredConfig()},
	}})
	if err != nil {
		t.Fatalf("run benchmark: %v", err)
	}
	if len(result.Records) != 2 {
		t.Fatalf("records = %d, want 2", len(result.Records))
	}
	if result.Records[0].Error == nil {
		t.Fatal("first target record should contain a request error")
	}
	if result.Records[1].Error != nil {
		t.Fatalf("second target record error = %#v, want nil", result.Records[1].Error)
	}
	if result.Summary.SuccessfulRequests != 1 || result.Summary.ErrorRequests != 1 {
		t.Fatalf("success/error = %d/%d, want 1/1", result.Summary.SuccessfulRequests, result.Summary.ErrorRequests)
	}
	if len(providerB.callsSnapshot()) != 1 {
		t.Fatalf("second target calls = %d, want 1", len(providerB.callsSnapshot()))
	}
}

// TestRunBenchmarkReturnsPartialResultOnCancellation verifies context cancellation aborts later targets and returns partial records.
func TestRunBenchmarkReturnsPartialResultOnCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	providerA := &benchmarkModelProvider{fakeProvider: &fakeProvider{completionTokens: 1, afterCall: cancel}, model: "model-a"}
	providerB := &benchmarkModelProvider{fakeProvider: &fakeProvider{completionTokens: 1}, model: "model-b"}

	result, err := RunBenchmark(ctx, BenchmarkConfig{Targets: []BenchmarkTarget{
		{ID: "target-a", Provider: providerA, Config: RunConfig{Scenario: Scenario{Prompt: "hello"}, MeasuredRequests: 5, CacheMode: CacheReuse}},
		{ID: "target-b", Provider: providerB, Config: benchmarkMeasuredConfig()},
	}})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("run error = %v, want context.Canceled", err)
	}
	if result == nil {
		t.Fatal("expected partial result")
	}
	if len(result.Records) != 1 {
		t.Fatalf("records = %d, want one partial record", len(result.Records))
	}
	if result.Summary.MeasuredRequests != 1 {
		t.Fatalf("summary measured = %d, want 1", result.Summary.MeasuredRequests)
	}
	if len(providerB.callsSnapshot()) != 0 {
		t.Fatalf("second target calls = %d, want 0 after cancellation", len(providerB.callsSnapshot()))
	}
}

// TestRunBenchmarkPreflightValidationPreventsRequests verifies every target is validated before any provider call starts.
func TestRunBenchmarkPreflightValidationPreventsRequests(t *testing.T) {
	tests := []struct {
		name    string
		targets []BenchmarkTarget
		wantErr string
	}{
		{
			name: "duplicate sanitized IDs",
			targets: []BenchmarkTarget{
				{ID: "Target A", Provider: &benchmarkModelProvider{fakeProvider: &fakeProvider{}, model: "model-a"}, Config: benchmarkMeasuredConfig()},
				{ID: "target-a", Provider: &benchmarkModelProvider{fakeProvider: &fakeProvider{}, model: "model-b"}, Config: benchmarkMeasuredConfig()},
			},
			wantErr: "duplicates targets[0].id",
		},
		{
			name: "nil provider",
			targets: []BenchmarkTarget{
				{ID: "target-a", Provider: &benchmarkModelProvider{fakeProvider: &fakeProvider{}, model: "model-a"}, Config: benchmarkMeasuredConfig()},
				{ID: "target-b", Provider: nil, Config: benchmarkMeasuredConfig()},
			},
			wantErr: "targets[1].provider is required",
		},
		{
			name: "invalid config",
			targets: []BenchmarkTarget{
				{ID: "target-a", Provider: &benchmarkModelProvider{fakeProvider: &fakeProvider{}, model: "model-a"}, Config: benchmarkMeasuredConfig()},
				{ID: "target-b", Provider: &benchmarkModelProvider{fakeProvider: &fakeProvider{}, model: "model-b"}, Config: RunConfig{}},
			},
			wantErr: "targets[1].config",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			result, err := RunBenchmark(context.Background(), BenchmarkConfig{Targets: test.targets})
			if err == nil {
				t.Fatal("expected preflight error")
			}
			if result != nil {
				t.Fatalf("result = %#v, want nil", result)
			}
			if got := err.Error(); !strings.Contains(got, test.wantErr) {
				t.Fatalf("error = %q, want substring %q", got, test.wantErr)
			}
			for index, target := range test.targets {
				provider, ok := target.Provider.(*benchmarkModelProvider)
				if !ok || provider == nil {
					continue
				}
				if calls := len(provider.callsSnapshot()); calls != 0 {
					t.Fatalf("target %d calls = %d, want 0 before preflight success", index, calls)
				}
			}
		})
	}
}

// TestRunBenchmarkRequiresTargets verifies an empty benchmark config fails validation.
func TestRunBenchmarkRequiresTargets(t *testing.T) {
	result, err := RunBenchmark(context.Background(), BenchmarkConfig{})
	if err == nil {
		t.Fatal("expected validation error")
	}
	if result != nil {
		t.Fatalf("result = %#v, want nil", result)
	}
}

// TestBenchmarkResultRunResultReturnsCopy verifies report-writer conversion does not alias slices.
func TestBenchmarkResultRunResultReturnsCopy(t *testing.T) {
	benchmarkResult := &BenchmarkResult{
		Records:     []RequestRecord{{RequestID: "req-1"}},
		Chunks:      []ChunkRecord{{RequestID: "req-1"}},
		Summary:     RunSummary{TotalRequests: 1},
		TargetOrder: SerialTargetOrder,
	}

	runResult := benchmarkResult.RunResult()
	if runResult == nil {
		t.Fatal("run result is nil")
	}
	runResult.Records[0].RequestID = "mutated"
	runResult.Chunks[0].RequestID = "mutated"
	if benchmarkResult.Records[0].RequestID != "req-1" || benchmarkResult.Chunks[0].RequestID != "req-1" {
		t.Fatalf("RunResult aliased benchmark slices: %#v", benchmarkResult)
	}
}

func benchmarkMeasuredConfig() RunConfig {
	return RunConfig{
		Scenario:         Scenario{Name: "short", Prompt: "Say hello."},
		MeasuredRequests: 1,
		CacheMode:        CacheReuse,
		ConnectionMode:   WarmConnections,
	}
}

func benchmarkGroupsByTarget(groups []SummaryGroup) map[string]*SummaryGroup {
	byTarget := make(map[string]*SummaryGroup, len(groups))
	for index := range groups {
		group := &groups[index]
		byTarget[group.TargetID] = group
	}

	return byTarget
}

type benchmarkModelProvider struct {
	*fakeProvider
	model string
}

func (p *benchmarkModelProvider) Name() string {
	return "fake"
}

func (p *benchmarkModelProvider) Model() string {
	return p.model
}
