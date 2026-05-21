package tui

import (
	"fmt"
	"strings"

	"github.com/gabrielmbmb/what-ttft/pkg/whatttft"

	"charm.land/bubbles/v2/help"
	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
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

	keys   keyMap
	help   help.Model
	styles styles
}

func newModel(events <-chan whatttft.RunEvent) model {
	helpModel := help.New()
	helpModel.ShowAll = false

	return model{
		events: events,
		store:  newLiveStore(),
		keys:   defaultKeyMap(),
		help:   helpModel,
		styles: defaultStyles(),
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
	progress := m.store.progress()
	status := m.renderStatus()
	if m.confirmingCancel {
		status = m.styles.error.Render("Cancel the running benchmark? Press y to confirm or n/esc to continue.")
	}

	sections := []string{
		m.renderHeader(),
		m.styles.section.Render(fmt.Sprintf(
			"progress: %d/%d completed  active=%d  ok=%d  err=%d  warmup=%d measured=%d",
			progress.Completed,
			progress.Total,
			progress.Active,
			progress.Successful,
			progress.Errors,
			progress.Warmup,
			progress.Measured,
		)),
		m.styles.section.Render(m.renderPane()),
		status,
	}
	if m.store.reportStatus != "" {
		sections = append(sections, m.styles.muted.Render("reports: "+m.store.reportStatus))
	}
	if m.store.outputDir != "" {
		sections = append(sections, m.styles.muted.Render("output: "+m.store.outputDir))
	}
	sections = append(sections, m.help.View(m.keys))

	return lipgloss.JoinVertical(lipgloss.Left, sections...)
}

func (m model) renderHeader() string {
	name := firstNonEmpty(m.store.benchmarkName, m.store.scenarioName, "what-ttft")
	parts := []string{m.styles.title.Render("what-ttft"), m.styles.muted.Render(name)}
	if m.store.currentTarget() != "-" {
		parts = append(parts, "target="+m.store.currentTarget())
	}
	if m.store.provider != "" || m.store.model != "" {
		parts = append(parts, strings.TrimSpace(m.store.provider+" "+m.store.model))
	}
	if m.width > 0 && m.height > 0 {
		parts = append(parts, fmt.Sprintf("%dx%d", m.width, m.height))
	}

	return strings.Join(parts, "  ")
}

func (m model) renderPane() string {
	switch m.pane {
	case paneTTFT:
		return "TTFT distribution placeholder\nrequest records: " + fmt.Sprint(len(m.store.records))
	case paneE2E:
		return "E2E/TPS distribution placeholder\nrequest records: " + fmt.Sprint(len(m.store.records))
	case paneWaterfall:
		return "slowest-request waterfall placeholder\nrequest records: " + fmt.Sprint(len(m.store.records))
	default:
		return fmt.Sprintf("summary pane  focus=%d  records=%d  target=%s", m.focus, len(m.store.records), m.store.currentTarget())
	}
}

func (m model) renderStatus() string {
	if m.failed {
		return m.styles.error.Render("status: error " + m.store.lastError)
	}
	if m.canceled {
		return m.styles.error.Render("status: canceled")
	}
	if m.completed {
		return m.styles.status.Render("status: completed; press q to exit")
	}
	if m.running {
		return m.styles.status.Render("status: running")
	}
	if m.channelClosed {
		return m.styles.muted.Render("status: event stream closed")
	}
	if m.store.status != "" {
		return m.styles.status.Render("status: " + m.store.status)
	}
	return m.styles.muted.Render("status: waiting for benchmark events")
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

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}
