# AGENTS.md

Instructions for coding agents working on `what-ttft`.

## Project mission

Build a small, rigorous Go benchmark for measuring where latency is spent in real-time AI pipelines.

Initial scope: LLM API providers, especially streaming HTTP APIs. Future scope: STT and TTS components. The benchmark should answer questions like:

- How long until the first response byte arrives?
- How long until the first user-visible model output arrives?
- How fast does output stream after generation starts?
- How much time is network setup vs request upload vs provider wait vs stream decode?
- Did a new model/provider/region actually get faster, or did traffic shape/network/concurrency change?

Prefer correctness, repeatability, and raw evidence over pretty aggregate numbers.

## Research basis and adopted methodology

Use these conventions unless there is a documented reason to change them:

- Follow common LLM benchmark terminology used by NVIDIA GenAI-Perf/NIM and Ray/Anyscale: TTFT, E2E latency, inter-token latency/time-per-output-token, user TPS, system TPS, RPS, and percentile summaries.
- In streaming mode, do **not** count empty/role-only chunks as TTFT. TTFT is the first non-empty user-visible output token/delta.
- E2E latency for a streaming request is from request start to the last non-empty output token/delta. Also record stream EOF / `[DONE]` separately.
- Treat provider prompt/KV caching as a first-class benchmark variable. Never compare cached and uncached requests in the same summary.
- ITL/TPOT should characterize decode/streaming after the first token; do not include TTFT in ITL unless a metric is explicitly named as including it.
- Use Go `net/http/httptrace` for client-observed network phases: DNS, TCP connect, TLS handshake, connection reuse, request write, and first response byte.
- Use HTTP streaming/SSE directly. Avoid provider SDKs in the hot path because they can hide buffering, retries, timestamps, and transport behavior.

Reference links for future agents:

- LLM metrics: <https://docs.nvidia.com/nim/benchmarking/llm/latest/metrics.html>
- LLM latency/throughput definitions: <https://docs.anyscale.com/llm/serving/benchmarking/metrics.md>
- Go HTTP tracing: <https://go.dev/blog/http-tracing> and <https://pkg.go.dev/net/http/httptrace>
- OpenAI-compatible streaming uses data-only Server-Sent Events (`stream=true`).

## Metric definitions

Store raw timestamps/durations for every request. Aggregate later.

All durations should be measured from a per-request monotonic `request_start` timestamp. Store durations in nanoseconds in machine-readable output; format milliseconds only for humans.

### Required request timeline events

Record these when available:

- `scheduled_at`: when the benchmark intended to start the request.
- `request_start`: immediately before `client.Do(req)`.
- `dns_start`, `dns_done`.
- `connect_start`, `connect_done`.
- `tls_start`, `tls_done`.
- `got_conn`: include `reused`, `was_idle`, and `idle_time` from `httptrace.GotConnInfo`.
- `wrote_request`: after request headers/body are written.
- `first_response_byte`: from `httptrace.GotFirstResponseByte`.
- `headers_received`: when `client.Do` returns a response.
- `first_sse_event`: first SSE `data:` event, even if empty.
- `first_output_delta`: first non-empty provider delta/chunk that is user-visible.
- `first_output_token`: first newly observed output token if tokenization is available.
- `last_output_delta`: last non-empty provider delta/chunk.
- `last_output_token`: last newly observed output token if tokenization is available.
- `done_event`: provider stream terminator, e.g. `[DONE]`.
- `body_eof`: response body read completed.

### Required derived metrics

Name metrics precisely so they are not confused:

- `http_ttfb_ms = first_response_byte - request_start`.
- `headers_latency_ms = headers_received - request_start`.
- `first_event_ms = first_sse_event - request_start`.
- `ttft_delta_ms = first_output_delta - request_start`.
- `ttft_token_ms = first_output_token - request_start` when tokenizer data is available.
- `e2e_delta_ms = last_output_delta - request_start`.
- `e2e_token_ms = last_output_token - request_start` when tokenizer data is available.
- `stream_total_ms = body_eof - request_start`.
- `generation_delta_ms = last_output_delta - first_output_delta`.
- `generation_token_ms = last_output_token - first_output_token` when tokenizer data is available.
- `decode_tps = max(output_tokens - 1, 0) / generation_token_seconds` when token timestamps are available.
- `e2e_output_tps = output_tokens / e2e_seconds`.
- `system_tps = total_successful_output_tokens / (last_successful_response_time - first_successful_request_start)`.
- `rps = successful_requests / (last_successful_response_time - first_successful_request_start)`.

For ITL/TPOT:

- Per-request token TPOT: `(e2e_token_ms - ttft_token_ms) / (output_tokens - 1)`.
- Also record observed per-token gaps and summarize p50/p90/p95/p99 if token timestamps are available.
- If true token timestamps are not available, report `chunk_itl_ms` separately. Never label chunk gaps as token gaps.

### Network allocation guidance

Client-only data cannot perfectly separate provider queueing from model prefill. Use conservative labels:

- `dns_ms = dns_done - dns_start`.
- `tcp_connect_ms = connect_done - connect_start`.
- `tls_ms = tls_done - tls_start`.
- `connection_acquire_ms = got_conn - request_start` or `got_conn - get_conn` if `get_conn` is recorded.
- `request_write_ms = wrote_request - got_conn` or from first write hook if implemented.
- `server_wait_to_first_byte_ms = first_response_byte - wrote_request`.
- `stream_protocol_to_first_output_ms = first_output_delta - first_response_byte`.

Document that provider wait includes remote queueing, prompt ingestion, prefill, provider-side buffering, and network return path.

## Token and chunk handling

Streaming providers often send chunks that are not exactly one tokenizer token. Agents must preserve this distinction.

- Parse provider deltas/chunks exactly as received and timestamp them at read time.
- Ignore empty deltas, role-only deltas, heartbeats, comments, and terminal `[DONE]` for TTFT.
- Record `first_sse_event` separately from `first_output_delta`.
- Prefer provider-reported usage (`prompt_tokens`, `completion_tokens`, `total_tokens`) when available.
- Preserve provider cache usage fields when available, e.g. cached prompt tokens, cache creation tokens, cache read tokens, context-cache IDs, or prompt-cache hit/miss indicators.
- For OpenAI-compatible APIs, request usage in streams when supported, e.g. `stream_options: {"include_usage": true}`.
- If provider usage is unavailable, use a tokenizer fallback and mark token counts as `estimated`.
- If a chunk adds multiple tokens, assign those newly observed tokens the timestamp of the chunk that revealed them and mark timestamps as estimated.
- Keep visible output, reasoning output, tool-call output, and metadata separate. Default TTFT should be user-visible text/audio only, not hidden reasoning or tool call JSON, unless a scenario opts in.

## Benchmark methodology

### Scenario control

Every run must record enough metadata to reproduce the scenario:

- Provider name, model, endpoint/base URL, region if known.
- Request parameters: temperature, max output tokens, top_p/top_k, stop sequences, seed, stream flag, modalities, response format.
- Prompt text or prompt dataset reference plus prompt token count, prompt hash, and whether the prompt intentionally reuses or busts provider caches.
- Cache metadata: requested cache mode, observed cache hit/miss if exposed, cached token counts, explicit cache keys/IDs with secrets redacted, and cache TTL when known.
- Client metadata: OS, architecture, Go version, git SHA, benchmark version, hostname, local region/VPN/proxy notes if available.
- Transport metadata: HTTP protocol, keepalive settings, timeout settings, compression setting, connection reuse/cold mode, TLS version when available.
- Wall-clock start time and monotonic durations.

Use fixed prompt shapes for comparisons:

- Short prompt / short output for realtime turn-taking.
- Medium prompt / medium output for normal chat.
- Long prompt / short output to expose prefill/TTFT sensitivity.
- Optional long output to measure sustained decode throughput.

For deterministic comparisons, default to `temperature=0`, fixed `max_tokens`, no tools, no JSON schema unless the scenario is specifically testing them.

### Prompt/KV cache control

Many providers cache prompt processing or KV state across requests when the same prefix/input is reused. This can dramatically reduce TTFT for repeated prompts, especially long prompts. The benchmark must make cache behavior explicit.

Support at least these cache modes:

1. `cache-bust`: default for cold model/provider comparisons. Add a small unique nonce in a semantically irrelevant part of the prompt, or vary a trailing user message, so provider prompt/KV caches are unlikely to hit. Record the nonce location.
2. `cache-reuse`: intentionally send identical cacheable prefixes/inputs to measure best-case cached latency. Separate warmup/cache-population requests from measured cache-hit requests.
3. `provider-explicit-cache`: use provider-specific cache controls/context-cache APIs when a scenario explicitly tests them. Record cache creation, read, TTL, and cache ID metadata with secrets redacted.
4. `unknown`: only when provider behavior cannot be controlled or observed; results must be labeled as potentially cache-affected.

Methodology requirements:

- Never aggregate cache-busted, cache-reuse, and explicit-cache requests together.
- Report TTFT and server-wait metrics split by observed cache hit/miss when the provider exposes cache counters.
- Record provider-specific usage fields such as cached input tokens, cache read tokens, and cache creation tokens.
- For long-prompt scenarios, run both cache-busted and cache-reuse variants when possible; the delta estimates prompt prefill/cache benefit.
- Warmup requests can populate provider caches. If measuring cold/uncached behavior, warmup must use different prompts from measured requests or disable/bust caching.
- If cache busting changes token count, record the exact prompt token count per request and compare only within a narrow token-count band.

### Warmup and repetitions

- Include warmup requests for each provider/model/scenario and exclude them from default summaries.
- Keep raw warmup records with `warmup: true`.
- Use enough samples for percentiles. Prefer at least 30 successful measured requests per scenario; use more when p95/p99 matters.
- Run multiple independent passes when comparing providers/models.
- Randomize or interleave scenario order when comparing models to reduce time-of-day and provider-load bias.

### Load modes

Support these modes as the project evolves:

1. `sequential`: one request at a time. Best for baseline latency and low-noise model comparisons.
2. `fixed-concurrency`: N workers each issue the next request after finishing. Good for user-experienced latency under concurrency.
3. `scheduled-rate` / open-loop: requests are scheduled at a target RPS regardless of previous latency. This avoids coordinated omission and is preferred for capacity/SLO testing.

Always record `scheduled_at` and actual `request_start` so queueing in the benchmark client is visible.

### Percentiles and summaries

For each metric, report at least:

- count, success count, error count.
- min, mean, median/p50, p90, p95, p99, max, standard deviation.
- token-weighted ITL and request-weighted TPOT when token data is available.
- goodput when SLO thresholds are configured, e.g. TTFT < 500 ms and TPOT < 50 ms.

Do not drop outliers unless the exclusion rule is explicitly configured and recorded. Network errors, rate limits, retries, and timeouts must be visible in the output.

## Go implementation guidance

### Documentation requirements

This project is a benchmark library; ambiguous fields make results untrustworthy. Document public API and result-schema types aggressively.

Rules for Go documentation:

- Every package must have a package comment.
- Every exported type, function, method, variable, and constant must have a Go doc comment that starts with the symbol name.
- Every constant must have its own comment. Do not rely only on a `const` block comment unless every individual constant is also self-documented with a comment.
- Every field in every exported struct must have an adjacent field comment.
- Every field serialized to JSON must have an adjacent field comment, even if the struct is internal.
- Field comments must state units (`ns`, `ms`, bytes, tokens, count), clock basis (monotonic-relative vs wall-clock), nil/zero semantics, redaction behavior, and whether values are observed, estimated, or provider-reported when applicable.
- For enum-like constants, document when each value should be used and whether it can be mixed with other values in one summary.
- For provider-specific metadata maps, document allowed key shapes and secret-redaction requirements.

Example style:

```go
// CacheMode describes how a request should interact with provider prompt/KV caches.
type CacheMode string

const (
    // CacheBust disables or avoids provider prompt/KV cache hits for cold comparisons.
    CacheBust CacheMode = "cache-bust"

    // CacheReuse intentionally reuses cacheable prompt prefixes to measure cached latency.
    CacheReuse CacheMode = "cache-reuse"
)

// RequestRecord is the machine-readable result for one attempted provider request.
type RequestRecord struct {
    // RequestID is a stable unique ID for this benchmark request attempt.
    RequestID string `json:"request_id"`

    // PromptHash is the SHA-256 hex digest of the final prompt after cache-mode mutation.
    PromptHash string `json:"prompt_hash"`

    // CompletionTokens is the provider-reported output token count, or nil when unavailable.
    CompletionTokens *int `json:"completion_tokens,omitempty"`
}
```

Enforcement:

- `.golangci.yml` enables `godoclint` with `require-doc` and `require-pkg-doc` to enforce package docs and exported symbol docs, including exported constants.
- `revive` remains enabled and also checks exported-symbol comments.
- Stock golangci-lint linters do not reliably enforce comments on every exported struct field or every JSON field; agents must enforce field-comment rules during code review and implementation.

### General rules

- Use the Go standard library for timing and HTTP transport wherever practical.
- Prefer direct `net/http` requests over SDKs for benchmarked paths.
- Use `time.Now()`/`time.Since()` values that preserve Go's monotonic clock for duration calculations.
- Keep stdout/stderr out of the hot path. Write raw results to buffered JSONL/CSV and print only summaries/progress.
- Avoid global mutable state. Make benchmarks cancellable with `context.Context`.
- Never log API keys, Authorization headers, cookies, or signed URLs.
- Redact secrets in configs and result metadata.

### Suggested package layout

```text
cmd/what-ttft/          CLI entrypoint
internal/bench/         scenario runner, worker scheduling, warmup/measured phases
internal/provider/      provider interfaces and shared request/response types
internal/provider/openai/ OpenAI-compatible chat/completions streaming adapter
internal/transport/     HTTP client construction and httptrace capture
internal/sse/           SSE parser with tests
internal/metrics/       request timeline, derived metrics, summaries, percentiles
internal/report/        JSONL, CSV, Markdown/table output
internal/tokenizer/     provider usage normalization and tokenizer fallbacks
internal/testserver/    deterministic httptest servers for unit/integration tests
```

Adjust names as needed, but keep measurement, provider protocol parsing, statistics, and presentation separated.

### Provider interface expectations

Provider adapters should emit standardized events rather than compute final metrics themselves. The runner/metrics package owns timestamping and derived metric math.

Use one shared observer/hook interface for every provider. Providers translate native protocol events into common hooks:

- `Mark`/`MarkFirst`/`MarkLast` for timeline events.
- `OnStreamEvent` for raw protocol frames/events, e.g. SSE `data:` events.
- `OnOutputDelta` for semantic user-visible output deltas. This drives TTFT/E2E.
- `OnToken` only when the provider emits true token-level events; never use this for arbitrary chunks.
- `OnUsage` for prompt/completion token counts.
- `OnCache` for prompt/KV cache hit/miss and cached-token metadata.
- `OnFinish` for finish reason and terminal events.
- `OnHTTP` for normalized transport metadata from shared HTTP tracing.

A provider adapter should:

- Build the request body from a scenario.
- Attach request metadata needed for reproduction.
- Stream raw protocol events and semantic output events to the observer.
- Normalize provider usage, cache metadata, and finish reasons.
- Return errors without hiding provider status codes or response bodies.

OpenAI-compatible adapter requirements:

- Support `/v1/chat/completions` streaming first.
- Parse SSE lines beginning with `data:` and the `[DONE]` sentinel.
- Handle empty deltas, role-only deltas, content deltas, refusal fields, tool calls, and usage chunks.
- Request `stream_options.include_usage` when supported, but tolerate providers that reject it if configured.
- Preserve raw chunk sizes and timestamps for diagnostics.

### HTTP transport requirements

- Use a configurable `http.Transport`.
- Expose warm/reused connection mode and cold connection mode separately. Do not mix them in one summary.
- Record whether a connection was reused and how long it was idle.
- Disable or explicitly configure automatic compression when it could change streaming behavior; record the choice.
- Set sane timeouts but distinguish connect timeout, header timeout, and whole-request timeout where possible.
- Attach `httptrace.ClientTrace` per request and make trace recording concurrency-safe.

## Output files

Default run output should be a directory containing:

```text
run.json              run metadata and scenario config
requests.jsonl        one JSON object per request with raw timeline + derived metrics
chunks.jsonl          optional raw per-chunk/per-token event stream for debug mode
summary.json          aggregate stats by scenario/provider/model
summary.md            human-readable table and notes
```

Raw request records should include failures. Summaries may compute percentiles only over successful requests, but must report failure counts and categories.

## Testing guidance

No unit test should call a real external provider.

Use `httptest.Server` and deterministic fake streams to test:

- delayed headers / first byte;
- empty SSE chunks before content;
- role-only chunks before content;
- one chunk containing multiple tokens;
- usage chunk after content;
- `[DONE]` before EOF;
- abrupt EOF and malformed JSON;
- slow token cadence with known gaps;
- non-200 status bodies;
- context cancellation and timeouts.

Required checks before submitting changes once code exists:

```sh
go test ./...
go test -race ./...
golangci-lint run
go build ./...
```

Use the repository `.golangci.yml` for linting and formatting (`gofumpt`/`goimports`).

Integration tests against real providers must be opt-in via environment variables and skipped by default, for example:

```sh
WHAT_TTFT_INTEGRATION=1 OPENAI_API_KEY=... go test ./internal/provider/openai -run Integration
```

## Future STT/TTS extension

Keep the timeline/event model generic enough for other realtime components.

STT metrics to support later:

- time to connection ready;
- time to first partial transcript;
- time to stable/final transcript;
- audio upload pacing and bytes sent;
- real-time factor (`processing_time / audio_duration`);
- VAD/end-of-utterance latency when applicable.

TTS metrics to support later:

- time to first response byte;
- time to first audio byte;
- time to first playable audio frame;
- audio duration generated per wall-clock second;
- jitter/gaps between audio chunks;
- full synthesis latency and real-time factor.

Do not bake LLM-specific names into core timeline storage; use component-specific derived metrics on top of generic timestamped events.
