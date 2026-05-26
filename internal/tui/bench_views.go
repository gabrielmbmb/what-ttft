package tui

import (
	"fmt"
	"strings"

	"github.com/gabrielmbmb/what-ttft/internal/tui/charts"
)

func renderBenchChartArea(store liveStore, width int, height int, mode pane, theme tuiTheme) string {
	if store.targetDetail && mode == paneSummary {
		return renderBenchTargetDetail(store, width, height, theme)
	}

	switch mode {
	case paneTTFT:
		return renderBenchFocusedTTFT(store, width, height, theme)
	case paneE2E:
		return renderBenchFocusedE2E(store, width, height, theme)
	case paneWaterfall:
		return renderFocusedWaterfall(store.selectedTargetStore(), width, height, theme)
	default:
		return renderBenchOverview(store, width, height, theme)
	}
}

func renderBenchOverview(store liveStore, width int, height int, theme tuiTheme) string {
	if len(store.TargetRows()) == 0 {
		return panel("Benchmark targets", "waiting for benchmark target metadata\ntarget_order=serial", width, height, theme, roleAccent)
	}
	return renderBenchCharts(store, width, height, theme)
}

func renderBenchCharts(store liveStore, width int, height int, theme tuiTheme) string {
	if width >= wideDashboardWidth && height >= 16 {
		return renderBenchWideOverview(store, width, height, theme)
	}
	if width >= mediumDashboardWidth && height >= 12 {
		return renderBenchMediumOverview(store, width, height, theme)
	}
	if len(benchMetricSeries(store, metricTTFTDeltaMS)) == 0 {
		return renderBenchTargetTablePanel(store, width, height, theme)
	}
	return renderBenchTTFTTrendPanel(store, width, height, theme)
}

func renderBenchWideOverview(store liveStore, width int, height int, theme tuiTheme) string {
	gap := 1
	leftWidth := (width - gap) / 2
	rightWidth := width - gap - leftWidth
	topHeight := height / 2
	bottomHeight := height - topHeight
	top := joinColumnsWithGap(
		renderBenchTTFTTrendPanel(store, leftWidth, topHeight, theme),
		renderBenchE2ETrendPanel(store, rightWidth, topHeight, theme),
		width,
		topHeight,
		gap,
	)
	bottom := joinColumnsWithGap(
		renderBenchDistributionPanel(store, leftWidth, bottomHeight, theme),
		renderBenchTPSChartPanel(store, rightWidth, bottomHeight, theme),
		width,
		bottomHeight,
		gap,
	)
	return joinVerticalToHeight([]string{top, bottom}, width, height)
}

func renderBenchMediumOverview(store liveStore, width int, height int, theme tuiTheme) string {
	if width >= 96 && height >= 15 {
		topHeight := min(height-5, max(8, height/2))
		bottomHeight := height - topHeight
		top := joinColumnsWithGap(
			renderBenchTTFTTrendPanel(store, (width-1)/2, topHeight, theme),
			renderBenchE2ETrendPanel(store, width-1-(width-1)/2, topHeight, theme),
			width,
			topHeight,
			1,
		)
		bottom := joinColumnsWithGap(
			renderBenchDistributionPanel(store, (width-1)/2, bottomHeight, theme),
			renderBenchTPSChartPanel(store, width-1-(width-1)/2, bottomHeight, theme),
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
		renderBenchTTFTTrendPanel(store, width, ttftHeight, theme),
		renderBenchE2ETrendPanel(store, width, e2eHeight, theme),
	}
	if remaining > 0 {
		if remaining >= 5 {
			tpsHeight := min(remaining-1, max(4, remaining/2))
			sections = append(sections, renderBenchDistributionPanel(store, width, remaining-tpsHeight, theme), renderBenchTPSChartPanel(store, width, tpsHeight, theme))
		} else {
			sections = append(sections, renderBenchDistributionPanel(store, width, remaining, theme))
		}
	}
	return joinVerticalToHeight(sections, width, height)
}

func renderBenchFocusedTTFT(store liveStore, width int, height int, theme tuiTheme) string {
	if height >= 12 {
		topHeight := height * 2 / 3
		return joinVerticalToHeight([]string{
			renderBenchTTFTTrendPanel(store, width, topHeight, theme),
			renderBenchDistributionPanel(store, width, height-topHeight, theme),
		}, width, height)
	}
	return renderBenchTTFTTrendPanel(store, width, height, theme)
}

func renderBenchFocusedE2E(store liveStore, width int, height int, theme tuiTheme) string {
	if height >= 10 {
		topHeight := height * 2 / 3
		return joinVerticalToHeight([]string{
			renderBenchE2ETrendPanel(store, width, topHeight, theme),
			renderBenchTPSChartPanel(store, width, height-topHeight, theme),
		}, width, height)
	}
	return renderBenchE2ETrendPanel(store, width, height, theme)
}

func renderBenchTargetDetail(store liveStore, width int, height int, theme tuiTheme) string {
	targetID := store.selectedTargetID()
	if targetID == "" {
		return renderBenchOverview(store, width, height, theme)
	}
	selected := store.selectedTargetStore()
	if height >= 12 {
		topHeight := height / 2
		return joinVerticalToHeight([]string{
			renderBenchSelectedTargetPanel(store, width, topHeight, theme),
			renderRunCharts(selected, width, height-topHeight, theme),
		}, width, height)
	}
	return renderBenchSelectedTargetPanel(store, width, height, theme)
}

func renderBenchTTFTTrendPanel(store liveStore, width int, height int, theme tuiTheme) string {
	body := charts.RenderMultiSeriesChart(benchMetricSeries(store, metricTTFTDeltaMS), charts.SeriesChartOptions{
		Width:      panelInnerWidth(width),
		Height:     panelInnerHeight(height),
		Title:      metricTTFTDeltaMS,
		Unit:       "ms",
		EmptyLabel: benchWaitingLabel(store, "waiting for first successful measured request"),
	}, theme.chartTheme(roleChartTTFT))
	return panel("TTFT trend · ttft_delta_ms", body, width, height, theme, roleChartTTFT)
}

func renderBenchE2ETrendPanel(store liveStore, width int, height int, theme tuiTheme) string {
	body := charts.RenderMultiSeriesChart(benchMetricSeries(store, metricE2EDeltaMS), charts.SeriesChartOptions{
		Width:      panelInnerWidth(width),
		Height:     panelInnerHeight(height),
		Title:      metricE2EDeltaMS,
		Unit:       "ms",
		EmptyLabel: benchWaitingLabel(store, "waiting for first successful measured request"),
	}, theme.chartTheme(roleChartE2E))
	return panel("E2E trend · e2e_delta_ms", body, width, height, theme, roleChartE2E)
}

func renderBenchDistributionPanel(store liveStore, width int, height int, theme tuiTheme) string {
	body := charts.RenderMultiHistogramChart(benchMetricSeries(store, metricTTFTDeltaMS), charts.HistogramOptions{
		Width:      panelInnerWidth(width),
		Height:     panelInnerHeight(height),
		Bins:       histogramBins(height),
		Title:      "TTFT distribution",
		Unit:       "ms",
		EmptyLabel: benchWaitingLabel(store, "waiting for first successful measured request"),
	}, theme.chartTheme(roleChartTTFT))
	return panel("TTFT distribution · histogram", body, width, height, theme, roleChartTTFT)
}

func renderBenchTPSChartPanel(store liveStore, width int, height int, theme tuiTheme) string {
	body := charts.RenderMultiSeriesChart(benchMetricSeries(store, metricE2EOutputTPS), charts.SeriesChartOptions{
		Width:      panelInnerWidth(width),
		Height:     panelInnerHeight(height),
		Title:      metricE2EOutputTPS,
		Unit:       "tokens/s",
		EmptyLabel: benchWaitingLabel(store, "TPS unavailable: provider usage not reported"),
	}, theme.chartTheme(roleChartTPS))
	return panel("Output TPS trend · e2e_output_tps", body, width, height, theme, roleChartTPS)
}

func renderBenchTargetTablePanel(store liveStore, width int, height int, theme tuiTheme) string {
	body := renderBenchTargetTable(store, panelInnerWidth(width), panelInnerHeight(height))
	return panel("Targets · target_order=serial", body, width, height, theme, roleAccent)
}

func renderBenchTargetTable(store liveStore, width int, height int) string {
	if width <= 0 || height <= 0 {
		return ""
	}
	lines := []string{fmt.Sprintf("%-2s %-5s %-16s %-9s %-9s %-9s %7s %4s %4s %-16s", "", "show", "target", "status", "api", "tier", "done", "ok", "err", "model")}
	selectedID := store.selectedTargetID()
	for _, row := range store.TargetRows() {
		marker := " "
		if row.ID == selectedID {
			marker = "›"
		}
		api := firstNonEmpty(row.ProviderAPI, row.Provider, "-")
		tier := firstNonEmpty(row.RequestedServiceTier, row.ObservedServiceTier, "-")
		visibility := "on"
		if !row.Visible {
			visibility = "off"
		}
		lines = append(lines, fmt.Sprintf("%-2s %-5s %-16s %-9s %-9s %-9s %3d/%-3d %4d %4d %-16s", marker, visibility, truncateVisible(row.ID, 16), row.Status, truncateVisible(api, 9), truncateVisible(tier, 9), row.Completed, row.Total, row.Successful, row.Errors, truncateVisible(row.Model, 16)))
	}
	if len(lines) == 1 {
		lines = append(lines, "waiting for target events")
	}
	return fitToBox(strings.Join(lines, "\n"), width, height)
}

func renderBenchSelectedTargetPanel(store liveStore, width int, height int, theme tuiTheme) string {
	targetID := store.selectedTargetID()
	if targetID == "" {
		return panel("Selected target", "no target selected", width, height, theme, roleMuted)
	}
	selected := store.selectedTargetStore()
	rows := selected.MetricRows()
	visibility := "on"
	if !store.targetVisible(targetID) {
		visibility = "off"
	}
	lines := []string{
		"selected=" + targetID + "  show=" + visibility + "  space=toggle after finish  a=show all  esc=overview",
		fmt.Sprintf("%-36s %5s  %-8s %-8s %-8s %-8s %s", "metric (selected target only)", "count", "p50", "p95", "p99", "mean", "unit"),
		metricTableLine(metricRowByName(rows, metricTTFTDeltaMS)),
		metricTableLine(metricRowByName(rows, metricE2EDeltaMS)),
		metricTableLine(metricRowByName(rows, metricE2EOutputTPS)),
	}
	return panel("Selected target detail", fitToBox(strings.Join(lines, "\n"), panelInnerWidth(width), panelInnerHeight(height)), width, height, theme, roleAccent)
}

func benchMetricSeries(store liveStore, name string) []charts.NamedSeries {
	rows := store.TargetRows()
	labels := benchSeriesLabels(rows)
	series := make([]charts.NamedSeries, 0, len(rows))
	for rowIndex, row := range rows {
		if !row.Visible {
			continue
		}
		values := make([]float64, 0)
		for _, record := range store.recordsForTarget(row.ID) {
			if record.Warmup || record.Error != nil {
				continue
			}
			appendMetricValue(&values, metricValue(record, name))
		}
		series = append(series, charts.NamedSeries{Label: labels[row.ID], Values: values, StyleIndex: rowIndex, UseStyleIndex: true})
	}
	return series
}

func benchSeriesLabels(rows []targetRow) map[string]string {
	modelCounts := make(map[string]int, len(rows))
	for _, row := range rows {
		if row.Model != "" {
			modelCounts[row.Model]++
		}
	}

	baseLabels := make(map[string]string, len(rows))
	labelCounts := make(map[string]int, len(rows))
	for _, row := range rows {
		label := benchSeriesBaseLabel(row, modelCounts[row.Model] > 1)
		baseLabels[row.ID] = label
		labelCounts[label]++
	}

	labels := make(map[string]string, len(rows))
	for _, row := range rows {
		label := baseLabels[row.ID]
		if labelCounts[label] > 1 && row.ID != "" {
			label = label + " (" + row.ID + ")"
		}
		labels[row.ID] = label
	}
	return labels
}

func benchSeriesBaseLabel(row targetRow, duplicateModel bool) string {
	if row.Model == "" {
		return firstNonEmpty(row.Name, row.ID, "target")
	}
	if !duplicateModel {
		return row.Model
	}
	if row.Name != "" && row.Name != row.Model {
		return row.Name
	}
	if row.RequestedServiceTier != "" {
		return row.Model + " " + row.RequestedServiceTier
	}
	if row.ID != "" {
		return row.Model + " (" + row.ID + ")"
	}
	return row.Model
}

func benchWaitingLabel(store liveStore, message string) string {
	labels := benchTargetLabels(visibleTargetRows(store.TargetRows()))
	if len(labels) == 0 {
		return message
	}
	return message + "; targets=" + strings.Join(labels, ",")
}

func benchTargetLabels(rows []targetRow) []string {
	labelsByID := benchSeriesLabels(rows)
	labels := make([]string, 0, len(rows))
	for _, row := range rows {
		labels = append(labels, labelsByID[row.ID])
	}
	return labels
}

func visibleTargetRows(rows []targetRow) []targetRow {
	visible := make([]targetRow, 0, len(rows))
	for _, row := range rows {
		if row.Visible {
			visible = append(visible, row)
		}
	}
	return visible
}
