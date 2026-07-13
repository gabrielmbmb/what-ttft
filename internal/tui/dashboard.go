package tui

import (
	"fmt"
	"strings"

	"charm.land/lipgloss/v2"
	"github.com/gabrielmbmb/what-ttft/internal/tui/charts"
	"github.com/gabrielmbmb/what-ttft/pkg/whatttft"
)

const (
	defaultDashboardWidth  = 100
	defaultDashboardHeight = 30
	minimumChartHeight     = 1
	wideDashboardWidth     = 120
	wideDashboardHeight    = 32
	mediumDashboardWidth   = 80
	mediumDashboardHeight  = 20
)

type layoutBox struct {
	Width  int
	Height int
}

type dashboardLayout struct {
	Root    layoutBox
	Header  layoutBox
	Charts  layoutBox
	Metrics layoutBox
	Footer  layoutBox
}

func renderDashboard(m model) string {
	layout := calculateDashboardLayout(m.width, m.height, m.help.ShowAll)
	if m.failed {
		return renderFailureDialog(m, layout)
	}

	header := fitToBox(renderDashboardHeader(m, layout.Header.Width, layout.Header.Height, m.theme), layout.Header.Width, layout.Header.Height)
	footer := fitToBox(renderShortcutFooter(m, layout.Footer.Width, layout.Footer.Height, m.theme), layout.Footer.Width, layout.Footer.Height)
	if m.store.IsBenchmark() && m.pane == paneMetrics {
		metricsHeight := layout.Charts.Height + layout.Metrics.Height
		metrics := fitToBox(renderBenchMetricsPanel(m.store, layout.Root.Width, metricsHeight, dashboardStatusText(m), m.confirmingCancel, m.theme), layout.Root.Width, metricsHeight)
		return joinVerticalToHeight([]string{header, metrics, footer}, layout.Root.Width, layout.Root.Height)
	}

	chartArea := fitToBox(renderChartArea(m.store, layout.Charts.Width, layout.Charts.Height, m.pane, m.requestExplorer, m.theme), layout.Charts.Width, layout.Charts.Height)
	metrics := fitToBox(renderMetricsPanel(m.store, layout.Metrics.Width, layout.Metrics.Height, dashboardStatusText(m), m.confirmingCancel, m.theme), layout.Metrics.Width, layout.Metrics.Height)
	return joinVerticalToHeight([]string{header, chartArea, metrics, footer}, layout.Root.Width, layout.Root.Height)
}

func renderFailureDialog(m model, layout dashboardLayout) string {
	dialogWidth := min(84, layout.Root.Width-4)
	if dialogWidth < 20 || layout.Root.Height < 5 {
		return fitToBox("ERROR: "+safeInline(m.store.lastError), layout.Root.Width, layout.Root.Height)
	}

	message := safeInline(m.store.lastError)
	if message == "" {
		message = "The benchmark stopped before it could complete."
	}
	bodyLines := wrapVisibleWords(message, dialogWidth-8)
	bodyLines = append([]string{m.theme.render(roleBad, "The benchmark could not start or continue.")}, bodyLines...)
	bodyLines = append(bodyLines, "", "Press enter, q, or esc to exit.")
	dialogHeight := min(layout.Root.Height-2, len(bodyLines)+2)
	title := "Benchmark failed"
	if !m.store.IsBenchmark() {
		title = "Run failed"
	}
	dialog := panel(title, strings.Join(bodyLines, "\n"), dialogWidth, dialogHeight, m.theme, roleBad)
	return lipgloss.Place(layout.Root.Width, layout.Root.Height, lipgloss.Center, lipgloss.Center, dialog)
}

func wrapVisibleWords(value string, width int) []string {
	if width <= 0 {
		return nil
	}
	words := strings.Fields(value)
	if len(words) == 0 {
		return []string{""}
	}

	lines := make([]string, 0, len(words))
	line := ""
	for _, word := range words {
		candidate := word
		if line != "" {
			candidate = line + " " + word
		}
		if lipgloss.Width(candidate) <= width {
			line = candidate
			continue
		}
		if line != "" {
			lines = append(lines, line)
		}
		line = truncateVisible(word, width)
	}
	return append(lines, line)
}

func calculateDashboardLayout(width int, height int, helpVisible bool) dashboardLayout {
	if width <= 0 {
		width = defaultDashboardWidth
	}
	if height <= 0 {
		height = defaultDashboardHeight
	}

	footerHeight := 3
	if height < 14 {
		footerHeight = 2
	}
	if height < 8 {
		footerHeight = 1
	}
	if helpVisible && height >= 18 {
		footerHeight = 5
	} else if helpVisible && height >= 12 {
		footerHeight = 4
	}
	if footerHeight > height-minimumChartHeight {
		footerHeight = height - minimumChartHeight
	}
	if footerHeight < 0 {
		footerHeight = 0
	}
	availableHeight := height - footerHeight
	if availableHeight < minimumChartHeight {
		availableHeight = minimumChartHeight
	}

	headerHeight := 4
	metricsHeight := 12
	if width < wideDashboardWidth || height < wideDashboardHeight {
		headerHeight = 3
		metricsHeight = 12
	}
	if width < mediumDashboardWidth || height < mediumDashboardHeight {
		headerHeight = 2
		metricsHeight = 5
	}
	if height < 12 {
		headerHeight = 1
		metricsHeight = 4
	}
	if metricsHeight > availableHeight-headerHeight-minimumChartHeight {
		metricsHeight = availableHeight - headerHeight - minimumChartHeight
	}
	if metricsHeight < 1 {
		metricsHeight = 1
	}
	chartHeight := availableHeight - headerHeight - metricsHeight
	if chartHeight < minimumChartHeight {
		chartHeight = minimumChartHeight
	}
	if headerHeight+metricsHeight+chartHeight > availableHeight {
		chartHeight = availableHeight - headerHeight - metricsHeight
		if chartHeight < 0 {
			chartHeight = 0
		}
	}

	root := layoutBox{Width: width, Height: height}
	return dashboardLayout{
		Root:    root,
		Header:  layoutBox{Width: width, Height: headerHeight},
		Charts:  layoutBox{Width: width, Height: chartHeight},
		Metrics: layoutBox{Width: width, Height: metricsHeight},
		Footer:  layoutBox{Width: width, Height: footerHeight},
	}
}

func renderDashboardHeader(m model, width int, height int, theme tuiTheme) string {
	progress := m.store.Progress()
	context := dashboardContextLabels(m.store)
	status := dashboardStatusText(m)
	progressWidth := width / 3
	if progressWidth < 12 {
		progressWidth = 12
	}
	if progressWidth > 36 {
		progressWidth = 36
	}

	bodyLines := []string{
		statusPill(status, theme) + "  " + strings.Join(context, "  "),
		progressBar(progress.Completed, progress.Total, progressWidth, theme) + fmt.Sprintf("  active=%d  ok=%d  err=%d  warmup=%d  measured=%d", progress.Active, progress.Successful, progress.Errors, progress.Warmup, progress.Measured),
	}
	if height >= 4 {
		bodyLines = append(bodyLines, legend([]legendItem{{Label: "ttft_delta_ms", Role: roleChartTTFT}, {Label: "e2e_delta_ms", Role: roleChartE2E}, {Label: "TTFT histogram", Role: roleChartTTFT}, {Label: "e2e_output_tps", Role: roleChartTPS}}, theme))
	}
	title := "what-ttft run"
	if m.store.IsBenchmark() {
		title = "what-ttft bench"
	}
	return panel(title, strings.Join(bodyLines, "\n"), width, height, theme, roleAccent)
}

func dashboardContextLabels(store liveStore) []string {
	parts := make([]string, 0, 8)
	if store.IsBenchmark() {
		if store.benchmarkName != "" {
			parts = append(parts, "benchmark="+safeInline(store.benchmarkName))
		}
		parts = append(parts, "target_order="+safeInline(firstNonEmpty(store.targetOrder, string(whatttft.SerialTargetOrder))))
	}
	if store.provider != "" && !store.IsBenchmark() {
		parts = append(parts, "provider="+safeInline(store.provider))
	}
	if store.model != "" && !store.IsBenchmark() {
		parts = append(parts, "model="+safeInline(store.model))
	}
	if store.scenarioName != "" {
		parts = append(parts, "scenario="+safeInline(store.scenarioName))
	}
	if store.cacheMode != "" {
		parts = append(parts, "cache="+safeInline(string(store.cacheMode)))
	}
	if store.connectionMode != "" {
		parts = append(parts, "conn="+safeInline(string(store.connectionMode)))
	}
	if store.requestedServiceTier != "" {
		parts = append(parts, "tier="+safeInline(store.requestedServiceTier))
	}
	if store.CurrentTarget() != "-" {
		parts = append(parts, "target="+safeInline(store.CurrentTarget()))
	}
	if len(parts) == 0 {
		parts = append(parts, "waiting for benchmark events")
	}
	return parts
}

func renderChartArea(store liveStore, width int, height int, mode pane, requestExplorer requestExplorerState, theme tuiTheme) string {
	if height <= 0 || width <= 0 {
		return ""
	}
	if mode == paneRequests {
		return renderRequestExplorer(store, requestExplorer, width, height, theme)
	}
	if store.IsBenchmark() {
		return renderBenchChartArea(store, width, height, mode, theme)
	}

	switch mode {
	case paneTTFT:
		return renderFocusedTTFT(store, width, height, theme)
	case paneE2E:
		return renderFocusedE2E(store, width, height, theme)
	case paneWaterfall:
		return renderFocusedWaterfall(store, width, height, theme)
	default:
		return renderRunCharts(store, width, height, theme)
	}
}

func renderRunCharts(store liveStore, width int, height int, theme tuiTheme) string {
	if width >= wideDashboardWidth && height >= 16 {
		return renderWideOverview(store, width, height, theme)
	}
	if width >= mediumDashboardWidth && height >= 12 {
		return renderMediumOverview(store, width, height, theme)
	}
	return renderSmallOverview(store, width, height, theme)
}

func renderWideOverview(store liveStore, width int, height int, theme tuiTheme) string {
	gap := 1
	leftWidth := (width - gap) / 2
	rightWidth := width - gap - leftWidth
	topHeight := height / 2
	bottomHeight := height - topHeight
	top := joinColumnsWithGap(
		renderTTFTTrendPanel(store, leftWidth, topHeight, theme),
		renderE2ETrendPanel(store, rightWidth, topHeight, theme),
		width,
		topHeight,
		gap,
	)
	bottom := joinColumnsWithGap(
		renderDistributionPanel(store, leftWidth, bottomHeight, theme),
		renderTPSChartPanel(store, rightWidth, bottomHeight, theme),
		width,
		bottomHeight,
		gap,
	)
	return joinVerticalToHeight([]string{top, bottom}, width, height)
}

func renderMediumOverview(store liveStore, width int, height int, theme tuiTheme) string {
	if width >= 96 && height >= 15 {
		topHeight := min(height-5, max(8, height/2))
		bottomHeight := height - topHeight
		top := joinColumnsWithGap(
			renderTTFTTrendPanel(store, (width-1)/2, topHeight, theme),
			renderE2ETrendPanel(store, width-1-(width-1)/2, topHeight, theme),
			width,
			topHeight,
			1,
		)
		bottom := joinColumnsWithGap(
			renderDistributionPanel(store, (width-1)/2, bottomHeight, theme),
			renderTPSChartPanel(store, width-1-(width-1)/2, bottomHeight, theme),
			width,
			bottomHeight,
			1,
		)
		return joinVerticalToHeight([]string{top, bottom}, width, height)
	}

	ttftHeight := max(4, height/3)
	e2eHeight := max(4, height/3)
	remaining := height - ttftHeight - e2eHeight
	if remaining < 4 {
		remaining = 4
		ttftHeight = max(3, (height-remaining)/2)
		e2eHeight = height - remaining - ttftHeight
	}
	sections := []string{
		renderTTFTTrendPanel(store, width, ttftHeight, theme),
		renderE2ETrendPanel(store, width, e2eHeight, theme),
	}
	if remaining > 0 {
		if remaining >= 4 {
			tpsHeight := min(remaining-1, max(2, remaining/2))
			sections = append(sections, renderDistributionPanel(store, width, remaining-tpsHeight, theme), renderTPSChartPanel(store, width, tpsHeight, theme))
		} else {
			sections = append(sections, renderDistributionPanel(store, width, remaining, theme))
		}
	}
	return joinVerticalToHeight(sections, width, height)
}

func renderSmallOverview(store liveStore, width int, height int, theme tuiTheme) string {
	return renderTTFTTrendPanel(store, width, height, theme)
}

func renderFocusedTTFT(store liveStore, width int, height int, theme tuiTheme) string {
	if height >= 12 {
		topHeight := height * 2 / 3
		return joinVerticalToHeight([]string{
			renderTTFTTrendPanel(store, width, topHeight, theme),
			renderDistributionPanel(store, width, height-topHeight, theme),
		}, width, height)
	}
	return renderTTFTTrendPanel(store, width, height, theme)
}

func renderFocusedE2E(store liveStore, width int, height int, theme tuiTheme) string {
	if height >= 10 {
		topHeight := height * 2 / 3
		return joinVerticalToHeight([]string{
			renderE2ETrendPanel(store, width, topHeight, theme),
			renderTPSPanel(store, width, height-topHeight, theme),
		}, width, height)
	}
	return renderE2ETrendPanel(store, width, height, theme)
}

func renderFocusedWaterfall(store liveStore, width int, height int, theme tuiTheme) string {
	return renderWaterfallPanel(store, width, height, theme)
}

func renderTTFTTrendPanel(store liveStore, width int, height int, theme tuiTheme) string {
	body := charts.RenderSeriesChart(store.RunSeries(metricTTFTDeltaMS), charts.SeriesChartOptions{
		Width:      panelInnerWidth(width),
		Height:     panelInnerHeight(height),
		Title:      metricTTFTDeltaMS,
		Unit:       "ms",
		EmptyLabel: "waiting for first successful measured request",
	}, theme.chartTheme(roleChartTTFT))
	return panel("TTFT trend · ttft_delta_ms", body, width, height, theme, roleChartTTFT)
}

func renderE2ETrendPanel(store liveStore, width int, height int, theme tuiTheme) string {
	body := charts.RenderSeriesChart(store.RunSeries(metricE2EDeltaMS), charts.SeriesChartOptions{
		Width:      panelInnerWidth(width),
		Height:     panelInnerHeight(height),
		Title:      metricE2EDeltaMS,
		Unit:       "ms",
		EmptyLabel: "waiting for first successful measured request",
	}, theme.chartTheme(roleChartE2E))
	return panel("E2E trend · e2e_delta_ms", body, width, height, theme, roleChartE2E)
}

func renderDistributionPanel(store liveStore, width int, height int, theme tuiTheme) string {
	body := charts.RenderHistogramChart(store.RunSeries(metricTTFTDeltaMS), charts.HistogramOptions{
		Width:      panelInnerWidth(width),
		Height:     panelInnerHeight(height),
		Bins:       histogramBins(height),
		Title:      "TTFT distribution",
		Unit:       "ms",
		EmptyLabel: "waiting for first successful measured request",
	}, theme.chartTheme(roleChartTTFT))
	return panel("TTFT distribution · histogram", body, width, height, theme, roleChartTTFT)
}

func renderTPSChartPanel(store liveStore, width int, height int, theme tuiTheme) string {
	body := charts.RenderSeriesChart(store.RunSeries(metricE2EOutputTPS), charts.SeriesChartOptions{
		Width:      panelInnerWidth(width),
		Height:     panelInnerHeight(height),
		Title:      metricE2EOutputTPS,
		Unit:       "tokens/s",
		EmptyLabel: "TPS unavailable: provider usage not reported",
	}, theme.chartTheme(roleChartTPS))
	return panel("Output TPS trend · e2e_output_tps", body, width, height, theme, roleChartTPS)
}

func renderTPSPanel(store liveStore, width int, height int, theme tuiTheme) string {
	rows := store.MetricRows()
	e2eTPS := metricRowByName(rows, metricE2EOutputTPS)
	generationTPS := metricRowByName(rows, metricGenerationDeltaOutputTPS)
	bodyWidth := panelInnerWidth(width)
	bodyHeight := panelInnerHeight(height)
	if bodyWidth < 78 || bodyHeight < 3 {
		lines := []string{
			compactMetricLine("e2e_output_tps", e2eTPS),
			compactMetricLine("generation_delta_output_tps", generationTPS),
			renderRatesLine(store),
		}
		if e2eTPS.Count == 0 && generationTPS.Count == 0 {
			lines = append(lines, "TPS unavailable: provider usage not reported")
		}
		return panel("E2E/TPS focus", strings.Join(lines, "\n"), width, height, theme, roleChartTPS)
	}

	lines := []string{
		fmt.Sprintf("%-36s %5s  %-8s %-8s %-8s %-8s %s", "metric (successful measured reqs)", "count", "p50", "p95", "p99", "mean", "unit"),
		metricTableLine(e2eTPS),
		metricTableLine(generationTPS),
	}
	if bodyHeight >= 4 {
		lines = append(lines, "usage source: provider-reported or estimated")
	}
	if bodyHeight >= 5 {
		lines = append(lines, "run-level rates: "+renderRatesLine(store))
	}
	if e2eTPS.Count == 0 && generationTPS.Count == 0 {
		lines = append(lines, "TPS unavailable: provider usage not reported")
	}
	return panel("E2E/TPS focus", fitToBox(strings.Join(lines, "\n"), bodyWidth, bodyHeight), width, height, theme, roleChartTPS)
}

func renderWaterfallPanel(store liveStore, width int, height int, theme tuiTheme) string {
	body := renderWaterfallPreview(store, panelInnerWidth(width), panelInnerHeight(height), theme)
	return panel("Slowest request waterfall", body, width, height, theme, roleChartWaterfall)
}

func renderWaterfallPreview(store liveStore, width int, height int, theme tuiTheme) string {
	if width <= 0 || height <= 0 {
		return ""
	}
	slowest := store.SlowestRequests(1)
	if len(slowest) == 0 {
		return charts.RenderWaterfallChart(emptyRequestRecord(), charts.WaterfallOptions{Width: width, Height: height, EmptyLabel: "waterfall unavailable: timeline events missing"}, theme.chartTheme(roleChartWaterfall))
	}
	header := fmt.Sprintf("request=%s  metric=%s  value=%.1f ms", safeInline(slowest[0].RequestID), slowest[0].MetricName, slowest[0].ValueMS)
	if height <= 1 {
		return truncateVisible(header, width)
	}
	chart := charts.RenderWaterfallChart(slowest[0].Record, charts.WaterfallOptions{Width: width, Height: height - 1, Title: "waterfall ms"}, theme.chartTheme(roleChartWaterfall))
	return fitToBox(strings.Join([]string{header, chart}, "\n"), width, height)
}

func emptyRequestRecord() whatttft.RequestRecord {
	return whatttft.RequestRecord{}
}

func renderMetricsPanel(store liveStore, width int, height int, status string, confirmingCancel bool, theme tuiTheme) string {
	if height <= 0 || width <= 0 {
		return ""
	}
	if store.IsBenchmark() {
		return renderBenchMetricsPanel(store, width, height, status, confirmingCancel, theme)
	}
	body := renderMetricsBody(store, panelInnerWidth(width), panelInnerHeight(height), status, confirmingCancel)
	return panel("METRICS", body, width, height, theme, roleAccent)
}

func renderMetricsBody(store liveStore, width int, height int, status string, confirmingCancel bool) string {
	if height <= 0 || width <= 0 {
		return ""
	}
	if confirmingCancel {
		status = "Cancel the running benchmark? Press y to confirm or n/esc to continue."
	}

	if width < 86 || height <= 5 {
		return renderCompactMetricsBody(store, width, height, status)
	}

	metricLines := []string{fmt.Sprintf("%-36s %5s  %-8s %-8s %-8s %-8s %s", "metric (successful measured reqs)", "count", "p50", "p95", "p99", "mean", "unit")}
	for _, row := range orderedMetricRowsForPanel(store.MetricRows()) {
		metricLines = append(metricLines, metricTableLine(row))
	}
	footerLines := metricsFooterLines(store, status)
	lines := fitMetricsLines(metricLines, footerLines, height)
	return fitToBox(strings.Join(lines, "\n"), width, height)
}

func renderCompactMetricsBody(store liveStore, width int, height int, status string) string {
	rows := store.MetricRows()
	lines := []string{
		compactMetricLine(metricTTFTDeltaMS, metricRowByName(rows, metricTTFTDeltaMS)) + "  " + compactMetricLine(metricE2EDeltaMS, metricRowByName(rows, metricE2EDeltaMS)),
	}
	lines = append(lines, renderCompactProgressStatusLine(store, status))
	if tokenLine := renderTokenTotalsLine(store); tokenLine != "" {
		lines = append(lines, tokenLine)
	}
	lines = append(lines, renderRatesLine(store))
	if filterLine := renderBenchModelFilterLine(store); filterLine != "" {
		lines = append(lines, filterLine)
	}
	if metricRowByName(rows, metricE2EOutputTPS).Count == 0 && metricRowByName(rows, metricGenerationDeltaOutputTPS).Count == 0 {
		lines = append(lines, "TPS unavailable: provider usage not reported")
	}
	return fitToBox(strings.Join(lines, "\n"), width, height)
}

func metricsFooterLines(store liveStore, status string) []string {
	rows := store.MetricRows()
	footerLines := []string{renderTokenTotalsLine(store), renderRatesLine(store)}
	if footerLines[0] == "" {
		footerLines = footerLines[1:]
	}
	if metricRowByName(rows, metricE2EOutputTPS).Count == 0 && metricRowByName(rows, metricGenerationDeltaOutputTPS).Count == 0 {
		footerLines = append(footerLines, "TPS unavailable: provider usage not reported")
	}
	if filterLine := renderBenchModelFilterLine(store); filterLine != "" {
		footerLines = append(footerLines, filterLine)
	}
	footerLines = append(footerLines, renderProgressStatusLine(store, status))
	return footerLines
}

func renderBenchModelFilterLine(store liveStore) string {
	if !store.IsBenchmark() {
		return ""
	}
	rows := store.TargetRows()
	if len(rows) == 0 {
		return ""
	}
	visibleCount := 0
	for _, row := range rows {
		if row.Visible {
			visibleCount++
		}
	}
	selectedID := store.selectedTargetID()
	selectedLabel := selectedID
	if selectedLabel == "" {
		selectedLabel = "-"
	}
	labels := benchSeriesLabels(rows)
	if label := labels[selectedID]; label != "" {
		selectedLabel = label
	}
	selectedState := "off"
	if store.targetVisible(selectedID) {
		selectedState = "on"
	}
	return fmt.Sprintf("models shown=%d/%d  selected=%s[%s]  space=toggle after finish  a=show all", visibleCount, len(rows), safeInline(selectedLabel), selectedState)
}

func fitMetricsLines(metricLines []string, footerLines []string, height int) []string {
	if height <= 0 {
		return nil
	}
	if len(footerLines) >= height {
		return footerLines[len(footerLines)-height:]
	}
	metricBudget := height - len(footerLines)
	if metricBudget > len(metricLines) {
		metricBudget = len(metricLines)
	}
	lines := append([]string(nil), metricLines[:metricBudget]...)
	lines = append(lines, footerLines...)
	return lines
}

func compactMetricLine(label string, row metricRow) string {
	return fmt.Sprintf("%s p50=%s p95=%s p99=%s mean=%s", label, formatMetricValue(row.P50), formatMetricValue(row.P95), formatMetricValue(row.P99), formatMetricValue(row.Mean))
}

func metricTableLine(row metricRow) string {
	return fmt.Sprintf("%-36s %5d  %-8s %-8s %-8s %-8s %s", row.Name, row.Count, formatMetricValue(row.P50), formatMetricValue(row.P95), formatMetricValue(row.P99), formatMetricValue(row.Mean), row.Unit)
}

func orderedMetricRowsForPanel(rows []metricRow) []metricRow {
	order := []string{
		metricTTFTDeltaMS,
		metricE2EDeltaMS,
		metricE2EOutputTPS,
		metricGenerationDeltaOutputTPS,
		metricHTTPTTFBMS,
		metricProviderProcessingMS,
		metricServerWaitToFirstByteMS,
		metricCompletionTokens,
	}
	ordered := make([]metricRow, 0, len(rows))
	seen := make(map[string]struct{}, len(rows))
	for _, name := range order {
		row := metricRowByName(rows, name)
		if row.Name == "" {
			continue
		}
		ordered = append(ordered, row)
		seen[row.Name] = struct{}{}
	}
	for _, row := range rows {
		if _, ok := seen[row.Name]; ok {
			continue
		}
		ordered = append(ordered, row)
	}
	return ordered
}

func metricRowByName(rows []metricRow, name string) metricRow {
	for _, row := range rows {
		if row.Name == name {
			return row
		}
	}
	return metricRow{Name: name}
}

func renderTokenTotalsLine(store liveStore) string {
	total, records := storeCompletionTokens(store)
	if records == 0 {
		return ""
	}
	return fmt.Sprintf("completion_tokens_total=%d tokens  completion_token_records=%d", total, records)
}

func renderRatesLine(store liveStore) string {
	systemTPS, rps := storeRates(store)
	return "system_tps=" + formatMetricValue(systemTPS) + " tokens/s  rps=" + formatMetricValue(rps) + " req/s"
}

func renderCompactProgressStatusLine(store liveStore, status string) string {
	progress := store.Progress()
	parts := []string{
		"status=" + safeInline(stripStatusPrefix(status)),
		fmt.Sprintf("active=%d", progress.Active),
		fmt.Sprintf("completed=%d/%d", progress.Completed, progress.Total),
		fmt.Sprintf("ok=%d", progress.Successful),
		fmt.Sprintf("err=%d", progress.Errors),
	}
	return strings.Join(parts, "  ")
}

func renderProgressStatusLine(store liveStore, status string) string {
	progress := store.Progress()
	counts := store.StatusCounts()
	fields := []string{
		fmt.Sprintf("active=%d", progress.Active),
		fmt.Sprintf("completed=%d/%d", progress.Completed, progress.Total),
		fmt.Sprintf("ok=%d", progress.Successful),
		fmt.Sprintf("err=%d", progress.Errors),
		"statuses=" + formatStringIntMap(counts.StatusCodes),
		"errors=" + formatStringIntMap(counts.ErrorCategories),
	}
	if store.reportStatus != "" {
		fields = append(fields, "reports="+safeInline(store.reportStatus))
	}
	if store.outputDir != "" {
		fields = append(fields, "output="+safeInline(store.outputDir))
	}
	if strings.TrimSpace(status) != "" {
		fields = append(fields, "status="+safeInline(stripStatusPrefix(status)))
	}
	return strings.Join(fields, "  ")
}

func storeCompletionTokens(store liveStore) (int, int) {
	total := 0
	records := 0
	for _, group := range store.Groups() {
		total += group.TotalCompletionTokens
		records += group.CompletionTokenRecords
	}
	return total, records
}

func storeRates(store liveStore) (*float64, *float64) {
	groups := store.Groups()
	if len(groups) == 0 {
		return nil, nil
	}
	return groups[0].SystemTPS, groups[0].RPS
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}

func stripStatusPrefix(status string) string {
	status = strings.TrimSpace(status)
	return strings.TrimPrefix(status, "status: ")
}

func dashboardStatusText(m model) string {
	if m.failed {
		return "error " + m.store.lastError
	}
	if m.canceled {
		return "canceled"
	}
	if m.completed {
		return "completed; press q to exit"
	}
	if m.running {
		return "running"
	}
	if m.channelClosed {
		return "event stream closed"
	}
	if m.store.status != "" {
		return m.store.status
	}
	return "waiting for benchmark events"
}

func histogramBins(height int) int {
	if height < 8 {
		return 3
	}
	if height < 14 {
		return 5
	}
	return 8
}

func panelInnerWidth(width int) int {
	if width <= 4 {
		return max(1, width)
	}
	return width - 4
}

func panelInnerHeight(height int) int {
	if height <= 2 {
		return max(1, height)
	}
	return height - 2
}

func joinColumnsWithGap(left string, right string, width int, height int, gap int) string {
	if gap < 0 {
		gap = 0
	}
	leftWidth := (width - gap) / 2
	rightWidth := width - gap - leftWidth
	left = fitToBox(left, leftWidth, height)
	right = fitToBox(right, rightWidth, height)
	leftLines := strings.Split(left, "\n")
	rightLines := strings.Split(right, "\n")
	lines := make([]string, height)
	for index := range height {
		lines[index] = leftLines[index] + strings.Repeat(" ", gap) + rightLines[index]
	}
	return strings.Join(lines, "\n")
}

func fitToBox(content string, width int, height int) string {
	if width <= 0 || height <= 0 {
		return ""
	}
	inputLines := strings.Split(strings.ReplaceAll(content, "\r\n", "\n"), "\n")
	lines := make([]string, height)
	for index := range height {
		line := ""
		if index < len(inputLines) {
			line = inputLines[index]
		}
		line = truncateVisible(line, width)
		lines[index] = padVisible(line, width)
	}
	return strings.Join(lines, "\n")
}

func joinVerticalToHeight(sections []string, width int, height int) string {
	if width <= 0 || height <= 0 {
		return ""
	}
	var rawLines []string
	for _, section := range sections {
		if section == "" {
			continue
		}
		rawLines = append(rawLines, strings.Split(section, "\n")...)
	}
	return fitToBox(strings.Join(rawLines, "\n"), width, height)
}

func truncateVisible(line string, width int) string {
	if width <= 0 {
		return ""
	}
	for lipgloss.Width(line) > width {
		runes := []rune(line)
		if len(runes) == 0 {
			return ""
		}
		line = string(runes[:len(runes)-1])
	}
	return line
}

func padVisible(line string, width int) string {
	visible := lipgloss.Width(line)
	if visible >= width {
		return line
	}
	return line + strings.Repeat(" ", width-visible)
}

func dashboardLineCount(content string) int {
	if content == "" {
		return 0
	}
	return len(strings.Split(content, "\n"))
}

func dashboardMaxLineWidth(content string) int {
	maxWidth := 0
	for _, line := range strings.Split(content, "\n") {
		if width := lipgloss.Width(line); width > maxWidth {
			maxWidth = width
		}
	}
	return maxWidth
}
