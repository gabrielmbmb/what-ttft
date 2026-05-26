package tui

import (
	"context"
	"runtime"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gabrielmbmb/what-ttft/pkg/whatttft"
)

// TestEventSinkPublishesClonedEvents verifies channel events are isolated from caller mutation.
func TestEventSinkPublishesClonedEvents(t *testing.T) {
	events := make(chan whatttft.RunEvent, 1)
	sink := NewEventSink(events)
	attempt := 1
	event := whatttft.RunEvent{Kind: whatttft.EventRequestScheduled, Attempt: &attempt}
	if err := sink.Publish(context.Background(), event); err != nil {
		t.Fatalf("publish event: %v", err)
	}
	attempt = 2

	got := <-events
	if got.Attempt == nil || *got.Attempt != 1 {
		t.Fatalf("published attempt = %#v, want cloned value 1", got.Attempt)
	}
}

// TestEventSinkDropsWhenFull verifies a full dashboard channel does not block event publication.
func TestEventSinkDropsWhenFull(t *testing.T) {
	events := make(chan whatttft.RunEvent, 1)
	sink := NewEventSink(events)
	if err := sink.Publish(context.Background(), whatttft.RunEvent{Kind: whatttft.EventRunStarted}); err != nil {
		t.Fatalf("publish first event: %v", err)
	}
	if err := sink.Publish(context.Background(), whatttft.RunEvent{Kind: whatttft.EventRunFinished}); err != nil {
		t.Fatalf("publish dropped event: %v", err)
	}
	if sink.Dropped() != 1 {
		t.Fatalf("dropped = %d, want 1", sink.Dropped())
	}
}

// TestEventSinkCloseClosesChannel verifies Close releases dashboard event consumers.
func TestEventSinkCloseClosesChannel(t *testing.T) {
	events := make(chan whatttft.RunEvent)
	sink := NewEventSink(events)
	if err := sink.Close(context.Background()); err != nil {
		t.Fatalf("close sink: %v", err)
	}
	if _, ok := <-events; ok {
		t.Fatal("events channel still open after close")
	}
	if err := sink.Close(context.Background()); err != nil {
		t.Fatalf("second close sink: %v", err)
	}
}

// TestEventSinkConcurrentPublishAndCloseDoesNotPanic exercises the TUI sink under race-detector-style concurrent publication and shutdown.
func TestEventSinkConcurrentPublishAndCloseDoesNotPanic(t *testing.T) {
	events := make(chan whatttft.RunEvent, 64)
	sink := NewEventSink(events)
	drained := make(chan struct{})
	go func() {
		defer close(drained)
		for event := range events {
			_ = event
		}
	}()

	start := make(chan struct{})
	stop := make(chan struct{})
	panicCh := make(chan any, 8)
	var attempts atomic.Int64
	var wg sync.WaitGroup
	for worker := 0; worker < 8; worker++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			defer func() {
				if recovered := recover(); recovered != nil {
					panicCh <- recovered
				}
			}()
			<-start
			for {
				select {
				case <-stop:
					return
				default:
					attempts.Add(1)
					if err := sink.Publish(context.Background(), whatttft.RunEvent{Kind: whatttft.EventRequestFinished}); err != nil {
						panicCh <- err
						return
					}
				}
			}
		}()
	}

	close(start)
	deadline := time.After(time.Second)
	for attempts.Load() < 1_000 {
		select {
		case <-deadline:
			t.Fatalf("publish attempts before close = %d, want at least 1000", attempts.Load())
		default:
			runtime.Gosched()
		}
	}
	if err := sink.Close(context.Background()); err != nil {
		t.Fatalf("close sink: %v", err)
	}
	close(stop)
	wg.Wait()
	close(panicCh)
	for recovered := range panicCh {
		t.Fatalf("concurrent publish/close recovered panic or error: %v", recovered)
	}
	<-drained
}

// TestModelCancelCallback verifies a confirmed cancellation invokes the configured callback.
func TestModelCancelCallback(t *testing.T) {
	called := false
	app := newModelWithCancel(nil, func() { called = true })
	app = updateModel(t, app, runEventMsg{Event: whatttft.RunEvent{Kind: whatttft.EventRunStarted}})
	app = updateModel(t, app, keyPress("q"))
	updated, cmd := app.Update(keyPress("y"))
	app = assertModel(t, updated)
	if !called || !app.canceled || cmd == nil {
		t.Fatalf("cancel called/canceled/cmd = %t/%t/%v, want true/true/non-nil", called, app.canceled, cmd)
	}
}
