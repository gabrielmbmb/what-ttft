package whatttft

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
)

// TestRunnerRunExecutesWarmupThenMeasuredSequentially verifies warmups are preserved but excluded from summary successes.
func TestRunnerRunExecutesWarmupThenMeasuredSequentially(t *testing.T) {
	provider := &fakeProvider{completionTokens: 2}
	runner := NewRunner(provider, RunConfig{
		Scenario:         Scenario{Name: "short", Prompt: "Say hello."},
		WarmupRequests:   2,
		MeasuredRequests: 3,
		CacheMode:        CacheReuse,
		ConnectionMode:   WarmConnections,
		SaveChunks:       true,
	})

	result, err := runner.Run(context.Background())
	if err != nil {
		t.Fatalf("run: %v", err)
	}

	if len(result.Records) != 5 {
		t.Fatalf("record count = %d, want 5", len(result.Records))
	}
	if result.Summary.TotalRequests != 5 {
		t.Fatalf("summary total = %d, want 5", result.Summary.TotalRequests)
	}
	if result.Summary.WarmupRequests != 2 {
		t.Fatalf("summary warmup = %d, want 2", result.Summary.WarmupRequests)
	}
	if result.Summary.MeasuredRequests != 3 {
		t.Fatalf("summary measured = %d, want 3", result.Summary.MeasuredRequests)
	}
	if result.Summary.SuccessfulRequests != 3 {
		t.Fatalf("summary successful = %d, want measured-only 3", result.Summary.SuccessfulRequests)
	}
	if result.Summary.ErrorRequests != 0 {
		t.Fatalf("summary errors = %d, want 0", result.Summary.ErrorRequests)
	}

	for index, record := range result.Records {
		if record.Attempt != index {
			t.Fatalf("record %d attempt = %d, want %d", index, record.Attempt, index)
		}
		if record.RequestID != fmt.Sprintf("req-%06d", index) {
			t.Fatalf("record %d request ID = %q", index, record.RequestID)
		}
		if record.Provider != "fake" {
			t.Fatalf("record %d provider = %q, want fake", index, record.Provider)
		}
		if record.Model != "fake-model" {
			t.Fatalf("record %d model = %q, want fake-model", index, record.Model)
		}
		if record.ScenarioName != "short" {
			t.Fatalf("record %d scenario = %q, want short", index, record.ScenarioName)
		}
		if record.Warmup != (index < 2) {
			t.Fatalf("record %d warmup = %t", index, record.Warmup)
		}
		if record.PromptHash == "" {
			t.Fatalf("record %d prompt hash is empty", index)
		}
		if record.CompletionTokens == nil || *record.CompletionTokens != 2 {
			t.Fatalf("record %d completion tokens = %v, want 2", index, record.CompletionTokens)
		}
		if record.Derived.TTFTDeltaMS == nil {
			t.Fatalf("record %d missing ttft_delta_ms", index)
		}
	}

	if len(result.Chunks) != 10 {
		t.Fatalf("chunk count = %d, want output and usage chunks for each request", len(result.Chunks))
	}
	calls := provider.callsSnapshot()
	if len(calls) != 5 {
		t.Fatalf("provider calls = %d, want 5", len(calls))
	}
	for index, call := range calls {
		if call.warmup != (index < 2) {
			t.Fatalf("call %d warmup = %t", index, call.warmup)
		}
	}
}

// TestRunnerRunAppliesTargetLabelsAndRequestIDPrefix verifies multi-target metadata is copied to records and chunks.
func TestRunnerRunAppliesTargetLabelsAndRequestIDPrefix(t *testing.T) {
	provider := &fakeProvider{completionTokens: 1}
	runner := NewRunner(provider, RunConfig{
		Scenario:         Scenario{Name: "targeted", Prompt: "Say hello."},
		MeasuredRequests: 1,
		CacheMode:        CacheReuse,
		TargetID:         "target-a",
		TargetName:       "Target A",
		RequestIDPrefix:  "target-a-",
		SaveChunks:       true,
	})

	result, err := runner.Run(context.Background())
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if len(result.Records) != 1 {
		t.Fatalf("record count = %d, want 1", len(result.Records))
	}
	record := result.Records[0]
	if record.RequestID != "target-a-req-000000" {
		t.Fatalf("request ID = %q, want prefixed ID", record.RequestID)
	}
	if record.TargetID != "target-a" || record.TargetName != "Target A" {
		t.Fatalf("target = id %q name %q, want target-a/Target A", record.TargetID, record.TargetName)
	}
	if len(result.Chunks) == 0 {
		t.Fatal("expected chunks with save chunks enabled")
	}
	for _, chunk := range result.Chunks {
		if chunk.RequestID != record.RequestID {
			t.Fatalf("chunk request ID = %q, want %q", chunk.RequestID, record.RequestID)
		}
	}
	calls := provider.callsSnapshot()
	if len(calls) != 1 || calls[0].requestID != record.RequestID {
		t.Fatalf("provider calls = %#v, want prefixed request ID", calls)
	}
	if len(result.Summary.Groups) != 1 || result.Summary.Groups[0].TargetID != "target-a" || result.Summary.Groups[0].TargetName != "Target A" {
		t.Fatalf("summary groups = %#v, want target metadata", result.Summary.Groups)
	}
}

// TestRunnerWithOptionsEmitsSequentialLifecycleEvents verifies sequential runs emit ordered live events with phase and request metadata.
func TestRunnerWithOptionsEmitsSequentialLifecycleEvents(t *testing.T) {
	recorder := &eventRecorder{}
	provider := &fakeProvider{completionTokens: 2}
	runner := NewRunnerWithOptions(provider, RunConfig{
		Scenario:         Scenario{Name: "events", Prompt: "Say hello."},
		WarmupRequests:   1,
		MeasuredRequests: 1,
		CacheMode:        CacheReuse,
		ConnectionMode:   WarmConnections,
	}, RunnerOptions{Observer: recorder})

	result, err := runner.Run(context.Background())
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if len(result.Records) != 2 {
		t.Fatalf("records = %d, want 2", len(result.Records))
	}

	events := recorder.snapshot()
	wantKinds := []RunEventKind{
		EventRunStarted,
		EventPhaseStarted,
		EventRequestScheduled,
		EventRequestDispatched,
		EventRequestFinished,
		EventSummaryUpdated,
		EventPhaseFinished,
		EventPhaseStarted,
		EventRequestScheduled,
		EventRequestDispatched,
		EventRequestFinished,
		EventSummaryUpdated,
		EventPhaseFinished,
		EventSummaryUpdated,
		EventRunFinished,
	}
	assertEventKinds(t, events, wantKinds)
	assertSequentialEventSequence(t, events)

	if events[0].Provider != "fake" || events[0].Model != "fake-model" || events[0].ScenarioName != "events" {
		t.Fatalf("run_started context = provider %q model %q scenario %q", events[0].Provider, events[0].Model, events[0].ScenarioName)
	}
	if events[1].Phase != PhaseWarmup || events[1].Warmup == nil || !*events[1].Warmup {
		t.Fatalf("warmup phase event = phase %q warmup %v", events[1].Phase, events[1].Warmup)
	}
	if events[7].Phase != PhaseMeasured || events[7].Warmup == nil || *events[7].Warmup {
		t.Fatalf("measured phase event = phase %q warmup %v", events[7].Phase, events[7].Warmup)
	}
	warmRequest := events[4]
	if warmRequest.Attempt == nil || *warmRequest.Attempt != 0 || warmRequest.Warmup == nil || !*warmRequest.Warmup {
		t.Fatalf("warm request event attempt/warmup = %v/%v", warmRequest.Attempt, warmRequest.Warmup)
	}
	if warmRequest.Record == nil || !warmRequest.Record.Warmup || warmRequest.Record.RequestID != "req-000000" {
		t.Fatalf("warm request record = %#v", warmRequest.Record)
	}
	measuredRequest := events[10]
	if measuredRequest.Attempt == nil || *measuredRequest.Attempt != 1 || measuredRequest.Warmup == nil || *measuredRequest.Warmup {
		t.Fatalf("measured request event attempt/warmup = %v/%v", measuredRequest.Attempt, measuredRequest.Warmup)
	}
	if measuredRequest.Record == nil || measuredRequest.Record.Warmup || measuredRequest.Record.RequestID != "req-000001" {
		t.Fatalf("measured request record = %#v", measuredRequest.Record)
	}
	if measuredRequest.SuccessfulRequests != 1 || measuredRequest.ErrorRequests != 0 {
		t.Fatalf("measured request counts = success %d errors %d", measuredRequest.SuccessfulRequests, measuredRequest.ErrorRequests)
	}
	finished := events[len(events)-1]
	if finished.CompletedRequests != 2 || finished.Summary == nil || finished.Summary.SuccessfulRequests != 1 {
		t.Fatalf("run_finished = completed %d summary %#v", finished.CompletedRequests, finished.Summary)
	}
}

// TestRunnerWithOptionsDoesNotPromoteRequestErrorsToRunFailures verifies provider errors remain request events.
func TestRunnerWithOptionsDoesNotPromoteRequestErrorsToRunFailures(t *testing.T) {
	recorder := &eventRecorder{}
	provider := &fakeProvider{completionTokens: 1, errorsByAttempt: map[int]error{0: errors.New("synthetic provider failure")}}
	runner := NewRunnerWithOptions(provider, RunConfig{
		Scenario:         Scenario{Prompt: "Say hello."},
		MeasuredRequests: 1,
		CacheMode:        CacheReuse,
	}, RunnerOptions{Observer: recorder})

	result, err := runner.Run(context.Background())
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if result.Summary.ErrorRequests != 1 {
		t.Fatalf("summary errors = %d, want 1", result.Summary.ErrorRequests)
	}

	events := recorder.snapshot()
	if countEvents(events, EventRunFailed) != 0 {
		t.Fatalf("events contain run_failed: %#v", eventKinds(events))
	}
	if countEvents(events, EventRunFinished) != 1 {
		t.Fatalf("run_finished count = %d, want 1", countEvents(events, EventRunFinished))
	}
	requestEvents := eventsByKind(events, EventRequestFinished)
	if len(requestEvents) != 1 {
		t.Fatalf("request_finished events = %d, want 1", len(requestEvents))
	}
	if requestEvents[0].Record == nil || requestEvents[0].Record.Error == nil {
		t.Fatalf("request event record error = %#v", requestEvents[0].Record)
	}
}

// TestRunnerWithOptionsEmitsCancellationEvent verifies context cancellation is represented as a run_canceled event.
func TestRunnerWithOptionsEmitsCancellationEvent(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	recorder := &eventRecorder{}
	provider := &fakeProvider{completionTokens: 1, afterCall: cancel}
	runner := NewRunnerWithOptions(provider, RunConfig{
		Scenario:         Scenario{Prompt: "Say hello."},
		MeasuredRequests: 5,
		CacheMode:        CacheReuse,
	}, RunnerOptions{Observer: recorder})

	result, err := runner.Run(ctx)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("run error = %v, want context.Canceled", err)
	}
	if result == nil || len(result.Records) != 1 {
		t.Fatalf("partial result = %#v, want one record", result)
	}

	events := recorder.snapshot()
	if countEvents(events, EventRunCanceled) != 1 {
		t.Fatalf("run_canceled count = %d, want 1 in %#v", countEvents(events, EventRunCanceled), eventKinds(events))
	}
	if countEvents(events, EventRunFinished) != 0 {
		t.Fatalf("run_finished count = %d, want 0", countEvents(events, EventRunFinished))
	}
	canceled := eventsByKind(events, EventRunCanceled)[0]
	if canceled.Error == nil || canceled.Error.Category != "context" {
		t.Fatalf("canceled error = %#v, want context category", canceled.Error)
	}
	if canceled.CompletedRequests != 1 || canceled.Summary == nil || canceled.Summary.MeasuredRequests != 1 {
		t.Fatalf("canceled event = completed %d summary %#v", canceled.CompletedRequests, canceled.Summary)
	}
}

// TestRunnerRunContinuesAfterRequestErrors verifies provider errors become records and later attempts still run.
func TestRunnerRunContinuesAfterRequestErrors(t *testing.T) {
	provider := &fakeProvider{
		completionTokens: 1,
		errorsByAttempt: map[int]error{
			1: errors.New("synthetic provider failure"),
		},
	}
	runner := NewRunner(provider, RunConfig{
		Scenario:         Scenario{Prompt: "Say hello."},
		MeasuredRequests: 3,
		CacheMode:        CacheReuse,
	})

	result, err := runner.Run(context.Background())
	if err != nil {
		t.Fatalf("run: %v", err)
	}

	if len(result.Records) != 3 {
		t.Fatalf("record count = %d, want 3", len(result.Records))
	}
	if result.Records[1].Error == nil {
		t.Fatal("second record should contain provider error")
	}
	if result.Records[1].Error.Category != "provider_error" {
		t.Fatalf("error category = %q, want provider_error", result.Records[1].Error.Category)
	}
	if !strings.Contains(result.Records[1].Error.Message, "synthetic provider failure") {
		t.Fatalf("error message = %q, want provider failure", result.Records[1].Error.Message)
	}
	if result.Records[2].Error != nil {
		t.Fatalf("third record error = %#v, want nil", result.Records[2].Error)
	}
	if result.Summary.SuccessfulRequests != 2 {
		t.Fatalf("summary successful = %d, want 2", result.Summary.SuccessfulRequests)
	}
	if result.Summary.ErrorRequests != 1 {
		t.Fatalf("summary errors = %d, want 1", result.Summary.ErrorRequests)
	}
	if got := len(provider.callsSnapshot()); got != 3 {
		t.Fatalf("provider calls = %d, want 3", got)
	}
}

// TestRunnerRunStopsAfterContextCancellation verifies cancellation ends the sequential loop promptly.
func TestRunnerRunStopsAfterContextCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	provider := &fakeProvider{completionTokens: 1, afterCall: cancel}
	runner := NewRunner(provider, RunConfig{
		Scenario:         Scenario{Prompt: "Say hello."},
		MeasuredRequests: 5,
		CacheMode:        CacheReuse,
	})

	result, err := runner.Run(ctx)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("run error = %v, want context.Canceled", err)
	}
	if len(result.Records) != 1 {
		t.Fatalf("record count = %d, want 1 after cancellation", len(result.Records))
	}
	if result.Summary.MeasuredRequests != 1 {
		t.Fatalf("summary measured = %d, want 1", result.Summary.MeasuredRequests)
	}
	if got := len(provider.callsSnapshot()); got != 1 {
		t.Fatalf("provider calls = %d, want 1", got)
	}
}

// TestRunnerRunValidatesConfig verifies invalid configurations fail before making provider calls.
func TestRunnerRunValidatesConfig(t *testing.T) {
	tests := []struct {
		name string
		cfg  RunConfig
	}{
		{name: "negative warmup", cfg: RunConfig{WarmupRequests: -1, MeasuredRequests: 1}},
		{name: "negative measured", cfg: RunConfig{MeasuredRequests: -1}},
		{name: "zero total", cfg: RunConfig{}},
		{name: "bad cache mode", cfg: RunConfig{MeasuredRequests: 1, CacheMode: CacheMode("bad")}},
		{name: "bad connection mode", cfg: RunConfig{MeasuredRequests: 1, ConnectionMode: ConnectionMode("bad")}},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			provider := &fakeProvider{}
			result, err := NewRunner(provider, test.cfg).Run(context.Background())
			if err == nil {
				t.Fatal("expected validation error")
			}
			if result != nil {
				t.Fatalf("result = %#v, want nil", result)
			}
			if got := len(provider.callsSnapshot()); got != 0 {
				t.Fatalf("provider calls = %d, want 0", got)
			}
		})
	}
}

// TestRunnerRunRequiresProvider verifies nil providers fail validation.
func TestRunnerRunRequiresProvider(t *testing.T) {
	result, err := NewRunner(nil, RunConfig{MeasuredRequests: 1}).Run(context.Background())
	if err == nil {
		t.Fatal("expected provider validation error")
	}
	if result != nil {
		t.Fatalf("result = %#v, want nil", result)
	}
}

type fakeProvider struct {
	mu               sync.Mutex
	calls            []fakeProviderCall
	completionTokens int
	errorsByAttempt  map[int]error
	afterCall        func()
}

type fakeProviderCall struct {
	requestID string
	prompt    string
	warmup    bool
}

func (p *fakeProvider) Name() string {
	return "fake"
}

func (p *fakeProvider) Model() string {
	return "fake-model"
}

func (p *fakeProvider) Capabilities() ProviderCapabilities {
	return ProviderCapabilities{StreamingProtocol: "fake-stream", SupportsChat: true}
}

func (p *fakeProvider) StreamChat(ctx context.Context, req ProviderRequest, obs ProviderObserver) error {
	p.mu.Lock()
	attempt := len(p.calls)
	p.calls = append(p.calls, fakeProviderCall{requestID: req.RequestID, prompt: req.Prompt, warmup: req.Warmup})
	p.mu.Unlock()

	if err := ctx.Err(); err != nil {
		return err
	}

	obs.Mark(EventRequestStart)
	obs.OnHTTP(HTTPRecord{StatusCode: 200, Status: "200 OK", Protocol: "HTTP/1.1"})
	obs.MarkFirst(EventFirstSSEEvent)
	obs.OnStreamEvent(StreamEvent{RequestID: req.RequestID, Protocol: "fake-stream", DataBytes: 5})
	obs.OnOutputDelta(OutputDelta{RequestID: req.RequestID, Text: "hello", Modality: "text", Visible: true})
	obs.OnUsage(ProviderUsage{
		PromptTokens:     fakeIntPointer(3),
		CompletionTokens: fakeIntPointer(p.completionTokens),
		TotalTokens:      fakeIntPointer(3 + p.completionTokens),
		Source:           "provider-reported",
	})
	obs.OnCache(CacheRecord{})
	obs.Mark(EventBodyEOF)

	if p.afterCall != nil {
		p.afterCall()
	}
	if err := p.errorsByAttempt[attempt]; err != nil {
		return err
	}

	return nil
}

func (p *fakeProvider) callsSnapshot() []fakeProviderCall {
	p.mu.Lock()
	defer p.mu.Unlock()

	return append([]fakeProviderCall(nil), p.calls...)
}

func fakeIntPointer(value int) *int {
	return &value
}

type eventRecorder struct {
	mu     sync.Mutex
	events []RunEvent
}

func (r *eventRecorder) OnRunEvent(_ context.Context, event RunEvent) {
	r.mu.Lock()
	defer r.mu.Unlock()

	r.events = append(r.events, event)
}

func (r *eventRecorder) snapshot() []RunEvent {
	r.mu.Lock()
	defer r.mu.Unlock()

	return append([]RunEvent(nil), r.events...)
}

func eventKinds(events []RunEvent) []RunEventKind {
	kinds := make([]RunEventKind, 0, len(events))
	for _, event := range events {
		kinds = append(kinds, event.Kind)
	}

	return kinds
}

func assertEventKinds(t *testing.T, events []RunEvent, want []RunEventKind) {
	t.Helper()

	got := eventKinds(events)
	if len(got) != len(want) {
		t.Fatalf("event kinds = %#v, want %#v", got, want)
	}
	for index := range want {
		if got[index] != want[index] {
			t.Fatalf("event kinds = %#v, want %#v", got, want)
		}
	}
}

func assertSequentialEventSequence(t *testing.T, events []RunEvent) {
	t.Helper()

	for index, event := range events {
		if event.Sequence != int64(index+1) {
			t.Fatalf("event %d sequence = %d, want %d", index, event.Sequence, index+1)
		}
		if event.WallUnixNano == 0 {
			t.Fatalf("event %d has zero wall time", index)
		}
	}
}

func countEvents(events []RunEvent, kind RunEventKind) int {
	count := 0
	for _, event := range events {
		if event.Kind == kind {
			count++
		}
	}

	return count
}

func eventsByKind(events []RunEvent, kind RunEventKind) []RunEvent {
	matched := make([]RunEvent, 0)
	for _, event := range events {
		if event.Kind == kind {
			matched = append(matched, event)
		}
	}

	return matched
}
