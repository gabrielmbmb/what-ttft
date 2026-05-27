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

## Testing and smoke checks

Unit tests do not call real providers. Before submitting changes, run the full local gate:

```sh
scripts/quality-gate.sh
```

The script runs the same checks CI expects:

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

No-network fake-server smoke tests exercise the CLI and report writers end to end. The first script covers the single-target `run` path and parses the generated reports. The second script covers the YAML multi-target `bench` path, verifies `/responses` is used by default, checks service-tier and TPS metadata, and ensures fake API keys are not written to reports.

The `--tui` paths are covered by Bubble Tea model tests and CLI tests with injected launchers rather than screenshot or headless-terminal smoke tests, so automated checks remain deterministic in non-interactive environments.

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

Use `run` for quick single-model/ad-hoc measurements. Use `bench` when you want a repeatable YAML config, multiple targets, a single combined report directory, and stable target labels for model comparison.

## YAML benchmark configs

YAML benchmark configs make multi-model comparisons repeatable. Inline API keys are intentionally not supported; put the environment variable name in `api_key_env` and export the key before running.

A complete two-model OpenAI Responses example is available at [`examples/openai-model-compare.yaml`](examples/openai-model-compare.yaml):

```yaml
schema_version: 1
name: openai-gpt-5-model-compare

defaults:
  provider: openai
  api: responses
  base_url: https://api.openai.com/v1
  api_key_env: OPENAI_API_KEY
  service_tier: default

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
  reasoning_effort: none

targets:
  - id: gpt-5.5
    name: GPT-5.5
    model: gpt-5.5

  - id: gpt-5.2
    name: GPT-5.2
    model: gpt-5.2
```

Validate the plan without sending requests:

```sh
what-ttft bench --config examples/openai-model-compare.yaml --dry-run
```

Run the benchmark:

```sh
export OPENAI_API_KEY="..."
what-ttft bench --config examples/openai-model-compare.yaml
```

The OpenAI provider defaults to `api: responses`. Use `api: chat-completions` only for compatibility endpoints that do not support `/responses`; Chat-only fields such as `include_usage` and `legacy_max_tokens` only apply in that mode.

`bench` supports command-line overrides for operational settings without editing the YAML:

```sh
what-ttft bench \
  --config examples/openai-model-compare.yaml \
  --samples 100 \
  --warmup 10 \
  --service-tier priority \
  --out runs/openai-gpt-5-priority-compare
```

## Live terminal dashboard

`--tui` is a presentation mode for the existing `run` and `bench` commands. It does not create a separate command group, change request timing, run provider calls from the UI, or replace the canonical report files.

Single-target dashboard:

```sh
what-ttft run \
  --provider openai \
  --model gpt-5.5 \
  --api-key-env OPENAI_API_KEY \
  --prompt "Answer in one short sentence: what is the capital of France?" \
  --samples 50 \
  --warmup 5 \
  --cache-mode cache-bust \
  --connection-mode warm \
  --reasoning-effort none \
  --max-output-tokens 64 \
  --tui
```

Multi-target benchmark dashboard:

```sh
what-ttft bench --config examples/openai-model-compare.yaml --tui
```

The dashboard uses the full terminal, including an alternate screen when supported. Use it in an interactive terminal with enough space for charts; for CI, scripts, cron jobs, or log capture, prefer the same commands without `--tui`. The non-TUI path writes the same reports and avoids terminal control sequences in logs.

A context-aware `Shortcuts` footer is pinned at the bottom of the dashboard and request explorer. Press `?` to expand the footer into a fuller guide for chart, request-list, request-detail, filter, and benchmark-target actions.

In `bench --tui`, the bottom panel is a `MODEL METRICS` matrix instead of an aggregate metrics table. It follows the selected benchmark target and shows per-model/per-target request counts, errors, distinct TTFT p50 and p95 columns, distinct E2E p50 and p95 columns, mean output TPS, and average completion tokens so model comparisons do not hide behind all-target aggregation.

Keyboard shortcuts:

| key | action |
|---|---|
| `1` | overview charts |
| `2` | TTFT-focused view |
| `3` | E2E/TPS-focused view |
| `4` | slowest-request waterfall |
| `5` or `r` | request explorer list for completed requests |
| `↑`/`↓` or `k`/`j` | select a benchmark target in chart views; select a request row in the request explorer |
| `pgup`/`pgdn`, `home`/`end` | page or jump in the request explorer |
| `space` | after a benchmark finishes, toggle the selected target/model in comparison charts |
| `a` | after a benchmark finishes, show all target/model series again |
| `enter` | open selected-target detail in `bench --tui`; open selected request detail in the request explorer |
| `/` | open request filter editor in the request explorer |
| `ctrl+u` | clear the request filter draft/query |
| `s` | cycle request explorer sort order |
| `e` | toggle errors-only request filter |
| `w` | cycle all/measured/warmup request phases |
| `[` / `]` | move between selected-request detail sections |
| `o` | jump to the selected-request output section |
| `esc` | return from selected-target detail, request detail, filter editor, or close help |
| `?` | expand/collapse the shortcut footer |
| `q` or `ctrl+c` | quit; while running, asks for cancellation confirmation |
| `y` | confirm cancellation |
| `n` | keep the run/benchmark running after a cancellation prompt |

Request explorer filters are space-separated. Bare words search safe request metadata. Structured filters include `target:ID`, `model:NAME`, `api:responses|chat-completions`, `phase:measured|warmup`, `warmup:true|false`, `outcome:ok|error`, `status:429`, `status:5xx`, `error:CATEGORY`, `cache:hit|miss|unknown`, and `id:SUBSTRING`. Metric filters use documented names such as `ttft_delta_ms>500`, `e2e_delta_ms>=2000`, `stream_total_ms<3000`, `http_ttfb_ms<=100`, `e2e_output_tps>20`, `generation_delta_output_tps>=10`, and `completion_tokens>=64`; short aliases `ttft`, `e2e`, `stream`, `ttfb`, `tps`, and `tokens` are also accepted. Add `sort:completion`, `sort:-ttft`, `sort:-e2e`, `sort:-stream`, `sort:-tps`, `sort:+tps`, `sort:errors`, or `sort:target` to choose a stable sort. In `bench --tui`, the request list follows hidden/visible chart targets by default; `hidden:all` shows requests for hidden chart targets without changing summaries or report files.

Request detail sections show identity, outcome, latency, timeline/waterfall, transport, usage/cache, and output availability. Generated text is not shown in request rows or non-output detail sections by default. If `--save-chunks` is enabled, the live dashboard waits until reports are written, loads `chunks.jsonl`, and then shows bounded visible-output text only in the selected request's explicit output section. Without `--save-chunks`, no generated text is retained solely for the TUI.

Request explorer text examples:

```text
Requests
requests=3/30  selected=1/3  filter=outcome:error status:5xx sort:-ttft  sort=slowest-ttft
   #   request              target      model        phase out   http  err        ttft     e2e  stream    ttfb     tps tokens cache conn   output
›  7   target-a-req-000006  target-a    gpt-5-mini   meas  err   500   provider   842.1  900.3   921.0   88.4       -      - unknown reused disabled
Shortcuts · ? more
requests: ↑/↓ move  •  enter detail  •  / filter  •  s sort  •  e errors  •  w phase  •  pgup/pgdn page  •  home/end jump  •  esc overview
```

```text
Request detail · output
request=target-a-req-000006  row=1/3  section=output 7/7
output_state=disabled
output_preview=unavailable; rerun with --save-chunks to write chunks.jsonl and enable request output inspection
Shortcuts · ? more
request detail: [/] section  •  o output  •  ↑/↓ request  •  esc list  •  ? all keys
```

Cancellation is graceful. If a TUI or context cancellation happens after at least one request record exists, `what-ttft` writes partial reports using the normal filenames (`run.json`, `requests.jsonl`, `chunks.jsonl` when enabled, `summary.json`, and `summary.md`) and exits with code `130`. The terminal summary says that partial results were written. If cancellation happens before any request record is available, the command exits with code `130` without partial report files. Report-writing failures exit with code `1` and are shown in stderr and the live dashboard status.

The live dashboard is intentionally decoupled from benchmark execution and report writing. Live chart updates are best-effort event rendering; under extreme load, dropped UI events may make the display lag or skip intermediate states, but final report files remain authoritative.

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
--tui
```

Notes:

- Prefer `--api-key-env` over `--api-key` so secrets do not appear in shell history.
- Some reasoning models do not support `--temperature 0`; omit temperature unless the model supports the value you choose.
- Use `--reasoning-effort none` where supported to avoid spending latency and tokens on hidden reasoning.
- Use `--service-tier` to request an OpenAI processing tier (`auto`, `default`, `flex`, `scale`, or `priority`). For YAML benchmarks, set `defaults.service_tier`, per-target `service_tier`, or use the `bench --service-tier` override. If omitted, `what-ttft` does not send `service_tier` and OpenAI uses its default `auto` behavior. Do not compare different requested or observed service tiers as if they were the same traffic shape.
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
| `ttft_delta_ms` | Time from request start to the first non-empty visible output delta. This is the main user-visible TTFT metric. |
| `e2e_delta_ms` | Time from request start to the last non-empty visible output delta. |
| `stream_total_ms` | Time from request start until response body EOF. |
| `generation_delta_ms` | Time between first and last visible output deltas. |
| `server_wait_to_first_byte_ms` | Time from request write completion to first response byte. |
| `stream_protocol_to_first_output_ms` | Time from first response byte to first visible output delta. |
| `completion_tokens` | Distribution of provider-reported generated/output token counts per successful measured request, when usage is available. |
| `e2e_output_tps` | User-perceived output-token throughput: provider-reported output/completion tokens divided by request-start-to-last-visible-delta seconds, when usage is available. This includes TTFT. For Responses reasoning models, provider output tokens may include hidden reasoning tokens. |
| `generation_delta_output_tps` | Post-first-delta output-token throughput: `max(output_tokens - 1, 0)` divided by first-visible-delta-to-last-visible-delta seconds. This uses visible chunk/delta timestamps, not true per-token timestamps, so it is not `decode_tps` or token ITL. |
| `system_tps` | Total successful output tokens divided by the first-successful-request to last-successful-response window for a summary group. |
| `rps` | Successful measured requests divided by the first-successful-request to last-successful-response window for a summary group. |

For OpenAI Responses streams, TTFT is driven by the first non-empty `response.output_text.delta` or visible refusal delta, not by metadata events such as `response.created`, `response.in_progress`, tool events, reasoning events, or empty content-part events.

`what-ttft` does not count empty chunks, role-only chunks, usage chunks, comments, heartbeats, metadata events, reasoning/tool events, or `[DONE]` as TTFT.

OpenAI-compatible streams do not guarantee that one chunk equals one tokenizer token. `generation_delta_output_tps` is useful for comparing post-first-delta streaming speed when provider usage is available, at least three visible output deltas were observed, and the observed post-first-delta window is at least 50 ms. It is intentionally not labeled decode TPS, token ITL, or TPOT. It is omitted for short/buffered responses because tiny first-to-last delta windows can produce meaningless TPS values.

TPS terms are deliberately separate:

- `completion_tokens` and `total_completion_tokens` describe generated-token volume; use them to judge whether TPS metrics are based on enough output.
- `e2e_output_tps` is user-perceived per-request output throughput and includes TTFT.
- `generation_delta_output_tps` is a post-first-visible-delta approximation based on visible delta timing, not true token timestamps.
- `system_tps` is group-level total successful output tokens divided by the successful response window.
- `rps` is successful measured requests per second over the same response window.

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

`summary.md` is the quickest way to compare p50, p95, p99, mean, and max values for the main metrics. For `bench` runs, it starts with a target comparison table containing per-target success/error counts, generated-token totals, token-usage record counts, TTFT/E2E percentiles, `e2e_output_tps_mean`, `generation_delta_output_tps_mean`, `generation_delta_output_tps_count`, system TPS, and RPS.

For YAML `bench` runs, `run.json` also records the benchmark name, config path, config SHA-256, target execution order, and per-target metadata such as target ID/name, provider API, model, requested service tier, observed service tier counts, redacted base URL, and API-key environment variable name. API key values are never written.

Canceled runs and benchmarks with partial records use the same output filenames as completed runs. Partial `requests.jsonl` files contain only records completed before cancellation, and partial `summary.json` / `summary.md` files summarize those completed measured records. Request-level provider failures remain visible as `error` objects in `requests.jsonl`; aggregate failure counts and categories appear in summaries.

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

## Comparing multiple models

Use `bench` for multi-model comparisons so target IDs, config hashes, and combined summaries stay together. In v0.2, all targets in a YAML benchmark share the same scenario, prompt, run counts, cache mode, connection mode, timeout, and concurrency unless defaults or per-target fields explicitly override provider settings.

Multi-target benchmarks execute targets serially in YAML order in v0.2. Serial execution is simple and reproducible, but it can be affected by time-of-day changes, provider-load drift, or transient network conditions between targets. For more rigorous comparisons:

- Run multiple independent passes.
- Consider alternating target order manually between passes until round-robin/randomized scheduling exists.
- Keep cache mode, connection mode, OpenAI API mode, reasoning effort, and service tier consistent for the comparison question.
- Never compare cached and uncached targets as if they represent the same condition.
- Treat requested service tier and observed provider-reported service tier as benchmark variables. Do not silently mix `auto`, `default`, `flex`, `scale`, and `priority` results.

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
