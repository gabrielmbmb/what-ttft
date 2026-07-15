package main

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/gabrielmbmb/what-ttft/internal/report"
	"github.com/gabrielmbmb/what-ttft/internal/testserver"
	"github.com/gabrielmbmb/what-ttft/pkg/whatttft"
)

// TestBenchCommandHelp verifies bench-specific help lists YAML benchmark flags.
func TestBenchCommandHelp(t *testing.T) {
	var stdout bytes.Buffer
	var stderr bytes.Buffer

	exitCode := runCLI([]string{"bench", "--help"}, &stdout, &stderr)
	if exitCode != 0 {
		t.Fatalf("exit code = %d, want 0", exitCode)
	}
	if stdout.Len() != 0 {
		t.Fatalf("stdout = %q, want empty", stdout.String())
	}
	if !strings.Contains(stderr.String(), "--config PATH") || !strings.Contains(stderr.String(), "--dry-run") || !strings.Contains(stderr.String(), "--service-tier") || !strings.Contains(stderr.String(), "--tui") {
		t.Fatalf("stderr missing bench help:\n%s", stderr.String())
	}
}

// TestBenchCommandRequiresConfig verifies --config is mandatory.
func TestBenchCommandRequiresConfig(t *testing.T) {
	var stdout bytes.Buffer
	var stderr bytes.Buffer

	exitCode := runCLI([]string{"bench"}, &stdout, &stderr)
	if exitCode != 2 {
		t.Fatalf("exit code = %d, want 2", exitCode)
	}
	if !strings.Contains(stderr.String(), "--config is required") {
		t.Fatalf("stderr missing config error:\n%s", stderr.String())
	}
	if stdout.Len() != 0 {
		t.Fatalf("stdout = %q, want empty", stdout.String())
	}
}

// TestBenchCommandTUILauncherIsInjectable verifies --tui routes through the launcher seam without sending requests.
func TestBenchCommandTUILauncherIsInjectable(t *testing.T) {
	oldLauncher := benchTUILauncher
	defer func() { benchTUILauncher = oldLauncher }()

	var invoked bool
	benchTUILauncher = func(_ context.Context, request benchTUILaunchRequest) int {
		invoked = true
		if !request.Config.tui {
			t.Error("launcher config tui = false, want true")
		}
		if request.Plan == nil || request.Plan.Name != "bench-test" {
			t.Fatalf("launcher plan name = %#v, want bench-test", request.Plan)
		}
		if request.Execute == nil {
			t.Error("launcher Execute callback is nil")
		}
		return 124
	}

	configPath := writeBenchConfig(t, benchYAML("http://127.0.0.1:1", "WHAT_TTFT_BENCH_KEY", "default"))
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	exitCode := runCLI([]string{
		"bench",
		"--config", configPath,
		"--out", filepath.Join(t.TempDir(), "reports"),
		"--tui",
	}, &stdout, &stderr)
	if exitCode != 124 {
		t.Fatalf("exit code = %d, want launcher code 124", exitCode)
	}
	if !invoked {
		t.Fatal("launcher was not invoked")
	}
}

// TestBenchCommandTUIEmitsPreflightFailure verifies missing credentials reach the dashboard event stream.
func TestBenchCommandTUIEmitsPreflightFailure(t *testing.T) {
	oldLauncher := benchTUILauncher
	defer func() { benchTUILauncher = oldLauncher }()

	t.Setenv("WHAT_TTFT_BENCH_KEY", "")
	configPath := writeBenchConfig(t, benchYAML("http://127.0.0.1:1", "WHAT_TTFT_BENCH_KEY", "default"))
	var failure *whatttft.RunEvent
	benchTUILauncher = func(ctx context.Context, request benchTUILaunchRequest) int {
		execution := request.Execute(ctx, whatttft.RunObserverFunc(func(_ context.Context, event whatttft.RunEvent) {
			if event.Kind == whatttft.EventBenchmarkFailed {
				cloned := event.Clone()
				failure = &cloned
			}
		}))
		if execution.Err == nil {
			t.Fatal("benchmark execution succeeded without API key")
		}
		return 1
	}

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	exitCode := runCLI([]string{"bench", "--config", configPath, "--out", filepath.Join(t.TempDir(), "reports"), "--tui"}, &stdout, &stderr)
	if exitCode != 1 {
		t.Fatalf("exit code = %d, want 1", exitCode)
	}
	if failure == nil || failure.Error == nil {
		t.Fatal("benchmark_failed event was not emitted")
	}
	if failure.Error.Category != "validation" || !strings.Contains(failure.Error.Message, "WHAT_TTFT_BENCH_KEY is empty") {
		t.Fatalf("benchmark_failed error = %#v", failure.Error)
	}
	if failure.BenchmarkName != "bench-test" || len(failure.Targets) != 2 {
		t.Fatalf("benchmark_failed context = name %q targets %#v", failure.BenchmarkName, failure.Targets)
	}
}

// TestBenchCommandTUIPathExecutesBenchmarkAndWritesReports verifies the --tui path uses the shared execution callback and report writer.
func TestBenchCommandTUIPathExecutesBenchmarkAndWritesReports(t *testing.T) {
	//nolint:gosec // Test uses a non-secret placeholder to verify redaction.
	const placeholderAPIKey = "bench-cli-tui-key"
	oldLauncher := benchTUILauncher
	defer func() { benchTUILauncher = oldLauncher }()

	t.Setenv("WHAT_TTFT_BENCH_KEY", placeholderAPIKey)
	server := testserver.NewOpenAIServer(testserver.OpenAIConfig{Steps: benchResponseSteps()})
	defer server.Close()
	configPath := writeBenchConfig(t, benchYAML(server.URL(), "WHAT_TTFT_BENCH_KEY", "default"))
	outputDir := filepath.Join(t.TempDir(), "reports")

	var observedEvents []whatttft.RunEvent
	benchTUILauncher = func(ctx context.Context, request benchTUILaunchRequest) int {
		execution := request.Execute(ctx, whatttft.RunObserverFunc(func(_ context.Context, event whatttft.RunEvent) {
			observedEvents = append(observedEvents, event)
		}))
		return finishBenchCommand(request.Stdout, request.Stderr, request.Plan, execution)
	}

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	exitCode := runCLI([]string{"bench", "--config", configPath, "--out", outputDir, "--tui"}, &stdout, &stderr)
	if exitCode != 0 {
		t.Fatalf("exit code = %d, want 0\nstdout:\n%s\nstderr:\n%s", exitCode, stdout.String(), stderr.String())
	}
	if !containsRunEventKind(observedEvents, whatttft.EventBenchmarkStarted) || !containsRunEventKind(observedEvents, whatttft.EventTargetStarted) || !containsRunEventKind(observedEvents, whatttft.EventReportWriteFinished) {
		t.Fatalf("bench TUI events missing from %#v", runEventKinds(observedEvents))
	}
	started := firstRunEventKind(observedEvents, whatttft.EventBenchmarkStarted)
	if started == nil || len(started.Targets) != 2 || started.Targets[0].TargetID != "target-a" || started.Targets[1].TargetID != "target-b" {
		t.Fatalf("benchmark_started targets = %#v, want target-a,target-b", started)
	}
	if started.Targets[0].ProviderAPI != "responses" || started.Targets[0].RequestedServiceTier != "default" {
		t.Fatalf("benchmark_started target metadata = %#v, want responses/default", started.Targets[0])
	}
	if !strings.Contains(stdout.String(), "wrote results to "+outputDir) {
		t.Fatalf("stdout missing final output dir:\n%s", stdout.String())
	}
	if strings.Contains(stdout.String(), placeholderAPIKey) || strings.Contains(stderr.String(), placeholderAPIKey) {
		t.Fatalf("API key leaked in bench TUI output\nstdout:%s\nstderr:%s", stdout.String(), stderr.String())
	}
	for _, name := range []string{"run.json", "requests.jsonl", "summary.json", "summary.md"} {
		if _, err := os.Stat(filepath.Join(outputDir, name)); err != nil {
			t.Fatalf("stat bench TUI %s: %v", name, err)
		}
	}
}

// TestBenchCommandTUIPathCancellationWritesPartialReports verifies TUI-triggered cancellation writes partial combined reports.
func TestBenchCommandTUIPathCancellationWritesPartialReports(t *testing.T) {
	//nolint:gosec // Test uses a non-secret placeholder to verify redaction.
	const placeholderAPIKey = "bench-cli-tui-cancel-key"
	oldLauncher := benchTUILauncher
	defer func() { benchTUILauncher = oldLauncher }()

	t.Setenv("WHAT_TTFT_BENCH_KEY", placeholderAPIKey)
	server := testserver.NewOpenAIServer(testserver.OpenAIConfig{Steps: benchResponseSteps()})
	defer server.Close()
	configPath := writeBenchConfig(t, strings.Replace(benchYAML(server.URL(), "WHAT_TTFT_BENCH_KEY", "default"), "samples: 1", "samples: 2", 1))
	outputDir := filepath.Join(t.TempDir(), "reports")

	benchTUILauncher = func(ctx context.Context, request benchTUILaunchRequest) int {
		benchCtx, cancel := context.WithCancel(ctx)
		defer cancel()
		execution := request.Execute(benchCtx, whatttft.RunObserverFunc(func(_ context.Context, event whatttft.RunEvent) {
			if event.Kind == whatttft.EventRequestFinished && event.TargetID == "target-b" {
				cancel()
			}
		}))
		return finishBenchCommand(request.Stdout, request.Stderr, request.Plan, execution)
	}

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	exitCode := runCLI([]string{"bench", "--config", configPath, "--out", outputDir, "--tui"}, &stdout, &stderr)
	if exitCode != interruptedExitCode {
		t.Fatalf("exit code = %d, want %d\nstdout:\n%s\nstderr:\n%s", exitCode, interruptedExitCode, stdout.String(), stderr.String())
	}
	if !strings.Contains(stdout.String(), "benchmark canceled; wrote partial results to "+outputDir) {
		t.Fatalf("stdout missing partial report message:\n%s", stdout.String())
	}
	var summary whatttft.RunSummary
	readCLIJSONFile(t, filepath.Join(outputDir, "summary.json"), &summary)
	if summary.SuccessfulRequests == 0 || summary.SuccessfulRequests >= 4 {
		t.Fatalf("partial summary successful requests = %d, want between 1 and 3", summary.SuccessfulRequests)
	}
	if strings.Contains(stdout.String(), placeholderAPIKey) || strings.Contains(stderr.String(), placeholderAPIKey) {
		t.Fatalf("API key leaked in canceled bench TUI output\nstdout:%s\nstderr:%s", stdout.String(), stderr.String())
	}
}

// TestBenchCommandRejectsInvalidYAML verifies config parse and validation errors use usage exit code 2.
func TestBenchCommandRejectsInvalidYAML(t *testing.T) {
	configPath := writeBenchConfig(t, `
schema_version: 1
unexpected: true
run:
  samples: 1
scenario:
  prompt: hello
targets:
  - model: gpt-test
    api_key_env: WHAT_TTFT_BENCH_KEY
`)

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	exitCode := runCLI([]string{"bench", "--config", configPath}, &stdout, &stderr)
	if exitCode != 2 {
		t.Fatalf("exit code = %d, want 2", exitCode)
	}
	if !strings.Contains(stderr.String(), "config:") || !strings.Contains(stderr.String(), "field unexpected not found") {
		t.Fatalf("stderr missing YAML validation error:\n%s", stderr.String())
	}
	if stdout.Len() != 0 {
		t.Fatalf("stdout = %q, want empty", stdout.String())
	}
}

// TestBenchCommandDryRunSendsNoRequests verifies dry-run validates and prints a plan without touching the server or output directory.
func TestBenchCommandDryRunSendsNoRequests(t *testing.T) {
	//nolint:gosec // Test uses a non-secret placeholder to verify redaction.
	const placeholderAPIKey = "bench-dry-run-key"
	t.Setenv("WHAT_TTFT_BENCH_KEY", placeholderAPIKey)

	server := testserver.NewOpenAIServer(testserver.OpenAIConfig{Steps: benchResponseSteps()})
	defer server.Close()
	configPath := writeBenchConfig(t, benchYAML(server.URL(), "WHAT_TTFT_BENCH_KEY", "default"))
	outputDir := filepath.Join(t.TempDir(), "dry-run-output")

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	exitCode := runCLI([]string{"bench", "--config", configPath, "--out", outputDir, "--dry-run", "--samples", "2"}, &stdout, &stderr)
	if exitCode != 0 {
		t.Fatalf("exit code = %d, want 0\nstdout:\n%s\nstderr:\n%s", exitCode, stdout.String(), stderr.String())
	}
	if requests := server.Requests(); len(requests) != 0 {
		t.Fatalf("server requests = %d, want 0", len(requests))
	}
	if _, err := os.Stat(outputDir); !os.IsNotExist(err) {
		t.Fatalf("dry-run output dir stat error = %v, want not exist", err)
	}
	if !strings.Contains(stdout.String(), "dry run: no requests sent") || !strings.Contains(stdout.String(), "target-a") || !strings.Contains(stdout.String(), "samples=2") {
		t.Fatalf("stdout missing dry-run plan:\n%s", stdout.String())
	}
	if strings.Contains(stdout.String(), placeholderAPIKey) || strings.Contains(stderr.String(), placeholderAPIKey) {
		t.Fatalf("API key leaked in dry-run output\nstdout:%s\nstderr:%s", stdout.String(), stderr.String())
	}
}

// TestBenchCommandAgainstFakeOpenAIServer verifies two YAML targets run through Responses by default and write combined reports.
func TestBenchCommandAgainstFakeOpenAIServer(t *testing.T) {
	//nolint:gosec // Test uses a non-secret placeholder to verify redaction.
	const placeholderAPIKey = "bench-cli-test-key"
	t.Setenv("WHAT_TTFT_BENCH_KEY", placeholderAPIKey)

	server := testserver.NewOpenAIServer(testserver.OpenAIConfig{Steps: benchResponseSteps()})
	defer server.Close()
	configPath := writeBenchConfig(t, benchYAML(server.URL(), "WHAT_TTFT_BENCH_KEY", "default"))
	outputDir := filepath.Join(t.TempDir(), "reports")

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	exitCode := runCLI([]string{
		"bench",
		"--config", configPath,
		"--out", outputDir,
		"--save-chunks",
	}, &stdout, &stderr)
	if exitCode != 0 {
		t.Fatalf("exit code = %d, want 0\nstdout:\n%s\nstderr:\n%s", exitCode, stdout.String(), stderr.String())
	}
	if strings.Contains(stdout.String(), placeholderAPIKey) || strings.Contains(stderr.String(), placeholderAPIKey) {
		t.Fatalf("API key leaked in CLI output\nstdout:%s\nstderr:%s", stdout.String(), stderr.String())
	}
	if !strings.Contains(stdout.String(), "target-a") || !strings.Contains(stdout.String(), "target-b") {
		t.Fatalf("stdout missing target comparison rows:\n%s", stdout.String())
	}
	if !strings.Contains(stdout.String(), "completion_tokens_total") || !strings.Contains(stdout.String(), "completion_token_records") || !strings.Contains(stdout.String(), "e2e_output_tps_mean") || !strings.Contains(stdout.String(), "generation_delta_output_tps_mean") || !strings.Contains(stdout.String(), "generation_delta_output_tps_count") {
		t.Fatalf("stdout missing explicit token/TPS comparison columns:\n%s", stdout.String())
	}

	requests := server.Requests()
	if len(requests) != 2 {
		t.Fatalf("server requests = %d, want 2", len(requests))
	}
	models := make([]string, 0, len(requests))
	for _, request := range requests {
		if request.Path != "/responses" {
			t.Fatalf("request path = %q, want /responses", request.Path)
		}
		if !request.AuthorizationPresent {
			t.Fatal("authorization header was not present")
		}
		if !request.Stream {
			t.Fatal("stream should be true")
		}
		if request.ServiceTier != "default" {
			t.Fatalf("service tier = %q, want default", request.ServiceTier)
		}
		models = append(models, request.Model)
	}
	sort.Strings(models)
	if strings.Join(models, ",") != "gpt-a,gpt-b" {
		t.Fatalf("models = %#v, want gpt-a,gpt-b", models)
	}

	for _, name := range []string{"run.json", "requests.jsonl", "chunks.jsonl", "summary.json", "summary.md"} {
		if _, err := os.Stat(filepath.Join(outputDir, name)); err != nil {
			t.Fatalf("stat %s: %v", name, err)
		}
	}

	var metadata report.RunMetadata
	readCLIJSONFile(t, filepath.Join(outputDir, "run.json"), &metadata)
	if metadata.BenchmarkName != "bench-test" {
		t.Fatalf("benchmark name = %q, want bench-test", metadata.BenchmarkName)
	}
	if metadata.ConfigPath != configPath {
		t.Fatalf("config path = %q, want %q", metadata.ConfigPath, configPath)
	}
	if metadata.ConfigSHA256 == "" {
		t.Fatal("config SHA-256 should be recorded")
	}
	if metadata.TargetOrder != "serial" {
		t.Fatalf("target order = %q, want serial", metadata.TargetOrder)
	}
	if metadata.Provider != "openai" {
		t.Fatalf("metadata provider = %q, want openai", metadata.Provider)
	}
	if metadata.ProviderAPI != "responses" {
		t.Fatalf("provider API = %q, want responses", metadata.ProviderAPI)
	}
	if metadata.RequestedServiceTier != "default" {
		t.Fatalf("requested service tier = %q, want default", metadata.RequestedServiceTier)
	}
	if metadata.RunConfig.MeasuredRequests != 1 || !metadata.RunConfig.SaveChunks {
		t.Fatalf("run config measured/save_chunks = %d/%t, want 1/true", metadata.RunConfig.MeasuredRequests, metadata.RunConfig.SaveChunks)
	}
	if len(metadata.Targets) != 2 {
		t.Fatalf("metadata targets = %d, want 2", len(metadata.Targets))
	}
	if metadata.Targets[0].TargetID != "target-a" || metadata.Targets[0].ProviderAPI != "responses" || metadata.Targets[0].APIKeyEnv != "WHAT_TTFT_BENCH_KEY" {
		t.Fatalf("first target metadata unexpected: %#v", metadata.Targets[0])
	}
	if metadata.Targets[0].ObservedServiceTier != "default" || metadata.Targets[0].ObservedServiceTierCounts["default"] != 1 {
		t.Fatalf("first target observed tier = %q counts %#v, want default", metadata.Targets[0].ObservedServiceTier, metadata.Targets[0].ObservedServiceTierCounts)
	}

	var summary whatttft.RunSummary
	readCLIJSONFile(t, filepath.Join(outputDir, "summary.json"), &summary)
	if summary.MeasuredRequests != 2 || summary.SuccessfulRequests != 2 || summary.ErrorRequests != 0 {
		t.Fatalf("summary counts = measured %d successful %d errors %d", summary.MeasuredRequests, summary.SuccessfulRequests, summary.ErrorRequests)
	}
	if len(summary.Groups) != 2 {
		t.Fatalf("summary groups = %d, want 2", len(summary.Groups))
	}
	groupsByTarget := make(map[string]whatttft.SummaryGroup, len(summary.Groups))
	for _, group := range summary.Groups {
		groupsByTarget[group.TargetID] = group
	}
	for _, targetID := range []string{"target-a", "target-b"} {
		group, ok := groupsByTarget[targetID]
		if !ok {
			t.Fatalf("missing summary group for %s", targetID)
		}
		if group.TotalCompletionTokens != 4 {
			t.Fatalf("group %s completion tokens = %d, want 4", targetID, group.TotalCompletionTokens)
		}
		if group.RequestedServiceTier != "default" {
			t.Fatalf("group %s requested tier = %q, want default", targetID, group.RequestedServiceTier)
		}
		if group.Metrics.E2EOutputTPS.Count != 1 {
			t.Fatalf("group %s e2e TPS count = %d, want 1", targetID, group.Metrics.E2EOutputTPS.Count)
		}
	}

	for _, name := range []string{"run.json", "requests.jsonl", "chunks.jsonl", "summary.json", "summary.md"} {
		//nolint:gosec // Tests read fixed report filenames under t.TempDir.
		data, err := os.ReadFile(filepath.Join(outputDir, name))
		if err != nil {
			t.Fatalf("read %s: %v", name, err)
		}
		if bytes.Contains(data, []byte(placeholderAPIKey)) {
			t.Fatalf("API key leaked in %s", name)
		}
	}
}

// TestBenchCommandServiceTierOverride verifies the CLI override applies to every OpenAI target.
func TestBenchCommandServiceTierOverride(t *testing.T) {
	//nolint:gosec // Test uses a non-secret placeholder to verify redaction.
	const placeholderAPIKey = "bench-cli-override-key"
	t.Setenv("WHAT_TTFT_BENCH_KEY", placeholderAPIKey)
	server := testserver.NewOpenAIServer(testserver.OpenAIConfig{Steps: benchResponseSteps()})
	defer server.Close()
	configPath := writeBenchConfig(t, benchYAML(server.URL(), "WHAT_TTFT_BENCH_KEY", "default"))

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	exitCode := runCLI([]string{
		"bench",
		"--config", configPath,
		"--out", filepath.Join(t.TempDir(), "reports"),
		"--service-tier", "priority",
	}, &stdout, &stderr)
	if exitCode != 0 {
		t.Fatalf("exit code = %d, want 0\nstdout:\n%s\nstderr:\n%s", exitCode, stdout.String(), stderr.String())
	}
	for _, request := range server.Requests() {
		if request.ServiceTier != "priority" {
			t.Fatalf("service tier = %q, want priority override", request.ServiceTier)
		}
	}
}

func firstRunEventKind(events []whatttft.RunEvent, kind whatttft.RunEventKind) *whatttft.RunEvent {
	for index := range events {
		if events[index].Kind == kind {
			return &events[index]
		}
	}
	return nil
}

func benchYAML(baseURL string, apiKeyEnv string, serviceTier string) string {
	return `schema_version: 1
name: bench-test

defaults:
  provider: openai
  api: responses
  base_url: ` + baseURL + `
  api_key_env: ` + apiKeyEnv + `
  service_tier: ` + serviceTier + `

run:
  samples: 1
  warmup: 0
  concurrency: 1
  cache_mode: cache-reuse
  connection_mode: warm
  timeout: 10s
  save_chunks: false

scenario:
  name: bench-short
  prompt: Say hello.
  max_output_tokens: 16
  reasoning_effort: none

targets:
  - id: target-a
    name: Target A
    model: gpt-a
  - id: target-b
    name: Target B
    model: gpt-b
`
}

// TestBenchCommandCerebrasTarget verifies a cerebras target runs through the bench pipeline,
// hits /chat/completions, records provider=cerebras, and captures time_info server timing.
func TestBenchCommandCerebrasTarget(t *testing.T) {
	//nolint:gosec // Test uses a non-secret placeholder to verify redaction.
	const placeholderAPIKey = "bench-cerebras-key"
	t.Setenv("WHAT_TTFT_CEREBRAS_KEY", placeholderAPIKey)

	var sawChatPath atomic.Bool
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/chat/completions" {
			sawChatPath.Store(true)
		}
		w.Header().Set("Content-Type", "text/event-stream")
		flusher, ok := w.(http.Flusher)
		if !ok {
			t.Fatal("response writer is not a flusher")
		}
		writeChunk := func(data string) {
			if _, err := w.Write([]byte("data: " + data + "\n\n")); err != nil {
				t.Errorf("write chunk: %v", err)
			}
			flusher.Flush()
		}
		writeChunk(`{"choices":[{"index":0,"delta":{"content":"Hello"},"finish_reason":"stop"}]}`)
		writeChunk(`{"choices":[],"usage":{"prompt_tokens":10,"completion_tokens":2,"total_tokens":12,"prompt_tokens_details":{"cached_tokens":0}},"time_info":{"queue_time":0.001,"prompt_time":0.006,"completion_time":0.014,"total_time":0.09}}`)
		writeChunk("[DONE]")
	}))
	defer server.Close()

	configPath := writeBenchConfig(t, cerebrasBenchYAML(server.URL, "WHAT_TTFT_CEREBRAS_KEY"))
	outputDir := filepath.Join(t.TempDir(), "reports")

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	exitCode := runCLI([]string{"bench", "--config", configPath, "--out", outputDir}, &stdout, &stderr)
	if exitCode != 0 {
		t.Fatalf("exit code = %d, want 0\nstdout:\n%s\nstderr:\n%s", exitCode, stdout.String(), stderr.String())
	}
	if !sawChatPath.Load() {
		t.Fatal("cerebras bench did not hit /chat/completions")
	}
	if strings.Contains(stdout.String(), placeholderAPIKey) || strings.Contains(stderr.String(), placeholderAPIKey) {
		t.Fatalf("API key leaked\nstdout:%s\nstderr:%s", stdout.String(), stderr.String())
	}
	if !strings.Contains(stdout.String(), "provider") || !strings.Contains(stdout.String(), "cerebras") {
		t.Fatalf("summary missing provider column with cerebras value:\n%s", stdout.String())
	}

	var metadata report.RunMetadata
	readCLIJSONFile(t, filepath.Join(outputDir, "run.json"), &metadata)
	if metadata.Provider != "cerebras" {
		t.Fatalf("metadata provider = %q, want cerebras", metadata.Provider)
	}
	if metadata.ProviderAPI != "chat-completions" {
		t.Fatalf("provider API = %q, want chat-completions", metadata.ProviderAPI)
	}

	//nolint:gosec // Test-controlled output directory path.
	requests, err := os.ReadFile(filepath.Join(outputDir, "requests.jsonl"))
	if err != nil {
		t.Fatalf("read requests.jsonl: %v", err)
	}
	if !strings.Contains(string(requests), "server_timing") || !strings.Contains(string(requests), "completion_time_ms") {
		t.Fatalf("requests.jsonl missing captured server timing:\n%s", requests)
	}
}

// TestBenchCommandTogetherTarget verifies a together target runs through the bench pipeline,
// hits /chat/completions, records provider=together, and keeps reasoning deltas out of TTFT.
func TestBenchCommandTogetherTarget(t *testing.T) {
	const placeholderAPIKey = "bench-together-key"
	t.Setenv("WHAT_TTFT_TOGETHER_KEY", placeholderAPIKey)

	var sawChatPath atomic.Bool
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/chat/completions" {
			sawChatPath.Store(true)
		}
		w.Header().Set("Content-Type", "text/event-stream")
		flusher, ok := w.(http.Flusher)
		if !ok {
			t.Fatal("response writer is not a flusher")
		}
		writeChunk := func(data string) {
			if _, err := w.Write([]byte("data: " + data + "\n\n")); err != nil {
				t.Errorf("write chunk: %v", err)
			}
			flusher.Flush()
		}
		writeChunk(`{"choices":[{"index":0,"delta":{"reasoning":"thinking"}}]}`)
		writeChunk(`{"choices":[{"index":0,"delta":{"content":"Hello"},"finish_reason":"stop"}]}`)
		writeChunk(`{"choices":[],"usage":{"prompt_tokens":10,"completion_tokens":2,"total_tokens":12}}`)
		writeChunk("[DONE]")
	}))
	defer server.Close()

	configPath := writeBenchConfig(t, togetherBenchYAML(server.URL, "WHAT_TTFT_TOGETHER_KEY"))
	outputDir := filepath.Join(t.TempDir(), "reports")

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	exitCode := runCLI([]string{"bench", "--config", configPath, "--out", outputDir}, &stdout, &stderr)
	if exitCode != 0 {
		t.Fatalf("exit code = %d, want 0\nstdout:\n%s\nstderr:\n%s", exitCode, stdout.String(), stderr.String())
	}
	if !sawChatPath.Load() {
		t.Fatal("together bench did not hit /chat/completions")
	}
	if strings.Contains(stdout.String(), placeholderAPIKey) || strings.Contains(stderr.String(), placeholderAPIKey) {
		t.Fatalf("API key leaked\nstdout:%s\nstderr:%s", stdout.String(), stderr.String())
	}
	if !strings.Contains(stdout.String(), "together") {
		t.Fatalf("summary missing together provider column:\n%s", stdout.String())
	}

	var metadata report.RunMetadata
	readCLIJSONFile(t, filepath.Join(outputDir, "run.json"), &metadata)
	if metadata.Provider != "together" {
		t.Fatalf("metadata provider = %q, want together", metadata.Provider)
	}
	if metadata.ProviderAPI != "chat-completions" {
		t.Fatalf("provider API = %q, want chat-completions", metadata.ProviderAPI)
	}
}

func togetherBenchYAML(baseURL string, apiKeyEnv string) string {
	return `schema_version: 1
name: together-bench-test

defaults:
  provider: together
  base_url: ` + baseURL + `
  api_key_env: ` + apiKeyEnv + `

run:
  samples: 1
  warmup: 0
  concurrency: 1
  cache_mode: cache-reuse
  connection_mode: warm
  timeout: 10s
  save_chunks: false

scenario:
  name: together-short
  prompt: Say hello.
  max_output_tokens: 16

targets:
  - id: together-llama
    name: Together Llama
    model: meta-llama/Llama-3.3-70B-Instruct-Turbo
`
}

func cerebrasBenchYAML(baseURL string, apiKeyEnv string) string {
	return `schema_version: 1
name: cerebras-bench-test

defaults:
  provider: cerebras
  base_url: ` + baseURL + `
  api_key_env: ` + apiKeyEnv + `

run:
  samples: 1
  warmup: 0
  concurrency: 1
  cache_mode: cache-reuse
  connection_mode: warm
  timeout: 10s
  save_chunks: false

scenario:
  name: cerebras-short
  prompt: Say hello.
  max_output_tokens: 16

targets:
  - id: cerebras-oss
    name: Cerebras GPT-OSS
    model: gpt-oss-120b
`
}

func benchResponseSteps() []testserver.StreamStep {
	return []testserver.StreamStep{
		{Data: `{"type":"response.created","response":{"status":"in_progress","service_tier":"default"}}`},
		{Data: `{"type":"response.output_text.delta","delta":"Hello"}`},
		{Data: `{"type":"response.output_text.delta","delta":" world"}`},
		{Data: `{"type":"response.completed","response":{"status":"completed","service_tier":"default","usage":{"input_tokens":3,"input_tokens_details":{"cached_tokens":0},"output_tokens":4,"output_tokens_details":{"reasoning_tokens":0},"total_tokens":7}}}`},
	}
}

func writeBenchConfig(t *testing.T, content string) string {
	t.Helper()

	path := filepath.Join(t.TempDir(), "benchmark.yaml")
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("write benchmark config: %v", err)
	}

	return path
}

// TestBenchYAMLFixtureIsValidJSONEscapedResponses ensures the test SSE payloads remain valid JSON.
func TestBenchYAMLFixtureIsValidJSONEscapedResponses(t *testing.T) {
	for index, step := range benchResponseSteps() {
		var decoded map[string]any
		if err := json.Unmarshal([]byte(step.Data), &decoded); err != nil {
			t.Fatalf("step %d JSON invalid: %v", index, err)
		}
	}
}
