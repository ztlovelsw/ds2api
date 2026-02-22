# Refactor Line Gate

## Rules

1. Production file default upper bound: `<= 300` lines.
2. Entry/facade files upper bound: `<= 120` lines.
3. Scope is limited to target files in `plans/refactor-line-gate-targets.txt`.
4. Test files are out of scope for this gate.

## Command

```bash
./tests/scripts/check-refactor-line-gate.sh
```

## Naming Note

- Original split plan used `internal/admin/handler_accounts_test.go` for account probing logic.
- In Go, `*_test.go` files are test-only compilation units and cannot host production handlers.
- The production file is implemented as `internal/admin/handler_accounts_testing.go`.

