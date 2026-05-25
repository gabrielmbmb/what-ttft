package tui

import (
	"fmt"
	"sort"
	"strings"

	"github.com/gabrielmbmb/what-ttft/pkg/whatttft"

	"charm.land/bubbles/v2/help"
	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"
)

type pane int

const (
	paneSummary pane = iota
	paneTTFT
	paneE2E
	paneWaterfall
	paneCount
)

var _ tea.Model = model{}

type model struct {
	events <-chan whatttft.RunEvent
	cancel func()
	width  int
	height int

	focus int
	pane  pane

	store            liveStore
	running          bool
	completed        bool
	canceled         bool
	failed           bool
	confirmingCancel bool
	channelClosed    bool

	keys  keyMap
	help  help.Model
	theme tuiTheme
}

func newModel(events <-chan whatttft.RunEvent) model {
	return newModelWithCancel(events, nil)
}

func newModelWithCancel(events <-chan whatttft.RunEvent, cancel func()) model {
	helpModel := help.New()
	helpModel.ShowAll = false

	return model{
		events: events,
		cancel: cancel,
		store:  newLiveStore(),
		keys:   defaultKeyMap(),
		help:   helpModel,
		theme:  defaultTheme(),
	}
}

func (m model) Init() tea.Cmd {
	return waitForRunEvent(m.events)
}

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		m.help.SetWidth(msg.Width)
		return m, nil
	case runEventMsg:
		m.applyRunEvent(msg.Event)
		return m, waitForRunEvent(m.events)
	case eventChannelClosedMsg:
		m.channelClosed = true
		return m, nil
	case tea.KeyPressMsg:
		return m.updateKey(msg)
	default:
		return m, nil
	}
}

func (m model) View() tea.View {
	content := m.render()
	view := tea.NewView(content)
	view.AltScreen = true
	view.WindowTitle = m.windowTitle()
	return view
}

func (m *model) applyRunEvent(event whatttft.RunEvent) {
	m.store.applyEvent(event)

	switch event.Kind {
	case whatttft.EventBenchmarkStarted, whatttft.EventRunStarted, whatttft.EventTargetStarted:
		m.running = true
	case whatttft.EventBenchmarkFinished, whatttft.EventRunFinished, whatttft.EventReportWriteFinished:
		m.running = false
		m.completed = true
	case whatttft.EventBenchmarkCanceled, whatttft.EventRunCanceled:
		m.running = false
		m.canceled = true
	case whatttft.EventBenchmarkFailed, whatttft.EventRunFailed, whatttft.EventTargetFailed, whatttft.EventReportWriteFailed:
		m.running = false
		m.failed = true
	}
}

func (m model) updateKey(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	if m.confirmingCancel {
		switch {
		case key.Matches(msg, m.keys.Confirm):
			m.confirmingCancel = false
			m.running = false
			m.canceled = true
			m.store.status = "cancel requested"
			if m.cancel != nil {
				m.cancel()
			}
			return m, tea.Quit
		case key.Matches(msg, m.keys.Cancel):
			m.confirmingCancel = false
			return m, nil
		}
	}

	switch {
	case key.Matches(msg, m.keys.Help):
		m.help.ShowAll = !m.help.ShowAll
	case key.Matches(msg, m.keys.Cancel):
		m.help.ShowAll = false
		m.confirmingCancel = false
	case key.Matches(msg, m.keys.Quit):
		if m.running && !m.completed && !m.canceled && !m.failed {
			m.confirmingCancel = true
			return m, nil
		}
		return m, tea.Quit
	case key.Matches(msg, m.keys.FocusNext):
		m.focus = (m.focus + 1) % 4
	case key.Matches(msg, m.keys.FocusPrev):
		m.focus = (m.focus + 3) % 4
	case key.Matches(msg, m.keys.Summary):
		m.pane = paneSummary
	case key.Matches(msg, m.keys.TTFT):
		m.pane = paneTTFT
	case key.Matches(msg, m.keys.E2E):
		m.pane = paneE2E
	case key.Matches(msg, m.keys.Waterfall):
		m.pane = paneWaterfall
	}

	return m, nil
}

func (m model) render() string {
	return renderDashboard(m)
}

func formatMetricValue(value *float64) string {
	if value == nil {
		return "-"
	}
	return fmt.Sprintf("%.1f", *value)
}

func formatStringIntMap(values map[string]int) string {
	if len(values) == 0 {
		return "-"
	}
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	parts := make([]string, 0, len(keys))
	for _, key := range keys {
		parts = append(parts, fmt.Sprintf("%s=%d", key, values[key]))
	}
	return strings.Join(parts, ",")
}

func (m model) windowTitle() string {
	if m.store.benchmarkName != "" {
		return "what-ttft - " + m.store.benchmarkName
	}
	if m.store.model != "" {
		return "what-ttft - " + m.store.model
	}
	return "what-ttft"
}
