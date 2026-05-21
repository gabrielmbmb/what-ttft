package tui

import (
	"github.com/gabrielmbmb/what-ttft/pkg/whatttft"

	tea "charm.land/bubbletea/v2"
)

type runEventMsg struct {
	Event whatttft.RunEvent
}

type eventChannelClosedMsg struct{}

func waitForRunEvent(events <-chan whatttft.RunEvent) tea.Cmd {
	if events == nil {
		return nil
	}

	return func() tea.Msg {
		event, ok := <-events
		if !ok {
			return eventChannelClosedMsg{}
		}

		return runEventMsg{Event: event.Clone()}
	}
}
