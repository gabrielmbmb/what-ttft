package tui

import (
	"strings"
	"testing"
)

// TestBenchmarkShortcutLineIncludesModelMetrics verifies benchmark users can discover the focused table shortcut.
func TestBenchmarkShortcutLineIncludesModelMetrics(t *testing.T) {
	app := newModel(nil)
	app.store.benchmarkMode = true
	line := benchmarkShortcutLine(app, 300)
	if !strings.Contains(line, "m metrics") {
		t.Fatalf("benchmark shortcut line missing model metrics shortcut: %q", line)
	}
}
