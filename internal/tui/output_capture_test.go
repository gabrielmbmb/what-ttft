package tui

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gabrielmbmb/what-ttft/pkg/whatttft"
)

// TestOutputCaptureLoaderHandlesFakeServerChunkShapes verifies captured chunks reconstruct only visible output text.
func TestOutputCaptureLoaderHandlesFakeServerChunkShapes(t *testing.T) {
	outputDir := t.TempDir()
	writeTestChunks(t, outputDir,
		whatttft.ChunkRecord{RequestID: "req-success", Index: 0, Empty: true},
		whatttft.ChunkRecord{RequestID: "req-success", Index: 1, Role: "assistant", Empty: true},
		whatttft.ChunkRecord{RequestID: "req-success", Index: 3, Content: " world"},
		whatttft.ChunkRecord{RequestID: "req-success", Index: 2, Content: "Hello"},
		whatttft.ChunkRecord{RequestID: "req-success", Index: 4, Empty: true, UsageChunk: true},
		whatttft.ChunkRecord{RequestID: "req-empty", Index: 0, Empty: true},
		whatttft.ChunkRecord{RequestID: "req-empty", Index: 1, Role: "assistant", Empty: true},
		whatttft.ChunkRecord{RequestID: "req-empty", Index: 2, Empty: true, UsageChunk: true},
	)

	captures, err := loadOutputCaptures(outputDir)
	if err != nil {
		t.Fatalf("load output captures: %v", err)
	}
	if captures["req-success"].Content != "Hello world" {
		t.Fatalf("req-success content = %q, want Hello world", captures["req-success"].Content)
	}
	if captures["req-success"].VisibleChunks != 2 {
		t.Fatalf("req-success visible chunks = %d, want 2", captures["req-success"].VisibleChunks)
	}
	if captures["req-empty"].Content != "" || captures["req-empty"].VisibleChunks != 0 {
		t.Fatalf("req-empty capture = %#v, want no visible output", captures["req-empty"])
	}
	if _, ok := captures["req-failed-no-output"]; ok {
		t.Fatalf("failed request without chunks should not get synthetic output: %#v", captures["req-failed-no-output"])
	}
}

// TestOutputCaptureLoaderTruncatesLargeGeneratedOutput verifies retained TUI output is bounded.
func TestOutputCaptureLoaderTruncatesLargeGeneratedOutput(t *testing.T) {
	outputDir := t.TempDir()
	writeTestChunks(t, outputDir, whatttft.ChunkRecord{RequestID: "req-large", Index: 0, Content: strings.Repeat("x", outputCaptureMaxBytes+32)})

	captures, err := loadOutputCaptures(outputDir)
	if err != nil {
		t.Fatalf("load output captures: %v", err)
	}
	capture := captures["req-large"]
	if !capture.Truncated {
		t.Fatalf("capture truncated = false, want true: %#v", capture)
	}
	if !strings.Contains(capture.Content, "output truncated in TUI") {
		t.Fatalf("truncation marker missing from content tail: %q", capture.Content[len(capture.Content)-80:])
	}
	if capture.OriginalBytes != outputCaptureMaxBytes+32 {
		t.Fatalf("original bytes = %d, want %d", capture.OriginalBytes, outputCaptureMaxBytes+32)
	}
}

func writeTestChunks(t *testing.T, outputDir string, chunks ...whatttft.ChunkRecord) {
	t.Helper()
	var builder strings.Builder
	encoder := json.NewEncoder(&builder)
	for _, chunk := range chunks {
		if err := encoder.Encode(chunk); err != nil {
			t.Fatalf("encode chunk: %v", err)
		}
	}
	if err := os.WriteFile(filepath.Join(outputDir, chunksJSONLFileName), []byte(builder.String()), 0o600); err != nil {
		t.Fatalf("write chunks.jsonl: %v", err)
	}
}
