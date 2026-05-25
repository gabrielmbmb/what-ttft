package tui

import (
	"strings"
	"testing"

	"github.com/gabrielmbmb/what-ttft/pkg/whatttft"

	tea "charm.land/bubbletea/v2"
)

// TestDashboardUsesAvailableDimensions verifies the dashboard fills the reported terminal box without overflowing it.
func TestDashboardUsesAvailableDimensions(t *testing.T) {
	app := newModel(nil)
	app = updateModel(t, app, tea.WindowSizeMsg{Width: 100, Height: 30})
	view := app.View()

	if got := dashboardLineCount(view.Content); got != 30 {
		t.Fatalf("dashboard line count = %d, want 30\n%s", got, view.Content)
	}
	if got := dashboardMaxLineWidth(view.Content); got > 100 {
		t.Fatalf("dashboard max line width = %d, want <= 100\n%s", got, view.Content)
	}
}

// TestDashboardTinySizeRendersEssentials verifies tiny terminals still show status and metrics without panicking.
func TestDashboardTinySizeRendersEssentials(t *testing.T) {
	app := newModel(nil)
	app = updateModel(t, app, tea.WindowSizeMsg{Width: 40, Height: 10})
	content := app.View().Content

	if got := dashboardLineCount(content); got != 10 {
		t.Fatalf("tiny dashboard line count = %d, want 10\n%s", got, content)
	}
	if got := dashboardMaxLineWidth(content); got > 40 {
		t.Fatalf("tiny dashboard max line width = %d, want <= 40\n%s", got, content)
	}
	if !strings.Contains(content, "METRICS") || !strings.Contains(content, "waiting") {
		t.Fatalf("tiny dashboard missing essentials:\n%s", content)
	}
}

// TestDashboardExtraHeightGoesToCharts verifies extra vertical space is allocated to charts.
func TestDashboardExtraHeightGoesToCharts(t *testing.T) {
	short := calculateDashboardLayout(100, 30, false)
	tall := calculateDashboardLayout(100, 40, false)
	if tall.Charts.Height <= short.Charts.Height {
		t.Fatalf("chart heights short/tall = %d/%d, want tall larger", short.Charts.Height, tall.Charts.Height)
	}
}

// TestDashboardWideOverviewContainsFourPanels verifies large terminals get the complete 2x2 overview.
func TestDashboardWideOverviewContainsFourPanels(t *testing.T) {
	app := dashboardAppWithRecords(t)
	app = updateModel(t, app, tea.WindowSizeMsg{Width: 140, Height: 36})
	content := app.View().Content

	for _, want := range []string{"TTFT trend · ttft_delta_ms", "E2E trend · e2e_delta_ms", "TTFT distribution · histogram", "Slowest request waterfall"} {
		if !strings.Contains(content, want) {
			t.Fatalf("wide dashboard missing %q:\n%s", want, content)
		}
	}
	if got := dashboardLineCount(content); got != 36 {
		t.Fatalf("wide dashboard line count = %d, want 36", got)
	}
	if got := dashboardMaxLineWidth(content); got > 140 {
		t.Fatalf("wide dashboard max line width = %d, want <= 140", got)
	}
}

// TestDashboardMediumPrioritizesTTFTAndE2E verifies medium terminals put trend charts first.
func TestDashboardMediumPrioritizesTTFTAndE2E(t *testing.T) {
	app := dashboardAppWithRecords(t)
	app = updateModel(t, app, tea.WindowSizeMsg{Width: 100, Height: 30})
	content := app.View().Content

	ttftIndex := strings.Index(content, "TTFT trend · ttft_delta_ms")
	e2eIndex := strings.Index(content, "E2E trend · e2e_delta_ms")
	distributionIndex := strings.Index(content, "TTFT distribution · histogram")
	if ttftIndex < 0 || e2eIndex < 0 || distributionIndex < 0 {
		t.Fatalf("medium dashboard missing trend/distribution panels:\n%s", content)
	}
	if ttftIndex >= e2eIndex || e2eIndex >= distributionIndex {
		t.Fatalf("medium dashboard panel order ttft/e2e/distribution = %d/%d/%d", ttftIndex, e2eIndex, distributionIndex)
	}
}

// TestDashboardDefaultShowsCharts verifies the default screen shows chart labels without pane navigation.
func TestDashboardDefaultShowsCharts(t *testing.T) {
	app := dashboardAppWithRecords(t)
	content := app.View().Content

	for _, want := range []string{"TTFT trend · ttft_delta_ms", "E2E trend · e2e_delta_ms", "TTFT distribution · histogram", "waterfall ms", "METRICS", "metric (successful measured reqs)", "p50", "p95", "p99", "mean"} {
		if !strings.Contains(content, want) {
			t.Fatalf("dashboard missing %q:\n%s", want, content)
		}
	}
	if strings.Contains(content, "METRICS (p50/p95/p99/mean)") {
		t.Fatalf("metrics panel title should not describe table columns:\n%s", content)
	}
}

// TestDashboardChartsUpdateOnRequestFinished verifies request completion changes chart output in realtime.
func TestDashboardChartsUpdateOnRequestFinished(t *testing.T) {
	app := newModel(nil)
	app = updateModel(t, app, tea.WindowSizeMsg{Width: 100, Height: 30})
	before := app.View().Content
	record := tuiTestRecord("req-1", "target-a", 25, 120, nil)
	app = updateModel(t, app, runEventMsg{Event: whatttft.RunEvent{Kind: whatttft.EventRequestFinished, RequestID: record.RequestID, Record: &record}})
	after := app.View().Content

	if before == after {
		t.Fatal("dashboard content did not change after request_finished event")
	}
	if !strings.Contains(after, "25.0") {
		t.Fatalf("updated dashboard missing observed TTFT value:\n%s", after)
	}
}

// TestMetricsPanelPinnedAcrossModes verifies the metrics panel stays at the bottom in every chart mode.
func TestMetricsPanelPinnedAcrossModes(t *testing.T) {
	for _, mode := range []pane{paneSummary, paneTTFT, paneE2E, paneWaterfall} {
		app := dashboardAppWithRecords(t)
		app.pane = mode
		content := app.View().Content
		bottom := bottomLines(content, 13)
		if !strings.Contains(bottom, "METRICS") || !strings.Contains(bottom, "metric (successful measured reqs)") || !strings.Contains(bottom, "keys:") {
			t.Fatalf("mode %d bottom metrics panel missing:\n%s", mode, bottom)
		}
	}
}

// TestMetricsPanelExplainsMissingTPS verifies missing provider usage is not silently rendered as zero throughput.
func TestMetricsPanelExplainsMissingTPS(t *testing.T) {
	app := newModel(nil)
	app = updateModel(t, app, tea.WindowSizeMsg{Width: 100, Height: 30})
	record := tuiTestRecord("req-no-usage", "target-a", 25, 120, nil)
	record.CompletionTokens = nil
	record.Derived.E2EOutputTPS = nil
	record.Derived.GenerationDeltaOutputTPS = nil
	app = updateModel(t, app, runEventMsg{Event: whatttft.RunEvent{Kind: whatttft.EventRequestFinished, RequestID: record.RequestID, Record: &record}})
	content := app.View().Content

	if !strings.Contains(content, "TPS unavailable: provider usage not reported") {
		t.Fatalf("dashboard missing missing-TPS explanation:\n%s", content)
	}
}

// TestFocusedE2ETPSPanelUsesReadableTable verifies mode 3 throughput details use aligned columns instead of dense prose.
func TestFocusedE2ETPSPanelUsesReadableTable(t *testing.T) {
	app := dashboardAppWithRecords(t)
	app.pane = paneE2E
	content := app.View().Content

	for _, want := range []string{"E2E/TPS focus", "metric (successful measured reqs)", "count", "p50", "p95", "p99", "mean", metricE2EOutputTPS, metricGenerationDeltaOutputTPS} {
		if !strings.Contains(content, want) {
			t.Fatalf("focused E2E/TPS panel missing %q:\n%s", want, content)
		}
	}
}

// TestMetricsPanelDistinguishesMissingFromObservedZero verifies nil metrics render as unavailable while true zero renders as 0.0.
func TestMetricsPanelDistinguishesMissingFromObservedZero(t *testing.T) {
	app := newModel(nil)
	app = updateModel(t, app, tea.WindowSizeMsg{Width: 100, Height: 30})
	zero := 0.0
	record := tuiTestRecord("req-zero", "target-a", 0, 0, nil)
	record.Derived.HTTPTTFBMS = &zero
	record.HTTP.ProviderProcessingMS = nil
	app = updateModel(t, app, runEventMsg{Event: whatttft.RunEvent{Kind: whatttft.EventRequestFinished, RequestID: record.RequestID, Record: &record}})
	content := app.View().Content

	if !strings.Contains(content, "http_ttfb_ms") || !strings.Contains(content, "0.0") {
		t.Fatalf("metrics panel missing observed zero:\n%s", content)
	}
	providerLine := lineContaining(content, "provider_processing_ms")
	if !strings.Contains(providerLine, "-") {
		t.Fatalf("provider processing line should show unavailable marker, got %q", providerLine)
	}
}

// TestDashboardDoesNotRenderSecrets verifies prompt/API/chunk-like sensitive strings in ignored metadata do not appear.
func TestDashboardDoesNotRenderSecrets(t *testing.T) {
	app := newModel(nil)
	app = updateModel(t, app, tea.WindowSizeMsg{Width: 100, Height: 30})
	record := tuiTestRecord("req-1", "target-a", 10, 100, nil)
	record.Cache.Extra = map[string]any{"ignored": "SECRET_API_KEY secret prompt chunk text Authorization"}
	app = updateModel(t, app, runEventMsg{Event: whatttft.RunEvent{Kind: whatttft.EventRequestFinished, RequestID: record.RequestID, Record: &record}})
	content := app.View().Content

	for _, forbidden := range []string{"SECRET_API_KEY", "secret prompt", "chunk text", "Authorization"} {
		if strings.Contains(content, forbidden) {
			t.Fatalf("dashboard rendered forbidden string %q:\n%s", forbidden, content)
		}
	}
}

func dashboardAppWithRecords(t *testing.T) model {
	t.Helper()

	app := newModel(nil)
	app = updateModel(t, app, tea.WindowSizeMsg{Width: 100, Height: 30})
	app = updateModel(t, app, runEventMsg{Event: whatttft.RunEvent{Kind: whatttft.EventRunStarted, Provider: "openai", Model: "gpt-test", ScenarioName: "short", TotalRequests: 2, MeasuredRequests: 2}})
	for _, record := range []whatttft.RequestRecord{
		tuiTestRecord("req-1", "target-a", 25, 120, nil),
		tuiTestRecord("req-2", "target-a", 40, 180, nil),
	} {
		app = updateModel(t, app, runEventMsg{Event: whatttft.RunEvent{Kind: whatttft.EventRequestFinished, RequestID: record.RequestID, Record: &record}})
	}
	return app
}

func bottomLines(content string, count int) string {
	lines := strings.Split(content, "\n")
	if len(lines) <= count {
		return content
	}
	return strings.Join(lines[len(lines)-count:], "\n")
}

func lineContaining(content string, needle string) string {
	for _, line := range strings.Split(content, "\n") {
		if strings.Contains(line, needle) {
			return line
		}
	}
	return ""
}
