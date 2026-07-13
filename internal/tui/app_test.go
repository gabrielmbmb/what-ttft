package tui

import (
	"strings"
	"testing"

	"github.com/gabrielmbmb/what-ttft/pkg/whatttft"

	tea "charm.land/bubbletea/v2"
)

// TestModelWindowSizeUpdatesDimensions verifies terminal resize messages update layout state.
func TestModelWindowSizeUpdatesDimensions(t *testing.T) {
	app := newModel(nil)
	updated := updateModel(t, app, tea.WindowSizeMsg{Width: 100, Height: 30})

	if updated.width != 100 || updated.height != 30 {
		t.Fatalf("model size = %dx%d, want 100x30", updated.width, updated.height)
	}
	if updated.help.Width() != 100 {
		t.Fatalf("help width = %d, want 100", updated.help.Width())
	}
}

// TestModelEventsUpdateStore verifies benchmark events update the root model store.
func TestModelEventsUpdateStore(t *testing.T) {
	app := newModel(nil)
	app = updateModel(t, app, runEventMsg{Event: whatttft.RunEvent{
		Kind:             whatttft.EventRunStarted,
		Provider:         "openai",
		Model:            "gpt-test",
		ScenarioName:     "short",
		TotalRequests:    2,
		MeasuredRequests: 2,
	}})
	if !app.running || app.store.provider != "openai" || app.store.model != "gpt-test" {
		t.Fatalf("run started state = running %t provider %q model %q", app.running, app.store.provider, app.store.model)
	}

	app = updateModel(t, app, runEventMsg{Event: whatttft.RunEvent{
		Kind:       whatttft.EventTargetStarted,
		TargetID:   "target-a",
		TargetName: "Target A",
	}})
	if got := app.store.currentTarget(); got != "target-a (Target A)" {
		t.Fatalf("current target = %q, want target-a (Target A)", got)
	}

	app = updateModel(t, app, runEventMsg{Event: whatttft.RunEvent{
		Kind:      whatttft.EventRequestDispatched,
		RequestID: "target-a-req-000000",
	}})
	if got := len(app.store.activeRequests); got != 1 {
		t.Fatalf("active requests = %d, want 1", got)
	}

	record := whatttft.RequestRecord{RequestID: "target-a-req-000000", Provider: "openai", Model: "gpt-test"}
	app = updateModel(t, app, runEventMsg{Event: whatttft.RunEvent{
		Kind:               whatttft.EventRequestFinished,
		RequestID:          record.RequestID,
		Record:             &record,
		CompletedRequests:  1,
		SuccessfulRequests: 1,
	}})
	if got := len(app.store.activeRequests); got != 0 {
		t.Fatalf("active requests after finish = %d, want 0", got)
	}
	if got := len(app.store.records); got != 1 {
		t.Fatalf("stored records = %d, want 1", got)
	}

	app = updateModel(t, app, runEventMsg{Event: whatttft.RunEvent{Kind: whatttft.EventReportWriteStarted, OutputDir: "runs/out"}})
	if app.store.reportStatus != "writing reports" || app.store.outputDir != "runs/out" {
		t.Fatalf("report start status/output = %q/%q", app.store.reportStatus, app.store.outputDir)
	}
	app = updateModel(t, app, runEventMsg{Event: whatttft.RunEvent{Kind: whatttft.EventReportWriteFinished, OutputDir: "runs/out"}})
	if !app.completed || app.store.reportStatus != "reports written" {
		t.Fatalf("report finish completed/status = %t/%q", app.completed, app.store.reportStatus)
	}
}

// TestModelViewReturnsAltScreen verifies the placeholder dashboard opts into Bubble Tea v2 alt-screen rendering.
func TestModelViewReturnsAltScreen(t *testing.T) {
	app := newModel(nil)
	view := app.View()

	if strings.TrimSpace(view.Content) == "" {
		t.Fatal("view content is empty")
	}
	if !view.AltScreen {
		t.Fatal("view AltScreen = false, want true")
	}
	if view.WindowTitle != "what-ttft" {
		t.Fatalf("window title = %q, want what-ttft", view.WindowTitle)
	}
	if view.MouseMode != tea.MouseModeNone {
		t.Fatalf("mouse mode = %v, want none", view.MouseMode)
	}
}

// TestModelQuitConfirmation verifies q/ctrl+c asks for confirmation while a benchmark is running.
func TestModelQuitConfirmation(t *testing.T) {
	app := newModel(nil)
	app = updateModel(t, app, runEventMsg{Event: whatttft.RunEvent{Kind: whatttft.EventRunStarted}})

	updated, cmd := app.Update(keyPress("q"))
	app = assertModel(t, updated)
	if !app.confirmingCancel || cmd != nil {
		t.Fatalf("after q while running confirming/cmd = %t/%v, want true/nil", app.confirmingCancel, cmd)
	}

	updated, cmd = app.Update(keyPress("n"))
	app = assertModel(t, updated)
	if app.confirmingCancel || cmd != nil {
		t.Fatalf("after n confirming/cmd = %t/%v, want false/nil", app.confirmingCancel, cmd)
	}

	app = updateModel(t, app, keyPress("q"))
	updated, cmd = app.Update(keyPress("y"))
	app = assertModel(t, updated)
	if !app.canceled || cmd == nil {
		t.Fatalf("after y canceled/cmd = %t/%v, want true/non-nil", app.canceled, cmd)
	}
}

// TestModelFailureDialogKeysExitImmediately verifies fatal-error dialog shortcuts do not ask for cancellation confirmation.
func TestModelFailureDialogKeysExitImmediately(t *testing.T) {
	for _, keyName := range []string{"enter", "q", "esc"} {
		app := newModel(nil)
		app = updateModel(t, app, runEventMsg{Event: whatttft.RunEvent{Kind: whatttft.EventBenchmarkFailed, Error: &whatttft.RunEventError{Message: "preflight failed"}}})
		updated, cmd := app.Update(keyPress(keyName))
		app = assertModel(t, updated)
		if app.confirmingCancel || cmd == nil {
			t.Fatalf("key %q after failure confirming/cmd = %t/%v, want false/non-nil", keyName, app.confirmingCancel, cmd)
		}
	}
}

// TestModelQuitAfterCompletionDoesNotConfirm verifies q exits immediately after completion.
func TestModelQuitAfterCompletionDoesNotConfirm(t *testing.T) {
	app := newModel(nil)
	app = updateModel(t, app, runEventMsg{Event: whatttft.RunEvent{Kind: whatttft.EventRunFinished}})

	updated, cmd := app.Update(keyPress("q"))
	app = assertModel(t, updated)
	if app.confirmingCancel || cmd == nil {
		t.Fatalf("after completed q confirming/cmd = %t/%v, want false/non-nil", app.confirmingCancel, cmd)
	}
}

// TestModelHelpAndPaneKeys verifies help, pane, and focus keybindings update state.
func TestModelHelpAndPaneKeys(t *testing.T) {
	app := newModel(nil)
	app = updateModel(t, app, keyPress("?"))
	if !app.help.ShowAll {
		t.Fatal("help ShowAll = false, want true")
	}
	app = updateModel(t, app, keyPress("2"))
	if app.pane != paneTTFT {
		t.Fatalf("pane = %d, want paneTTFT", app.pane)
	}
	app = updateModel(t, app, keyPress("tab"))
	if app.focus != 1 {
		t.Fatalf("focus = %d, want 1", app.focus)
	}
}

// TestModelBenchmarkTargetNavigationKeys verifies benchmark target selection, detail-mode, and post-run visibility key handling without a terminal.
func TestModelBenchmarkTargetNavigationKeys(t *testing.T) {
	app := newModel(nil)
	app = updateModel(t, app, runEventMsg{Event: whatttft.RunEvent{Kind: whatttft.EventBenchmarkStarted, Targets: []whatttft.RunEventTarget{{TargetID: "target-a"}, {TargetID: "target-b"}}}})
	if got := app.store.selectedTargetID(); got != "target-a" {
		t.Fatalf("initial selected target = %q, want target-a", got)
	}

	app = updateModel(t, app, keyPress("j"))
	if got := app.store.selectedTargetID(); got != "target-b" {
		t.Fatalf("selected target after j = %q, want target-b", got)
	}
	app = updateModel(t, app, keyPress("enter"))
	if !app.store.targetDetail {
		t.Fatal("target detail = false after enter, want true")
	}
	app = updateModel(t, app, keyPress("esc"))
	if app.store.targetDetail {
		t.Fatal("target detail = true after esc, want false")
	}
	app = updateModel(t, app, keyPress("space"))
	if !app.store.targetVisible("target-b") {
		t.Fatal("target-b hidden while benchmark is still running; visibility toggles should be post-run only")
	}
	app = updateModel(t, app, runEventMsg{Event: whatttft.RunEvent{Kind: whatttft.EventBenchmarkFinished}})
	app = updateModel(t, app, keyPress("space"))
	if app.store.targetVisible("target-b") {
		t.Fatal("target-b visible after post-run space toggle, want hidden")
	}
	app = updateModel(t, app, keyPress("a"))
	if !app.store.targetVisible("target-b") {
		t.Fatal("target-b hidden after show-all key, want visible")
	}
	app = updateModel(t, app, keyPress("k"))
	if got := app.store.selectedTargetID(); got != "target-a" {
		t.Fatalf("selected target after k = %q, want target-a", got)
	}
}

// TestModelInitReadsEventsAndClosedChannel verifies Init consumes event-channel messages without a terminal.
func TestModelInitReadsEventsAndClosedChannel(t *testing.T) {
	events := make(chan whatttft.RunEvent, 1)
	events <- whatttft.RunEvent{Kind: whatttft.EventRunStarted}
	close(events)

	app := newModel(events)
	cmd := app.Init()
	if cmd == nil {
		t.Fatal("Init command is nil")
	}
	if msg, ok := cmd().(runEventMsg); !ok || msg.Event.Kind != whatttft.EventRunStarted {
		t.Fatalf("first init message = %#v, want runEventMsg", msg)
	}
	if msg := waitForRunEvent(events)(); msg != (eventChannelClosedMsg{}) {
		t.Fatalf("closed channel message = %#v, want eventChannelClosedMsg", msg)
	}
}

func updateModel(t *testing.T, app model, msg tea.Msg) model {
	t.Helper()

	updated, _ := app.Update(msg)
	return assertModel(t, updated)
}

func assertModel(t *testing.T, value tea.Model) model {
	t.Helper()

	app, ok := value.(model)
	if !ok {
		t.Fatalf("updated model type = %T, want tui.model", value)
	}
	return app
}

func keyPress(value string) tea.KeyPressMsg {
	switch value {
	case "tab":
		return tea.KeyPressMsg(tea.Key{Code: tea.KeyTab})
	case "enter":
		return tea.KeyPressMsg(tea.Key{Code: tea.KeyEnter})
	case "esc":
		return tea.KeyPressMsg(tea.Key{Code: tea.KeyEsc})
	case "space":
		return tea.KeyPressMsg(tea.Key{Code: tea.KeySpace})
	case "home":
		return tea.KeyPressMsg(tea.Key{Code: tea.KeyHome})
	case "end":
		return tea.KeyPressMsg(tea.Key{Code: tea.KeyEnd})
	case "pgup":
		return tea.KeyPressMsg(tea.Key{Code: tea.KeyPgUp})
	case "pgdown":
		return tea.KeyPressMsg(tea.Key{Code: tea.KeyPgDown})
	}
	if strings.HasPrefix(value, "ctrl+") && len(value) == len("ctrl+")+1 {
		return tea.KeyPressMsg(tea.Key{Code: rune(value[len(value)-1]), Mod: tea.ModCtrl})
	}
	return tea.KeyPressMsg(tea.Key{Text: value, Code: []rune(value)[0]})
}
