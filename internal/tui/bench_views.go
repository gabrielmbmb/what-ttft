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
		return renderFocusedTTFT(store.selectedTargetStore(), width, height, theme)
	case paneE2E:
		return renderFocusedE2E(store.selectedTargetStore(), width, height, theme)
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
	if width >= wideDashboardWidth && height >= 16 {
		gap := 1
		leftWidth := (width - gap) / 2
		rightWidth := width - gap - leftWidth
		topHeight := height / 2
		bottomHeight := height - topHeight
		top := joinColumnsWithGap(
			renderBenchTargetTablePanel(store, leftWidth, topHeight, theme),
			renderBenchComparisonPanel(store, rightWidth, topHeight, theme),
			width,
			topHeight,
			gap,
		)
		bottom := joinColumnsWithGap(
			renderBenchPercentilePanel(store, leftWidth, bottomHeight, theme),
			renderBenchSelectedTargetPanel(store, rightWidth, bottomHeight, theme),
			width,
			bottomHeight,
			gap,
		)
		return joinVerticalToHeight([]string{top, bottom}, width, height)
	}

	if width >= mediumDashboardWidth && height >= 12 {
		targetHeight := max(4, height/3)
		comparisonHeight := max(4, height/3)
		remaining := height - targetHeight - comparisonHeight
		sections := []string{
			renderBenchTargetTablePanel(store, width, targetHeight, theme),
			renderBenchComparisonPanel(store, width, comparisonHeight, theme),
		}
		if remaining > 0 {
			sections = append(sections, renderBenchPercentilePanel(store, width, remaining, theme))
		}
		return joinVerticalToHeight(sections, width, height)
	}

	return renderBenchTargetTablePanel(store, width, height, theme)
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

func renderBenchTargetTablePanel(store liveStore, width int, height int, theme tuiTheme) string {
	body := renderBenchTargetTable(store, panelInnerWidth(width), panelInnerHeight(height))
	return panel("Targets · target_order=serial", body, width, height, theme, roleAccent)
}

func renderBenchTargetTable(store liveStore, width int, height int) string {
	if width <= 0 || height <= 0 {
		return ""
	}
	lines := []string{fmt.Sprintf("%-2s %-16s %-9s %-9s %-9s %7s %4s %4s %-16s", "", "target", "status", "api", "tier", "done", "ok", "err", "model")}
	selectedID := store.selectedTargetID()
	for _, row := range store.TargetRows() {
		marker := " "
		if row.ID == selectedID {
			marker = "›"
		}
		api := firstNonEmpty(row.ProviderAPI, row.Provider, "-")
		tier := firstNonEmpty(row.RequestedServiceTier, row.ObservedServiceTier, "-")
		lines = append(lines, fmt.Sprintf("%-2s %-16s %-9s %-9s %-9s %3d/%-3d %4d %4d %-16s", marker, truncateVisible(row.ID, 16), row.Status, truncateVisible(api, 9), truncateVisible(tier, 9), row.Completed, row.Total, row.Successful, row.Errors, truncateVisible(row.Model, 16)))
	}
	if len(lines) == 1 {
		lines = append(lines, "waiting for target events")
	}
	return fitToBox(strings.Join(lines, "\n"), width, height)
}

func renderBenchComparisonPanel(store liveStore, width int, height int, theme tuiTheme) string {
	body := charts.TargetTable(store.Groups(), panelInnerWidth(width))
	return panel("Target comparison", body, width, height, theme, roleAccent)
}

func renderBenchPercentilePanel(store liveStore, width int, height int, theme tuiTheme) string {
	body := charts.RenderPercentileChart(charts.PercentileGroupsFromSummary(store.Groups()), charts.PercentileOptions{
		Width:      panelInnerWidth(width),
		Height:     panelInnerHeight(height),
		Title:      "TTFT percentiles by target",
		Unit:       "ms",
		EmptyLabel: "waiting for successful measured requests",
	}, theme.chartTheme(roleChartTTFT))
	return panel("TTFT target percentiles", body, width, height, theme, roleChartTTFT)
}

func renderBenchSelectedTargetPanel(store liveStore, width int, height int, theme tuiTheme) string {
	targetID := store.selectedTargetID()
	if targetID == "" {
		return panel("Selected target", "no target selected", width, height, theme, roleMuted)
	}
	selected := store.selectedTargetStore()
	rows := selected.MetricRows()
	lines := []string{
		"selected=" + targetID + "  enter=detail  ↑/↓ or j/k=select  esc=overview",
		fmt.Sprintf("%-36s %5s  %-8s %-8s %-8s %-8s %s", "metric (selected target only)", "count", "p50", "p95", "p99", "mean", "unit"),
		metricTableLine(metricRowByName(rows, metricTTFTDeltaMS)),
		metricTableLine(metricRowByName(rows, metricE2EDeltaMS)),
		metricTableLine(metricRowByName(rows, metricE2EOutputTPS)),
	}
	return panel("Selected target detail", fitToBox(strings.Join(lines, "\n"), panelInnerWidth(width), panelInnerHeight(height)), width, height, theme, roleAccent)
}
