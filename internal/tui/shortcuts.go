package tui

import (
	"strings"
)

func renderShortcutFooter(m model, width int, height int, theme tuiTheme) string {
	if width <= 0 || height <= 0 {
		return ""
	}
	title := "Shortcuts · ? more"
	if m.help.ShowAll {
		title = "Shortcuts · ? less"
	}
	bodyWidth := panelInnerWidth(width)
	bodyHeight := panelInnerHeight(height)
	lines := shortcutFooterLines(m, bodyWidth, bodyHeight)
	return panel(title, strings.Join(lines, "\n"), width, height, theme, roleAccent)
}

func shortcutFooterLines(m model, width int, height int) []string {
	if height <= 0 {
		return nil
	}
	if m.confirmingCancel {
		return []string{shortcutLine(width, "cancel benchmark?", "y confirm", "n/esc keep running")}
	}
	if m.help.ShowAll {
		return expandedShortcutLines(m, width, height)
	}
	if m.store.targetDetail {
		return []string{shortcutLine(width, "target detail", "esc back", "↑/↓ target", "1-4 charts", "r requests", quitShortcut(m))}
	}
	if m.pane == paneRequests {
		return requestShortcutLines(m.requestExplorer, width, height)
	}
	if m.store.IsBenchmark() {
		return []string{benchmarkShortcutLine(m, width)}
	}
	return []string{runShortcutLine(m, width)}
}

func requestShortcutLines(state requestExplorerState, width int, height int) []string {
	switch state.Mode {
	case requestExplorerModeDetail:
		return []string{shortcutLine(width, "request detail", "[/] section", "o output", "↑/↓ request", "esc list", "? all keys")}
	case requestExplorerModeFilter:
		line := shortcutLine(width, "filter", "enter apply", "esc discard", "ctrl+u clear", "example: status:5xx ttft>500 sort:-ttft")
		if height > 1 && state.FilterError != "" {
			return []string{line, shortcutLine(width, "error", requestFilterDisplay(state.FilterError))}
		}
		return []string{line}
	default:
		return []string{shortcutLine(width, "requests", "↑/↓ move", "enter detail", "/ filter", "s sort", "e errors", "w phase", "pgup/pgdn page", "home/end jump", "esc overview")}
	}
}

func runShortcutLine(m model, width int) string {
	return shortcutLine(width, "charts", "1 overview", "2 TTFT", "3 E2E/TPS", "4 waterfall", "5/r requests", "? all keys", quitShortcut(m))
}

func benchmarkShortcutLine(m model, width int) string {
	if m.running {
		return shortcutLine(width, "bench", "↑/↓ target", "enter detail", "space toggle after finish", "a show all", "1-4 charts", "5/r requests", "? all keys", quitShortcut(m))
	}
	return shortcutLine(width, "bench", "↑/↓ target", "enter detail", "space toggle model", "a show all", "1-4 charts", "5/r requests", "? all keys", quitShortcut(m))
}

func expandedShortcutLines(m model, width int, height int) []string {
	lines := []string{
		shortcutLine(width, "global", "1 overview", "2 TTFT", "3 E2E/TPS", "4 waterfall", "5/r requests", "? less", quitShortcut(m)),
		shortcutLine(width, "requests", "↑/↓ row", "enter detail", "/ filter", "s sort", "e errors", "w phase", "pgup/pgdn", "home/end", "esc back"),
		shortcutLine(width, "detail/filter", "[/] section", "o output", "enter apply filter", "ctrl+u clear", "esc discard/back"),
	}
	if m.store.IsBenchmark() {
		lines = append(lines, shortcutLine(width, "benchmark", "↑/↓ target", "enter target detail", "space toggle after finish", "a show all"))
	}
	if len(lines) > height {
		return lines[:height]
	}
	return lines
}

func shortcutLine(width int, label string, parts ...string) string {
	values := make([]string, 0, len(parts))
	for _, part := range parts {
		part = safeInline(part)
		if part != "" {
			values = append(values, part)
		}
	}
	prefix := ""
	if strings.TrimSpace(label) != "" {
		prefix = safeInline(label) + ": "
	}
	line := prefix + strings.Join(values, "  •  ")
	if width > 0 && len(values) > 3 {
		for len(values) > 1 && len([]rune(line)) > width {
			values = values[:len(values)-1]
			line = prefix + strings.Join(values, "  •  ")
		}
	}
	return line
}

func quitShortcut(m model) string {
	if m.running && !m.completed && !m.canceled && !m.failed {
		return "q cancel"
	}
	return "q quit"
}
