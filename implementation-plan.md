# Implementation Plan: `what-ttft`

This file is the source-of-truth TODO list for current and completed `what-ttft` milestones. The active milestone is v0.4: request exploration inside the existing Bubble Tea terminal dashboards. Completed v0.1/v0.2/v0.3 plans are retained below for context and regression guardrails.

## Agent execution instructions

Implement exactly one TODO task at a time. After completing that task, update this file to mark the task as done, commit the changes, and then print a detailed summary of what you implemented, including files changed, validation commands run, and any follow-up work.

Historical v0.1 goal, kept for context: a Go library plus a small CLI that benchmarks OpenAI-compatible streaming endpoints and reports client-observed latency allocation: HTTP/network timing, first response byte, first SSE event, first non-empty output delta, end-to-end latency, chunk cadence, token usage, cache metadata, and aggregate percentiles.

Historical v0.1 non-goals:

- OpenAI Responses API support. Keep architecture ready for it, but do not implement it yet.
- STT/TTS benchmarking.
- Provider SDK usage in the hot path.
- Perfect provider-side attribution. Client-side timing cannot separate remote queueing vs prefill vs provider buffering unless provider exposes those metrics.
- True token-by-token timestamps when providers emit multi-token chunks. v0.1 reports chunk cadence and provider token usage; token timestamp estimation can be added later.

## Baseline architecture

Use a public package for the reusable benchmark API and provider packages for integrations. Keep low-level helpers, CLI event fanout, and Bubble Tea rendering internal.

```text
cmd/what-ttft/                  CLI entrypoint for run and bench
pkg/whatttft/                   Public runner, config, scenario, records, metrics, events
pkg/provider/openai/            Public OpenAI-compatible Responses and Chat Completions provider
internal/configfile/            Strict YAML benchmark config loader
internal/eventbus/              v0.3 planned bounded CLI event fanout
internal/tui/                   v0.3 planned Bubble Tea live dashboard
internal/httptracecap/          HTTP client trace capture
internal/sse/                   Server-Sent Events parser
internal/stats/                 Percentiles and summary statistics
internal/report/                JSONL/JSON/Markdown writers
internal/testserver/            Deterministic httptest helpers
```

The CLI should be thin. Most behavior should be accessible from Go code:

```go
provider := openai.New(openai.Config{
    BaseURL:   "https://api.openai.com/v1",
    APIKey:    os.Getenv("OPENAI_API_KEY"),
    Model:     "gpt-5.5",
})

runner := whatttft.NewRunner(provider, whatttft.RunConfig{
    Scenario: whatttft.Scenario{
        Name:   "realtime-short",
        Prompt: "Answer in one short sentence: what is the capital of France?",
        MaxOutputTokens: 64,
        Temperature: ptr(0.0),
    },
    WarmupRequests:   5,
    MeasuredRequests: 50,
    Concurrency:      1,
    CacheMode:        whatttft.CacheBust,
    ConnectionMode:   whatttft.WarmConnections,
})

result, err := runner.Run(ctx)
```

The CLI should call the same API:

```sh
what-ttft run \
  --provider openai \
  --model gpt-5.5 \
  --api-key-env OPENAI_API_KEY \
  --prompt 'Answer in one short sentence: what is the capital of France?' \
  --samples 50 \
  --warmup 5 \
  --concurrency 1 \
  --cache-mode cache-bust \
  --connection-mode warm \
  --max-output-tokens 64 \
  --temperature 0 \
  --out runs/openai-gpt-5.5-short-cold
```

---

## Documentation rule for all tasks

All exported Go symbols must have Go doc comments. In addition, every field in exported structs and every field serialized to JSON must have an adjacent field comment documenting units, nil/zero semantics, whether the value is observed/estimated/provider-reported, and redaction requirements. Some snippets below omit comments to stay focused on shape; the actual implementation must satisfy `AGENTS.md` and `.golangci.yml`.

## TODO list

### [x] 0. Set up GitHub CI

Create a GitHub Actions workflow that gates changes in the same order contributors should run checks locally.

Implemented details:

- Workflow file: `.github/workflows/ci.yml`.
- Triggered on pushes to `main`/`master`, pull requests, and manual `workflow_dispatch`.
- Uses read-only repository permissions.
- Runs jobs in order:
  1. lint with `golangci/golangci-lint-action`;
  2. tests with `go test ./...` and `go test -race ./...`;
  3. final build with `go build ./...`.
- Uses `actions/setup-go` with `go-version-file: go.mod` so CI follows the repository Go version.

Definition of done:

- Workflow exists and references the repository `.golangci.yml`.
- Local equivalents pass: `go test ./...`, `go test -race ./...`, `golangci-lint run`, and `go build ./...`.

---

### [x] 1. Bootstrap Go module and repository skeleton

Create the initial Go module, directory structure, and minimal build scaffolding.

Implementation details:

- Initialize the module:

  ```sh
  go mod init github.com/gabrielmbmb/what-ttft
  ```

- Create directories:

  ```text
  cmd/what-ttft/
  pkg/whatttft/
  pkg/provider/openai/
  internal/httptracecap/
  internal/sse/
  internal/stats/
  internal/report/
  internal/testserver/
  ```

- Add a minimal `cmd/what-ttft/main.go` that prints usage or supports `--help`.
- Add a minimal package doc in `pkg/whatttft/doc.go`.
- Keep dependencies at zero for this task. Use only the Go standard library.
- Confirm `.golangci.yml` is used by the repo.

Definition of done:

- `go test ./...` succeeds.
- `golangci-lint run` succeeds.
- Running `go run ./cmd/what-ttft --help` succeeds, even if it only prints placeholder usage.

---

### [x] 2. Define public domain model and result schema

Implement the public types that all packages will share. This must happen before provider code so the provider emits standardized events and records.

Files:

- `pkg/whatttft/config.go`
- `pkg/whatttft/scenario.go`
- `pkg/whatttft/timeline.go`
- `pkg/whatttft/records.go`
- `pkg/whatttft/provider.go`

Core types:

```go
package whatttft

type CacheMode string

const (
    CacheBust             CacheMode = "cache-bust"
    CacheReuse            CacheMode = "cache-reuse"
    ProviderExplicitCache CacheMode = "provider-explicit-cache"
    CacheUnknown          CacheMode = "unknown"
)

type ConnectionMode string

const (
    WarmConnections ConnectionMode = "warm"
    ColdConnections ConnectionMode = "cold"
)

type Scenario struct {
    Name            string
    Prompt          string
    SystemPrompt    string
    MaxOutputTokens int
    Temperature     *float64
    TopP            *float64
    Stop            []string
    Seed            *int64
    Extra           map[string]any
}

type RunConfig struct {
    Scenario         Scenario
    WarmupRequests   int
    MeasuredRequests int
    Concurrency      int
    CacheMode        CacheMode
    ConnectionMode   ConnectionMode
    OutputDir        string
    SaveChunks       bool
}
```

Timeline representation:

- Store wall-clock base time for human inspection.
- Store all event timings as nanoseconds relative to `request_start` so JSON is stable and monotonic-duration safe.
- Do not JSON-marshal `time.Time` values that depend on monotonic internals for duration math.

```go
type EventName string

const (
    EventScheduledAt       EventName = "scheduled_at"
    EventRequestStart      EventName = "request_start"
    EventDNSStart          EventName = "dns_start"
    EventDNSDone           EventName = "dns_done"
    EventConnectStart      EventName = "connect_start"
    EventConnectDone       EventName = "connect_done"
    EventTLSStart          EventName = "tls_start"
    EventTLSDone           EventName = "tls_done"
    EventGotConn           EventName = "got_conn"
    EventWroteRequest      EventName = "wrote_request"
    EventFirstResponseByte EventName = "first_response_byte"
    EventHeadersReceived   EventName = "headers_received"
    EventFirstSSEEvent     EventName = "first_sse_event"
    EventFirstOutputDelta  EventName = "first_output_delta"
    EventLastOutputDelta   EventName = "last_output_delta"
    EventDone             EventName = "done_event"
    EventBodyEOF           EventName = "body_eof"
)

type Timeline struct {
    BaseWallUnixNano int64            `json:"base_wall_unix_nano"`
    EventsNS         map[EventName]int64 `json:"events_ns"`
}
```

Request and chunk records:

```go
type RequestRecord struct {
    RequestID       string            `json:"request_id"`
    Provider        string            `json:"provider"`
    Model           string            `json:"model"`
    ScenarioName    string            `json:"scenario_name"`
    Warmup          bool              `json:"warmup"`
    Attempt         int               `json:"attempt"`
    CacheMode       CacheMode         `json:"cache_mode"`
    ConnectionMode  ConnectionMode    `json:"connection_mode"`
    PromptHash      string            `json:"prompt_hash"`
    PromptTokens    *int              `json:"prompt_tokens,omitempty"`
    CompletionTokens *int             `json:"completion_tokens,omitempty"`
    TotalTokens     *int              `json:"total_tokens,omitempty"`
    Cache           CacheRecord       `json:"cache"`
    HTTP            HTTPRecord        `json:"http"`
    Timeline        Timeline          `json:"timeline"`
    Derived         DerivedMetrics    `json:"derived"`
    Error           *ErrorRecord      `json:"error,omitempty"`
}

type ChunkRecord struct {
    RequestID       string `json:"request_id"`
    Index           int    `json:"index"`
    AtNS            int64  `json:"at_ns"`
    SSEDataBytes    int    `json:"sse_data_bytes"`
    Content         string `json:"content,omitempty"`
    Role            string `json:"role,omitempty"`
    FinishReason    string `json:"finish_reason,omitempty"`
    Empty           bool   `json:"empty"`
    UsageChunk      bool   `json:"usage_chunk"`
}
```

Provider interface and standard instrumentation hooks:

Every provider must use the same observer/hook interface. This is critical: providers should translate their native protocol events into standardized benchmark events, while the runner/metrics packages own timestamping and derived metric math. Provider packages should not invent provider-specific metric names for common concepts like TTFT, TTFB, E2E latency, usage, or cache hits.

```go
type Provider interface {
    Name() string
    Model() string
    Capabilities() ProviderCapabilities
    StreamChat(ctx context.Context, req ProviderRequest, obs ProviderObserver) error
}

type ProviderCapabilities struct {
    StreamingProtocol     string // "sse", "websocket", "http-chunked", etc.
    SupportsChat          bool
    SupportsUsageInStream bool
    SupportsPromptCache   bool
    SupportsExplicitCache bool
    SupportsTokenEvents   bool // true only if provider emits token-level events, not arbitrary chunks
}

type ProviderRequest struct {
    RequestID string
    Scenario  Scenario
    Prompt    string // after cache-busting/reuse mutation
    Warmup    bool
}

// ProviderObserver is the standard provider hook surface. Implementations are
// concurrency-safe and timestamp events at the call site using a monotonic clock.
// Providers should call these hooks as close as possible to observing the event:
// after a stream frame is read, before expensive JSON parsing when possible, and
// immediately when a semantic output delta is identified.
type ProviderObserver interface {
    // Generic timeline events. Transport code also uses these for httptrace events.
    Mark(EventName)
    MarkFirst(EventName)
    MarkLast(EventName)

    // Raw stream/protocol events. For OpenAI Chat Completions this is one SSE data event.
    OnStreamEvent(StreamEvent)

    // Semantic model-output events. These drive TTFT/E2E metrics.
    OnOutputDelta(OutputDelta)
    OnToken(TokenEvent) // only when provider emits true token-level events; otherwise do not call

    // Provider metadata surfaced during or after the stream.
    OnUsage(ProviderUsage)
    OnCache(CacheRecord)
    OnFinish(FinishEvent)
    OnHTTP(HTTPRecord)
}

type StreamEvent struct {
    RequestID    string `json:"request_id"`
    Index        int    `json:"index"`
    Protocol     string `json:"protocol"` // e.g. "sse"
    AtNS         int64  `json:"at_ns"`
    RawBytes     int    `json:"raw_bytes"`
    DataBytes    int    `json:"data_bytes"`
    Empty        bool   `json:"empty"`
    Terminal     bool   `json:"terminal"`
}

type OutputDelta struct {
    RequestID    string `json:"request_id"`
    Index        int    `json:"index"`
    AtNS         int64  `json:"at_ns"`
    Text         string `json:"text,omitempty"`
    Role         string `json:"role,omitempty"`
    Modality     string `json:"modality"` // "text" for v0.1
    Visible      bool   `json:"visible"`  // false for role-only/tool/metadata deltas
    FinishReason string `json:"finish_reason,omitempty"`
}

type TokenEvent struct {
    RequestID string `json:"request_id"`
    Index     int    `json:"index"`
    AtNS      int64  `json:"at_ns"`
    Text      string `json:"text,omitempty"`
}

type FinishEvent struct {
    RequestID    string `json:"request_id"`
    AtNS         int64  `json:"at_ns"`
    FinishReason string `json:"finish_reason,omitempty"`
    Terminal     bool   `json:"terminal"`
}
```

Hook semantics:

- `OnStreamEvent` records protocol frames/events and drives `first_sse_event` for SSE providers. It does **not** drive TTFT by itself.
- `OnOutputDelta` records semantic provider output. The first `Visible=true` text delta drives `first_output_delta`; later visible deltas update `last_output_delta`.
- `OnToken` is reserved for real token-level events. Do not call it for arbitrary text chunks. If a provider emits multi-token chunks, record them as `OnOutputDelta` only.
- `OnUsage` normalizes provider token counts, including stream usage chunks.
- `OnCache` normalizes prompt/KV cache information, including cached prompt tokens and explicit cache IDs.
- `OnHTTP` stores HTTP status, protocol, connection reuse, TLS, and other transport data captured by shared HTTP tracing.
- Providers may include raw provider-specific metadata in `Extra map[string]any` fields, but summaries must group by standardized fields first.

Implemented details:

- Added public run configuration, scenario, cache mode, connection mode, timeline, request record, chunk record, cache, HTTP, usage, error, and derived metric schema types under `pkg/whatttft`.
- Added the shared provider interface, provider capabilities, provider request, observer hooks, and standardized stream/output/token/finish event types.
- Documented all exported symbols and JSON fields with units, nil/zero semantics, and redaction notes where relevant.
- Added JSON round-trip tests for request records, chunk records, and provider event shapes.

Definition of done:

- Public types compile and have basic tests for JSON marshaling shape.
- Comments document cache mode and connection mode semantics.
- No provider-specific fields leak into generic types except through generic metadata maps or normalized cache/usage records.

---

### [x] 3. Implement monotonic timeline recorder and clock abstraction

Create a small timing utility that ensures every duration is calculated using Go monotonic time.

Files:

- `pkg/whatttft/clock.go`
- `pkg/whatttft/recorder.go`

Design:

```go
type Clock interface {
    Now() time.Time
}

type RealClock struct{}

func (RealClock) Now() time.Time { return time.Now() }

type Recorder struct {
    mu        sync.Mutex
    clock     Clock
    start     time.Time
    baseWall  time.Time
    events    map[EventName]int64
}

func NewRecorder(clock Clock) *Recorder {
    now := clock.Now()
    return &Recorder{
        clock:    clock,
        start:    now,
        baseWall: now,
        events:   make(map[EventName]int64),
    }
}

func (r *Recorder) Mark(name EventName) {
    now := r.clock.Now()
    r.mu.Lock()
    defer r.mu.Unlock()
    if name == EventRequestStart {
        r.start = now
        r.baseWall = now
    }
    r.events[name] = now.Sub(r.start).Nanoseconds()
}
```

Important details:

- `request_start` should be zero or close to zero by definition.
- `scheduled_at` may be negative if recorded before `request_start`. That is allowed and useful.
- `MarkFirst(name)` should exist for events that should only be recorded once, e.g. `first_sse_event` and `first_output_delta`.
- `MarkLast(name)` can just overwrite, e.g. `last_output_delta`.
- Recorder must be concurrency-safe because `httptrace` callbacks can be invoked asynchronously.

Implemented details:

- Added `Clock` and `RealClock` to abstract timestamp reads while preserving Go monotonic duration behavior.
- Added a concurrency-safe `Recorder` with `Mark`, `MarkFirst`, `MarkLast`, `ElapsedNS`, and `Timeline` snapshot methods.
- Rebased previously recorded events when `request_start` is marked so `scheduled_at` and other pre-start events can be negative relative to the real request start.
- Added one-file/one-test-file coverage for `clock.go` and `recorder.go`, including fake-clock tests with no sleeps.

Definition of done:

- Unit tests verify `MarkFirst` does not overwrite.
- Unit tests verify negative `scheduled_at` is possible and preserved.
- Unit tests use a fake clock; no sleeps in recorder tests.

---

### [x] 4. Implement HTTP trace capture

Capture client-side network and HTTP lifecycle timing with `net/http/httptrace`.

Files:

- `internal/httptracecap/trace.go`
- `internal/httptracecap/client.go`

Behavior:

- Attach `httptrace.ClientTrace` to every request context.
- Record:
  - DNS start/done
  - TCP connect start/done
  - TLS handshake start/done
  - got connection + reuse metadata
  - request write complete
  - first response byte
- Return an `HTTPRecord` with connection reuse and status metadata.

Sketch:

```go
func WithTrace(ctx context.Context, rec *whatttft.Recorder, httpRec *whatttft.HTTPRecord) context.Context {
    trace := &httptrace.ClientTrace{
        DNSStart: func(info httptrace.DNSStartInfo) {
            rec.Mark(whatttft.EventDNSStart)
        },
        DNSDone: func(info httptrace.DNSDoneInfo) {
            rec.Mark(whatttft.EventDNSDone)
            httpRec.DNSAddrs = len(info.Addrs)
            if info.Err != nil {
                httpRec.DNSError = info.Err.Error()
            }
        },
        ConnectStart: func(network, addr string) {
            rec.Mark(whatttft.EventConnectStart)
            httpRec.Network = network
            httpRec.RemoteAddr = addr
        },
        ConnectDone: func(network, addr string, err error) {
            rec.Mark(whatttft.EventConnectDone)
            if err != nil {
                httpRec.ConnectError = err.Error()
            }
        },
        TLSHandshakeStart: func() {
            rec.Mark(whatttft.EventTLSStart)
        },
        TLSHandshakeDone: func(state tls.ConnectionState, err error) {
            rec.Mark(whatttft.EventTLSDone)
            httpRec.TLSVersion = tlsVersionString(state.Version)
            if err != nil {
                httpRec.TLSError = err.Error()
            }
        },
        GotConn: func(info httptrace.GotConnInfo) {
            rec.Mark(whatttft.EventGotConn)
            httpRec.ConnReused = info.Reused
            httpRec.ConnWasIdle = info.WasIdle
            httpRec.ConnIdleTimeNS = info.IdleTime.Nanoseconds()
        },
        WroteRequest: func(info httptrace.WroteRequestInfo) {
            rec.Mark(whatttft.EventWroteRequest)
            if info.Err != nil {
                httpRec.WriteError = info.Err.Error()
            }
        },
        GotFirstResponseByte: func() {
            rec.MarkFirst(whatttft.EventFirstResponseByte)
        },
    }
    return httptrace.WithClientTrace(ctx, trace)
}
```

Client construction:

```go
type TransportConfig struct {
    ConnectionMode ConnectionMode
    Timeout        time.Duration
}

func NewHTTPClient(cfg TransportConfig) *http.Client {
    tr := &http.Transport{
        Proxy:                 http.ProxyFromEnvironment,
        ForceAttemptHTTP2:     true,
        DisableCompression:    true,
        MaxIdleConns:          100,
        MaxIdleConnsPerHost:   100,
        IdleConnTimeout:       90 * time.Second,
        TLSHandshakeTimeout:   10 * time.Second,
        ResponseHeaderTimeout: 0,
    }
    if cfg.ConnectionMode == ColdConnections {
        tr.DisableKeepAlives = true
        tr.MaxIdleConnsPerHost = -1
    }
    return &http.Client{Transport: tr, Timeout: cfg.Timeout}
}
```

Important details:

- For cold connection mode, use either a new transport per request or `DisableKeepAlives: true` plus `CloseIdleConnections`; document that OS DNS/TLS/session caching may still exist.
- For warm mode, reuse one transport for the run.
- Disable automatic compression by default and record this choice.
- Never log authorization headers.

Implemented details:

- Added `internal/httptracecap.Capture` with concurrency-safe HTTP trace callbacks for DNS, TCP connect, TLS handshake, connection acquisition/reuse, request write completion, and first response byte.
- Added response observation for status code, status text, and HTTP protocol, plus normalized TLS version labels and compression-disabled metadata.
- Added `NewHTTPClient` with benchmark-oriented transport defaults: HTTP/2 enabled, automatic compression disabled, configurable warm/cold keepalive behavior, and whole-request timeout support.
- Added one-file/one-test-file coverage for trace capture and client construction/reuse behavior using `httptest.Server`/`httptest.NewTLSServer`.

Definition of done:

- Unit tests with `httptest.Server` verify `GotFirstResponseByte` and `GotConn` are recorded.
- Tests verify warm mode can reuse a connection and cold mode does not reuse a connection.
- Race detector passes for trace capture tests.

---

### [x] 5. Implement SSE parser

Implement a small Server-Sent Events parser instead of relying on provider SDKs.

Files:

- `internal/sse/parser.go`
- `internal/sse/parser_test.go`

Requirements:

- Read from `io.Reader`.
- Support LF and CRLF line endings.
- Support multi-line `data:` fields and join them with `\n` per SSE rules.
- Ignore comment lines beginning with `:`.
- Preserve raw byte counts for diagnostics.
- Return event timestamp as close as possible to the moment the blank line completing the event is read.
- Avoid `bufio.Scanner`'s default 64 KiB token limit, or explicitly increase it. Prefer `bufio.Reader.ReadString('\n')`.

API sketch:

```go
type Event struct {
    Data     []byte
    Event    string
    ID       string
    Retry    string
    RawBytes int
}

type Parser struct {
    r *bufio.Reader
}

func New(r io.Reader) *Parser {
    return &Parser{r: bufio.NewReaderSize(r, 64*1024)}
}

func (p *Parser) Next() (Event, error) {
    var data [][]byte
    var raw int
    for {
        line, err := p.r.ReadBytes('\n')
        raw += len(line)
        if err != nil && len(line) == 0 {
            if errors.Is(err, io.EOF) && len(data) > 0 {
                return Event{Data: bytes.Join(data, []byte("\n")), RawBytes: raw}, nil
            }
            return Event{}, err
        }

        line = bytes.TrimRight(line, "\r\n")
        if len(line) == 0 {
            return Event{Data: bytes.Join(data, []byte("\n")), RawBytes: raw}, nil
        }
        if line[0] == ':' {
            continue
        }
        // parse field/value, especially data:
    }
}
```

Test cases:

- Single `data: {...}\n\n` event.
- CRLF input.
- Multi-line data.
- Comment/heartbeat before data.
- EOF after a complete event.
- EOF with partial event.
- Large JSON chunk over 64 KiB.

Implemented details:

- Added `internal/sse.Parser` and `Event` for provider-independent Server-Sent Events parsing from any `io.Reader`.
- Supports LF/CRLF, multi-line `data:` joining with `\n`, comments/heartbeat skipping, `event`, `id`, and `retry` fields, EOF with a partial event, and explicit empty data events.
- Uses `bufio.Reader.ReadBytes('\n')` instead of `bufio.Scanner`, so large JSON/data lines over 64 KiB are supported.
- Preserves raw byte counts for returned event blocks, including comments inside the same block.

Definition of done:

- Parser tests pass.
- Parser has no provider-specific logic.
- Parser does not allocate excessive memory for normal streams.

---

### [x] 6. Implement OpenAI-compatible Chat Completions provider

Implement direct HTTP streaming for `POST /v1/chat/completions`.

Files:

- `pkg/provider/openai/config.go`
- `pkg/provider/openai/provider.go`
- `pkg/provider/openai/types.go`
- `pkg/provider/openai/provider_test.go`

Config:

```go
package openai

type Config struct {
    BaseURL     string // default: https://api.openai.com/v1
    APIKey      string
    APIKeyEnv   string
    Organization string
    Project     string
    Model       string

    // For OpenAI current APIs prefer max_completion_tokens, but some
    // OpenAI-compatible providers still expect max_tokens.
    UseLegacyMaxTokens bool

    // If true, send stream_options.include_usage=true.
    IncludeUsage bool

    HTTPClient *http.Client
}
```

Request body shape:

```go
type chatCompletionRequest struct {
    Model               string           `json:"model"`
    Messages            []chatMessage    `json:"messages"`
    Stream              bool             `json:"stream"`
    StreamOptions       *streamOptions   `json:"stream_options,omitempty"`
    Temperature         *float64         `json:"temperature,omitempty"`
    TopP                *float64         `json:"top_p,omitempty"`
    Stop                []string         `json:"stop,omitempty"`
    Seed                *int64           `json:"seed,omitempty"`
    MaxCompletionTokens *int             `json:"max_completion_tokens,omitempty"`
    MaxTokens           *int             `json:"max_tokens,omitempty"`
}

type chatMessage struct {
    Role    string `json:"role"`
    Content string `json:"content"`
}

type streamOptions struct {
    IncludeUsage bool `json:"include_usage"`
}
```

Streaming response shape:

```go
type chatCompletionChunk struct {
    ID      string   `json:"id"`
    Object  string   `json:"object"`
    Created int64    `json:"created"`
    Model   string   `json:"model"`
    Choices []choice `json:"choices"`
    Usage   *usage   `json:"usage"`
}

type choice struct {
    Index        int    `json:"index"`
    Delta        delta  `json:"delta"`
    FinishReason string `json:"finish_reason"`
}

type delta struct {
    Role    string `json:"role"`
    Content string `json:"content"`
    Refusal string `json:"refusal"`
    // Tool calls can be added for capture later. v0.1 can ignore for TTFT.
}

type usage struct {
    PromptTokens     int `json:"prompt_tokens"`
    CompletionTokens int `json:"completion_tokens"`
    TotalTokens      int `json:"total_tokens"`
    PromptTokensDetails *struct {
        CachedTokens int `json:"cached_tokens"`
    } `json:"prompt_tokens_details"`
    CompletionTokensDetails map[string]any `json:"completion_tokens_details"`
}
```

Provider behavior:

- Build request body from `whatttft.ProviderRequest`.
- Use `stream=true`.
- Default `BaseURL` to `https://api.openai.com/v1`.
- Endpoint: `${BaseURL}/chat/completions`.
- Add headers:
  - `Authorization: Bearer <api key>`
  - `Content-Type: application/json`
  - optional `OpenAI-Organization`
  - optional `OpenAI-Project`
- Attach httptrace using the package from task 4.
- Mark `headers_received` when `client.Do` returns.
- For non-2xx responses:
  - read a bounded response body, e.g. 64 KiB;
  - return a structured error with status code and body snippet;
  - never retry automatically in v0.1.
- Parse SSE events:
  - first SSE event marks `first_sse_event`, even if empty;
  - `data: [DONE]` marks `done_event` and is not output;
  - empty data, role-only delta, and usage-only chunks do not mark TTFT;
  - first non-empty `delta.content` marks `first_output_delta`;
  - every non-empty `delta.content` updates `last_output_delta` and emits a `ChunkRecord`;
  - usage chunks update `ProviderUsage` and `CacheRecord`.
- Mark `body_eof` after response body is fully consumed and closed.

Pseudo-loop:

```go
parser := sse.New(resp.Body)
for {
    ev, err := parser.Next()
    if errors.Is(err, io.EOF) {
        sink.Mark(whatttft.EventBodyEOF)
        return usage, nil
    }
    if err != nil {
        return usage, fmt.Errorf("read SSE event: %w", err)
    }

    sink.MarkFirst(whatttft.EventFirstSSEEvent)

    data := bytes.TrimSpace(ev.Data)
    if bytes.Equal(data, []byte("[DONE]")) {
        sink.Mark(whatttft.EventDone)
        continue
    }
    if len(data) == 0 {
        sink.AddChunk(emptyChunk(...))
        continue
    }

    var chunk chatCompletionChunk
    if err := json.Unmarshal(data, &chunk); err != nil {
        return usage, fmt.Errorf("decode chat completion chunk: %w", err)
    }

    if chunk.Usage != nil {
        sink.SetUsage(normalizeUsage(chunk.Usage))
        sink.SetCache(normalizeCache(chunk.Usage))
    }

    for _, choice := range chunk.Choices {
        content := choice.Delta.Content
        if content == "" {
            sink.AddChunk(roleOrEmptyChunk(...))
            continue
        }
        sink.MarkFirst(whatttft.EventFirstOutputDelta)
        sink.Mark(whatttft.EventLastOutputDelta)
        sink.AddChunk(contentChunk(...))
    }
}
```

Implemented details:

- Added `pkg/provider/openai` with config resolution, default base URL handling, direct HTTP `POST /chat/completions` streaming, and an `openai.New` provider constructor.
- Builds OpenAI-compatible Chat Completions request bodies with system/user messages, `stream=true`, optional stream usage, sampling parameters, stop sequences, seed, and modern or legacy max-token fields.
- Attaches HTTP trace capture, records request/header/body lifecycle events, normalizes response status/protocol metadata, and returns bounded redacted `APIError` values for non-2xx responses without retries.
- Parses SSE streams directly, ignoring comments/heartbeats for TTFT, distinguishing empty/role-only/usage-only chunks from visible output, recording `[DONE]`, usage, prompt-cache counters, finish reasons, and body EOF.
- Updated `internal/httptracecap.WithTrace` to accept any marker implementing `Mark`/`MarkFirst`, so provider observers can receive trace events directly.
- Added `httptest.Server` coverage for successful streams, empty chunks, role-only chunks before content, usage chunks, `[DONE]`, non-200 redaction, malformed JSON, request-body construction, capabilities, and required-input validation.

Definition of done:

- Unit tests use `httptest.Server`, not the real OpenAI API.
- Tests cover role-only chunk before content, empty chunks, usage chunk, `[DONE]`, non-200 error, and malformed JSON.
- Authorization header is present in requests but never appears in errors/logs.

---

### [x] 7. Implement derived metrics calculation

Convert raw timeline events into named metrics.

Files:

- `pkg/whatttft/metrics.go`
- `pkg/whatttft/metrics_test.go`

Derived metrics struct:

```go
type DerivedMetrics struct {
    HTTPTTFBMS                       *float64 `json:"http_ttfb_ms,omitempty"`
    HeadersLatencyMS                 *float64 `json:"headers_latency_ms,omitempty"`
    FirstEventMS                     *float64 `json:"first_event_ms,omitempty"`
    TTFTDeltaMS                      *float64 `json:"ttft_delta_ms,omitempty"`
    E2EDeltaMS                       *float64 `json:"e2e_delta_ms,omitempty"`
    StreamTotalMS                    *float64 `json:"stream_total_ms,omitempty"`
    GenerationDeltaMS                *float64 `json:"generation_delta_ms,omitempty"`
    E2EOutputTPS                     *float64 `json:"e2e_output_tps,omitempty"`
    ServerWaitToFirstByteMS          *float64 `json:"server_wait_to_first_byte_ms,omitempty"`
    StreamProtocolToFirstOutputMS    *float64 `json:"stream_protocol_to_first_output_ms,omitempty"`
    DNSMS                            *float64 `json:"dns_ms,omitempty"`
    TCPConnectMS                     *float64 `json:"tcp_connect_ms,omitempty"`
    TLSMS                            *float64 `json:"tls_ms,omitempty"`
    RequestWriteMS                   *float64 `json:"request_write_ms,omitempty"`
}
```

Rules:

- If either endpoint event is missing, metric is `nil`/omitted.
- `generation_delta_ms = last_output_delta - first_output_delta`.
- `e2e_output_tps = completion_tokens / e2e_delta_seconds` when completion tokens are known.
- Do not compute token TPOT in v0.1 unless true token timestamps are implemented.
- Compute `chunk_itl_ms` summary later from chunks; keep it separate from token ITL.

Implemented details:

- Added `CalculateDerivedMetrics` to compute standardized request-level latency and throughput fields from monotonic-relative `Timeline` events.
- Metrics return nil when required endpoint events are absent, while observed zero-duration event pairs return non-nil `0` values.
- Added `e2e_output_tps` calculation from provider-reported completion tokens and positive `e2e_delta` duration.
- Added one-file/one-test-file coverage for complete timelines, missing endpoint events, zero-duration metrics, and zero completion-token throughput.

Definition of done:

- Unit tests cover complete timeline and missing-event timeline.
- Tests cover `0` duration metrics and ensure they are not confused with missing metrics.
- Names match `AGENTS.md` exactly.

---

### [x] 8. Implement cache mode prompt mutation

Make prompt/KV cache behavior explicit and reproducible.

Files:

- `pkg/whatttft/cache.go`
- `pkg/whatttft/cache_test.go`

Behavior:

- `cache-bust`: generate a unique nonce per request and place it where configured.
  - Default nonce location should be `prefix` to avoid prefix-cache hits.
  - Example system prompt prefix: `Benchmark nonce: <nonce>. Do not mention this nonce.\n\n`
  - Record nonce location in request metadata.
- `cache-reuse`: measured requests use identical prompt text. Warmup/cache-population requests also use the same prompt unless configured otherwise.
- `provider-explicit-cache`: no-op for OpenAI Chat Completions v0.1 unless provider-specific cache controls are added later. Records requested mode.
- `unknown`: no prompt mutation.

API sketch:

```go
type PromptPlan struct {
    Prompt        string
    PromptHash    string
    CacheMode     CacheMode
    Nonce         string
    NonceLocation string
}

func BuildPromptPlan(cfg RunConfig, requestIndex int, warmup bool) PromptPlan
```

Hashing:

- Use SHA-256 of final prompt text.
- Store hex hash in `RequestRecord.PromptHash`.
- Do not store prompt text in request records by default if it may contain sensitive data; store it in `run.json` only if explicitly configured.

Implemented details:

- Added `PromptPlan` and `BuildPromptPlan` for cache-mode-aware prompt mutation and SHA-256 prompt hashing.
- `cache-bust` now inserts a per-request prefix nonce into the final prompt, records the nonce and location, and defaults from an empty cache mode.
- `cache-reuse`, `provider-explicit-cache`, and `unknown` leave prompt text unchanged while preserving the requested cache mode in the plan.
- Added one-file/one-test-file coverage for cache-busted uniqueness, cache-reuse stability, no-op modes, default cache mode behavior, prompt hashing, and warmup/measured nonce labels.

Definition of done:

- Tests verify cache-bust produces different prompt hashes per request.
- Tests verify cache-reuse produces the same prompt hash per request.
- Tests verify nonce does not appear in summaries unless explicitly requested.

---

### [x] 9. Implement sequential runner with warmup and measured phases

Implement the first end-to-end runner mode: one request at a time.

Files:

- `pkg/whatttft/runner.go`
- `pkg/whatttft/runner_test.go`

Runner responsibilities:

- Validate `RunConfig`.
- Create/reuse HTTP client according to connection mode. The OpenAI provider may accept an HTTP client from the runner or construct one from config; choose one approach and keep it explicit.
- Execute warmup requests first, then measured requests.
- Do not include warmup requests in default aggregate summaries.
- Preserve warmup request records in `requests.jsonl`.
- Respect `context.Context` cancellation.
- Continue after per-request provider errors unless context is cancelled.
- Record request IDs and attempt numbers.

Pseudo-flow:

```go
func (r *Runner) Run(ctx context.Context) (*RunResult, error) {
    total := r.cfg.WarmupRequests + r.cfg.MeasuredRequests
    records := make([]RequestRecord, 0, total)

    for i := 0; i < total; i++ {
        warmup := i < r.cfg.WarmupRequests
        rec := r.runOne(ctx, i, warmup)
        records = append(records, rec)
        if ctx.Err() != nil {
            break
        }
    }

    summary := Summarize(records, r.chunks)
    return &RunResult{Records: records, Summary: summary}, nil
}
```

Implemented details:

- Added `Runner`, `RunResult`, `RunSummary`, and `NewRunner` with a sequential `Run` implementation that executes warmup requests before measured requests.
- Added per-request prompt planning, scheduled-at recording, request IDs, attempt numbers, warmup flags, provider/model/scenario metadata, usage/cache/HTTP capture, derived metric calculation, and redacted error records.
- Added a concurrency-safe provider observer that timestamps semantic output hooks, updates TTFT/E2E timeline events, and optionally captures output/usage chunks when `SaveChunks` is true.
- Added initial measured-only run summary counts so warmup successes/errors are preserved as records but excluded from default summary success/error counts.
- Added validation for request counts, unsupported fixed concurrency, cache mode, connection mode, and nil providers.
- Added fake-provider tests for warmup exclusion, continuation after per-request errors, context cancellation, and validation failures.

Definition of done:

- Unit tests use a fake provider and verify warmup exclusion from summary.
- Cancellation test stops the run promptly.
- Request errors are captured as records and do not crash the entire run.

---

### [x] 10. Implement fixed-concurrency runner mode

Add concurrent request execution after sequential mode works.

Files:

- `pkg/whatttft/runner_concurrent.go`
- `pkg/whatttft/runner_concurrent_test.go`

Behavior:

- `Concurrency <= 1` uses sequential mode.
- `Concurrency > 1` starts N workers.
- Each worker gets the next request after finishing the previous one.
- Record `scheduled_at` before enqueueing or assigning the request.
- Preserve deterministic request indexes and IDs.
- Warmup phase and measured phase should be separate barriers:
  1. finish all warmup requests;
  2. then start measured requests.

Important details:

- Do not print from workers in the hot path.
- Protect shared writers/record slices with channels or mutexes.
- Per-request recorder and chunk slice must not be shared unsafely.
- Use `errgroup` only if you choose to add `golang.org/x/sync`; otherwise use `sync.WaitGroup` and channels to keep dependencies minimal.

Implemented details:

- Added fixed-concurrency execution for `RunConfig.Concurrency > 1`, while preserving sequential behavior for `Concurrency <= 1`.
- Runs warmup and measured phases through separate worker-pool barriers so measured jobs are not scheduled until all warmup jobs complete.
- Records `scheduled_at` before sending jobs to workers, keeps per-request recorders/observers isolated, and sorts completed outputs by deterministic attempt index before appending records/chunks.
- Returns partial results plus context errors when cancellation interrupts scheduling or in-flight work.
- Added one-file/one-test-file coverage for exact measured record counts, deterministic record ordering, maximum observed concurrency, warmup/measured barrier behavior, and context cancellation.

Definition of done:

- Tests verify exactly the configured number of measured records.
- Tests verify warmup records are completed before measured records begin.
- `go test -race ./...` passes.

---

### [x] 11. Implement statistics and aggregate summaries

Compute useful aggregate metrics over successful measured requests.

Files:

- `internal/stats/stats.go`
- `pkg/whatttft/summary.go`
- `pkg/whatttft/summary_test.go`

Stats API:

```go
type Distribution struct {
    Count  int      `json:"count"`
    Min    *float64 `json:"min,omitempty"`
    Mean   *float64 `json:"mean,omitempty"`
    P50    *float64 `json:"p50,omitempty"`
    P90    *float64 `json:"p90,omitempty"`
    P95    *float64 `json:"p95,omitempty"`
    P99    *float64 `json:"p99,omitempty"`
    Max    *float64 `json:"max,omitempty"`
    StdDev *float64 `json:"stddev,omitempty"`
}
```

Summary should include:

- run count, warmup count, measured count;
- success count and error count;
- error categories and status codes;
- distributions for:
  - `http_ttfb_ms`
  - `headers_latency_ms`
  - `first_event_ms`
  - `ttft_delta_ms`
  - `e2e_delta_ms`
  - `stream_total_ms`
  - `generation_delta_ms`
  - `server_wait_to_first_byte_ms`
  - `stream_protocol_to_first_output_ms`
  - `dns_ms`, `tcp_connect_ms`, `tls_ms`, `request_write_ms`
- output token totals when usage is available;
- `system_tps` and `rps` over successful measured requests;
- cache summary:
  - cache mode;
  - observed cached prompt token distribution;
  - observed cache hit count if `cached_tokens > 0`.
- connection summary:
  - reused connection count;
  - protocol counts, e.g. HTTP/2 vs HTTP/1.1.

Percentile method:

- Sort values ascending.
- Use nearest-rank or linear interpolation; document which one.
- Keep it simple and deterministic. Recommended: nearest-rank for v0.1.

Implemented details:

- Added `internal/stats` with deterministic nearest-rank percentile distributions, arithmetic mean, min/max, and population standard deviation.
- Added public summary schema types for distributions, run summaries, grouped summaries, metric distributions, cache summaries, and connection summaries.
- Added `Summarize` to aggregate measured requests while excluding warmups from default success/error counts and metric distributions.
- Summary groups are split by provider, model, scenario, cache mode, and connection mode so cache-bust and cache-reuse records are never combined.
- Added measured error category/status-code counts, successful measured metric distributions, completion token totals, system TPS, RPS, cache-hit/cached-token summaries, reused-connection counts, and protocol counts.
- Added one-file/one-test-file coverage for percentile behavior, warmup exclusion, cache-mode grouping, status/error counts, throughput behavior, cache summaries, and connection summaries.

Definition of done:

- Unit tests verify percentile behavior on known values.
- Summary excludes warmup by default.
- Summary groups cache-bust and cache-reuse separately if a mixed record list is ever provided.

---

### [x] 12. Implement report writers

Write raw and summarized results to disk.

Files:

- `internal/report/writer.go`
- `internal/report/markdown.go`
- `internal/report/writer_test.go`

Output directory format:

```text
run.json
requests.jsonl
chunks.jsonl      # only when SaveChunks is true
summary.json
summary.md
```

`run.json` should contain:

- benchmark version;
- git SHA if detectable;
- Go version;
- OS/arch;
- provider/model/base URL with secrets redacted;
- scenario config;
- run config;
- wall-clock start/end;
- command-line args if invoked from CLI.

`requests.jsonl`:

- one full `RequestRecord` per line;
- includes errors;
- includes warmup records.

`chunks.jsonl`:

- one `ChunkRecord` per line;
- omit by default unless `--save-chunks` is set because chunks may contain generated content.

`summary.md`:

- human-readable table focused on p50/p95/p99:

```md
| metric | count | mean | p50 | p95 | p99 | max |
|---|---:|---:|---:|---:|---:|---:|
| ttft_delta_ms | 50 | 312.4 | 299.1 | 410.7 | 590.3 | 590.3 |
```

Implemented details:

- Added `internal/report` with `WriteRun`, `RunMetadata`, `WriteOptions`, and URL redaction for provider base URLs in `run.json`.
- Writer creates the output directory, protects existing non-empty directories unless `Overwrite` is true, and writes `run.json`, `requests.jsonl`, optional `chunks.jsonl`, `summary.json`, and `summary.md`.
- Runtime metadata fields are filled automatically when omitted, including benchmark version, Go version, OS, architecture, and best-effort git SHA.
- Added Markdown summary rendering with run counts, grouped metric tables, and key p50/p95/p99-focused metric rows.
- Added one-file/one-test-file coverage for parseable JSON/JSONL output, overwrite behavior, chunk omission, required option validation, URL redaction, and Markdown metric names.

Definition of done:

- Writer creates output directory if missing.
- Writer fails if output directory exists and is non-empty unless `Overwrite` is configured.
- Tests parse generated JSON/JSONL back into structs.
- Markdown writer test checks key metric names are present.

---

### [x] 13. Implement CLI `run` command

Build a minimal CLI around the library.

Files:

- `cmd/what-ttft/main.go`
- `cmd/what-ttft/run.go`

Use standard library `flag` for v0.1.

Required flags:

```text
--provider openai
--base-url https://api.openai.com/v1
--api-key-env OPENAI_API_KEY
--api-key <discouraged; supported for local testing but never printed>
--model gpt-5.5
--prompt "..."
--system-prompt "..."
--samples 50
--warmup 5
--concurrency 1
--cache-mode cache-bust|cache-reuse|provider-explicit-cache|unknown
--connection-mode warm|cold
--max-output-tokens 64
--temperature 0
--top-p 1
--timeout 120s
--out runs/<name>
--save-chunks=false
--include-usage=true
--legacy-max-tokens=false
```

Behavior:

- `what-ttft --help` prints top-level usage.
- `what-ttft run --help` prints run usage.
- Validate provider is `openai` for v0.1.
- Resolve API key from `--api-key-env` or `--api-key`.
- Refuse to print the API key.
- Print a concise summary and output directory path at the end.

Example final output:

```text
provider=openai model=gpt-5.5 scenario=ad-hoc samples=50 warmup=5 concurrency=1 cache=cache-bust connection=warm

metric                p50      p95      p99      mean
http_ttfb_ms          180.4    290.1    350.2    195.8
ttft_delta_ms         312.7    450.9    610.4    330.2
e2e_delta_ms          980.0    1300.4   1601.2   1005.1

wrote results to runs/openai-gpt-5.5-short-cold
```

Implemented details:

- Reworked the CLI dispatcher to support top-level help, unknown-command handling, and a `run` subcommand while keeping the process exit behavior testable.
- Added `what-ttft run` using standard-library `flag` with OpenAI v0.1 flags for base URL, API key/env, model, prompt, warmup/samples, concurrency, cache/connection modes, generation parameters, timeout, output directory, chunks, usage, and legacy max-token compatibility.
- The run command constructs the OpenAI provider with an explicit benchmark HTTP client, executes the shared runner, writes reports through `internal/report`, redacts inline API keys in recorded args, and prints a concise metric summary without printing secrets.
- Added CLI tests for top-level help, run help, invalid provider validation, and an end-to-end fake OpenAI streaming server that writes `run.json`, `requests.jsonl`, `chunks.jsonl`, `summary.json`, and `summary.md`.

Definition of done:

- CLI can run against `httptest.Server` in tests by passing `--base-url`.
- `go run ./cmd/what-ttft run --help` works.
- `go run ./cmd/what-ttft run ...` writes all output files against a fake OpenAI server.

---

### [x] 14. Add deterministic fake OpenAI test server

Create reusable fake server helpers for end-to-end tests.

Files:

- `internal/testserver/openai.go`
- `internal/testserver/openai_test.go`

Capabilities:

- Validate request path `/v1/chat/completions` or `/chat/completions` depending on base URL construction.
- Validate `stream=true`.
- Validate authorization header exists without checking secret value.
- Emit configurable delays:
  - delay before headers;
  - delay before first SSE event;
  - delay before first content chunk;
  - delay between chunks.
- Emit scriptable chunks:

```go
type StreamStep struct {
    Delay   time.Duration
    Data    string // raw data payload, not including "data: "
    Comment string
}
```

Example fake stream:

```text
:data heartbeat

data: {"choices":[{"delta":{"role":"assistant"}}]}

data: {"choices":[{"delta":{"content":"Hello"}}]}

data: {"choices":[{"delta":{"content":" world"},"finish_reason":"stop"}]}

data: {"choices":[],"usage":{"prompt_tokens":10,"completion_tokens":2,"total_tokens":12,"prompt_tokens_details":{"cached_tokens":0}}}

data: [DONE]

```

Implemented details:

- Added `internal/testserver.OpenAIServer` with reusable scriptable SSE stream steps and request capture for OpenAI-compatible Chat Completions tests.
- The fake server validates `/chat/completions` and `/v1/chat/completions`, `POST`, `stream=true`, and presence of an Authorization header without checking or logging the secret value.
- Added configurable delays before headers, before the first stream event, before the first apparent content delta, between steps, and per step.
- Added end-to-end coverage through the real OpenAI provider adapter, runner, metrics, summaries, and report writer using a fake stream with heartbeat, empty event, role-only chunk, content chunks, usage/cache metadata, and `[DONE]`.
- Tests verify TTFT occurs after the first SSE event, usage/cache metadata is captured, `done_event`/`body_eof` are recorded, and report files are written.

Definition of done:

- End-to-end runner test uses fake server and verifies TTFT ignores heartbeat and role-only chunk.
- End-to-end test verifies usage and cache metadata are captured.
- End-to-end test verifies report files are written.

---

### [x] 15. Add optional real OpenAI integration test

Add an opt-in integration test that can be run manually.

Files:

- `pkg/provider/openai/integration_test.go`

Rules:

- Skipped unless `WHAT_TTFT_INTEGRATION=1` and `OPENAI_API_KEY` are set.
- Model defaults to a cheap configurable model via `WHAT_TTFT_OPENAI_MODEL`; do not hardcode expensive models.
- Use `MeasuredRequests=1` and `WarmupRequests=0` by default.
- Do not assert strict latency values.
- Assert:
  - request succeeds;
  - first output delta exists;
  - body EOF exists;
  - if usage is returned, token counts are non-negative.

Command:

```sh
WHAT_TTFT_INTEGRATION=1 \
OPENAI_API_KEY=... \
WHAT_TTFT_OPENAI_MODEL=gpt-5.5 \
go test ./pkg/provider/openai -run Integration -count=1
```

Implemented details:

- Added `TestIntegrationOpenAIStreaming` under `pkg/provider/openai`, skipped unless `WHAT_TTFT_INTEGRATION=1` and `OPENAI_API_KEY` are set.
- Uses a configurable model via `WHAT_TTFT_OPENAI_MODEL`, defaulting to `gpt-5.5`, and an optional `WHAT_TTFT_OPENAI_BASE_URL` override.
- Runs one measured streaming request through the shared runner with `reasoning_effort=none`, cache-bust prompt planning, and no warmup.
- Asserts the request succeeds, `first_output_delta` and `body_eof` are present, and any returned token counts are non-negative without logging the API key.

Definition of done:

- Integration test is skipped by default.
- Integration test does not leak API key in logs.
- Unit tests still do not require network access.

---

### [x] 16. Document the v0.1 methodology and usage in README

Update README with practical usage and methodology warnings.

Files:

- `README.md`

Include:

- What the tool measures.
- What it cannot measure from the client side.
- Installation/build instructions.
- OpenAI example.
- Explanation of key metrics:
  - HTTP TTFB;
  - first SSE event;
  - TTFT delta;
  - E2E delta;
  - chunk cadence;
  - token usage;
  - cache metadata.
- Cache-mode warning:

  > Do not compare cached and uncached results. Prompt/KV cache hits can dominate TTFT for long prompts.

- Recommended benchmark procedure:
  1. Run sequential cache-busted baseline.
  2. Run sequential cache-reuse test.
  3. Run warm vs cold connection comparison.
  4. Run concurrency sweep.
  5. Compare p50/p95/p99, not just averages.

Implemented details:

- Expanded README with v0.1 scope, what the tool measures, client-side attribution limits, build/check commands, and a copy-paste OpenAI-compatible command.
- Documented key metrics, output files, cache modes, the cached-vs-uncached comparison warning, recommended benchmark procedure, and optional real OpenAI integration test usage.
- Noted practical model options such as omitting unsupported temperature values and using `--reasoning-effort none` where supported.
- Linked `AGENTS.md` as the contributor methodology source of truth.

Definition of done:

- README has a complete copy-paste OpenAI command.
- README explains output files.
- README references `AGENTS.md` for contributor methodology.

---

### [x] 17. Final v0.1 quality gate

Before tagging or calling v0.1 complete, run the full local gate.

Commands:

```sh
go test ./...
go test -race ./...
golangci-lint run
go build ./...
go run ./cmd/what-ttft run --help
```

Manual fake-server smoke test:

- Add or use a test helper command/script that runs the CLI against a local fake OpenAI server and writes reports.
- Confirm `summary.md`, `summary.json`, `requests.jsonl`, and `run.json` are produced.

Implemented details:

- Added `scripts/smoke-fake-openai.sh`, a no-network smoke script that builds the CLI, starts a local deterministic fake OpenAI-compatible SSE server, runs `what-ttft run`, verifies all report files are produced, and checks the smoke API key is absent from output files.
- Documented the fake-server smoke script in README testing instructions and ignored transient `.what-ttft-smoke.*` build directories.
- Ran the full local v0.1 quality gate and the fake-server smoke script successfully.

Definition of done:

- All commands pass.
- No API keys or secrets appear in output files.
- `implementation-plan.md` tasks completed for v0.1 are marked `[x]`.

---

## v0.2 feature plan: Responses-default OpenAI, multi-model YAML benchmarks, and TPS

The next milestone should first move the OpenAI provider to the Responses API by default, then make `what-ttft` useful for repeatable model comparisons without compromising the measurement discipline from v0.1.

### Research notes and adopted design decisions

Sources reviewed while planning v0.2:

- NVIDIA NIM/GenAI-Perf metrics documentation: <https://docs.nvidia.com/nim/benchmarking/llm/latest/metrics.html>
- Ray/Anyscale LLM latency and throughput definitions: <https://docs.anyscale.com/llm/serving/benchmarking/metrics.md>
- YAML maintained Go package documentation: <https://pkg.go.dev/go.yaml.in/yaml/v3>
- NVIDIA AI Perf YAML configuration examples: <https://docs.nvidia.com/aiperf/tutorials/configuration/yaml-configuration-files>
- Local OpenAI OpenAPI spec: `openai-openapi.yaml`, especially `POST /responses`, `CreateResponse`, `ResponseStreamEvent`, `ResponseTextDeltaEvent`, `ResponseCompletedEvent`, `ResponseUsage`, and `ServiceTier`.

Design decisions for this milestone:

- Do the OpenAI Responses API migration first. The OpenAI provider should default to `/v1/responses` for real OpenAI targets, with Chat Completions retained as an explicit compatibility mode for OpenAI-compatible providers that only expose `/v1/chat/completions`.
- Keep `what-ttft run` as the low-friction single-model/ad-hoc command. Add a new `what-ttft bench --config benchmark.yaml` command for declarative, repeatable, multi-model benchmarks. This avoids overloading the existing flag-heavy `run` command and gives YAML a stable schema.
- Treat a benchmark config as a matrix of targets using a common scenario and run settings for v0.2. A target is a provider/model/API/endpoint/API-key-env tuple. The schema should be forward-compatible with multiple scenarios and provider-specific overrides, but v0.2 should avoid implementing a large matrix language before the single-scenario multi-model path is solid.
- Use one output directory per `bench` invocation with combined `requests.jsonl`, optional combined `chunks.jsonl`, `summary.json`, and `summary.md`. Existing summary grouping by provider/model/scenario/cache/connection is a good base, but v0.2 must add a `target_id` dimension so identical model names on different base URLs, regions, or deployments are not accidentally mixed.
- Use `go.yaml.in/yaml/v3`, not `gopkg.in/yaml.v3`, for YAML parsing. The maintained module is run by the YAML organization and supports `Decoder.KnownFields(true)`, which should be enabled so typos in benchmark configs fail fast.
- Do not store inline API keys in YAML in v0.2. Support `api_key_env` only. This keeps `run.json` and committed benchmark configs safe by default.
- Tokens-per-second must remain precisely named:
  - `e2e_output_tps` already exists and means provider-reported output tokens divided by request-start-to-last-visible-delta seconds. This is user-perceived TPS including TTFT.
  - `system_tps` already exists at summary-group level and means total successful output tokens divided by the first-successful-request to last-successful-response window.
  - Add a new post-TTFT metric only if it is named to reflect the available evidence, e.g. `generation_delta_output_tps`. Do **not** call it `decode_tps` unless true token timestamps are available.
  - Never label chunk cadence as token cadence. Chunk gaps can be reported separately later as `chunk_itl_ms`, but they are not token ITL/TPOT.
- v0.2 may execute targets serially at first for implementation simplicity, but it must record target order in `run.json` and make the limitation clear. A later task can add round-robin or randomized interleaving to reduce time-of-day/provider-load bias.

### [x] 18. Implement OpenAI Responses API default and immediate `service_tier` support

This must be the first v0.2 implementation task. The OpenAI provider should benchmark `/v1/responses` by default because the OpenAPI spec marks Responses as the current model-response API and exposes streaming `ResponseStreamEvent` objects. Chat Completions should remain available as a compatibility mode for OpenAI-compatible providers that have not implemented Responses. In the same task, add `service_tier` support for both Responses and Chat Completions; do not defer service-tier request, record, summary, CLI, or test coverage to the later YAML/multi-model tasks.

OpenAPI findings from `openai-openapi.yaml`:

- `POST /responses` has `operationId: createResponse`, tag `Responses`, and returns either `application/json` `Response` or `text/event-stream` `ResponseStreamEvent`.
- `CreateResponse` supports `model`, `input`, `instructions`, `stream`, `stream_options`, `max_output_tokens`, `temperature`, `top_p`, `reasoning`, tools, text formatting, and related fields.
- Text input can be a plain string through `InputParam`; system/developer instructions can be sent through `instructions`.
- Streaming events include `response.created`, `response.in_progress`, `response.output_item.added`, `response.content_part.added`, `response.output_text.delta`, `response.output_text.done`, `response.completed`, `response.failed`, `response.incomplete`, and `error`, plus many tool/audio/reasoning events that must not count as default text TTFT.
- The first user-visible text delta is `response.output_text.delta` with a non-empty `delta`. Refusal deltas are also user-visible and should be handled separately from hidden reasoning/tool events.
- Usage is reported on the completed `Response` as `usage.input_tokens`, `usage.output_tokens`, `usage.total_tokens`, `usage.input_tokens_details.cached_tokens`, and `usage.output_tokens_details.reasoning_tokens`.
- `ResponseStreamOptions` currently includes `include_obfuscation`; it is not the same as Chat Completions `stream_options.include_usage`.
- `ServiceTier` is a shared OpenAI request/response field with allowed values `auto`, `default`, `flex`, `scale`, and `priority`. When omitted, OpenAI defaults to `auto`; the response may report the actual tier used, which can differ from the requested value.

Implementation details:

- Add an OpenAI API selection enum to `pkg/provider/openai`:

  ```go
  type API string

  const (
      ResponsesAPI API = "responses"
      ChatCompletionsAPI API = "chat-completions"
  )
  ```

  Rules:

  - Empty `Config.API` defaults to `ResponsesAPI`.
  - `ChatCompletionsAPI` preserves the existing `/chat/completions` behavior for compatibility.
  - Unsupported values return a clear validation error before sending a request.
  - Public doc comments must state that `ResponsesAPI` is the default for the OpenAI provider.

- Refactor the existing OpenAI provider without changing the public `whatttft.Provider` interface:
  - keep `StreamChat(ctx, req, obs)` as the normalized chat-style benchmark entrypoint;
  - dispatch internally to `streamResponses` or `streamChatCompletions` based on `Config.API`;
  - keep shared HTTP setup, API-key handling, redaction, status-code errors, and trace capture in common helpers;
  - keep direct `net/http` and the existing SSE parser in the hot path; do not use the OpenAI SDK.
- Implement `POST {base_url}/responses` request construction:
  - `model`: configured model;
  - `input`: cache-mode-mutated user prompt from `ProviderRequest.Prompt` as a plain string for v0.2 text benchmarks;
  - `instructions`: `Scenario.SystemPrompt` when non-empty;
  - `stream`: `true`;
  - `stream_options.include_obfuscation`: `false` by default to avoid artificial payload overhead, and record this provider-specific choice in safe metadata if a metadata field is available;
  - `temperature`, `top_p`, and `reasoning.effort`: map from scenario fields; `service_tier`: map from OpenAI provider config/CLI/YAML target settings;
  - `max_output_tokens`: map from `Scenario.MaxOutputTokens` when positive; validate that Responses requests obey the OpenAPI minimum of 16 when the field is set;
  - do not send Chat-only `max_completion_tokens`, `max_tokens`, or Chat `stream_options.include_usage` to `/responses`.
- Add service-tier support as an immediate first-task benchmark variable for both OpenAI API modes:
  - add an OpenAI `ServiceTier` type or validated string field with allowed values empty, `auto`, `default`, `flex`, `scale`, and `priority`; empty means omit the request field and use provider default behavior;
  - add `ServiceTier` to `openai.Config` so both `ResponsesAPI` and `ChatCompletionsAPI` can send the same configured tier without relying on later YAML work;
  - add additive request-record fields such as `requested_service_tier` and `observed_service_tier`, with comments stating they are OpenAI provider tier labels, not secrets, and empty means unset/unreported;
  - add matching summary-group fields and include requested service tier in the summary group key immediately, so `auto`, `default`, `flex`, `scale`, and `priority` results are never silently aggregated together;
  - record the requested service tier in `run.json`, request records, and summaries, with no redaction required because tier labels are not secrets;
  - capture the provider-reported/actual `service_tier` from Responses terminal `response` objects and Chat Completions chunks/responses when present;
  - do not aggregate or compare results across different requested service tiers.
- Preserve Chat Completions compatibility:
  - existing request/response structs may be renamed to make the Chat-vs-Responses split clear;
  - `service_tier` should also be sent on Chat Completions requests when configured because `CreateChatCompletionRequest` inherits it through `CreateModelResponseProperties`;
  - `UseLegacyMaxTokens` applies only to Chat Completions and should be ignored or rejected with a clear warning/error for Responses, whichever keeps behavior least surprising;
  - `IncludeUsage` applies only to Chat Completions because Responses usage is delivered on the terminal response event when available.
- Parse Responses SSE events by `type` from JSON and optionally cross-check the SSE `event:` field:
  - `response.created`, `response.in_progress`, `response.output_item.added`, empty `response.content_part.added`, reasoning events, function/tool call argument deltas, web/file/code tool events, annotations, and metadata events drive `first_sse_event` but not TTFT;
  - first non-empty `response.output_text.delta.delta` drives `first_output_delta`;
  - later non-empty output text deltas update `last_output_delta`;
  - non-empty `response.refusal.delta.delta` should be treated as visible text/refusal output and should also drive TTFT/E2E unless a scenario explicitly opts out later;
  - `response.output_text.done` records finish metadata but must not create a duplicate output delta;
  - `response.completed` marks `done_event`, captures usage/cache metadata, and records finish status;
  - `response.incomplete` marks `done_event`, captures usage/cache metadata when present, records incomplete reason, and should be treated like a normal length/content-filter finish unless the event also carries an error;
  - `response.failed` and `error` events should return structured provider errors with redacted message/code details;
  - `[DONE]`, if encountered for compatibility, marks `done_event` but is not required for Responses streams;
  - `body_eof` is still recorded only when the response body read completes.
- Normalize Responses usage/cache metadata:
  - `input_tokens` -> `ProviderUsage.PromptTokens`;
  - `output_tokens` -> `ProviderUsage.CompletionTokens`;
  - `total_tokens` -> `ProviderUsage.TotalTokens`;
  - `input_tokens_details.cached_tokens` -> `CacheRecord.PromptCachedTokens` and `CacheRecord.Hit`;
  - `output_tokens_details.reasoning_tokens` -> a redacted/safe usage `Extra` field for now, with future schema work allowed to promote it to a first-class field;
  - document that provider `output_tokens` may include hidden reasoning tokens, so visible-output TPS and provider-output-token TPS can differ for reasoning models.
- Update CLI `run` defaults:
  - add `--openai-api responses|chat-completions`, default `responses`;
  - add `--service-tier auto|default|flex|scale|priority`; omit the field when the flag is not set;
  - top-level and run help should say the OpenAI provider defaults to Responses API;
  - existing fake-server CLI tests must be updated so default `run` posts to `/responses`;
  - add at least one compatibility test proving `--openai-api chat-completions` still posts to `/chat/completions`.
- Update `internal/testserver` or add a new fake Responses server helper:
  - validate `POST /responses` or `/v1/responses`;
  - validate `stream=true`;
  - validate `model`, `input`, optional `instructions`, optional `service_tier`, and authorization header;
  - emit scriptable Responses SSE events with `event:` and `data:` fields;
  - support delayed headers, empty/metadata events before text, text deltas, refusal deltas, terminal completed/incomplete/failed events, usage/cache metadata, malformed JSON, non-200 errors, and abrupt EOF.
- Update tests:
  - request-body construction for Responses, including `service_tier` when configured and omission when unset;
  - request-body construction for Chat Completions compatibility mode, including `service_tier` when configured and omission when unset;
  - default API selection is Responses;
  - first SSE metadata event is recorded before first text delta;
  - `response.output_text.delta` drives TTFT and E2E;
  - reasoning/tool/metadata events do not drive TTFT;
  - usage/cache/reasoning-token metadata and actual `service_tier` are captured from `response.completed`;
  - `response.failed` and `error` become request errors;
  - `response.incomplete` with `max_output_tokens` is captured as finish metadata and does not crash the stream;
  - non-200 and malformed JSON errors are bounded and redacted;
  - Chat Completions compatibility tests still pass under `ChatCompletionsAPI`;
  - summaries split otherwise-identical records by requested service tier, and Markdown/CLI output displays requested and observed tiers.
- Update README after implementation:
  - the quick-start command uses Responses by default;
  - explain `--openai-api chat-completions` for OpenAI-compatible providers that lack `/responses`;
  - explain Responses stream TTFT semantics (`response.output_text.delta`, not metadata events);
  - explain Responses usage fields and the reasoning-token caveat;
  - document `--service-tier` and warn that different service tiers must be compared separately.

Implemented details:

- Added `openai.API` with `ResponsesAPI` as the default and `ChatCompletionsAPI` as an explicit compatibility mode.
- Added `openai.ServiceTier` values and validation for `auto`, `default`, `flex`, `scale`, and `priority`.
- Implemented direct HTTP `POST /responses` streaming without SDK usage, including Responses request construction, SSE event parsing, visible text/refusal delta TTFT semantics, terminal usage/cache capture, reasoning-token metadata, failure/error events, and `[DONE]` compatibility handling.
- Preserved the existing Chat Completions streaming implementation behind `Config.API: ChatCompletionsAPI` and added `service_tier` request/response capture for that path too.
- Added request-record, HTTP-record, summary-group, Markdown, CLI, and run metadata fields for requested and observed service tiers; summaries now group by requested service tier immediately.
- Added `what-ttft run --openai-api responses|chat-completions` and `--service-tier auto|default|flex|scale|priority` flags.
- Updated fake OpenAI server helpers, CLI tests, provider tests, report tests, smoke script, README, and integration test comments for Responses-default behavior.

Definition of done:

- `what-ttft run --provider openai ...` defaults to `/v1/responses` without requiring a new flag.
- `what-ttft run --provider openai --openai-api chat-completions ...` preserves the old Chat Completions streaming path.
- Unit and fake-server tests prove Responses metadata/role/tool/reasoning events do not count as TTFT.
- Usage/cache metadata and provider-reported actual service tier from `response.completed` appear in `requests.jsonl` and summaries when available.
- Requested service tier is present in request records, summary groups, Markdown, and `run.json` for the single-run CLI path.
- Configured service tier is sent for both Responses and Chat Completions compatibility mode, and omitted when unset.
- Summary grouping includes requested service tier immediately, before multi-target/YAML work starts.
- No provider SDK is used in the benchmark hot path.

---

### [x] 19. Make tokens-per-second first-class and rigorously named

Current state:

- `DerivedMetrics.E2EOutputTPS` exists.
- `SummaryGroup.SystemTPS` and `SummaryGroup.RPS` exist.
- Markdown summary includes `e2e_output_tps`, but the single-run CLI summary does not print it.
- There is no post-TTFT output-token throughput metric.

Implementation details:

- Keep `e2e_output_tps` unchanged and document it as per-request/user-perceived throughput including TTFT.
- Add a new optional request-level derived metric named `generation_delta_output_tps`:

  ```text
  generation_delta_output_tps = max(completion_tokens - 1, 0) / generation_delta_seconds
  ```

  Rules:

  - Compute only when provider-reported or estimated completion token count is available, `completion_tokens > 1`, `first_output_delta` exists, `last_output_delta` exists, and `last_output_delta > first_output_delta`.
  - Return nil/omit the field when inputs are missing, token count is zero/one, or generation duration is zero/non-positive.
  - Field documentation must state that timing bounds are visible-output delta timestamps, not true token timestamps, so this is a post-first-delta output-token throughput approximation. It must not be called `decode_tps`.
  - If the token count source is provider-reported, the numerator is provider-reported tokens. If a future tokenizer fallback estimates counts, the metric remains estimated and must be labeled through the usage source.

- Extend `DerivedMetrics`, `CalculateDerivedMetrics`, and `metrics_test.go`:
  - complete-timeline case with 10 completion tokens and a known generation delta;
  - nil when completion tokens are missing;
  - nil when completion tokens are 0 or 1;
  - nil when first/last output delta are missing;
  - nil when generation duration is zero;
  - preserve existing `e2e_output_tps` behavior.
- Extend `MetricDistributions`, summary builders, and `summary_test.go` to include `generation_delta_output_tps`.
- Update `internal/report/markdown.go` so detailed group tables include both:
  - `e2e_output_tps`;
  - `generation_delta_output_tps`.
- Update `cmd/what-ttft/run.go` summary printing to include:
  - `e2e_output_tps` p50/p95/p99/mean;
  - `generation_delta_output_tps` p50/p95/p99/mean;
  - group-level `system_tps` and `rps` when available.
- Update README metric definitions to explain the difference between user-perceived TPS, post-first-delta TPS, and system TPS.

Implemented details:

- Added `generation_delta_output_tps` to per-request derived metrics with nil semantics for missing token counts, zero/one output token, missing first/last visible output delta, and non-positive generation duration.
- Kept `e2e_output_tps` unchanged as user-perceived throughput including TTFT.
- Added `generation_delta_output_tps` to summary distributions, Markdown reports, JSON record tests, summary tests, and CLI output.
- Updated the CLI summary to print `e2e_output_tps`, `generation_delta_output_tps`, `system_tps`, and `rps`.
- Updated README metric descriptions to distinguish user-perceived TPS, post-first-delta output TPS, system TPS, and RPS, with a warning not to treat chunk/delta timing as true decode TPS or token ITL/TPOT.

Definition of done:

- Unit tests cover all new nil/non-nil TPS cases.
- Existing tests still pass without requiring provider usage.
- CLI and Markdown output display TPS metrics when usage is available and `-`/empty values when usage is unavailable.
- No metric is named `decode_tps`, `token_itl`, or `tpot` in v0.2 unless true token timestamps are implemented.

---

### [x] 20. Add target identity to request records and summaries

Multi-model and YAML benchmarks need a stable comparison label that is distinct from provider/model. Without this, two targets using the same model against different base URLs, deployments, regions, or API accounts could be summarized together incorrectly.

Implementation details:

- Add optional target metadata to `RequestRecord`:

  ```go
  type RequestRecord struct {
      TargetID string `json:"target_id,omitempty"`
      TargetName string `json:"target_name,omitempty"`
      // existing fields...
  }
  ```

  Field documentation requirements:

  - `TargetID` is a stable, sanitized benchmark-config identifier; empty means the record came from legacy/single-run execution.
  - `TargetName` is a human-readable target label; empty means no separate label was supplied.
  - Neither field may contain secrets.

- Add matching target fields to `SummaryGroup` and include `target_id` in the group key when non-empty, while preserving the requested service-tier grouping added in task 18.
- Update Markdown headings to include `target_id` and `target_name` when present:

  ```md
  ## target=gpt-5.5 provider=openai model=gpt-5.5 scenario=short cache=cache-bust connection=warm service_tier=default
  ```

- Add `RunConfig.RequestIDPrefix` or an equivalent runner option so a batch/multi-target caller can create globally unique request IDs without rewriting records after the fact.
  - Single-run default remains `req-000000`, `req-000001`, etc.
  - Batch runs should use deterministic prefixes such as `target-gpt-5.5-req-000000`.
  - Chunk records must keep joining through `request_id` without ambiguity.
- Add runner support for target identity:
  - either explicit fields on `RunConfig` (`TargetID`, `TargetName`, `RequestIDPrefix`), or a small `RunLabels` struct if that keeps `RunConfig` cleaner;
  - populate `RequestRecord.TargetID`/`TargetName` in `runOne`;
  - keep these fields empty for `what-ttft run` unless the caller explicitly sets them.
- Update JSON round-trip tests and summary tests:
  - two records with the same provider/model/scenario/cache/connection but different `target_id` must produce two groups;
  - two records with the same provider/model/scenario/cache/connection but different requested service tiers must produce two groups;
  - records without `target_id` keep existing grouping behavior;
  - request IDs are unique and chunk join remains correct when prefixes are set.

Implemented details:

- Added `TargetID`, `TargetName`, and `RequestIDPrefix` to `RunConfig` with secret-safety documentation.
- Added optional `target_id` and `target_name` fields to `RequestRecord` and copied them from the runner into every request record.
- Added target fields to `SummaryGroup` and included `target_id` in the summary group key while preserving service-tier grouping.
- Added deterministic prefixed request ID generation for runners, preserving the old `req-000000` shape when no prefix is configured.
- Updated Markdown group headings to include target ID/name when present.
- Added tests for JSON shape, target grouping, prefixed request IDs, chunk joins, concurrent runner prefixed IDs, and Markdown target headings.

Definition of done:

- `requests.jsonl` can represent multiple targets without duplicate request IDs.
- `summary.json` never mixes different non-empty `target_id` values or different requested service tiers.
- Existing single-run output remains backward-compatible except for additive optional JSON fields.

---

### [x] 21. Define the v0.2 YAML benchmark schema and loader

Add strict YAML configuration support for repeatable benchmarks. The schema should cover the user's requested multi-model use case first and leave obvious extension points for providers/scenarios later.

Proposed v0.2 schema:

```yaml
schema_version: 1
name: openai-short-model-compare

defaults:
  provider: openai
  api: responses
  base_url: https://api.openai.com/v1
  api_key_env: OPENAI_API_KEY
  service_tier: default
  include_usage: true        # chat-completions only; Responses usage is captured from terminal events
  legacy_max_tokens: false   # chat-completions only

run:
  samples: 50
  warmup: 5
  concurrency: 1
  cache_mode: cache-bust
  connection_mode: warm
  timeout: 120s
  save_chunks: false

scenario:
  name: short-capital
  system_prompt: You are a concise assistant.
  prompt: "Answer in one short sentence: what is the capital of France?"
  max_output_tokens: 64
  temperature: 0
  top_p: 1
  reasoning_effort: none

targets:
  - id: gpt-5.5
    model: gpt-5.5
  - id: gpt-5.2
    model: gpt-5.2
```

Schema rules:

- `schema_version` is required and must equal `1` for v0.2.
- `name` is optional but recommended; it is used in default output directory names and report metadata.
- `defaults` values are inherited by every target and may be overridden per target.
- `targets` is required and must contain at least one target.
- Each target requires a model after inheritance.
- Each target requires `provider: openai` after inheritance in v0.2. Other providers should fail with a clear validation error.
- Each OpenAI target uses `api: responses` by default. `api: chat-completions` is allowed only as an explicit compatibility mode for OpenAI-compatible endpoints that do not support `/responses`.
- Responses targets must send `/responses` requests and must not send Chat-only `stream_options.include_usage`, `max_completion_tokens`, or `max_tokens` fields.
- Chat Completions targets preserve existing `/chat/completions` behavior; `include_usage` and `legacy_max_tokens` only apply in this mode.
- Each target requires `api_key_env` after inheritance. Inline `api_key` is not supported in v0.2.
- Target `id` is optional only if it can be generated deterministically from provider/model/base-url index; generated IDs must be stable and collision-free.
- Duplicate target IDs are invalid.
- `scenario.prompt` is required and must be non-empty.
- `run.samples + run.warmup` must be greater than zero.
- Cache mode, connection mode, reasoning effort, OpenAI API mode, and service tier validation must reuse the same accepted values as the CLI.
- Unknown YAML keys must be errors, not ignored.
- Durations use Go duration strings such as `120s`, `2m`, or `500ms`; integer nanosecond durations should not be accepted in YAML because they are error-prone for humans.
- Optional floats such as `temperature` and `top_p` must preserve the difference between omitted and explicitly set zero.
- `defaults.service_tier` and per-target `service_tier` are optional OpenAI provider settings; when omitted after inheritance, OpenAI requests omit `service_tier` and OpenAI uses its default `auto` behavior. When present, the value must be one of `auto`, `default`, `flex`, `scale`, or `priority`.

Implementation details:

- Add a focused config package, preferably `internal/configfile` unless a public Go API for YAML configs is intentionally exposed.
- Add types with both `yaml` and `json` tags if they will be embedded in `run.json`.
- Use `go.yaml.in/yaml/v3` with `Decoder.KnownFields(true)`.
- Add a custom duration type for YAML duration strings.
- Add a custom optional-float type or pointer-based decoding so `temperature: 0` is preserved.
- Add validation that returns all practical config errors in one message where possible, or at least includes the YAML path, e.g. `targets[1].model is required`.
- Add config redaction/sanitization helpers:
  - API key values are never loaded from YAML;
  - `api_key_env` names may be written to `run.json`;
  - base URLs are redacted with existing report URL redaction before writing metadata;
  - target IDs/names are sanitized for paths but preserved in metadata if safe.
- Add fixtures under the package testdata, for example:
  - `valid_minimal.yaml`;
  - `valid_two_models.yaml`;
  - `invalid_unknown_field.yaml`;
  - `invalid_duplicate_target_id.yaml`;
  - `invalid_missing_prompt.yaml`;
  - `invalid_bad_duration.yaml`;
  - `invalid_bad_service_tier.yaml`;
  - `invalid_inline_api_key.yaml` if the schema explicitly rejects that field.

Implemented details:

- Added `internal/configfile` with strict YAML loading via `go.yaml.in/yaml/v3` and `Decoder.KnownFields(true)`.
- Added normalized config types for shared run settings, shared `whatttft.Scenario`, and inherited OpenAI target settings, including default `api: responses`, optional `service_tier`, `include_usage`, and `legacy_max_tokens`.
- Added validation for schema version, target presence, inherited provider/model/API-key-env requirements, duplicate sanitized target IDs, cache/connection modes, OpenAI API mode, service tier, reasoning effort, non-negative counts/timeouts, explicit-zero floats, and Responses `max_output_tokens` minimums.
- Added custom YAML duration parsing that accepts Go duration strings and rejects integer nanosecond-looking values.
- Rejected inline `api_key` fields with actionable errors, added target ID sanitization/generation, request-run config construction for targets, base URL redaction helpers, and config byte SHA-256 hashing.
- Added valid and invalid YAML fixtures plus loader tests covering defaults, inheritance/overrides, unknown fields, duplicate IDs, missing prompt/target fields, bad durations, bad service tiers, inline API keys, explicit chat-completions compatibility, generated IDs, redaction, and stable config hashes.

Definition of done:

- Loading a valid config yields normalized targets, a shared `whatttft.Scenario`, run settings, and OpenAI provider settings with inherited defaults applied, including `api: responses` when omitted and `service_tier` when provided.
- Unknown fields fail in tests.
- Duplicate/missing required fields fail in tests with actionable messages.
- No test fixture contains a real secret.
- `go.mod` uses `go.yaml.in/yaml/v3` and not `gopkg.in/yaml.v3`.

---

### [x] 22. Implement a multi-target benchmark runner/orchestrator

Build the library/CLI bridge that executes a YAML benchmark plan across multiple targets while reusing the existing single-provider `Runner` where practical.

Implementation details:

- Add an orchestration type, for example:

  ```go
  type BenchmarkTarget struct {
      ID       string
      Name     string
      Provider Provider
      Config   RunConfig
  }

  type BenchmarkConfig struct {
      Name    string
      Targets []BenchmarkTarget
  }

  type BenchmarkResult struct {
      Records []RequestRecord
      Chunks  []ChunkRecord
      Summary RunSummary
  }
  ```

  Exact names may differ, but the public API should make it easy for Go callers to run the same scenario/config against several providers/models without going through the CLI.

- Start with serial target execution in v0.2:
  1. validate all targets before sending any provider request;
  2. for each target in config order, run warmup and measured requests using `NewRunner`;
  3. append records/chunks into one combined result;
  4. compute one combined `RunSummary` over all records.
- Record execution order in report metadata so readers know whether the run was serial. Use a value such as `target_order: serial`.
- Ensure each target gets its own HTTP client/transport constructed from the shared connection mode and timeout:
  - warm connections are reused within a target run;
  - transports are not shared across different target base URLs unless that is explicitly added later;
  - cold mode still disables keepalives as in v0.1.
- Ensure each OpenAI target is constructed with the normalized API mode from YAML, defaulting to Responses and using Chat Completions only when explicitly requested.
- Ensure request IDs and target labels are set before each target run:
  - `TargetID` and `TargetName` from the YAML target;
  - deterministic request ID prefix based on sanitized target ID.
- Error behavior:
  - missing API key env vars, invalid target provider settings, or provider construction errors should fail before any target starts;
  - per-request provider errors remain request records and do not abort the whole benchmark;
  - context cancellation aborts promptly and returns partial combined results;
  - if one target completes with all request-level errors, still run subsequent targets unless the context is canceled or preflight failed.
- Add tests with fake providers:
  - two targets, same scenario, different models produce two summary groups;
  - same provider/model but different target IDs produce two groups;
  - warmups are excluded per target;
  - request IDs are unique across targets;
  - context cancellation returns partial combined results;
  - target preflight catches duplicate IDs and nil providers.

Implemented details:

- Added public multi-target benchmark API types in `pkg/whatttft`: `BenchmarkConfig`, `BenchmarkTarget`, `BenchmarkResult`, `TargetOrder`, `BenchmarkRunner`, `NewBenchmarkRunner`, and `RunBenchmark`.
- Implemented v0.2 serial target execution that preflights every target before any provider request, then runs each target through the existing single-provider `Runner` and combines records, chunks, and a grouped `RunSummary`.
- Added target ID sanitization, duplicate-ID detection, nil-provider validation, per-target run-config validation, target label propagation, and deterministic per-target request ID prefixes.
- Added partial-result behavior for context cancellation while keeping request-level provider errors as records and continuing normally when a target completes with request errors.
- Added a `BenchmarkResult.RunResult` conversion helper for existing report writers that accept `RunResult`.
- Added tests covering two-target runs, summary grouping by target/model, same-model target separation, warmup exclusion per target, request/chunk ID uniqueness, cancellation partial results, and preflight validation with no provider calls.

Definition of done:

- Go callers can run a multi-target benchmark without invoking the CLI.
- Combined summaries are grouped correctly by target and model.
- No request or chunk IDs collide across targets.
- The orchestrator does not introduce stdout/stderr writes in the hot path.

---

### [x] 23. Add the `what-ttft bench --config` CLI command

Expose YAML benchmarks through a dedicated CLI command while keeping `run` unchanged for single-model ad-hoc use.

Command shape:

```sh
what-ttft bench --config benchmark.yaml --out runs/openai-short-model-compare
```

Flags:

```text
--config PATH        required YAML benchmark config
--out DIR           optional output directory override
--overwrite         allow replacing a non-empty output directory
--dry-run           parse, validate, and print the normalized plan without sending requests
--save-chunks       optional override for run.save_chunks
--samples N         optional override for run.samples
--warmup N          optional override for run.warmup
--concurrency N     optional override for run.concurrency
--timeout DURATION  optional override for run.timeout
--service-tier TIER optional override for every OpenAI target service_tier; auto|default|flex|scale|priority
```

Implementation details:

- Update top-level usage to list `bench`:

  ```text
  run      benchmark one OpenAI-compatible model from flags
  bench    run a YAML benchmark across one or more targets
  version  print build version information
  ```

- Parse with the standard-library `flag` package to match v0.1.
- Require `--config`; fail with exit code 2 for parse/validation errors.
- `--dry-run` must:
  - validate YAML and environment-variable presence if practical;
  - print target IDs, provider/API/model/base URL, samples/warmup/concurrency/cache/connection, service tier, scenario name, and output directory;
  - not print API key values;
  - not send HTTP requests;
  - not create report files unless an explicit future flag requests plan export.
- Normal execution must:
  - load and validate YAML;
  - resolve API keys from `api_key_env` for each target;
  - build one OpenAI provider per target with the normalized API mode, defaulting to Responses;
  - run the multi-target orchestrator;
  - write combined reports;
  - print a concise comparison table.
- Suggested terminal comparison table columns:

  ```text
  target        api        tier     model        ok err ttft_p50 ttft_p95 e2e_p50 e2e_p95 e2e_output_tps_mean generation_delta_output_tps_mean generation_delta_output_tps_count system_tps rps
  gpt-5.5      responses  default  gpt-5.5      50 0   312.7    450.9    980.0   1300.4   58.2                 72.1                             50/50                             55.0       0.9
  gpt-5.2      responses  default  gpt-5.2      50 0   280.1    420.3    870.5   1201.8   64.0                 80.4                             50/50                             60.2       1.0
  ```

- Redact CLI args in metadata. `--config` path is not secret, but do not write resolved API key values.
- Add CLI tests:
  - help text for `bench`;
  - missing `--config` validation;
  - invalid YAML returns exit code 2;
  - `--dry-run` sends no HTTP requests;
  - two-target fake OpenAI benchmark writes reports and prints comparison rows;
  - output files do not contain fake API key values.

Implemented details:

- Added a `bench` subcommand to the top-level CLI and usage text while preserving the existing `run` command behavior.
- Implemented `what-ttft bench --config benchmark.yaml` with standard-library flag parsing, output overwrite checks, dry-run mode, and optional overrides for save chunks, samples, warmup, concurrency, timeout, and OpenAI service tier.
- Wired YAML configs through `internal/configfile`, resolved API keys from `api_key_env` before provider requests, built one OpenAI provider/client per target, and executed the public multi-target benchmark runner.
- Wrote combined report outputs through the existing report writer and printed a concise target comparison table with TTFT/E2E and TPS columns.
- Added dry-run plan printing with redacted base URLs and API key environment names only; no API key values are printed or written.
- Added CLI tests for help text, missing `--config`, invalid YAML exit code 2, dry-run with no HTTP requests or files, two-target fake OpenAI Responses benchmark output, service-tier override, report file creation, summary grouping, and secret non-leakage.

Definition of done:

- `go run ./cmd/what-ttft bench --help` works.
- `what-ttft bench --config valid.yaml` can benchmark two fake OpenAI targets in tests.
- `run` command behavior and tests remain unchanged.
- CLI output includes TPS columns when usage is available.

---

### [x] 24. Extend report metadata and Markdown for YAML/multi-target benchmarks

Make `run.json`, `summary.json`, and `summary.md` self-explanatory for a benchmark containing multiple targets.

Implementation details:

- Extend `internal/report.RunMetadata` with optional fields, for example:

  ```go
  type RunMetadata struct {
      BenchmarkName string `json:"benchmark_name,omitempty"`
      ConfigPath string `json:"config_path,omitempty"`
      ConfigSHA256 string `json:"config_sha256,omitempty"`
      TargetOrder string `json:"target_order,omitempty"`
      Targets []RunTargetMetadata `json:"targets,omitempty"`
      // existing fields...
  }
  ```

- Add `RunTargetMetadata` with documented fields:
  - target ID/name;
  - provider;
  - OpenAI API mode (`responses` by default or `chat-completions` compatibility mode);
  - requested service tier and observed/actual service tier when reported by the provider;
  - model;
  - redacted base URL;
  - API key env var name, not value;
  - include-usage / legacy-max-tokens flags;
  - optional provider-specific metadata map with secrets redacted.
- For single-model `run`, keep existing `Provider`, `Model`, `BaseURL`, `Scenario`, and `RunConfig` fields populated as before.
- For multi-target `bench`, populate:
  - `BenchmarkName`;
  - `ConfigPath` and SHA-256 of the YAML file bytes;
  - `TargetOrder` (`serial` for v0.2);
  - `Targets` array;
  - common `Scenario` and `RunConfig`.
- Update default output directory naming:
  - single `run`: current provider-model-scenario-cache-connection-timestamp behavior;
  - `bench`: prefer benchmark name, scenario, cache mode, connection mode, timestamp, e.g. `runs/openai-short-model-compare-short-capital-cache-bust-warm-20260519T...Z`.
- Add a high-level comparison table at the top of `summary.md` before detailed per-group metric tables:

  ```md
  | target | provider | api | requested tier | observed tier | model | ok | err | ttft p50 ms | ttft p95 ms | e2e p50 ms | e2e p95 ms | e2e_output_tps mean | generation_delta_output_tps mean | generation_delta_output_tps count | system tps | rps |
  |---|---|---|---|---|---|---:|---:|---:|---:|---:|---:|---:|---:|---:|---:|---:|
  ```

- Keep the existing detailed per-group metric tables after the comparison table.
- Add report tests:
  - multi-target `run.json` is parseable and redacts base URLs/secrets;
  - config hash is stable for known bytes;
  - Markdown includes target IDs and TPS columns;
  - single-run report output remains compatible.

Implemented details:

- Extended `report.RunMetadata` with benchmark name, YAML config path, config SHA-256, target execution order, and a documented per-target metadata array.
- Added `RunTargetMetadata` with target ID/name, provider, provider API, requested and observed service tiers, observed tier counts, model, redacted base URL, API-key env var name, usage/legacy-token flags, and provider-specific extra metadata.
- Updated report writing to redact target base URLs, enrich target metadata with observed service tiers from summary groups, and render Markdown with metadata-aware target comparison tables.
- Added benchmark-aware default output directory naming that uses benchmark name, scenario, cache mode, connection mode, and timestamp while preserving single-run naming.
- Updated the `bench` CLI metadata path to record benchmark name, config path, config hash, serial target order, and per-target OpenAI settings without storing API key values.
- Added report, Markdown, and CLI tests for multi-target `run.json`, config hash presence, target metadata redaction, observed tier enrichment, comparison Markdown target/TPS columns, and single-run compatibility.

Definition of done:

- A reader can open `run.json` and know exactly which YAML config and targets produced the run.
- `summary.md` is useful for model comparison without manually reading JSON.
- No API key values are written to any report file.

---

### [x] 25. Document YAML benchmarks, multi-model methodology, and TPS semantics

Update user-facing documentation after the CLI and report behavior exists.

Implementation details:

- Add a README section titled "YAML benchmark configs" with:
  - a complete two-model example using `api: responses`;
  - `what-ttft bench --config benchmark.yaml` command;
  - `--dry-run` example;
  - note that inline API keys are not supported and `api_key_env` should be used;
  - note that `api: chat-completions` is only for compatibility with endpoints that do not support `/responses`.
- Add a README section titled "Comparing multiple models" explaining:
  - all targets in a YAML benchmark share the same prompt, cache mode, generation settings, OpenAI API mode, connection mode, samples, warmup, and concurrency unless explicitly overridden by target/default fields or future schema versions;
  - serial target execution can be affected by time-of-day/provider-load changes;
  - for rigorous comparisons, run multiple independent passes and consider alternating target order manually until round-robin/randomized scheduling exists;
  - never compare cached and uncached targets in one conclusion.
- Document service tier control:
  - `--service-tier` for single-run benchmarks;
  - `defaults.service_tier` and target-level `service_tier` for YAML benchmarks;
  - allowed values `auto`, `default`, `flex`, `scale`, `priority`;
  - omitted value means do not send `service_tier`, allowing OpenAI's default `auto` behavior;
  - requested service tier and observed provider-reported service tier should be treated as benchmark variables and not mixed silently.
- Expand metric definitions for:
  - `e2e_output_tps`;
  - `generation_delta_output_tps`;
  - `system_tps`;
  - `rps`.
- Include a warning that post-first-delta TPS is not true decode TPS/token ITL because OpenAI-compatible streaming chunks are not guaranteed to equal tokens.
- Add a sample `benchmark.yaml` under `examples/` or `docs/` with fake model names and no secrets.

Implemented details:

- Added `examples/openai-model-compare.yaml`, a complete two-target OpenAI Responses benchmark config using fake model names and `api_key_env` only.
- Expanded README with a YAML benchmark config section, copy-paste two-model YAML, dry-run command, normal `bench` command, and `bench` override example.
- Documented why `run` remains useful for single-model/ad-hoc measurements while `bench` is for repeatable YAML and multi-target comparisons.
- Documented that OpenAI defaults to Responses API and that `api: chat-completions` is only a compatibility mode for endpoints without `/responses`.
- Added multi-model methodology notes about shared settings, serial target execution, repeated passes, manual target-order alternation, cache-mode separation, and service-tier separation.
- Expanded TPS semantics for `e2e_output_tps`, `generation_delta_output_tps`, `system_tps`, and `rps`, including the warning that post-first-delta TPS is not true decode TPS/token ITL/TPOT.
- Documented service-tier control for single-run and YAML benchmarks, allowed tier values, omitted-tier behavior, and requested/observed tier comparison warnings.

Definition of done:

- README contains a copy-paste YAML example and command.
- Documentation explains why `run` and `bench` both exist.
- Documentation says the OpenAI provider defaults to Responses API and explains how to opt into Chat Completions compatibility mode.
- Documentation distinguishes user TPS, post-first-delta TPS, and system TPS.
- Documentation explains service tier semantics and warns not to compare different service tiers as if they were the same traffic shape.

---

### [x] 26. Add quality gates and fake-server smoke coverage for `bench`

Mirror the v0.1 quality gate for the new multi-target path.

Implementation details:

- Add a script, for example `scripts/smoke-fake-openai-bench.sh`, that:
  - builds the CLI;
  - starts two deterministic fake OpenAI Responses-compatible SSE endpoints or one endpoint that accepts two model names;
  - writes a temporary YAML config with two targets using fake API key env vars, default `api: responses`, and a configured `service_tier` such as `default`;
  - runs `what-ttft bench --config ...`;
  - verifies `run.json`, `requests.jsonl`, `summary.json`, and `summary.md` exist;
  - verifies two target/model groups are present;
  - verifies `/responses` was used by default;
  - verifies configured `service_tier` is sent in requests and appears in metadata/summaries;
  - verifies TPS fields are present when Responses terminal usage events are emitted;
  - verifies fake API keys are absent from report files.
- Update README testing instructions to include the new smoke script.
- Run the full gate before marking v0.2 tasks complete:

  ```sh
  go test ./...
  go test -race ./...
  golangci-lint run
  go build ./...
  go run ./cmd/what-ttft run --help
  go run ./cmd/what-ttft bench --help
  scripts/smoke-fake-openai.sh
  scripts/smoke-fake-openai-bench.sh
  ```

Implemented details:

- Added `scripts/smoke-fake-openai-bench.sh`, a no-network smoke script that builds the CLI, starts a local deterministic Responses-compatible fake OpenAI server, writes a temporary two-target YAML config, runs `what-ttft bench`, and validates the generated reports.
- The bench smoke script verifies `/responses` is used by default, two target/model groups are present, configured `service_tier=default` is sent and reported, TPS distributions are present from terminal Responses usage events, and fake API key values are absent from all report files.
- Updated README testing instructions with the full local gate, `run` and `bench` help checks, and both fake-server smoke scripts.
- Ran the full v0.2 quality gate successfully, including both fake-server smoke scripts.

Definition of done:

- All commands pass locally.
- Fake bench smoke test uses no external network.
- Report files from smoke tests contain no API keys.
- `implementation-plan.md` v0.2 tasks are marked `[x]` as they are completed.

---

## v0.3 feature plan: event-driven execution and Bubble Tea live dashboards

The next milestone should add a live terminal experience without creating a separate `tui` command group. The user-facing shape is:

```sh
what-ttft run --tui [existing run flags]
what-ttft bench --config benchmark.yaml --tui [existing bench flags]
```

The architectural change underneath is more important than the initial UI: `run` and `bench` should emit structured benchmark events that can be consumed by the Bubble Tea dashboard and future in-process integrations. The event stream is a live side channel. It must not replace the canonical raw result files (`requests.jsonl`, optional `chunks.jsonl`, `run.json`, `summary.json`, and `summary.md`) until a later milestone deliberately adopts event sourcing.

### Research notes and adopted design decisions

Sources reviewed while planning v0.3:

- Bubble Tea repository and tutorial: <https://github.com/charmbracelet/bubbletea>
- Bubble Tea v2 upgrade guide: <https://github.com/charmbracelet/bubbletea/blob/main/UPGRADE_GUIDE_V2.md>
- Bubbles component library: <https://github.com/charmbracelet/bubbles>
- Lip Gloss layout/styling library: <https://github.com/charmbracelet/lipgloss>
- ntcharts terminal charting library: <https://github.com/NimbleMarkets/ntcharts>

Design decisions for this milestone:

- Do **not** add `what-ttft tui`, `what-ttft tui run`, or `what-ttft tui open` in v0.3. The TUI is a presentation mode of the existing commands, not a separate execution command group.
- Add `--tui` to both `run` and `bench`. The same benchmark plan, validation, provider construction, runner, report writer, and output directory behavior should be used whether or not `--tui` is enabled.
- Add a core event model that imports no Bubble Tea packages. Bubble Tea-specific code should adapt `whatttft.RunEvent` values into `tea.Msg` values at the CLI/TUI boundary.
- Keep the current `RunResult`/`BenchmarkResult` return values and report writers. Events are live progress/diagnostic messages and must not be the only source of truth.
- Events must be documented as **not suitable for latency math**. Request timelines and derived metrics are still calculated from per-request monotonic `Recorder` data. Event wall times are for UI ordering and external monitoring only.
- Do not emit per-chunk or per-token events by default. Chunk streams can be high volume and may contain sensitive generated content. v0.3 should focus on run, target, phase, request-completion, summary, and report-writing events. Stream/chunk debug events can be added later behind explicit opt-in flags.
- A slow TUI must not block hot benchmark paths. CLI code should use a bounded asynchronous event bus between runner observers and sinks. If a live-display sink cannot keep up, it may coalesce snapshots or drop non-critical progress events, but the benchmark must still write canonical result files.
- Request-completion events should carry the completed `RequestRecord` because it is already the standardized, redacted machine-readable unit. The TUI can compute live summaries by applying existing `Summarize` logic to completed records.
- For fixed-concurrency runs, live events may arrive in completion order. Final result records must remain deterministically sorted by attempt within a phase/target as they are today.
- Use Bubble Tea v2 import paths: `charm.land/bubbletea/v2`, `charm.land/bubbles/v2/...`, and `charm.land/lipgloss/v2`. `View()` methods must return `tea.View`, and alt-screen/mouse/window-title behavior should be declared on the returned view.
- The first v0.3 TUI should be a live dashboard, not a full interactive benchmark-config wizard. Configuration remains flags/YAML. Interactive forms can be a later milestone once execution events are solid.
- The dashboard should favor compact, trustworthy terminal visuals: progress bars, percentile bars, sparklines, histograms/ECDF approximations, target comparison tables, error/status tables, and slowest-request waterfalls. Use custom deterministic renderers first; add `ntcharts` only when it clearly improves a chart without making tests brittle.
- TUI cancellation should be explicit. Pressing `q`/`ctrl+c` during a running benchmark should ask for confirmation, cancel the benchmark context if confirmed, and write partial results when any records exist. Partial reports must be labeled as interrupted/canceled in the UI and command exit status.

### [x] 27. Define the public benchmark event model and observer interface

Add a small, documented public event surface that both single-target and multi-target runners can emit without depending on the CLI or TUI.

Files:

- `pkg/whatttft/events.go`
- `pkg/whatttft/events_test.go`

Implementation details:

- Add event enums and a JSON-serializable event schema. Exact field names may change during implementation, but the model should cover these concepts:

  ```go
  type RunEventKind string

  const (
      EventBenchmarkStarted  RunEventKind = "benchmark_started"
      EventBenchmarkFinished RunEventKind = "benchmark_finished"
      EventBenchmarkCanceled RunEventKind = "benchmark_canceled"
      EventBenchmarkFailed   RunEventKind = "benchmark_failed"

      EventRunStarted  RunEventKind = "run_started"
      EventRunFinished RunEventKind = "run_finished"
      EventRunCanceled RunEventKind = "run_canceled"
      EventRunFailed   RunEventKind = "run_failed"

      EventTargetStarted  RunEventKind = "target_started"
      EventTargetFinished RunEventKind = "target_finished"
      EventTargetFailed   RunEventKind = "target_failed"

      EventPhaseStarted  RunEventKind = "phase_started"
      EventPhaseFinished RunEventKind = "phase_finished"

      EventRequestScheduled  RunEventKind = "request_scheduled"
      EventRequestDispatched RunEventKind = "request_dispatched"
      EventRequestFinished   RunEventKind = "request_finished"

      EventSummaryUpdated RunEventKind = "summary_updated"

      EventReportWriteStarted  RunEventKind = "report_write_started"
      EventReportWriteFinished RunEventKind = "report_write_finished"
      EventReportWriteFailed   RunEventKind = "report_write_failed"
  )

  type RunPhase string

  const (
      PhaseWarmup   RunPhase = "warmup"
      PhaseMeasured RunPhase = "measured"
  )
  ```

- Prefer `request_dispatched` over `request_started` for runner-level events unless the implementation can emit from the exact provider `request_start` mark. This avoids confusing live UI events with the metric timeline event named `request_start`.
- Add an error payload type with bounded, redacted fields:

  ```go
  type RunEventError struct {
      Category string `json:"category"`
      Message  string `json:"message"`
      Retryable bool  `json:"retryable"`
  }
  ```

- Add a `RunEvent` struct with enough context for external consumers and the TUI:

  ```go
  type RunEvent struct {
      Sequence int64 `json:"sequence"`
      Kind RunEventKind `json:"kind"`
      WallUnixNano int64 `json:"wall_unix_nano"`

      BenchmarkName string `json:"benchmark_name,omitempty"`
      TargetID string `json:"target_id,omitempty"`
      TargetName string `json:"target_name,omitempty"`
      Provider string `json:"provider,omitempty"`
      Model string `json:"model,omitempty"`
      ScenarioName string `json:"scenario_name,omitempty"`
      CacheMode CacheMode `json:"cache_mode,omitempty"`
      ConnectionMode ConnectionMode `json:"connection_mode,omitempty"`
      RequestedServiceTier string `json:"requested_service_tier,omitempty"`

      Phase RunPhase `json:"phase,omitempty"`
      Attempt int `json:"attempt,omitempty"`
      Warmup bool `json:"warmup,omitempty"`
      RequestID string `json:"request_id,omitempty"`

      TotalRequests int `json:"total_requests,omitempty"`
      WarmupRequests int `json:"warmup_requests,omitempty"`
      MeasuredRequests int `json:"measured_requests,omitempty"`
      CompletedRequests int `json:"completed_requests,omitempty"`
      SuccessfulRequests int `json:"successful_requests,omitempty"`
      ErrorRequests int `json:"error_requests,omitempty"`
      ActiveRequests int `json:"active_requests,omitempty"`

      Record *RequestRecord `json:"record,omitempty"`
      Summary *RunSummary `json:"summary,omitempty"`
      Error *RunEventError `json:"error,omitempty"`
      OutputDir string `json:"output_dir,omitempty"`
      Message string `json:"message,omitempty"`
  }
  ```

  Field documentation must be exact:

  - `Sequence` is process-local event order, not a timestamp and not stable across reruns.
  - `WallUnixNano` is wall-clock Unix nanoseconds for event display/order; it must not be used for benchmark latency math.
  - `Record` is a completed request record and may contain generated chunk counts/metadata but not saved chunk content; chunk content remains controlled by `SaveChunks` and report writing.
  - `Summary` is a snapshot over records known at emission time; final `summary.json` remains authoritative.
  - `OutputDir` is a filesystem path and may reveal local directory names but must not contain secrets by construction.

- Add observer interfaces and helpers:

  ```go
  type RunObserver interface {
      OnRunEvent(context.Context, RunEvent)
  }

  type RunObserverFunc func(context.Context, RunEvent)
  ```

  `RunObserverFunc` should implement `OnRunEvent`.

- Add a no-op observer helper if it improves runner code clarity. Avoid a large public sink framework in this task.
- Add `RunEvent.Clone` or equivalent defensive-copy helper only if needed to keep async sinks from mutating shared pointers. If events include pointers to `RequestRecord` or `RunSummary`, tests must prove async consumers cannot observe later unintended mutations.
- Add JSON round-trip tests for representative events:
  - benchmark started;
  - target started;
  - request finished with a record;
  - summary updated;
  - report write failed with a redacted error;
  - event with service tier and target ID.
- Add tests that `RunObserverFunc` is invoked and that nil observers are safe through helper code.

Implemented details:

- Added `pkg/whatttft/events.go` with documented `RunEventKind`, `RunPhase`, `RunEventError`, `RunEvent`, `RunObserver`, and `RunObserverFunc` types.
- Added lifecycle/progress/reporting event kinds for benchmark, run, target, phase, request, summary, and report-writing events without importing Bubble Tea or any CLI/TUI package.
- Used pointer fields for request attempt and warmup flags so attempt zero and measured `false` values survive JSON round trips without ambiguity.
- Added `RunEvent.Clone` plus defensive-copy helpers for attached request records, summaries, timelines, maps, pointer metrics, cache metadata, and event errors so asynchronous consumers can isolate event snapshots.
- Added nil-safe observer notification helper behavior and `RunObserverFunc` support for future runner/event-bus wiring.
- Added JSON shape tests for representative benchmark, target, request-finished, summary, and report-write-failed events, plus clone isolation and nil observer tests.

Definition of done:

- The event schema compiles, is documented, and has JSON shape tests.
- No Bubble Tea, Bubbles, or Lip Gloss import appears under `pkg/whatttft`.
- Event docs explicitly warn that event wall times are not benchmark metric timings.

---

### [x] 28. Add runner and benchmark observer options, then emit core events

Wire the event model into existing single-target and multi-target execution while preserving current public constructors and return values.

Files:

- `pkg/whatttft/runner.go`
- `pkg/whatttft/runner_concurrent.go`
- `pkg/whatttft/benchmark.go`
- `pkg/whatttft/events.go`
- matching tests:
  - `pkg/whatttft/runner_test.go`
  - `pkg/whatttft/runner_concurrent_test.go`
  - `pkg/whatttft/benchmark_test.go`

Implementation details:

- Add options without breaking existing callers:

  ```go
  type RunnerOptions struct {
      Observer RunObserver
  }

  func NewRunnerWithOptions(provider Provider, cfg RunConfig, options RunnerOptions) *Runner
  ```

  `NewRunner(provider, cfg)` should continue to work and should call the options constructor with zero options.

- Add benchmark-level options without breaking existing callers:

  ```go
  type BenchmarkOptions struct {
      Observer RunObserver
  }

  func NewBenchmarkRunnerWithOptions(cfg BenchmarkConfig, options BenchmarkOptions) *BenchmarkRunner
  func RunBenchmarkWithOptions(ctx context.Context, cfg BenchmarkConfig, options BenchmarkOptions) (*BenchmarkResult, error)
  ```

  Existing `NewBenchmarkRunner` and `RunBenchmark` should delegate to the options path.

- Add a small internal event emitter on `Runner`/`BenchmarkRunner` that:
  - assigns monotonically increasing `Sequence` values;
  - populates shared run context fields consistently;
  - handles nil observers;
  - never panics when observers are nil;
  - snapshots records/summaries before handing them to observers if mutation is possible.

- Single-target runner event semantics:
  - `run_started`: after config/provider validation, before the first warmup or measured request is scheduled. Include provider, model, scenario, cache mode, connection mode, requested service tier if known, warmup count, measured count, total count, and concurrency.
  - `phase_started`: once for warmup when `WarmupRequests > 0`, and once for measured when `MeasuredRequests > 0`.
  - `request_scheduled`: immediately after `scheduled_at` is marked and before the job is sent to a worker or run sequentially.
  - `request_dispatched`: immediately before `provider.StreamChat` is invoked. Document that this is not the HTTP `request_start` metric event.
  - `request_finished`: after `RequestRecord` and optional chunk records are complete. Include the completed `RequestRecord`, target/run labels, attempt, warmup, phase, and updated completion/success/error counters where practical.
  - `summary_updated`: after measured request completions or phase completion. For v0.3 it is acceptable to emit this after each `request_finished` and at final run completion.
  - `phase_finished`: after all requests in a phase are complete or the phase is interrupted.
  - `run_finished`: after the runner summary is final when no run-level error occurred.
  - `run_canceled`: when the context is canceled; include partial counts and a redacted event error.
  - `run_failed`: for validation or non-cancellation run-level errors; request-level provider errors should still primarily be represented as `request_finished` records with `record.error`.

- Fixed-concurrency live behavior:
  - emit request events as workers schedule/dispatch/finish jobs, not only after sorting the phase output;
  - keep the final `RunResult.Records` and chunks deterministically sorted by attempt exactly as before;
  - if maintaining live partial summaries during concurrent execution would introduce contention, compute them only from completed records on a single collector goroutine, not from workers directly;
  - tests must tolerate completion-order events while asserting final record order.

- Multi-target benchmark event semantics:
  - `benchmark_started`: after all target preflight validation passes and before the first target starts;
  - `target_started`: before invoking the single-target runner for that target;
  - per-target single-run events should include `TargetID`/`TargetName` and request ID prefixes as they do in records;
  - `target_finished`: after a target result is appended to the combined benchmark result;
  - `benchmark_finished`: after combined summary is final;
  - `benchmark_canceled`/`benchmark_failed`: for context cancellation or run-level benchmark errors, with partial counts when available.

- Error/cancellation behavior:
  - preserve existing partial-result returns on context cancellation;
  - do not transform per-request provider errors into run-level failures;
  - if observer code itself panics, the runner should not recover silently unless a wrapper explicitly handles it. The recommended CLI path will isolate sinks in an async event bus.

- Add tests:
  - sequential runner emits run/phase/request/summary/run-finished events in expected order;
  - warmup and measured phases are labeled correctly;
  - request-level provider errors produce `request_finished` events with `record.error` and do not produce `run_failed`;
  - context cancellation emits `run_canceled` and returns partial results;
  - fixed-concurrency emits one `request_finished` per attempted request and final result order remains deterministic;
  - benchmark runner emits benchmark/target events and propagates target identity into per-request events;
  - nil observer does not change runner behavior.

Implemented details:

- Added `RunnerOptions` and `NewRunnerWithOptions` while preserving `NewRunner` as a zero-options wrapper.
- Added `BenchmarkOptions`, `NewBenchmarkRunnerWithOptions`, and `RunBenchmarkWithOptions` while preserving existing benchmark constructors/functions as zero-options wrappers.
- Added an internal event emitter that assigns process-local sequence numbers, fills wall-clock event timestamps, handles nil observers, and emits defensive event snapshots.
- Emitted single-target run events for run start/finish/cancel/failure, phase start/finish, request scheduled/dispatched/finished, and live summary updates.
- Emitted fixed-concurrency request events from workers as requests are scheduled, dispatched, and completed while preserving deterministic sorted final result records.
- Emitted multi-target benchmark events for benchmark start/finish/cancel/failure plus target start/finish/failure while reusing the same event sequence across nested target runners.
- Preserved existing request-level provider-error behavior: provider errors remain request records and `request_finished` events, not run-level failures.
- Added tests for sequential event order, warmup/measured labeling, request errors, cancellation, concurrent request-finished events, benchmark/target events, target identity propagation, and benchmark cancellation.

Definition of done:

- Existing runner and benchmark APIs continue to compile for old callers.
- Event tests cover sequential, concurrent, and multi-target paths.
- Final result summaries are unchanged except for event side effects.
- `go test -race ./pkg/whatttft` passes.

---

### [x] 29. Add a bounded asynchronous event bus for CLI/TUI consumers

The CLI should fan out runner events to the TUI without letting slow rendering perturb benchmark timing.

Files:

- `internal/eventbus/eventbus.go`
- `internal/eventbus/eventbus_test.go`

Implementation details:

- Add an internal event bus with a small interface for sinks:

  ```go
  type Sink interface {
      Publish(context.Context, whatttft.RunEvent) error
      Close(context.Context) error
  }
  ```

  The exact names may differ, but the bus should be internal to the CLI/reporting layer for v0.3.

- The event bus should itself implement `whatttft.RunObserver` so it can be passed directly to runner options.
- Use a bounded channel between runner observer calls and sink processing. The default capacity should be large enough for typical `run`/`bench` executions, e.g. 1024 or configurable through code.
- Define event delivery policy explicitly:
  - critical lifecycle and request-completion events should be strongly preferred;
  - summary/progress events may be coalesced or dropped if the UI sink is behind;
  - no event delivery policy changes the canonical request/chunk/report files;
  - if events are dropped, emit or expose a diagnostic dropped-count event/message to sinks that remain connected.

- Keep the v0.3 implementation simple but safe:
  - it is acceptable to make the bus non-blocking and count drops for all events if the buffer is full, as long as docs/tests state that event streams are best-effort live telemetry;
  - do not block request streaming or provider parsing on TUI rendering.

- Do not add a public persisted event sink in v0.3. Canonical report files already provide machine-readable analysis data, and a persisted best-effort live event log could confuse users about which files are authoritative.

- Tests:
  - bus delivers events to one and multiple sinks;
  - slow sink cannot deadlock publisher;
  - dropped-count behavior is visible and deterministic under tiny buffer capacity;
  - sink close is called;
  - sink publish and close errors are surfaced.

Implemented details:

- Added `internal/eventbus` with a documented `Sink` interface, `Options`, and `Bus` that implements `whatttft.RunObserver`.
- Implemented bounded asynchronous fanout with non-blocking `OnRunEvent`, default queue capacity, nil-sink filtering, dropped-event counting, safe close, queue draining, and sink error aggregation.
- Deliberately omitted a persisted event sink from v0.3 because canonical `requests.jsonl` and `summary.json` remain the machine-readable analysis outputs.
- Added tests for multiple sinks, slow-sink non-deadlock behavior, deterministic drops with tiny capacity, close behavior, and publish/close errors.

Definition of done:

- CLI code can pass one observer to runners and fan out to multiple consumers.
- Slow or failing sinks cannot leave benchmark goroutines permanently blocked.
- The bus remains internal plumbing for TUI/live consumers and does not create another persisted output format.

---

### [x] 30. Refactor `run` and `bench` command execution to accept observers, contexts, and report-write events

Prepare the existing commands for `--tui` by separating benchmark execution, event fanout, report writing, and final console output.

Files:

- `cmd/what-ttft/run.go`
- `cmd/what-ttft/bench.go`
- `cmd/what-ttft/main.go`
- `cmd/what-ttft/run_test.go`
- `cmd/what-ttft/bench_test.go`

Implementation details:

- Add CLI config fields for event/TUI settings:

  ```go
  type runCLIConfig struct {
      // existing fields...
      tui bool
  }

  type benchCLIConfig struct {
      // existing fields...
      tui bool
  }
  ```

- Add `--tui` to both `run` and `bench` help text, even if it is wired to a placeholder until task 33/34.
- Refactor execution helpers so tests and TUI launchers can call the same code:

  ```go
  type commandExecution struct {
      Result *whatttft.RunResult // or BenchmarkResult for bench path
      Metadata report.RunMetadata
      OutputDir string
      Err error
      Canceled bool
      Partial bool
  }
  ```

  Exact shapes can differ, but the commands need a clear way to write partial reports after cancellation.

- Add observer/context parameters to execution helpers:

  ```go
  func executeRun(ctx context.Context, cfg runCLIConfig, args []string, observer whatttft.RunObserver) (*whatttft.RunResult, report.RunMetadata, error)
  func executeBench(ctx context.Context, plan *configfile.Config, cliCfg benchCLIConfig, args []string, observer whatttft.RunObserver) (*whatttft.BenchmarkResult, report.RunMetadata, error)
  ```

  Preserve existing tests by updating call sites.

- Emit CLI/report events around report writing through the same bus:
  - `report_write_started` before `report.WriteRun`;
  - `report_write_finished` after success, including resolved output directory;
  - `report_write_failed` on error with redacted message.

- Partial/canceled report behavior:
  - if a context cancellation returns a non-nil result with at least one record, write partial report files unless output directory validation fails;
  - label the final console/TUI status as canceled/interrupted;
  - return an interrupted exit code. Prefer `130` for user cancellation if practical; otherwise document the chosen code;
  - do not write empty report directories when no records were produced unless this is explicitly documented.

- Non-TUI behavior should remain familiar:
  - without `--tui`, commands still print concise final summaries and output directory path;
  - output directory preflight still happens before sending provider requests;
  - `--dry-run` on `bench` must not start event bus sinks that write files unless explicitly requested.

- Add test seams for TUI launch:
  - avoid running a real Bubble Tea alt-screen program in normal CLI unit tests;
  - use an injectable `liveRunner`/`tuiLauncher` function variable or interface in `cmd/what-ttft` tests to assert `--tui` would be invoked with the right config and event channel.

- Tests:
  - `run --help` and `bench --help` include `--tui`;
  - canceled run with partial records writes partial reports and returns the documented interrupted code;
  - non-TUI run/bench behavior remains compatible.

Implemented details:

- Added `--tui` parsing and help text for both `run` and `bench`, plus top-level usage examples for TUI presentation mode.
- Split command execution into context-aware execution/write/finalization helpers, with observer parameters for both single-run and YAML benchmark paths.
- Added injectable run and bench TUI launcher seams that receive validated configs plus an `Execute(ctx, observer)` callback; the default implementation is a clear placeholder until tasks 33/34 wire Bubble Tea.
- Added command-level event sequencing and report lifecycle events for `report_write_started`, `report_write_finished`, and `report_write_failed` around canonical report writing.
- Implemented interrupted exit code `130` for context cancellation, partial report writing when at least one record exists, and canceled/interrupted console messages without creating empty report directories.
- Added CLI tests for `--tui` help, launcher injection, report-write event visibility, increasing event sequences, and partial canceled runs writing canonical report files.

Definition of done:

- Existing CLI behavior remains unchanged unless `--tui` is provided.
- Report write lifecycle events are available to live consumers.
- Partial cancellation behavior is explicit and tested.

---

### [x] 31. Add Bubble Tea dependencies and a minimal internal TUI application skeleton

Introduce Bubble Tea v2 in an internal package without wiring it into the commands yet.

Files:

- `internal/tui/doc.go`
- `internal/tui/app.go`
- `internal/tui/events.go`
- `internal/tui/keys.go`
- `internal/tui/styles.go`
- `internal/tui/store.go`
- `internal/tui/app_test.go`
- `internal/tui/store_test.go`

Dependencies:

```sh
go get charm.land/bubbletea/v2 charm.land/bubbles/v2 charm.land/lipgloss/v2
```

Implementation details:

- Add a package comment explaining that `internal/tui` renders live benchmark events and must not perform provider requests or benchmark timing itself.
- Add a TUI event adapter:

  ```go
  type runEventMsg struct {
      Event whatttft.RunEvent
  }
  ```

  This type stays internal; core events remain `whatttft.RunEvent`.

- Add a root model that stores:
  - terminal width/height from `tea.WindowSizeMsg`;
  - current screen/pane focus;
  - live run store;
  - running/completed/canceled/error state;
  - whether a cancel confirmation is open;
  - help/keymap state.

- `Init` should start waiting for events from a channel and optionally start a low-frequency UI tick. Avoid high-frequency ticks by default.
- `Update` should handle:
  - `tea.WindowSizeMsg` for responsive layout;
  - `runEventMsg` to update store;
  - `tea.KeyPressMsg` for quit, cancel confirmation, tab/focus navigation, help toggle, and chart view selection;
  - internal event-channel closed messages.

- `View` must return `tea.View` and set:
  - `AltScreen = true`;
  - `WindowTitle = "what-ttft"` or a target-specific title when available;
  - `MouseMode = tea.MouseModeCellMotion` only if mouse interactions are implemented. Otherwise leave mouse off for v0.3 simplicity.

- Add deterministic placeholder rendering first:
  - header with benchmark/run name;
  - progress counts;
  - current target/model;
  - status line;
  - minimal help footer.

- Do not read/write files from the TUI model. It receives events and returns user intents through messages/callbacks.
- Do not import provider packages from `internal/tui`.
- Tests:
  - window size updates model dimensions;
  - run/target/request/report events update store;
  - `View()` returns a non-empty `tea.View` with `AltScreen` set;
  - `q`/`ctrl+c` moves to confirmation state while running and quits immediately after completion;
  - no test requires a real terminal.

Implemented details:

- Added Bubble Tea v2, Bubbles v2, and Lip Gloss v2 dependencies using `charm.land/.../v2` import paths.
- Added `internal/tui` package documentation stating that the TUI consumes live events and must not execute benchmarks, call providers, write reports, or own timing math.
- Added an internal `runEventMsg` adapter and event-channel close message while keeping core event values as `whatttft.RunEvent`.
- Added a Bubble Tea v2 root model with terminal size state, pane/focus state, live event store, running/completed/canceled/error flags, cancel confirmation, keymap/help state, and deterministic placeholder rendering.
- Implemented `Init`, `Update`, and `View` with `tea.WindowSizeMsg`, `runEventMsg`, `tea.KeyPressMsg`, event-channel close handling, `tea.View` output, alt-screen mode, window titles, and mouse mode left disabled.
- Added Lip Gloss styles and Bubbles key/help bindings for help, quit/cancel confirmation, focus navigation, and pane selection.
- Added a live store that tracks context labels, active request IDs, completed request records, summary snapshots, report status, progress counts, and defensive copies of records/maps.
- Added unit tests for resize handling, event-driven store updates, alt-screen view output, quit/cancel behavior, help/pane keys, event-channel commands, store progress, record-copy isolation, summary-copy isolation, and target label fallbacks without requiring a terminal.

Definition of done:

- `internal/tui` compiles with Bubble Tea v2.
- The package has unit tests for model update/render behavior.
- No benchmark execution, provider request, or report-writing code is embedded in the TUI model.

---

### [x] 32. Implement live dashboard store, summaries, and terminal chart renderers

Build the reusable state and chart pieces that make the TUI useful for `what-ttft` metrics.

Files:

- `internal/tui/store.go`
- `internal/tui/charts/sparkline.go`
- `internal/tui/charts/histogram.go`
- `internal/tui/charts/percentile.go`
- `internal/tui/charts/waterfall.go`
- `internal/tui/charts/heatmap.go` or `target_table.go`
- matching `*_test.go` files

Implementation details:

- Store behavior:
  - maintain completed request records by request ID;
  - maintain per-target/per-group request counts;
  - track active request IDs when scheduled/dispatched events arrive;
  - track slowest successful requests by `ttft_delta_ms`, `e2e_delta_ms`, and optionally `stream_total_ms`;
  - track error categories/status codes;
  - track latest `RunSummary` snapshot when `summary_updated` events arrive;
  - recompute summaries from completed records using `whatttft.Summarize` when needed and when this is cheap enough.

- The store must never mutate `RequestRecord` values after adding them. Copy records as needed so tests can catch aliasing bugs.
- Add view-model helper methods for the UI:
  - `Progress()` returning total/warmup/measured/completed/success/error counts;
  - `Groups()` returning summary groups sorted in stable display order;
  - `MetricRows()` for TTFT, E2E, HTTP TTFB, TPS, and server wait;
  - `SlowestRequests(n int)`;
  - `StatusCounts()`;
  - `CurrentTarget()`.

- Chart renderers should be pure functions returning strings. They should accept width/height and degrade gracefully when the terminal is small.
- Implement at least these chart types for v0.3:
  1. **Sparkline over request order** for `ttft_delta_ms` and `e2e_delta_ms`.
  2. **Percentile bars/lollipop chart** for p50/p90/p95/p99 across target groups.
  3. **Histogram** for TTFT or E2E distribution.
  4. **Request waterfall** for a selected slow request, using available timeline events.
  5. **Target comparison table/heatmap-like coloring** for `bench --tui`.

- Waterfall chart phases:
  - DNS;
  - TCP;
  - TLS;
  - connection acquire;
  - request write;
  - server wait to first byte;
  - first byte to first SSE event;
  - first response byte or first event to first output delta;
  - first output delta to last output delta.

  Missing phases should be shown as `-`/omitted, not zero.

- Chart labeling rules:
  - label chunk/delta cadence as chunks/deltas, never token ITL unless token timestamps exist in a future milestone;
  - show units (`ms`, `tokens/s`, `req/s`) in chart titles or axes;
  - if usage is missing, TPS charts should show unavailable values clearly.

- Styling:
  - use Lip Gloss for color and borders;
  - support low-color/no-color terminals by keeping charts understandable without color;
  - avoid Unicode-only assumptions where possible, but Braille/sparkline runes are acceptable with simple fallback tests if needed.

- Tests:
  - chart functions handle empty input, one value, many values, NaN/Inf avoidance, and tiny widths;
  - percentile bars produce deterministic strings for known values;
  - histogram bins are deterministic;
  - waterfall omits missing phases and labels observed phases correctly;
  - store summaries match `whatttft.Summarize` for a known record set;
  - target comparison order is stable.

Implemented details:

- Extended the live TUI store with stable completed-record snapshots, recomputed `whatttft.Summarize` groups, progress counters derived from records when live counts are unavailable, metric rows, slowest request/metric rows, status-code/error-category counts, and current-target helpers.
- Deepened store defensive copies for request records, cache/HTTP/timeline/derived metric pointers, summary groups, distributions, connection maps, and cache summaries so live state is isolated from caller mutation.
- Added `internal/tui/charts` with pure deterministic renderers for sparklines, histograms, percentile bars, request waterfalls, and target comparison tables.
- Added request waterfall phases for DNS, TCP, TLS, connection acquire, request write, server wait to first byte, first byte to first SSE, stream protocol to first output, and visible-generation deltas, with missing phases omitted rather than rendered as zero.
- Updated the placeholder TUI panes to render metric rows, TTFT sparkline/histogram, E2E sparkline/TPS rows, slowest-request waterfalls, and target comparison tables from completed request records.
- Added unit tests for chart empty/one/many/tiny/non-finite behavior, deterministic percentile/histogram output, waterfall phase omission, target table ordering/unavailable TPS markers, store summary equivalence with `whatttft.Summarize`, metric rows, slowest requests, status counts, and copy isolation.

Definition of done:

- The TUI can render meaningful benchmark metrics from a slice/stream of completed request records.
- Charts have deterministic unit tests and do not require a terminal.
- Labels preserve the project's metric terminology discipline.

---

### [x] 33. Wire `what-ttft run --tui` to the live dashboard

Connect the single-run CLI path to Bubble Tea using the event bus and shared execution code.

Files:

- `cmd/what-ttft/run.go`
- `cmd/what-ttft/main.go` if needed for shared signal handling
- `internal/tui/app.go`
- `internal/tui/runner.go` or equivalent launcher file
- CLI and TUI tests

Implementation details:

- `what-ttft run --tui ...` should:
  1. parse and validate existing flags;
  2. resolve and preflight the output directory before starting provider requests;
  3. create a cancelable context;
  4. create an event bus with at least one TUI sink/channel;
  5. start the benchmark execution in a goroutine;
  6. run the Bubble Tea program in the foreground;
  7. wait for benchmark/report completion when the UI exits normally;
  8. print a concise final line after leaving alt screen, especially the output directory or cancellation/error status.

- TUI sink behavior:
  - convert `whatttft.RunEvent` values to internal `runEventMsg` values;
  - use `Program.Send` only from a controlled goroutine/sink, not directly from provider or runner hot paths;
  - if the Bubble Tea program exits before the benchmark finishes, either cancel the benchmark or detach only if a future explicit mode is implemented. For v0.3, prefer cancel-with-confirmation.

- Keyboard behavior for single-run TUI:
  - `?`: toggle help;
  - `tab`/`shift+tab`: move focus between summary, charts, slowest requests, and log/status panes;
  - `1`: summary dashboard;
  - `2`: TTFT distribution;
  - `3`: E2E/TPS distribution;
  - `4`: slowest-request waterfall;
  - `q`/`ctrl+c`: if running, open cancel confirmation; if completed, quit;
  - `esc`: close confirmation/help/detail pane;
  - `y`/`n`: confirm or reject cancellation when confirmation is open.

- Single-run dashboard content:
  - header: provider, API, model, scenario, cache mode, connection mode, service tier, output directory if known;
  - progress: warmup/measured counts, active requests, errors;
  - metric cards/table: HTTP TTFB, provider processing when available, server wait, TTFT delta, E2E delta, e2e TPS, generation-delta TPS, system TPS, RPS;
  - chart area: TTFT sparkline/histogram or percentile chart depending on pane;
  - status/error table: HTTP statuses, error categories;
  - slowest requests list and request waterfall.

- Completion behavior:
  - after runner finishes, reports should be written while the TUI shows `writing reports...`;
  - after `report_write_finished`, show the output directory and a `press q to exit` prompt;
  - if report writing fails, show a clear error and return non-zero after exit.

- Cancellation behavior:
  - confirmed cancel calls the benchmark context cancel function;
  - partial records are written when available;
  - TUI clearly labels the output as partial/canceled;
  - final process exit code follows task 30's documented interrupted code.

- Terminal behavior:
  - if `--tui` is requested in a non-interactive environment and Bubble Tea cannot initialize, return a clear error instead of silently falling back;
  - logs/debug output should use Bubble Tea file logging only when explicitly configured through an environment variable such as `DEBUG` or `WHAT_TTFT_TUI_DEBUG`.

- Tests:
  - CLI parser accepts `--tui` with existing run flags;
  - an injected fake TUI launcher receives events and returns success;
  - fake TUI cancellation cancels context and writes partial reports when records exist;
  - fake OpenAI run with `--tui` path executes the same provider/runner/report code as non-TUI;
  - final stdout after TUI exit includes output directory and no API key.

Implemented details:

- Replaced the `run --tui` placeholder with a real launcher that creates a cancelable benchmark context, event bus, TUI event sink/channel, benchmark execution goroutine, foreground Bubble Tea dashboard, and final post-alt-screen command status output.
- Added `internal/tui.Run` and `RunOptions` to launch the Bubble Tea v2 dashboard without embedding benchmark execution, provider calls, or report writing in the TUI package.
- Added `internal/tui.EventSink` to bridge `whatttft.RunEvent` values from `internal/eventbus` into the dashboard event channel with cloned best-effort delivery and dropped-event counting.
- Wired confirmed TUI cancellation (`q`/`ctrl+c`, then `y`) to call the benchmark cancel function; early UI exit also cancels any still-running benchmark before waiting for partial report writing.
- Preserved canonical report behavior: reports are still written through the shared run execution path, `report_write_*` events update the dashboard, partial canceled runs use exit code `130`, and non-TUI output remains unchanged.
- Added CLI tests for fake TUI execution through the real run callback, fake TUI cancellation producing partial reports, final stdout output directory reporting, and API-key non-leakage.
- Added TUI runner/sink tests for cloned event forwarding, non-blocking drops when the channel is full, channel close behavior, and cancel callback invocation.

Definition of done:

- `what-ttft run --tui` works against the fake OpenAI server without using provider SDKs.
- The TUI displays live request progress and final metrics.
- Non-TUI `run` output remains unchanged except for documented new flags.

---

### [x] 33.5. Redesign the live TUI as a full-screen realtime chart dashboard

The first wired TUI is functional but not the desired UX. Before wiring `bench --tui`, redesign the dashboard so `run --tui` and future `bench --tui` share a full-screen chart-first layout.

User-facing target shape:

```text
┌──────────────────────────────────────────────────────────────────────────────┐
│ what-ttft  provider=openai api=responses model=gpt-x scenario=short running  │
├──────────────────────────────────────────────────────────────────────────────┤
│                                                                              │
│ LIVE CHART AREA                                                              │
│                                                                              │
│ run --tui:                                                                    │
│   TTFT over request order        E2E over request order                       │
│   TTFT histogram                 slowest-request waterfall                    │
│                                                                              │
│ bench --tui later:                                                            │
│   target comparison table        target percentile bars                       │
│   selected/current target charts                                             │
│                                                                              │
├──────────────────────────────────────────────────────────────────────────────┤
│ METRICS PANEL (always visible, pinned to bottom)                             │
│ metric                          count      p50      p95      p99      mean   │
│ http_ttfb_ms                    12         ...      ...      ...      ...    │
│ ttft_delta_ms                   12         ...      ...      ...      ...    │
│ e2e_delta_ms                    12         ...      ...      ...      ...    │
│ e2e_output_tps                  12         ...      ...      ...      ...    │
│ generation_delta_output_tps     8          ...      ...      ...      ...    │
│ system_tps=... rps=... active=... ok=... err=... reports=... output=...      │
└──────────────────────────────────────────────────────────────────────────────┘
```

Architecture requirements:

- Keep the existing event flow:
  - runners emit `whatttft.RunEvent` values;
  - CLI code fans events through `internal/eventbus`;
  - `internal/tui.EventSink` forwards cloned events into the TUI channel;
  - the TUI model consumes events and renders state.
- Do **not** move benchmark execution, provider requests, or report writing into `internal/tui`.
- Do **not** use event wall-clock times for metric math. Charts and metric panels must use completed `RequestRecord` timelines, derived metrics, and `whatttft.Summarize` results.
- Realtime updates are request-completion realtime in v0.3:
  - update charts on `request_finished` and `summary_updated` events;
  - do not emit or display per-chunk/per-token charts unless a future explicit debug event stream is added;
  - label visible-output/delta cadence correctly and never call chunk timing token ITL/TPOT.
- The TUI must use the maximum width and height reported by `tea.WindowSizeMsg`:
  - render into a deterministic root canvas sized to `width x height` when both are positive;
  - preserve meaningful output for tiny terminals by truncating/omitting lower-priority content;
  - never let rendered lines exceed the available width in normal layouts;
  - use all available vertical space by allocating extra rows to the chart area.
- Layout should be centralized and testable. Add layout primitives/types such as:

  ```go
  type layoutBox struct {
      Width  int
      Height int
  }

  type dashboardLayout struct {
      Root    layoutBox
      Header  layoutBox
      Charts  layoutBox
      Metrics layoutBox
      Footer  layoutBox
  }
  ```

  Exact names may differ, but layout math should be separated from chart rendering so it can be unit tested.
- Prefer a single full-screen dashboard over pane-first navigation:
  - charts are visible by default while the run is active;
  - number keys may switch chart emphasis/detail, but the default screen must already contain charts and metrics;
  - the bottom metrics panel remains visible across chart/detail modes.
- Add pure render helpers under `internal/tui`, for example:

  ```go
  func renderHeader(store liveStore, width int) string
  func renderChartArea(store liveStore, width int, height int, mode dashboardMode) string
  func renderRunCharts(store liveStore, width int, height int) string
  func renderMetricsPanel(store liveStore, width int, height int) string
  func fitToBox(content string, width int, height int) string
  func joinVerticalToHeight(sections []string, width int, height int) string
  ```

  Exact signatures may differ, but each helper should be deterministic and unit-testable without a terminal.
- The metrics panel should be a stable table pinned to the bottom, not a pane:
  - include `http_ttfb_ms`, `provider_processing_ms` when available, `server_wait_to_first_byte_ms`, `ttft_delta_ms`, `e2e_delta_ms`, `e2e_output_tps`, and `generation_delta_output_tps`;
  - include group-level `system_tps` and `rps` when available;
  - include active, completed, successful, error, HTTP status, and error-category counts;
  - show unavailable values as `-`, never `0` unless the metric is truly observed as zero;
  - include units in headers or row labels (`ms`, `tokens/s`, `req/s`).
- Run dashboard chart area should include, at minimum:
  - TTFT sparkline over successful measured request order;
  - E2E sparkline over successful measured request order;
  - TTFT histogram or percentile bars when enough values exist;
  - slowest-request waterfall when a successful request has timeline phases;
  - a clear empty-state message before the first measured request completes.
- Bench dashboard support can be completed in task 34, but this task should shape the reusable layout so bench charts can occupy the same chart area and metrics panel.
- Styling:
  - use Lip Gloss for borders, titles, and low-priority text;
  - keep content understandable without color;
  - avoid excessive animation/ticks; request-completion events are enough for v0.3.
- Keyboard behavior after redesign:
  - `?`: toggle help overlay/footer;
  - `1`: default dashboard;
  - `2`: focus TTFT charts while keeping metrics panel visible;
  - `3`: focus E2E/TPS charts while keeping metrics panel visible;
  - `4`: focus slowest-request waterfall while keeping metrics panel visible;
  - `q`/`ctrl+c`: open cancellation confirmation while running; quit immediately after completion;
  - `esc`: close help/detail/confirmation overlay.

Implementation notes:

- Existing chart renderers in `internal/tui/charts` can be reused, but may need width/height-aware variants or wrappers.
- Existing `liveStore.MetricRows`, `SlowestRequests`, `Groups`, and `StatusCounts` should be extended rather than replaced.
- Consider adding `store.RunSeries(metricName)` helpers that return successful measured request values in completion/order-safe order.
- The root `View()` should be a thin composition function: calculate layout, render header/charts/metrics/footer, fit to terminal size, return `tea.View{AltScreen: true}`.
- Keep `MouseMode` disabled unless mouse interaction is actually implemented.

Tests:

- Layout tests:
  - `WindowSizeMsg{Width: 100, Height: 30}` produces a rendered view with no line wider than 100 and enough lines to use the available height;
  - tiny sizes such as `40x10` render without panic and include at least status/progress/metrics;
  - extra height is allocated to the chart area, not lost.
- Run dashboard tests:
  - default `run --tui` view contains chart labels without pressing number keys;
  - after feeding request-finished events, TTFT/E2E chart text changes;
  - before any request completes, the chart area shows a clear empty state;
  - metrics panel is present at the bottom in default, TTFT, E2E/TPS, and waterfall modes.
- Store/view-model tests:
  - metric rows distinguish missing values from observed zero;
  - status/error counts are reflected in the metrics panel;
  - no prompt text, API key, Authorization header, or chunk content appears in rendered strings.
- CLI tests:
  - existing fake TUI launcher tests remain valid;
  - non-TUI `run` output remains unchanged.

Implemented details:

- Added a centralized dashboard layout model with root/header/chart/metrics boxes and deterministic width/height allocation from `tea.WindowSizeMsg`.
- Reworked the root TUI view to render a full-screen dashboard that fits exactly within the reported terminal size when dimensions are available.
- Made the default `run --tui` screen chart-first: TTFT and E2E sparklines are visible by default, alongside TTFT histograms and slowest-request waterfall previews when data exists.
- Kept focused chart modes for `1` dashboard, `2` TTFT, `3` E2E/TPS, and `4` waterfall while keeping the metrics panel pinned at the bottom in all modes.
- Rebuilt the metrics panel as a bottom-pinned table with p50/p95/p99/mean, units, `http_ttfb_ms`, `provider_processing_ms`, `server_wait_to_first_byte_ms`, `ttft_delta_ms`, `e2e_delta_ms`, `e2e_output_tps`, `generation_delta_output_tps`, system TPS, RPS, progress, status codes, error categories, report status, and output directory.
- Added width/height fitting helpers that truncate/pad rendered content by visible width, preserve useful tiny-terminal output, and allocate extra vertical space to the chart area.
- Added `liveStore.RunSeries` and p99 metric-row support so chart renderers and the metrics table consume successful measured request values in request-completion order.
- Added layout/dashboard tests for exact terminal dimensions, tiny terminals, chart-area growth, default chart visibility, realtime request-finished updates, bottom metrics persistence across modes, missing-vs-zero metrics, and rendered-secret avoidance.

Definition of done:

- `run --tui` opens to a full-screen chart dashboard by default.
- The dashboard uses the available terminal width/height and keeps a metrics panel pinned at the bottom.
- Charts and metrics update as measured requests finish.
- The layout/rendering code has deterministic unit tests and does not require a real terminal.
- Metric labels preserve the project's terminology discipline.

---

### [x] 33.6. Create a polished TUI design system and ntcharts-backed chart architecture

The full-screen dashboard from task 33.5 fixes the rough layout, but it is still visually utilitarian. Before expanding `bench --tui`, do a deliberate UI/UX design pass so every visible element has a job, charts feel like real terminal charts, and the dashboard can scale from small laptops to large terminals.

Decision: use `github.com/NimbleMarkets/ntcharts/v2` for the main chart primitives when implementation starts. It supports Bubble Tea v2 import paths, has canvas/line/bar/heatmap/time-series primitives, and produces substantially richer terminal charts than our hand-rolled ASCII charts. Because ntcharts notes that its v2 API can still change, isolate it behind `internal/tui/charts` adapters so the rest of the TUI never imports ntcharts directly. Do not add ntcharts to `go.mod` until the implementation imports it, so `go mod tidy` remains clean.

Design principles:

- **Glanceable hierarchy:** the user should understand run health in under two seconds: status, progress, TTFT trend, E2E trend, errors, and output path.
- **Evidence over decoration:** charts should be beautiful but still map directly to recorded request metrics. Avoid meaningless animation or smoothing that hides outliers.
- **Charts first, table second:** the main body is visual; dense metric tables are pinned at the bottom as supporting evidence.
- **No hidden methodology:** chart titles and legends must use exact metric names and units (`ttft_delta_ms`, `e2e_delta_ms`, `tokens/s`, `req/s`). Never label chunks/deltas as token ITL/TPOT.
- **Stable under resize:** layout changes should feel intentional at large, medium, and small sizes; no accidental wrapping, ragged boxes, or clipped labels unless terminal size forces truncation.
- **Accessible without color:** color should reinforce meaning, not carry the only signal. Use symbols, labels, and ordering that remain understandable in no-color terminals.
- **No secrets:** rendered strings must never include API keys, Authorization headers, prompt text, generated chunk content, or provider metadata maps that may contain sensitive values.

Visual architecture:

```text
┌ what-ttft ─ provider=openai ─ model=gpt-x ─ scenario=short ─ cache=cache-bust ┐
│ RUNNING  elapsed=00:01:18  samples=17/50  active=1  ok=17  err=0  tier=default │
├───────────────────────────────┬──────────────────────────────────────────────┤
│ TTFT delta ms                  │ E2E delta ms                                 │
│  p50 312  p95 451  max 610    │  p50 980  p95 1300  max 1601                │
│  ntcharts line/stream chart    │  ntcharts line/stream chart                 │
│  y-axis ms, x=request order    │  y-axis ms, x=request order                 │
├───────────────────────────────┼──────────────────────────────────────────────┤
│ TTFT distribution              │ Slowest request waterfall                    │
│  ntcharts bar/histogram        │  DNS/TCP/TLS/write/server-wait/stream/decode │
├──────────────────────────────────────────────────────────────────────────────┤
│ METRICS (p50/p95/p99/mean)                                                │
│ http_ttfb_ms ...  ttft_delta_ms ...  e2e_delta_ms ...  e2e_output_tps ... │
│ generation_delta_output_tps ...  system_tps ...  rps ...  statuses/errors │
└──────────────────────────────────────────────────────────────────────────────┘
```

Responsive layout breakpoints:

1. **Wide terminals (`width >= 120`, `height >= 32`)**
   - Header: two compact lines.
   - Body: 2x2 grid.
     - top-left: TTFT line/stream chart;
     - top-right: E2E line/stream chart;
     - bottom-left: TTFT distribution/percentile bars;
     - bottom-right: slowest-request waterfall/status detail.
   - Bottom metrics panel: 7-10 rows depending on height.
2. **Medium terminals (`80 <= width < 120` or `20 <= height < 32`)**
   - Header: one or two lines depending on available height.
   - Body: stacked chart sections with TTFT and E2E first; distribution/waterfall can rotate by focus mode.
   - Bottom metrics panel remains visible, compressed to key rows plus status line.
3. **Small terminals (`width < 80` or `height < 20`)**
   - Header: status/progress only.
   - Body: one primary chart selected by mode, with clear empty state when no data exists.
   - Bottom metrics panel: compact rows for TTFT, E2E, TPS, ok/err, and output/report status.

Chart architecture:

- Add an ntcharts-backed adapter layer under `internal/tui/charts`, for example:

  ```go
  type Theme struct { ... }
  type SeriesPoint struct { Index int; Value float64 }
  type SeriesChartOptions struct { Width, Height int; Title, Unit string; ColorRole string }

  func RenderSeriesChart(values []float64, opts SeriesChartOptions, theme Theme) string
  func RenderHistogramChart(values []float64, opts HistogramOptions, theme Theme) string
  func RenderPercentileChart(groups []PercentileGroup, opts PercentileOptions, theme Theme) string
  func RenderWaterfallChart(record whatttft.RequestRecord, opts WaterfallOptions, theme Theme) string
  ```

  Exact names may differ, but the rest of `internal/tui` should depend on our wrappers, not ntcharts model types.
- Use ntcharts `linechart`/`streamlinechart` for TTFT and E2E request-order series:
  - X axis is successful measured request order, not wall time;
  - Y axis is milliseconds;
  - outliers remain visible; do not clip unless the chart explicitly labels clipping;
  - use Braille/line drawing when terminal size supports it, fallback to sparkline for tiny boxes.
- Use ntcharts `barchart` or `canvas` for distributions:
  - TTFT histogram with bins labelled in ms;
  - or p50/p90/p95/p99 lollipop/bars when sample size is too small for meaningful histogram bins.
- Keep the waterfall renderer custom unless ntcharts provides a better fit:
  - phases are categorical durations rather than continuous series;
  - labels must use project terminology (`server wait to first byte`, `stream protocol to first output`, `visible-generation deltas`).
- Keep existing pure string chart functions as fallbacks and for tests where ntcharts output may be too brittle.

Theme/design system:

- Add `internal/tui/theme.go` with semantic roles instead of ad-hoc colors:
  - `Accent`, `Good`, `Warn`, `Bad`, `Muted`, `Border`, `ChartTTFT`, `ChartE2E`, `ChartTPS`, `ChartWaterfall`, `Background`.
- Add `internal/tui/components.go` or equivalent for reusable components:
  - `panel(title, body, width, height, styleRole)`;
  - `metricCard(label, value, unit, severity)`;
  - `statusPill(status)`;
  - `progressBar(completed, total, width)`;
  - `legend(items...)`.
- Severity rules:
  - errors > 0 uses warning/bad styling;
  - request failures and report-write errors use bad styling;
  - no data uses muted empty-state styling;
  - successful completion uses good styling.
- Make color disable-friendly:
  - when `NO_COLOR` or an explicit future `--no-color` is set, styles should remove color while preserving borders/text.

Dashboard content rules:

- Header must show:
  - command mode (`run` now, `bench` later);
  - provider/API/model/scenario;
  - cache mode, connection mode, service tier if known;
  - status pill (`running`, `writing reports`, `completed`, `canceled`, `error`);
  - progress (`completed/total`, active, ok, err).
- Main chart area for `run --tui` must show by default:
  - TTFT trend chart;
  - E2E trend chart;
  - TTFT distribution/percentile chart;
  - slowest-request waterfall or an actionable empty state.
- Bottom metrics panel must show:
  - `http_ttfb_ms`, `provider_processing_ms` when available, `server_wait_to_first_byte_ms`, `ttft_delta_ms`, `e2e_delta_ms`, `e2e_output_tps`, `generation_delta_output_tps`;
  - p50/p95/p99/mean with unavailable values as `-`;
  - `system_tps` and `rps`;
  - HTTP status counts and error categories;
  - report status and output directory when known.
- Empty states must be specific:
  - before first measured success: `waiting for first successful measured request`;
  - no usage tokens: `TPS unavailable: provider usage not reported`;
  - no waterfall phases: `waterfall unavailable: timeline events missing`.

Interaction model:

- Default screen is non-interactive and useful immediately.
- Keybindings remain simple:
  - `?` toggles help;
  - `1` overview;
  - `2` TTFT focus;
  - `3` E2E/TPS focus;
  - `4` waterfall/slowest request focus;
  - `q`/`ctrl+c` asks for cancel confirmation while running and quits after completion;
  - `esc` closes help/detail/confirmation.
- Do not enable mouse support in v0.3 unless there is a concrete interaction. ntcharts can work without mouse support for static charts.

Implementation steps:

1. Add ntcharts dependency when first used:

   ```sh
   go get github.com/NimbleMarkets/ntcharts/v2
   ```

2. Add `internal/tui/theme.go` and tests for color/no-color semantic styles.
3. Add `internal/tui/layout.go` or expand `dashboard.go` to use named panels/cards instead of raw text joins.
4. Replace current chart wrappers with ntcharts-backed renderers behind our `internal/tui/charts` API:
   - line/stream chart wrapper for TTFT/E2E;
   - bar/histogram/percentile wrapper;
   - preserve pure string fallback functions for tiny terminals and deterministic tests.
5. Rework `renderRunCharts` into explicit `overview`, `ttftFocus`, `e2eFocus`, and `waterfallFocus` renderers that compose panels.
6. Rework `renderMetricsPanel` into a designed component with aligned rows/cards, status line, and compact mode.
7. Update tests to assert structure/labels/line widths rather than exact ntcharts glyph output, with separate deterministic wrapper tests for our fallback renderers.

Tests:

- Visual structure tests:
  - wide layout contains four named panels in overview;
  - medium layout prioritizes TTFT/E2E charts before distribution/waterfall;
  - small layout renders a compact useful dashboard;
  - all rendered lines fit within the terminal width.
- Chart adapter tests:
  - ntcharts-backed series renderer includes title/unit/axis labels and changes when values change;
  - empty/non-finite inputs render explicit empty states;
  - tiny dimensions fall back to compact sparklines or no-data labels;
  - histogram/percentile renderer labels units and does not show NaN/Inf.
- UX semantics tests:
  - status pill text changes for running/writing reports/completed/canceled/error;
  - errors/status codes are visibly represented without relying only on color;
  - missing TPS explains provider usage is unavailable;
  - no secrets/prompt/chunk content render.
- Regression tests:
  - existing `run --tui` fake-launcher tests still pass;
  - race detector still passes for `internal/tui` and `internal/eventbus`.

Definition of done:

- The `run --tui` overview looks intentional and polished in wide, medium, and small terminals.
- The main TTFT/E2E charts use ntcharts-backed renderers or a justified fallback when dimensions are too small.
- The bottom metrics panel is visually aligned, stable, and includes all critical metrics/status data.
- The chart and component architecture is isolated behind internal adapters so ntcharts API changes do not leak into the rest of the app.
- Tests cover layout, semantics, chart state changes, no-color/fallback behavior, and secret non-leakage.

---

### [x] 34. Wire `what-ttft bench --tui` to target-aware live dashboards

Files:

- `cmd/what-ttft/bench.go`
- `internal/tui/app.go`
- `internal/tui/store.go`
- `internal/tui/bench_views.go` or equivalent
- CLI and TUI tests

Implementation details:

- `what-ttft bench --config benchmark.yaml --tui ...` should use the same event bus/TUI launcher path as `run --tui` but show benchmark-level and target-level information.
- Bench dashboard content:
  - header: benchmark name, scenario, cache mode, connection mode, target order, output directory;
  - target list/table: target ID/name, provider, API, service tier, model, status, completed/total, ok/error counts;
  - comparison table: p50/p95 TTFT, p50/p95 E2E, e2e TPS mean, generation TPS mean, system TPS, RPS;
  - chart pane: target percentile bars or heatmap-like comparison;
  - current target detail pane with the same single-run charts when a target is selected;
  - report-writing and final output status.

- Target navigation:
  - up/down or `j`/`k`: select target/group row;
  - enter: drill into selected target details;
  - esc: return to benchmark overview;
  - number keys should continue to switch chart panes.

- Event handling:
  - `benchmark_started` initializes total target/run counts;
  - `target_started` marks current target;
  - per-target request events update the correct group using `TargetID` and `RequestID`;
  - `target_finished` updates target status;
  - `benchmark_finished` freezes final combined summary;
  - report events show write status.

- Serial target caveat:
  - because v0.3 still uses v0.2 serial target execution, the TUI should display `target_order=serial` somewhere in the bench view;
  - documentation/help should warn that time-of-day/provider-load drift can affect target comparisons.

- Tests:
  - fake two-target benchmark events produce two target rows;
  - selected target detail shows only that target's records;
  - comparison table values match the combined `RunSummary` groups;
  - target statuses transition pending -> running -> finished;
  - cancellation during the second target writes partial combined reports and marks incomplete target state;
  - no API keys appear in final TUI-rendered strings.

Definition of done:

- `what-ttft bench --config ... --tui` displays live target progress and final comparison metrics.
- Target grouping in the TUI matches `summary.json` grouping.
- Serial execution limitations are visible to the user.

---

### [x] 34.1. Reuse run-style charts for benchmark model comparisons

Files:

- `internal/tui/bench_views.go`
- `internal/tui/charts/series.go`
- `internal/tui/charts/histogram.go`
- TUI chart/dashboard tests

Implementation details:

- Benchmark overview should use the same four chart slots as `run --tui`: TTFT trend, E2E trend, TTFT distribution, and output TPS trend.
- For benchmark runs, chart data should be grouped into one visible series per target/model instead of flattening all records into a single run-like series.
- Focused TTFT and E2E/TPS panes should keep the same run-style chart semantics while comparing multiple target/model series.
- Selected-target drill-down should remain available for single-target details.
- The benchmark header must not imply a benchmark has only one model when multiple targets are present.

Tests:

- Multi-series line charts show model labels, per-target request-order semantics, and latest values.
- Multi-series histograms show model labels and shared-bin counts.
- Benchmark dashboard overview renders the four run-style chart panels with multiple model series and still avoids secrets.

Definition of done:

- `bench --tui` presents the same chart mental model as `run --tui`, with multiple target/model series where applicable.
- Users can compare models from the overview without first drilling into one selected target.

---

### [x] 34.2. Add explicit benchmark chart legends

Files:

- `internal/tui/bench_views.go`
- `internal/tui/charts/series.go`
- `internal/tui/charts/histogram.go`
- TUI chart/dashboard tests

Implementation details:

- Multi-target benchmark charts must display an explicit `legend:` row so users can map each line/bar color and marker to a model or target.
- Legend labels should prefer model IDs when unique, but use target names/service tiers/IDs to disambiguate repeated model IDs.
- The line chart and histogram legends should use stable markers (`●`, `◆`, etc.) that match the rendered series order, with color when available and readable no-color output in tests.

Definition of done:

- Benchmark TTFT/E2E/TPS charts visibly identify each model/target series.
- Duplicate model IDs no longer collapse to indistinguishable legend text.

---

### [x] 34.3. Make benchmark TPS columns explicit and show generation TPS sample counts

Files:

- `cmd/what-ttft/bench.go`
- `internal/report/markdown.go`
- `internal/tui/charts/target_table.go`
- CLI, report, and chart tests

Implementation details:

- Rename ambiguous benchmark comparison columns from `e2e_tps_mean` / `gen_tps_mean` to `e2e_output_tps_mean` / `generation_delta_output_tps_mean`.
- Display `generation_delta_output_tps_count` as `observed/successful` so short/buffered-output scenarios make sparse post-first-delta TPS samples obvious.
- Preserve existing JSON metric names and request-level derived metric names.

Definition of done:

- CLI and Markdown target comparison tables show explicit TPS metric names and generation TPS sample counts.
- Tests assert the new column names and count formatting.

---

### [x] 34.4. Add generated-token total metrics to summaries and comparison tables

Files:

- `pkg/whatttft/summary.go`
- `cmd/what-ttft/bench.go`
- `internal/report/markdown.go`
- `internal/tui/dashboard.go`
- `internal/tui/charts/target_table.go`
- Summary, CLI, report, and TUI tests

Implementation details:

- Add a `completion_tokens` distribution over successful measured requests with provider-reported output token counts.
- Surface aggregate generated-token totals with `completion_tokens_total` / `total_completion_tokens` and `completion_token_records` so TPS metrics have token-volume context.
- Add token total columns to benchmark CLI and Markdown comparison tables.
- Show generated-token totals in the live TUI metrics footer without dropping existing latency/transport metrics.

Definition of done:

- Machine-readable summaries include per-request completion-token distributions and aggregate token totals.
- Human-readable CLI, Markdown, and TUI summaries make the generated-token volume visible next to TPS metrics.

---

### [x] 35. Update README and examples for v0.3 live dashboards and events

Document the new user-facing behavior only after `run --tui` and `bench --tui` exist.

Files:

- `README.md`
- optionally `examples/README.md` or new docs/examples if helpful

Documentation details:

- Add a section titled `Live terminal dashboard` with:
  - `what-ttft run --tui ...` example;
  - `what-ttft bench --config examples/openai-model-compare.yaml --tui` example;
  - a short explanation that `--tui` is a presentation mode for existing commands, not a separate command group;
  - terminal requirements and non-interactive behavior;
  - keyboard shortcuts;
  - cancellation behavior and partial report behavior.

- Update output file docs if partial/canceled reports are now possible:
  - explain whether partial reports have the same file names;
  - explain how failures/cancellations appear in request records and summaries;
  - explain exit codes for cancellation and report-writing failures.

- Add screenshots/asciicasts only if easy and generated from fake data; do not block v0.3 on polished media.
- Add methodology caveats:
  - the TUI does not change benchmark timing methodology;
  - rendering is decoupled from the benchmark runner;
  - if event drops are possible under extreme load, final reports remain authoritative.

Definition of done:

- README includes copy-paste `--tui` examples for both `run` and `bench`.
- README documents cancellation/partial result behavior.

---

### [x] 35.1. Label histogram X axis with request counts

Files:

- `internal/tui/charts/histogram.go`
- `internal/tui/charts/histogram_test.go`

Implementation details:

- Add an explicit horizontal `x=requests` axis row under ntcharts-backed TTFT histograms so histogram bar length maps to request count.
- Use the largest per-bin count as the right-side axis bound for both single-series and multi-series stacked histograms.
- Keep the existing legend row for multi-target benchmark histograms.

Definition of done:

- TTFT distribution charts show a visible request-count X axis.
- Histogram tests assert the request-count axis label is rendered.

---

### [x] 35.2. Stabilize benchmark chart colors and simplify histogram axis labels

Files:

- `internal/tui/theme.go`
- `internal/tui/charts/series.go`
- `internal/tui/charts/histogram.go`
- chart/theme tests

Implementation details:

- Use one stable model/target palette across TTFT, E2E, TPS, and histogram charts so the same target keeps the same color in every panel.
- Preserve per-target color/marker indices even when a metric is missing for an earlier target, so remaining targets do not shift colors.
- Avoid duplicate colors within the fixed model palette; generate deterministic fallback colors only after the fixed palette is exhausted.
- Move the histogram X-axis meaning into the legend (`x=request count`) and keep the axis row itself to tick labels only.

Definition of done:

- Multi-target benchmark chart legends identify the X-axis meaning and target/model series without changing colors between panels.
- Histogram X-axis labels no longer include explanatory prose.
- Tests cover stable palette assignment and explicit style-index preservation.

---

### [x] 36. Add v0.3 quality gates, TUI model tests, and smoke coverage

Finish the milestone with tests and smoke checks that exercise events and TUI paths without requiring real providers.

Files:

- existing smoke scripts, or new scripts such as:
  - `scripts/smoke-fake-openai-tui-headless.sh` only if a stable headless TUI mode is implemented
- README testing instructions

Implementation details:

- Extend fake-server smoke coverage for `--tui` paths if a stable headless TUI launcher exists; otherwise keep TUI coverage in unit tests with injected launchers. Normal report files must still be produced and parseable.

- TUI automated tests should be unit/model tests, not terminal screenshots:
  - Bubble Tea model update/view tests;
  - store/chart deterministic tests;
  - CLI `--tui` path using an injected fake TUI launcher;
  - cancellation/partial report behavior using fake provider/server.

- If a headless TUI smoke mode is added, keep it clearly internal/test-only and do not expose a confusing public flag unless documented.
- Run the full local gate before marking v0.3 tasks complete:

  ```sh
  go test ./...
  go test -race ./...
  golangci-lint run
  go build ./...
  go run ./cmd/what-ttft run --help
  go run ./cmd/what-ttft bench --help
  scripts/smoke-fake-openai.sh
  scripts/smoke-fake-openai-bench.sh
  ```

  Add any stable TUI-specific smoke command only after it works reliably in non-interactive CI/local environments.

Implemented details:

- Added `scripts/quality-gate.sh` to run the full local gate in the documented order: tests, race tests, lint, build, command help checks, and both fake-provider smoke scripts.
- Strengthened the single-target fake OpenAI smoke script to parse `requests.jsonl`, `summary.json`, and `summary.md`, assert warmup/measured counts, usage/TPS distributions, successful request records, and no API-key leakage.
- Added TUI model coverage for benchmark target keyboard navigation/detail mode and concurrent `EventSink` publish/close behavior, and made `EventSink` shutdown safe against concurrent publishers.
- Updated README testing instructions to point contributors at the quality-gate script and document that `--tui` automation stays in model/injected-launcher tests rather than a public headless TUI flag.

Definition of done:

- Full local gate passes.
- Event bus and TUI code have race-detector coverage.
- Fake-provider smoke tests use no external network.
- Report files from smoke tests contain no API keys.
- `implementation-plan.md` v0.3 tasks are marked `[x]` only as they are completed.

---

## v0.4 feature plan: TUI request explorer and request detail drilldown

Goal: make the existing `run --tui` and `bench --tui` dashboards useful for debugging individual requests after, and where practical during, a benchmark. Users should be able to identify suspicious requests, filter/sort the request set, open one request, inspect timing/HTTP/cache/usage/error details, and view generated output when content capture was explicitly enabled.

Non-goals for v0.4:

- Do not add a separate `what-ttft tui` command group. Request exploration is a presentation mode inside the existing `run --tui` and `bench --tui` commands.
- Do not make generated content visible by default. Generated output can be sensitive and must remain gated by explicit capture controls such as `--save-chunks` or a clearly documented future output-preview flag.
- Do not emit high-volume per-chunk/per-token live events by default. If request details need content, aggregate or load content through opt-in mechanisms that preserve benchmark hot-path timing.
- Do not replace canonical report files. TUI request exploration is for live/operator inspection; `requests.jsonl`, `chunks.jsonl`, `summary.json`, and `run.json` remain authoritative.

### v0.4 design constraints and invariants

- Request exploration must consume data already present in `request_finished` events, `RequestRecord` snapshots, final `RunSummary` snapshots, and canonical report files. It must not add provider SDKs, retries, hidden buffering, or additional work in provider hot paths.
- The request explorer must be shared by `run --tui` and `bench --tui`. Single-target mode is just a filtered/simplified version of the same store/query/rendering code.
- The TUI may keep derived indexes and display rows in memory, but canonical request records must remain copied snapshots. Sorting and filtering must never mutate `liveStore.recordOrder`, `liveStore.records`, report writer inputs, or public `RunResult` values.
- Missing metrics and observed zero metrics must be visually distinct everywhere. Use `-` for missing/unavailable and `0.0` for observed zero values.
- Warmup requests must remain visible and identifiable in the explorer, but default summary metrics still exclude them. Filters should make it easy to hide or isolate warmups.
- Failed request records must remain first-class. They may not have TTFT/E2E/output metrics, but they should still show request ID, target/model, phase, HTTP status, error category, bounded error message, transport timings available before failure, and report-write state.
- The explorer must preserve v0.3 chart/model filtering semantics. Hiding a target/model from comparison charts should not delete records; request filters can either respect the visible model set by default in bench mode or expose an explicit `hidden:on/all` override.
- All request-table and request-detail text must pass through the same safe inline/truncation helpers used by the dashboard. No prompt text, API keys, Authorization headers, cookies, signed URLs, raw provider bodies, or unredacted provider metadata may be rendered by default.
- Generated output is sensitive even when it is model output rather than user input. It may be shown only in an explicit detail/output section after an explicit capture opt-in. It must never appear in request table rows, filter labels, lifecycle error messages, status lines, or logs.
- Rendering must be bounded by terminal dimensions. Large runs should render only visible rows plus small headers/footers, not the full request set on every frame.
- The first v0.4 implementation should favor deterministic keyboard interactions and unit/model tests over terminal screenshots or headless terminal emulation.

Suggested internal files:

```text
internal/tui/request_explorer.go        request pane model, list/detail/filter modes, key handling helpers
internal/tui/request_explorer_test.go   model tests for navigation, detail, filters, and privacy
internal/tui/request_rows.go            requestRow construction, display column selection, safe formatting
internal/tui/request_rows_test.go       row construction/order/redaction tests
internal/tui/request_filter.go          filter and sort data structures, parser, predicate, comparator
internal/tui/request_filter_test.go     filter/sort/parser tests
internal/tui/request_detail.go          detail/zoom rendering sections
internal/tui/request_detail_test.go     success/error/cache/transport/output detail rendering tests
internal/tui/request_output.go          optional chunks.jsonl loading and per-request output reconstruction
internal/tui/request_output_test.go     output opt-in, reconstruction, truncation, and redaction tests
```

Proposed store/display types. Names may change, but the implementation should preserve this separation of responsibilities:

```go
type requestExplorerState struct {
    CursorRequestID string
    Offset          int
    Mode            requestExplorerMode // list, detail, filter
    DetailSection   requestDetailSection
    FilterInput     string
    Filters         requestFilters
    Sort            requestSort
}

type requestRow struct {
    RequestID        string
    Attempt          int
    Phase            string // warmup|measured
    TargetID         string
    TargetName       string
    Model            string
    ProviderAPI      string
    ServiceTier      string
    Outcome          string // ok|error|canceled|unknown
    HTTPStatus       string
    ErrorCategory    string
    FinishReason     string
    TTFTMS           *float64
    E2EMS            *float64
    StreamTotalMS    *float64
    TTFBMS           *float64
    ProviderMS       *float64
    E2EOutputTPS     *float64
    GenerationTPS    *float64
    PromptTokens     *int
    CompletionTokens *int
    CacheState       string // hit|miss|unknown
    CachedTokens     *int
    Conn             string // reused/new + protocol when known
    OutputState      string // disabled|pending|available|empty|truncated
}

type requestFilters struct {
    Query            string
    TargetIDs        map[string]bool
    Models           map[string]bool
    ProviderAPIs     map[string]bool
    Phases           map[string]bool
    Outcomes         map[string]bool
    HTTPStatuses     map[string]bool
    HTTPClasses      map[string]bool // 2xx, 4xx, 5xx
    ErrorCategories  map[string]bool
    CacheStates      map[string]bool
    MetricRanges     []metricRangeFilter
    RequestIDSubstr  string
    RespectChartVisibility bool
}

type requestSort string

const (
    requestSortCompletionOrder requestSort = "completion-order"
    requestSortSlowestTTFT     requestSort = "slowest-ttft"
    requestSortSlowestE2E      requestSort = "slowest-e2e"
    requestSortSlowestStream   requestSort = "slowest-stream"
    requestSortHighestTPS      requestSort = "highest-tps"
    requestSortLowestTPS       requestSort = "lowest-tps"
    requestSortErrorsFirst     requestSort = "errors-first"
    requestSortTargetOrder     requestSort = "target-order"
)
```

Suggested request explorer filter language for `/` input. This can be implemented incrementally, but tests should lock down whichever subset is shipped:

```text
model:gpt-5.5 target:target-a api:responses phase:measured outcome:error
status:429 status:5xx error:http_status cache:hit id:000123
warmup:false ttft>500 e2e<=2000 stream>3000 ttfb>=100 tps<20 tokens>=50
sort:-ttft sort:e2e sort:errors sort:target clear
```

Parsing rules:

- Split on whitespace; quoted values are optional for v0.4 only if tests cover them.
- `key:value` filters are case-insensitive for keys and exact, case-sensitive for IDs/models unless documented otherwise.
- Numeric metric filters support `>`, `>=`, `<`, `<=`, and `=`.
- Missing metric values do not match numeric range filters unless the filter explicitly asks for missing values, for example `ttft:missing` if implemented.
- Multiple values for the same dimension should be ORed within that dimension; different dimensions should be ANDed.
- Invalid filters should not crash or clear the previous filter unexpectedly. Show a bounded validation message and keep the previous valid query active.
- Always show active filters and sort order in the request explorer header or footer.

Suggested keybindings for request exploration:

| key | request list behavior |
|---|---|
| `5` or `r` | open request explorer pane |
| `↑`/`↓` or `k`/`j` | move selected request row |
| `pgup`/`pgdn` | move by one page |
| `home`/`end` | jump to first/last matching request |
| `enter` | open selected request detail |
| `esc` | leave detail/filter mode, then return to overview if already in list mode |
| `/` | edit filter query |
| `ctrl+u` | clear filter query |
| `s` | cycle common sort orders |
| `e` | toggle errors-only filter |
| `w` | cycle all/measured/warmup phases |
| `o` | jump to output section in detail view when available |
| `[`/`]` | previous/next detail section |

### [x] 37. Design request-explorer UX, modes, and keybindings

Add a clear request exploration mode shared by single-target `run --tui` and multi-target `bench --tui`.

Implementation details:

- Add a new dashboard mode/pane for request exploration, for example `5` or `r` for Requests.
- Keep existing chart panes (`1`-`4`) stable unless there is a documented migration note.
- In the request explorer:
  - show a scrollable request table;
  - use `↑`/`↓` or `j`/`k` to move selection;
  - use page navigation for large runs;
  - use `enter` to zoom into a selected request detail view;
  - use `esc` to return from detail/filter modes;
  - use `/` or another explicit key to edit filters;
  - use a small set of sort/filter shortcut keys only if they are discoverable in the help footer.
- Preserve benchmark target/model selection behavior from v0.3. If keybindings overlap, document precedence clearly by mode.
- The request list must work while a run is still in progress and after it finishes. It can show partial results while active.
- Define explicit mode transitions before implementation:
  - chart pane + `5`/`r` -> request list;
  - request list + `enter` -> selected request detail;
  - request detail + `esc` -> request list;
  - request list + `/` -> filter editor;
  - filter editor + `enter` -> apply filter and return to request list;
  - filter editor + `esc` -> discard edits and return to request list;
  - request list + `esc` -> previous chart pane or overview;
  - cancellation confirmation still takes priority while the benchmark is running.
- In `bench --tui`, target/model selection keys should select chart targets outside the request pane and select request rows inside the request pane. If users need target filtering while in the request pane, expose it through filters rather than overloading row navigation.
- Detail navigation should not require a mouse. If mouse support is ever added later, it must be optional and tested separately.

Implemented details:

- Added a `paneRequests` TUI pane reachable with `5` or `r` from both `run --tui` and `bench --tui`.
- Added request-explorer state for list, detail, and filter-editing modes, including previous-pane return behavior, cursor request ID, paging offset, draft filter input, and committed filter text.
- Added keyboard handling for request row navigation (`↑`/`↓`, `j`/`k`, page up/down, home/end), request detail open/close (`enter`/`esc`), filter editor open/apply/discard (`/`, `enter`, `esc`), and filter clearing (`ctrl+u`).
- Preserved cancellation confirmation and global quit behavior while making request-explorer row navigation take precedence over benchmark target navigation inside the request pane.
- Added a first bounded request-list/detail renderer so the pane is visibly reachable before richer v0.4 row/filter/detail tasks are implemented.
- Documented the new request-explorer keybindings in `README.md`.
- Added TUI model tests for run request navigation/detail transitions, bench keybinding precedence, and filter editor transitions.

Definition of done:

- UX behavior and keybindings are documented in `README.md`.
- TUI model tests cover switching into request explorer, row selection, detail open/close, and keybinding precedence for `bench --tui`.
- The request explorer is reachable from both `run --tui` and `bench --tui` without changing how requests are executed.

---

### [x] 38. Build request-explorer store indexes, row models, and redacted display fields

Extend the TUI store with queryable request rows derived from completed `RequestRecord` values.

Implementation details:

- Maintain a stable request list in completion order and support deterministic sorting without mutating canonical record order.
- Build row fields that help identify requests quickly:
  - request ID and attempt index;
  - warmup/measured phase;
  - target ID/name, model, provider API, and service tier for `bench --tui`;
  - success/error state, HTTP status, error category, and finish reason;
  - TTFT, E2E, stream total, provider processing, TTFB, output TPS, generation TPS;
  - completion tokens, prompt tokens, cache-hit/cached-token indicators;
  - connection reuse/protocol and output-preview availability.
- Keep rows compact for narrow terminals and include more columns on wide terminals.
- Never render prompts, API keys, Authorization headers, cookies, signed URLs, raw provider bodies, or generated output in the request table.
- Reuse existing copy helpers so records stored in the TUI cannot be mutated by event producers.
- Define row value semantics precisely:
  - `Outcome=ok` means measured or warmup request completed with `Error == nil`;
  - `Outcome=error` means `Error != nil`, regardless of whether partial timeline data exists;
  - HTTP status is `-` when unavailable, not `0`;
  - cache state is `hit` when provider-reported cached prompt tokens are greater than zero, `miss` when provider reported cache fields with zero cached tokens, and `unknown` otherwise;
  - connection state should distinguish reused vs new when `GotConn` metadata exists and show protocol separately when space allows;
  - output state is independent of success/error because some failed requests may have partial output and some successful requests may have no visible text.
- Keep a row-level stable ordinal, e.g. completion index, so the list can show `#` and preserve selection across filter/sort changes by request ID when possible.
- Avoid storing formatted-only strings as the source of truth. Store typed values in row structs and format at render time so filters/sorts do not parse display text.

Implemented details:

- Added `internal/tui/request_rows.go` with typed request row construction from copied `RequestRecord` snapshots and target metadata.
- Added stable row ordinals, target ordinals, phase/outcome/cache/connection/output state labels, HTTP status fallback, token pointers, latency/TPS pointers, provider API, service tier, target/model labels, and protocol fields.
- Added `liveStore.requestRows()` so run and bench request explorers use the same queryable row model.
- Added copy-based `sortRequestRows` scaffolding for deterministic completion-order, target-order, and errors-first row ordering without mutating the input rows or canonical store order.
- Added `providerAPI` to live store context so single-target and benchmark request rows can carry provider API metadata from lifecycle events.
- Updated the initial request explorer list renderer to consume typed request rows rather than formatting directly from `RequestRecord` values.
- Added store tests for successful, failed, warmup, cache-hit, cache-miss, and multi-target row generation; sorting/copy behavior; and canonical record-order immutability.
- Added a dashboard privacy regression test proving request rows do not render ignored secret-like cache metadata or provider body snippets.

Definition of done:

- Store tests cover row generation for successful, failed, warmup, cache-hit, and multi-target records.
- Store tests verify row order is stable and canonical request records are not mutated by sorting/filtering.
- Dashboard tests verify sensitive strings in record metadata do not appear in request rows.

---

### [x] 39. Implement request list rendering for run and bench dashboards

Render the request explorer table as a first-class TUI pane.

Implementation details:

- For `run --tui`, default columns should prioritize request ID, phase, status, TTFT, E2E, TPS/tokens, HTTP status, and error category.
- For `bench --tui`, include target/model columns and respect the v0.3 model visibility selection when appropriate, while still allowing filters to override or reveal hidden targets explicitly if the user asks.
- Show useful empty states:
  - no requests completed yet;
  - filters matched no requests;
  - output preview unavailable because content capture was not enabled.
- Keep rendering deterministic and bounded for large runs; do not render thousands of rows at once.
- Preserve chart performance by deriving visible rows from store snapshots without expensive per-frame recomputation over large content blobs.
- Suggested compact columns for narrow terminals: `#`, `request`, `phase`, `ok/error`, `ttft`, `e2e`, `status`, `model/target`.
- Suggested wide columns: `#`, `request`, `target`, `model`, `phase`, `outcome`, `http`, `err`, `ttft`, `e2e`, `stream`, `ttfb`, `tps`, `tokens`, `cache`, `conn`, `finish`, `output`.
- Highlight or prefix selected rows without relying only on color, for example `›` for cursor and `!` for failed requests.
- Long model IDs, target names, request IDs, and error categories should be middle- or end-truncated consistently. Keep enough request ID suffix to identify rows against `requests.jsonl`.
- Table headers should include current match count, total request count, active sort, active filters, and whether chart visibility filters are being respected.

Implemented details:

- Reworked request-list rendering to use adaptive compact, benchmark, and wide table layouts with bounded rows based on terminal height.
- Added compact run columns for narrow terminals, benchmark target/model columns for medium-width bench views, and wide diagnostic columns for target, model, phase, outcome, HTTP status, error category, TTFT, E2E, stream total, TTFB, TPS, tokens, cache, connection, and output state.
- Added selected/error row markers that do not rely only on color.
- Added request-list status headers showing matched/total request counts, selected row, active filter text, sort order, and count of rows hidden by benchmark chart model visibility.
- Made `bench --tui` request lists respect v0.3 chart model visibility by default while preserving canonical records for future filter overrides.
- Added no-request and no-match empty states.
- Added simple text matching for the committed filter text as list-rendering scaffolding; full filter parsing/sorting remains task 41.
- Added dashboard tests for run list updates from `request_finished`, wide bench list rendering and chart visibility, narrow layout bounds, and no-match rendering.

Definition of done:

- Dashboard tests cover run and bench request-list rendering.
- Tests cover narrow and wide terminal layouts.
- The list updates when new `request_finished` events arrive.

---

### [x] 40. Implement request detail / zoom view

Allow users to open one request from the list and inspect detailed diagnostics.

Implementation details:

- Detail view sections should include:
  - request identity: request ID, target/model, attempt, warmup/measured, cache mode, connection mode;
  - outcome: success/error, provider finish reason, HTTP status, error category/message with secrets redacted;
  - latency summary: TTFB, headers latency, first SSE/event, TTFT, E2E, stream total, generation duration, TPS metrics;
  - timeline/waterfall using existing chart helpers where possible;
  - transport: DNS/TCP/TLS/request-write/server-wait/protocol/connection reuse/TLS version;
  - usage/cache: prompt/completion/total tokens, reasoning tokens when available, cached tokens, cache hit/miss metadata, service tier;
  - output preview/content only when explicit content capture is enabled.
- Provide section navigation for terminals that cannot show all details at once.
- The detail view should distinguish missing metrics from observed zero values.
- Error details should include enough provider status/body-snippet information to debug failures while preserving existing redaction guarantees.
- Suggested detail sections:
  1. `identity`: request ID, attempt, phase, target/model/provider/API, scenario, cache/connection mode, service tier;
  2. `outcome`: success/error, finish reason, HTTP status/status text, error category/message/retryable, provider body snippet when already redacted;
  3. `latency`: all derived request metrics with units and missing-vs-zero formatting;
  4. `timeline`: key event offsets and waterfall chart;
  5. `transport`: DNS/TCP/TLS/request write/server wait/protocol/reuse/TLS version/compression metadata;
  6. `usage/cache`: prompt/completion/total tokens, reasoning tokens, cached tokens, cache hit/miss, cache IDs only if redacted;
  7. `output`: availability, preview, and full captured visible output when enabled.
- Detail view should show both a human summary and raw field names for important metrics, e.g. `TTFT delta (ttft_delta_ms)` so users can map what they see back to JSON output.
- Include links-by-path rather than terminal hyperlinks if referencing report files, e.g. `requests.jsonl line unavailable in live view` or `chunks.jsonl loaded`.

Implemented details:

- Added `internal/tui/request_detail.go` with explicit request detail sections: identity, outcome, latency, timeline, transport, usage/cache, and output.
- Added detail section state and keyboard navigation with `[`/`]`, plus `o` to jump to output state.
- Expanded detail rendering for request identity, target/model/provider/API/scenario, cache and connection modes, service tiers, success/error outcome, HTTP status/text, retryability, latency metrics, timeline offsets, waterfall chart, transport phases, connection reuse, TLS/compression metadata, token usage, cache metadata, and output availability.
- Detail latency rendering distinguishes missing metrics (`-`) from observed zero values (`0.0`) and includes raw metric names such as `ttft_delta_ms` for mapping back to JSON.
- Error body snippets and messages go through a defensive request-detail redaction helper before display.
- Output section now explicitly explains that content inspection requires `--save-chunks`; full captured output remains task 42.
- Added tests for successful request details, provider/non-200 errors, missing-vs-zero metrics, cache hit metadata, reused connection transport, warmup bench records, timeline/waterfall rendering, output-unavailable state, secret redaction, and detail section key navigation.

Definition of done:

- Tests cover request detail rendering for success, provider error, non-200 HTTP status, missing metrics, observed zero metrics, cache hit, reused connection, and warmup request cases.
- Tests verify secret-like strings in metadata are not rendered.
- Detail view works for both `run --tui` and `bench --tui` records.

---

### [x] 41. Add request filtering and sorting

Support filtering the request list by common debugging dimensions.

Implementation details:

- Required filters:
  - target/model/provider API;
  - warmup vs measured;
  - success vs failed;
  - HTTP status code or status class;
  - error category;
  - cache hit/miss when cache metadata is available;
  - metric thresholds/ranges for TTFT, E2E, stream total, TTFB, output TPS, and token counts;
  - request ID substring.
- Required sorts:
  - completion order;
  - slowest TTFT;
  - slowest E2E/stream total;
  - highest/lowest TPS;
  - error/status first;
  - target/model then request order for `bench --tui`.
- Keep the first implementation simple and deterministic. Prefer explicit filter fields or small shortcut toggles over a complex query language unless tests cover parsing thoroughly.
- Display active filters and sort order in the request explorer footer/header.
- Ensure filters never affect canonical summaries or report files.
- Filter editor behavior:
  - keep a committed filter query and a draft filter query;
  - show parse errors inline while editing without applying invalid drafts;
  - `enter` applies a valid draft;
  - `esc` discards draft changes;
  - `ctrl+u` clears the draft or committed filter, depending on mode;
  - after applying filters, keep the selected request if it still matches, otherwise move to the nearest matching row.
- Sort behavior:
  - all sorts must be stable;
  - missing numeric metrics sort last for slowest/highest sorts and last for lowest sorts unless a missing-first sort is explicitly added;
  - tie-break by target order, completion order, then request ID for deterministic output.
- Metric filters must use the documented metric names from `DerivedMetrics` where possible (`ttft_delta_ms`, `e2e_delta_ms`, `stream_total_ms`, `http_ttfb_ms`, `e2e_output_tps`, `generation_delta_output_tps`) and may support short aliases only if documented.

Implemented details:

- Added typed request filter parsing for target/model/provider API, phase/warmup, outcome, HTTP status/status class, error category, cache state, request ID, safe metadata substring search, and metric thresholds over documented metric names and documented aliases.
- Added stable request sorts for completion order, slowest TTFT, slowest E2E, slowest stream total, highest/lowest output TPS, errors/status first, and target/model order.
- Added request explorer state for committed filters, draft filters, parse errors, parsed filter predicates, and active sort order.
- Added request explorer shortcuts: `s` cycles common sorts, `e` toggles errors-only, and `w` cycles measured/warmup/all phases.
- Filter editor now keeps invalid drafts unapplied, shows parse errors inline, applies valid drafts on `enter`, discards drafts on `esc`, and uses `ctrl+u` as draft/committed clear depending on mode.
- After filters or sorts change, the selected request is preserved when still visible; otherwise selection moves to the nearest matching completed-request ordinal.
- Bench request lists still respect chart target visibility by default, while `hidden:all` can reveal hidden-target requests without mutating canonical records, summaries, or reports.
- Active filters and sort order are shown in request explorer status/no-match states with defensive redaction of secret-like query text.
- README documents request filter syntax, metric aliases, sorts, hidden-target override, detail sections, and output-content safety behavior.

Definition of done:

- Unit tests cover every required filter and sort.
- Model tests cover editing/applying/clearing filters and preserving selection when possible.
- No filter path renders prompts, API keys, or generated content unless output content was explicitly enabled and the user is in the request detail/output section.

---

### [x] 42. Add opt-in generated-output inspection for request details

Make generated output available in the request detail view only when the user has explicitly chosen to capture content.

Implementation details:

- Reuse `--save-chunks` as the initial content opt-in unless a separate flag is justified and documented.
- When content capture is disabled:
  - request rows may show that output preview is unavailable;
  - detail view should explain how to rerun with content capture enabled;
  - no generated text should be retained solely for the TUI.
- When content capture is enabled:
  - reconstruct visible output per request from captured output chunks/deltas;
  - preserve distinctions between visible text, refusal text, tool-call output, reasoning output, and metadata when available;
  - show bounded previews in tables and scrollable/bounded full text in detail view;
  - avoid emitting high-volume per-chunk events unless an explicit debug mode is added and documented;
  - ensure content is redacted or omitted from logs and lifecycle error messages.
- Preferred first implementation data path:
  - while the benchmark is running, request details show output state as `pending until reports are written` when `--save-chunks` is enabled;
  - after `report_write_finished`, the TUI loads `chunks.jsonl` from `OutputDir` asynchronously if it exists;
  - chunk loading builds a bounded `map[request_id]outputCapture` in the TUI only;
  - request rows update output state to `available`, `empty`, or `truncated` after loading;
  - if loading fails, show a redacted TUI status message but do not fail the benchmark or report writing.
- If a later implementation chooses in-memory live output capture instead, it must remain gated by `SaveChunks`, avoid high-volume event fanout, and prove with tests/race detector that it does not block provider streaming.
- Output reconstruction rules:
  - concatenate `ChunkRecord.Content` for visible content chunks in index order;
  - ignore role-only, empty, usage-only, and terminal chunks for visible output;
  - preserve refusal/tool/reasoning fields separately if/when `ChunkRecord` grows those fields;
  - cap per-request retained output in the TUI, for example first/last N KiB with an explicit truncation marker;
  - never infer tokenizer-level tokens from chunks in the output view.
- If implementation diverges from the preferred `chunks.jsonl` loading path, document the final data path and its timing/privacy implications before marking the task complete.

Implemented details:

- Reused `--save-chunks` as the only generated-output opt-in; live events still do not carry generated text.
- Added `RunEvent.SaveChunks` so the TUI can distinguish disabled capture from pending chunk-file loading without retaining content during the hot path.
- Added asynchronous `chunks.jsonl` loading after `report_write_finished` when `SaveChunks` is true and `OutputDir` is available.
- Added bounded TUI-only output captures keyed by `request_id`; visible `ChunkRecord.Content` values are concatenated in chunk index order and role-only, empty, usage-only, and terminal/finish-only chunks are ignored for visible output.
- Added output states for disabled, pending, empty, available, truncated, and load error; rows show only the state, never generated text.
- Added request-detail output rendering that shows generated text only in the explicit output section, with truncation metadata and a clear `--save-chunks` explanation when disabled.
- Added defensive load-error redaction/status handling so chunk loading failures do not fail report writing or the benchmark.
- Documented the final `--save-chunks`/`chunks.jsonl` loading path and privacy behavior in the README.

Definition of done:

- Tests verify generated output is unavailable when content capture is disabled.
- Tests verify visible output can be inspected when content capture is enabled.
- Tests verify output text is not shown in request rows by default and is only shown inside the explicit request detail/output section.
- Fake-server tests cover multi-chunk output, empty chunks, role-only chunks, usage chunks, and failed requests with no output.

---

### [x] 43. Document, test, and quality-gate the request explorer

Finish v0.4 with documentation and deterministic tests.

Implementation details:

- Update README live-dashboard docs with request explorer screenshots as text examples if easy; do not require image assets.
- Extend fake-provider/TUI tests with injected events rather than real terminals.
- Add model tests for large request sets so scrolling/filtering remains bounded and deterministic.
- Add privacy regression tests for request rows, request details, filters, and output-preview states.
- Run the full local gate before marking v0.4 tasks complete:

  ```sh
  scripts/quality-gate.sh
  ```

Implemented details:

- Added README request explorer text examples for filtered list and output detail states, plus documented keybindings, filters, sorts, and `--save-chunks` output-content safety behavior.
- Added deterministic TUI model coverage for a 120-request injected-event set to verify paging, end-key navigation, filtering, sorting, and bounded rendering.
- Added privacy regression coverage for request rows, details, secret-like filter display, output states, output load errors, and explicit output-section-only generated content rendering.
- Kept request explorer tests event/model based with no real terminal or external provider network.
- Ran the full local quality gate through `scripts/quality-gate.sh`.

Definition of done:

- `go test ./...`, `go test -race ./...`, `golangci-lint run`, `go build ./...`, and fake-server smoke tests pass through `scripts/quality-gate.sh`.
- README documents request explorer keybindings, filters, and output-content safety behavior.
- Request explorer tests use no external provider network.
- `implementation-plan.md` v0.4 tasks are marked `[x]` only as they are completed.

---

## Future tasks beyond this v0.4 plan

These are intentionally outside the current v0.4 feature set but should influence design decisions.

### [ ] Future: additional provider protocols and APIs

After Responses becomes the OpenAI default, keep the provider abstraction ready for other APIs and protocols, including non-OpenAI providers, WebSocket-style realtime APIs, and future multimodal Responses features beyond text-only v0.2 coverage.

### [ ] Future: offline result explorer

Add a non-`tui`-group command shape for exploring completed runs, such as `what-ttft view --tui runs/...` or `what-ttft report --tui runs/...`, after the live `run --tui` and `bench --tui` path is stable. It should read canonical report files rather than requiring live events.

### [ ] Future: interactive benchmark setup wizard

Consider an interactive form mode such as `what-ttft run --tui --interactive` or `what-ttft bench --tui --edit-config` only after the live dashboard is reliable. The wizard should print or save the equivalent reproducible CLI/YAML config before running.

### [ ] Future: scheduled-rate/open-loop load mode

Add target RPS scheduling to avoid coordinated omission in capacity/SLO tests.

### [ ] Future: tokenizer fallback

Add optional tokenizer-based token count estimation when provider usage is unavailable. Mark all such counts and token timestamps as estimated.

### [ ] Future: true per-token event reconstruction

If a provider emits token-level events, store `first_output_token`, `last_output_token`, and token ITL/TPOT separately from chunk cadence.

### [ ] Future: richer matrix scheduling and multi-scenario configs

Extend the v0.2 YAML benchmark format beyond one shared scenario to full matrices across scenarios, prompts/datasets, cache modes, connection modes, concurrency levels, and provider-specific options. Add round-robin or randomized target/scenario interleaving so model comparisons are less sensitive to time-of-day and provider-load drift.

### [ ] Future: event-sourced report writing

After the v0.3 event schema has proven stable, evaluate whether report writing should consume the same event stream. Do not make this change until event ordering, backpressure, persistence, and schema-versioning rules are mature enough that canonical `requests.jsonl` output cannot be degraded by live telemetry concerns.

### [ ] Future: persisted live event streams

If external integrations need a tail-able live event stream, revisit an opt-in persisted event sink. It should not be named or documented in a way that competes with canonical `requests.jsonl`, `summary.json`, and `run.json`, and it must have explicit delivery guarantees and schema-versioning rules.

### [ ] Future: opt-in stream/chunk debug events

Add high-volume stream, output-delta, and token events only behind explicit debug flags. Generated text and provider metadata may be sensitive, so these events must have clear redaction/content controls and must never be enabled implicitly by `--tui`.

### [ ] Future: STT and TTS components

Extend the generic event/timeline model to audio upload/download and realtime factors as described in `AGENTS.md`.
