package tui

import (
	"strings"
	"testing"

	"github.com/gabrielmbmb/what-ttft/pkg/whatttft"

	tea "charm.land/bubbletea/v2"
)

// TestRequestExplorerRunNavigationAndDetail verifies the request explorer is reachable in run dashboards and supports row/detail transitions.
func TestRequestExplorerRunNavigationAndDetail(t *testing.T) {
	app := newModel(nil)
	app = updateModel(t, app, tea.WindowSizeMsg{Width: 120, Height: 32})
	app = updateModel(t, app, runEventMsg{Event: whatttft.RunEvent{Kind: whatttft.EventRunStarted, Provider: "openai", Model: "gpt-test", TotalRequests: 2, MeasuredRequests: 2}})
	for _, record := range []whatttft.RequestRecord{
		tuiTestRecord("req-000000", "", 10, 100, nil),
		tuiTestRecord("req-000001", "", 20, 200, nil),
	} {
		app = updateModel(t, app, runEventMsg{Event: whatttft.RunEvent{Kind: whatttft.EventRequestFinished, RequestID: record.RequestID, Record: &record}})
	}

	app = updateModel(t, app, keyPress("r"))
	if app.pane != paneRequests || app.requestExplorer.Mode != requestExplorerModeList {
		t.Fatalf("request explorer pane/mode = %d/%d, want requests/list", app.pane, app.requestExplorer.Mode)
	}
	if content := app.View().Content; !strings.Contains(content, "Requests") || !strings.Contains(content, "req-000000") || !strings.Contains(content, "req-000001") {
		t.Fatalf("request explorer list missing records:\n%s", content)
	}

	app = updateModel(t, app, keyPress("j"))
	if app.requestExplorer.CursorRequestID != "req-000001" {
		t.Fatalf("cursor request ID = %q, want req-000001", app.requestExplorer.CursorRequestID)
	}
	app = updateModel(t, app, keyPress("enter"))
	if app.requestExplorer.Mode != requestExplorerModeDetail {
		t.Fatalf("request explorer mode = %d, want detail", app.requestExplorer.Mode)
	}
	if content := app.View().Content; !strings.Contains(content, "Request detail") || !strings.Contains(content, "req-000001") || !strings.Contains(content, "section=identity") {
		t.Fatalf("request detail missing selected request:\n%s", content)
	}

	app = updateModel(t, app, keyPress("esc"))
	if app.pane != paneRequests || app.requestExplorer.Mode != requestExplorerModeList {
		t.Fatalf("after detail esc pane/mode = %d/%d, want requests/list", app.pane, app.requestExplorer.Mode)
	}
	app = updateModel(t, app, keyPress("esc"))
	if app.pane != paneSummary {
		t.Fatalf("after list esc pane = %d, want summary", app.pane)
	}
}

// TestRequestExplorerBenchKeyPrecedence verifies row navigation in request explorer does not change benchmark target selection.
func TestRequestExplorerBenchKeyPrecedence(t *testing.T) {
	app := newModel(nil)
	app = updateModel(t, app, tea.WindowSizeMsg{Width: 140, Height: 36})
	app = updateModel(t, app, runEventMsg{Event: whatttft.RunEvent{Kind: whatttft.EventBenchmarkStarted, BenchmarkName: "bench", Targets: []whatttft.RunEventTarget{
		{TargetID: "target-a", Model: "gpt-a", TotalRequests: 1, MeasuredRequests: 1},
		{TargetID: "target-b", Model: "gpt-b", TotalRequests: 1, MeasuredRequests: 1},
	}}})
	for _, record := range []whatttft.RequestRecord{
		tuiTestRecord("target-a-req-000000", "target-a", 10, 100, nil),
		tuiTestRecord("target-b-req-000000", "target-b", 90, 200, nil),
	} {
		app = updateModel(t, app, runEventMsg{Event: whatttft.RunEvent{Kind: whatttft.EventRequestFinished, TargetID: record.TargetID, RequestID: record.RequestID, Record: &record}})
	}
	if got := app.store.selectedTargetID(); got != "target-a" {
		t.Fatalf("initial selected target = %q, want target-a", got)
	}

	app = updateModel(t, app, keyPress("5"))
	app = updateModel(t, app, keyPress("j"))
	if app.requestExplorer.CursorRequestID != "target-b-req-000000" {
		t.Fatalf("request cursor = %q, want target-b request", app.requestExplorer.CursorRequestID)
	}
	if got := app.store.selectedTargetID(); got != "target-a" {
		t.Fatalf("benchmark target changed in request explorer to %q, want target-a", got)
	}

	app = updateModel(t, app, keyPress("esc"))
	app = updateModel(t, app, keyPress("j"))
	if got := app.store.selectedTargetID(); got != "target-b" {
		t.Fatalf("benchmark target after leaving request explorer = %q, want target-b", got)
	}
}

// TestRequestExplorerFilterModeTransitions verifies filter editor open/apply/discard transitions are modeled before filter predicates are implemented.
func TestRequestExplorerFilterModeTransitions(t *testing.T) {
	app := newModel(nil)
	record := tuiTestRecord("req-000000", "", 10, 100, nil)
	app = updateModel(t, app, runEventMsg{Event: whatttft.RunEvent{Kind: whatttft.EventRunStarted, TotalRequests: 1, MeasuredRequests: 1}})
	app = updateModel(t, app, runEventMsg{Event: whatttft.RunEvent{Kind: whatttft.EventRequestFinished, RequestID: record.RequestID, Record: &record}})
	app = updateModel(t, app, keyPress("r"))

	app = updateModel(t, app, keyPress("/"))
	if app.requestExplorer.Mode != requestExplorerModeFilter {
		t.Fatalf("mode after / = %d, want filter", app.requestExplorer.Mode)
	}
	app = updateModel(t, app, keyPress("e"))
	app = updateModel(t, app, keyPress("r"))
	app = updateModel(t, app, keyPress("r"))
	app = updateModel(t, app, keyPress("enter"))
	if app.requestExplorer.Mode != requestExplorerModeList || app.requestExplorer.CommittedFilter != "err" {
		t.Fatalf("mode/filter after apply = %d/%q, want list/err", app.requestExplorer.Mode, app.requestExplorer.CommittedFilter)
	}
	if content := app.View().Content; !strings.Contains(content, "filter=err") {
		t.Fatalf("request explorer did not render committed filter:\n%s", content)
	}

	app = updateModel(t, app, keyPress("/"))
	app = updateModel(t, app, keyPress("x"))
	app = updateModel(t, app, keyPress("esc"))
	if app.requestExplorer.CommittedFilter != "err" || app.requestExplorer.FilterInput != "err" {
		t.Fatalf("discard changed committed/draft filter = %q/%q, want err/err", app.requestExplorer.CommittedFilter, app.requestExplorer.FilterInput)
	}
}

// TestRequestExplorerFilterApplyClearAndSelection verifies filter editing, invalid drafts, clearing, and nearest-selection behavior.
func TestRequestExplorerFilterApplyClearAndSelection(t *testing.T) {
	app := newModel(nil)
	app = updateModel(t, app, tea.WindowSizeMsg{Width: 120, Height: 30})
	app = updateModel(t, app, runEventMsg{Event: whatttft.RunEvent{Kind: whatttft.EventRunStarted, TotalRequests: 3, MeasuredRequests: 3}})
	records := []whatttft.RequestRecord{
		tuiTestRecord("req-000000", "", 10, 100, nil),
		tuiTestRecord("req-000001", "", 20, 200, &whatttft.ErrorRecord{Category: "provider"}),
		tuiTestRecord("req-000002", "", 30, 300, nil),
	}
	records[0].Model = "alpha"
	records[1].Model = "beta"
	records[2].Model = "alpha"
	for _, record := range records {
		app = updateModel(t, app, runEventMsg{Event: whatttft.RunEvent{Kind: whatttft.EventRequestFinished, RequestID: record.RequestID, Record: &record}})
	}
	app = updateModel(t, app, keyPress("r"))
	app = updateModel(t, app, keyPress("j"))
	if app.requestExplorer.CursorRequestID != "req-000001" {
		t.Fatalf("cursor before filtering = %q, want req-000001", app.requestExplorer.CursorRequestID)
	}

	app = updateModel(t, app, keyPress("/"))
	app = typeFilterText(t, app, "model:beta")
	app = updateModel(t, app, keyPress("enter"))
	if app.requestExplorer.Mode != requestExplorerModeList || app.requestExplorer.CursorRequestID != "req-000001" {
		t.Fatalf("after model:beta mode/cursor = %d/%q, want list/req-000001", app.requestExplorer.Mode, app.requestExplorer.CursorRequestID)
	}
	if content := app.View().Content; !strings.Contains(content, "requests=1/3") || !strings.Contains(content, "filter=model:beta") {
		t.Fatalf("filtered model:beta view missing status:\n%s", content)
	}

	app = updateModel(t, app, keyPress("/"))
	app = updateModel(t, app, keyPress("ctrl+u"))
	app = typeFilterText(t, app, "model:alpha")
	app = updateModel(t, app, keyPress("enter"))
	if app.requestExplorer.CursorRequestID != "req-000000" {
		t.Fatalf("cursor after filtering out selected row = %q, want nearest req-000000", app.requestExplorer.CursorRequestID)
	}

	app = updateModel(t, app, keyPress("ctrl+u"))
	if app.requestExplorer.CommittedFilter != "" || app.requestExplorer.CursorRequestID != "req-000000" {
		t.Fatalf("after clear filter/cursor = %q/%q, want empty/req-000000", app.requestExplorer.CommittedFilter, app.requestExplorer.CursorRequestID)
	}
	if content := app.View().Content; !strings.Contains(content, "requests=3/3") || !strings.Contains(content, "filter=none") {
		t.Fatalf("cleared filter view missing status:\n%s", content)
	}

	app = updateModel(t, app, keyPress("/"))
	app = typeFilterText(t, app, "status:nope")
	app = updateModel(t, app, keyPress("enter"))
	if app.requestExplorer.Mode != requestExplorerModeFilter || app.requestExplorer.CommittedFilter != "" || app.requestExplorer.FilterError == "" {
		t.Fatalf("invalid draft mode/committed/error = %d/%q/%q, want filter/empty/error", app.requestExplorer.Mode, app.requestExplorer.CommittedFilter, app.requestExplorer.FilterError)
	}
	if content := app.View().Content; !strings.Contains(content, "filter error:") {
		t.Fatalf("invalid draft did not render parse error:\n%s", content)
	}
}

// TestRequestExplorerSortAndShortcutToggles verifies shortcut filters update active filter and sort state deterministically.
func TestRequestExplorerSortAndShortcutToggles(t *testing.T) {
	app := newModel(nil)
	app = updateModel(t, app, tea.WindowSizeMsg{Width: 120, Height: 30})
	app = updateModel(t, app, runEventMsg{Event: whatttft.RunEvent{Kind: whatttft.EventRunStarted, TotalRequests: 2, MeasuredRequests: 1}})
	ok := tuiTestRecord("req-ok", "", 10, 100, nil)
	err := tuiTestRecord("req-error", "", 90, 500, &whatttft.ErrorRecord{Category: "provider"})
	err.Warmup = true
	for _, record := range []whatttft.RequestRecord{ok, err} {
		app = updateModel(t, app, runEventMsg{Event: whatttft.RunEvent{Kind: whatttft.EventRequestFinished, RequestID: record.RequestID, Record: &record}})
	}
	app = updateModel(t, app, keyPress("r"))

	app = updateModel(t, app, keyPress("s"))
	if app.requestExplorer.Sort != requestSortSlowestTTFT {
		t.Fatalf("sort after s = %q, want slowest-ttft", app.requestExplorer.Sort)
	}
	if content := app.View().Content; !strings.Contains(content, "sort=slowest-ttft") || app.requestExplorer.CursorRequestID != "req-ok" {
		t.Fatalf("slowest sort did not update status while preserving cursor:\n%s", content)
	}

	app = updateModel(t, app, keyPress("e"))
	if app.requestExplorer.CommittedFilter != "outcome:error sort:-ttft" || app.requestExplorer.CursorRequestID != "req-error" {
		t.Fatalf("after e filter/cursor = %q/%q, want errors-only req-error", app.requestExplorer.CommittedFilter, app.requestExplorer.CursorRequestID)
	}
	app = updateModel(t, app, keyPress("w"))
	if !strings.Contains(app.requestExplorer.CommittedFilter, "phase:measured") {
		t.Fatalf("after first w filter = %q, want measured phase", app.requestExplorer.CommittedFilter)
	}
	app = updateModel(t, app, keyPress("w"))
	if !strings.Contains(app.requestExplorer.CommittedFilter, "phase:warmup") {
		t.Fatalf("after second w filter = %q, want warmup phase", app.requestExplorer.CommittedFilter)
	}
}

// TestRequestExplorerFilterDisplayRedactsSecretLikeQueries verifies filter rendering does not echo API-key-like strings.
func TestRequestExplorerFilterDisplayRedactsSecretLikeQueries(t *testing.T) {
	app := newModel(nil)
	record := tuiTestRecord("req-000000", "", 10, 100, nil)
	app = updateModel(t, app, tea.WindowSizeMsg{Width: 100, Height: 28})
	app = updateModel(t, app, runEventMsg{Event: whatttft.RunEvent{Kind: whatttft.EventRunStarted, TotalRequests: 1, MeasuredRequests: 1}})
	app = updateModel(t, app, runEventMsg{Event: whatttft.RunEvent{Kind: whatttft.EventRequestFinished, RequestID: record.RequestID, Record: &record}})
	app = updateModel(t, app, keyPress("r"))
	filters, sortOrder, err := parseRequestFilterQuery("id:sk-secret-token")
	if err != nil {
		t.Fatalf("parse secret-like query: %v", err)
	}
	app.requestExplorer.Filters = filters
	app.requestExplorer.Sort = sortOrder
	app.requestExplorer.CommittedFilter = "id:sk-secret-token"
	content := app.View().Content
	if strings.Contains(content, "sk-secret-token") {
		t.Fatalf("secret-like filter query rendered:\n%s", content)
	}
	if !strings.Contains(content, "[redacted]") {
		t.Fatalf("redacted filter label missing:\n%s", content)
	}
}

// TestRequestExplorerRunListRenderingUpdates verifies run request-list rendering updates as request_finished events arrive.
func TestRequestExplorerRunListRenderingUpdates(t *testing.T) {
	app := newModel(nil)
	app = updateModel(t, app, tea.WindowSizeMsg{Width: 100, Height: 28})
	app = updateModel(t, app, runEventMsg{Event: whatttft.RunEvent{Kind: whatttft.EventRunStarted, Provider: "openai", Model: "gpt-test", TotalRequests: 2, MeasuredRequests: 2}})
	app = updateModel(t, app, keyPress("r"))
	if content := app.View().Content; !strings.Contains(content, "no requests completed yet") {
		t.Fatalf("empty request explorer missing waiting state:\n%s", content)
	}

	first := tuiTestRecord("req-000000", "", 10, 100, nil)
	app = updateModel(t, app, runEventMsg{Event: whatttft.RunEvent{Kind: whatttft.EventRequestFinished, RequestID: first.RequestID, Record: &first}})
	content := app.View().Content
	for _, want := range []string{"requests=1/1", "req-000000", "meas", "ok", "10.0", "100.0", "gpt-test"} {
		if !strings.Contains(content, want) {
			t.Fatalf("run request explorer missing %q after first request:\n%s", want, content)
		}
	}

	second := tuiTestRecord("req-000001", "", 20, 200, &whatttft.ErrorRecord{Category: "provider"})
	second.HTTP.StatusCode = 500
	app = updateModel(t, app, runEventMsg{Event: whatttft.RunEvent{Kind: whatttft.EventRequestFinished, RequestID: second.RequestID, Record: &second}})
	content = app.View().Content
	for _, want := range []string{"requests=2/2", "req-000001", "err", "500"} {
		if !strings.Contains(content, want) {
			t.Fatalf("run request explorer missing %q after second request:\n%s", want, content)
		}
	}
}

// TestRequestExplorerBenchWideListRenderingAndVisibility verifies bench request rows include target/model columns and respect chart visibility.
func TestRequestExplorerBenchWideListRenderingAndVisibility(t *testing.T) {
	app := benchmarkDashboardAppWithRecords(t)
	app = updateModel(t, app, tea.WindowSizeMsg{Width: 150, Height: 36})
	app = updateModel(t, app, keyPress("r"))
	content := app.View().Content
	for _, want := range []string{"target", "model", "stream", "ttfb", "tokens", "cache", "conn", "output", "target-a", "gpt-a", "target-b", "gpt-b", "requests=2/2"} {
		if !strings.Contains(content, want) {
			t.Fatalf("wide bench request explorer missing %q:\n%s", want, content)
		}
	}

	app = updateModel(t, app, keyPress("esc"))
	app = updateModel(t, app, runEventMsg{Event: whatttft.RunEvent{Kind: whatttft.EventBenchmarkFinished}})
	app = updateModel(t, app, keyPress("j"))
	app = updateModel(t, app, keyPress("space"))
	app = updateModel(t, app, keyPress("r"))
	content = app.View().Content
	if !strings.Contains(content, "requests=1/2") || !strings.Contains(content, "hidden_by_chart=1") || !strings.Contains(content, "target-a") {
		t.Fatalf("filtered bench request explorer missing visible target/hidden count:\n%s", content)
	}
	if strings.Contains(content, "target-b-req-000000") {
		t.Fatalf("hidden target request still rendered:\n%s", content)
	}

	filters, sortOrder, err := parseRequestFilterQuery("hidden:all")
	if err != nil {
		t.Fatalf("parse hidden override: %v", err)
	}
	app.requestExplorer.Filters = filters
	app.requestExplorer.Sort = sortOrder
	app.requestExplorer.CommittedFilter = "hidden:all"
	content = app.View().Content
	if !strings.Contains(content, "requests=2/2") || !strings.Contains(content, "target-b-req-000000") {
		t.Fatalf("hidden:all did not reveal hidden target requests:\n%s", content)
	}
}

// TestRequestExplorerNarrowListRendering verifies narrow terminals use compact request columns.
func TestRequestExplorerNarrowListRendering(t *testing.T) {
	app := newModel(nil)
	app = updateModel(t, app, tea.WindowSizeMsg{Width: 62, Height: 18})
	record := tuiTestRecord("req-compact", "target-a", 10, 100, nil)
	app = updateModel(t, app, runEventMsg{Event: whatttft.RunEvent{Kind: whatttft.EventRunStarted, Model: "gpt-compact", TotalRequests: 1, MeasuredRequests: 1}})
	app = updateModel(t, app, runEventMsg{Event: whatttft.RunEvent{Kind: whatttft.EventRequestFinished, RequestID: record.RequestID, Record: &record}})
	app = updateModel(t, app, keyPress("r"))
	content := app.View().Content
	for _, want := range []string{"request", "ph", "model", "req-compact", "gpt-test"} {
		if !strings.Contains(content, want) {
			t.Fatalf("compact request explorer missing %q:\n%s", want, content)
		}
	}
	if got := dashboardMaxLineWidth(content); got > 62 {
		t.Fatalf("compact request explorer width = %d, want <= 62\n%s", got, content)
	}
}

func typeFilterText(t *testing.T, app model, text string) model {
	t.Helper()
	for _, char := range text {
		app = updateModel(t, app, keyPress(string(char)))
	}
	return app
}

// TestRequestExplorerNoMatchesState verifies active filters that match nothing render a useful empty state.
func TestRequestExplorerNoMatchesState(t *testing.T) {
	app := newModel(nil)
	app = updateModel(t, app, tea.WindowSizeMsg{Width: 100, Height: 28})
	record := tuiTestRecord("req-000000", "", 10, 100, nil)
	app = updateModel(t, app, runEventMsg{Event: whatttft.RunEvent{Kind: whatttft.EventRunStarted, TotalRequests: 1, MeasuredRequests: 1}})
	app = updateModel(t, app, runEventMsg{Event: whatttft.RunEvent{Kind: whatttft.EventRequestFinished, RequestID: record.RequestID, Record: &record}})
	app = updateModel(t, app, keyPress("r"))
	app.requestExplorer.CommittedFilter = "does-not-match"
	content := app.View().Content
	if !strings.Contains(content, "no requests match current view") || !strings.Contains(content, "filter=does-not-match") {
		t.Fatalf("request explorer missing no-match state:\n%s", content)
	}
}
