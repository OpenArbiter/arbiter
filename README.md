# Arbiter

**A trust layer for software changes.**

Arbiter evaluates proposed software changes and produces a reliable merge decision — accepted, rejected, or needs action. It sits between contributors (humans or agents) and your source control system, gating merges on evidence rather than trust.

For design documentation and project roadmap, see the [design repo](https://github.com/OpenArbiter/design).

## How it works

```
GitHub PR → Arbiter webhook → evaluate evidence → merge decision → check run
```

Arbiter evaluates every proposal through five configurable gates:

1. **Mechanical** — build, lint, tests passing
2. **Policy** — security rules, dependency restrictions
3. **Behavioral** — sufficient test evidence
4. **Challenges** — structured objections resolved
5. **Scope** — changes stay within declared scope

Each gate can be set to **enforce** (blocks merge), **warn** (reports but doesn't block), or **skip**. Configure via `.arbiter.yml` in your repo.

## Quick Start

```bash
# Start Postgres + Redis
make docker-up

# Apply database migrations
docker compose exec postgres psql -U arbiter -d arbiter -f /dev/stdin < migrations/001_initial.sql

# Build
make build

# Run unit tests
make test

# Run integration tests (requires Docker services)
ARBITER_DB_URL="postgres://arbiter:arbiter@localhost:5432/arbiter?sslmode=disable" \
  ARBITER_REDIS_URL="redis://localhost:6380" \
  make test-integration

# Run the testing harness (no GitHub App needed)
make harness-scenarios

# Run security scans
make security-scan
```

## Configuration

Create `.arbiter.yml` in your repo root:

```yaml
gates:
  mechanical:
    mode: enforce
    checks:
      - build_check
      - test_suite
  policy:
    mode: enforce
  behavioral:
    mode: enforce
    min_passing_tests: 1
  challenges:
    mode: enforce
    block_on_severity: high
  scope:
    mode: warn
```

When no config file is present, sensible defaults are used. Config is always read from the **base branch**, not the PR branch.

## Project Structure

```
cmd/arbiter/       — application entrypoint
cmd/harness/       — local testing harness
internal/model/    — core types (Task, Proposal, Evidence, Challenge, Decision)
internal/store/    — Postgres storage layer
internal/engine/   — decision engine (pure function, config-driven)
internal/github/   — GitHub adapter (webhooks, API, processor)
internal/config/   — .arbiter.yml parsing
internal/queue/    — Redis job queue with retry + dead letter
migrations/        — SQL database migrations
```

## Architecture

See [ARCHITECTURE.md](ARCHITECTURE.md) for a quick reference, or the full [architecture docs](https://github.com/OpenArbiter/design/blob/main/docs/architecture.md) in the design repo.

## Development

Requirements: Go 1.22+, Docker, Docker Compose

```bash
make build              # Build the binary
make test               # Unit tests
make test-integration   # Integration tests (Postgres + Redis)
make test-all           # All tests
make lint               # golangci-lint
make security-scan      # semgrep + trufflehog + govulncheck
make harness-scenarios  # Run 9 predefined test scenarios
make harness-live       # Full pipeline against real Postgres
```

## Status

Active development. Phase 1 (Core Trust Layer) implementation complete. Pending GitHub App integration testing.

## License

Apache 2.0 — see [LICENSE](LICENSE)
