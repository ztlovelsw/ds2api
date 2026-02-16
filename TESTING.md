# DS2API Testing Guide

## Overview

DS2API provides a live end-to-end testsuite that runs against your **local configured accounts** and records full artifacts for post-mortem debugging.

Entry points:

- `./scripts/testsuite/run-live.sh`
- `go run ./cmd/ds2api-tests`

## Quick Start

```bash
./scripts/testsuite/run-live.sh
```

Default behavior:

- runs preflight checks:
  - `go test ./... -count=1`
  - `node --check api/chat-stream.js`
  - `node --check api/helpers/stream-tool-sieve.js`
  - `npm run build --prefix webui`
- copies `config.json` into an isolated temporary config
- starts local server with `go run ./cmd/ds2api`
- executes live scenarios (OpenAI/Claude/Admin/stream/toolcall/concurrency)
- continues on failures and writes final summary

## CLI Flags

```bash
go run ./cmd/ds2api-tests \
  --config config.json \
  --admin-key admin \
  --out artifacts/testsuite \
  --port 0 \
  --timeout 120 \
  --retries 2 \
  --no-preflight=false
```

- `--config`: config file path (default `config.json`)
- `--admin-key`: admin key (default from `DS2API_ADMIN_KEY`, fallback `admin`)
- `--out`: artifact root directory (default `artifacts/testsuite`)
- `--port`: test server port (`0` = auto pick free port)
- `--timeout`: per request timeout in seconds (default `120`)
- `--retries`: retry count for network/5xx requests (default `2`)
- `--no-preflight`: skip preflight checks

## Artifact Layout

Each run creates:

`artifacts/testsuite/<run_id>/`

- `summary.json`: machine-readable report
- `summary.md`: human-readable report
- `server.log`: server stdout/stderr log during run
- `preflight.log`: preflight command outputs
- `cases/<case_id>/`
  - `request.json`
  - `response.headers`
  - `response.body`
  - `stream.raw`
  - `assertions.json`
  - `meta.json`

## Trace Binding (for fast debugging)

Each request includes:

- header: `X-Ds2-Test-Trace: <trace_id>`
- query: `__trace_id=<trace_id>`

When a case fails, `summary.md` includes trace IDs. You can locate related server logs quickly:

```bash
rg "<trace_id>" artifacts/testsuite/<run_id>/server.log
```

## Exit Code

- `0`: all cases passed
- `1`: one or more cases failed

This allows using the testsuite as a local release gate.

## Sensitive Data Warning

This testsuite stores **full raw request/response payloads** for debugging.

- Do not upload artifacts publicly.
- Do not share artifact directories in issue trackers without manual redaction.

