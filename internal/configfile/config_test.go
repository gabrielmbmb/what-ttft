package configfile

import (
	"strings"
	"testing"
	"time"

	"github.com/gabrielmbmb/what-ttft/pkg/provider/cerebras"
	"github.com/gabrielmbmb/what-ttft/pkg/provider/openai"
	"github.com/gabrielmbmb/what-ttft/pkg/provider/together"
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
	if target.Settings.API != openai.ResponsesAPI {
		t.Fatalf("api = %q, want responses default", target.Settings.API)
	}
	if target.Settings.BaseURL != openai.DefaultBaseURL {
		t.Fatalf("base URL = %q, want default %q", target.Settings.BaseURL, openai.DefaultBaseURL)
	}
	if target.Settings.APIKeyEnv != "OPENAI_API_KEY" {
		t.Fatalf("api key env = %q, want OPENAI_API_KEY", target.Settings.APIKeyEnv)
	}
	if target.Settings.Model != "gpt-5.5" {
		t.Fatalf("model = %q, want gpt-5.5", target.Settings.Model)
	}
	if target.Settings.ServiceTier != "" {
		t.Fatalf("service tier = %q, want empty", target.Settings.ServiceTier)
	}
	if !target.Settings.IncludeUsage {
		t.Fatal("include usage = false, want true default")
	}
	if target.Settings.LegacyMaxTokens {
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
	if first.Settings.ServiceTier != openai.ServiceTierDefault {
		t.Fatalf("first service tier = %q, want default", first.Settings.ServiceTier)
	}
	if first.Settings.API != openai.ResponsesAPI {
		t.Fatalf("first API = %q, want responses", first.Settings.API)
	}
	if first.Settings.Model != "gpt-5.5" {
		t.Fatalf("first model = %q, want gpt-5.5", first.Settings.Model)
	}

	second := cfg.Targets[1]
	if second.ID != "gpt-5.2" || second.Name != "GPT 5.2 priority" {
		t.Fatalf("second target id/name = %q/%q", second.ID, second.Name)
	}
	if second.Settings.ServiceTier != openai.ServiceTierPriority {
		t.Fatalf("second service tier = %q, want priority override", second.Settings.ServiceTier)
	}
	if second.Settings.BaseURL != first.Settings.BaseURL {
		t.Fatalf("second base URL = %q, want inherited %q", second.Settings.BaseURL, first.Settings.BaseURL)
	}
	redacted := second.Settings.RedactedBaseURL()
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
	if target.Settings.API != openai.ChatCompletionsAPI {
		t.Fatalf("api = %q, want chat-completions", target.Settings.API)
	}
	if target.Settings.IncludeUsage {
		t.Fatal("include usage = true, want explicit false")
	}
	if !target.Settings.LegacyMaxTokens {
		t.Fatal("legacy max tokens = false, want explicit true")
	}
}

// TestLoadAcceptsCerebrasTargetWithProviderDefaults verifies cerebras targets default to the Cerebras base URL and omit the OpenAI API field.
func TestLoadAcceptsCerebrasTargetWithProviderDefaults(t *testing.T) {
	cfg, err := Load(strings.NewReader(`
schema_version: 1
defaults:
  provider: cerebras
  api_key_env: CEREBRAS_API_KEY
run:
  samples: 1
scenario:
  prompt: hello
  max_output_tokens: 16
targets:
  - model: gpt-oss-120b
    service_tier: priority
`))
	if err != nil {
		t.Fatalf("load cerebras config: %v", err)
	}
	target := cfg.Targets[0]
	if target.Provider != "cerebras" {
		t.Fatalf("provider = %q, want cerebras", target.Provider)
	}
	if target.Settings.BaseURL != cerebras.DefaultBaseURL {
		t.Fatalf("base URL = %q, want cerebras default %q", target.Settings.BaseURL, cerebras.DefaultBaseURL)
	}
	if target.Settings.API != "" {
		t.Fatalf("api = %q, want empty for cerebras", target.Settings.API)
	}
	if target.Settings.ServiceTier != openai.ServiceTier(cerebras.ServiceTierPriority) {
		t.Fatalf("service tier = %q, want priority", target.Settings.ServiceTier)
	}
	if target.ID != "cerebras-gpt-oss-120b-001" {
		t.Fatalf("target ID = %q, want cerebras-gpt-oss-120b-001", target.ID)
	}
}

// TestLoadCerebrasRejectsUnsupportedServiceTier verifies Cerebras rejects the OpenAI-only scale tier.
func TestLoadCerebrasRejectsUnsupportedServiceTier(t *testing.T) {
	_, err := Load(strings.NewReader(`
schema_version: 1
defaults:
  provider: cerebras
  api_key_env: CEREBRAS_API_KEY
run:
  samples: 1
scenario:
  prompt: hello
  max_output_tokens: 16
targets:
  - model: gpt-oss-120b
    service_tier: scale
`))
	if err == nil {
		t.Fatal("expected a service tier validation error")
	}
	if !strings.Contains(err.Error(), "service_tier \"scale\" is invalid") {
		t.Fatalf("error = %q, want cerebras service tier rejection", err.Error())
	}
}

// TestLoadMixedProvidersIsolatesProviderDefaults verifies a cerebras target does not inherit the OpenAI defaults block's base URL.
func TestLoadMixedProvidersIsolatesProviderDefaults(t *testing.T) {
	cfg, err := Load(strings.NewReader(`
schema_version: 1
defaults:
  provider: openai
  api: chat-completions
  base_url: https://api.openai.com/v1
  api_key_env: OPENAI_API_KEY
run:
  samples: 1
scenario:
  prompt: hello
  max_output_tokens: 16
targets:
  - id: gpt
    model: gpt-5.5
  - id: cerebras-oss
    provider: cerebras
    api_key_env: CEREBRAS_API_KEY
    model: gpt-oss-120b
`))
	if err != nil {
		t.Fatalf("load mixed config: %v", err)
	}
	if len(cfg.Targets) != 2 {
		t.Fatalf("targets = %d, want 2", len(cfg.Targets))
	}

	openAITarget := cfg.Targets[0]
	if openAITarget.Provider != "openai" || openAITarget.Settings.BaseURL != "https://api.openai.com/v1" {
		t.Fatalf("openai target = provider %q base %q, want inherited openai defaults", openAITarget.Provider, openAITarget.Settings.BaseURL)
	}
	if openAITarget.Settings.API != openai.ChatCompletionsAPI {
		t.Fatalf("openai target api = %q, want inherited chat-completions", openAITarget.Settings.API)
	}

	cerebrasTarget := cfg.Targets[1]
	if cerebrasTarget.Provider != "cerebras" {
		t.Fatalf("cerebras target provider = %q, want cerebras", cerebrasTarget.Provider)
	}
	if cerebrasTarget.Settings.BaseURL != cerebras.DefaultBaseURL {
		t.Fatalf("cerebras target base URL = %q, want cerebras default (not inherited OpenAI URL)", cerebrasTarget.Settings.BaseURL)
	}
	if cerebrasTarget.Settings.APIKeyEnv != "CEREBRAS_API_KEY" {
		t.Fatalf("cerebras target api_key_env = %q, want CEREBRAS_API_KEY", cerebrasTarget.Settings.APIKeyEnv)
	}
	if cerebrasTarget.Settings.API != "" {
		t.Fatalf("cerebras target api = %q, want empty (OpenAI api not inherited)", cerebrasTarget.Settings.API)
	}
}

// TestLoadPerTargetScenarioOverride verifies targets inherit the shared scenario and can override individual fields.
func TestLoadPerTargetScenarioOverride(t *testing.T) {
	cfg, err := Load(strings.NewReader(`
schema_version: 1
run:
  samples: 1
scenario:
  prompt: hello
  max_output_tokens: 16
  reasoning_effort: none
targets:
  - id: nano
    provider: openai
    api: chat-completions
    api_key_env: OPENAI_API_KEY
    model: gpt-x
  - id: oss
    provider: cerebras
    api_key_env: CEREBRAS_API_KEY
    model: gpt-oss-120b
    scenario:
      reasoning_effort: low
      max_output_tokens: 64
`))
	if err != nil {
		t.Fatalf("load per-target scenario config: %v", err)
	}

	if cfg.Scenario.ReasoningEffort != "none" || cfg.Scenario.MaxOutputTokens != 16 {
		t.Fatalf("base scenario mutated: effort=%q max=%d", cfg.Scenario.ReasoningEffort, cfg.Scenario.MaxOutputTokens)
	}

	nano := cfg.Targets[0]
	if nano.Scenario.ReasoningEffort != "none" || nano.Scenario.MaxOutputTokens != 16 {
		t.Fatalf("nano did not inherit shared scenario: effort=%q max=%d", nano.Scenario.ReasoningEffort, nano.Scenario.MaxOutputTokens)
	}

	oss := cfg.Targets[1]
	if oss.Scenario.ReasoningEffort != "low" {
		t.Fatalf("oss reasoning_effort = %q, want low override", oss.Scenario.ReasoningEffort)
	}
	if oss.Scenario.MaxOutputTokens != 64 {
		t.Fatalf("oss max_output_tokens = %d, want 64 override", oss.Scenario.MaxOutputTokens)
	}
	if oss.Scenario.Prompt != "hello" {
		t.Fatalf("oss prompt = %q, want inherited hello", oss.Scenario.Prompt)
	}

	if got := cfg.RunConfigForTarget(oss).Scenario.ReasoningEffort; got != "low" {
		t.Fatalf("RunConfigForTarget scenario effort = %q, want low", got)
	}
}

// TestLoadPerTargetScenarioOverrideValidatesFields verifies invalid per-target scenario overrides are rejected with a target path.
func TestLoadPerTargetScenarioOverrideValidatesFields(t *testing.T) {
	_, err := Load(strings.NewReader(`
schema_version: 1
run:
  samples: 1
scenario:
  prompt: hello
  max_output_tokens: 16
targets:
  - id: bad
    provider: openai
    api: chat-completions
    api_key_env: OPENAI_API_KEY
    model: gpt-x
    scenario:
      reasoning_effort: turbo
`))
	if err == nil {
		t.Fatal("expected a per-target scenario validation error")
	}
	if !strings.Contains(err.Error(), "targets[0].scenario.reasoning_effort \"turbo\" is invalid") {
		t.Fatalf("error = %q, want targeted reasoning_effort rejection", err.Error())
	}
}

// TestLoadAcceptsTogetherTargetWithProviderDefaults verifies together targets default to the Together base URL and omit the OpenAI API field.
func TestLoadAcceptsTogetherTargetWithProviderDefaults(t *testing.T) {
	cfg, err := Load(strings.NewReader(`
schema_version: 1
run:
  samples: 1
scenario:
  prompt: hello
  max_output_tokens: 16
targets:
  - provider: together
    api_key_env: TOGETHER_API_KEY
    model: meta-llama/Llama-3.3-70B-Instruct-Turbo
`))
	if err != nil {
		t.Fatalf("load together config: %v", err)
	}
	target := cfg.Targets[0]
	if target.Provider != "together" {
		t.Fatalf("provider = %q, want together", target.Provider)
	}
	if target.Settings.BaseURL != together.DefaultBaseURL {
		t.Fatalf("base URL = %q, want together default %q", target.Settings.BaseURL, together.DefaultBaseURL)
	}
	if target.Settings.API != "" {
		t.Fatalf("api = %q, want empty for together", target.Settings.API)
	}
}

// TestLoadTogetherRejectsServiceTier verifies Together targets cannot set a service tier.
func TestLoadTogetherRejectsServiceTier(t *testing.T) {
	_, err := Load(strings.NewReader(`
schema_version: 1
run:
  samples: 1
scenario:
  prompt: hello
  max_output_tokens: 16
targets:
  - provider: together
    api_key_env: TOGETHER_API_KEY
    model: some-model
    service_tier: priority
`))
	if err == nil || !strings.Contains(err.Error(), "service_tier \"priority\" is not supported for the together provider") {
		t.Fatalf("error = %v, want together service tier rejection", err)
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
