package whatttft

import "context"

// Provider is the common interface implemented by benchmarked AI provider adapters.
type Provider interface {
	// Name returns the normalized provider name, such as "openai"; it must not contain secrets.
	Name() string

	// Model returns the provider model identifier requested by this adapter; it must not contain API keys or credentials.
	Model() string

	// Capabilities returns the provider features supported by this adapter for benchmark planning and reporting.
	Capabilities() ProviderCapabilities

	// StreamChat sends one streaming chat request and reports provider events through obs without computing aggregate metrics itself.
	StreamChat(ctx context.Context, req ProviderRequest, obs ProviderObserver) error
}

// ProviderCapabilities describes provider protocol and metadata features available to the benchmark runner.
type ProviderCapabilities struct {
	// StreamingProtocol is the provider stream protocol name, such as "sse", "websocket", or "http-chunked"; empty means unknown.
	StreamingProtocol string `json:"streaming_protocol"`

	// SupportsChat is true when the provider adapter supports chat-style prompts for this model; false means chat support is unavailable or unknown.
	SupportsChat bool `json:"supports_chat"`

	// SupportsUsageInStream is true when token usage can be reported during or at the end of a streaming response; false means unavailable or unknown.
	SupportsUsageInStream bool `json:"supports_usage_in_stream"`

	// SupportsPromptCache is true when the provider exposes implicit prompt/KV cache metadata; false means unavailable or unknown.
	SupportsPromptCache bool `json:"supports_prompt_cache"`

	// SupportsExplicitCache is true when the provider adapter can use explicit cache-control or context-cache APIs; false means unavailable or unknown.
	SupportsExplicitCache bool `json:"supports_explicit_cache"`

	// SupportsTokenEvents is true only when the provider emits true token-level events rather than arbitrary text chunks.
	SupportsTokenEvents bool `json:"supports_token_events"`
}

// ProviderRequest contains the normalized request data passed from a runner to a provider adapter.
type ProviderRequest struct {
	// RequestID is the stable unique ID for this benchmark request attempt; it contains no secrets.
	RequestID string

	// Scenario is the scenario configuration for this attempt before provider-specific request-body translation.
	Scenario Scenario

	// Prompt is the final user prompt after cache-mode mutation; it may contain sensitive data and must not be logged by providers.
	Prompt string

	// Warmup is true when this request belongs to the warmup phase and should be excluded from default summaries.
	Warmup bool
}

// ProviderObserver is the standard hook surface providers use to report request timeline, stream, usage, cache, and HTTP events.
type ProviderObserver interface {
	// Mark records or overwrites a generic timeline event at the current monotonic time.
	Mark(EventName)

	// MarkFirst records a generic timeline event only if it has not already been recorded.
	MarkFirst(EventName)

	// MarkLast records the latest occurrence of a generic timeline event, overwriting any earlier occurrence.
	MarkLast(EventName)

	// OnStreamEvent records a raw protocol frame or event, such as one SSE data event, without marking user-visible TTFT by itself.
	OnStreamEvent(StreamEvent)

	// OnOutputDelta records a semantic model-output delta and drives first_output_delta only when the delta is visible user content.
	OnOutputDelta(OutputDelta)

	// OnToken records a true provider-emitted token event; providers must not call it for arbitrary multi-token text chunks.
	OnToken(TokenEvent)

	// OnUsage records normalized token usage metadata from a provider stream or tokenizer fallback.
	OnUsage(ProviderUsage)

	// OnCache records normalized prompt/KV cache metadata with any provider-specific secrets redacted.
	OnCache(CacheRecord)

	// OnFinish records provider finish metadata, including terminal stream events when available.
	OnFinish(FinishEvent)

	// OnHTTP records normalized HTTP status, protocol, connection reuse, TLS, and transport metadata.
	OnHTTP(HTTPRecord)
}

// StreamEvent records one raw provider stream/protocol event.
type StreamEvent struct {
	// RequestID is the stable request ID this event belongs to; it contains no secrets and joins to RequestRecord.RequestID.
	RequestID string `json:"request_id"`

	// Index is the zero-based stream event index within the request; zero is the first observed event and is not a missing value.
	Index int `json:"index"`

	// Protocol is the protocol name for this event, such as "sse"; empty means unknown.
	Protocol string `json:"protocol"`

	// AtNS is the event observation time in nanoseconds relative to request_start; zero means the event was observed at request_start or not populated.
	AtNS int64 `json:"at_ns"`

	// RawBytes is the byte count of the raw protocol frame or event; zero is valid for synthetic events and units are bytes.
	RawBytes int `json:"raw_bytes"`

	// DataBytes is the byte count of the protocol data payload; zero is valid for empty events and units are bytes.
	DataBytes int `json:"data_bytes"`

	// Empty is true when the event has no data payload after protocol parsing; false means data may still be metadata rather than visible output.
	Empty bool `json:"empty"`

	// Terminal is true when the event is a provider stream terminator such as [DONE]; false means it is not a terminal event.
	Terminal bool `json:"terminal"`
}

// OutputDelta records one semantic model-output delta identified by a provider adapter.
type OutputDelta struct {
	// RequestID is the stable request ID this delta belongs to; it contains no secrets and joins to RequestRecord.RequestID.
	RequestID string `json:"request_id"`

	// Index is the zero-based semantic delta index within the request; zero is the first observed delta and is not a missing value.
	Index int `json:"index"`

	// AtNS is the delta observation time in nanoseconds relative to request_start; zero means the delta was observed at request_start or not populated.
	AtNS int64 `json:"at_ns"`

	// Text is user-visible output text for this delta; empty means the delta contained no visible text or text was redacted.
	Text string `json:"text,omitempty"`

	// Role is the provider role delta, such as "assistant"; empty means no role delta was present.
	Role string `json:"role,omitempty"`

	// Modality is the output modality, such as "text" for v0.1; empty means the provider did not classify the modality.
	Modality string `json:"modality"`

	// Visible is true when Text is user-visible output and should drive TTFT/E2E delta metrics; false means role/tool/metadata output.
	Visible bool `json:"visible"`

	// FinishReason is the provider-reported finish reason associated with this delta; empty means no finish reason was present.
	FinishReason string `json:"finish_reason,omitempty"`
}

// TokenEvent records one true token-level output event when a provider exposes token timestamps directly.
type TokenEvent struct {
	// RequestID is the stable request ID this token belongs to; it contains no secrets and joins to RequestRecord.RequestID.
	RequestID string `json:"request_id"`

	// Index is the zero-based token index within the request output; zero is the first observed token and is not a missing value.
	Index int `json:"index"`

	// AtNS is the token observation time in nanoseconds relative to request_start; zero means the token was observed at request_start or not populated.
	AtNS int64 `json:"at_ns"`

	// Text is the token text as exposed by the provider; empty means the provider redacted or emitted an empty token.
	Text string `json:"text,omitempty"`
}

// FinishEvent records provider finish metadata for one streaming request.
type FinishEvent struct {
	// RequestID is the stable request ID this finish event belongs to; it contains no secrets and joins to RequestRecord.RequestID.
	RequestID string `json:"request_id"`

	// AtNS is the finish observation time in nanoseconds relative to request_start; zero means the finish was observed at request_start or not populated.
	AtNS int64 `json:"at_ns"`

	// FinishReason is the provider-reported reason generation stopped; empty means no reason was reported.
	FinishReason string `json:"finish_reason,omitempty"`

	// Terminal is true when this event corresponds to a terminal stream marker; false means the stream may continue or termination was not explicit.
	Terminal bool `json:"terminal"`
}
