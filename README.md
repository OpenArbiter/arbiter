# Arbiter

**A trust layer for software changes.**

Arbiter evaluates proposed software changes and produces a reliable merge decision — accepted, rejected, or needs action.

For design documentation and project roadmap, see the [design repo](https://github.com/OpenArbiter/design).

## Quick Start

```bash
# Start dependencies
make docker-up

# Build
make build

# Run tests
make test

# Run all tests (unit + integration + e2e)
make test-all
```

## Project Structure

```
cmd/arbiter/       — application entrypoint
internal/model/    — core types (Task, Proposal, Evidence, Challenge, Decision)
internal/store/    — Postgres storage layer
internal/engine/   — decision engine
internal/github/   — GitHub adapter
internal/config/   — configuration loading
internal/queue/    — Redis job queue
migrations/        — Atlas database migrations
```

## License

Apache 2.0 — see [LICENSE](LICENSE)
