package cerebras

import "testing"

// TestConfigBaseURLDefaultsToCerebras verifies the default base URL and trailing-slash trimming.
func TestConfigBaseURLDefaultsToCerebras(t *testing.T) {
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

	const keyEnv = "CEREBRAS_TEST_KEY"
	t.Setenv(keyEnv, "from-env")
	if got := (Config{APIKeyEnv: keyEnv}).apiKey(); got != "from-env" {
		t.Fatalf("env api key = %q, want from-env", got)
	}
	if got := (Config{}).apiKey(); got != "" {
		t.Fatalf("empty api key = %q, want empty", got)
	}
}

// TestValidServiceTier verifies accepted and rejected Cerebras service tiers.
func TestValidServiceTier(t *testing.T) {
	for _, tier := range []ServiceTier{"", ServiceTierAuto, ServiceTierDefault, ServiceTierFlex, ServiceTierPriority} {
		if !ValidServiceTier(tier) {
			t.Fatalf("service tier %q should be valid", tier)
		}
	}
	for _, tier := range []ServiceTier{"scale", "turbo"} {
		if ValidServiceTier(tier) {
			t.Fatalf("service tier %q should be invalid", tier)
		}
	}
}
