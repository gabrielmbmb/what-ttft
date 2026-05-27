package tui

import (
	"fmt"
	"strings"

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
	DetailSection   requestDetailSection
	PreviousPane    pane
	FilterInput     string
	CommittedFilter string
	FilterError     string
	Filters         requestFilters
	Sort            requestSort
}

func (m *model) openRequestExplorer() {
	if m.pane != paneRequests {
		m.requestExplorer.PreviousPane = m.pane
	}
	m.pane = paneRequests
	m.store.setTargetDetail(false)
	m.requestExplorer.Mode = requestExplorerModeList
	m.requestExplorer.ensureCursor(m.store.requestRows())
}

func (m model) updateRequestExplorerKey(msg tea.KeyPressMsg) (model, bool) {
	rows, _, _ := requestExplorerRows(m.store, m.requestExplorer)
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
	case m.requestExplorer.Mode == requestExplorerModeDetail && key.Matches(msg, m.keys.DetailSectionPrev):
		m.requestExplorer.moveDetailSection(-1)
		return m, true
	case m.requestExplorer.Mode == requestExplorerModeDetail && key.Matches(msg, m.keys.DetailSectionNext):
		m.requestExplorer.moveDetailSection(1)
		return m, true
	case m.requestExplorer.Mode == requestExplorerModeDetail && key.Matches(msg, m.keys.OutputSection):
		m.requestExplorer.DetailSection = requestDetailSectionOutput
		return m, true
	case key.Matches(msg, m.keys.FilterRequests):
		m.requestExplorer.Mode = requestExplorerModeFilter
		m.requestExplorer.FilterInput = m.requestExplorer.CommittedFilter
		return m, true
	case key.Matches(msg, m.keys.ClearFilter):
		previousOrdinal := requestExplorerSelectedOrdinal(rows, m.requestExplorer.CursorRequestID)
		m.requestExplorer.clearFilters()
		rows, _, _ = requestExplorerRows(m.store, m.requestExplorer)
		m.requestExplorer.ensureCursorNear(rows, previousOrdinal, pageSize)
		return m, true
	case key.Matches(msg, m.keys.SortRequests):
		previousOrdinal := requestExplorerSelectedOrdinal(rows, m.requestExplorer.CursorRequestID)
		m.requestExplorer.cycleSort()
		rows, _, _ = requestExplorerRows(m.store, m.requestExplorer)
		m.requestExplorer.ensureCursorNear(rows, previousOrdinal, pageSize)
		return m, true
	case key.Matches(msg, m.keys.ToggleErrors):
		previousOrdinal := requestExplorerSelectedOrdinal(rows, m.requestExplorer.CursorRequestID)
		m.requestExplorer.toggleErrorsOnly()
		rows, _, _ = requestExplorerRows(m.store, m.requestExplorer)
		m.requestExplorer.ensureCursorNear(rows, previousOrdinal, pageSize)
		return m, true
	case key.Matches(msg, m.keys.CycleRequestPhase):
		previousOrdinal := requestExplorerSelectedOrdinal(rows, m.requestExplorer.CursorRequestID)
		m.requestExplorer.cyclePhaseFilter()
		rows, _, _ = requestExplorerRows(m.store, m.requestExplorer)
		m.requestExplorer.ensureCursorNear(rows, previousOrdinal, pageSize)
		return m, true
	case key.Matches(msg, m.keys.TargetUp):
		m.requestExplorer.move(rows, -1, pageSize)
		return m, true
	case key.Matches(msg, m.keys.TargetDown):
		m.requestExplorer.move(rows, 1, pageSize)
		return m, true
	case key.Matches(msg, m.keys.PageUp):
		m.requestExplorer.move(rows, -pageSize, pageSize)
		return m, true
	case key.Matches(msg, m.keys.PageDown):
		m.requestExplorer.move(rows, pageSize, pageSize)
		return m, true
	case key.Matches(msg, m.keys.Home):
		m.requestExplorer.jump(rows, 0, pageSize)
		return m, true
	case key.Matches(msg, m.keys.End):
		m.requestExplorer.jump(rows, len(rows)-1, pageSize)
		return m, true
	case key.Matches(msg, m.keys.Enter):
		if len(rows) > 0 {
			m.requestExplorer.ensureCursor(rows)
			m.requestExplorer.Mode = requestExplorerModeDetail
		}
		return m, true
	}

	return m, false
}

func (m model) updateRequestExplorerFilterKey(msg tea.KeyPressMsg) model {
	switch {
	case key.Matches(msg, m.keys.Enter):
		previousRows, _, _ := requestExplorerRows(m.store, m.requestExplorer)
		previousOrdinal := requestExplorerSelectedOrdinal(previousRows, m.requestExplorer.CursorRequestID)
		if err := m.requestExplorer.applyFilterInput(); err != nil {
			return m
		}
		m.requestExplorer.Mode = requestExplorerModeList
		m.requestExplorer.Offset = 0
		rows, _, _ := requestExplorerRows(m.store, m.requestExplorer)
		m.requestExplorer.ensureCursorNear(rows, previousOrdinal, requestExplorerPageSize(m.height))
	case key.Matches(msg, m.keys.ExplorerBack):
		m.requestExplorer.FilterInput = m.requestExplorer.CommittedFilter
		m.requestExplorer.FilterError = ""
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

func (s *requestExplorerState) applyFilterInput() error {
	filters, sortOrder, err := parseRequestFilterQuery(s.FilterInput)
	if err != nil {
		s.FilterError = err.Error()
		return err
	}
	s.Filters = filters
	s.Sort = sortOrder
	s.CommittedFilter = strings.TrimSpace(s.FilterInput)
	s.FilterError = ""
	return nil
}

func (s *requestExplorerState) clearFilters() {
	s.Filters = requestFilters{RespectChartVisibility: true}
	s.Sort = requestSortCompletionOrder
	s.CommittedFilter = ""
	s.FilterInput = ""
	s.FilterError = ""
	s.Offset = 0
}

func (s *requestExplorerState) cycleSort() {
	sorts := []requestSort{requestSortCompletionOrder, requestSortSlowestTTFT, requestSortSlowestE2E, requestSortErrorsFirst, requestSortTargetOrder}
	current := normalizedRequestSort(s.Sort)
	next := sorts[0]
	for index, candidate := range sorts {
		if candidate == current {
			next = sorts[(index+1)%len(sorts)]
			break
		}
	}
	s.Sort = next
	s.CommittedFilter = renderRequestFilterQuery(s.Filters.withDefaultVisibility(), s.Sort)
	s.FilterInput = s.CommittedFilter
	s.FilterError = ""
	s.Offset = 0
}

func (s *requestExplorerState) toggleErrorsOnly() {
	filters := s.Filters.withDefaultVisibility()
	if len(filters.Outcomes) == 1 && filters.Outcomes[requestOutcomeError] {
		filters.Outcomes = nil
	} else {
		filters.Outcomes = map[string]bool{requestOutcomeError: true}
	}
	s.Filters = filters
	s.CommittedFilter = renderRequestFilterQuery(s.Filters, normalizedRequestSort(s.Sort))
	s.FilterInput = s.CommittedFilter
	s.FilterError = ""
	s.Offset = 0
}

func (s *requestExplorerState) cyclePhaseFilter() {
	filters := s.Filters.withDefaultVisibility()
	next := requestPhaseMeasured
	if len(filters.Phases) == 1 && filters.Phases[requestPhaseMeasured] {
		next = requestPhaseWarmup
	} else if len(filters.Phases) == 1 && filters.Phases[requestPhaseWarmup] {
		filters.Phases = nil
		s.Filters = filters
		s.CommittedFilter = renderRequestFilterQuery(s.Filters, normalizedRequestSort(s.Sort))
		s.FilterInput = s.CommittedFilter
		s.FilterError = ""
		s.Offset = 0
		return
	}
	filters.Phases = map[string]bool{next: true}
	s.Filters = filters
	s.CommittedFilter = renderRequestFilterQuery(s.Filters, normalizedRequestSort(s.Sort))
	s.FilterInput = s.CommittedFilter
	s.FilterError = ""
	s.Offset = 0
}

func (s requestExplorerState) previousPaneOrSummary() pane {
	if s.PreviousPane == paneRequests || s.PreviousPane < paneSummary || s.PreviousPane >= paneCount {
		return paneSummary
	}
	return s.PreviousPane
}

func (s *requestExplorerState) ensureCursor(rows []requestRow) {
	if len(rows) == 0 {
		s.CursorRequestID = ""
		s.Offset = 0
		return
	}
	if requestRowIndex(rows, s.CursorRequestID) >= 0 {
		return
	}
	s.CursorRequestID = rows[0].RequestID
	s.Offset = 0
}

func (s *requestExplorerState) ensureCursorNear(rows []requestRow, previousOrdinal int, pageSize int) {
	if len(rows) == 0 {
		s.ensureCursor(rows)
		return
	}
	if index := requestRowIndex(rows, s.CursorRequestID); index >= 0 {
		s.Offset = requestExplorerOffsetForSelection(s.Offset, index, pageSize, len(rows))
		return
	}
	if previousOrdinal < 0 {
		s.ensureCursor(rows)
		return
	}
	closest := 0
	closestDistance := absInt(rows[0].Ordinal - previousOrdinal)
	for index := 1; index < len(rows); index++ {
		distance := absInt(rows[index].Ordinal - previousOrdinal)
		if distance < closestDistance || distance == closestDistance && rows[index].Ordinal < rows[closest].Ordinal {
			closest = index
			closestDistance = distance
		}
	}
	s.CursorRequestID = rows[closest].RequestID
	s.Offset = requestExplorerOffsetForSelection(s.Offset, closest, pageSize, len(rows))
}

func requestExplorerSelectedOrdinal(rows []requestRow, requestID string) int {
	index := requestRowIndex(rows, requestID)
	if index < 0 {
		return -1
	}
	return rows[index].Ordinal
}

func absInt(value int) int {
	if value < 0 {
		return -value
	}
	return value
}

func (s *requestExplorerState) move(rows []requestRow, delta int, pageSize int) {
	if len(rows) == 0 {
		s.ensureCursor(rows)
		return
	}
	index := requestRowIndex(rows, s.CursorRequestID)
	if index < 0 {
		index = 0
	}
	s.jump(rows, index+delta, pageSize)
}

func (s *requestExplorerState) jump(rows []requestRow, index int, pageSize int) {
	if len(rows) == 0 {
		s.ensureCursor(rows)
		return
	}
	if index < 0 {
		index = 0
	}
	if index >= len(rows) {
		index = len(rows) - 1
	}
	s.CursorRequestID = rows[index].RequestID
	s.Offset = requestExplorerOffsetForSelection(s.Offset, index, pageSize, len(rows))
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

func renderRequestExplorer(store liveStore, state requestExplorerState, width int, height int, theme tuiTheme) string {
	if state.Mode == requestExplorerModeDetail {
		return renderRequestExplorerDetail(store, state, width, height, theme)
	}
	return renderRequestExplorerList(store, state, width, height, theme)
}

func renderRequestExplorerList(store liveStore, state requestExplorerState, width int, height int, theme tuiTheme) string {
	rows, totalRows, hiddenRows := requestExplorerRows(store, state)
	bodyWidth := panelInnerWidth(width)
	bodyHeight := panelInnerHeight(height)
	if totalRows == 0 {
		body := "no requests completed yet\nrequests appear here after request_finished events\nkeys: esc overview  / filter  enter detail"
		return panel("Requests", fitToBox(body, bodyWidth, bodyHeight), width, height, theme, roleAccent)
	}
	if len(rows) == 0 {
		body := renderRequestExplorerNoMatches(state, totalRows, hiddenRows)
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
	_, sortOrder := requestExplorerFiltersAndSort(state)
	lines := []string{
		renderRequestExplorerStatusLine(len(rows), totalRows, hiddenRows, selected, filterLabel, sortOrder),
		requestExplorerHeader(requestExplorerLayout(store.IsBenchmark(), bodyWidth)),
	}
	layout := requestExplorerLayout(store.IsBenchmark(), bodyWidth)
	for index := offset; index < end; index++ {
		lines = append(lines, requestExplorerRow(rows[index], index == selected, layout))
	}
	if state.Mode == requestExplorerModeFilter {
		if state.FilterError != "" {
			lines = append(lines, "filter error: "+requestFilterDisplay(state.FilterError))
		}
		lines = append(lines, "filter: enter apply  esc discard  ctrl+u clear")
	} else {
		lines = append(lines, "keys: ↑/↓ row  pgup/pgdn page  enter detail  / filter  s sort  e errors  w phase  esc overview")
	}
	return panel("Requests", fitToBox(strings.Join(lines, "\n"), bodyWidth, bodyHeight), width, height, theme, roleAccent)
}

type requestExplorerTableLayout int

const (
	requestExplorerLayoutCompact requestExplorerTableLayout = iota
	requestExplorerLayoutBench
	requestExplorerLayoutWide
)

func requestExplorerRows(store liveStore, state requestExplorerState) ([]requestRow, int, int) {
	rows := store.requestRows()
	totalRows := len(rows)
	hiddenRows := 0
	filters, sortOrder := requestExplorerFiltersAndSort(state)
	if store.IsBenchmark() && filters.RespectChartVisibility {
		visible := make([]requestRow, 0, len(rows))
		for _, row := range rows {
			if row.TargetID != "" && !store.targetVisible(row.TargetID) {
				hiddenRows++
				continue
			}
			visible = append(visible, row)
		}
		rows = visible
	} else if store.IsBenchmark() {
		for _, row := range rows {
			if row.TargetID != "" && !store.targetVisible(row.TargetID) {
				hiddenRows++
			}
		}
	}
	if strings.TrimSpace(state.CommittedFilter) != "" || !filters.isEmpty() {
		filtered := make([]requestRow, 0, len(rows))
		for _, row := range rows {
			if filters.matches(row) {
				filtered = append(filtered, row)
			}
		}
		rows = filtered
	}
	rows = sortRequestRows(rows, sortOrder)
	return rows, totalRows, hiddenRows
}

func requestExplorerFiltersAndSort(state requestExplorerState) (requestFilters, requestSort) {
	filters := state.Filters.withDefaultVisibility()
	sortOrder := normalizedRequestSort(state.Sort)
	if strings.TrimSpace(state.CommittedFilter) != "" && filters.isEmpty() && sortOrder == requestSortCompletionOrder {
		parsedFilters, parsedSort, err := parseRequestFilterQuery(state.CommittedFilter)
		if err == nil {
			filters = parsedFilters.withDefaultVisibility()
			sortOrder = normalizedRequestSort(parsedSort)
		}
	}
	return filters, sortOrder
}

func renderRequestExplorerNoMatches(state requestExplorerState, totalRows int, hiddenRows int) string {
	filterLabel := strings.TrimSpace(state.CommittedFilter)
	if filterLabel == "" {
		filterLabel = "none"
	}
	_, sortOrder := requestExplorerFiltersAndSort(state)
	lines := []string{
		fmt.Sprintf("no requests match current view  total=%d  hidden_by_chart=%d", totalRows, hiddenRows),
		"filter=" + requestFilterDisplay(filterLabel),
		"sort=" + string(sortOrder),
	}
	if hiddenRows > 0 {
		lines = append(lines, "bench chart model visibility is hiding some requests; leave request pane and press a to show all models")
	}
	lines = append(lines, "keys: esc overview  / filter  ctrl+u clear")
	return strings.Join(lines, "\n")
}

func renderRequestExplorerStatusLine(matchRows int, totalRows int, hiddenRows int, selected int, filterLabel string, sortOrder requestSort) string {
	parts := []string{
		fmt.Sprintf("requests=%d/%d", matchRows, totalRows),
		fmt.Sprintf("selected=%d/%d", selected+1, matchRows),
		"filter=" + requestFilterDisplay(filterLabel),
		"sort=" + string(normalizedRequestSort(sortOrder)),
	}
	if hiddenRows > 0 {
		parts = append(parts, fmt.Sprintf("hidden_by_chart=%d", hiddenRows))
	}
	return strings.Join(parts, "  ")
}

func requestExplorerLayout(benchmark bool, width int) requestExplorerTableLayout {
	if width >= 132 {
		return requestExplorerLayoutWide
	}
	if benchmark && width >= 96 {
		return requestExplorerLayoutBench
	}
	return requestExplorerLayoutCompact
}

func requestExplorerHeader(layout requestExplorerTableLayout) string {
	switch layout {
	case requestExplorerLayoutWide:
		return fmt.Sprintf("%-2s %-4s %-20s %-10s %-12s %-5s %-5s %-5s %-8s %7s %7s %7s %7s %7s %-6s %-5s %-6s %-7s", "", "#", "request", "target", "model", "phase", "out", "http", "err", "ttft", "e2e", "stream", "ttfb", "tps", "tokens", "cache", "conn", "output")
	case requestExplorerLayoutBench:
		return fmt.Sprintf("%-2s %-4s %-22s %-12s %-14s %-5s %-5s %-5s %7s %7s %7s", "", "#", "request", "target", "model", "phase", "out", "http", "ttft", "e2e", "tps")
	default:
		return fmt.Sprintf("%-2s %-3s %-12s %-4s %-3s %-4s %5s %5s %-8s", "", "#", "request", "ph", "out", "http", "ttft", "e2e", "model")
	}
}

func requestExplorerRow(row requestRow, selected bool, layout requestExplorerTableLayout) string {
	marker := requestExplorerMarker(row, selected)
	outcome := requestExplorerOutcomeLabel(row)
	phase := requestExplorerPhaseLabel(row)
	requestID := truncateVisible(row.RequestID, 22)
	switch layout {
	case requestExplorerLayoutWide:
		return fmt.Sprintf("%-2s %-4d %-20s %-10s %-12s %-5s %-5s %-5s %-8s %7s %7s %7s %7s %7s %-6s %-5s %-6s %-7s", marker, row.Ordinal+1, truncateVisible(row.RequestID, 20), truncateVisible(firstNonEmpty(row.TargetID, "-"), 10), truncateVisible(firstNonEmpty(row.Model, "-"), 12), phase, outcome, row.HTTPStatus, truncateVisible(firstNonEmpty(row.ErrorCategory, "-"), 8), formatMetricValue(row.TTFTMS), formatMetricValue(row.E2EMS), formatMetricValue(row.StreamTotalMS), formatMetricValue(row.TTFBMS), formatMetricValue(row.E2EOutputTPS), formatIntPointer(row.CompletionTokens), row.CacheState, row.Conn, row.OutputState)
	case requestExplorerLayoutBench:
		return fmt.Sprintf("%-2s %-4d %-22s %-12s %-14s %-5s %-5s %-5s %7s %7s %7s", marker, row.Ordinal+1, requestID, truncateVisible(firstNonEmpty(row.TargetID, "-"), 12), truncateVisible(firstNonEmpty(row.Model, "-"), 14), phase, outcome, row.HTTPStatus, formatMetricValue(row.TTFTMS), formatMetricValue(row.E2EMS), formatMetricValue(row.E2EOutputTPS))
	default:
		modelOrTarget := truncateVisible(firstNonEmpty(row.Model, row.TargetID, "-"), 8)
		return fmt.Sprintf("%-2s %-3d %-12s %-4s %-3s %-4s %5s %5s %-8s", marker, row.Ordinal+1, truncateVisible(row.RequestID, 12), phase, outcome, row.HTTPStatus, formatMetricValue(row.TTFTMS), formatMetricValue(row.E2EMS), modelOrTarget)
	}
}

func requestExplorerMarker(row requestRow, selected bool) string {
	cursor := " "
	if selected {
		cursor = "›"
	}
	flag := " "
	if row.Outcome == requestOutcomeError {
		flag = "!"
	}
	return cursor + flag
}

func requestExplorerOutcomeLabel(row requestRow) string {
	if row.Outcome == requestOutcomeError {
		return "err"
	}
	return "ok"
}

func requestExplorerPhaseLabel(row requestRow) string {
	if row.Phase == requestPhaseWarmup {
		return "warm"
	}
	return "meas"
}

func renderRequestExplorerDetail(store liveStore, state requestExplorerState, width int, height int, theme tuiTheme) string {
	return renderRequestDetail(store, state, width, height, theme)
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
