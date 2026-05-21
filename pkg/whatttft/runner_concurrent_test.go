package whatttft

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"
)

// TestRunnerRunConcurrentProducesConfiguredMeasuredRecords verifies fixed concurrency completes every measured request.
func TestRunnerRunConcurrentProducesConfiguredMeasuredRecords(t *testing.T) {
	provider := &concurrentProvider{delay: time.Millisecond}
	runner := NewRunner(provider, RunConfig{
		Scenario:         Scenario{Name: "concurrent", Prompt: "Say hello."},
		MeasuredRequests: 7,
		Concurrency:      3,
		CacheMode:        CacheReuse,
		RequestIDPrefix:  "concurrent-",
	})

	result, err := runner.Run(context.Background())
	if err != nil {
		t.Fatalf("run: %v", err)
	}

	if len(result.Records) != 7 {
		t.Fatalf("record count = %d, want 7", len(result.Records))
	}
	if result.Summary.MeasuredRequests != 7 {
		t.Fatalf("summary measured = %d, want 7", result.Summary.MeasuredRequests)
	}
	if result.Summary.SuccessfulRequests != 7 {
		t.Fatalf("summary successful = %d, want 7", result.Summary.SuccessfulRequests)
	}
	for index, record := range result.Records {
		if record.Attempt != index {
			t.Fatalf("record %d attempt = %d, want %d", index, record.Attempt, index)
		}
		if record.RequestID != "concurrent-req-"+sixDigit(index) {
			t.Fatalf("record %d request ID = %q", index, record.RequestID)
		}
		if record.Warmup {
			t.Fatalf("record %d unexpectedly marked warmup", index)
		}
	}
	if provider.maxActiveSnapshot() > 3 {
		t.Fatalf("max active = %d, want <= 3", provider.maxActiveSnapshot())
	}
}

// TestRunnerRunConcurrentEmitsLiveRequestEvents verifies fixed-concurrency request completion events are emitted before final sorted results are returned.
func TestRunnerRunConcurrentEmitsLiveRequestEvents(t *testing.T) {
	recorder := &eventRecorder{}
	provider := &concurrentProvider{delay: time.Millisecond}
	runner := NewRunnerWithOptions(provider, RunConfig{
		Scenario:         Scenario{Name: "concurrent-events", Prompt: "Say hello."},
		MeasuredRequests: 7,
		Concurrency:      3,
		CacheMode:        CacheReuse,
		RequestIDPrefix:  "events-",
	}, RunnerOptions{Observer: recorder})

	result, err := runner.Run(context.Background())
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if len(result.Records) != 7 {
		t.Fatalf("records = %d, want 7", len(result.Records))
	}
	for index, record := range result.Records {
		if record.Attempt != index {
			t.Fatalf("record %d attempt = %d, want sorted attempt", index, record.Attempt)
		}
	}

	events := recorder.snapshot()
	if countEvents(events, EventRequestScheduled) != 7 {
		t.Fatalf("request_scheduled count = %d, want 7 in %#v", countEvents(events, EventRequestScheduled), eventKinds(events))
	}
	if countEvents(events, EventRequestDispatched) != 7 {
		t.Fatalf("request_dispatched count = %d, want 7", countEvents(events, EventRequestDispatched))
	}
	requestFinished := eventsByKind(events, EventRequestFinished)
	if len(requestFinished) != 7 {
		t.Fatalf("request_finished events = %d, want 7", len(requestFinished))
	}
	seen := make(map[string]bool, len(requestFinished))
	for _, event := range requestFinished {
		if event.Record == nil {
			t.Fatalf("request_finished missing record: %#v", event)
		}
		if event.Attempt == nil || event.Warmup == nil || *event.Warmup {
			t.Fatalf("request event attempt/warmup = %v/%v", event.Attempt, event.Warmup)
		}
		seen[event.RequestID] = true
	}
	for index := range 7 {
		requestID := "events-req-" + sixDigit(index)
		if !seen[requestID] {
			t.Fatalf("missing request_finished event for %s in %#v", requestID, requestFinished)
		}
	}
	if countEvents(events, EventRunFinished) != 1 {
		t.Fatalf("run_finished count = %d, want 1", countEvents(events, EventRunFinished))
	}
}

// TestRunnerRunConcurrentWarmupBarrier verifies all warmups finish before measured requests begin.
func TestRunnerRunConcurrentWarmupBarrier(t *testing.T) {
	provider := &concurrentProvider{delay: time.Millisecond, warmupTotal: 4}
	runner := NewRunner(provider, RunConfig{
		Scenario:         Scenario{Name: "barrier", Prompt: "Say hello."},
		WarmupRequests:   4,
		MeasuredRequests: 5,
		Concurrency:      2,
		CacheMode:        CacheReuse,
	})

	result, err := runner.Run(context.Background())
	if err != nil {
		t.Fatalf("run: %v", err)
	}

	if len(result.Records) != 9 {
		t.Fatalf("record count = %d, want 9", len(result.Records))
	}
	for index, record := range result.Records {
		if record.Attempt != index {
			t.Fatalf("record %d attempt = %d, want %d", index, record.Attempt, index)
		}
		if record.Warmup != (index < 4) {
			t.Fatalf("record %d warmup = %t", index, record.Warmup)
		}
	}
	if provider.measuredStartedBeforeWarmupDoneSnapshot() {
		t.Fatal("measured request started before all warmup requests finished")
	}
}

// TestRunnerRunConcurrentStopsOnContextCancellation verifies cancellation returns partial records and context error.
func TestRunnerRunConcurrentStopsOnContextCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	provider := &concurrentProvider{delay: time.Millisecond, afterFirstStart: cancel}
	runner := NewRunner(provider, RunConfig{
		Scenario:         Scenario{Name: "cancel", Prompt: "Say hello."},
		MeasuredRequests: 8,
		Concurrency:      2,
		CacheMode:        CacheReuse,
	})

	result, err := runner.Run(ctx)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("run error = %v, want context.Canceled", err)
	}
	if result == nil {
		t.Fatal("result should contain partial records")
	}
	if len(result.Records) == 0 {
		t.Fatal("expected at least one partial record")
	}
	if len(result.Records) > 8 {
		t.Fatalf("record count = %d, want <= 8", len(result.Records))
	}
}

type concurrentProvider struct {
	mu                              sync.Mutex
	delay                           time.Duration
	active                          int
	maxActive                       int
	started                         int
	warmupTotal                     int
	warmupFinished                  int
	measuredStartedBeforeWarmupDone bool
	afterFirstStart                 func()
}

func (p *concurrentProvider) Name() string {
	return "concurrent-fake"
}

func (p *concurrentProvider) Model() string {
	return "concurrent-model"
}

func (p *concurrentProvider) Capabilities() ProviderCapabilities {
	return ProviderCapabilities{StreamingProtocol: "fake-stream", SupportsChat: true}
}

func (p *concurrentProvider) StreamChat(ctx context.Context, req ProviderRequest, obs ProviderObserver) error {
	p.recordStart(req.Warmup)
	defer p.recordFinish(req.Warmup)

	if err := ctx.Err(); err != nil {
		return err
	}
	if p.delay > 0 {
		select {
		case <-time.After(p.delay):
		case <-ctx.Done():
			return ctx.Err()
		}
	}

	obs.Mark(EventRequestStart)
	obs.MarkFirst(EventFirstSSEEvent)
	obs.OnOutputDelta(OutputDelta{RequestID: req.RequestID, Text: "hello", Modality: "text", Visible: true})
	obs.OnUsage(ProviderUsage{CompletionTokens: concurrentIntPointer(1)})
	obs.Mark(EventBodyEOF)

	return nil
}

func (p *concurrentProvider) recordStart(warmup bool) {
	var callback func()

	p.mu.Lock()
	p.active++
	if p.active > p.maxActive {
		p.maxActive = p.active
	}
	p.started++
	if !warmup && p.warmupFinished < p.warmupTotal {
		p.measuredStartedBeforeWarmupDone = true
	}
	if p.started == 1 {
		callback = p.afterFirstStart
	}
	p.mu.Unlock()

	if callback != nil {
		callback()
	}
}

func (p *concurrentProvider) recordFinish(warmup bool) {
	p.mu.Lock()
	defer p.mu.Unlock()

	p.active--
	if warmup {
		p.warmupFinished++
	}
}

func (p *concurrentProvider) maxActiveSnapshot() int {
	p.mu.Lock()
	defer p.mu.Unlock()

	return p.maxActive
}

func (p *concurrentProvider) measuredStartedBeforeWarmupDoneSnapshot() bool {
	p.mu.Lock()
	defer p.mu.Unlock()

	return p.measuredStartedBeforeWarmupDone
}

func concurrentIntPointer(value int) *int {
	return &value
}

func sixDigit(value int) string {
	return string([]byte{
		byte('0' + value/100000%10),
		byte('0' + value/10000%10),
		byte('0' + value/1000%10),
		byte('0' + value/100%10),
		byte('0' + value/10%10),
		byte('0' + value%10),
	})
}
