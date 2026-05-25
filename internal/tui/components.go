package tui

import (
	"fmt"
	"strings"

	"charm.land/lipgloss/v2"
)

type metricSeverity int

const (
	severityNeutral metricSeverity = iota
	severityGood
	severityWarn
	severityBad
	severityMuted
)

type legendItem struct {
	Label string
	Role  themeRole
}

func panel(title string, body string, width int, height int, theme tuiTheme, role themeRole) string {
	if width <= 0 || height <= 0 {
		return ""
	}
	if width < 4 || height < 3 {
		return fitToBox(strings.Join([]string{safeInline(title), body}, "\n"), width, height)
	}

	innerWidth := width - 4
	innerHeight := height - 2
	body = fitToBox(body, innerWidth, innerHeight)
	bodyLines := strings.Split(body, "\n")

	lines := make([]string, 0, height)
	lines = append(lines, panelTop(title, width, theme, role))
	for _, line := range bodyLines {
		lines = append(lines, theme.Border.Render("│")+" "+line+" "+theme.Border.Render("│"))
	}
	lines = append(lines, theme.Border.Render("╰"+strings.Repeat("─", width-2)+"╯"))
	return strings.Join(lines, "\n")
}

func panelTop(title string, width int, theme tuiTheme, role themeRole) string {
	title = safeInline(title)
	if title == "" {
		return theme.Border.Render("╭" + strings.Repeat("─", width-2) + "╮")
	}
	maxTitleWidth := width - 4
	if maxTitleWidth < 0 {
		maxTitleWidth = 0
	}
	title = truncateVisible(title, maxTitleWidth)
	titlePart := " " + theme.render(role, title) + " "
	fillWidth := width - 2 - lipgloss.Width(titlePart)
	if fillWidth < 0 {
		fillWidth = 0
	}
	return theme.Border.Render("╭") + titlePart + theme.Border.Render(strings.Repeat("─", fillWidth)+"╮")
}

func metricCard(label string, value string, unit string, severity metricSeverity, width int, theme tuiTheme) string {
	if width <= 0 {
		return ""
	}
	text := strings.TrimSpace(fmt.Sprintf("%s %s %s", safeInline(label), safeInline(value), safeInline(unit)))
	text = truncateVisible(text, width)
	return theme.style(severityRole(severity)).Render(padVisible(text, width))
}

func statusPill(status string, theme tuiTheme) string {
	status = strings.ToLower(strings.TrimSpace(status))
	role := roleMuted
	symbol := "○"
	label := "WAITING"
	switch {
	case strings.Contains(status, "error") || strings.Contains(status, "failed"):
		role = roleBad
		symbol = "✕"
		label = "ERROR"
	case strings.Contains(status, "cancel"):
		role = roleWarn
		symbol = "◼"
		label = "CANCELED"
	case strings.Contains(status, "report") || strings.Contains(status, "writing"):
		role = roleWarn
		symbol = "◌"
		label = "WRITING REPORTS"
	case strings.Contains(status, "complete") || strings.Contains(status, "written"):
		role = roleGood
		symbol = "✓"
		label = "COMPLETED"
	case strings.Contains(status, "running"):
		role = roleGood
		symbol = "●"
		label = "RUNNING"
	}
	return theme.render(role, symbol+" "+label)
}

func progressBar(completed int, total int, width int, theme tuiTheme) string {
	if width <= 0 {
		return ""
	}
	label := fmt.Sprintf("%d/%d", completed, total)
	if total <= 0 {
		label = fmt.Sprintf("%d/?", completed)
	}
	barWidth := width - lipgloss.Width(label) - 3
	if barWidth < 1 {
		return truncateVisible(label, width)
	}
	filled := 0
	if total > 0 {
		filled = int(float64(completed) / float64(total) * float64(barWidth))
	}
	if filled > barWidth {
		filled = barWidth
	}
	if filled < 0 {
		filled = 0
	}
	bar := strings.Repeat("█", filled) + strings.Repeat("░", barWidth-filled)
	return theme.ChartTPS.Render("["+bar+"]") + " " + label
}

func legend(items []legendItem, theme tuiTheme) string {
	parts := make([]string, 0, len(items))
	for _, item := range items {
		label := safeInline(item.Label)
		if label == "" {
			continue
		}
		parts = append(parts, theme.render(item.Role, "■ "+label))
	}
	return strings.Join(parts, "  ")
}

func severityRole(severity metricSeverity) themeRole {
	switch severity {
	case severityGood:
		return roleGood
	case severityWarn:
		return roleWarn
	case severityBad:
		return roleBad
	case severityMuted:
		return roleMuted
	default:
		return roleAccent
	}
}

func safeInline(value string) string {
	value = strings.ReplaceAll(value, "\r", " ")
	value = strings.ReplaceAll(value, "\n", " ")
	value = strings.ReplaceAll(value, "\t", " ")
	return strings.Join(strings.Fields(value), " ")
}
