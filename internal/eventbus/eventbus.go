// Package eventbus provides bounded asynchronous fanout for live benchmark events.
package eventbus

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"

	"github.com/gabrielmbmb/what-ttft/pkg/whatttft"
)

const defaultCapacity = 1024

// Sink receives live benchmark events from a Bus.
type Sink interface {
	// Publish handles one live benchmark event. Implementations must redact secrets and should return promptly.
	Publish(context.Context, whatttft.RunEvent) error

	// Close flushes and releases sink resources. It may be called after zero or more Publish calls.
	Close(context.Context) error
}

// Options configures a Bus.
type Options struct {
	// Capacity is the bounded event queue size; zero uses a default suitable for typical CLI runs, and negative values are treated as zero.
	Capacity int
}

// Bus asynchronously fans out RunEvent values to one or more sinks.
type Bus struct {
	sinks []Sink
	queue chan whatttft.RunEvent
	done  chan struct{}

	mu     sync.RWMutex
	closed bool

	dropped atomic.Int64

	errMu sync.Mutex
	errs  []error
}

// New creates and starts a bounded asynchronous event bus.
func New(sinks []Sink, options Options) *Bus {
	capacity := options.Capacity
	if capacity <= 0 {
		capacity = defaultCapacity
	}

	bus := &Bus{
		sinks: compactSinks(sinks),
		queue: make(chan whatttft.RunEvent, capacity),
		done:  make(chan struct{}),
	}
	go bus.run()

	return bus
}

// OnRunEvent implements whatttft.RunObserver with non-blocking best-effort delivery.
func (b *Bus) OnRunEvent(_ context.Context, event whatttft.RunEvent) {
	if b == nil {
		return
	}

	b.mu.RLock()
	defer b.mu.RUnlock()
	if b.closed {
		return
	}

	cloned := event.Clone()
	select {
	case b.queue <- cloned:
	default:
		b.dropped.Add(1)
	}
}

// Dropped returns the count of events dropped because the bounded queue was full.
func (b *Bus) Dropped() int64 {
	if b == nil {
		return 0
	}

	return b.dropped.Load()
}

// Close stops accepting events, drains queued events, closes sinks, and returns collected sink errors.
func (b *Bus) Close(ctx context.Context) error {
	if b == nil {
		return nil
	}
	if ctx == nil {
		ctx = context.Background()
	}

	b.closeQueue()
	select {
	case <-b.done:
	case <-ctx.Done():
		return ctx.Err()
	}

	var closeErrs []error
	for _, sink := range b.sinks {
		if err := sink.Close(ctx); err != nil {
			closeErrs = append(closeErrs, err)
		}
	}

	return errors.Join(b.publishErrors(), errors.Join(closeErrs...))
}

func (b *Bus) closeQueue() {
	b.mu.Lock()
	defer b.mu.Unlock()

	if b.closed {
		return
	}
	b.closed = true
	close(b.queue)
}

func (b *Bus) run() {
	defer close(b.done)

	for event := range b.queue {
		for _, sink := range b.sinks {
			if err := sink.Publish(context.Background(), event); err != nil {
				b.addError(err)
			}
		}
	}
}

func (b *Bus) addError(err error) {
	if err == nil {
		return
	}

	b.errMu.Lock()
	defer b.errMu.Unlock()

	b.errs = append(b.errs, err)
}

func (b *Bus) publishErrors() error {
	b.errMu.Lock()
	defer b.errMu.Unlock()

	return errors.Join(b.errs...)
}

func compactSinks(sinks []Sink) []Sink {
	compacted := make([]Sink, 0, len(sinks))
	for _, sink := range sinks {
		if sink == nil {
			continue
		}
		compacted = append(compacted, sink)
	}

	return compacted
}
