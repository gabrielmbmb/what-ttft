package cerebras

type chatCompletionRequest struct {
	// Model is the Cerebras model identifier requested for this chat completion.
	Model string `json:"model"`

	// Messages is the ordered chat transcript sent to the provider.
	Messages []chatMessage `json:"messages"`

	// Stream is always true because this benchmark measures streaming responses.
	Stream bool `json:"stream"`

	// StreamOptions optionally requests provider metadata such as token usage in the stream; nil omits the field.
	StreamOptions *chatStreamOptions `json:"stream_options,omitempty"`

	// Temperature is the optional sampling temperature; nil omits the field so the provider default applies.
	Temperature *float64 `json:"temperature,omitempty"`

	// TopP is the optional nucleus sampling value; nil omits the field so the provider default applies.
	TopP *float64 `json:"top_p,omitempty"`

	// Stop is the optional list of stop sequences; nil or empty omits the field.
	Stop []string `json:"stop,omitempty"`

	// Seed is the optional deterministic seed; nil omits the field.
	Seed *int64 `json:"seed,omitempty"`

	// ReasoningEffort is the optional reasoning effort for reasoning-capable models such as low, medium, high, or none; empty omits the field.
	ReasoningEffort string `json:"reasoning_effort,omitempty"`

	// ServiceTier is the optional Cerebras service_tier request label; empty omits the field.
	ServiceTier ServiceTier `json:"service_tier,omitempty"`

	// MaxCompletionTokens is the maximum output token field, including reasoning tokens; nil omits the field.
	MaxCompletionTokens *int `json:"max_completion_tokens,omitempty"`

	// MaxTokens is the legacy maximum output token field for compatibility experiments; nil omits the field.
	MaxTokens *int `json:"max_tokens,omitempty"`
}

type chatMessage struct {
	// Role is the chat role, such as system or user.
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

	// Object is the provider object type, such as chat.completion.chunk; empty means the provider omitted it.
	Object string `json:"object"`

	// Created is the provider-reported Unix timestamp in seconds; zero means the provider omitted it.
	Created int64 `json:"created"`

	// Model is the provider model identifier reported for this chunk; empty means the provider omitted it.
	Model string `json:"model"`

	// ServiceTier is the provider-reported actual service tier for this chunk; empty means the provider omitted it.
	ServiceTier ServiceTier `json:"service_tier"`

	// ServiceTierUsed is the provider-reported tier actually used when service_tier auto was requested; empty means the provider omitted it.
	ServiceTierUsed ServiceTier `json:"service_tier_used"`

	// Choices is the list of streamed choice deltas in this chunk; empty is valid for usage-only chunks.
	Choices []choice `json:"choices"`

	// Usage is the optional provider-reported token usage payload, usually sent in the terminal chunk.
	Usage *usage `json:"usage"`

	// TimeInfo is the optional provider-reported server-side timing breakdown, usually sent in the terminal chunk; nil means the provider omitted it.
	TimeInfo *timeInfo `json:"time_info"`
}

type choice struct {
	// Index is the provider choice index; zero is the first choice and is not a missing value.
	Index int `json:"index"`

	// Delta is the incremental role/content/reasoning payload for this choice.
	Delta delta `json:"delta"`

	// FinishReason is the provider-reported reason this choice stopped; empty means no finish reason was present.
	FinishReason string `json:"finish_reason"`
}

type delta struct {
	// Role is the streamed role delta, such as assistant; empty means no role delta was present.
	Role string `json:"role"`

	// Content is the streamed user-visible text delta; empty means no content delta was present.
	Content string `json:"content"`

	// Reasoning is the streamed hidden reasoning delta for reasoning models; empty means no reasoning delta was present and it is never counted as user-visible output.
	Reasoning string `json:"reasoning"`
}

type usage struct {
	// PromptTokens is the provider-reported input token count; zero means either zero tokens or omitted by the provider.
	PromptTokens int `json:"prompt_tokens"`

	// CompletionTokens is the provider-reported output token count; zero means either zero tokens or omitted by the provider.
	CompletionTokens int `json:"completion_tokens"`

	// TotalTokens is the provider-reported total token count; zero means either zero tokens or omitted by the provider.
	TotalTokens int `json:"total_tokens"`

	// ImageTokens is the provider-reported count of tokens used to represent image inputs on vision-capable models; zero means none or omitted.
	ImageTokens int `json:"image_tokens"`

	// PromptTokensDetails contains optional provider-reported input token details, including prompt-cache counters.
	PromptTokensDetails *promptTokensDetails `json:"prompt_tokens_details"`

	// CompletionTokensDetails contains optional provider-reported output token details, including reasoning-token counters.
	CompletionTokensDetails *completionTokensDetails `json:"completion_tokens_details"`
}

type promptTokensDetails struct {
	// CachedTokens is the provider-reported count of prompt tokens served from cache; zero means no cached tokens or omitted.
	CachedTokens int `json:"cached_tokens"`
}

type completionTokensDetails struct {
	// ReasoningTokens is the provider-reported count of hidden reasoning tokens included in completion_tokens; zero means none or omitted.
	ReasoningTokens int `json:"reasoning_tokens"`

	// AcceptedPredictionTokens is the provider-reported count of predicted-output tokens that appeared in the completion; zero means none or omitted.
	AcceptedPredictionTokens int `json:"accepted_prediction_tokens"`

	// RejectedPredictionTokens is the provider-reported count of predicted-output tokens that did not appear in the completion; zero means none or omitted.
	RejectedPredictionTokens int `json:"rejected_prediction_tokens"`
}

type timeInfo struct {
	// QueueTime is the provider-reported time the request waited in queue before processing began; units are seconds and zero means none reported.
	QueueTime float64 `json:"queue_time"`

	// PromptTime is the provider-reported time spent processing prompt/input tokens; units are seconds and zero means none reported.
	PromptTime float64 `json:"prompt_time"`

	// CompletionTime is the provider-reported time spent generating completion/output tokens; units are seconds and zero means none reported.
	CompletionTime float64 `json:"completion_time"`

	// TotalTime is the provider-reported total request time from submission to completion; units are seconds and zero means none reported.
	TotalTime float64 `json:"total_time"`

	// Created is the provider-reported Unix timestamp when the timing was recorded; units are seconds and zero means none reported.
	Created float64 `json:"created"`
}
