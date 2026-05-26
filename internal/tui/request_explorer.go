package tui

import (
	"fmt"
	"strings"

	"github.com/gabrielmbmb/what-ttft/pkg/whatttft"

	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"
)

type requestExplorerMode int

const (
	requestExplorerModeList requestExplorerMode = iota
	requestExplorerModeDetail
	requestExplorerModeFilter
)

type requestExplorerState struct {
	CursorRequestID string
	Offset          int
	Mode            requestExplorerMode
	PreviousPane    pane
	FilterInput     string
	CommittedFilter string
}

func (m *model) openRequestExplorer() {
	if m.pane != paneRequests {
		m.requestExplorer.PreviousPane = m.pane
	}
	m.pane = paneRequests
	m.store.setTargetDetail(false)
	m.requestExplorer.Mode = requestExplorerModeList
	m.requestExplorer.ensureCursor(m.store.completedRecords())
}

func (m model) updateRequestExplorerKey(msg tea.KeyPressMsg) (model, bool) {
	records := m.store.completedRecords()
	pageSize := requestExplorerPageSize(m.height)

	if m.requestExplorer.Mode == requestExplorerModeFilter {
		return m.updateRequestExplorerFilterKey(msg), true
	}

	switch {
	case key.Matches(msg, m.keys.ExplorerBack):
		if m.requestExplorer.Mode == requestExplorerModeDetail {
			m.requestExplorer.Mode = requestExplorerModeList
			return m, true
		}
		m.pane = m.requestExplorer.previousPaneOrSummary()
		m.requestExplorer.Mode = requestExplorerModeList
		return m, true
	case key.Matches(msg, m.keys.FilterRequests):
		m.requestExplorer.Mode = requestExplorerModeFilter
		m.requestExplorer.FilterInput = m.requestExplorer.CommittedFilter
		return m, true
	case key.Matches(msg, m.keys.ClearFilter):
		m.requestExplorer.FilterInput = ""
		m.requestExplorer.CommittedFilter = ""
		m.requestExplorer.Offset = 0
		m.requestExplorer.ensureCursor(records)
		return m, true
	case key.Matches(msg, m.keys.TargetUp):
		m.requestExplorer.move(records, -1, pageSize)
		return m, true
	case key.Matches(msg, m.keys.TargetDown):
		m.requestExplorer.move(records, 1, pageSize)
		return m, true
	case key.Matches(msg, m.keys.PageUp):
		m.requestExplorer.move(records, -pageSize, pageSize)
		return m, true
	case key.Matches(msg, m.keys.PageDown):
		m.requestExplorer.move(records, pageSize, pageSize)
		return m, true
	case key.Matches(msg, m.keys.Home):
		m.requestExplorer.jump(records, 0, pageSize)
		return m, true
	case key.Matches(msg, m.keys.End):
		m.requestExplorer.jump(records, len(records)-1, pageSize)
		return m, true
	case key.Matches(msg, m.keys.Enter):
		if len(records) > 0 {
			m.requestExplorer.ensureCursor(records)
			m.requestExplorer.Mode = requestExplorerModeDetail
		}
		return m, true
	}

	return m, false
}

func (m model) updateRequestExplorerFilterKey(msg tea.KeyPressMsg) model {
	switch {
	case key.Matches(msg, m.keys.Enter):
		m.requestExplorer.CommittedFilter = strings.TrimSpace(m.requestExplorer.FilterInput)
		m.requestExplorer.Mode = requestExplorerModeList
		m.requestExplorer.Offset = 0
		m.requestExplorer.ensureCursor(m.store.completedRecords())
	case key.Matches(msg, m.keys.ExplorerBack):
		m.requestExplorer.FilterInput = m.requestExplorer.CommittedFilter
		m.requestExplorer.Mode = requestExplorerModeList
	case key.Matches(msg, m.keys.ClearFilter):
		m.requestExplorer.FilterInput = ""
	case tea.Key(msg).Code == tea.KeyBackspace:
		m.requestExplorer.FilterInput = trimLastRune(m.requestExplorer.FilterInput)
	case tea.Key(msg).Text != "":
		m.requestExplorer.FilterInput += tea.Key(msg).Text
	}
	return m
}

func (s requestExplorerState) previousPaneOrSummary() pane {
	if s.PreviousPane == paneRequests || s.PreviousPane < paneSummary || s.PreviousPane >= paneCount {
		return paneSummary
	}
	return s.PreviousPane
}

func (s *requestExplorerState) ensureCursor(records []whatttft.RequestRecord) {
	if len(records) == 0 {
		s.CursorRequestID = ""
		s.Offset = 0
		return
	}
	if requestRecordIndex(records, s.CursorRequestID) >= 0 {
		return
	}
	s.CursorRequestID = records[0].RequestID
	s.Offset = 0
}

func (s *requestExplorerState) move(records []whatttft.RequestRecord, delta int, pageSize int) {
	if len(records) == 0 {
		s.ensureCursor(records)
		return
	}
	index := requestRecordIndex(records, s.CursorRequestID)
	if index < 0 {
		index = 0
	}
	s.jump(records, index+delta, pageSize)
}

func (s *requestExplorerState) jump(records []whatttft.RequestRecord, index int, pageSize int) {
	if len(records) == 0 {
		s.ensureCursor(records)
		return
	}
	if index < 0 {
		index = 0
	}
	if index >= len(records) {
		index = len(records) - 1
	}
	s.CursorRequestID = records[index].RequestID
	s.Offset = requestExplorerOffsetForSelection(s.Offset, index, pageSize, len(records))
}

func requestExplorerOffsetForSelection(offset int, selected int, pageSize int, count int) int {
	if pageSize < 1 {
		pageSize = 1
	}
	if selected < offset {
		offset = selected
	}
	if selected >= offset+pageSize {
		offset = selected - pageSize + 1
	}
	maxOffset := count - pageSize
	if maxOffset < 0 {
		maxOffset = 0
	}
	if offset > maxOffset {
		offset = maxOffset
	}
	if offset < 0 {
		offset = 0
	}
	return offset
}

func requestExplorerPageSize(height int) int {
	pageSize := height - 18
	if pageSize < 3 {
		pageSize = 3
	}
	if pageSize > 25 {
		pageSize = 25
	}
	return pageSize
}

func requestRecordIndex(records []whatttft.RequestRecord, requestID string) int {
	for index, record := range records {
		if record.RequestID == requestID {
			return index
		}
	}
	return -1
}

func renderRequestExplorer(store liveStore, state requestExplorerState, width int, height int, theme tuiTheme) string {
	if state.Mode == requestExplorerModeDetail {
		return renderRequestExplorerDetail(store, state, width, height, theme)
	}
	return renderRequestExplorerList(store, state, width, height, theme)
}

func renderRequestExplorerList(store liveStore, state requestExplorerState, width int, height int, theme tuiTheme) string {
	rows := store.requestRows()
	bodyWidth := panelInnerWidth(width)
	bodyHeight := panelInnerHeight(height)
	if len(rows) == 0 {
		body := "no requests completed yet\nrequests appear here after request_finished events\nkeys: esc overview  / filter  enter detail"
		return panel("Requests", fitToBox(body, bodyWidth, bodyHeight), width, height, theme, roleAccent)
	}

	selected := requestRowIndex(rows, state.CursorRequestID)
	if selected < 0 {
		selected = 0
	}
	pageSize := bodyHeight - 3
	if pageSize < 1 {
		pageSize = 1
	}
	offset := requestExplorerOffsetForSelection(state.Offset, selected, pageSize, len(rows))
	end := offset + pageSize
	if end > len(rows) {
		end = len(rows)
	}

	filterLabel := state.CommittedFilter
	if state.Mode == requestExplorerModeFilter {
		filterLabel = state.FilterInput + "_"
	}
	if strings.TrimSpace(filterLabel) == "" {
		filterLabel = "none"
	}
	lines := []string{
		fmt.Sprintf("requests=%d  selected=%d/%d  filter=%s  sort=completion-order", len(rows), selected+1, len(rows), safeInline(filterLabel)),
		requestExplorerHeader(store.IsBenchmark(), bodyWidth),
	}
	for index := offset; index < end; index++ {
		lines = append(lines, requestExplorerRow(rows[index], index == selected, store.IsBenchmark(), bodyWidth))
	}
	if state.Mode == requestExplorerModeFilter {
		lines = append(lines, "filter: enter apply  esc discard  ctrl+u clear")
	} else {
		lines = append(lines, "keys: ↑/↓ row  pgup/pgdn page  enter detail  / filter  esc overview")
	}
	return panel("Requests", fitToBox(strings.Join(lines, "\n"), bodyWidth, bodyHeight), width, height, theme, roleAccent)
}

func requestExplorerHeader(benchmark bool, width int) string {
	if benchmark && width >= 96 {
		return fmt.Sprintf("%-2s %-5s %-24s %-14s %-16s %-7s %-5s %8s %8s %8s", "", "#", "request", "target", "model", "phase", "ok", "ttft", "e2e", "http")
	}
	return fmt.Sprintf("%-2s %-5s %-24s %-8s %-5s %8s %8s %8s", "", "#", "request", "phase", "ok", "ttft", "e2e", "http")
}

func requestExplorerRow(row requestRow, selected bool, benchmark bool, width int) string {
	marker := " "
	if selected {
		marker = "›"
	}
	outcome := row.Outcome
	if outcome == requestOutcomeError {
		outcome = "err"
	}
	phase := row.Phase
	if phase == requestPhaseMeasured {
		phase = "meas"
	}
	if phase == requestPhaseWarmup {
		phase = "warm"
	}
	requestID := truncateVisible(row.RequestID, 24)
	if benchmark && width >= 96 {
		target := truncateVisible(firstNonEmpty(row.TargetID, "-"), 14)
		model := truncateVisible(firstNonEmpty(row.Model, "-"), 16)
		return fmt.Sprintf("%-2s %-5d %-24s %-14s %-16s %-7s %-5s %8s %8s %8s", marker, row.Ordinal+1, requestID, target, model, phase, outcome, formatMetricValue(row.TTFTMS), formatMetricValue(row.E2EMS), row.HTTPStatus)
	}
	return fmt.Sprintf("%-2s %-5d %-24s %-8s %-5s %8s %8s %8s", marker, row.Ordinal+1, requestID, phase, outcome, formatMetricValue(row.TTFTMS), formatMetricValue(row.E2EMS), row.HTTPStatus)
}

func renderRequestExplorerDetail(store liveStore, state requestExplorerState, width int, height int, theme tuiTheme) string {
	records := store.completedRecords()
	selected := requestRecordIndex(records, state.CursorRequestID)
	if selected < 0 && len(records) > 0 {
		selected = 0
	}
	if selected < 0 {
		return panel("Request detail", "no request selected\nesc requests", width, height, theme, roleAccent)
	}
	record := records[selected]
	bodyWidth := panelInnerWidth(width)
	bodyHeight := panelInnerHeight(height)
	lines := []string{
		fmt.Sprintf("request=%s  #%d/%d  attempt=%d  phase=%s", safeInline(record.RequestID), selected+1, len(records), record.Attempt, requestPhase(record)),
		fmt.Sprintf("target=%s  model=%s  provider=%s  protocol=%s", safeInline(firstNonEmpty(record.TargetID, "-")), safeInline(firstNonEmpty(record.Model, "-")), safeInline(firstNonEmpty(record.Provider, "-")), safeInline(firstNonEmpty(record.HTTP.Protocol, "-"))),
		fmt.Sprintf("outcome=%s  http=%s  finish=%s", requestOutcome(record), requestHTTPStatus(record), safeInline(firstNonEmpty(requestFinishReason(record), "-"))),
		fmt.Sprintf("ttft_delta_ms=%s  e2e_delta_ms=%s  stream_total_ms=%s  http_ttfb_ms=%s", formatMetricValue(record.Derived.TTFTDeltaMS), formatMetricValue(record.Derived.E2EDeltaMS), formatMetricValue(record.Derived.StreamTotalMS), formatMetricValue(record.Derived.HTTPTTFBMS)),
		fmt.Sprintf("e2e_output_tps=%s  generation_delta_output_tps=%s", formatMetricValue(record.Derived.E2EOutputTPS), formatMetricValue(record.Derived.GenerationDeltaOutputTPS)),
		fmt.Sprintf("prompt_tokens=%s  completion_tokens=%s  total_tokens=%s", formatIntPointer(record.PromptTokens), formatIntPointer(record.CompletionTokens), formatIntPointer(record.TotalTokens)),
		fmt.Sprintf("cache=%s  conn=%s  protocol=%s", requestCacheState(record), requestConnState(record), safeInline(firstNonEmpty(record.HTTP.Protocol, "-"))),
	}
	if record.Error != nil {
		lines = append(lines, fmt.Sprintf("error_category=%s  retryable=%t", safeInline(firstNonEmpty(record.Error.Category, "unknown")), record.Error.Retryable))
		if record.Error.Message != "" {
			lines = append(lines, "error_message="+safeInline(record.Error.Message))
		}
	}
	lines = append(lines, "keys: esc request list  [/]=filters later  output requires --save-chunks")
	return panel("Request detail", fitToBox(strings.Join(lines, "\n"), bodyWidth, bodyHeight), width, height, theme, roleAccent)
}

func requestPhase(record whatttft.RequestRecord) string {
	if record.Warmup {
		return "warmup"
	}
	return "measured"
}

func requestOutcome(record whatttft.RequestRecord) string {
	if record.Error != nil {
		return "error"
	}
	return "ok"
}

func requestHTTPStatus(record whatttft.RequestRecord) string {
	if record.HTTP.StatusCode == 0 {
		return "-"
	}
	return fmt.Sprintf("%d", record.HTTP.StatusCode)
}

func requestFinishReason(record whatttft.RequestRecord) string {
	if record.Error != nil && record.Error.Category != "" {
		return record.Error.Category
	}
	return ""
}

func requestCacheState(record whatttft.RequestRecord) string {
	if record.Cache.PromptCachedTokens != nil {
		if *record.Cache.PromptCachedTokens > 0 {
			return "hit"
		}
		return "miss"
	}
	if record.Cache.Hit != nil {
		if *record.Cache.Hit {
			return "hit"
		}
		return "miss"
	}
	return "unknown"
}

func requestConnState(record whatttft.RequestRecord) string {
	if !record.HTTP.GotConn {
		return "unknown"
	}
	if record.HTTP.ConnReused {
		return "reused"
	}
	return "new"
}

func formatIntPointer(value *int) string {
	if value == nil {
		return "-"
	}
	return fmt.Sprintf("%d", *value)
}

func trimLastRune(value string) string {
	if value == "" {
		return ""
	}
	runes := []rune(value)
	return string(runes[:len(runes)-1])
}
