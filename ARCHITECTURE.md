# Architecture

For full internal architecture documentation, see the [design repo](https://github.com/OpenArbiter/design/blob/main/docs/architecture.md).

## Quick Reference

```
cmd/arbiter/       → entrypoint
internal/model/    → core types (Task, Proposal, Evidence, Challenge, Decision)
internal/config/   → .arbiter.yml parsing
internal/engine/   → decision engine (pure function: EvalContext → Decision)
internal/store/    → Postgres persistence
internal/queue/    → Redis job queue
internal/github/   → GitHub adapter
migrations/        → SQL migrations
```

### Key design decisions

- **Engine is pure** — no DB calls, no side effects. Caller gathers data, engine evaluates.
- **Config-driven** — gate modes (enforce/warn/skip), thresholds, and policies all in `.arbiter.yml`.
- **No short-circuiting** — all gates run, all issues reported at once.
- **Append-only storage** — decisions are immutable. New evaluation = new Decision record.
