package whatttft

// RequestRecord is the machine-readable result for one attempted provider request.
type RequestRecord struct {
	// RequestID is a stable unique ID for this benchmark request attempt; it contains no secrets and is required for joining chunk records.
	RequestID string `json:"request_id"`

	// TargetID is a stable sanitized benchmark target identifier for multi-target runs; empty means no target dimension was configured and the value must not contain secrets.
	TargetID string `json:"target_id,omitempty"`

	// TargetName is a human-readable benchmark target label for reports; empty means no target label was configured and the value must not contain secrets.
	TargetName string `json:"target_name,omitempty"`

	// Provider is the normalized provider name, such as "openai"; it contains no secrets and is used for grouping summaries.
	Provider string `json:"provider"`

	// Model is the provider model identifier requested for this attempt; it contains no secrets unless a provider embeds sensitive deployment names.
	Model string `json:"model"`

	// ScenarioName is the scenario grouping label copied from Scenario.Name; empty means the run used an unnamed scenario.
	ScenarioName string `json:"scenario_name"`

	// Warmup is true when this request is excluded from default measured summaries; false means the request is part of the measured phase.
	Warmup bool `json:"warmup"`

	// Attempt is the zero-based request attempt index within the run; zero is the first request and is not a missing value.
	Attempt int `json:"attempt"`

	// CacheMode is the requested prompt/KV cache behavior for this request; summaries must not mix different cache modes.
	CacheMode CacheMode `json:"cache_mode"`

	// ConnectionMode is the requested HTTP connection reuse behavior for this request; summaries must not mix different connection modes.
	ConnectionMode ConnectionMode `json:"connection_mode"`

	// RequestedServiceTier is the provider service tier requested for this request, such as OpenAI auto, default, flex, scale, or priority; empty means no tier was requested and the value is not secret.
	RequestedServiceTier string `json:"requested_service_tier,omitempty"`

	// ObservedServiceTier is the provider-reported actual service tier used for this request; empty means the provider did not report it and the value is not secret.
	ObservedServiceTier string `json:"observed_service_tier,omitempty"`

	// PromptHash is the SHA-256 hex digest of the final prompt after cache-mode mutation; it is used instead of storing prompt text in request records.
	PromptHash string `json:"prompt_hash"`

	// PromptTokens is the provider-reported input token count for this request, or nil when unavailable; units are tokens.
	PromptTokens *int `json:"prompt_tokens,omitempty"`

	// CompletionTokens is the provider-reported output token count for this request, or nil when unavailable; units are tokens.
	CompletionTokens *int `json:"completion_tokens,omitempty"`

	// TotalTokens is the provider-reported total token count for this request, or nil when unavailable; units are tokens.
	TotalTokens *int `json:"total_tokens,omitempty"`

	// OutputDeltaCount is the count of non-empty user-visible output deltas observed in the stream; units are deltas, zero means none were observed or the request failed before visible output.
	OutputDeltaCount int `json:"output_delta_count,omitempty"`

	// Cache is normalized provider prompt/KV cache metadata; empty fields mean the provider did not expose cache details.
	Cache CacheRecord `json:"cache"`

	// HTTP is normalized client-observed HTTP and connection metadata; zero values mean the corresponding phase was not observed.
	HTTP HTTPRecord `json:"http"`

	// Timeline contains monotonic-relative request lifecycle timestamps in nanoseconds; missing events mean the phase was not observed.
	Timeline Timeline `json:"timeline"`

	// Derived contains latency and throughput metrics calculated from Timeline and token counts; nil metric fields mean inputs were missing.
	Derived DerivedMetrics `json:"derived"`

	// Error describes a failed request with redacted details, or nil when the provider request completed successfully.
	Error *ErrorRecord `json:"error,omitempty"`
}

// ChunkRecord is the optional machine-readable record for one raw stream chunk or semantic output chunk.
type ChunkRecord struct {
	// RequestID is the stable request ID this chunk belongs to; it contains no secrets and joins to RequestRecord.RequestID.
	RequestID string `json:"request_id"`

	// Index is the zero-based chunk index within the request stream; zero is the first observed chunk and is not a missing value.
	Index int `json:"index"`

	// AtNS is the chunk observation time in nanoseconds relative to request_start; zero means the chunk was observed at request_start or the caller did not populate it.
	AtNS int64 `json:"at_ns"`

	// SSEDataBytes is the byte count of the SSE data payload for this chunk; zero is valid for empty events and units are bytes.
	SSEDataBytes int `json:"sse_data_bytes"`

	// Content is the user-visible output text carried by this chunk; empty means no visible text and non-empty values may contain sensitive generated content.
	Content string `json:"content,omitempty"`

	// Role is the provider role delta carried by this chunk, such as "assistant"; empty means no role delta was present.
	Role string `json:"role,omitempty"`

	// FinishReason is the provider-reported finish reason associated with this chunk; empty means no finish reason was present.
	FinishReason string `json:"finish_reason,omitempty"`

	// Empty is true when the provider event carried no user-visible content; false means content may still be absent for role or finish-only chunks.
	Empty bool `json:"empty"`

	// UsageChunk is true when this chunk represented provider usage metadata instead of model output; false means it was not a usage-only chunk.
	UsageChunk bool `json:"usage_chunk"`
}

// CacheRecord is normalized prompt/KV cache metadata observed or requested for one provider request.
type CacheRecord struct {
	// Hit is the provider-reported cache-hit indicator, or nil when the provider did not expose a hit/miss boolean.
	Hit *bool `json:"hit,omitempty"`

	// PromptCachedTokens is the provider-reported count of input tokens served from a prompt/KV cache, or nil when unavailable; units are tokens.
	PromptCachedTokens *int `json:"prompt_cached_tokens,omitempty"`

	// CacheReadTokens is the provider-reported count of tokens read from an explicit cache, or nil when unavailable; units are tokens.
	CacheReadTokens *int `json:"cache_read_tokens,omitempty"`

	// CacheCreationTokens is the provider-reported count of tokens written to an explicit cache, or nil when unavailable; units are tokens.
	CacheCreationTokens *int `json:"cache_creation_tokens,omitempty"`

	// CacheID is a redacted provider cache identifier when explicit caching is used; empty means no cache identifier was reported or it was fully redacted.
	CacheID string `json:"cache_id,omitempty"`

	// CacheTTLSeconds is the provider-reported cache time-to-live, or nil when unavailable; units are seconds.
	CacheTTLSeconds *int64 `json:"cache_ttl_seconds,omitempty"`

	// Extra contains provider-specific JSON-compatible cache metadata with secrets redacted; nil or empty means no extra cache metadata was recorded.
	Extra map[string]any `json:"extra,omitempty"`
}

// HTTPRecord is normalized HTTP response, transport, and response-header metadata for one provider request.
type HTTPRecord struct {
	// StatusCode is the HTTP response status code observed after headers arrive; zero means no response status was available.
	StatusCode int `json:"status_code"`

	// Status is the HTTP response status text observed after headers arrive, such as "200 OK"; empty means no response status was available.
	Status string `json:"status,omitempty"`

	// Protocol is the HTTP protocol negotiated for the response, such as "HTTP/2.0"; empty means it was not observed.
	Protocol string `json:"protocol,omitempty"`

	// ProviderProcessingMS is the provider-reported server-side request processing duration parsed from response metadata such as openai-processing-ms; units are milliseconds, nil means unavailable or unparseable, and values are provider-reported rather than client-observed.
	ProviderProcessingMS *float64 `json:"provider_processing_ms,omitempty"`

	// ServerTiming is the provider-reported server-side latency breakdown parsed from the streaming response body, such as Cerebras time_info; nil means the provider did not report it and all durations are provider-reported rather than client-observed.
	ServerTiming *ServerTiming `json:"server_timing,omitempty"`

	// RequestedServiceTier is the provider service tier requested for this HTTP request; empty means unset, values are provider labels such as OpenAI default or priority, and no redaction is required.
	RequestedServiceTier string `json:"requested_service_tier,omitempty"`

	// ObservedServiceTier is the provider-reported actual service tier for this HTTP response or stream; empty means unavailable and no redaction is required.
	ObservedServiceTier string `json:"observed_service_tier,omitempty"`

	// Network is the network name reported by httptrace ConnectStart, such as "tcp"; empty means connection setup was not observed.
	Network string `json:"network,omitempty"`

	// RemoteAddr is the remote host:port reported by httptrace ConnectStart; it must not include credentials and empty means it was not observed.
	RemoteAddr string `json:"remote_addr,omitempty"`

	// DNSAddrs is the count of DNS addresses returned by httptrace DNSDone; zero means none were returned or DNS was not observed.
	DNSAddrs int `json:"dns_addrs,omitempty"`

	// DNSError is a redacted DNS-resolution error string from httptrace DNSDone; empty means no DNS error was observed.
	DNSError string `json:"dns_error,omitempty"`

	// ConnectError is a redacted TCP-connection error string from httptrace ConnectDone; empty means no connect error was observed.
	ConnectError string `json:"connect_error,omitempty"`

	// TLSError is a redacted TLS-handshake error string from httptrace TLSHandshakeDone; empty means no TLS error was observed.
	TLSError string `json:"tls_error,omitempty"`

	// WriteError is a redacted request-write error string from httptrace WroteRequest; empty means no write error was observed.
	WriteError string `json:"write_error,omitempty"`

	// GotConn is true when httptrace reported connection acquisition; false means connection metadata may be unavailable.
	GotConn bool `json:"got_conn"`

	// ConnReused is true when httptrace reported a reused connection; false means a new connection was used or GotConn is false.
	ConnReused bool `json:"conn_reused"`

	// ConnWasIdle is true when the reused connection had been idle before the request; false means it was active, new, or GotConn is false.
	ConnWasIdle bool `json:"conn_was_idle"`

	// ConnIdleTimeNS is the connection idle duration reported by httptrace GotConnInfo; units are nanoseconds and zero means not idle or unavailable.
	ConnIdleTimeNS int64 `json:"conn_idle_time_ns"`

	// TLSVersion is the negotiated TLS protocol version, such as "TLS 1.3"; empty means TLS was not used or not observed.
	TLSVersion string `json:"tls_version,omitempty"`

	// CompressionDisabled is true when the benchmark transport disabled automatic response compression; false means compression may be enabled or unknown.
	CompressionDisabled bool `json:"compression_disabled"`
}

// ServerTiming is a provider-reported server-side latency breakdown for one provider request, such as the Cerebras time_info block; all durations are provider-reported milliseconds converted from the provider's reported seconds, and nil fields mean the provider omitted that phase.
type ServerTiming struct {
	// QueueTimeMS is the provider-reported time the request waited in the provider queue before processing began, or nil when omitted; units are milliseconds and the value is provider-reported rather than client-observed.
	QueueTimeMS *float64 `json:"queue_time_ms,omitempty"`

	// PromptTimeMS is the provider-reported time spent processing prompt/input tokens (prefill), or nil when omitted; units are milliseconds and the value is provider-reported rather than client-observed.
	PromptTimeMS *float64 `json:"prompt_time_ms,omitempty"`

	// CompletionTimeMS is the provider-reported time spent generating completion/output tokens (decode), or nil when omitted; units are milliseconds and the value is provider-reported rather than client-observed.
	CompletionTimeMS *float64 `json:"completion_time_ms,omitempty"`

	// TotalTimeMS is the provider-reported total server-side request time from submission to completion, or nil when omitted; units are milliseconds, the value is provider-reported rather than client-observed, and it may include provider-side network and buffering not captured by the phase fields.
	TotalTimeMS *float64 `json:"total_time_ms,omitempty"`
}

// ProviderUsage is normalized token-usage metadata reported by a provider or estimated by a tokenizer fallback.
type ProviderUsage struct {
	// PromptTokens is the input token count, or nil when unavailable; units are tokens and Source identifies whether it is provider-reported or estimated.
	PromptTokens *int `json:"prompt_tokens,omitempty"`

	// CompletionTokens is the output token count, or nil when unavailable; units are tokens and Source identifies whether it is provider-reported or estimated.
	CompletionTokens *int `json:"completion_tokens,omitempty"`

	// TotalTokens is the total token count, or nil when unavailable; units are tokens and Source identifies whether it is provider-reported or estimated.
	TotalTokens *int `json:"total_tokens,omitempty"`

	// Source describes the token-count source, such as "provider-reported" or "estimated"; empty means the source was not recorded.
	Source string `json:"source,omitempty"`

	// Extra contains provider-specific JSON-compatible usage metadata with secrets redacted; nil or empty means no extra usage metadata was recorded.
	Extra map[string]any `json:"extra,omitempty"`
}

// ErrorRecord describes a failed request using bounded, redacted diagnostic fields.
type ErrorRecord struct {
	// Category is a stable error grouping label, such as "http_status", "stream_decode", or "context"; empty means uncategorized.
	Category string `json:"category"`

	// Message is a redacted human-readable error message; empty means no message was recorded.
	Message string `json:"message"`

	// StatusCode is the HTTP response status code associated with the error; zero means no HTTP status was available.
	StatusCode int `json:"status_code,omitempty"`

	// Retryable is true when the error appears transient; false means non-retryable or unknown.
	Retryable bool `json:"retryable"`

	// AtNS is the error observation time in nanoseconds relative to request_start; zero means request_start or unavailable.
	AtNS int64 `json:"at_ns,omitempty"`

	// BodySnippet is a bounded, redacted response-body excerpt for provider errors; empty means no body snippet was recorded.
	BodySnippet string `json:"body_snippet,omitempty"`
}

// DerivedMetrics contains latency and throughput metrics calculated from request timeline events.
type DerivedMetrics struct {
	// HTTPTTFBMS is first_response_byte minus request_start in milliseconds, or nil when either event is missing.
	HTTPTTFBMS *float64 `json:"http_ttfb_ms,omitempty"`

	// HeadersLatencyMS is headers_received minus request_start in milliseconds, or nil when headers_received is missing.
	HeadersLatencyMS *float64 `json:"headers_latency_ms,omitempty"`

	// FirstEventMS is first_sse_event minus request_start in milliseconds, or nil when first_sse_event is missing.
	FirstEventMS *float64 `json:"first_event_ms,omitempty"`

	// TTFTDeltaMS is first_output_delta minus request_start in milliseconds, or nil when first_output_delta is missing.
	TTFTDeltaMS *float64 `json:"ttft_delta_ms,omitempty"`

	// E2EDeltaMS is last_output_delta minus request_start in milliseconds, or nil when last_output_delta is missing.
	E2EDeltaMS *float64 `json:"e2e_delta_ms,omitempty"`

	// StreamTotalMS is body_eof minus request_start in milliseconds, or nil when body_eof is missing.
	StreamTotalMS *float64 `json:"stream_total_ms,omitempty"`

	// GenerationDeltaMS is last_output_delta minus first_output_delta in milliseconds, or nil when either event is missing.
	GenerationDeltaMS *float64 `json:"generation_delta_ms,omitempty"`

	// E2EOutputTPS is completion tokens divided by e2e_delta seconds, or nil when completion tokens or e2e_delta are unavailable; units are tokens/second and this user-perceived metric includes TTFT.
	E2EOutputTPS *float64 `json:"e2e_output_tps,omitempty"`

	// GenerationDeltaOutputTPS is max(completion tokens minus one, zero) divided by generation_delta seconds, or nil when completion tokens, first_output_delta, last_output_delta, a positive generation duration, enough visible deltas, or a long enough observation window are unavailable; units are tokens/second, timing bounds are visible-output delta timestamps rather than true token timestamps, short buffered responses are intentionally omitted, and this must not be interpreted as decode_tps.
	GenerationDeltaOutputTPS *float64 `json:"generation_delta_output_tps,omitempty"`

	// ServerWaitToFirstByteMS is first_response_byte minus wrote_request in milliseconds, or nil when either event is missing.
	ServerWaitToFirstByteMS *float64 `json:"server_wait_to_first_byte_ms,omitempty"`

	// StreamProtocolToFirstOutputMS is first_output_delta minus first_response_byte in milliseconds, or nil when either event is missing.
	StreamProtocolToFirstOutputMS *float64 `json:"stream_protocol_to_first_output_ms,omitempty"`

	// DNSMS is dns_done minus dns_start in milliseconds, or nil when either DNS event is missing.
	DNSMS *float64 `json:"dns_ms,omitempty"`

	// TCPConnectMS is connect_done minus connect_start in milliseconds, or nil when either connect event is missing.
	TCPConnectMS *float64 `json:"tcp_connect_ms,omitempty"`

	// TLSMS is tls_done minus tls_start in milliseconds, or nil when either TLS event is missing.
	TLSMS *float64 `json:"tls_ms,omitempty"`

	// RequestWriteMS is wrote_request minus got_conn in milliseconds, or nil when either event is missing.
	RequestWriteMS *float64 `json:"request_write_ms,omitempty"`
}
