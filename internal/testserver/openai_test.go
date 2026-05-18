package testserver

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/gabrielmbmb/what-ttft/internal/httptracecap"
	"github.com/gabrielmbmb/what-ttft/internal/report"
	"github.com/gabrielmbmb/what-ttft/pkg/provider/openai"
	"github.com/gabrielmbmb/what-ttft/pkg/whatttft"
)

// TestOpenAIServerAcceptsV1PathAndRecordsRequests verifies request validation and capture for /v1 base URLs.
func TestOpenAIServerAcceptsV1PathAndRecordsRequests(t *testing.T) {
	server := NewOpenAIServer(OpenAIConfig{
		OpenAIProcessingDuration: 123 * time.Millisecond,
		Steps:                    []StreamStep{{Data: "[DONE]"}},
	})
	defer server.Close()

	body := stringsReader(`{"model":"gpt-test","stream":true}`)
	req, err := http.NewRequestWithContext(context.Background(), http.MethodPost, server.URL()+"/v1/chat/completions", body)
	if err != nil {
		t.Fatalf("create request: %v", err)
	}
	req.Header.Set("Authorization", "Bearer placeholder")
	req.Header.Set("Content-Type", "application/json")

	//nolint:gosec // Tests send requests only to httptest.Server URLs.
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("perform request: %v", err)
	}
	defer closeResponseBody(t, resp)

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	if got := resp.Header.Get(openAIProcessingMSHeader); got != "123" {
		t.Fatalf("%s = %q, want 123", openAIProcessingMSHeader, got)
	}
	requests := server.Requests()
	if len(requests) != 1 {
		t.Fatalf("request count = %d, want 1", len(requests))
	}
	if requests[0].Path != "/v1/chat/completions" {
		t.Fatalf("path = %q, want /v1/chat/completions", requests[0].Path)
	}
	if requests[0].Model != "gpt-test" {
		t.Fatalf("model = %q, want gpt-test", requests[0].Model)
	}
	if !requests[0].Stream {
		t.Fatal("stream should be true")
	}
	if !requests[0].AuthorizationPresent {
		t.Fatal("authorization header should be recorded as present")
	}
}

// TestOpenAIServerRejectsMissingStream verifies stream=true validation catches invalid benchmark requests.
func TestOpenAIServerRejectsMissingStream(t *testing.T) {
	server := NewOpenAIServer(OpenAIConfig{})
	defer server.Close()

	body := stringsReader(`{"model":"gpt-test","stream":false}`)
	req, err := http.NewRequestWithContext(context.Background(), http.MethodPost, server.URL()+"/chat/completions", body)
	if err != nil {
		t.Fatalf("create request: %v", err)
	}
	req.Header.Set("Authorization", "Bearer placeholder")

	//nolint:gosec // Tests send requests only to httptest.Server URLs.
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("perform request: %v", err)
	}
	defer closeResponseBody(t, resp)

	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", resp.StatusCode)
	}
}

// TestOpenAIServerEndToEndRunnerAndReports verifies the full provider-runner-report path against the fake stream.
func TestOpenAIServerEndToEndRunnerAndReports(t *testing.T) {
	server := NewOpenAIServer(OpenAIConfig{
		DelayBeforeHeaders:      17 * time.Millisecond,
		DelayBeforeFirstEvent:   2 * time.Millisecond,
		DelayBetweenSteps:       2 * time.Millisecond,
		DelayBeforeFirstContent: 5 * time.Millisecond,
		Steps: []StreamStep{
			{Comment: "heartbeat"},
			{Data: ""},
			{Data: `{"choices":[{"index":0,"delta":{"role":"assistant"}}]}`},
			{Data: `{"choices":[{"index":0,"delta":{"content":"Hello"}}]}`},
			{Data: `{"choices":[{"index":0,"delta":{"content":" world"},"finish_reason":"stop"}]}`},
			{Data: `{"choices":[],"usage":{"prompt_tokens":10,"completion_tokens":2,"total_tokens":12,"prompt_tokens_details":{"cached_tokens":3}}}`},
			{Data: "[DONE]"},
		},
	})
	defer server.Close()

	client := httptracecap.NewHTTPClient(httptracecap.TransportConfig{Timeout: 5 * time.Second})
	provider := openai.New(openai.Config{
		BaseURL:      server.URL(),
		APIKey:       "placeholder",
		Model:        "gpt-test",
		IncludeUsage: true,
		HTTPClient:   client,
	})
	runner := whatttft.NewRunner(provider, whatttft.RunConfig{
		Scenario: whatttft.Scenario{
			Name:            "fake-openai",
			Prompt:          "Say hello.",
			MaxOutputTokens: 8,
		},
		MeasuredRequests: 1,
		CacheMode:        whatttft.CacheReuse,
		ConnectionMode:   whatttft.WarmConnections,
		SaveChunks:       true,
	})

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	result, err := runner.Run(ctx)
	if err != nil {
		t.Fatalf("runner run: %v", err)
	}

	assertEndToEndRecord(t, result)
	assertEndToEndReports(t, result, server.URL())
}

func assertEndToEndRecord(t *testing.T, result *whatttft.RunResult) {
	t.Helper()

	if result.Summary.MeasuredRequests != 1 || result.Summary.SuccessfulRequests != 1 {
		t.Fatalf("summary counts = measured %d successful %d", result.Summary.MeasuredRequests, result.Summary.SuccessfulRequests)
	}
	if len(result.Records) != 1 {
		t.Fatalf("record count = %d, want 1", len(result.Records))
	}
	record := result.Records[0]
	if record.Error != nil {
		t.Fatalf("record error = %#v, want nil", record.Error)
	}
	if record.Derived.FirstEventMS == nil {
		t.Fatal("first_event_ms should be populated")
	}
	if record.Derived.TTFTDeltaMS == nil {
		t.Fatal("ttft_delta_ms should be populated")
	}
	if *record.Derived.TTFTDeltaMS <= *record.Derived.FirstEventMS {
		t.Fatalf("TTFT %.3f ms should be after first SSE event %.3f ms", *record.Derived.TTFTDeltaMS, *record.Derived.FirstEventMS)
	}
	if record.CompletionTokens == nil || *record.CompletionTokens != 2 {
		t.Fatalf("completion tokens = %v, want 2", record.CompletionTokens)
	}
	if record.PromptTokens == nil || *record.PromptTokens != 10 {
		t.Fatalf("prompt tokens = %v, want 10", record.PromptTokens)
	}
	if record.TotalTokens == nil || *record.TotalTokens != 12 {
		t.Fatalf("total tokens = %v, want 12", record.TotalTokens)
	}
	if record.Cache.PromptCachedTokens == nil || *record.Cache.PromptCachedTokens != 3 {
		t.Fatalf("cached prompt tokens = %v, want 3", record.Cache.PromptCachedTokens)
	}
	if record.Cache.Hit == nil || !*record.Cache.Hit {
		t.Fatalf("cache hit = %v, want pointer to true", record.Cache.Hit)
	}
	if record.HTTP.ProviderProcessingMS == nil || *record.HTTP.ProviderProcessingMS != 17 {
		t.Fatalf("provider processing ms = %v, want 17", record.HTTP.ProviderProcessingMS)
	}
	if _, ok := record.Timeline.EventsNS[whatttft.EventDone]; !ok {
		t.Fatal("done_event should be recorded")
	}
	if _, ok := record.Timeline.EventsNS[whatttft.EventBodyEOF]; !ok {
		t.Fatal("body_eof should be recorded")
	}
	if len(result.Chunks) < 4 {
		t.Fatalf("chunk count = %d, want role/content/usage chunks", len(result.Chunks))
	}
}

func assertEndToEndReports(t *testing.T, result *whatttft.RunResult, baseURL string) {
	t.Helper()

	outputDir := filepath.Join(t.TempDir(), "reports")
	err := report.WriteRun(report.WriteOptions{
		OutputDir:  outputDir,
		SaveChunks: true,
		Run: report.RunMetadata{
			Provider: result.Records[0].Provider,
			Model:    result.Records[0].Model,
			BaseURL:  baseURL,
		},
		Result: result,
	})
	if err != nil {
		t.Fatalf("write reports: %v", err)
	}

	for _, name := range []string{"run.json", "requests.jsonl", "chunks.jsonl", "summary.json", "summary.md"} {
		if _, statErr := os.Stat(filepath.Join(outputDir, name)); statErr != nil {
			t.Fatalf("stat report %s: %v", name, statErr)
		}
	}

	var summary whatttft.RunSummary
	//nolint:gosec // Tests read a fixed report filename under t.TempDir.
	data, err := os.ReadFile(filepath.Join(outputDir, "summary.json"))
	if err != nil {
		t.Fatalf("read summary.json: %v", err)
	}
	if err := json.Unmarshal(data, &summary); err != nil {
		t.Fatalf("unmarshal summary: %v", err)
	}
	if summary.SuccessfulRequests != 1 {
		t.Fatalf("summary successful = %d, want 1", summary.SuccessfulRequests)
	}
}

func closeResponseBody(t *testing.T, resp *http.Response) {
	t.Helper()

	if err := resp.Body.Close(); err != nil {
		t.Fatalf("close response body: %v", err)
	}
}

func stringsReader(value string) io.Reader {
	return strings.NewReader(value)
}
