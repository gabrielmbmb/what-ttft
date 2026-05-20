#!/usr/bin/env bash
# Run the what-ttft bench CLI against a local deterministic fake OpenAI-compatible server.
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
OUT_DIR="${1:-$ROOT_DIR/runs/fake-openai-bench-smoke-$(date -u +%Y%m%dT%H%M%SZ)}"
API_KEY="what-ttft-bench-smoke-test-key"
API_KEY_ENV="WHAT_TTFT_SMOKE_OPENAI_API_KEY"
TMP_DIR="$(mktemp -d "${TMPDIR:-/tmp}/what-ttft-bench-smoke.XXXXXX")"
REPO_TMP_DIR="$(mktemp -d "$ROOT_DIR/.what-ttft-smoke.XXXXXX")"
SERVER_PID=""

cleanup() {
  if [[ -n "$SERVER_PID" ]] && kill -0 "$SERVER_PID" 2>/dev/null; then
    kill "$SERVER_PID" 2>/dev/null || true
    wait "$SERVER_PID" 2>/dev/null || true
  fi
  rm -rf "$TMP_DIR" "$REPO_TMP_DIR"
}
trap cleanup EXIT

cat > "$REPO_TMP_DIR/fake_openai_bench_server.go" <<'GOEOF'
package main

import (
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"
)

type requestRecord struct {
	Path                 string `json:"path"`
	Model                string `json:"model"`
	Stream               bool   `json:"stream"`
	ServiceTier          string `json:"service_tier"`
	AuthorizationPresent bool   `json:"authorization_present"`
}

type requestBody struct {
	Model       string `json:"model"`
	Stream      bool   `json:"stream"`
	ServiceTier string `json:"service_tier"`
}

func main() {
	if len(os.Args) != 2 {
		fmt.Fprintln(os.Stderr, "usage: fake-openai-bench-server REQUEST_LOG")
		os.Exit(2)
	}
	requestLog := os.Args[1]
	var mu sync.Mutex

	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/responses" && r.URL.Path != "/v1/responses" {
			http.NotFound(w, r)
			return
		}
		if r.Method != http.MethodPost {
			http.Error(w, "method must be POST", http.StatusMethodNotAllowed)
			return
		}

		rawBody, err := io.ReadAll(r.Body)
		if err != nil {
			http.Error(w, "read body", http.StatusBadRequest)
			return
		}
		var body requestBody
		if err := json.Unmarshal(rawBody, &body); err != nil {
			http.Error(w, "decode body", http.StatusBadRequest)
			return
		}

		record := requestRecord{
			Path:                 r.URL.Path,
			Model:                body.Model,
			Stream:               body.Stream,
			ServiceTier:          body.ServiceTier,
			AuthorizationPresent: r.Header.Get("Authorization") != "",
		}
		if err := appendRequest(requestLog, &mu, record); err != nil {
			http.Error(w, "record request", http.StatusInternalServerError)
			return
		}

		if !record.AuthorizationPresent {
			http.Error(w, "authorization header is required", http.StatusUnauthorized)
			return
		}
		if !record.Stream {
			http.Error(w, "stream must be true", http.StatusBadRequest)
			return
		}
		if record.ServiceTier != "default" {
			http.Error(w, "service_tier must be default", http.StatusBadRequest)
			return
		}

		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("openai-processing-ms", "5")
		writeSSE(w, "response.created", `{"type":"response.created","response":{"status":"in_progress","service_tier":"default"}}`)
		writeSSE(w, "response.output_text.delta", `{"type":"response.output_text.delta","delta":"Hello"}`)
		time.Sleep(30 * time.Millisecond)
		writeSSE(w, "response.output_text.delta", `{"type":"response.output_text.delta","delta":" smoke"}`)
		time.Sleep(30 * time.Millisecond)
		writeSSE(w, "response.output_text.delta", `{"type":"response.output_text.delta","delta":" test"}`)
		writeSSE(w, "response.completed", `{"type":"response.completed","response":{"status":"completed","service_tier":"default","usage":{"input_tokens":4,"input_tokens_details":{"cached_tokens":0},"output_tokens":6,"output_tokens_details":{"reasoning_tokens":0},"total_tokens":10}}}`)
	})

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		fmt.Fprintf(os.Stderr, "listen: %v\n", err)
		os.Exit(1)
	}
	server := &http.Server{Handler: mux, ReadHeaderTimeout: 5 * time.Second}
	go func() {
		if err := server.Serve(listener); err != nil && err != http.ErrServerClosed {
			fmt.Fprintf(os.Stderr, "serve: %v\n", err)
		}
	}()

	fmt.Printf("http://%s\n", listener.Addr().String())

	signals := make(chan os.Signal, 1)
	signal.Notify(signals, os.Interrupt, syscall.SIGTERM)
	<-signals
}

func appendRequest(path string, mu *sync.Mutex, record requestRecord) error {
	mu.Lock()
	defer mu.Unlock()

	file, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	defer func() { _ = file.Close() }()

	return json.NewEncoder(file).Encode(record)
}

func writeSSE(w http.ResponseWriter, event string, data string) {
	_, _ = fmt.Fprintf(w, "event: %s\n", event)
	_, _ = fmt.Fprintf(w, "data: %s\n\n", data)
	if flusher, ok := w.(http.Flusher); ok {
		flusher.Flush()
	}
}
GOEOF

CLI_BIN="$TMP_DIR/what-ttft"
SERVER_BIN="$TMP_DIR/fake-openai-bench-server"
SERVER_URL_FILE="$TMP_DIR/server.url"
SERVER_LOG="$TMP_DIR/server.log"
REQUEST_LOG="$TMP_DIR/requests.jsonl"
CONFIG_FILE="$TMP_DIR/benchmark.yaml"

(
  cd "$ROOT_DIR"
  go build -o "$CLI_BIN" ./cmd/what-ttft
  go build -o "$SERVER_BIN" "$REPO_TMP_DIR/fake_openai_bench_server.go"
)

"$SERVER_BIN" "$REQUEST_LOG" > "$SERVER_URL_FILE" 2> "$SERVER_LOG" &
SERVER_PID="$!"

for _ in {1..100}; do
  if [[ -s "$SERVER_URL_FILE" ]]; then
    break
  fi
  if ! kill -0 "$SERVER_PID" 2>/dev/null; then
    echo "fake OpenAI bench server exited before printing its URL" >&2
    cat "$SERVER_LOG" >&2 || true
    exit 1
  fi
  sleep 0.05
done

if [[ ! -s "$SERVER_URL_FILE" ]]; then
  echo "timed out waiting for fake OpenAI bench server URL" >&2
  cat "$SERVER_LOG" >&2 || true
  exit 1
fi

SERVER_URL="$(head -n 1 "$SERVER_URL_FILE")"

cat > "$CONFIG_FILE" <<YAMLEOF
schema_version: 1
name: fake-openai-bench-smoke

defaults:
  provider: openai
  base_url: $SERVER_URL
  api_key_env: $API_KEY_ENV
  service_tier: default

run:
  samples: 1
  warmup: 0
  concurrency: 1
  cache_mode: cache-reuse
  connection_mode: warm
  timeout: 10s
  save_chunks: false

scenario:
  name: bench-smoke-short
  prompt: Say hello for the what-ttft fake-server bench smoke test.
  max_output_tokens: 16
  reasoning_effort: none

targets:
  - id: target-a
    name: Target A
    model: gpt-smoke-a
  - id: target-b
    name: Target B
    model: gpt-smoke-b
YAMLEOF

export "$API_KEY_ENV=$API_KEY"
"$CLI_BIN" bench \
  --config "$CONFIG_FILE" \
  --out "$OUT_DIR" \
  --overwrite

for report_file in run.json requests.jsonl summary.json summary.md; do
  if [[ ! -f "$OUT_DIR/$report_file" ]]; then
    echo "missing expected report file: $OUT_DIR/$report_file" >&2
    exit 1
  fi
done

python3 - <<'PY' "$REQUEST_LOG" "$OUT_DIR" "$API_KEY" "$API_KEY_ENV"
import json
import pathlib
import sys

request_log = pathlib.Path(sys.argv[1])
out_dir = pathlib.Path(sys.argv[2])
api_key = sys.argv[3]
api_key_env = sys.argv[4]

requests = [json.loads(line) for line in request_log.read_text(encoding="utf-8").splitlines() if line.strip()]
if len(requests) != 2:
    raise SystemExit(f"expected 2 fake server requests, got {len(requests)}")
models = sorted(request["model"] for request in requests)
if models != ["gpt-smoke-a", "gpt-smoke-b"]:
    raise SystemExit(f"unexpected models: {models}")
for request in requests:
    if request["path"] != "/responses":
        raise SystemExit(f"request path {request['path']!r} was not /responses")
    if request["service_tier"] != "default":
        raise SystemExit(f"request service_tier {request['service_tier']!r} was not default")
    if not request["stream"]:
        raise SystemExit("request stream flag was false")
    if not request["authorization_present"]:
        raise SystemExit("authorization header was not present")

run = json.loads((out_dir / "run.json").read_text(encoding="utf-8"))
if run.get("benchmark_name") != "fake-openai-bench-smoke":
    raise SystemExit(f"unexpected benchmark name: {run.get('benchmark_name')!r}")
if run.get("target_order") != "serial":
    raise SystemExit(f"unexpected target order: {run.get('target_order')!r}")
if len(run.get("targets", [])) != 2:
    raise SystemExit("run.json does not contain two targets")
for target in run["targets"]:
    if target.get("provider_api") != "responses":
        raise SystemExit(f"target API was not responses: {target}")
    if target.get("requested_service_tier") != "default":
        raise SystemExit(f"target requested tier was not default: {target}")
    if target.get("api_key_env") != api_key_env:
        raise SystemExit(f"target api_key_env mismatch: {target}")
    if target.get("observed_service_tier") != "default":
        raise SystemExit(f"target observed tier was not default: {target}")

summary = json.loads((out_dir / "summary.json").read_text(encoding="utf-8"))
if summary.get("successful_requests") != 2 or summary.get("error_requests") != 0:
    raise SystemExit(f"unexpected summary success/error counts: {summary.get('successful_requests')}/{summary.get('error_requests')}")
groups = {group.get("target_id"): group for group in summary.get("groups", [])}
if sorted(groups) != ["target-a", "target-b"]:
    raise SystemExit(f"unexpected summary groups: {sorted(groups)}")
for target_id, group in groups.items():
    if group.get("model") not in {"gpt-smoke-a", "gpt-smoke-b"}:
        raise SystemExit(f"unexpected group model for {target_id}: {group.get('model')}")
    if group.get("requested_service_tier") != "default":
        raise SystemExit(f"unexpected group tier for {target_id}: {group.get('requested_service_tier')}")
    if group.get("total_completion_tokens") != 6:
        raise SystemExit(f"unexpected completion tokens for {target_id}: {group.get('total_completion_tokens')}")
    metrics = group.get("metrics", {})
    if metrics.get("e2e_output_tps", {}).get("count") != 1:
        raise SystemExit(f"missing e2e_output_tps for {target_id}: {metrics.get('e2e_output_tps')}")
    if metrics.get("generation_delta_output_tps", {}).get("count") != 1:
        raise SystemExit(f"missing generation_delta_output_tps for {target_id}: {metrics.get('generation_delta_output_tps')}")

summary_md = (out_dir / "summary.md").read_text(encoding="utf-8")
for expected in ("## Target comparison", "target-a", "target-b", "e2e tps mean", "generation tps mean"):
    if expected not in summary_md:
        raise SystemExit(f"summary.md missing {expected!r}")

for path in out_dir.rglob("*"):
    if path.is_file() and api_key in path.read_text(encoding="utf-8", errors="ignore"):
        raise SystemExit(f"smoke API key leaked into {path}")
PY

if grep -R --fixed-strings "$API_KEY" "$OUT_DIR" >/dev/null; then
  echo "bench smoke API key leaked into output files under $OUT_DIR" >&2
  exit 1
fi

echo "fake OpenAI bench smoke test passed; wrote reports to $OUT_DIR"
