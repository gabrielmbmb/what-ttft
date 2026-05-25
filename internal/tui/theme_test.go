package tui

import (
	"strings"
	"testing"
)

// TestThemeNoColorRendersPlainText verifies no-color mode preserves text while removing ANSI styling.
func TestThemeNoColorRendersPlainText(t *testing.T) {
	theme := newTheme(true)
	if !theme.noColor {
		t.Fatal("noColor flag = false, want true")
	}
	for _, rendered := range []string{theme.render(roleBad, "ERROR"), statusPill("running", theme), progressBar(1, 2, 12, theme)} {
		if strings.Contains(rendered, "\x1b[") {
			t.Fatalf("no-color render included ANSI escape: %q", rendered)
		}
	}
}

// TestStatusPillUsesTextAndSymbols verifies status severity is visible without relying only on color.
func TestStatusPillUsesTextAndSymbols(t *testing.T) {
	theme := newTheme(true)
	cases := map[string]string{
		"running":         "● RUNNING",
		"writing reports": "◌ WRITING REPORTS",
		"completed":       "✓ COMPLETED",
		"canceled":        "◼ CANCELED",
		"error":           "✕ ERROR",
	}
	for input, want := range cases {
		if got := statusPill(input, theme); !strings.Contains(got, want) {
			t.Fatalf("statusPill(%q) = %q, want %q", input, got, want)
		}
	}
}

// TestPanelFitsDimensions verifies the reusable panel component respects its requested box.
func TestPanelFitsDimensions(t *testing.T) {
	got := panel("Title", "body\nsecond", 24, 5, newTheme(true), roleAccent)
	if lines := dashboardLineCount(got); lines != 5 {
		t.Fatalf("panel lines = %d, want 5:\n%s", lines, got)
	}
	if width := dashboardMaxLineWidth(got); width != 24 {
		t.Fatalf("panel width = %d, want 24:\n%s", width, got)
	}
}

// TestMetricCardFitsWidth verifies metric cards are bounded and keep label/value text.
func TestMetricCardFitsWidth(t *testing.T) {
	got := metricCard("ttft_delta_ms", "123.4", "ms", severityWarn, 18, newTheme(true))
	if dashboardMaxLineWidth(got) > 18 {
		t.Fatalf("metric card overflowed: %q", got)
	}
	if !strings.Contains(got, "ttft") {
		t.Fatalf("metric card lost label: %q", got)
	}
}
