package whatttft

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"
)

// interleavedProbe is shared across every target provider in an interleaved run so tests can
// observe the global in-flight concurrency and the warmup-before-measured barrier across targets.
type interleavedProbe struct {
	mu                       sync.Mutex
	delay                    time.Duration
	active                   int
	maxActive                int
	warmupTotal              int
	warmupDone               int
	measuredBeforeWarmupDone bool
}

func (p *interleavedProbe) start(warmup bool) {
	p.mu.Lock()
	p.active++
	if p.active > p.maxActive {
		p.maxActive = p.active
	}
	if !warmup && p.warmupDone < p.warmupTotal {
		p.measuredBeforeWarmupDone = true
	}
	p.mu.Unlock()
}

func (p *interleavedProbe) finish(warmup bool) {
	p.mu.Lock()
	p.active--
	if warmup {
		p.warmupDone++
	}
	p.mu.Unlock()
}

func (p *interleavedProbe) snapshot() (maxActive int, measuredBeforeWarmupDone bool) {
	p.mu.Lock()
	defer p.mu.Unlock()

	return p.maxActive, p.measuredBeforeWarmupDone
}

type probeProvider struct {
	probe *interleavedProbe
	model string
}

func (p *probeProvider) Name() string {
	return "probe-fake"
}

func (p *probeProvider) Model() string {
	return p.model
}

func (p *probeProvider) Capabilities() ProviderCapabilities {
	return ProviderCapabilities{StreamingProtocol: "fake-stream", SupportsChat: true}
}

func (p *probeProvider) StreamChat(ctx context.Context, req ProviderRequest, obs ProviderObserver) error {
	p.probe.start(req.Warmup)
	defer p.probe.finish(req.Warmup)

	if err := ctx.Err(); err != nil {
		return err
	}
	if p.probe.delay > 0 {
		select {
		case <-time.After(p.probe.delay):
		case <-ctx.Done():
			return ctx.Err()
		}
	}

	obs.Mark(EventRequestStart)
	obs.OnHTTP(HTTPRecord{StatusCode: 200, Status: "200 OK", Protocol: "HTTP/1.1"})
	obs.MarkFirst(EventFirstSSEEvent)
	obs.OnOutputDelta(OutputDelta{RequestID: req.RequestID, Text: "hello", Modality: "text", Visible: true})
	obs.OnUsage(ProviderUsage{CompletionTokens: fakeIntPointer(1)})
	obs.Mark(EventBodyEOF)

	return nil
}

// TestInterleavedSharesConcurrencyBudgetAcrossTargets verifies interleaved runs cap total in-flight
// requests to the shared budget, barrier all warmups before measured, and produce per-target records.
func TestInterleavedSharesConcurrencyBudgetAcrossTargets(t *testing.T) {
	const (
		targetCount  = 3
		warmupEach   = 2
		measuredEach = 4
		budget       = 4
	)

	probe := &interleavedProbe{delay: 5 * time.Millisecond, warmupTotal: targetCount * warmupEach}

	targets := make([]BenchmarkTarget, 0, targetCount)
	for i := range targetCount {
		targets = append(targets, BenchmarkTarget{
			ID:       benchmarkProbeTargetID(i),
			Provider: &probeProvider{probe: probe, model: benchmarkProbeTargetID(i)},
			Config: RunConfig{
				Scenario:         Scenario{Name: "short", Prompt: "Say hello."},
				WarmupRequests:   warmupEach,
				MeasuredRequests: measuredEach,
				Concurrency:      budget,
				CacheMode:        CacheReuse,
				ConnectionMode:   WarmConnections,
			},
		})
	}

	result, err := RunBenchmark(context.Background(), BenchmarkConfig{
		Targets:     targets,
		TargetOrder: InterleavedTargetOrder,
	})
	if err != nil {
		t.Fatalf("interleaved benchmark: %v", err)
	}

	if result.TargetOrder != InterleavedTargetOrder {
		t.Fatalf("target order = %q, want interleaved", result.TargetOrder)
	}

	wantTotal := targetCount * (warmupEach + measuredEach)
	if len(result.Records) != wantTotal {
		t.Fatalf("records = %d, want %d", len(result.Records), wantTotal)
	}

	maxActive, measuredBeforeWarmupDone := probe.snapshot()
	if maxActive > budget {
		t.Fatalf("max concurrent = %d, want <= shared budget %d (per-target concurrency was not multiplied)", maxActive, budget)
	}
	if maxActive < 2 {
		t.Fatalf("max concurrent = %d, want >= 2 (targets should interleave under the shared pool)", maxActive)
	}
	if measuredBeforeWarmupDone {
		t.Fatal("a measured request started before all warmups completed; warmup barrier was not respected")
	}

	// Every target contributes exactly measuredEach successful measured requests.
	groups := benchmarkGroupsByTarget(result.Summary.Groups)
	if len(groups) != targetCount {
		t.Fatalf("summary groups = %d, want %d", len(groups), targetCount)
	}
	for i := range targetCount {
		id := benchmarkProbeTargetID(i)
		group, ok := groups[id]
		if !ok {
			t.Fatalf("missing summary group for %s", id)
		}
		if group.SuccessfulRequests != measuredEach {
			t.Fatalf("group %s successful = %d, want %d", id, group.SuccessfulRequests, measuredEach)
		}
	}
}

// TestInterleavedPerTargetEventsDoNotCarryGlobalCounts guards against the dashboard double-count
// bug: no target-scoped event may report completed/successful counts larger than that target's own
// request total, otherwise a dashboard that sums per-target counters overcounts the whole run.
func TestInterleavedPerTargetEventsDoNotCarryGlobalCounts(t *testing.T) {
	probe := &interleavedProbe{warmupTotal: 2 * 2}
	perTargetTotal := 2 + 3 // warmup + measured
	targets := []BenchmarkTarget{
		{ID: "t0", Provider: &probeProvider{probe: probe, model: "m0"}, Config: RunConfig{Scenario: Scenario{Prompt: "hi"}, WarmupRequests: 2, MeasuredRequests: 3, Concurrency: 4}},
		{ID: "t1", Provider: &probeProvider{probe: probe, model: "m1"}, Config: RunConfig{Scenario: Scenario{Prompt: "hi"}, WarmupRequests: 2, MeasuredRequests: 3, Concurrency: 4}},
	}

	rec := &eventRecorder{}
	if _, err := RunBenchmarkWithOptions(context.Background(), BenchmarkConfig{Targets: targets, TargetOrder: InterleavedTargetOrder}, BenchmarkOptions{Observer: rec}); err != nil {
		t.Fatalf("interleaved benchmark: %v", err)
	}

	finishedByTarget := map[string]int{}
	for _, event := range rec.snapshot() {
		if event.TargetID == "" {
			continue
		}
		if event.Kind == EventTargetFinished {
			finishedByTarget[event.TargetID] = event.CompletedRequests
		}
		// Every target-scoped event must stay within the target's own totals; a target_finished
		// event carrying the benchmark-wide total is exactly the double-count bug this guards.
		if event.CompletedRequests > perTargetTotal {
			t.Fatalf("event %s for %s reports completed=%d, exceeds per-target total %d (global counts leaked into a per-target event)", event.Kind, event.TargetID, event.CompletedRequests, perTargetTotal)
		}
		if event.SuccessfulRequests > perTargetTotal {
			t.Fatalf("event %s for %s reports successful=%d, exceeds per-target total %d", event.Kind, event.TargetID, event.SuccessfulRequests, perTargetTotal)
		}
	}

	// target_finished must carry each target's own completed count so the dashboard can reconcile.
	for _, id := range []string{"t0", "t1"} {
		if finishedByTarget[id] != perTargetTotal {
			t.Fatalf("target_finished for %s reports completed=%d, want per-target total %d", id, finishedByTarget[id], perTargetTotal)
		}
	}
}

// TestInterleavedDefaultsToSerialWhenUnset verifies an empty target order still runs (serially).
func TestInterleavedDefaultsToSerialWhenUnset(t *testing.T) {
	probe := &interleavedProbe{}
	result, err := RunBenchmark(context.Background(), BenchmarkConfig{
		Targets: []BenchmarkTarget{{
			ID:       "solo",
			Provider: &probeProvider{probe: probe, model: "m"},
			Config:   benchmarkMeasuredConfig(),
		}},
	})
	if err != nil {
		t.Fatalf("benchmark: %v", err)
	}
	if result.TargetOrder != SerialTargetOrder {
		t.Fatalf("target order = %q, want serial default", result.TargetOrder)
	}
}

func benchmarkProbeTargetID(index int) string {
	return fmt.Sprintf("probe-%d", index)
}
