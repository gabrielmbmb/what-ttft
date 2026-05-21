package tui

import (
	"context"
	"io"
	"sync"
	"sync/atomic"

	"github.com/gabrielmbmb/what-ttft/pkg/whatttft"

	tea "charm.land/bubbletea/v2"
)

// RunOptions configures one live Bubble Tea dashboard run.
type RunOptions struct {
	// Events carries live benchmark events into the dashboard; nil means the dashboard starts in a waiting state.
	Events <-chan whatttft.RunEvent

	// Cancel is called when the user confirms cancellation; nil means confirmation only updates local UI state.
	Cancel func()

	// Input overrides the terminal input stream when non-nil; nil uses Bubble Tea's default input.
	Input io.Reader

	// Output is the terminal output stream; nil uses Bubble Tea's default output.
	Output io.Writer
}

// Run starts the live Bubble Tea dashboard and blocks until it exits.
func Run(ctx context.Context, options RunOptions) error {
	if ctx == nil {
		ctx = context.Background()
	}

	programOptions := make([]tea.ProgramOption, 0, 2)
	if options.Input != nil {
		programOptions = append(programOptions, tea.WithInput(options.Input))
	}
	if options.Output != nil {
		programOptions = append(programOptions, tea.WithOutput(options.Output))
	}

	program := tea.NewProgram(newModelWithCancel(options.Events, options.Cancel), programOptions...)
	done := make(chan struct{})
	go func() {
		select {
		case <-ctx.Done():
			program.Quit()
		case <-done:
		}
	}()
	_, err := program.Run()
	close(done)
	return err
}

// EventSink forwards live benchmark events into a TUI event channel.
type EventSink struct {
	events chan<- whatttft.RunEvent
	once   sync.Once
	closed atomic.Bool
	drops  atomic.Int64
}

// NewEventSink creates a sink that forwards cloned events to events without blocking benchmark execution.
func NewEventSink(events chan<- whatttft.RunEvent) *EventSink {
	return &EventSink{events: events}
}

// Publish forwards event to the dashboard event channel when capacity is available.
func (s *EventSink) Publish(_ context.Context, event whatttft.RunEvent) error {
	if s == nil || s.events == nil || s.closed.Load() {
		return nil
	}

	select {
	case s.events <- event.Clone():
	default:
		s.drops.Add(1)
	}
	return nil
}

// Close closes the dashboard event channel once and releases the sink.
func (s *EventSink) Close(context.Context) error {
	if s == nil {
		return nil
	}

	s.once.Do(func() {
		s.closed.Store(true)
		if s.events != nil {
			close(s.events)
		}
	})
	return nil
}

// Dropped returns the count of events dropped because the dashboard channel was full.
func (s *EventSink) Dropped() int64 {
	if s == nil {
		return 0
	}
	return s.drops.Load()
}
