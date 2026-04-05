# Arbiter — Agent Guide

## What this is

Arbiter is a trust layer for software changes. It evaluates proposed code changes through configurable gates and produces merge decisions.

## Build & Test

```bash
make build              # Build binary
make test               # Unit tests (no external deps)
make lint               # golangci-lint
make test-integration   # Requires ARBITER_DB_URL and ARBITER_REDIS_URL
make harness-scenarios  # 9 predefined test scenarios
make security-scan      # semgrep + trufflehog + govulncheck
```

## Architecture

- **Engine is pure** — `engine.Evaluate(*EvalContext) EvalResult`. No DB calls, no side effects. All data passed in, decision returned.
- **Config-driven** — all behavior controlled by `.arbiter.yml`. No recompilation to change gates or actions.
- **Append-only storage** — decisions are immutable. New evaluation = new record.
- **Pass large structs by pointer** — `EvalContext`, `Job`, `CheckRunOpts`, `ActionContext` are all passed as pointers.

## Code conventions

- Use `slog.InfoContext(ctx, ...)` (not `slog.Info`) — context carries correlation IDs.
- Integration tests use build tag `//go:build integration`. E2E tests use `//go:build e2e`.
- Store JSONB unmarshal errors are suppressed in lint (internal data written by us).
- Use index-based range loops for slices of large structs (`for i := range` not `for _, v := range`).

## Testing requirements

- Every exported function must have tests.
- Table-driven tests for multiple input/output combinations.
- Test error paths and edge cases, not just happy paths.
- Engine tests: pure — build an EvalContext, call Evaluate, check the result.
- Always run `make test && make lint` before committing.

## Dependencies

- Always use latest versions when adding new deps (`go get <pkg>@latest`).
- Verify with `go list -m -u <pkg>` after adding.
- Current external deps: pgx v5 (Postgres), go-redis v9, go-github v72, golang-jwt v5, yaml.v3.

## Key files

- `internal/engine/engine.go` — decision engine (the core)
- `internal/config/config.go` — .arbiter.yml schema and defaults
- `internal/github/processor.go` — webhook event → decision pipeline
- `internal/github/actions.go` — post-decision action execution
- `internal/store/store.go` — Store interface
- `cmd/harness/main.go` — local testing tool
