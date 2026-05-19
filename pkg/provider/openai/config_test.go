package openai

import "testing"

// TestConfigBaseURLDefault verifies an empty base URL uses the OpenAI default.
func TestConfigBaseURLDefault(t *testing.T) {
	cfg := Config{}

	if got := cfg.baseURL(); got != DefaultBaseURL {
		t.Fatalf("baseURL = %q, want %q", got, DefaultBaseURL)
	}
	if got := cfg.api(); got != ResponsesAPI {
		t.Fatalf("api = %q, want responses default", got)
	}
	if got := cfg.responsesEndpointURL(); got != DefaultBaseURL+"/responses" {
		t.Fatalf("responses endpoint = %q, want default responses endpoint", got)
	}
	if got := cfg.chatCompletionsEndpointURL(); got != DefaultBaseURL+"/chat/completions" {
		t.Fatalf("chat endpoint = %q, want default chat completions endpoint", got)
	}
}

// TestConfigEndpointURLTrimsTrailingSlash verifies endpoint construction avoids duplicate slashes.
func TestConfigEndpointURLTrimsTrailingSlash(t *testing.T) {
	cfg := Config{BaseURL: "https://example.test/v1/"}

	if got := cfg.responsesEndpointURL(); got != "https://example.test/v1/responses" {
		t.Fatalf("responses endpoint = %q, want trimmed endpoint", got)
	}
	if got := cfg.chatCompletionsEndpointURL(); got != "https://example.test/v1/chat/completions" {
		t.Fatalf("chat endpoint = %q, want trimmed endpoint", got)
	}
}

// TestConfigAPIKeyPrefersExplicitValue verifies explicit API keys take precedence over environment variables.
func TestConfigAPIKeyPrefersExplicitValue(t *testing.T) {
	t.Setenv("WHAT_TTFT_OPENAI_TEST_KEY", "from-env")
	//nolint:gosec // Tests use non-secret placeholder API key values.
	cfg := Config{APIKey: "explicit", APIKeyEnv: "WHAT_TTFT_OPENAI_TEST_KEY"}

	if got := cfg.apiKey(); got != "explicit" {
		t.Fatalf("apiKey = %q, want explicit", got)
	}
}

// TestConfigAPIKeyUsesEnvironment verifies API key environment lookup works when no explicit key is set.
func TestConfigAPIKeyUsesEnvironment(t *testing.T) {
	t.Setenv("WHAT_TTFT_OPENAI_TEST_KEY", "from-env")
	//nolint:gosec // Tests use non-secret placeholder API key environment variables.
	cfg := Config{APIKeyEnv: "WHAT_TTFT_OPENAI_TEST_KEY"}

	if got := cfg.apiKey(); got != "from-env" {
		t.Fatalf("apiKey = %q, want from-env", got)
	}
}
