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
	if !strings.Contains(stderr.String(), "--provider openai") || !strings.Contains(stderr.String(), "--model MODEL") || !strings.Contains(stderr.String(), "--openai-api") {
		t.Fatalf("stderr missing run help:\n%s", stderr.String())
	}
}

// TestRunCommandAgainstFakeOpenAIServer verifies the CLI writes reports against an httptest OpenAI Responses-compatible stream.
func TestRunCommandAgainstFakeOpenAIServer(t *testing.T) {
	const placeholderAPIKey = "cli-test-key"

	var sawAuthorization atomic.Bool
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/responses" {
			t.Errorf("path = %q, want /responses", r.URL.Path)
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
			Model       string `json:"model"`
			Stream      bool   `json:"stream"`
			ServiceTier string `json:"service_tier"`
			Reasoning   struct {
				Effort string `json:"effort"`
			} `json:"reasoning"`
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
		if body.Reasoning.Effort != "none" {
			t.Errorf("reasoning effort = %q, want none", body.Reasoning.Effort)
		}
		if body.ServiceTier != "default" {
			t.Errorf("service_tier = %q, want default", body.ServiceTier)
		}

		w.Header().Set("Content-Type", "text/event-stream")
		writeCLISSEEvent(t, w, "response.created", `{"type":"response.created","response":{"status":"in_progress","service_tier":"default"}}`)
		writeCLISSEEvent(t, w, "response.output_text.delta", `{"type":"response.output_text.delta","delta":"Hello"}`)
		writeCLISSEEvent(t, w, "response.output_text.delta", `{"type":"response.output_text.delta","delta":" world"}`)
		writeCLISSEEvent(t, w, "response.completed", `{"type":"response.completed","response":{"status":"completed","service_tier":"default","usage":{"input_tokens":3,"input_tokens_details":{"cached_tokens":0},"output_tokens":2,"output_tokens_details":{"reasoning_tokens":0},"total_tokens":5}}}`)
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
		"--reasoning-effort", "none",
		"--service-tier", "default",
		"--max-output-tokens", "16",
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
	if !strings.Contains(stdout.String(), "successful=1 errors=0") {
		t.Fatalf("stdout missing success/error counts:\n%s", stdout.String())
	}
	if !strings.Contains(stdout.String(), "service_tier=default") {
		t.Fatalf("stdout missing service tier:\n%s", stdout.String())
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
	if metadata.ProviderAPI != "responses" {
		t.Fatalf("provider API = %q, want responses", metadata.ProviderAPI)
	}
	if metadata.RequestedServiceTier != "default" {
		t.Fatalf("metadata service tier = %q, want default", metadata.RequestedServiceTier)
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
	if summary.Groups[0].RequestedServiceTier != "default" {
		t.Fatalf("requested service tier = %q, want default", summary.Groups[0].RequestedServiceTier)
	}
	if summary.Groups[0].ObservedServiceTierCounts["default"] != 1 {
		t.Fatalf("observed service tiers = %#v, want default count 1", summary.Groups[0].ObservedServiceTierCounts)
	}
}

// TestRunCommandCanUseChatCompletionsCompatibility verifies the explicit compatibility API posts to /chat/completions.
func TestRunCommandCanUseChatCompletionsCompatibility(t *testing.T) {
	const placeholderAPIKey = "cli-test-key"

	var sawChatPath atomic.Bool
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/chat/completions" {
			t.Errorf("path = %q, want /chat/completions", r.URL.Path)
			http.NotFound(w, r)
			return
		}
		sawChatPath.Store(true)
		var body struct {
			ServiceTier string `json:"service_tier"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Errorf("decode request body: %v", err)
		}
		if body.ServiceTier != "flex" {
			t.Errorf("service_tier = %q, want flex", body.ServiceTier)
		}
		w.Header().Set("Content-Type", "text/event-stream")
		writeCLISSEData(t, w, `{"service_tier":"flex","choices":[{"delta":{"content":"Hello"},"finish_reason":"stop"}]}`)
		writeCLISSEData(t, w, `{"service_tier":"flex","choices":[],"usage":{"prompt_tokens":1,"completion_tokens":1,"total_tokens":2,"prompt_tokens_details":{"cached_tokens":0}}}`)
		writeCLISSEData(t, w, "[DONE]")
	}))
	defer server.Close()

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	exitCode := runCLI([]string{
		"run",
		"--provider", "openai",
		"--openai-api", "chat-completions",
		"--base-url", server.URL,
		"--api-key", placeholderAPIKey,
		"--model", "gpt-test",
		"--prompt", "Say hello.",
		"--samples", "1",
		"--warmup", "0",
		"--service-tier", "flex",
		"--max-output-tokens", "8",
		"--out", filepath.Join(t.TempDir(), "reports"),
	}, &stdout, &stderr)
	if exitCode != 0 {
		t.Fatalf("exit code = %d, want 0\nstdout:\n%s\nstderr:\n%s", exitCode, stdout.String(), stderr.String())
	}
	if !sawChatPath.Load() {
		t.Fatal("chat completions path was not observed")
	}
}

// TestRunCommandGeneratesOutputDir verifies --out is optional and creates a runs/ directory automatically.
func TestRunCommandGeneratesOutputDir(t *testing.T) {
	const placeholderAPIKey = "cli-test-key"

	t.Chdir(t.TempDir())
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Authorization"); got != "Bearer "+placeholderAPIKey {
			t.Errorf("authorization header = %q", got)
		}
		w.Header().Set("Content-Type", "text/event-stream")
		writeCLISSEEvent(t, w, "response.output_text.delta", `{"type":"response.output_text.delta","delta":"Hello"}`)
		writeCLISSEEvent(t, w, "response.completed", `{"type":"response.completed","response":{"status":"completed","usage":{"input_tokens":1,"input_tokens_details":{"cached_tokens":0},"output_tokens":1,"output_tokens_details":{"reasoning_tokens":0},"total_tokens":2}}}`)
	}))
	defer server.Close()

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
		"--max-output-tokens", "16",
	}, &stdout, &stderr)
	if exitCode != 0 {
		t.Fatalf("exit code = %d, want 0\nstdout:\n%s\nstderr:\n%s", exitCode, stdout.String(), stderr.String())
	}
	if !strings.Contains(stdout.String(), "wrote results to runs") {
		t.Fatalf("stdout missing generated output path:\n%s", stdout.String())
	}

	entries, err := os.ReadDir("runs")
	if err != nil {
		t.Fatalf("read generated runs directory: %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("generated run directory count = %d, want 1", len(entries))
	}
	outputDir := filepath.Join("runs", entries[0].Name())
	for _, name := range []string{"run.json", "requests.jsonl", "summary.json", "summary.md"} {
		if _, err := os.Stat(filepath.Join(outputDir, name)); err != nil {
			t.Fatalf("stat generated %s: %v", name, err)
		}
	}

	var metadata report.RunMetadata
	readCLIJSONFile(t, filepath.Join(outputDir, "run.json"), &metadata)
	if metadata.RunConfig.OutputDir != outputDir {
		t.Fatalf("run config output dir = %q, want %q", metadata.RunConfig.OutputDir, outputDir)
	}
}

// TestRunCommandFailsWhenAPIKeyEnvMissing verifies unresolved API key env vars fail before provider requests start.
func TestRunCommandFailsWhenAPIKeyEnvMissing(t *testing.T) {
	t.Setenv("WHAT_TTFT_MISSING_API_KEY", "")
	var requestCount atomic.Int64
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		requestCount.Add(1)
		http.Error(w, "should not be called", http.StatusInternalServerError)
	}))
	defer server.Close()

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	exitCode := runCLI([]string{
		"run",
		"--provider", "openai",
		"--base-url", server.URL,
		"--api-key-env", "WHAT_TTFT_MISSING_API_KEY",
		"--model", "gpt-test",
		"--prompt", "Say hello.",
		"--samples", "1",
		"--warmup", "0",
	}, &stdout, &stderr)
	if exitCode != 1 {
		t.Fatalf("exit code = %d, want 1", exitCode)
	}
	if requestCount.Load() != 0 {
		t.Fatalf("provider request count = %d, want 0", requestCount.Load())
	}
	if !strings.Contains(stderr.String(), "WHAT_TTFT_MISSING_API_KEY is empty") {
		t.Fatalf("stderr missing API key env error:\n%s", stderr.String())
	}
	if stdout.Len() != 0 {
		t.Fatalf("stdout = %q, want empty", stdout.String())
	}
}

// TestRunCommandChecksOutputDirBeforeProviderRun verifies non-empty outputs fail before any provider request starts.
func TestRunCommandChecksOutputDirBeforeProviderRun(t *testing.T) {
	var requestCount atomic.Int64
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		requestCount.Add(1)
		http.Error(w, "should not be called", http.StatusInternalServerError)
	}))
	defer server.Close()

	outputDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(outputDir, "stale.txt"), []byte("stale"), 0o600); err != nil {
		t.Fatalf("write stale file: %v", err)
	}
	t.Setenv("WHAT_TTFT_TEST_API_KEY", "placeholder")

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	exitCode := runCLI([]string{
		"run",
		"--provider", "openai",
		"--base-url", server.URL,
		"--api-key-env", "WHAT_TTFT_TEST_API_KEY",
		"--model", "gpt-test",
		"--prompt", "Say hello.",
		"--samples", "1",
		"--warmup", "0",
		"--out", outputDir,
	}, &stdout, &stderr)
	if exitCode != 1 {
		t.Fatalf("exit code = %d, want 1", exitCode)
	}
	if requestCount.Load() != 0 {
		t.Fatalf("provider request count = %d, want 0", requestCount.Load())
	}
	if !strings.Contains(stderr.String(), "output directory check") {
		t.Fatalf("stderr missing output directory preflight error:\n%s", stderr.String())
	}
	if stdout.Len() != 0 {
		t.Fatalf("stdout = %q, want empty", stdout.String())
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

func writeCLISSEEvent(t *testing.T, w http.ResponseWriter, event string, data string) {
	t.Helper()

	if _, err := w.Write([]byte("event: " + event + "\n")); err != nil {
		t.Errorf("write SSE event: %v", err)
	}
	writeCLISSEData(t, w, data)
}

func writeCLISSEData(t *testing.T, w http.ResponseWriter, data string) {
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
