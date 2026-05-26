#!/usr/bin/env bash
# Run the what-ttft CLI against a local deterministic fake OpenAI-compatible server.
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
OUT_DIR="${1:-$ROOT_DIR/runs/fake-openai-smoke-$(date -u +%Y%m%dT%H%M%SZ)}"
API_KEY="what-ttft-smoke-test-key"
TMP_DIR="$(mktemp -d "${TMPDIR:-/tmp}/what-ttft-smoke.XXXXXX")"
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

cat > "$REPO_TMP_DIR/fake_openai_server.go" <<'GOEOF'
package main

import (
	"fmt"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/gabrielmbmb/what-ttft/internal/testserver"
)

func main() {
	server := testserver.NewOpenAIServer(testserver.OpenAIConfig{
		DelayBeforeHeaders:      5 * time.Millisecond,
		DelayBeforeFirstEvent:   5 * time.Millisecond,
		DelayBeforeFirstContent: 5 * time.Millisecond,
		DelayBetweenSteps:       2 * time.Millisecond,
		Steps: []testserver.StreamStep{
			{Comment: "smoke heartbeat"},
			{Data: `{"type":"response.created","response":{"status":"in_progress","service_tier":"default"}}`},
			{Data: `{"type":"response.output_text.delta","delta":"Hello"}`},
			{Data: `{"type":"response.output_text.delta","delta":" smoke"}`},
			{Data: `{"type":"response.completed","response":{"status":"completed","service_tier":"default","usage":{"input_tokens":4,"input_tokens_details":{"cached_tokens":0},"output_tokens":2,"output_tokens_details":{"reasoning_tokens":0},"total_tokens":6}}}`},
		},
	})
	defer server.Close()

	fmt.Println(server.URL())

	signals := make(chan os.Signal, 1)
	signal.Notify(signals, os.Interrupt, syscall.SIGTERM)
	<-signals
}
GOEOF

CLI_BIN="$TMP_DIR/what-ttft"
SERVER_BIN="$TMP_DIR/fake-openai-server"
SERVER_URL_FILE="$TMP_DIR/server.url"
SERVER_LOG="$TMP_DIR/server.log"

(
  cd "$ROOT_DIR"
  go build -o "$CLI_BIN" ./cmd/what-ttft
  go build -o "$SERVER_BIN" "$REPO_TMP_DIR/fake_openai_server.go"
)

"$SERVER_BIN" > "$SERVER_URL_FILE" 2> "$SERVER_LOG" &
SERVER_PID="$!"

for _ in {1..100}; do
  if [[ -s "$SERVER_URL_FILE" ]]; then
    break
  fi
  if ! kill -0 "$SERVER_PID" 2>/dev/null; then
    echo "fake OpenAI server exited before printing its URL" >&2
    cat "$SERVER_LOG" >&2 || true
    exit 1
  fi
  sleep 0.05
done

if [[ ! -s "$SERVER_URL_FILE" ]]; then
  echo "timed out waiting for fake OpenAI server URL" >&2
  cat "$SERVER_LOG" >&2 || true
  exit 1
fi

SERVER_URL="$(head -n 1 "$SERVER_URL_FILE")"

"$CLI_BIN" run \
  --provider openai \
  --base-url "$SERVER_URL" \
  --api-key "$API_KEY" \
  --model gpt-smoke \
  --prompt "Say hello for the what-ttft fake-server smoke test." \
  --samples 2 \
  --warmup 1 \
  --concurrency 1 \
  --cache-mode cache-reuse \
  --connection-mode warm \
  --reasoning-effort none \
  --service-tier default \
  --max-output-tokens 16 \
  --timeout 10s \
  --out "$OUT_DIR" \
  --save-chunks=true \
  --include-usage=true \
  --overwrite

for report_file in run.json requests.jsonl chunks.jsonl summary.json summary.md; do
  if [[ ! -f "$OUT_DIR/$report_file" ]]; then
    echo "missing expected report file: $OUT_DIR/$report_file" >&2
    exit 1
  fi
done

python3 - <<'PY' "$OUT_DIR" "$API_KEY"
import json
import pathlib
import sys

out_dir = pathlib.Path(sys.argv[1])
api_key = sys.argv[2]

requests = [json.loads(line) for line in (out_dir / "requests.jsonl").read_text(encoding="utf-8").splitlines() if line.strip()]
if len(requests) != 3:
    raise SystemExit(f"expected 3 request records including warmup, got {len(requests)}")
if sum(1 for request in requests if request.get("warmup")) != 1:
    raise SystemExit("expected exactly one warmup request record")
for request in requests:
    if request.get("provider") != "openai":
        raise SystemExit(f"unexpected provider in request record: {request.get('provider')!r}")
    if request.get("model") != "gpt-smoke":
        raise SystemExit(f"unexpected model in request record: {request.get('model')!r}")
    if request.get("error") is not None:
        raise SystemExit(f"request contained unexpected error: {request.get('error')}")

summary = json.loads((out_dir / "summary.json").read_text(encoding="utf-8"))
if summary.get("total_requests") != 3 or summary.get("warmup_requests") != 1 or summary.get("measured_requests") != 2:
    raise SystemExit(f"unexpected summary request counts: {summary}")
if summary.get("successful_requests") != 2 or summary.get("error_requests") != 0:
    raise SystemExit(f"unexpected summary success/error counts: {summary.get('successful_requests')}/{summary.get('error_requests')}")
groups = summary.get("groups", [])
if len(groups) != 1:
    raise SystemExit(f"expected one summary group, got {len(groups)}")
group = groups[0]
if group.get("total_completion_tokens") != 4 or group.get("completion_token_records") != 2:
    raise SystemExit(f"unexpected completion-token summary: {group}")
metrics = group.get("metrics", {})
if metrics.get("ttft_delta_ms", {}).get("count") != 2:
    raise SystemExit(f"missing ttft_delta_ms distribution: {metrics.get('ttft_delta_ms')}")
if metrics.get("e2e_output_tps", {}).get("count") != 2:
    raise SystemExit(f"missing e2e_output_tps distribution: {metrics.get('e2e_output_tps')}")

summary_md = (out_dir / "summary.md").read_text(encoding="utf-8")
for expected in ("ttft_delta_ms", "e2e_output_tps", "completion_tokens"):
    if expected not in summary_md:
        raise SystemExit(f"summary.md missing {expected!r}")

for path in out_dir.rglob("*"):
    if path.is_file() and api_key in path.read_text(encoding="utf-8", errors="ignore"):
        raise SystemExit(f"smoke API key leaked into {path}")
PY

if grep -R --fixed-strings "$API_KEY" "$OUT_DIR" >/dev/null; then
  echo "smoke API key leaked into output files under $OUT_DIR" >&2
  exit 1
fi

echo "fake OpenAI smoke test passed; wrote reports to $OUT_DIR"
