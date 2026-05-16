package sse

import (
	"errors"
	"io"
	"strings"
	"testing"
)

// TestParserNextSingleDataEvent verifies a basic data-only SSE event is parsed.
func TestParserNextSingleDataEvent(t *testing.T) {
	parser := New(strings.NewReader("data: {\"ok\":true}\n\n"))

	event, err := parser.Next()
	if err != nil {
		t.Fatalf("next event: %v", err)
	}

	if string(event.Data) != `{"ok":true}` {
		t.Fatalf("data = %q, want JSON payload", event.Data)
	}
	if event.RawBytes != len("data: {\"ok\":true}\n\n") {
		t.Fatalf("raw bytes = %d, want %d", event.RawBytes, len("data: {\"ok\":true}\n\n"))
	}
}

// TestParserNextCRLF verifies CRLF line endings are accepted.
func TestParserNextCRLF(t *testing.T) {
	parser := New(strings.NewReader("data: hello\r\n\r\n"))

	event, err := parser.Next()
	if err != nil {
		t.Fatalf("next event: %v", err)
	}

	if string(event.Data) != "hello" {
		t.Fatalf("data = %q, want hello", event.Data)
	}
}

// TestParserNextMultiLineData verifies multiple data fields are joined with newlines.
func TestParserNextMultiLineData(t *testing.T) {
	parser := New(strings.NewReader("data: hello\ndata: world\n\n"))

	event, err := parser.Next()
	if err != nil {
		t.Fatalf("next event: %v", err)
	}

	if string(event.Data) != "hello\nworld" {
		t.Fatalf("data = %q, want joined data", event.Data)
	}
}

// TestParserNextIgnoresCommentHeartbeat verifies comment-only heartbeats are not returned as events.
func TestParserNextIgnoresCommentHeartbeat(t *testing.T) {
	parser := New(strings.NewReader(": heartbeat\n\ndata: hello\n\n"))

	event, err := parser.Next()
	if err != nil {
		t.Fatalf("next event: %v", err)
	}

	if string(event.Data) != "hello" {
		t.Fatalf("data = %q, want hello", event.Data)
	}
	if event.RawBytes != len("data: hello\n\n") {
		t.Fatalf("raw bytes = %d, want only data event bytes %d", event.RawBytes, len("data: hello\n\n"))
	}
}

// TestParserNextPreservesCommentsWithinEventRawBytes verifies comments inside an event block count as raw bytes.
func TestParserNextPreservesCommentsWithinEventRawBytes(t *testing.T) {
	input := ": comment\ndata: hello\n\n"
	parser := New(strings.NewReader(input))

	event, err := parser.Next()
	if err != nil {
		t.Fatalf("next event: %v", err)
	}

	if string(event.Data) != "hello" {
		t.Fatalf("data = %q, want hello", event.Data)
	}
	if event.RawBytes != len(input) {
		t.Fatalf("raw bytes = %d, want %d", event.RawBytes, len(input))
	}
}

// TestParserNextEOFAfterCompleteEvent verifies EOF is returned after a complete event is consumed.
func TestParserNextEOFAfterCompleteEvent(t *testing.T) {
	parser := New(strings.NewReader("data: hello\n\n"))

	event, err := parser.Next()
	if err != nil {
		t.Fatalf("next event: %v", err)
	}
	if string(event.Data) != "hello" {
		t.Fatalf("data = %q, want hello", event.Data)
	}

	_, err = parser.Next()
	if !errors.Is(err, io.EOF) {
		t.Fatalf("second next error = %v, want EOF", err)
	}
}

// TestParserNextEOFWithPartialEvent verifies EOF dispatches a final event without a blank line.
func TestParserNextEOFWithPartialEvent(t *testing.T) {
	parser := New(strings.NewReader("data: partial"))

	event, err := parser.Next()
	if err != nil {
		t.Fatalf("next event: %v", err)
	}

	if string(event.Data) != "partial" {
		t.Fatalf("data = %q, want partial", event.Data)
	}
}

// TestParserNextLargeChunk verifies lines larger than bufio.Scanner's default token limit are supported.
func TestParserNextLargeChunk(t *testing.T) {
	large := strings.Repeat("x", 70*1024)
	parser := New(strings.NewReader("data: " + large + "\n\n"))

	event, err := parser.Next()
	if err != nil {
		t.Fatalf("next event: %v", err)
	}

	if string(event.Data) != large {
		t.Fatalf("large data length = %d, want %d", len(event.Data), len(large))
	}
}

// TestParserNextEmptyDataEvent verifies an explicit empty data field is returned.
func TestParserNextEmptyDataEvent(t *testing.T) {
	parser := New(strings.NewReader("data:\n\n"))

	event, err := parser.Next()
	if err != nil {
		t.Fatalf("next event: %v", err)
	}

	if event.Data == nil {
		t.Fatal("empty data event should return a non-nil data slice")
	}
	if len(event.Data) != 0 {
		t.Fatalf("empty data length = %d, want 0", len(event.Data))
	}
}

// TestParserNextNamedFields verifies event, id, and retry fields are captured.
func TestParserNextNamedFields(t *testing.T) {
	parser := New(strings.NewReader("event: chunk\nid: abc\nretry: 1000\ndata: hello\n\n"))

	event, err := parser.Next()
	if err != nil {
		t.Fatalf("next event: %v", err)
	}

	if event.Event != "chunk" {
		t.Fatalf("event type = %q, want chunk", event.Event)
	}
	if event.ID != "abc" {
		t.Fatalf("id = %q, want abc", event.ID)
	}
	if event.Retry != "1000" {
		t.Fatalf("retry = %q, want 1000", event.Retry)
	}
	if string(event.Data) != "hello" {
		t.Fatalf("data = %q, want hello", event.Data)
	}
}

// TestParserNextFieldWithoutColon verifies fields without a colon use an empty value.
func TestParserNextFieldWithoutColon(t *testing.T) {
	parser := New(strings.NewReader("data\n\n"))

	event, err := parser.Next()
	if err != nil {
		t.Fatalf("next event: %v", err)
	}

	if event.Data == nil {
		t.Fatal("data field without colon should still count as an empty data event")
	}
	if len(event.Data) != 0 {
		t.Fatalf("data length = %d, want 0", len(event.Data))
	}
}
