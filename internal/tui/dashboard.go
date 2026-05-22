package tui

import (
	"fmt"
	"strings"

	"charm.land/lipgloss/v2"
	"github.com/gabrielmbmb/what-ttft/internal/tui/charts"
)

const (
	defaultDashboardWidth  = 100
	defaultDashboardHeight = 30
	minimumChartHeight     = 1
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
	header := fitToBox(renderDashboardHeader(m), layout.Header.Width, layout.Header.Height)
	chartArea := fitToBox(renderChartArea(m.store, layout.Charts.Width, layout.Charts.Height, m.pane), layout.Charts.Width, layout.Charts.Height)
	metrics := fitToBox(renderMetricsPanel(m.store, layout.Metrics.Width, layout.Metrics.Height, m.help.ShowAll, dashboardStatusText(m), m.confirmingCancel), layout.Metrics.Width, layout.Metrics.Height)

	return joinVerticalToHeight([]string{header, chartArea, metrics}, layout.Root.Width, layout.Root.Height)
}

func calculateDashboardLayout(width int, height int, helpVisible bool) dashboardLayout {
	if width <= 0 {
		width = defaultDashboardWidth
	}
	if height <= 0 {
		height = defaultDashboardHeight
	}

	headerHeight := 3
	metricsHeight := 12
	if helpVisible {
		metricsHeight = 13
	}
	if height < 18 {
		headerHeight = 2
		metricsHeight = 6
	}
	if height < 12 {
		headerHeight = 1
		metricsHeight = 4
	}
	if metricsHeight > height-headerHeight-minimumChartHeight {
		metricsHeight = height - headerHeight - minimumChartHeight
	}
	if metricsHeight < 1 {
		metricsHeight = 1
	}
	chartHeight := height - headerHeight - metricsHeight
	if chartHeight < minimumChartHeight {
		chartHeight = minimumChartHeight
	}
	// If tiny-height correction pushed the total over height, give charts the remaining row budget.
	if headerHeight+metricsHeight+chartHeight > height {
		chartHeight = height - headerHeight - metricsHeight
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
		Footer:  layoutBox{Width: width, Height: 0},
	}
}

func renderDashboardHeader(m model) string {
	progress := m.store.Progress()
	parts := []string{"what-ttft"}
	if m.store.provider != "" {
		parts = append(parts, "provider="+m.store.provider)
	}
	if m.store.model != "" {
		parts = append(parts, "model="+m.store.model)
	}
	if m.store.scenarioName != "" {
		parts = append(parts, "scenario="+m.store.scenarioName)
	}
	if m.store.CurrentTarget() != "-" {
		parts = append(parts, "target="+m.store.CurrentTarget())
	}
	parts = append(parts, "status="+statusWord(m))

	progressLine := fmt.Sprintf("completed=%d/%d active=%d ok=%d err=%d warmup=%d measured=%d", progress.Completed, progress.Total, progress.Active, progress.Successful, progress.Errors, progress.Warmup, progress.Measured)
	return strings.Join([]string{strings.Join(parts, "  "), progressLine, strings.Repeat("─", max(1, m.width))}, "\n")
}

func renderChartArea(store liveStore, width int, height int, mode pane) string {
	if height <= 0 || width <= 0 {
		return ""
	}

	switch mode {
	case paneTTFT:
		return renderFocusedTTFT(store, width, height)
	case paneE2E:
		return renderFocusedE2E(store, width, height)
	case paneWaterfall:
		return renderFocusedWaterfall(store, width, height)
	default:
		return renderRunCharts(store, width, height)
	}
}

func renderRunCharts(store liveStore, width int, height int) string {
	ttftValues := store.RunSeries(metricTTFTDeltaMS)
	e2eValues := store.RunSeries(metricE2EDeltaMS)
	if len(ttftValues) == 0 && len(e2eValues) == 0 {
		return emptyChartState(store)
	}

	left := strings.Join([]string{
		"TTFT delta ms over request order",
		charts.Sparkline(ttftValues, max(1, width/2-4)),
		charts.Histogram(ttftValues, histogramBins(height), max(1, width/2-2)),
	}, "\n")
	right := strings.Join([]string{
		"E2E delta ms over request order",
		charts.Sparkline(e2eValues, max(1, width/2-4)),
		renderWaterfallPreview(store, max(1, width/2-2)),
	}, "\n")

	if width >= 88 && height >= 8 {
		return joinColumns(left, right, width, height)
	}

	return joinVerticalToHeight([]string{left, right}, width, height)
}

func renderFocusedTTFT(store liveStore, width int, height int) string {
	values := store.RunSeries(metricTTFTDeltaMS)
	if len(values) == 0 {
		return emptyChartState(store)
	}
	return joinVerticalToHeight([]string{
		"TTFT focus (ttft_delta_ms)",
		charts.Sparkline(values, width),
		charts.Histogram(values, histogramBins(height), width),
	}, width, height)
}

func renderFocusedE2E(store liveStore, width int, height int) string {
	values := store.RunSeries(metricE2EDeltaMS)
	if len(values) == 0 {
		return emptyChartState(store)
	}
	return joinVerticalToHeight([]string{
		"E2E/TPS focus (e2e_delta_ms, tokens/s)",
		charts.Sparkline(values, width),
		renderMetricRows(store.MetricRows()),
	}, width, height)
}

func renderFocusedWaterfall(store liveStore, width int, height int) string {
	return joinVerticalToHeight([]string{"Slowest-request waterfall", renderWaterfallPreview(store, width)}, width, height)
}

func renderWaterfallPreview(store liveStore, width int) string {
	slowest := store.SlowestRequests(1)
	if len(slowest) == 0 {
		return "waterfall ms\n(no successful request timeline yet)"
	}
	return "request=" + slowest[0].RequestID + " metric=" + slowest[0].MetricName + "\n" + charts.Waterfall(slowest[0].Record, width)
}

func emptyChartState(store liveStore) string {
	progress := store.Progress()
	return fmt.Sprintf("LIVE CHART AREA\nwaiting for successful measured requests... completed=%d active=%d errors=%d", progress.Completed, progress.Active, progress.Errors)
}

func renderMetricsPanel(store liveStore, width int, height int, helpVisible bool, status string, confirmingCancel bool) string {
	if height <= 0 || width <= 0 {
		return ""
	}
	if confirmingCancel {
		status = "Cancel the running benchmark? Press y to confirm or n/esc to continue."
	}

	metricLines := []string{"METRICS PANEL (ms, tokens/s, req/s)", "metric                         count  p50       p95       p99       mean      unit"}
	for _, row := range store.MetricRows() {
		metricLines = append(metricLines, fmt.Sprintf("%-30s %5d  %-8s %-8s %-8s %-8s %s", row.Name, row.Count, formatMetricValue(row.P50), formatMetricValue(row.P95), formatMetricValue(row.P99), formatMetricValue(row.Mean), row.Unit))
	}
	footerLines := []string{renderRatesLine(store), renderProgressStatusLine(store, status)}
	if helpVisible {
		footerLines = append(footerLines, "keys: 1 dashboard  2 TTFT  3 E2E/TPS  4 waterfall  q cancel/quit  esc close  ? help")
	} else {
		footerLines = append(footerLines, "keys: ? help  1 dashboard  2 TTFT  3 E2E/TPS  4 waterfall  q cancel/quit")
	}

	lines := fitMetricsLines(metricLines, footerLines, height)
	return strings.Join(lines, "\n")
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

func renderRatesLine(store liveStore) string {
	systemTPS, rps := storeRates(store)
	return "system_tps=" + formatMetricValue(systemTPS) + " tokens/s  rps=" + formatMetricValue(rps) + " req/s"
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
		fields = append(fields, "reports="+store.reportStatus)
	}
	if store.outputDir != "" {
		fields = append(fields, "output="+store.outputDir)
	}
	if strings.TrimSpace(status) != "" {
		fields = append(fields, "status="+stripStatusPrefix(status))
	}
	return strings.Join(fields, "  ")
}

func storeRates(store liveStore) (*float64, *float64) {
	groups := store.Groups()
	if len(groups) == 0 {
		return nil, nil
	}
	return groups[0].SystemTPS, groups[0].RPS
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

func statusWord(m model) string {
	if m.failed {
		return "error"
	}
	if m.canceled {
		return "canceled"
	}
	if m.completed {
		return "completed"
	}
	if m.running {
		return "running"
	}
	if m.channelClosed {
		return "event-stream-closed"
	}
	if m.store.status != "" {
		return strings.ReplaceAll(m.store.status, " ", "-")
	}
	return "waiting"
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

func joinColumns(left string, right string, width int, height int) string {
	gap := 2
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
