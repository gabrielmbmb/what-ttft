#!/usr/bin/env bash
# Run the full local quality gate for what-ttft, including no-network smoke tests.
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$ROOT_DIR"

run_step() {
  printf '\n==> %s\n' "$*"
  "$@"
}

run_step go test ./...
run_step go test -race ./...
run_step golangci-lint run
run_step go build ./...
run_step go run ./cmd/what-ttft run --help
run_step go run ./cmd/what-ttft bench --help
run_step scripts/smoke-fake-openai.sh
run_step scripts/smoke-fake-openai-bench.sh

printf '\nquality gate passed\n'
