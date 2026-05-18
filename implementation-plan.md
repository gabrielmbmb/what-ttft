# Implementation Plan: `what-ttft` v0.1

This file is the source-of-truth TODO list for the first usable version of `what-ttft`.

## Agent execution instructions

Implement exactly one TODO task at a time. After completing that task, update this file to mark the task as done, commit the changes, and then print a detailed summary of what you implemented, including files changed, validation commands run, and any follow-up work.

Goal for v0.1: a Go library plus a small CLI that benchmarks OpenAI-compatible **Chat Completions streaming** endpoints and reports client-observed latency allocation: HTTP/network timing, first response byte, first SSE event, first non-empty output delta, end-to-end latency, chunk cadence, token usage, cache metadata, and aggregate percentiles.

Non-goals for v0.1:

- OpenAI Responses API support. Keep architecture ready for it, but do not implement it yet.
- STT/TTS benchmarking.
- Provider SDK usage in the hot path.
- Perfect provider-side attribution. Client-side timing cannot separate remote queueing vs prefill vs provider buffering unless provider exposes those metrics.
- True token-by-token timestamps when providers emit multi-token chunks. v0.1 reports chunk cadence and provider token usage; token timestamp estimation can be added later.

## Proposed architecture

Use a public package for the reusable benchmark API and provider packages for integrations. Keep low-level helpers internal.

```text
cmd/what-ttft/                  CLI entrypoint
pkg/whatttft/                   Public runner, config, scenario, records, metrics
pkg/provider/openai/            Public OpenAI-compatible Chat Completions provider
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

### [ ] 17. Final v0.1 quality gate

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

Definition of done:

- All commands pass.
- No API keys or secrets appear in output files.
- `implementation-plan.md` tasks completed for v0.1 are marked `[x]`.

---

## Future tasks after v0.1

These are intentionally outside the first version but should influence design decisions.

### [ ] Future: OpenAI Responses API provider

Implement `/v1/responses` streaming with the same timeline model. Keep output event types separate from Chat Completions chunks.

### [ ] Future: scheduled-rate/open-loop load mode

Add target RPS scheduling to avoid coordinated omission in capacity/SLO tests.

### [ ] Future: tokenizer fallback

Add optional tokenizer-based token count estimation when provider usage is unavailable. Mark all such counts and token timestamps as estimated.

### [ ] Future: true per-token event reconstruction

If a provider emits token-level events, store `first_output_token`, `last_output_token`, and token ITL/TPOT separately from chunk cadence.

### [ ] Future: multi-scenario config file

Add JSON or YAML config for running matrix benchmarks across providers, models, prompts, cache modes, and concurrency levels.

### [ ] Future: STT and TTS components

Extend the generic event/timeline model to audio upload/download and realtime factors as described in `AGENTS.md`.
