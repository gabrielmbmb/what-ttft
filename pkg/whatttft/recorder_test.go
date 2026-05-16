package whatttft

import (
	"sync"
	"testing"
	"time"
)

// TestRecorderMarkFirstDoesNotOverwrite verifies first-event semantics preserve the first timestamp.
func TestRecorderMarkFirstDoesNotOverwrite(t *testing.T) {
	clock := newFakeClock()
	rec := NewRecorder(clock)
	rec.Mark(EventRequestStart)

	clock.Advance(100 * time.Millisecond)
	rec.MarkFirst(EventFirstSSEEvent)

	clock.Advance(200 * time.Millisecond)
	rec.MarkFirst(EventFirstSSEEvent)

	timeline := rec.Timeline()
	if got := timeline.EventsNS[EventFirstSSEEvent]; got != int64(100*time.Millisecond) {
		t.Fatalf("first SSE event = %d ns, want %d ns", got, 100*time.Millisecond)
	}
}

// TestRecorderScheduledAtCanBeNegative verifies events recorded before request_start are rebased correctly.
func TestRecorderScheduledAtCanBeNegative(t *testing.T) {
	clock := newFakeClock()
	rec := NewRecorder(clock)

	scheduledWall := clock.Now()
	rec.Mark(EventScheduledAt)

	clock.Advance(25 * time.Millisecond)
	requestStartWall := clock.Now()
	rec.Mark(EventRequestStart)

	timeline := rec.Timeline()
	if got := timeline.EventsNS[EventScheduledAt]; got != int64(-25*time.Millisecond) {
		t.Fatalf("scheduled_at = %d ns, want %d ns", got, -25*time.Millisecond)
	}
	if got := timeline.EventsNS[EventRequestStart]; got != 0 {
		t.Fatalf("request_start = %d ns, want 0 ns", got)
	}
	if timeline.BaseWallUnixNano != requestStartWall.UnixNano() {
		t.Fatalf("base wall = %d, want request_start wall %d", timeline.BaseWallUnixNano, requestStartWall.UnixNano())
	}
	if timeline.BaseWallUnixNano == scheduledWall.UnixNano() {
		t.Fatal("base wall should be rebased from scheduled_at to request_start")
	}
}

// TestRecorderMarkLastOverwrites verifies latest-event semantics replace earlier timestamps.
func TestRecorderMarkLastOverwrites(t *testing.T) {
	clock := newFakeClock()
	rec := NewRecorder(clock)
	rec.Mark(EventRequestStart)

	clock.Advance(10 * time.Millisecond)
	rec.MarkLast(EventLastOutputDelta)

	clock.Advance(5 * time.Millisecond)
	rec.MarkLast(EventLastOutputDelta)

	timeline := rec.Timeline()
	if got := timeline.EventsNS[EventLastOutputDelta]; got != int64(15*time.Millisecond) {
		t.Fatalf("last output delta = %d ns, want %d ns", got, 15*time.Millisecond)
	}
}

// TestRecorderElapsedNS verifies elapsed timestamps can be read without recording an event.
func TestRecorderElapsedNS(t *testing.T) {
	clock := newFakeClock()
	rec := NewRecorder(clock)
	rec.Mark(EventRequestStart)

	clock.Advance(7 * time.Millisecond)

	if got := rec.ElapsedNS(); got != int64(7*time.Millisecond) {
		t.Fatalf("elapsed = %d ns, want %d ns", got, 7*time.Millisecond)
	}
	if _, ok := rec.Timeline().EventsNS[EventFirstSSEEvent]; ok {
		t.Fatal("ElapsedNS should not record timeline events")
	}
}

// TestRecorderTimelineReturnsCopy verifies callers cannot mutate recorder state through snapshots.
func TestRecorderTimelineReturnsCopy(t *testing.T) {
	clock := newFakeClock()
	rec := NewRecorder(clock)
	rec.Mark(EventRequestStart)

	clock.Advance(3 * time.Millisecond)
	rec.Mark(EventFirstResponseByte)

	timeline := rec.Timeline()
	timeline.EventsNS[EventFirstResponseByte] = 99

	fresh := rec.Timeline()
	if got := fresh.EventsNS[EventFirstResponseByte]; got != int64(3*time.Millisecond) {
		t.Fatalf("fresh first response byte = %d ns, want %d ns", got, 3*time.Millisecond)
	}
}

// TestRecorderAllowsNilClock verifies nil clocks default to RealClock.
func TestRecorderAllowsNilClock(t *testing.T) {
	rec := NewRecorder(nil)
	rec.Mark(EventRequestStart)

	timeline := rec.Timeline()
	if timeline.BaseWallUnixNano == 0 {
		t.Fatal("base wall timestamp should be populated")
	}
	if got := timeline.EventsNS[EventRequestStart]; got != 0 {
		t.Fatalf("request_start = %d ns, want 0 ns", got)
	}
}

// TestRecorderConcurrentMarks verifies timeline mutation is safe for asynchronous trace callbacks.
func TestRecorderConcurrentMarks(t *testing.T) {
	clock := newFakeClock()
	rec := NewRecorder(clock)
	rec.Mark(EventRequestStart)

	var wg sync.WaitGroup
	for range 20 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			rec.MarkFirst(EventFirstSSEEvent)
			rec.MarkLast(EventLastOutputDelta)
			_ = rec.ElapsedNS()
			_ = rec.Timeline()
		}()
	}
	wg.Wait()

	timeline := rec.Timeline()
	if _, ok := timeline.EventsNS[EventFirstSSEEvent]; !ok {
		t.Fatal("first SSE event should be recorded")
	}
	if _, ok := timeline.EventsNS[EventLastOutputDelta]; !ok {
		t.Fatal("last output delta should be recorded")
	}
}

type fakeClock struct {
	mu  sync.Mutex
	now time.Time
}

func newFakeClock() *fakeClock {
	return &fakeClock{now: time.Now()}
}

func (c *fakeClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()

	return c.now
}

func (c *fakeClock) Advance(duration time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.now = c.now.Add(duration)
}
