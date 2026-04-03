#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "$0")/../.." && pwd)"
cd "$ROOT_DIR"

REPORT_DIR="artifacts/raw-stream-sim"
mkdir -p "$REPORT_DIR"
REPORT_PATH="$REPORT_DIR/report-$(date -u +%Y%m%dT%H%M%SZ).json"

node tests/tools/deepseek-sse-simulator.mjs \
  --samples-root tests/raw_stream_samples \
  --report "$REPORT_PATH" \
  "$@"

echo "[run-raw-stream-sim] report: $REPORT_PATH"
