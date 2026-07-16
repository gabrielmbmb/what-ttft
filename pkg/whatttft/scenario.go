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

	// TopK is the optional top-k sampling cutoff requested from providers that support it, such as Together and the Hugging Face router; nil means the provider default is used and OpenAI-style providers ignore it.
	TopK *int

	// MinP is the optional minimum-probability sampling cutoff requested from providers that support it; nil means the provider default is used and a pointer to zero requests an explicit 0.
	MinP *float64

	// FrequencyPenalty is the optional frequency penalty requested from providers that support it; nil means the provider default is used and a pointer to zero requests an explicit 0.
	FrequencyPenalty *float64

	// PresencePenalty is the optional presence penalty requested from providers that support it; nil means the provider default is used and a pointer to zero requests an explicit 0.
	PresencePenalty *float64

	// RepetitionPenalty is the optional repetition penalty requested from providers that support it, such as Together and the Hugging Face router; nil means the provider default is used and OpenAI-style providers ignore it.
	RepetitionPenalty *float64

	// Stop is the optional list of stop sequences requested from the provider; nil or empty means no explicit stop sequence is used.
	Stop []string

	// Seed is the optional deterministic seed requested from providers that support seeding; nil means no seed is requested.
	Seed *int64

	// ReasoningEffort is the optional provider reasoning/thinking effort setting, such as "none", "minimal", "low", "medium", "high", or "xhigh"; empty means the provider default is used.
	ReasoningEffort string

	// ChatTemplateKwargs is an optional map of chat-template arguments forwarded verbatim as the request's chat_template_kwargs field on providers that support it, such as Together and the Hugging Face router; a common use is {"enable_thinking": false} to disable a model's thinking mode. Nil or empty omits the field and OpenAI-style providers ignore it. Values must not contain secrets.
	ChatTemplateKwargs map[string]any

	// Extra contains provider-independent JSON-compatible scenario metadata; values must not contain secrets unless the caller handles redaction before reporting.
	Extra map[string]any
}
