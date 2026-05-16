package whatttft

import (
	"sync"
	"time"
)

// Recorder captures monotonic-relative request timeline events in a concurrency-safe way.
type Recorder struct {
	mu       sync.Mutex
	clock    Clock
	start    time.Time
	baseWall time.Time
	events   map[EventName]int64
}

// NewRecorder creates a Recorder using clock, or RealClock when clock is nil.
func NewRecorder(clock Clock) *Recorder {
	if clock == nil {
		clock = RealClock{}
	}

	now := clock.Now()

	return &Recorder{
		clock:    clock,
		start:    now,
		baseWall: now,
		events:   make(map[EventName]int64),
	}
}

// Mark records or overwrites name at the current monotonic time.
func (r *Recorder) Mark(name EventName) {
	r.mark(name, true)
}

// MarkFirst records name at the current monotonic time only when name is not already present.
func (r *Recorder) MarkFirst(name EventName) {
	r.mark(name, false)
}

// MarkLast records the latest occurrence of name at the current monotonic time.
func (r *Recorder) MarkLast(name EventName) {
	r.Mark(name)
}

// ElapsedNS returns the current monotonic duration in nanoseconds relative to request_start without recording an event.
func (r *Recorder) ElapsedNS() int64 {
	now := r.clock.Now()

	r.mu.Lock()
	defer r.mu.Unlock()

	return now.Sub(r.start).Nanoseconds()
}

// Timeline returns a snapshot of recorded events in nanoseconds relative to request_start.
func (r *Recorder) Timeline() Timeline {
	r.mu.Lock()
	defer r.mu.Unlock()

	events := make(map[EventName]int64, len(r.events))
	for name, atNS := range r.events {
		events[name] = atNS
	}

	return Timeline{
		BaseWallUnixNano: r.baseWall.UnixNano(),
		EventsNS:         events,
	}
}

func (r *Recorder) mark(name EventName, overwrite bool) {
	now := r.clock.Now()

	r.mu.Lock()
	defer r.mu.Unlock()

	if !overwrite {
		if _, ok := r.events[name]; ok {
			return
		}
	}

	if name == EventRequestStart {
		r.resetStartLocked(now)
	}

	r.events[name] = now.Sub(r.start).Nanoseconds()
}

func (r *Recorder) resetStartLocked(start time.Time) {
	offsetNS := r.start.Sub(start).Nanoseconds()
	for name, atNS := range r.events {
		r.events[name] = atNS + offsetNS
	}

	r.start = start
	r.baseWall = start
}
