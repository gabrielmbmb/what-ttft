// Package configfile loads strict YAML benchmark configuration files.
package configfile

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"os"
	"strings"
	"time"

	"github.com/gabrielmbmb/what-ttft/internal/report"
	"github.com/gabrielmbmb/what-ttft/pkg/provider/cerebras"
	"github.com/gabrielmbmb/what-ttft/pkg/provider/openai"
	"github.com/gabrielmbmb/what-ttft/pkg/provider/together"
	"github.com/gabrielmbmb/what-ttft/pkg/whatttft"
	"go.yaml.in/yaml/v3"
)

const (
	defaultBenchmarkConcurrency = 1
	defaultBenchmarkTimeout     = 120 * time.Second

	// providerOpenAI is the config provider value selecting the OpenAI-compatible adapter.
	providerOpenAI = "openai"

	// providerCerebras is the config provider value selecting the Cerebras adapter.
	providerCerebras = "cerebras"

	// providerTogether is the config provider value selecting the Together AI adapter.
	providerTogether = "together"
)

// Config is a validated, normalized YAML benchmark configuration.
type Config struct {
	// SchemaVersion is the YAML schema version; v0.2 accepts only version 1 and zero is invalid.
	SchemaVersion int `json:"schema_version"`

	// Name is the optional benchmark name used for report metadata and default output directory names; empty means no user label was supplied and it must not contain secrets.
	Name string `json:"name,omitempty"`

	// Scenario is the shared prompt and generation configuration used by every target; prompt fields may contain sensitive data and belong only in run metadata, not request records.
	Scenario whatttft.Scenario `json:"scenario"`

	// Run contains shared request-count, cache, connection, timeout, and chunk-capture settings; counts are requests and Timeout is stored as nanoseconds in JSON.
	Run RunSettings `json:"run"`

	// Targets contains the normalized benchmark targets in YAML order; the slice is non-empty after successful validation and target IDs are unique.
	Targets []Target `json:"targets"`
}

// RunSettings contains run-level settings shared by all YAML benchmark targets.
type RunSettings struct {
	// Samples is the count of measured requests per target; units are requests and zero is valid only when Warmup is positive.
	Samples int `json:"samples"`

	// Warmup is the count of warmup requests per target; units are requests and zero means no warmup phase.
	Warmup int `json:"warmup"`

	// Concurrency is the fixed-concurrency worker count per target; units are workers and values less than one are normalized to one before validation.
	Concurrency int `json:"concurrency"`

	// CacheMode is the requested prompt/KV cache behavior shared by all targets; summaries must not mix different cache modes.
	CacheMode whatttft.CacheMode `json:"cache_mode"`

	// ConnectionMode is the HTTP connection reuse behavior shared by all targets; summaries must not mix different connection modes.
	ConnectionMode whatttft.ConnectionMode `json:"connection_mode"`

	// Timeout is the whole-request HTTP client timeout; units are nanoseconds in JSON and zero means no whole-request timeout.
	Timeout time.Duration `json:"timeout_ns"`

	// SaveChunks controls whether generated chunks are written to chunks.jsonl; false omits chunks because they may contain sensitive model output.
	SaveChunks bool `json:"save_chunks"`
}

// Target is one normalized benchmark target after defaults and per-target overrides are applied.
type Target struct {
	// ID is a stable sanitized benchmark target identifier used for grouping, request ID prefixes, and path-safe labels; it never contains secrets.
	ID string `json:"id"`

	// Name is an optional human-readable target label preserved from YAML; empty means no separate label was supplied and it must not contain secrets.
	Name string `json:"name,omitempty"`

	// Provider is the normalized provider name; accepted values are openai and cerebras, and the value contains no secrets.
	Provider string `json:"provider"`

	// Settings contains normalized per-target provider settings shared across providers; zero values are invalid after successful validation except optional fields documented on ProviderSettings.
	Settings ProviderSettings `json:"settings"`

	// Scenario is the effective scenario for this target after applying any per-target scenario overrides onto the shared top-level scenario; prompt fields may contain sensitive data and belong only in run metadata, not request records.
	Scenario whatttft.Scenario `json:"scenario"`
}

// ProviderSettings contains normalized per-target provider settings shared across supported providers.
type ProviderSettings struct {
	// API is the OpenAI API surface requested for the target; it applies only when Provider is openai and is empty for providers that expose a single API surface such as cerebras.
	API openai.API `json:"api,omitempty"`

	// BaseURL is the provider API base URL without endpoint suffix; credentials must not be present and report metadata should use RedactedBaseURL.
	BaseURL string `json:"base_url"`

	// APIKeyEnv is the environment variable name used to resolve the API key at execution time; it is not secret and inline API keys are not supported.
	APIKeyEnv string `json:"api_key_env"`

	// Model is the provider model identifier for this target; it contains no secrets unless a deployment naming convention embeds them.
	Model string `json:"model"`

	// ServiceTier is the optional provider service_tier request value; empty means omit the request field and allow provider default behavior, and accepted values depend on Provider.
	ServiceTier openai.ServiceTier `json:"service_tier,omitempty"`

	// IncludeUsage requests Chat Completions stream usage chunks when true; OpenAI Responses usage comes from terminal response events and this flag is ignored there.
	IncludeUsage bool `json:"include_usage"`

	// LegacyMaxTokens requests the Chat Completions legacy max_tokens field when true; Responses targets ignore this compatibility setting.
	LegacyMaxTokens bool `json:"legacy_max_tokens"`
}

// RedactedBaseURL returns the target base URL with credentials and secret-looking query values removed for metadata output.
func (s ProviderSettings) RedactedBaseURL() string {
	return report.RedactURL(s.BaseURL)
}

// RunConfigForTarget builds a single-target runner config with target identity and deterministic request ID prefixes populated.
func (c Config) RunConfigForTarget(target Target) whatttft.RunConfig {
	return whatttft.RunConfig{
		Scenario:         target.Scenario,
		WarmupRequests:   c.Run.Warmup,
		MeasuredRequests: c.Run.Samples,
		Concurrency:      c.Run.Concurrency,
		CacheMode:        c.Run.CacheMode,
		ConnectionMode:   c.Run.ConnectionMode,
		TargetID:         target.ID,
		TargetName:       target.Name,
		RequestIDPrefix:  target.ID,
		SaveChunks:       c.Run.SaveChunks,
	}
}

// LoadFile reads, parses, and validates a strict YAML benchmark config from path.
func LoadFile(path string) (cfg *Config, err error) {
	// #nosec G304 -- benchmark config paths are caller-provided CLI inputs by design.
	file, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("open config file: %w", err)
	}
	defer func() {
		if closeErr := file.Close(); err == nil && closeErr != nil {
			err = fmt.Errorf("close config file: %w", closeErr)
		}
	}()

	cfg, err = Load(file)
	if err != nil {
		return nil, fmt.Errorf("load %s: %w", path, err)
	}

	return cfg, nil
}

// Load parses and validates a strict YAML benchmark config from reader.
func Load(reader io.Reader) (*Config, error) {
	var raw rawFile
	decoder := yaml.NewDecoder(reader)
	decoder.KnownFields(true)
	if err := decoder.Decode(&raw); err != nil {
		return nil, fmt.Errorf("decode YAML config: %w", err)
	}

	cfg, err := normalize(raw)
	if err != nil {
		return nil, err
	}

	return cfg, nil
}

// SHA256Hex returns the SHA-256 hex digest of data for recording the exact YAML bytes used for a benchmark.
func SHA256Hex(data []byte) string {
	digest := sha256.Sum256(data)
	return hex.EncodeToString(digest[:])
}

type rawFile struct {
	// SchemaVersion is the YAML schema version; v0.2 requires exactly 1.
	SchemaVersion int `yaml:"schema_version" json:"schema_version"`

	// Name is an optional benchmark label used in metadata and default output names; empty means unset.
	Name string `yaml:"name,omitempty" json:"name,omitempty"`

	// Defaults contains provider/model/API defaults inherited by every target; zero values are filled during normalization.
	Defaults rawTargetDefaults `yaml:"defaults,omitempty" json:"defaults,omitempty"`

	// Run contains shared sample counts, timeout, cache mode, and connection mode; omitted optional fields are defaulted during normalization.
	Run rawRunSettings `yaml:"run" json:"run"`

	// Scenario contains the shared prompt and model parameter settings; prompt is required and may contain sensitive data.
	Scenario rawScenario `yaml:"scenario" json:"scenario"`

	// Targets contains provider/model/API target overrides; at least one target is required.
	Targets []rawTarget `yaml:"targets" json:"targets"`
}

type rawTargetDefaults struct {
	// Provider is the default provider inherited by targets; empty normalizes to openai.
	Provider string `yaml:"provider,omitempty" json:"provider,omitempty"`

	// API is the default OpenAI API mode inherited by targets; empty normalizes to responses.
	API string `yaml:"api,omitempty" json:"api,omitempty"`

	// BaseURL is the default provider base URL inherited by targets; empty normalizes to the public OpenAI base URL.
	BaseURL string `yaml:"base_url,omitempty" json:"base_url,omitempty"`

	// APIKeyEnv is the default environment variable name used to resolve API keys; empty remains invalid unless a target overrides it.
	APIKeyEnv string `yaml:"api_key_env,omitempty" json:"api_key_env,omitempty"`

	// APIKey is rejected when present because v0.2 YAML configs support api_key_env only and must not contain inline secrets.
	APIKey *string `yaml:"api_key,omitempty" json:"-"`

	// ServiceTier is the default OpenAI service tier inherited by targets; empty means targets omit service_tier unless they override it.
	ServiceTier string `yaml:"service_tier,omitempty" json:"service_tier,omitempty"`

	// IncludeUsage controls Chat Completions usage chunks when inherited; nil means use the benchmark default of true.
	IncludeUsage *bool `yaml:"include_usage,omitempty" json:"include_usage,omitempty"`

	// LegacyMaxTokens controls Chat Completions legacy max_tokens compatibility when inherited; nil means false.
	LegacyMaxTokens *bool `yaml:"legacy_max_tokens,omitempty" json:"legacy_max_tokens,omitempty"`

	// Model is the default provider model inherited by targets; empty remains invalid unless every target overrides it.
	Model string `yaml:"model,omitempty" json:"model,omitempty"`
}

type rawTarget struct {
	// ID is an optional target identifier; empty generates a deterministic sanitized ID from provider, model, and target index.
	ID string `yaml:"id,omitempty" json:"id,omitempty"`

	// Name is an optional human-readable target label; empty means no label distinct from ID/model was supplied.
	Name string `yaml:"name,omitempty" json:"name,omitempty"`

	// Provider overrides defaults.provider for this target; empty inherits the default provider.
	Provider string `yaml:"provider,omitempty" json:"provider,omitempty"`

	// API overrides defaults.api for this target; empty inherits and ultimately defaults to responses.
	API string `yaml:"api,omitempty" json:"api,omitempty"`

	// BaseURL overrides defaults.base_url for this target; empty inherits and ultimately defaults to the public OpenAI base URL.
	BaseURL string `yaml:"base_url,omitempty" json:"base_url,omitempty"`

	// APIKeyEnv overrides defaults.api_key_env for this target; empty inherits and is invalid if no inherited value exists.
	APIKeyEnv string `yaml:"api_key_env,omitempty" json:"api_key_env,omitempty"`

	// APIKey is rejected when present because v0.2 YAML configs support api_key_env only and must not contain inline secrets.
	APIKey *string `yaml:"api_key,omitempty" json:"-"`

	// ServiceTier overrides defaults.service_tier for this target; empty inherits and may remain empty to omit service_tier.
	ServiceTier string `yaml:"service_tier,omitempty" json:"service_tier,omitempty"`

	// IncludeUsage overrides defaults.include_usage for this target when set; nil inherits and ultimately defaults to true.
	IncludeUsage *bool `yaml:"include_usage,omitempty" json:"include_usage,omitempty"`

	// LegacyMaxTokens overrides defaults.legacy_max_tokens for this target when set; nil inherits and ultimately defaults to false.
	LegacyMaxTokens *bool `yaml:"legacy_max_tokens,omitempty" json:"legacy_max_tokens,omitempty"`

	// Model overrides defaults.model for this target; empty inherits and is invalid if no inherited model exists.
	Model string `yaml:"model,omitempty" json:"model,omitempty"`

	// Scenario optionally overrides individual top-level scenario fields for this target; nil means the target uses the shared scenario unchanged.
	Scenario *rawScenarioOverride `yaml:"scenario,omitempty" json:"scenario,omitempty"`
}

// rawScenarioOverride carries optional per-target overrides of top-level scenario fields; nil pointer fields inherit the shared scenario value.
type rawScenarioOverride struct {
	// SystemPrompt overrides scenario.system_prompt for this target; nil inherits and a non-nil empty string clears the system prompt.
	SystemPrompt *string `yaml:"system_prompt,omitempty" json:"system_prompt,omitempty"`

	// Prompt overrides scenario.prompt for this target; nil inherits and the effective prompt must be non-empty after inheritance.
	Prompt *string `yaml:"prompt,omitempty" json:"prompt,omitempty"`

	// MaxOutputTokens overrides scenario.max_output_tokens for this target; nil inherits and units are tokens.
	MaxOutputTokens *int `yaml:"max_output_tokens,omitempty" json:"max_output_tokens,omitempty"`

	// Temperature overrides scenario.temperature for this target; nil inherits while a pointer to zero sets an explicit zero.
	Temperature *float64 `yaml:"temperature,omitempty" json:"temperature,omitempty"`

	// TopP overrides scenario.top_p for this target; nil inherits while a pointer to zero sets an explicit zero.
	TopP *float64 `yaml:"top_p,omitempty" json:"top_p,omitempty"`

	// ReasoningEffort overrides scenario.reasoning_effort for this target; nil inherits and the effective value must be a supported reasoning effort label.
	ReasoningEffort *string `yaml:"reasoning_effort,omitempty" json:"reasoning_effort,omitempty"`
}

type rawRunSettings struct {
	// Samples is the measured request count per target; units are requests and zero is valid only when warmup is positive.
	Samples int `yaml:"samples" json:"samples"`

	// Warmup is the warmup request count per target; units are requests and zero means no warmup phase.
	Warmup int `yaml:"warmup" json:"warmup"`

	// Concurrency is the fixed-concurrency worker count per target; zero means use the default of one worker.
	Concurrency int `yaml:"concurrency" json:"concurrency"`

	// CacheMode is the requested prompt/KV cache behavior; empty defaults to cache-bust.
	CacheMode string `yaml:"cache_mode" json:"cache_mode"`

	// ConnectionMode is the HTTP connection reuse behavior; empty defaults to warm.
	ConnectionMode string `yaml:"connection_mode" json:"connection_mode"`

	// Timeout is the optional whole-request timeout parsed from a Go duration string; nil defaults to 120s.
	Timeout *rawDuration `yaml:"timeout" json:"timeout,omitempty"`

	// SaveChunks controls chunks.jsonl output; false omits chunks because they may contain generated content.
	SaveChunks bool `yaml:"save_chunks" json:"save_chunks"`
}

type rawScenario struct {
	// Name is an optional scenario label used for summaries; empty means the scenario is unnamed.
	Name string `yaml:"name,omitempty" json:"name,omitempty"`

	// SystemPrompt is the optional system/developer prompt shared by all targets; empty means no system prompt is requested and non-empty values may be sensitive.
	SystemPrompt string `yaml:"system_prompt,omitempty" json:"system_prompt,omitempty"`

	// Prompt is the required user prompt shared by all targets before cache-mode mutation; it may contain sensitive data.
	Prompt string `yaml:"prompt" json:"prompt"`

	// MaxOutputTokens is the optional output-token cap; units are tokens and zero means provider default.
	MaxOutputTokens int `yaml:"max_output_tokens,omitempty" json:"max_output_tokens,omitempty"`

	// Temperature is the optional sampling temperature; nil means omitted while a pointer to zero means explicitly configured zero.
	Temperature *float64 `yaml:"temperature,omitempty" json:"temperature,omitempty"`

	// TopP is the optional nucleus-sampling probability; nil means omitted while a pointer to zero means explicitly configured zero.
	TopP *float64 `yaml:"top_p,omitempty" json:"top_p,omitempty"`

	// ReasoningEffort is the optional reasoning/thinking effort label; empty means provider default.
	ReasoningEffort string `yaml:"reasoning_effort,omitempty" json:"reasoning_effort,omitempty"`
}

type rawDuration struct {
	value time.Duration
}

func (d *rawDuration) UnmarshalYAML(node *yaml.Node) error {
	if node.Kind != yaml.ScalarNode || node.ShortTag() != "!!str" {
		return fmt.Errorf("duration must be a Go duration string such as 120s, 2m, or 500ms")
	}

	parsed, err := time.ParseDuration(node.Value)
	if err != nil {
		return fmt.Errorf("parse duration %q: %w", node.Value, err)
	}
	d.value = parsed
	return nil
}

func (d rawDuration) MarshalJSON() ([]byte, error) {
	return json.Marshal(d.value.String())
}

type validationErrors []string

func (e validationErrors) Error() string {
	return "invalid benchmark config: " + strings.Join(e, "; ")
}

func (e *validationErrors) addf(format string, args ...any) {
	*e = append(*e, fmt.Sprintf(format, args...))
}

func (e validationErrors) err() error {
	if len(e) == 0 {
		return nil
	}

	return e
}

func normalize(raw rawFile) (*Config, error) {
	var errs validationErrors
	if raw.SchemaVersion != 1 {
		errs.addf("schema_version must be 1")
	}
	if raw.Defaults.APIKey != nil {
		errs.addf("defaults.api_key is not supported; use defaults.api_key_env")
	}
	if len(raw.Targets) == 0 {
		errs.addf("targets must contain at least one target")
	}

	run := normalizeRun(raw.Run, &errs)
	scenario := normalizeScenario(raw.Scenario, &errs)
	targets := normalizeTargets(raw.Defaults, raw.Targets, scenario, &errs)
	if err := errs.err(); err != nil {
		return nil, err
	}

	return &Config{
		SchemaVersion: raw.SchemaVersion,
		Name:          strings.TrimSpace(raw.Name),
		Scenario:      scenario,
		Run:           run,
		Targets:       targets,
	}, nil
}

func normalizeRun(raw rawRunSettings, errs *validationErrors) RunSettings {
	if raw.Samples < 0 {
		errs.addf("run.samples must be non-negative")
	}
	if raw.Warmup < 0 {
		errs.addf("run.warmup must be non-negative")
	}
	if raw.Samples+raw.Warmup == 0 {
		errs.addf("run.samples + run.warmup must be greater than zero")
	}

	concurrency := raw.Concurrency
	if concurrency == 0 {
		concurrency = defaultBenchmarkConcurrency
	}
	if concurrency < 0 {
		errs.addf("run.concurrency must be at least 1")
	}

	cacheMode := whatttft.CacheMode(strings.TrimSpace(raw.CacheMode))
	if cacheMode == "" {
		cacheMode = whatttft.CacheBust
	}
	if !validCacheMode(cacheMode) {
		errs.addf("run.cache_mode %q is invalid; expected cache-bust, cache-reuse, provider-explicit-cache, or unknown", raw.CacheMode)
	}

	connectionMode := whatttft.ConnectionMode(strings.TrimSpace(raw.ConnectionMode))
	if connectionMode == "" {
		connectionMode = whatttft.WarmConnections
	}
	if !validConnectionMode(connectionMode) {
		errs.addf("run.connection_mode %q is invalid; expected warm or cold", raw.ConnectionMode)
	}

	timeout := defaultBenchmarkTimeout
	if raw.Timeout != nil {
		timeout = raw.Timeout.value
	}
	if timeout < 0 {
		errs.addf("run.timeout must be non-negative")
	}

	return RunSettings{
		Samples:        raw.Samples,
		Warmup:         raw.Warmup,
		Concurrency:    concurrency,
		CacheMode:      cacheMode,
		ConnectionMode: connectionMode,
		Timeout:        timeout,
		SaveChunks:     raw.SaveChunks,
	}
}

func normalizeScenario(raw rawScenario, errs *validationErrors) whatttft.Scenario {
	if strings.TrimSpace(raw.Prompt) == "" {
		errs.addf("scenario.prompt is required")
	}
	if raw.MaxOutputTokens < 0 {
		errs.addf("scenario.max_output_tokens must be non-negative")
	}
	if raw.Temperature != nil && !finiteFloat(*raw.Temperature) {
		errs.addf("scenario.temperature must be finite")
	}
	if raw.TopP != nil && !finiteFloat(*raw.TopP) {
		errs.addf("scenario.top_p must be finite")
	}
	if !validReasoningEffort(raw.ReasoningEffort) {
		errs.addf("scenario.reasoning_effort %q is invalid; expected none, minimal, low, medium, high, xhigh, or empty", raw.ReasoningEffort)
	}

	return whatttft.Scenario{
		Name:            strings.TrimSpace(raw.Name),
		Prompt:          raw.Prompt,
		SystemPrompt:    raw.SystemPrompt,
		MaxOutputTokens: raw.MaxOutputTokens,
		Temperature:     raw.Temperature,
		TopP:            raw.TopP,
		ReasoningEffort: strings.TrimSpace(raw.ReasoningEffort),
	}
}

func normalizeTargets(defaults rawTargetDefaults, raws []rawTarget, scenario whatttft.Scenario, errs *validationErrors) []Target {
	targets := make([]Target, 0, len(raws))
	seenIDs := make(map[string]int, len(raws))
	for index, raw := range raws {
		path := fmt.Sprintf("targets[%d]", index)
		if raw.APIKey != nil {
			errs.addf("%s.api_key is not supported; use %s.api_key_env", path, path)
		}

		// Provider-specific defaults (base URL, API key env, service tier, API, usage/token flags,
		// and even the model) only flow from the defaults block when a target keeps the default
		// provider. Switching a target's provider opts it out of those inherited values so a
		// Cerebras target never inherits an OpenAI base URL or API key env in a mixed benchmark.
		defaultProvider := firstNonEmptyTrimmed(defaults.Provider, providerOpenAI)
		provider := firstNonEmptyTrimmed(raw.Provider, defaults.Provider, providerOpenAI)
		sameProvider := provider == defaultProvider

		api := normalizeProviderAPI(provider, sameProvider, raw.API, defaults.API)
		baseURL := inheritIfSameProvider(sameProvider, raw.BaseURL, defaults.BaseURL)
		if baseURL == "" {
			baseURL = defaultBaseURLForProvider(provider)
		}
		apiKeyEnv := inheritIfSameProvider(sameProvider, raw.APIKeyEnv, defaults.APIKeyEnv)
		model := inheritIfSameProvider(sameProvider, raw.Model, defaults.Model)
		serviceTier := openai.ServiceTier(inheritIfSameProvider(sameProvider, raw.ServiceTier, defaults.ServiceTier))
		includeUsage := boolWithDefault(raw.IncludeUsage, inheritedBool(sameProvider, defaults.IncludeUsage), true)
		legacyMaxTokens := boolWithDefault(raw.LegacyMaxTokens, inheritedBool(sameProvider, defaults.LegacyMaxTokens), false)

		targetScenario := applyScenarioOverride(scenario, raw.Scenario)
		validateTargetScenario(targetScenario, path, errs)

		if !validProvider(provider) {
			errs.addf("%s.provider %q is invalid after inheritance; supported providers are openai and cerebras", path, provider)
		} else {
			validateProviderTargetOptions(provider, api, serviceTier, targetScenario, path, errs)
		}
		if apiKeyEnv == "" {
			errs.addf("%s.api_key_env is required after inheritance", path)
		}
		if model == "" {
			errs.addf("%s.model is required after inheritance", path)
		}

		id := normalizeTargetID(raw.ID, provider, model, index)
		if id == "" {
			errs.addf("%s.id cannot be sanitized into a non-empty target ID", path)
		}
		if previous, ok := seenIDs[id]; ok {
			errs.addf("%s.id %q duplicates targets[%d].id after normalization", path, id, previous)
		} else if id != "" {
			seenIDs[id] = index
		}

		targets = append(targets, Target{
			ID:       id,
			Name:     strings.TrimSpace(raw.Name),
			Provider: provider,
			Settings: ProviderSettings{
				API:             api,
				BaseURL:         baseURL,
				APIKeyEnv:       apiKeyEnv,
				Model:           model,
				ServiceTier:     serviceTier,
				IncludeUsage:    includeUsage,
				LegacyMaxTokens: legacyMaxTokens,
			},
			Scenario: targetScenario,
		})
	}

	return targets
}

// applyScenarioOverride returns the base scenario with any non-nil per-target override fields applied.
func applyScenarioOverride(base whatttft.Scenario, override *rawScenarioOverride) whatttft.Scenario {
	if override == nil {
		return base
	}

	result := base
	if override.SystemPrompt != nil {
		result.SystemPrompt = *override.SystemPrompt
	}
	if override.Prompt != nil {
		result.Prompt = *override.Prompt
	}
	if override.MaxOutputTokens != nil {
		result.MaxOutputTokens = *override.MaxOutputTokens
	}
	if override.Temperature != nil {
		value := *override.Temperature
		result.Temperature = &value
	}
	if override.TopP != nil {
		value := *override.TopP
		result.TopP = &value
	}
	if override.ReasoningEffort != nil {
		result.ReasoningEffort = strings.TrimSpace(*override.ReasoningEffort)
	}

	return result
}

// validateTargetScenario validates the effective per-target scenario after overrides are applied.
func validateTargetScenario(scenario whatttft.Scenario, path string, errs *validationErrors) {
	if strings.TrimSpace(scenario.Prompt) == "" {
		errs.addf("%s.scenario.prompt is required after inheritance", path)
	}
	if scenario.MaxOutputTokens < 0 {
		errs.addf("%s.scenario.max_output_tokens must be non-negative", path)
	}
	if scenario.Temperature != nil && !finiteFloat(*scenario.Temperature) {
		errs.addf("%s.scenario.temperature must be finite", path)
	}
	if scenario.TopP != nil && !finiteFloat(*scenario.TopP) {
		errs.addf("%s.scenario.top_p must be finite", path)
	}
	if !validReasoningEffort(scenario.ReasoningEffort) {
		errs.addf("%s.scenario.reasoning_effort %q is invalid; expected none, minimal, low, medium, high, xhigh, or empty", path, scenario.ReasoningEffort)
	}
}

func normalizeTargetID(rawID string, provider string, model string, index int) string {
	if strings.TrimSpace(rawID) != "" {
		return sanitizeIdentifier(rawID, "")
	}

	return sanitizeIdentifier(fmt.Sprintf("%s-%s-%03d", provider, model, index+1), fmt.Sprintf("target-%03d", index+1))
}

func sanitizeIdentifier(value string, fallback string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	var builder strings.Builder
	lastSeparator := false
	for _, char := range value {
		if identifierChar(char) {
			builder.WriteRune(char)
			lastSeparator = false
			continue
		}
		if !lastSeparator {
			builder.WriteByte('-')
			lastSeparator = true
		}
	}

	sanitized := strings.Trim(builder.String(), "-")
	if len(sanitized) > 80 {
		sanitized = strings.Trim(sanitized[:80], "-")
	}
	if sanitized == "" {
		return fallback
	}

	return sanitized
}

func identifierChar(char rune) bool {
	return char >= 'a' && char <= 'z' || char >= '0' && char <= '9' || char == '-' || char == '_' || char == '.'
}

func boolWithDefault(value *bool, inherited *bool, fallback bool) bool {
	if value != nil {
		return *value
	}
	if inherited != nil {
		return *inherited
	}

	return fallback
}

func firstNonEmptyTrimmed(values ...string) string {
	for _, value := range values {
		trimmed := strings.TrimSpace(value)
		if trimmed != "" {
			return trimmed
		}
	}

	return ""
}

func finiteFloat(value float64) bool {
	return !math.IsNaN(value) && !math.IsInf(value, 0)
}

func validCacheMode(mode whatttft.CacheMode) bool {
	switch mode {
	case whatttft.CacheBust, whatttft.CacheReuse, whatttft.ProviderExplicitCache, whatttft.CacheUnknown:
		return true
	default:
		return false
	}
}

func validConnectionMode(mode whatttft.ConnectionMode) bool {
	switch mode {
	case whatttft.WarmConnections, whatttft.ColdConnections:
		return true
	default:
		return false
	}
}

// validProvider reports whether provider is a supported benchmark provider.
func validProvider(provider string) bool {
	switch provider {
	case providerOpenAI, providerCerebras, providerTogether:
		return true
	default:
		return false
	}
}

// defaultBaseURLForProvider returns the public API base URL used when a target does not set one.
func defaultBaseURLForProvider(provider string) string {
	switch provider {
	case providerCerebras:
		return cerebras.DefaultBaseURL
	case providerTogether:
		return together.DefaultBaseURL
	default:
		return openai.DefaultBaseURL
	}
}

// normalizeProviderAPI resolves the OpenAI API surface for a target; it is empty for providers
// that expose a single API surface such as cerebras, and inherits defaults.api only when the
// target keeps the default provider.
func normalizeProviderAPI(provider string, inheritDefault bool, rawAPI string, defaultAPI string) openai.API {
	if provider != providerOpenAI {
		return ""
	}

	inherited := ""
	if inheritDefault {
		inherited = defaultAPI
	}

	return openai.API(firstNonEmptyTrimmed(rawAPI, inherited, string(openai.ResponsesAPI)))
}

// inheritIfSameProvider returns the trimmed target value, falling back to the default only when
// the target keeps the default provider so provider-specific settings never cross provider boundaries.
func inheritIfSameProvider(sameProvider bool, targetValue string, defaultValue string) string {
	if sameProvider {
		return firstNonEmptyTrimmed(targetValue, defaultValue)
	}

	return strings.TrimSpace(targetValue)
}

// inheritedBool returns the inherited default boolean only when the target keeps the default provider.
func inheritedBool(sameProvider bool, defaultValue *bool) *bool {
	if sameProvider {
		return defaultValue
	}

	return nil
}

// validateProviderTargetOptions validates provider-specific target fields after inheritance.
func validateProviderTargetOptions(provider string, api openai.API, serviceTier openai.ServiceTier, scenario whatttft.Scenario, path string, errs *validationErrors) {
	switch provider {
	case providerCerebras:
		if !cerebras.ValidServiceTier(cerebras.ServiceTier(serviceTier)) {
			errs.addf("%s.service_tier %q is invalid after inheritance; expected auto, default, flex, priority, or empty", path, serviceTier)
		}
	case providerTogether:
		if serviceTier != "" {
			errs.addf("%s.service_tier %q is not supported for the together provider", path, serviceTier)
		}
	case providerOpenAI:
		if !validOpenAIAPI(api) {
			errs.addf("%s.api %q is invalid after inheritance; expected responses or chat-completions", path, api)
		}
		if !validServiceTier(serviceTier) {
			errs.addf("%s.service_tier %q is invalid after inheritance; expected auto, default, flex, scale, priority, or empty", path, serviceTier)
		}
		if api == openai.ResponsesAPI && scenario.MaxOutputTokens > 0 && scenario.MaxOutputTokens < 16 {
			errs.addf("scenario.max_output_tokens must be at least 16 when %s.api is responses", path)
		}
	}
}

func validOpenAIAPI(api openai.API) bool {
	switch api {
	case openai.ResponsesAPI, openai.ChatCompletionsAPI:
		return true
	default:
		return false
	}
}

func validServiceTier(tier openai.ServiceTier) bool {
	switch tier {
	case "", openai.ServiceTierAuto, openai.ServiceTierDefault, openai.ServiceTierFlex, openai.ServiceTierScale, openai.ServiceTierPriority:
		return true
	default:
		return false
	}
}

func validReasoningEffort(effort string) bool {
	switch strings.TrimSpace(effort) {
	case "", "none", "minimal", "low", "medium", "high", "xhigh":
		return true
	default:
		return false
	}
}
