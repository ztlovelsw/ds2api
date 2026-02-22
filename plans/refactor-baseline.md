# DS2API Refactor Baseline (Backfilled)

- Recorded at: `2026-02-22T08:53:54Z`
- Branch: `dev`
- HEAD: `5d3989a`
- Scope: backend + node api + webui large-file decoupling (no behavior change)

## Gate Commands

1. `./tests/scripts/run-unit-all.sh`
   - Result: PASS
   - Includes:
     - `go test ./...`
     - `node --test api/helpers/stream-tool-sieve.test.js api/chat-stream.test.js api/compat/js_compat_test.js`
2. `npm --prefix webui run build`
   - Result: PASS
3. `./tests/scripts/check-refactor-line-gate.sh`
   - Result: PASS (`checked=131 missing=0 over_limit=0`)
4. Stage gates (1-5) replay:
   - `go test ./internal/config ./internal/admin ./internal/account ./internal/deepseek ./internal/format/openai` -> PASS
   - `go test ./internal/adapter/openai ./internal/util ./internal/sse ./internal/compat` -> PASS
   - `go test ./internal/adapter/claude ./internal/adapter/gemini ./internal/config` -> PASS
   - `go test ./internal/testsuite ./cmd/ds2api-tests` -> PASS
   - `node --test api/helpers/stream-tool-sieve.test.js api/chat-stream.test.js api/compat/js_compat_test.js` -> PASS
5. Final full regression:
   - `go test ./... -count=1` -> PASS

## Notes

- This baseline file is a backfill artifact for phase 0 process tracking.
- Frontend manual smoke for phase 6 still requires human execution and sign-off.
