package eventbus

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gabrielmbmb/what-ttft/pkg/whatttft"
)

// TestBusDeliversEventsToMultipleSinks verifies one bus fans out events to all configured sinks.
func TestBusDeliversEventsToMultipleSinks(t *testing.T) {
	sinkA := &recordingSink{}
	sinkB := &recordingSink{}
	bus := New([]Sink{sinkA, nil, sinkB}, Options{Capacity: 4})

	bus.OnRunEvent(context.Background(), whatttft.RunEvent{Sequence: 1, Kind: whatttft.EventRunStarted})
	if err := bus.Close(context.Background()); err != nil {
		t.Fatalf("close bus: %v", err)
	}

	assertRecordingSinkEvents(t, sinkA, []whatttft.RunEventKind{whatttft.EventRunStarted})
	assertRecordingSinkEvents(t, sinkB, []whatttft.RunEventKind{whatttft.EventRunStarted})
	if !sinkA.closedSnapshot() || !sinkB.closedSnapshot() {
		t.Fatalf("sinks closed = %t/%t, want true/true", sinkA.closedSnapshot(), sinkB.closedSnapshot())
	}
}

// TestBusSlowSinkDoesNotDeadlockPublisher verifies a blocked sink does not block OnRunEvent callers.
func TestBusSlowSinkDoesNotDeadlockPublisher(t *testing.T) {
	sink := newBlockingSink()
	bus := New([]Sink{sink}, Options{Capacity: 1})

	bus.OnRunEvent(context.Background(), whatttft.RunEvent{Sequence: 1, Kind: whatttft.EventRunStarted})
	select {
	case <-sink.started:
	case <-time.After(time.Second):
		t.Fatal("blocking sink did not receive first event")
	}

	done := make(chan struct{})
	go func() {
		defer close(done)
		for index := 0; index < 5; index++ {
			bus.OnRunEvent(context.Background(), whatttft.RunEvent{Sequence: int64(index + 2), Kind: whatttft.EventRequestFinished})
		}
	}()

	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("OnRunEvent blocked behind slow sink")
	}
	if got := bus.Dropped(); got != 4 {
		t.Fatalf("dropped events = %d, want deterministic count 4", got)
	}

	close(sink.release)
	if err := bus.Close(context.Background()); err != nil {
		t.Fatalf("close bus: %v", err)
	}
}

// TestBusCloseReturnsPublishAndCloseErrors verifies sink publish and close failures are surfaced.
func TestBusCloseReturnsPublishAndCloseErrors(t *testing.T) {
	sink := &errorSink{publishErr: errors.New("publish failed"), closeErr: errors.New("close failed")}
	bus := New([]Sink{sink}, Options{Capacity: 2})
	bus.OnRunEvent(context.Background(), whatttft.RunEvent{Kind: whatttft.EventRunStarted})

	err := bus.Close(context.Background())
	if err == nil {
		t.Fatal("expected close error")
	}
	if !strings.Contains(err.Error(), "publish failed") || !strings.Contains(err.Error(), "close failed") {
		t.Fatalf("close error = %q, want publish and close failures", err)
	}
}

// TestBusCloseWithContext verifies Close respects a caller context while a sink is blocked.
func TestBusCloseWithContext(t *testing.T) {
	sink := newBlockingSink()
	bus := New([]Sink{sink}, Options{Capacity: 1})
	bus.OnRunEvent(context.Background(), whatttft.RunEvent{Kind: whatttft.EventRunStarted})
	select {
	case <-sink.started:
	case <-time.After(time.Second):
		t.Fatal("blocking sink did not receive first event")
	}

	ctx, cancel := context.WithTimeout(context.Background(), time.Millisecond)
	defer cancel()
	if err := bus.Close(ctx); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("close error = %v, want deadline exceeded", err)
	}

	close(sink.release)
	if err := bus.Close(context.Background()); err != nil {
		t.Fatalf("second close after release: %v", err)
	}
}

// TestBusOnRunEventAfterCloseIsNoop verifies post-close events are ignored safely.
func TestBusOnRunEventAfterCloseIsNoop(t *testing.T) {
	sink := &recordingSink{}
	bus := New([]Sink{sink}, Options{Capacity: 2})
	if err := bus.Close(context.Background()); err != nil {
		t.Fatalf("close bus: %v", err)
	}

	bus.OnRunEvent(context.Background(), whatttft.RunEvent{Kind: whatttft.EventRunStarted})
	if got := len(sink.snapshot()); got != 0 {
		t.Fatalf("sink events = %d, want 0 after close", got)
	}
}

type recordingSink struct {
	mu     sync.Mutex
	events []whatttft.RunEvent
	closed bool
}

func (s *recordingSink) Publish(_ context.Context, event whatttft.RunEvent) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.events = append(s.events, event)
	return nil
}

func (s *recordingSink) Close(context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.closed = true
	return nil
}

func (s *recordingSink) snapshot() []whatttft.RunEvent {
	s.mu.Lock()
	defer s.mu.Unlock()

	return append([]whatttft.RunEvent(nil), s.events...)
}

func (s *recordingSink) closedSnapshot() bool {
	s.mu.Lock()
	defer s.mu.Unlock()

	return s.closed
}

type blockingSink struct {
	started chan struct{}
	release chan struct{}
	once    sync.Once
}

func newBlockingSink() *blockingSink {
	return &blockingSink{started: make(chan struct{}), release: make(chan struct{})}
}

func (s *blockingSink) Publish(context.Context, whatttft.RunEvent) error {
	s.once.Do(func() { close(s.started) })
	<-s.release
	return nil
}

func (s *blockingSink) Close(context.Context) error {
	return nil
}

type errorSink struct {
	publishErr error
	closeErr   error
}

func (s *errorSink) Publish(context.Context, whatttft.RunEvent) error {
	return s.publishErr
}

func (s *errorSink) Close(context.Context) error {
	return s.closeErr
}

func assertRecordingSinkEvents(t *testing.T, sink *recordingSink, want []whatttft.RunEventKind) {
	t.Helper()

	events := sink.snapshot()
	if len(events) != len(want) {
		t.Fatalf("events = %#v, want kinds %#v", events, want)
	}
	for index, kind := range want {
		if events[index].Kind != kind {
			t.Fatalf("event kinds = %#v, want %#v", eventKinds(events), want)
		}
	}
}

func eventKinds(events []whatttft.RunEvent) []whatttft.RunEventKind {
	kinds := make([]whatttft.RunEventKind, 0, len(events))
	for _, event := range events {
		kinds = append(kinds, event.Kind)
	}

	return kinds
}
