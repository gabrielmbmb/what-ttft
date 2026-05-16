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
		{name: "unsupported concurrency", cfg: RunConfig{MeasuredRequests: 1, Concurrency: 2}},
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
