package whatttft

// Scenario describes the prompt and model-generation parameters for one benchmark shape.
type Scenario struct {
	// Name is a human-readable scenario identifier used for grouping summaries; empty means callers should assign a default name.
	Name string

	// Prompt is the user prompt before cache-mode mutation; it may contain sensitive data and should not be copied into request records by default.
	Prompt string

	// SystemPrompt is the optional system prompt prepended by providers that support chat roles; empty means no system message is requested.
	SystemPrompt string

	// MaxOutputTokens is the requested maximum output token count; zero means the provider default is used.
	MaxOutputTokens int

	// Temperature is the optional sampling temperature requested from the provider; nil means the provider default is used.
	Temperature *float64

	// TopP is the optional nucleus-sampling probability requested from the provider; nil means the provider default is used.
	TopP *float64

	// Stop is the optional list of stop sequences requested from the provider; nil or empty means no explicit stop sequence is used.
	Stop []string

	// Seed is the optional deterministic seed requested from providers that support seeding; nil means no seed is requested.
	Seed *int64

	// Extra contains provider-independent JSON-compatible scenario metadata; values must not contain secrets unless the caller handles redaction before reporting.
	Extra map[string]any
}
