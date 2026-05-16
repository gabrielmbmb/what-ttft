package whatttft

// EventName is the stable JSON name for a timestamped request lifecycle event.
type EventName string

const (
	// EventScheduledAt records when the benchmark intended to start a request and may be negative relative to request_start.
	EventScheduledAt EventName = "scheduled_at"

	// EventRequestStart records the monotonic origin immediately before the provider request is started and is normally zero nanoseconds.
	EventRequestStart EventName = "request_start"

	// EventDNSStart records when DNS resolution starts for a request, when observable by net/http tracing.
	EventDNSStart EventName = "dns_start"

	// EventDNSDone records when DNS resolution completes for a request, when observable by net/http tracing.
	EventDNSDone EventName = "dns_done"

	// EventConnectStart records when TCP connection establishment starts for a request, when observable by net/http tracing.
	EventConnectStart EventName = "connect_start"

	// EventConnectDone records when TCP connection establishment completes for a request, when observable by net/http tracing.
	EventConnectDone EventName = "connect_done"

	// EventTLSStart records when TLS handshake negotiation starts for a request, when observable by net/http tracing.
	EventTLSStart EventName = "tls_start"

	// EventTLSDone records when TLS handshake negotiation completes for a request, when observable by net/http tracing.
	EventTLSDone EventName = "tls_done"

	// EventGotConn records when the HTTP client obtains a connection and connection reuse metadata is available.
	EventGotConn EventName = "got_conn"

	// EventWroteRequest records when the HTTP client finishes writing request headers and body, when observable by net/http tracing.
	EventWroteRequest EventName = "wrote_request"

	// EventFirstResponseByte records when the first response byte arrives from the server and drives HTTP TTFB.
	EventFirstResponseByte EventName = "first_response_byte"

	// EventHeadersReceived records when response headers are available to provider code after client.Do returns.
	EventHeadersReceived EventName = "headers_received"

	// EventFirstSSEEvent records the first Server-Sent Events data frame, including empty or role-only events, and does not drive TTFT.
	EventFirstSSEEvent EventName = "first_sse_event"

	// EventFirstOutputDelta records the first non-empty user-visible output delta and drives TTFT for streaming text providers.
	EventFirstOutputDelta EventName = "first_output_delta"

	// EventFirstOutputToken records the first newly observed output token when a provider emits true token-level timestamps.
	EventFirstOutputToken EventName = "first_output_token"

	// EventLastOutputDelta records the last non-empty user-visible output delta and drives delta-based end-to-end latency.
	EventLastOutputDelta EventName = "last_output_delta"

	// EventLastOutputToken records the last newly observed output token when a provider emits true token-level timestamps.
	EventLastOutputToken EventName = "last_output_token"

	// EventDone records the provider stream terminator, such as an OpenAI-compatible [DONE] event.
	EventDone EventName = "done_event"

	// EventBodyEOF records when the response body is fully read and closed.
	EventBodyEOF EventName = "body_eof"
)

// Timeline stores monotonic-relative event times for one request.
type Timeline struct {
	// BaseWallUnixNano is the wall-clock Unix nanosecond timestamp corresponding to request_start; zero means the base wall time was not recorded.
	BaseWallUnixNano int64 `json:"base_wall_unix_nano"`

	// EventsNS maps event names to nanoseconds relative to request_start using monotonic-duration calculations; missing keys mean the event was not observed.
	EventsNS map[EventName]int64 `json:"events_ns"`
}
