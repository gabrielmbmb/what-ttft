package eventbus

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gabrielmbmb/what-ttft/pkg/whatttft"
)

// TestJSONLSinkWritesParseableEvents verifies JSONL event output can be parsed back into RunEvent values.
func TestJSONLSinkWritesParseableEvents(t *testing.T) {
	path := filepath.Join(t.TempDir(), "nested", "events.jsonl")
	sink, err := NewJSONLSink(path)
	if err != nil {
		t.Fatalf("new JSONL sink: %v", err)
	}

	if err := sink.Publish(context.Background(), whatttft.RunEvent{Sequence: 1, Kind: whatttft.EventRunStarted}); err != nil {
		t.Fatalf("publish first event: %v", err)
	}
	if err := sink.Publish(context.Background(), whatttft.RunEvent{Sequence: 2, Kind: whatttft.EventReportWriteFinished, OutputDir: "runs/example"}); err != nil {
		t.Fatalf("publish second event: %v", err)
	}
	if err := sink.Close(context.Background()); err != nil {
		t.Fatalf("close JSONL sink: %v", err)
	}

	events := readJSONLEvents(t, path)
	if len(events) != 2 {
		t.Fatalf("events = %d, want 2", len(events))
	}
	if events[0].Kind != whatttft.EventRunStarted || events[1].Kind != whatttft.EventReportWriteFinished {
		t.Fatalf("event kinds = %#v, want run_started/report_write_finished", events)
	}
	if events[1].OutputDir != "runs/example" {
		t.Fatalf("output dir = %q, want runs/example", events[1].OutputDir)
	}
}

// TestJSONLSinkOpenErrors verifies invalid output paths fail clearly.
func TestJSONLSinkOpenErrors(t *testing.T) {
	if _, err := NewJSONLSink(""); err == nil {
		t.Fatal("expected empty path error")
	}

	dir := t.TempDir()
	if _, err := NewJSONLSink(dir); err == nil {
		t.Fatal("expected directory path open error")
	}
}

// TestJSONLSinkPublishError verifies write failures are returned from Publish.
func TestJSONLSinkPublishError(t *testing.T) {
	sink := newJSONLSinkWriter(failingWriter{}, nil)
	err := sink.Publish(context.Background(), whatttft.RunEvent{Kind: whatttft.EventRunStarted})
	if err == nil {
		t.Fatal("expected publish error")
	}
	if !strings.Contains(err.Error(), "event JSONL") {
		t.Fatalf("publish error = %q, want event JSONL context", err)
	}
}

// TestJSONLSinkClosedPublishError verifies publishing after Close returns an error instead of writing.
func TestJSONLSinkClosedPublishError(t *testing.T) {
	path := filepath.Join(t.TempDir(), "events.jsonl")
	sink, err := NewJSONLSink(path)
	if err != nil {
		t.Fatalf("new JSONL sink: %v", err)
	}
	if err := sink.Close(context.Background()); err != nil {
		t.Fatalf("close JSONL sink: %v", err)
	}
	if err := sink.Publish(context.Background(), whatttft.RunEvent{Kind: whatttft.EventRunStarted}); err == nil {
		t.Fatal("expected publish-after-close error")
	}
}

// TestJSONLSinkDoesNotWriteSecretsWhenEventsAreRedacted verifies safe events do not gain secret-looking data during encoding.
func TestJSONLSinkDoesNotWriteSecretsWhenEventsAreRedacted(t *testing.T) {
	path := filepath.Join(t.TempDir(), "events.jsonl")
	sink, err := NewJSONLSink(path)
	if err != nil {
		t.Fatalf("new JSONL sink: %v", err)
	}
	event := whatttft.RunEvent{
		Kind:      whatttft.EventRequestFinished,
		Provider:  "openai",
		Model:     "test-model",
		RequestID: "req-000000",
		Record: &whatttft.RequestRecord{
			RequestID:      "req-000000",
			Provider:       "openai",
			Model:          "test-model",
			ScenarioName:   "short",
			CacheMode:      whatttft.CacheBust,
			ConnectionMode: whatttft.WarmConnections,
			PromptHash:     strings.Repeat("a", 64),
		},
	}
	if publishErr := sink.Publish(context.Background(), event); publishErr != nil {
		t.Fatalf("publish event: %v", publishErr)
	}
	if closeErr := sink.Close(context.Background()); closeErr != nil {
		t.Fatalf("close sink: %v", closeErr)
	}

	//nolint:gosec // Test reads the explicit temp-file path it just wrote.
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read event JSONL: %v", err)
	}
	content := string(data)
	for _, forbidden := range []string{"sk-test", "Authorization", "Bearer", "api_key"} {
		if strings.Contains(content, forbidden) {
			t.Fatalf("event JSONL contains forbidden %q: %s", forbidden, content)
		}
	}
}

func readJSONLEvents(t *testing.T, path string) []whatttft.RunEvent {
	t.Helper()

	//nolint:gosec // Test reads the explicit temp-file path it just wrote.
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read JSONL events: %v", err)
	}
	lines := strings.Split(strings.TrimSpace(string(data)), "\n")
	events := make([]whatttft.RunEvent, 0, len(lines))
	for _, line := range lines {
		var event whatttft.RunEvent
		if err := json.Unmarshal([]byte(line), &event); err != nil {
			t.Fatalf("unmarshal event line %q: %v", line, err)
		}
		events = append(events, event)
	}

	return events
}

type failingWriter struct{}

func (failingWriter) Write([]byte) (int, error) {
	return 0, errors.New("synthetic write failure")
}
