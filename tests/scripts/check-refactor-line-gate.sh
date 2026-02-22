#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "$0")/../.." && pwd)"
TARGETS_FILE="$ROOT_DIR/plans/refactor-line-gate-targets.txt"

DEFAULT_MAX=300
ENTRY_MAX=120

is_entry_file() {
  case "$1" in
    api/chat-stream.js|\
    api/helpers/stream-tool-sieve.js|\
    webui/src/App.jsx|\
    webui/src/components/AccountManager.jsx|\
    webui/src/components/ApiTester.jsx|\
    webui/src/components/Settings.jsx|\
    webui/src/components/VercelSync.jsx)
      return 0
      ;;
  esac
  return 1
}

if [[ ! -f "$TARGETS_FILE" ]]; then
  echo "missing targets file: $TARGETS_FILE" >&2
  exit 1
fi

missing=0
over=0
checked=0

while IFS= read -r file; do
  [[ -z "$file" ]] && continue
  [[ "${file:0:1}" == "#" ]] && continue

  checked=$((checked + 1))
  abs="$ROOT_DIR/$file"
  if [[ ! -f "$abs" ]]; then
    echo "MISSING $file"
    missing=$((missing + 1))
    continue
  fi

  lines="$(wc -l < "$abs" | tr -d ' ')"
  limit="$DEFAULT_MAX"
  if is_entry_file "$file"; then
    limit="$ENTRY_MAX"
  fi

  if (( lines > limit )); then
    echo "OVER $file lines=$lines limit=$limit"
    over=$((over + 1))
  fi
done < "$TARGETS_FILE"

echo "checked=$checked missing=$missing over_limit=$over"

if (( missing > 0 || over > 0 )); then
  exit 1
fi
