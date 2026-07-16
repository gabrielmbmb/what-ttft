package huggingface

type chatCompletionRequest struct {
	// Model is the Hugging Face model identifier requested for this chat completion, optionally suffixed with a backend as owner/model:provider.
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

	// TopK is the optional top-k sampling cutoff forwarded to the routed backend; nil omits the field so the provider default applies.
	TopK *int `json:"top_k,omitempty"`

	// MinP is the optional minimum-probability sampling cutoff forwarded to the routed backend; nil omits the field so the provider default applies.
	MinP *float64 `json:"min_p,omitempty"`

	// FrequencyPenalty is the optional frequency penalty; nil omits the field so the provider default applies.
	FrequencyPenalty *float64 `json:"frequency_penalty,omitempty"`

	// PresencePenalty is the optional presence penalty; nil omits the field so the provider default applies.
	PresencePenalty *float64 `json:"presence_penalty,omitempty"`

	// RepetitionPenalty is the optional repetition penalty forwarded to the routed backend; nil omits the field so the provider default applies.
	RepetitionPenalty *float64 `json:"repetition_penalty,omitempty"`

	// Stop is the optional list of stop sequences; nil or empty omits the field.
	Stop []string `json:"stop,omitempty"`

	// Seed is the optional deterministic seed; nil omits the field.
	Seed *int64 `json:"seed,omitempty"`

	// ReasoningEffort is the optional reasoning effort for reasoning-capable models; empty omits the field, which is recommended for models that do not support it.
	ReasoningEffort string `json:"reasoning_effort,omitempty"`

	// ChatTemplateKwargs is an optional map of chat-template arguments forwarded to the routed backend, such as {"enable_thinking": false}; nil or empty omits the field.
	ChatTemplateKwargs map[string]any `json:"chat_template_kwargs,omitempty"`

	// MaxCompletionTokens is the maximum output token field; nil omits the field.
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

	// Choices is the list of streamed choice deltas in this chunk; empty is valid for usage-only chunks.
	Choices []choice `json:"choices"`

	// Usage is the optional provider-reported token usage payload, usually sent in the terminal chunk; the router sets it to null on non-terminal chunks.
	Usage *usage `json:"usage"`
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

	// Reasoning is the optional hidden reasoning delta emitted by some routed backends; empty means no reasoning delta was present and it is never counted as user-visible output.
	Reasoning string `json:"reasoning"`
}

type usage struct {
	// PromptTokens is the provider-reported input token count; zero means either zero tokens or omitted by the provider.
	PromptTokens int `json:"prompt_tokens"`

	// CompletionTokens is the provider-reported output token count; zero means either zero tokens or omitted by the provider.
	CompletionTokens int `json:"completion_tokens"`

	// TotalTokens is the provider-reported total token count; zero means either zero tokens or omitted by the provider.
	TotalTokens int `json:"total_tokens"`
}
