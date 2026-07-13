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

	for _, want := range []string{"TTFT trend · ttft_delta_ms", "E2E trend · e2e_delta_ms", "TTFT distribution · histogram", "Output TPS trend · e2e_output_tps"} {
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

	for _, want := range []string{"TTFT trend · ttft_delta_ms", "E2E trend · e2e_delta_ms", "TTFT distribution · histogram", "Output TPS trend · e2e_output_tps", "tokens/s", "completion_tokens_total=8", "completion_token_records=2", "METRICS", "metric (successful measured reqs)", "p50", "p95", "p99", "mean"} {
		if !strings.Contains(content, want) {
			t.Fatalf("dashboard missing %q:\n%s", want, content)
		}
	}
	if strings.Contains(content, "METRICS (p50/p95/p99/mean)") {
		t.Fatalf("metrics panel title should not describe table columns:\n%s", content)
	}
	if strings.Contains(content, "Slowest request waterfall") || strings.Contains(content, "waterfall ms") {
		t.Fatalf("overview should show TPS chart instead of waterfall panel:\n%s", content)
	}
}

// TestDashboardShortcutFooterIsContextualAndExpandable verifies shortcut help is pinned and responds to pane/mode changes.
func TestDashboardShortcutFooterIsContextualAndExpandable(t *testing.T) {
	app := dashboardAppWithRecords(t)
	content := app.View().Content
	for _, want := range []string{"Shortcuts · ? more", "charts:", "5/r requests", "? all keys"} {
		if !strings.Contains(content, want) {
			t.Fatalf("dashboard shortcut footer missing %q:\n%s", want, content)
		}
	}

	app = updateModel(t, app, keyPress("?"))
	content = app.View().Content
	for _, want := range []string{"Shortcuts · ? less", "global:", "requests:", "detail/filter:"} {
		if !strings.Contains(content, want) {
			t.Fatalf("expanded shortcut footer missing %q:\n%s", want, content)
		}
	}

	app = updateModel(t, app, keyPress("?"))
	app = updateModel(t, app, keyPress("r"))
	content = app.View().Content
	for _, want := range []string{"Requests", "Shortcuts · ? more", "requests:", "enter detail", "/ filter", "s sort"} {
		if !strings.Contains(content, want) {
			t.Fatalf("request-list shortcut footer missing %q:\n%s", want, content)
		}
	}

	app = updateModel(t, app, keyPress("/"))
	content = app.View().Content
	for _, want := range []string{"filter:", "enter apply", "esc discard", "ctrl+u clear"} {
		if !strings.Contains(content, want) {
			t.Fatalf("filter shortcut footer missing %q:\n%s", want, content)
		}
	}

	app = updateModel(t, app, keyPress("esc"))
	app = updateModel(t, app, keyPress("enter"))
	content = app.View().Content
	for _, want := range []string{"Request detail", "request detail:", "[/] section", "o output", "esc list"} {
		if !strings.Contains(content, want) {
			t.Fatalf("detail shortcut footer missing %q:\n%s", want, content)
		}
	}
}

// TestBenchmarkDashboardShowsRunStyleMultiModelCharts verifies bench overview reuses run-style charts with multiple model series.
func TestBenchmarkDashboardShowsRunStyleMultiModelCharts(t *testing.T) {
	app := benchmarkDashboardAppWithRecords(t)
	content := app.View().Content

	for _, want := range []string{"what-ttft bench", "target_order=serial", "TTFT trend · ttft_delta_ms", "E2E trend · e2e_delta_ms", "TTFT distribution · histogram", "Output TPS trend · e2e_output_tps", "legend:", "gpt-a latest", "gpt-b latest", "series=2", "x=request order per target", "MODEL METRICS", "model/target", "models shown=2/2", "Shortcuts", "space toggle"} {
		if !strings.Contains(content, want) {
			t.Fatalf("benchmark dashboard missing %q:\n%s", want, content)
		}
	}
	if strings.Contains(content, "model=gpt") {
		t.Fatalf("benchmark header should not collapse multiple models into one model label:\n%s", content)
	}
	for _, forbidden := range []string{"SECRET_API_KEY", "Authorization", "prompt text"} {
		if strings.Contains(content, forbidden) {
			t.Fatalf("benchmark dashboard rendered forbidden string %q:\n%s", forbidden, content)
		}
	}
}

// TestBenchmarkDashboardModelMetricsArePerTarget verifies the bench metrics panel compares models without aggregating them together.
func TestBenchmarkDashboardModelMetricsArePerTarget(t *testing.T) {
	app := benchmarkDashboardAppWithRecords(t)
	content := app.View().Content

	for _, want := range []string{"ttft50", "ttft95", "e2e50", "e2e95"} {
		if !strings.Contains(content, want) {
			t.Fatalf("model metrics table missing distinct percentile column %q:\n%s", want, content)
		}
	}
	if strings.Contains(content, "ttft p50/p95") || strings.Contains(content, "e2e p50/p95") || strings.Contains(content, "10.0/10.0") || strings.Contains(content, "90.0/90.0") {
		t.Fatalf("model metrics table should not combine percentiles with slash-separated cells:\n%s", content)
	}
	gptALine := lineContaining(content, "gpt-a                        target-a")
	if !strings.Contains(gptALine, "gpt-a") || !strings.Contains(gptALine, "10.0") || !strings.Contains(gptALine, "100.0") {
		t.Fatalf("gpt-a model metrics row missing target-scoped values:\n%s", content)
	}
	gptBLine := lineContaining(content, "gpt-b                        target-b")
	if !strings.Contains(gptBLine, "gpt-b") || !strings.Contains(gptBLine, "90.0") || !strings.Contains(gptBLine, "200.0") {
		t.Fatalf("gpt-b model metrics row missing target-scoped values:\n%s", content)
	}
}

// TestBenchmarkDashboardCanHideFinishedTargets verifies post-run target visibility filters chart series without changing reports.
func TestBenchmarkDashboardCanHideFinishedTargets(t *testing.T) {
	app := benchmarkDashboardAppWithRecords(t)
	app = updateModel(t, app, runEventMsg{Event: whatttft.RunEvent{Kind: whatttft.EventBenchmarkFinished}})
	app = updateModel(t, app, keyPress("j"))
	app = updateModel(t, app, keyPress("space"))
	content := app.View().Content

	for _, want := range []string{"models shown=1/2", "selected=gpt-b", "off  gpt-b", "series=1", "gpt-a latest"} {
		if !strings.Contains(content, want) {
			t.Fatalf("filtered benchmark dashboard missing %q:\n%s", want, content)
		}
	}
	if strings.Contains(content, "gpt-b latest") {
		t.Fatalf("hidden target still rendered as a chart series:\n%s", content)
	}

	app = updateModel(t, app, keyPress("a"))
	content = app.View().Content
	if !strings.Contains(content, "models shown=2/2") || !strings.Contains(content, "gpt-b latest") {
		t.Fatalf("show-all did not restore hidden chart series:\n%s", content)
	}
}

// TestDashboardFatalErrorShowsDialog verifies failures that stop execution are immediately visible instead of looking stalled.
func TestDashboardFatalErrorShowsDialog(t *testing.T) {
	app := newModel(nil)
	app = updateModel(t, app, tea.WindowSizeMsg{Width: 100, Height: 30})
	app = updateModel(t, app, runEventMsg{Event: whatttft.RunEvent{
		Kind:          whatttft.EventBenchmarkFailed,
		BenchmarkName: "model-compare",
		Targets:       []whatttft.RunEventTarget{{TargetID: "gpt-5.5"}},
		Error: &whatttft.RunEventError{
			Category: "validation",
			Message:  "target gpt-5.5 API key environment variable OPENAI_API_KEY is empty",
		},
	}})
	content := app.View().Content

	for _, want := range []string{"Benchmark failed", "could not start or continue", "OPENAI_API_KEY is empty", "Press enter, q, or esc to exit"} {
		if !strings.Contains(content, want) {
			t.Fatalf("failure dialog missing %q:\n%s", want, content)
		}
	}
}

// TestRequestExplorerRowsDoNotRenderSecretMetadata verifies request rows omit prompt/API/chunk-like sensitive fields.
func TestRequestExplorerRowsDoNotRenderSecretMetadata(t *testing.T) {
	app := newModel(nil)
	app = updateModel(t, app, tea.WindowSizeMsg{Width: 120, Height: 30})
	record := tuiTestRecord("req-1", "target-a", 10, 100, &whatttft.ErrorRecord{Category: "provider", Message: "redacted", BodySnippet: "SECRET_API_KEY raw provider body Authorization prompt text"})
	record.Cache.Extra = map[string]any{"ignored": "SECRET_API_KEY secret prompt chunk text Authorization"}
	app = updateModel(t, app, runEventMsg{Event: whatttft.RunEvent{Kind: whatttft.EventRunStarted, Provider: "openai", Model: "gpt-test", TotalRequests: 1, MeasuredRequests: 1}})
	app = updateModel(t, app, runEventMsg{Event: whatttft.RunEvent{Kind: whatttft.EventRequestFinished, RequestID: record.RequestID, Record: &record}})
	app = updateModel(t, app, keyPress("r"))
	content := app.View().Content

	for _, forbidden := range []string{"SECRET_API_KEY", "raw provider body", "secret prompt", "chunk text", "Authorization"} {
		if strings.Contains(content, forbidden) {
			t.Fatalf("request explorer row rendered forbidden string %q:\n%s", forbidden, content)
		}
	}
}

// TestBenchmarkDashboardSelectedTargetDetailUsesSelectedRecords verifies selected-target detail is scoped to one target.
func TestBenchmarkDashboardSelectedTargetDetailUsesSelectedRecords(t *testing.T) {
	app := benchmarkDashboardAppWithRecords(t)
	app.store.selectTarget(1)
	app.store.setTargetDetail(true)
	content := renderBenchTargetDetail(app.store, 120, 20, app.theme)

	if !strings.Contains(content, "selected=target-b") || !strings.Contains(content, "90.0") {
		t.Fatalf("selected target detail missing target-b values:\n%s", content)
	}
	if strings.Contains(content, "10.0") {
		t.Fatalf("selected target detail leaked target-a value:\n%s", content)
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

// TestMetricsPanelAndShortcutsPinnedAcrossModes verifies metrics and shortcut help stay visible in every chart mode.
func TestMetricsPanelAndShortcutsPinnedAcrossModes(t *testing.T) {
	for _, mode := range []pane{paneSummary, paneTTFT, paneE2E, paneWaterfall} {
		app := dashboardAppWithRecords(t)
		app.pane = mode
		content := app.View().Content
		bottom := bottomLines(content, 15)
		if !strings.Contains(bottom, "METRICS") || !strings.Contains(bottom, "metric (successful measured reqs)") || !strings.Contains(bottom, "Shortcuts") || !strings.Contains(bottom, "5/r requests") {
			t.Fatalf("mode %d bottom metrics/shortcuts panels missing:\n%s", mode, bottom)
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

func benchmarkDashboardAppWithRecords(t *testing.T) model {
	t.Helper()

	app := newModel(nil)
	app = updateModel(t, app, tea.WindowSizeMsg{Width: 140, Height: 36})
	app = updateModel(t, app, runEventMsg{Event: whatttft.RunEvent{Kind: whatttft.EventBenchmarkStarted, BenchmarkName: "bench-test", TotalRequests: 2, MeasuredRequests: 2, Targets: []whatttft.RunEventTarget{
		{TargetID: "target-a", TargetName: "Target A", Provider: "openai", ProviderAPI: "responses", RequestedServiceTier: "default", Model: "gpt-a", ScenarioName: "bench-short", TotalRequests: 1, MeasuredRequests: 1},
		{TargetID: "target-b", TargetName: "Target B", Provider: "openai", ProviderAPI: "responses", RequestedServiceTier: "default", Model: "gpt-b", ScenarioName: "bench-short", TotalRequests: 1, MeasuredRequests: 1},
	}}})
	recordA := tuiTestRecord("target-a-req-000000", "target-a", 10, 100, nil)
	recordA.Model = "gpt-a"
	recordB := tuiTestRecord("target-b-req-000000", "target-b", 90, 200, nil)
	recordB.Model = "gpt-b"
	for _, record := range []whatttft.RequestRecord{recordA, recordB} {
		app = updateModel(t, app, runEventMsg{Event: whatttft.RunEvent{Kind: whatttft.EventTargetStarted, TargetID: record.TargetID, TargetName: "Target " + strings.ToUpper(strings.TrimPrefix(record.TargetID, "target-")), Provider: "openai", Model: record.Model, TotalRequests: 1, MeasuredRequests: 1}})
		app = updateModel(t, app, runEventMsg{Event: whatttft.RunEvent{Kind: whatttft.EventRequestFinished, TargetID: record.TargetID, RequestID: record.RequestID, Record: &record, CompletedRequests: 1, SuccessfulRequests: 1}})
		app = updateModel(t, app, runEventMsg{Event: whatttft.RunEvent{Kind: whatttft.EventTargetFinished, TargetID: record.TargetID}})
	}
	return app
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
