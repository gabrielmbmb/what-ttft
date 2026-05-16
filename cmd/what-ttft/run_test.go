package main

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/gabrielmbmb/what-ttft/internal/report"
	"github.com/gabrielmbmb/what-ttft/pkg/whatttft"
)

// TestRunCommandHelp verifies run-specific help lists required flags.
func TestRunCommandHelp(t *testing.T) {
	var stdout bytes.Buffer
	var stderr bytes.Buffer

	exitCode := runCLI([]string{"run", "--help"}, &stdout, &stderr)
	if exitCode != 0 {
		t.Fatalf("exit code = %d, want 0", exitCode)
	}
	if stdout.Len() != 0 {
		t.Fatalf("stdout = %q, want empty", stdout.String())
	}
	if !strings.Contains(stderr.String(), "--provider openai") || !strings.Contains(stderr.String(), "--model MODEL") {
		t.Fatalf("stderr missing run help:\n%s", stderr.String())
	}
}

// TestRunCommandAgainstFakeOpenAIServer verifies the CLI writes reports against an httptest OpenAI-compatible stream.
func TestRunCommandAgainstFakeOpenAIServer(t *testing.T) {
	const placeholderAPIKey = "cli-test-key"

	var sawAuthorization atomic.Bool
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/chat/completions" {
			t.Errorf("path = %q, want /chat/completions", r.URL.Path)
			http.NotFound(w, r)
			return
		}
		if r.Method != http.MethodPost {
			t.Errorf("method = %q, want POST", r.Method)
			http.Error(w, "bad method", http.StatusMethodNotAllowed)
			return
		}
		if got := r.Header.Get("Authorization"); got == "Bearer "+placeholderAPIKey {
			sawAuthorization.Store(true)
		} else {
			t.Errorf("authorization header = %q", got)
		}

		var body struct {
			Model  string `json:"model"`
			Stream bool   `json:"stream"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Errorf("decode request body: %v", err)
			http.Error(w, "bad json", http.StatusBadRequest)
			return
		}
		if body.Model != "gpt-test" {
			t.Errorf("model = %q, want gpt-test", body.Model)
		}
		if !body.Stream {
			t.Error("stream should be true")
		}

		w.Header().Set("Content-Type", "text/event-stream")
		writeCLISSE(t, w, `{"choices":[{"delta":{"role":"assistant"}}]}`)
		writeCLISSE(t, w, `{"choices":[{"delta":{"content":"Hello"}}]}`)
		writeCLISSE(t, w, `{"choices":[{"delta":{"content":" world"},"finish_reason":"stop"}]}`)
		writeCLISSE(t, w, `{"choices":[],"usage":{"prompt_tokens":3,"completion_tokens":2,"total_tokens":5,"prompt_tokens_details":{"cached_tokens":0}}}`)
		writeCLISSE(t, w, "[DONE]")
	}))
	defer server.Close()

	outputDir := filepath.Join(t.TempDir(), "reports")
	var stdout bytes.Buffer
	var stderr bytes.Buffer

	exitCode := runCLI([]string{
		"run",
		"--provider", "openai",
		"--base-url", server.URL,
		"--api-key", placeholderAPIKey,
		"--model", "gpt-test",
		"--prompt", "Say hello.",
		"--samples", "1",
		"--warmup", "0",
		"--concurrency", "1",
		"--cache-mode", "cache-reuse",
		"--connection-mode", "warm",
		"--max-output-tokens", "8",
		"--temperature", "0",
		"--top-p", "1",
		"--timeout", "10s",
		"--out", outputDir,
		"--save-chunks=true",
	}, &stdout, &stderr)
	if exitCode != 0 {
		t.Fatalf("exit code = %d, want 0\nstdout:\n%s\nstderr:\n%s", exitCode, stdout.String(), stderr.String())
	}
	if !sawAuthorization.Load() {
		t.Fatal("authorization header was not observed")
	}
	if strings.Contains(stdout.String(), placeholderAPIKey) || strings.Contains(stderr.String(), placeholderAPIKey) {
		t.Fatalf("API key leaked in CLI output\nstdout:%s\nstderr:%s", stdout.String(), stderr.String())
	}
	if !strings.Contains(stdout.String(), "ttft_delta_ms") {
		t.Fatalf("stdout missing concise metric summary:\n%s", stdout.String())
	}

	for _, name := range []string{"run.json", "requests.jsonl", "chunks.jsonl", "summary.json", "summary.md"} {
		if _, err := os.Stat(filepath.Join(outputDir, name)); err != nil {
			t.Fatalf("stat %s: %v", name, err)
		}
	}

	var metadata report.RunMetadata
	readCLIJSONFile(t, filepath.Join(outputDir, "run.json"), &metadata)
	if metadata.Args == nil {
		t.Fatal("run metadata args should be recorded")
	}
	for _, arg := range metadata.Args {
		if strings.Contains(arg, placeholderAPIKey) {
			t.Fatalf("API key leaked in metadata args: %#v", metadata.Args)
		}
	}

	var summary whatttft.RunSummary
	readCLIJSONFile(t, filepath.Join(outputDir, "summary.json"), &summary)
	if summary.MeasuredRequests != 1 || summary.SuccessfulRequests != 1 {
		t.Fatalf("summary counts = measured %d successful %d", summary.MeasuredRequests, summary.SuccessfulRequests)
	}
	if len(summary.Groups) != 1 {
		t.Fatalf("group count = %d, want 1", len(summary.Groups))
	}
	if summary.Groups[0].TotalCompletionTokens != 2 {
		t.Fatalf("total completion tokens = %d, want 2", summary.Groups[0].TotalCompletionTokens)
	}
}

// TestRunCommandRejectsInvalidProvider verifies validation fails before running.
func TestRunCommandRejectsInvalidProvider(t *testing.T) {
	var stdout bytes.Buffer
	var stderr bytes.Buffer

	exitCode := runCLI([]string{"run", "--provider", "other", "--model", "m", "--prompt", "p", "--out", t.TempDir()}, &stdout, &stderr)
	if exitCode != 2 {
		t.Fatalf("exit code = %d, want 2", exitCode)
	}
	if !strings.Contains(stderr.String(), `unsupported provider "other"`) {
		t.Fatalf("stderr missing unsupported provider:\n%s", stderr.String())
	}
}

func writeCLISSE(t *testing.T, w http.ResponseWriter, data string) {
	t.Helper()

	if _, err := w.Write([]byte("data: " + data + "\n\n")); err != nil {
		t.Errorf("write SSE: %v", err)
	}
	if flusher, ok := w.(http.Flusher); ok {
		flusher.Flush()
	}
}

func readCLIJSONFile(t *testing.T, path string, value any) {
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
