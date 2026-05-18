package openai

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/gabrielmbmb/what-ttft/pkg/whatttft"
)

// TestIntegrationOpenAIStreaming performs an opt-in smoke test against the real OpenAI Chat Completions API.
func TestIntegrationOpenAIStreaming(t *testing.T) {
	if os.Getenv("WHAT_TTFT_INTEGRATION") != "1" {
		t.Skip("set WHAT_TTFT_INTEGRATION=1 to run real OpenAI integration test")
	}
	apiKey := os.Getenv("OPENAI_API_KEY")
	if apiKey == "" {
		t.Skip("set OPENAI_API_KEY to run real OpenAI integration test")
	}

	model := os.Getenv("WHAT_TTFT_OPENAI_MODEL")
	if model == "" {
		model = "gpt-5.5"
	}
	baseURL := os.Getenv("WHAT_TTFT_OPENAI_BASE_URL")
	if baseURL == "" {
		baseURL = defaultBaseURL
	}

	provider := New(Config{
		BaseURL:      baseURL,
		APIKey:       apiKey,
		Model:        model,
		IncludeUsage: true,
	})
	runner := whatttft.NewRunner(provider, whatttft.RunConfig{
		Scenario: whatttft.Scenario{
			Name:            "integration-smoke",
			Prompt:          "Answer in one short sentence: what is the capital of France?",
			MaxOutputTokens: 16,
			ReasoningEffort: "none",
		},
		MeasuredRequests: 1,
		CacheMode:        whatttft.CacheBust,
		ConnectionMode:   whatttft.WarmConnections,
	})

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	result, err := runner.Run(ctx)
	if err != nil {
		t.Fatalf("integration run failed: %v", err)
	}
	if len(result.Records) != 1 {
		t.Fatalf("record count = %d, want 1", len(result.Records))
	}

	record := result.Records[0]
	if record.Error != nil {
		t.Fatalf("request error: category=%s status=%d message=%s", record.Error.Category, record.Error.StatusCode, record.Error.Message)
	}
	if _, ok := record.Timeline.EventsNS[whatttft.EventFirstOutputDelta]; !ok {
		t.Fatalf("first_output_delta missing from timeline: %#v", record.Timeline.EventsNS)
	}
	if _, ok := record.Timeline.EventsNS[whatttft.EventBodyEOF]; !ok {
		t.Fatalf("body_eof missing from timeline: %#v", record.Timeline.EventsNS)
	}
	if record.PromptTokens != nil && *record.PromptTokens < 0 {
		t.Fatalf("prompt tokens = %d, want non-negative", *record.PromptTokens)
	}
	if record.CompletionTokens != nil && *record.CompletionTokens < 0 {
		t.Fatalf("completion tokens = %d, want non-negative", *record.CompletionTokens)
	}
	if record.TotalTokens != nil && *record.TotalTokens < 0 {
		t.Fatalf("total tokens = %d, want non-negative", *record.TotalTokens)
	}
}
