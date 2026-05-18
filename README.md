# what-ttft

`what-ttft` is a small Go benchmark for measuring where latency is spent in real-time AI streaming pipelines.

v0.1 focuses on OpenAI-compatible **Chat Completions streaming** endpoints. It uses direct `net/http` requests and SSE parsing rather than provider SDKs, so request timing, transport tracing, stream frames, and raw records remain visible.

## What it measures

For each streaming request, `what-ttft` records client-observed timeline events such as:

- DNS, TCP connect, TLS handshake, connection reuse, request write, and first response byte via Go `httptrace`.
- Response headers received.
- First SSE data event.
- First non-empty user-visible output delta.
- Last non-empty user-visible output delta.
- Provider `[DONE]` event.
- Response body EOF.
- Provider-reported usage and prompt-cache counters when available.

Key derived metrics include:

- `http_ttfb_ms`: first response byte minus request start.
- `first_event_ms`: first SSE event minus request start.
- `ttft_delta_ms`: first non-empty visible output delta minus request start.
- `e2e_delta_ms`: last non-empty visible output delta minus request start.
- `stream_total_ms`: response body EOF minus request start.
- `generation_delta_ms`: last visible delta minus first visible delta.
- `server_wait_to_first_byte_ms`: first response byte minus request write completion.
- `stream_protocol_to_first_output_ms`: first visible output delta minus first response byte.
- `e2e_output_tps`: provider-reported completion tokens divided by E2E delta seconds.

## What it cannot measure

This is a client-side benchmark. It cannot perfectly separate provider queueing, remote request ingestion, model prefill, provider-side buffering, and network return path unless the provider exposes those timings.

Treat `server_wait_to_first_byte_ms` as a conservative client-observed bucket that includes provider-side and return-path effects.

## Install

Linux `amd64` and `arm64` release assets are published on GitHub Releases as `.tar.gz` archives plus native `.deb`, `.rpm`, and `.apk` packages.

Homebrew users can install from the tap:

```sh
brew install --cask gabrielmbmb/tap/what-ttft
```

Quick Linux install to `/usr/local/bin`:

```sh
curl -fsSL https://raw.githubusercontent.com/gabrielmbmb/what-ttft/main/install.sh | sh
```

Install without sudo by choosing a writable directory:

```sh
curl -fsSL https://raw.githubusercontent.com/gabrielmbmb/what-ttft/main/install.sh | INSTALL_DIR="$HOME/.local/bin" sh
```

For native package managers, download the matching package from the release and install it with `sudo apt install ./what-ttft*.deb`, `sudo rpm -i ./what-ttft*.rpm`, or `sudo apk add --allow-untrusted ./what-ttft*.apk`.

## Build

```sh
go test ./...
go build ./...
```

Run the local quality gate used by contributors:

```sh
go test ./...
go test -race ./...
golangci-lint run
go build ./...
```

## OpenAI-compatible example

Set your API key:

```sh
export OPENAI_API_KEY="..."
```

Run a cache-busted sequential baseline:

```sh
go run ./cmd/what-ttft run \
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
  --max-output-tokens 64 \
  --out runs/openai-gpt-5.5-short-cache-bust
```

Notes:

- For some newer reasoning models, `--temperature 0` is unsupported. Omit temperature unless the model supports the value you choose.
- Use `--reasoning-effort none` where supported to avoid spending latency/tokens on hidden reasoning.
- The tool refuses to write into a non-empty `--out` directory unless `--overwrite` is set. This prevents accidentally mixing runs.

## Output files

Each run writes:

```text
run.json          run metadata, environment, scenario, and config
requests.jsonl    one full request record per attempted request, including errors and warmups
chunks.jsonl      optional per-chunk records when --save-chunks is true
summary.json      aggregate summaries over successful measured requests
summary.md        human-readable metric table
```

`chunks.jsonl` is omitted by default because chunks may contain generated content.

## Cache modes

Prompt/KV cache behavior can dominate TTFT, especially with long prompts.

> Do not compare cached and uncached results. Prompt/KV cache hits can dominate TTFT for long prompts.

Supported modes:

- `cache-bust`: inserts a per-request nonce into the prompt prefix to avoid prompt-cache hits. This is the default for cold comparisons.
- `cache-reuse`: sends identical prompts so provider caches can be reused intentionally.
- `provider-explicit-cache`: reserved for provider-specific explicit cache APIs.
- `unknown`: no cache mutation; results may be cache-affected.

Summaries group cache modes separately.

## Recommended benchmark procedure

1. Run a sequential cache-busted baseline.
2. Run a sequential cache-reuse test.
3. Compare warm and cold connection modes.
4. Run a concurrency sweep.
5. Compare p50/p95/p99, not just averages.
6. Keep raw `requests.jsonl` and inspect errors/outliers before drawing conclusions.

## Testing

Unit and end-to-end tests use `httptest.Server` and do not call real providers by default:

```sh
go test ./...
```

Optional real OpenAI smoke test:

```sh
WHAT_TTFT_INTEGRATION=1 \
OPENAI_API_KEY=... \
WHAT_TTFT_OPENAI_MODEL=gpt-5.5 \
go test ./pkg/provider/openai -run Integration -count=1
```

## Contributor methodology

See [`AGENTS.md`](AGENTS.md) for the benchmark methodology, metric definitions, cache-control rules, documentation requirements, and testing expectations.
