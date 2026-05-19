package configfile

import (
	"strings"
	"testing"
	"time"

	"github.com/gabrielmbmb/what-ttft/pkg/provider/openai"
	"github.com/gabrielmbmb/what-ttft/pkg/whatttft"
)

// TestLoadValidMinimalAppliesDefaults verifies omitted YAML fields normalize to v0.2 defaults.
func TestLoadValidMinimalAppliesDefaults(t *testing.T) {
	cfg, err := LoadFile("testdata/valid_minimal.yaml")
	if err != nil {
		t.Fatalf("load valid minimal: %v", err)
	}
	if cfg.SchemaVersion != 1 {
		t.Fatalf("schema version = %d, want 1", cfg.SchemaVersion)
	}
	if cfg.Name != "minimal-benchmark" {
		t.Fatalf("name = %q, want minimal-benchmark", cfg.Name)
	}
	if cfg.Run.Samples != 1 || cfg.Run.Warmup != 0 {
		t.Fatalf("run samples/warmup = %d/%d, want 1/0", cfg.Run.Samples, cfg.Run.Warmup)
	}
	if cfg.Run.Concurrency != 1 {
		t.Fatalf("concurrency = %d, want 1 default", cfg.Run.Concurrency)
	}
	if cfg.Run.CacheMode != whatttft.CacheBust {
		t.Fatalf("cache mode = %q, want cache-bust default", cfg.Run.CacheMode)
	}
	if cfg.Run.ConnectionMode != whatttft.WarmConnections {
		t.Fatalf("connection mode = %q, want warm default", cfg.Run.ConnectionMode)
	}
	if cfg.Run.Timeout != 120*time.Second {
		t.Fatalf("timeout = %s, want 120s default", cfg.Run.Timeout)
	}
	if cfg.Run.SaveChunks {
		t.Fatal("save chunks = true, want false default")
	}
	if cfg.Scenario.Prompt == "" {
		t.Fatal("scenario prompt should be populated")
	}
	if cfg.Scenario.MaxOutputTokens != 16 {
		t.Fatalf("max output tokens = %d, want 16", cfg.Scenario.MaxOutputTokens)
	}
	if cfg.Scenario.Temperature == nil || *cfg.Scenario.Temperature != 0 {
		t.Fatalf("temperature = %v, want explicit zero pointer", cfg.Scenario.Temperature)
	}
	if cfg.Scenario.TopP != nil {
		t.Fatalf("top_p = %v, want nil when omitted", cfg.Scenario.TopP)
	}

	if len(cfg.Targets) != 1 {
		t.Fatalf("targets = %d, want 1", len(cfg.Targets))
	}
	target := cfg.Targets[0]
	if target.ID != "openai-gpt-5.5-001" {
		t.Fatalf("generated target ID = %q, want openai-gpt-5.5-001", target.ID)
	}
	if target.Provider != "openai" {
		t.Fatalf("provider = %q, want openai", target.Provider)
	}
	if target.OpenAI.API != openai.ResponsesAPI {
		t.Fatalf("api = %q, want responses default", target.OpenAI.API)
	}
	if target.OpenAI.BaseURL != openai.DefaultBaseURL {
		t.Fatalf("base URL = %q, want default %q", target.OpenAI.BaseURL, openai.DefaultBaseURL)
	}
	if target.OpenAI.APIKeyEnv != "OPENAI_API_KEY" {
		t.Fatalf("api key env = %q, want OPENAI_API_KEY", target.OpenAI.APIKeyEnv)
	}
	if target.OpenAI.Model != "gpt-5.5" {
		t.Fatalf("model = %q, want gpt-5.5", target.OpenAI.Model)
	}
	if target.OpenAI.ServiceTier != "" {
		t.Fatalf("service tier = %q, want empty", target.OpenAI.ServiceTier)
	}
	if !target.OpenAI.IncludeUsage {
		t.Fatal("include usage = false, want true default")
	}
	if target.OpenAI.LegacyMaxTokens {
		t.Fatal("legacy max tokens = true, want false default")
	}
}

// TestLoadValidTwoModelsInheritsAndOverrides verifies defaults are inherited and target overrides are applied.
func TestLoadValidTwoModelsInheritsAndOverrides(t *testing.T) {
	cfg, err := LoadFile("testdata/valid_two_models.yaml")
	if err != nil {
		t.Fatalf("load valid two models: %v", err)
	}
	if cfg.Run.Samples != 50 || cfg.Run.Warmup != 5 || cfg.Run.Timeout != 120*time.Second {
		t.Fatalf("run settings = samples %d warmup %d timeout %s", cfg.Run.Samples, cfg.Run.Warmup, cfg.Run.Timeout)
	}
	if cfg.Scenario.Name != "short-capital" || cfg.Scenario.SystemPrompt == "" || cfg.Scenario.ReasoningEffort != "none" {
		t.Fatalf("scenario was not normalized as expected: %#v", cfg.Scenario)
	}
	if cfg.Scenario.TopP == nil || *cfg.Scenario.TopP != 1 {
		t.Fatalf("top_p = %v, want explicit 1", cfg.Scenario.TopP)
	}

	if len(cfg.Targets) != 2 {
		t.Fatalf("targets = %d, want 2", len(cfg.Targets))
	}
	first := cfg.Targets[0]
	if first.ID != "gpt-5.5" || first.Name != "GPT 5.5" {
		t.Fatalf("first target id/name = %q/%q", first.ID, first.Name)
	}
	if first.OpenAI.ServiceTier != openai.ServiceTierDefault {
		t.Fatalf("first service tier = %q, want default", first.OpenAI.ServiceTier)
	}
	if first.OpenAI.API != openai.ResponsesAPI {
		t.Fatalf("first API = %q, want responses", first.OpenAI.API)
	}
	if first.OpenAI.Model != "gpt-5.5" {
		t.Fatalf("first model = %q, want gpt-5.5", first.OpenAI.Model)
	}

	second := cfg.Targets[1]
	if second.ID != "gpt-5.2" || second.Name != "GPT 5.2 priority" {
		t.Fatalf("second target id/name = %q/%q", second.ID, second.Name)
	}
	if second.OpenAI.ServiceTier != openai.ServiceTierPriority {
		t.Fatalf("second service tier = %q, want priority override", second.OpenAI.ServiceTier)
	}
	if second.OpenAI.BaseURL != first.OpenAI.BaseURL {
		t.Fatalf("second base URL = %q, want inherited %q", second.OpenAI.BaseURL, first.OpenAI.BaseURL)
	}
	redacted := second.OpenAI.RedactedBaseURL()
	if strings.Contains(redacted, "user:") || strings.Contains(redacted, "api_key=secret") {
		t.Fatalf("redacted base URL still contains credentials: %q", redacted)
	}
	if !strings.Contains(redacted, "region=test") {
		t.Fatalf("redacted base URL should preserve non-secret query: %q", redacted)
	}
}

// TestRunConfigForTargetPopulatesTargetIdentity verifies normalized configs can build single-target runner configs.
func TestRunConfigForTargetPopulatesTargetIdentity(t *testing.T) {
	cfg, err := LoadFile("testdata/valid_two_models.yaml")
	if err != nil {
		t.Fatalf("load config: %v", err)
	}

	runConfig := cfg.RunConfigForTarget(cfg.Targets[1])
	if runConfig.TargetID != "gpt-5.2" || runConfig.TargetName != "GPT 5.2 priority" {
		t.Fatalf("target identity = %q/%q", runConfig.TargetID, runConfig.TargetName)
	}
	if runConfig.RequestIDPrefix != "gpt-5.2" {
		t.Fatalf("request ID prefix = %q, want target ID", runConfig.RequestIDPrefix)
	}
	if runConfig.MeasuredRequests != 50 || runConfig.WarmupRequests != 5 {
		t.Fatalf("request counts = measured %d warmup %d", runConfig.MeasuredRequests, runConfig.WarmupRequests)
	}
	if runConfig.Scenario.Name != "short-capital" {
		t.Fatalf("scenario name = %q, want short-capital", runConfig.Scenario.Name)
	}
}

// TestLoadRejectsInvalidFixtures verifies strict decoding and validation failures are actionable.
func TestLoadRejectsInvalidFixtures(t *testing.T) {
	tests := []struct {
		name    string
		path    string
		wantErr string
	}{
		{name: "unknown field", path: "testdata/invalid_unknown_field.yaml", wantErr: "field unexpected not found"},
		{name: "duplicate target ID", path: "testdata/invalid_duplicate_target_id.yaml", wantErr: "duplicates targets[0].id"},
		{name: "missing prompt", path: "testdata/invalid_missing_prompt.yaml", wantErr: "scenario.prompt is required"},
		{name: "bad duration", path: "testdata/invalid_bad_duration.yaml", wantErr: "duration must be a Go duration string"},
		{name: "bad service tier", path: "testdata/invalid_bad_service_tier.yaml", wantErr: "targets[0].service_tier"},
		{name: "inline API key", path: "testdata/invalid_inline_api_key.yaml", wantErr: "defaults.api_key is not supported"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, err := LoadFile(test.path)
			if err == nil {
				t.Fatal("expected error")
			}
			if !strings.Contains(err.Error(), test.wantErr) {
				t.Fatalf("error = %q, want substring %q", err.Error(), test.wantErr)
			}
		})
	}
}

// TestLoadReportsTargetRequiredFieldsWithPaths verifies validation points to inherited target fields.
func TestLoadReportsTargetRequiredFieldsWithPaths(t *testing.T) {
	_, err := Load(strings.NewReader(`
schema_version: 1
run:
  samples: 1
scenario:
  prompt: hello
  max_output_tokens: 16
targets:
  - id: missing-required
`))
	if err == nil {
		t.Fatal("expected required field errors")
	}
	message := err.Error()
	for _, want := range []string{"targets[0].api_key_env is required", "targets[0].model is required"} {
		if !strings.Contains(message, want) {
			t.Fatalf("error = %q, want substring %q", message, want)
		}
	}
}

// TestLoadPreservesExplicitZeroOptionalFloats verifies pointer-backed floats distinguish omitted and explicit zero.
func TestLoadPreservesExplicitZeroOptionalFloats(t *testing.T) {
	cfg, err := Load(strings.NewReader(`
schema_version: 1
defaults:
  api_key_env: OPENAI_API_KEY
run:
  samples: 1
scenario:
  prompt: hello
  max_output_tokens: 16
  temperature: 0
  top_p: 0
targets:
  - model: gpt-5.5
`))
	if err != nil {
		t.Fatalf("load config: %v", err)
	}
	if cfg.Scenario.Temperature == nil || *cfg.Scenario.Temperature != 0 {
		t.Fatalf("temperature = %v, want explicit zero", cfg.Scenario.Temperature)
	}
	if cfg.Scenario.TopP == nil || *cfg.Scenario.TopP != 0 {
		t.Fatalf("top_p = %v, want explicit zero", cfg.Scenario.TopP)
	}
}

// TestLoadGeneratesCollisionFreeIDs verifies omitted target IDs remain deterministic and unique.
func TestLoadGeneratesCollisionFreeIDs(t *testing.T) {
	cfg, err := Load(strings.NewReader(`
schema_version: 1
defaults:
  api_key_env: OPENAI_API_KEY
run:
  samples: 1
scenario:
  prompt: hello
  max_output_tokens: 16
targets:
  - model: gpt-same
  - model: gpt-same
`))
	if err != nil {
		t.Fatalf("load config: %v", err)
	}
	if len(cfg.Targets) != 2 {
		t.Fatalf("targets = %d, want 2", len(cfg.Targets))
	}
	if cfg.Targets[0].ID == cfg.Targets[1].ID {
		t.Fatalf("generated IDs collided: %q", cfg.Targets[0].ID)
	}
	if cfg.Targets[0].ID != "openai-gpt-same-001" || cfg.Targets[1].ID != "openai-gpt-same-002" {
		t.Fatalf("generated IDs = %q/%q", cfg.Targets[0].ID, cfg.Targets[1].ID)
	}
}

// TestLoadAllowsChatCompletionsCompatibility verifies chat-completions targets are accepted explicitly.
func TestLoadAllowsChatCompletionsCompatibility(t *testing.T) {
	cfg, err := Load(strings.NewReader(`
schema_version: 1
defaults:
  api_key_env: OPENAI_API_KEY
  api: chat-completions
  include_usage: false
  legacy_max_tokens: true
run:
  warmup: 1
scenario:
  prompt: hello
targets:
  - model: compat-model
`))
	if err != nil {
		t.Fatalf("load chat completions config: %v", err)
	}
	target := cfg.Targets[0]
	if target.OpenAI.API != openai.ChatCompletionsAPI {
		t.Fatalf("api = %q, want chat-completions", target.OpenAI.API)
	}
	if target.OpenAI.IncludeUsage {
		t.Fatal("include usage = true, want explicit false")
	}
	if !target.OpenAI.LegacyMaxTokens {
		t.Fatal("legacy max tokens = false, want explicit true")
	}
}

// TestSHA256Hex verifies config byte digests are stable for report metadata.
func TestSHA256Hex(t *testing.T) {
	got := SHA256Hex([]byte("benchmark config\n"))
	want := "1ead915672d9fa4328972f77bd629db00a8df7e5e1d8e2513daf16e015e35d3b"
	if got != want {
		t.Fatalf("sha256 = %q, want %q", got, want)
	}
}
