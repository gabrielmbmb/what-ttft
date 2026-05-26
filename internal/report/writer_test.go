package report

import (
	"bufio"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/gabrielmbmb/what-ttft/pkg/whatttft"
)

// TestWriteRunCreatesOutputFilesAndParsesJSON verifies all configured report files are written with parseable JSON.
func TestWriteRunCreatesOutputFilesAndParsesJSON(t *testing.T) {
	outputDir := filepath.Join(t.TempDir(), "run-output")
	result := sampleRunResult()

	//nolint:gosec // Tests use non-secret placeholder credential strings to verify redaction.
	writtenDir, err := WriteRun(WriteOptions{
		OutputDir:  outputDir,
		SaveChunks: true,
		Run: RunMetadata{
			Provider: "openai",
			Model:    "gpt-test",
			BaseURL:  "https://user:secret@example.test/v1?api_key=secret&region=us",
			Scenario: whatttft.Scenario{Name: "short", Prompt: "hello"},
			RunConfig: whatttft.RunConfig{
				Scenario:         whatttft.Scenario{Name: "short", Prompt: "hello"},
				MeasuredRequests: 1,
				CacheMode:        whatttft.CacheBust,
			},
			WallStartUnixNano: 1,
			WallEndUnixNano:   2,
		},
		Result: result,
	})
	if err != nil {
		t.Fatalf("write run: %v", err)
	}
	if writtenDir != outputDir {
		t.Fatalf("written dir = %q, want %q", writtenDir, outputDir)
	}

	for _, name := range []string{runJSONName, requestsJSONLName, chunksJSONLName, summaryJSONName, summaryMarkdownName} {
		if _, err := os.Stat(filepath.Join(outputDir, name)); err != nil {
			t.Fatalf("stat %s: %v", name, err)
		}
	}

	var metadata RunMetadata
	readJSONFile(t, filepath.Join(outputDir, runJSONName), &metadata)
	if metadata.GoVersion == "" || metadata.GOOS == "" || metadata.GOARCH == "" {
		t.Fatalf("runtime metadata should be filled: %#v", metadata)
	}
	if strings.Contains(metadata.BaseURL, "secret") || strings.Contains(metadata.BaseURL, "user:") {
		t.Fatalf("base URL was not redacted: %q", metadata.BaseURL)
	}
	if !strings.Contains(metadata.BaseURL, "region=us") {
		t.Fatalf("non-secret query should be preserved: %q", metadata.BaseURL)
	}

	requests := readJSONLFile[whatttft.RequestRecord](t, filepath.Join(outputDir, requestsJSONLName))
	if len(requests) != 1 {
		t.Fatalf("request records = %d, want 1", len(requests))
	}
	if requests[0].RequestID != "req-000001" {
		t.Fatalf("request ID = %q, want req-000001", requests[0].RequestID)
	}

	chunks := readJSONLFile[whatttft.ChunkRecord](t, filepath.Join(outputDir, chunksJSONLName))
	if len(chunks) != 1 {
		t.Fatalf("chunk records = %d, want 1", len(chunks))
	}
	if chunks[0].Content != "hello" {
		t.Fatalf("chunk content = %q, want hello", chunks[0].Content)
	}

	var summary whatttft.RunSummary
	readJSONFile(t, filepath.Join(outputDir, summaryJSONName), &summary)
	if summary.MeasuredRequests != 1 {
		t.Fatalf("summary measured requests = %d, want 1", summary.MeasuredRequests)
	}
}

// TestWriteRunWritesMultiTargetMetadataAndMarkdown verifies benchmark metadata and comparison reports are self-describing.
func TestWriteRunWritesMultiTargetMetadataAndMarkdown(t *testing.T) {
	outputDir := filepath.Join(t.TempDir(), "bench-output")
	result := sampleMultiTargetRunResult()

	//nolint:gosec // Tests use non-secret placeholder credential strings to verify target URL redaction.
	_, err := WriteRun(WriteOptions{
		OutputDir: outputDir,
		Run: RunMetadata{
			BenchmarkName: "model compare",
			ConfigPath:    "benchmark.yaml",
			ConfigSHA256:  "abc123",
			TargetOrder:   "serial",
			Targets: []RunTargetMetadata{
				{
					TargetID:             "target-a",
					TargetName:           "Target A",
					Provider:             "openai",
					ProviderAPI:          "responses",
					RequestedServiceTier: "default",
					Model:                "gpt-a",
					BaseURL:              "https://user:secret@example.test/v1?api_key=secret&region=a",
					APIKeyEnv:            "OPENAI_API_KEY_A",
					IncludeUsage:         true,
				},
				{
					TargetID:             "target-b",
					Provider:             "openai",
					ProviderAPI:          "responses",
					RequestedServiceTier: "priority",
					Model:                "gpt-b",
					BaseURL:              "https://example.test/v1?token=secret&region=b",
					APIKeyEnv:            "OPENAI_API_KEY_B",
				},
			},
			Provider:  "openai",
			Model:     "model compare",
			Scenario:  whatttft.Scenario{Name: "short", Prompt: "hello"},
			RunConfig: whatttft.RunConfig{Scenario: whatttft.Scenario{Name: "short", Prompt: "hello"}, CacheMode: whatttft.CacheBust, ConnectionMode: whatttft.WarmConnections},
		},
		Result: result,
	})
	if err != nil {
		t.Fatalf("write multi-target run: %v", err)
	}

	var metadata RunMetadata
	readJSONFile(t, filepath.Join(outputDir, runJSONName), &metadata)
	if metadata.BenchmarkName != "model compare" || metadata.ConfigPath != "benchmark.yaml" || metadata.ConfigSHA256 != "abc123" || metadata.TargetOrder != "serial" {
		t.Fatalf("metadata benchmark fields unexpected: %#v", metadata)
	}
	if len(metadata.Targets) != 2 {
		t.Fatalf("metadata targets = %d, want 2", len(metadata.Targets))
	}
	if strings.Contains(metadata.Targets[0].BaseURL, "secret") || strings.Contains(metadata.Targets[0].BaseURL, "user:") {
		t.Fatalf("target base URL was not redacted: %q", metadata.Targets[0].BaseURL)
	}
	if metadata.Targets[0].ObservedServiceTier != "default" || metadata.Targets[0].ObservedServiceTierCounts["default"] != 1 {
		t.Fatalf("target observed tier = %q counts %#v, want default", metadata.Targets[0].ObservedServiceTier, metadata.Targets[0].ObservedServiceTierCounts)
	}
	if metadata.Targets[1].ObservedServiceTier != "priority" || metadata.Targets[1].ObservedServiceTierCounts["priority"] != 1 {
		t.Fatalf("target b observed tier = %q counts %#v, want priority", metadata.Targets[1].ObservedServiceTier, metadata.Targets[1].ObservedServiceTierCounts)
	}

	//nolint:gosec // Tests read fixed report filenames under t.TempDir.
	markdownBytes, err := os.ReadFile(filepath.Join(outputDir, summaryMarkdownName))
	if err != nil {
		t.Fatalf("read summary markdown: %v", err)
	}
	markdown := string(markdownBytes)
	for _, want := range []string{"## Target comparison", "| target | provider | api | requested tier | observed tier | model | ok | err |", "target-a", "target-b", "responses", "e2e_output_tps mean", "generation_delta_output_tps mean", "generation_delta_output_tps count"} {
		if !strings.Contains(markdown, want) {
			t.Fatalf("markdown missing %q:\n%s", want, markdown)
		}
	}
}

// TestWriteRunFailsForNonEmptyOutputDirUnlessOverwrite verifies existing files are protected by default.
func TestWriteRunFailsForNonEmptyOutputDirUnlessOverwrite(t *testing.T) {
	outputDir := t.TempDir()
	stalePath := filepath.Join(outputDir, "stale.txt")
	if err := os.WriteFile(stalePath, []byte("stale"), 0o600); err != nil {
		t.Fatalf("write stale file: %v", err)
	}

	_, err := WriteRun(WriteOptions{OutputDir: outputDir, Result: sampleRunResult()})
	if err == nil {
		t.Fatal("expected non-empty directory error")
	}
	if _, statErr := os.Stat(stalePath); statErr != nil {
		t.Fatalf("stale file should remain after failed write: %v", statErr)
	}

	_, err = WriteRun(WriteOptions{OutputDir: outputDir, Overwrite: true, Result: sampleRunResult()})
	if err != nil {
		t.Fatalf("write with overwrite: %v", err)
	}
	if _, statErr := os.Stat(stalePath); !errors.Is(statErr, os.ErrNotExist) {
		t.Fatalf("stale file stat error = %v, want not exist", statErr)
	}
}

// TestValidateOutputDirFailsForNonEmptyDirectory verifies preflight checks can fail before a benchmark starts.
func TestValidateOutputDirFailsForNonEmptyDirectory(t *testing.T) {
	outputDir := t.TempDir()
	stalePath := filepath.Join(outputDir, "stale.txt")
	if err := os.WriteFile(stalePath, []byte("stale"), 0o600); err != nil {
		t.Fatalf("write stale file: %v", err)
	}

	err := ValidateOutputDir(outputDir, false)
	if err == nil {
		t.Fatal("expected non-empty directory error")
	}
	if _, statErr := os.Stat(stalePath); statErr != nil {
		t.Fatalf("validate should not mutate stale file: %v", statErr)
	}
	if err := ValidateOutputDir(outputDir, true); err != nil {
		t.Fatalf("validate with overwrite: %v", err)
	}
}

// TestWriteRunGeneratesOutputDir verifies empty output directories are automatically placed under runs/.
func TestWriteRunGeneratesOutputDir(t *testing.T) {
	t.Chdir(t.TempDir())
	result := sampleRunResult()
	metadata := RunMetadata{
		Provider: "OpenAI",
		Model:    "gpt/test",
		Scenario: whatttft.Scenario{Name: "Short Chat"},
		RunConfig: whatttft.RunConfig{
			Scenario:       whatttft.Scenario{Name: "Short Chat"},
			CacheMode:      whatttft.CacheBust,
			ConnectionMode: whatttft.WarmConnections,
		},
	}

	outputDir, err := WriteRun(WriteOptions{Run: metadata, Result: result})
	if err != nil {
		t.Fatalf("write run: %v", err)
	}
	if !strings.HasPrefix(outputDir, filepath.Join(defaultOutputRoot, "openai-gpt-test-short-chat-cache-bust-warm-")) {
		t.Fatalf("generated output dir = %q", outputDir)
	}
	if _, err := os.Stat(filepath.Join(outputDir, runJSONName)); err != nil {
		t.Fatalf("stat generated run.json: %v", err)
	}

	var writtenMetadata RunMetadata
	readJSONFile(t, filepath.Join(outputDir, runJSONName), &writtenMetadata)
	if writtenMetadata.RunConfig.OutputDir != outputDir {
		t.Fatalf("run config output dir = %q, want %q", writtenMetadata.RunConfig.OutputDir, outputDir)
	}
}

// TestWriteRunGeneratesBenchmarkOutputDir verifies multi-target benchmark defaults emphasize benchmark and scenario names.
func TestWriteRunGeneratesBenchmarkOutputDir(t *testing.T) {
	t.Chdir(t.TempDir())
	metadata := RunMetadata{
		BenchmarkName: "OpenAI Compare",
		Scenario:      whatttft.Scenario{Name: "Short Chat"},
		RunConfig: whatttft.RunConfig{
			Scenario:       whatttft.Scenario{Name: "Short Chat"},
			CacheMode:      whatttft.CacheBust,
			ConnectionMode: whatttft.WarmConnections,
		},
		Targets: []RunTargetMetadata{{TargetID: "gpt-a", Provider: "openai", ProviderAPI: "responses", Model: "gpt-a"}},
	}

	outputDir, err := WriteRun(WriteOptions{Run: metadata, Result: sampleRunResult()})
	if err != nil {
		t.Fatalf("write benchmark run: %v", err)
	}
	if !strings.HasPrefix(outputDir, filepath.Join(defaultOutputRoot, "openai-compare-short-chat-cache-bust-warm-")) {
		t.Fatalf("generated benchmark output dir = %q", outputDir)
	}
}

// TestResolveOutputDirUsesProvidedDir verifies explicit output directories are preserved exactly.
func TestResolveOutputDirUsesProvidedDir(t *testing.T) {
	outputDir := " custom output "
	resolved := ResolveOutputDir(outputDir, RunMetadata{}, time.Unix(0, 0))

	if resolved != outputDir {
		t.Fatalf("resolved dir = %q, want %q", resolved, outputDir)
	}
}

// TestResolveOutputDirUsesMetadataConfig verifies RunConfig output directories are used before generated defaults.
func TestResolveOutputDirUsesMetadataConfig(t *testing.T) {
	metadata := RunMetadata{RunConfig: whatttft.RunConfig{OutputDir: "metadata-output"}}
	resolved := ResolveOutputDir("", metadata, time.Unix(0, 0))

	if resolved != "metadata-output" {
		t.Fatalf("resolved dir = %q, want metadata-output", resolved)
	}
}

// TestWriteRunOmitsChunksWhenDisabled verifies chunks.jsonl is not written unless SaveChunks is true.
func TestWriteRunOmitsChunksWhenDisabled(t *testing.T) {
	outputDir := filepath.Join(t.TempDir(), "run-output")

	_, err := WriteRun(WriteOptions{OutputDir: outputDir, SaveChunks: false, Result: sampleRunResult()})
	if err != nil {
		t.Fatalf("write run: %v", err)
	}

	if _, err := os.Stat(filepath.Join(outputDir, chunksJSONLName)); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("chunks.jsonl stat error = %v, want not exist", err)
	}
}

// TestWriteRunValidatesRequiredInputs verifies writer options fail fast for missing required fields.
func TestWriteRunValidatesRequiredInputs(t *testing.T) {
	if _, err := WriteRun(WriteOptions{OutputDir: filepath.Join(t.TempDir(), "out")}); err == nil {
		t.Fatal("missing result should fail")
	}
}

// TestRedactURL verifies URL redaction removes credentials and secret-looking query values.
func TestRedactURL(t *testing.T) {
	redacted := RedactURL("https://user:password@example.test/path?token=abc&region=us#fragment")

	if strings.Contains(redacted, "user") || strings.Contains(redacted, "password") || strings.Contains(redacted, "abc") || strings.Contains(redacted, "fragment") {
		t.Fatalf("URL was not redacted: %q", redacted)
	}
	if !strings.Contains(redacted, "token=%5BREDACTED%5D") {
		t.Fatalf("secret query was not redacted: %q", redacted)
	}
	if !strings.Contains(redacted, "region=us") {
		t.Fatalf("non-secret query was not preserved: %q", redacted)
	}
}

func sampleMultiTargetRunResult() *whatttft.RunResult {
	ttftA := 42.0
	ttftB := 36.0
	e2eA := 100.0
	e2eB := 90.0
	e2eTPS := 20.0
	genTPS := 25.0
	records := []whatttft.RequestRecord{
		{
			RequestID:            "target-a-req-000000",
			TargetID:             "target-a",
			TargetName:           "Target A",
			Provider:             "openai",
			Model:                "gpt-a",
			ScenarioName:         "short",
			CacheMode:            whatttft.CacheBust,
			ConnectionMode:       whatttft.WarmConnections,
			RequestedServiceTier: "default",
			ObservedServiceTier:  "default",
			PromptHash:           strings.Repeat("a", 64),
			CompletionTokens:     intPtr(2),
			Timeline:             whatttft.Timeline{BaseWallUnixNano: 1000, EventsNS: map[whatttft.EventName]int64{whatttft.EventBodyEOF: 100000000}},
			Derived: whatttft.DerivedMetrics{
				TTFTDeltaMS:              &ttftA,
				E2EDeltaMS:               &e2eA,
				E2EOutputTPS:             &e2eTPS,
				GenerationDeltaOutputTPS: &genTPS,
			},
		},
		{
			RequestID:            "target-b-req-000000",
			TargetID:             "target-b",
			Provider:             "openai",
			Model:                "gpt-b",
			ScenarioName:         "short",
			CacheMode:            whatttft.CacheBust,
			ConnectionMode:       whatttft.WarmConnections,
			RequestedServiceTier: "priority",
			ObservedServiceTier:  "priority",
			PromptHash:           strings.Repeat("b", 64),
			CompletionTokens:     intPtr(2),
			Timeline:             whatttft.Timeline{BaseWallUnixNano: 2000, EventsNS: map[whatttft.EventName]int64{whatttft.EventBodyEOF: 90000000}},
			Derived: whatttft.DerivedMetrics{
				TTFTDeltaMS:              &ttftB,
				E2EDeltaMS:               &e2eB,
				E2EOutputTPS:             &e2eTPS,
				GenerationDeltaOutputTPS: &genTPS,
			},
		},
	}

	return &whatttft.RunResult{Records: records, Summary: whatttft.Summarize(records)}
}

func sampleRunResult() *whatttft.RunResult {
	ttft := 42.0
	record := whatttft.RequestRecord{
		RequestID:      "req-000001",
		Provider:       "openai",
		Model:          "gpt-test",
		ScenarioName:   "short",
		CacheMode:      whatttft.CacheBust,
		ConnectionMode: whatttft.WarmConnections,
		PromptHash:     strings.Repeat("a", 64),
		Timeline: whatttft.Timeline{EventsNS: map[whatttft.EventName]int64{
			whatttft.EventRequestStart:     0,
			whatttft.EventFirstOutputDelta: 42000000,
		}},
		Derived: whatttft.DerivedMetrics{TTFTDeltaMS: &ttft},
	}
	chunk := whatttft.ChunkRecord{RequestID: "req-000001", Index: 0, Content: "hello"}

	return &whatttft.RunResult{
		Records: []whatttft.RequestRecord{record},
		Chunks:  []whatttft.ChunkRecord{chunk},
		Summary: whatttft.Summarize([]whatttft.RequestRecord{record}),
	}
}

func intPtr(value int) *int {
	return &value
}

func readJSONFile(t *testing.T, path string, value any) {
	t.Helper()

	//nolint:gosec // Tests read paths created under t.TempDir or fixed report output filenames.
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", filepath.Base(path), err)
	}
	if err := json.Unmarshal(data, value); err != nil {
		t.Fatalf("unmarshal %s: %v", filepath.Base(path), err)
	}
}

func readJSONLFile[T any](t *testing.T, path string) []T {
	t.Helper()

	//nolint:gosec // Tests read paths created under t.TempDir or fixed report output filenames.
	file, err := os.Open(path)
	if err != nil {
		t.Fatalf("open %s: %v", filepath.Base(path), err)
	}
	defer closeTestFile(t, file)

	var values []T
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		var value T
		if err := json.Unmarshal(scanner.Bytes(), &value); err != nil {
			t.Fatalf("unmarshal JSONL line: %v", err)
		}
		values = append(values, value)
	}
	if err := scanner.Err(); err != nil {
		t.Fatalf("scan JSONL: %v", err)
	}

	return values
}

func closeTestFile(t *testing.T, file *os.File) {
	t.Helper()

	if err := file.Close(); err != nil {
		t.Fatalf("close file: %v", err)
	}
}
