package huggingface

import "testing"

// TestConfigBaseURLDefaultsToRouter verifies the default base URL and trailing-slash trimming.
func TestConfigBaseURLDefaultsToRouter(t *testing.T) {
	if got := (Config{}).baseURL(); got != DefaultBaseURL {
		t.Fatalf("default base URL = %q, want %q", got, DefaultBaseURL)
	}
	if got := (Config{BaseURL: "https://example.test/v1/"}).baseURL(); got != "https://example.test/v1" {
		t.Fatalf("trimmed base URL = %q, want https://example.test/v1", got)
	}
	if got := (Config{}).chatCompletionsEndpointURL(); got != DefaultBaseURL+"/chat/completions" {
		t.Fatalf("endpoint = %q, want %s/chat/completions", got, DefaultBaseURL)
	}
}

// TestConfigAPIKeyPrefersInlineThenEnv verifies API key resolution order.
func TestConfigAPIKeyPrefersInlineThenEnv(t *testing.T) {
	if got := (Config{APIKey: "inline"}).apiKey(); got != "inline" {
		t.Fatalf("inline api key = %q, want inline", got)
	}

	const keyEnv = "HF_TEST_TOKEN"
	t.Setenv(keyEnv, "from-env")
	if got := (Config{APIKeyEnv: keyEnv}).apiKey(); got != "from-env" {
		t.Fatalf("env api key = %q, want from-env", got)
	}
	if got := (Config{}).apiKey(); got != "" {
		t.Fatalf("empty api key = %q, want empty", got)
	}
}
