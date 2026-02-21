#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "$0")/../.." && pwd)"
cd "$ROOT_DIR"

node --test api/helpers/stream-tool-sieve.test.js api/chat-stream.test.js api/compat/js_compat_test.js "$@"
