package openai

type chatCompletionRequest struct {
	// Model is the provider model identifier requested for this chat completion.
	Model string `json:"model"`

	// Messages is the ordered chat transcript sent to the provider.
	Messages []chatMessage `json:"messages"`

	// Stream is true for v0.1 because this benchmark measures streaming responses.
	Stream bool `json:"stream"`

	// StreamOptions optionally requests provider metadata such as token usage in the stream.
	StreamOptions *chatStreamOptions `json:"stream_options,omitempty"`

	// Temperature is the optional sampling temperature; nil omits the field so the provider default applies.
	Temperature *float64 `json:"temperature,omitempty"`

	// TopP is the optional nucleus sampling value; nil omits the field so the provider default applies.
	TopP *float64 `json:"top_p,omitempty"`

	// Stop is the optional list of stop sequences; nil or empty omits the field.
	Stop []string `json:"stop,omitempty"`

	// Seed is the optional deterministic seed for providers that support seeding; nil omits the field.
	Seed *int64 `json:"seed,omitempty"`

	// ReasoningEffort is the optional reasoning/thinking effort for reasoning-capable models; empty omits the field.
	ReasoningEffort string `json:"reasoning_effort,omitempty"`

	// ServiceTier is the optional OpenAI service_tier request label; empty omits the field.
	ServiceTier ServiceTier `json:"service_tier,omitempty"`

	// MaxCompletionTokens is the modern maximum output token field; nil omits the field.
	MaxCompletionTokens *int `json:"max_completion_tokens,omitempty"`

	// MaxTokens is the legacy maximum output token field for OpenAI-compatible providers that still require it; nil omits the field.
	MaxTokens *int `json:"max_tokens,omitempty"`
}

type chatMessage struct {
	// Role is the chat role, such as system, user, or assistant.
	Role string `json:"role"`

	// Content is the message text and may contain sensitive prompt data.
	Content string `json:"content"`
}

type chatStreamOptions struct {
	// IncludeUsage requests a provider usage chunk in the streaming response when true.
	IncludeUsage bool `json:"include_usage"`
}

type chatCompletionChunk struct {
	// ID is the provider chunk identifier; empty means the provider omitted it.
	ID string `json:"id"`

	// Object is the provider object type; empty means the provider omitted it.
	Object string `json:"object"`

	// Created is the provider-reported Unix timestamp in seconds; zero means the provider omitted it.
	Created int64 `json:"created"`

	// Model is the provider model identifier reported for this chunk; empty means the provider omitted it.
	Model string `json:"model"`

	// ServiceTier is the provider-reported actual OpenAI service tier for this chunk; empty means the provider omitted it.
	ServiceTier ServiceTier `json:"service_tier"`

	// Choices is the list of streamed choice deltas in this chunk; empty is valid for usage-only chunks.
	Choices []choice `json:"choices"`

	// Usage is the optional provider-reported token usage payload, usually sent near the end of the stream.
	Usage *usage `json:"usage"`
}

type choice struct {
	// Index is the provider choice index; zero is the first choice and is not a missing value.
	Index int `json:"index"`

	// Delta is the incremental role/content/refusal payload for this choice.
	Delta delta `json:"delta"`

	// FinishReason is the provider-reported reason this choice stopped; empty means no finish reason was present.
	FinishReason string `json:"finish_reason"`
}

type delta struct {
	// Role is the streamed role delta, such as assistant; empty means no role delta was present.
	Role string `json:"role"`

	// Content is the streamed user-visible text delta; empty means no content delta was present.
	Content string `json:"content"`

	// Refusal is the streamed refusal text delta; empty means no refusal delta was present.
	Refusal string `json:"refusal"`
}

type usage struct {
	// PromptTokens is the provider-reported input token count; zero means either zero tokens or omitted by the provider.
	PromptTokens int `json:"prompt_tokens"`

	// CompletionTokens is the provider-reported output token count; zero means either zero tokens or omitted by the provider.
	CompletionTokens int `json:"completion_tokens"`

	// TotalTokens is the provider-reported total token count; zero means either zero tokens or omitted by the provider.
	TotalTokens int `json:"total_tokens"`

	// PromptTokensDetails contains optional provider-reported input token details, including prompt-cache counters.
	PromptTokensDetails *promptTokensDetails `json:"prompt_tokens_details"`

	// CompletionTokensDetails contains optional provider-specific output token details with no secrets expected.
	CompletionTokensDetails map[string]any `json:"completion_tokens_details"`
}

type promptTokensDetails struct {
	// CachedTokens is the provider-reported count of prompt tokens served from cache; zero means no cached tokens or omitted.
	CachedTokens int `json:"cached_tokens"`
}

type responseRequest struct {
	// Model is the provider model identifier requested for this response.
	Model string `json:"model"`

	// Input is the cache-mode-mutated text prompt sent to the model and may contain sensitive prompt data.
	Input string `json:"input"`

	// Instructions is the optional system/developer instruction text; empty omits the field and may contain sensitive prompt data when set.
	Instructions string `json:"instructions,omitempty"`

	// Stream is true because this benchmark measures streaming responses.
	Stream bool `json:"stream"`

	// StreamOptions configures Responses API streaming behavior; nil means provider defaults apply.
	StreamOptions *responseStreamOptions `json:"stream_options,omitempty"`

	// Temperature is the optional sampling temperature; nil omits the field so the provider default applies.
	Temperature *float64 `json:"temperature,omitempty"`

	// TopP is the optional nucleus sampling value; nil omits the field so the provider default applies.
	TopP *float64 `json:"top_p,omitempty"`

	// Reasoning is the optional reasoning/thinking configuration; nil omits the field.
	Reasoning *responseReasoning `json:"reasoning,omitempty"`

	// ServiceTier is the optional OpenAI service_tier request label; empty omits the field.
	ServiceTier ServiceTier `json:"service_tier,omitempty"`

	// MaxOutputTokens is the requested upper bound for generated output tokens, including hidden reasoning tokens; nil omits the field.
	MaxOutputTokens *int `json:"max_output_tokens,omitempty"`
}

type responseStreamOptions struct {
	// IncludeObfuscation controls Responses stream obfuscation overhead; nil uses provider default, false requests no obfuscation.
	IncludeObfuscation *bool `json:"include_obfuscation,omitempty"`
}

type responseReasoning struct {
	// Effort is the optional reasoning effort setting; empty means omitted by the containing request.
	Effort string `json:"effort,omitempty"`
}

type responseStreamEvent struct {
	// Type is the Responses stream event type, such as response.output_text.delta; empty means malformed or unknown JSON.
	Type string `json:"type"`

	// Delta is the incremental visible text or refusal text for delta event types; empty means no visible text.
	Delta string `json:"delta"`

	// Response is the full response object carried by terminal or status events; nil means the event did not include it.
	Response *responseObject `json:"response"`

	// Code is the optional provider error code carried by error events; empty means absent or null.
	Code *string `json:"code"`

	// Message is the provider error message carried by error events; empty means absent.
	Message string `json:"message"`

	// Param is the optional provider error parameter carried by error events; empty means absent or null.
	Param *string `json:"param"`
}

type responseObject struct {
	// ID is the provider response identifier; empty means omitted by the provider.
	ID string `json:"id"`

	// Status is the response status such as completed, failed, or incomplete; empty means omitted by the provider.
	Status string `json:"status"`

	// Model is the provider model identifier reported for this response; empty means omitted by the provider.
	Model string `json:"model"`

	// ServiceTier is the provider-reported actual service tier used for this response; empty means omitted by the provider.
	ServiceTier ServiceTier `json:"service_tier"`

	// Usage is provider-reported token usage for the whole response; nil means unavailable.
	Usage *responseUsage `json:"usage"`

	// Error is the provider-reported terminal error details; nil means no terminal error was reported.
	Error *responseError `json:"error"`

	// IncompleteDetails contains provider-reported incomplete response details; nil means the response was not incomplete or details were omitted.
	IncompleteDetails *responseIncompleteDetails `json:"incomplete_details"`
}

type responseUsage struct {
	// InputTokens is the provider-reported input token count; zero means either zero tokens or omitted by the provider.
	InputTokens int `json:"input_tokens"`

	// InputTokensDetails contains optional provider-reported input token details, including prompt-cache counters.
	InputTokensDetails *responseInputTokensDetails `json:"input_tokens_details"`

	// OutputTokens is the provider-reported output token count; it may include hidden reasoning tokens for reasoning models.
	OutputTokens int `json:"output_tokens"`

	// OutputTokensDetails contains optional provider-reported output token details, including hidden reasoning-token counters.
	OutputTokensDetails *responseOutputTokensDetails `json:"output_tokens_details"`

	// TotalTokens is the provider-reported total token count; zero means either zero tokens or omitted by the provider.
	TotalTokens int `json:"total_tokens"`
}

type responseInputTokensDetails struct {
	// CachedTokens is the provider-reported count of input tokens retrieved from prompt cache; zero means no cached tokens or omitted.
	CachedTokens int `json:"cached_tokens"`
}

type responseOutputTokensDetails struct {
	// ReasoningTokens is the provider-reported count of hidden reasoning tokens included in output_tokens; zero means none or omitted.
	ReasoningTokens int `json:"reasoning_tokens"`
}

type responseError struct {
	// Code is the provider terminal error code; empty means omitted by the provider.
	Code string `json:"code"`

	// Message is the provider terminal error message; empty means omitted by the provider.
	Message string `json:"message"`
}

type responseIncompleteDetails struct {
	// Reason is the provider-reported reason the response is incomplete, such as max_output_tokens or content_filter.
	Reason string `json:"reason"`
}
