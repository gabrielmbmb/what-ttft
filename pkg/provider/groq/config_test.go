package groq

import "testing"

// TestConfigDefaults verifies base URL, API default, and endpoint construction.
func TestConfigDefaults(t *testing.T) {
	if got := (Config{}).baseURL(); got != DefaultBaseURL {
		t.Fatalf("default base URL = %q, want %q", got, DefaultBaseURL)
	}
	if got := (Config{BaseURL: "https://example.test/openai/v1/"}).baseURL(); got != "https://example.test/openai/v1" {
		t.Fatalf("trimmed base URL = %q, want https://example.test/openai/v1", got)
	}
	if got := (Config{}).api(); got != ChatCompletionsAPI {
		t.Fatalf("default api = %q, want chat-completions", got)
	}
	if got := (Config{}).chatCompletionsEndpointURL(); got != DefaultBaseURL+"/chat/completions" {
		t.Fatalf("chat endpoint = %q", got)
	}
	if got := (Config{}).responsesEndpointURL(); got != DefaultBaseURL+"/responses" {
		t.Fatalf("responses endpoint = %q", got)
	}
}

// TestConfigAPIKeyPrefersInlineThenEnv verifies API key resolution order.
func TestConfigAPIKeyPrefersInlineThenEnv(t *testing.T) {
	if got := (Config{APIKey: "inline"}).apiKey(); got != "inline" {
		t.Fatalf("inline api key = %q, want inline", got)
	}

	const keyEnv = "GROQ_TEST_KEY"
	t.Setenv(keyEnv, "from-env")
	if got := (Config{APIKeyEnv: keyEnv}).apiKey(); got != "from-env" {
		t.Fatalf("env api key = %q, want from-env", got)
	}
	if got := (Config{}).apiKey(); got != "" {
		t.Fatalf("empty api key = %q, want empty", got)
	}
}

// TestValidAPIAndServiceTier verifies accepted and rejected API surfaces and service tiers.
func TestValidAPIAndServiceTier(t *testing.T) {
	for _, api := range []API{"", ChatCompletionsAPI, ResponsesAPI} {
		if !ValidAPI(api) {
			t.Fatalf("api %q should be valid", api)
		}
	}
	if ValidAPI("completions") {
		t.Fatal("api 'completions' should be invalid")
	}
	for _, tier := range []ServiceTier{"", ServiceTierOnDemand, ServiceTierFlex, ServiceTierAuto, ServiceTierPerformance} {
		if !ValidServiceTier(tier) {
			t.Fatalf("service tier %q should be valid", tier)
		}
	}
	if ValidServiceTier("priority") {
		t.Fatal("service tier 'priority' should be invalid for groq")
	}
}
