# what-ttft

`what-ttft` measures user-visible latency for OpenAI-compatible streaming endpoints. For the OpenAI provider, the default API is the Responses API (`/v1/responses`); Chat Completions remains available as an explicit compatibility mode for endpoints that do not support Responses. It is designed for answering practical questions like:

- How long until the first response byte arrives?
- How long until the first visible model output appears?
- How fast does the stream continue after output starts?
- Are latency changes caused by connection setup, provider wait, stream timing, cache behavior, or concurrency?

The tool uses direct Go `net/http` requests, Go `httptrace`, and a built-in SSE parser instead of provider SDKs, so the raw client-observed timeline remains visible in the output files.

## Install

Homebrew:

```sh
brew install --cask gabrielmbmb/tap/what-ttft
```

Linux quick install to `/usr/local/bin`:

```sh
curl -fsSL https://raw.githubusercontent.com/gabrielmbmb/what-ttft/main/install.sh | sh
```

Install without sudo by choosing a writable directory:

```sh
curl -fsSL https://raw.githubusercontent.com/gabrielmbmb/what-ttft/main/install.sh | INSTALL_DIR="$HOME/.local/bin" sh
```

You can also download release archives and native `.deb`, `.rpm`, or `.apk` packages from GitHub Releases.

## Build from source

```sh
go build ./cmd/what-ttft
```

Then run the built binary:

```sh
./what-ttft --help
```

Or run directly from a checkout:

```sh
go run ./cmd/what-ttft --help
```

## Quick start

Set your API key:

```sh
export OPENAI_API_KEY="..."
```

Run a sequential, cache-busted benchmark:

```sh
what-ttft run \
  --provider openai \
  --model gpt-5.5 \
  --api-key-env OPENAI_API_KEY \
  --prompt "Answer in one short sentence: what is the capital of France?" \
  --samples 50 \
  --warmup 5 \
  --concurrency 1 \
  --cache-mode cache-bust \
  --connection-mode warm \
  --reasoning-effort none \
  --service-tier default \
  --max-output-tokens 64 \
  --out runs/openai-gpt-5.5-short-cache-bust
```

If you are running from source, replace `what-ttft run` with:

```sh
go run ./cmd/what-ttft run
```

For OpenAI-compatible providers, set `--base-url` to the provider API base URL. If the endpoint only supports Chat Completions, add `--openai-api chat-completions`:

```sh
what-ttft run \
  --provider openai \
  --openai-api chat-completions \
  --base-url https://provider.example/v1 \
  --api-key-env PROVIDER_API_KEY \
  --model provider-model \
  --prompt "Say hello." \
  --samples 10 \
  --warmup 1
```

## Common options

```text
--provider openai
--openai-api responses|chat-completions
--base-url URL
--api-key-env ENV
--api-key KEY
--model MODEL
--prompt PROMPT
--system-prompt PROMPT
--samples N
--warmup N
--concurrency N
--cache-mode cache-bust|cache-reuse|provider-explicit-cache|unknown
--connection-mode warm|cold
--reasoning-effort none|minimal|low|medium|high|xhigh
--service-tier auto|default|flex|scale|priority
--max-output-tokens N
--temperature FLOAT
--top-p FLOAT
--timeout DURATION
--out DIR
--save-chunks
--include-usage
--legacy-max-tokens
--overwrite
```

Notes:

- Prefer `--api-key-env` over `--api-key` so secrets do not appear in shell history.
- Some reasoning models do not support `--temperature 0`; omit temperature unless the model supports the value you choose.
- Use `--reasoning-effort none` where supported to avoid spending latency and tokens on hidden reasoning.
- Use `--service-tier` to request an OpenAI processing tier (`auto`, `default`, `flex`, `scale`, or `priority`). Do not compare different requested or observed service tiers as if they were the same traffic shape.
- The tool refuses to write into a non-empty `--out` directory unless `--overwrite` is set.
- `--save-chunks` writes generated content to `chunks.jsonl`; leave it off when output text may be sensitive.

## What is measured

For every request, `what-ttft` records client-observed events such as:

- DNS lookup, TCP connect, TLS handshake, connection reuse, request write, and first response byte.
- Response headers received.
- First SSE data event.
- First non-empty user-visible output delta.
- Last non-empty user-visible output delta.
- Provider stream terminator, such as `[DONE]`.
- Response body EOF.
- Provider-reported token usage, prompt-cache counters, and OpenAI service tier metadata when available.

Important metrics:

| metric | meaning |
|---|---|
| `http_ttfb_ms` | Time from request start to first response byte. |
| `headers_latency_ms` | Time from request start until response headers are received. |
| `first_event_ms` | Time from request start to the first SSE data event, even if it is empty or metadata-only. |
| `ttft_delta_ms` | Time from request start to the first non-empty visible output delta. This is the main TTFT metric for v0.1. |
| `e2e_delta_ms` | Time from request start to the last non-empty visible output delta. |
| `stream_total_ms` | Time from request start until response body EOF. |
| `generation_delta_ms` | Time between first and last visible output deltas. |
| `server_wait_to_first_byte_ms` | Time from request write completion to first response byte. |
| `stream_protocol_to_first_output_ms` | Time from first response byte to first visible output delta. |
| `e2e_output_tps` | User-perceived output-token throughput: provider-reported output/completion tokens divided by request-start-to-last-visible-delta seconds, when usage is available. This includes TTFT. For Responses reasoning models, provider output tokens may include hidden reasoning tokens. |
| `generation_delta_output_tps` | Post-first-delta output-token throughput: `max(output_tokens - 1, 0)` divided by first-visible-delta-to-last-visible-delta seconds. This uses visible chunk/delta timestamps, not true per-token timestamps, so it is not `decode_tps` or token ITL. |
| `system_tps` | Total successful output tokens divided by the first-successful-request to last-successful-response window for a summary group. |
| `rps` | Successful measured requests divided by the first-successful-request to last-successful-response window for a summary group. |

For OpenAI Responses streams, TTFT is driven by the first non-empty `response.output_text.delta` or visible refusal delta, not by metadata events such as `response.created`, `response.in_progress`, tool events, reasoning events, or empty content-part events.

`what-ttft` does not count empty chunks, role-only chunks, usage chunks, comments, heartbeats, metadata events, reasoning/tool events, or `[DONE]` as TTFT.

OpenAI-compatible streams do not guarantee that one chunk equals one tokenizer token. `generation_delta_output_tps` is useful for comparing post-first-delta streaming speed when provider usage is available and at least two visible output deltas were observed, but it is intentionally not labeled decode TPS, token ITL, or TPOT. It is omitted for single-delta responses because the first and last visible output are the same user-visible event.

## Client-side limits

`what-ttft` measures what the client can observe. It cannot perfectly separate provider queueing, request ingestion, model prefill, provider-side buffering, and the network return path unless the provider exposes those timings.

Treat `server_wait_to_first_byte_ms` as a conservative bucket that can include remote provider work and network return-path effects.

## Output files

Each run writes a directory containing:

```text
run.json          run metadata, environment, scenario, and config
requests.jsonl    one full request record per attempted request, including errors and warmups
chunks.jsonl      optional per-chunk records when --save-chunks is true
summary.json      aggregate summaries over successful measured requests
summary.md        human-readable metric table
```

`requests.jsonl` is the most useful file for detailed investigation. It contains raw timelines, HTTP trace metadata, derived metrics, usage fields, cache fields, requested/observed service tier fields, and errors for each request.

`summary.md` is the quickest way to compare p50, p95, p99, mean, and max values for the main metrics.

## Cache modes

Prompt/KV cache behavior can dominate TTFT, especially with long prompts.

> Do not compare cached and uncached results. Prompt/KV cache hits can dominate TTFT for long prompts.

Supported modes:

- `cache-bust`: inserts a per-request nonce into the prompt prefix to avoid prompt-cache hits. This is the default for cold comparisons.
- `cache-reuse`: sends identical prompts so provider caches can be reused intentionally.
- `provider-explicit-cache`: reserved for provider-specific explicit cache APIs.
- `unknown`: leaves the prompt unchanged; results may be cache-affected.

Summaries group cache modes separately so cache-busted and cache-reuse measurements are not mixed.

## Recommended workflow

1. Start with a sequential cache-busted run to establish a baseline.
2. Run a cache-reuse comparison to estimate the impact of provider prompt/KV caching.
3. Compare `warm` and `cold` connection modes to understand connection setup cost.
4. Sweep concurrency values to see how user-visible latency changes under load.
5. Compare p50, p95, and p99 values, not just averages.
6. Inspect `requests.jsonl` for errors, outliers, connection reuse, cache counters, and token usage before drawing conclusions.

## Practical examples

Cold connections:

```sh
what-ttft run \
  --provider openai \
  --model gpt-5.5 \
  --api-key-env OPENAI_API_KEY \
  --prompt "Answer in one short sentence: what is the capital of France?" \
  --samples 30 \
  --warmup 0 \
  --connection-mode cold \
  --cache-mode cache-bust \
  --max-output-tokens 64 \
  --out runs/openai-gpt-5.5-short-cold
```

Cache-reuse comparison:

```sh
what-ttft run \
  --provider openai \
  --model gpt-5.5 \
  --api-key-env OPENAI_API_KEY \
  --prompt "Use this fixed prompt for a cache reuse comparison. Answer with one short sentence." \
  --samples 50 \
  --warmup 5 \
  --cache-mode cache-reuse \
  --connection-mode warm \
  --max-output-tokens 64 \
  --out runs/openai-gpt-5.5-cache-reuse
```

Concurrency sweep example:

```sh
for c in 1 2 4 8; do
  what-ttft run \
    --provider openai \
    --model gpt-5.5 \
    --api-key-env OPENAI_API_KEY \
    --prompt "Answer in one short sentence: what is the capital of France?" \
    --samples 50 \
    --warmup 5 \
    --concurrency "$c" \
    --cache-mode cache-bust \
    --connection-mode warm \
    --max-output-tokens 64 \
    --out "runs/openai-gpt-5.5-concurrency-$c"
done
```
