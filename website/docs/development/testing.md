---
title: Testing
---

# Testing

Verification has two layers:

- local implementation verification in this repository
- cross-repository contract verification with `oas-cli-conformance` and schemas from `oas-cli-spec`

## Baseline commands

From the repo root:

```bash
go test ./...
go build ./cmd/oascli ./cmd/oasclird
```

Repository convenience targets:

```bash
make verify
```

Current `make verify` runs:

- `gofmt -w $(find . -name '*.go' -print)`
- `go test ./...`
- `go build ./cmd/oascli ./cmd/oasclird`

## Docs verification

For documentation changes:

```bash
cd website
npm ci
npm run build
```

That verifies Markdown, sidebars, generated routes, and Docusaurus config. CI runs this build alongside root `make verify`, but `make verify` itself remains Go-only.

## Cross-project contract verification

`spec/` and `conformance/` live in this repository as first-class subprojects, so contract verification runs in-repo without checking out external repositories:

```bash
make verify-spec          # validate spec examples against schemas in spec/schemas/
make verify-conformance   # run conformance fixtures; uses spec/schemas/ automatically
make verify-all           # fmt + test + build + verify-spec + verify-conformance
```

`verify-conformance` uses `spec/schemas/` as the default schema root via the `OASCLI_SCHEMA_ROOT` fallback in `conformance/scripts/run_conformance.py`. You can override that with an explicit `--schema-root` flag if needed.

If you change config semantics, catalog output, schema-facing behavior, or anything else that affects the public contract, run `make verify-all` to confirm the Go implementation, the spec examples, and the conformance fixtures all agree.

## Product tests

`product-tests/` holds end-to-end tests that exercise the CLI against real infrastructure (REST API, OAuth stub, MCP servers). They are separate from `go test ./...` because they require Docker and `npx`.

### Smoke vs full harness

| Mode | Command | What it does | When to run |
| --- | --- | --- | --- |
| Smoke | `make product-test-smoke` | Checks prerequisites (Go, Docker, npx) and validates all `docker compose` config files without starting services | Every PR; runs in CI automatically |
| Full | `make product-test-full` | Validates smoke prerequisites/configs, starts services, then tears them down (capability test runs are a placeholder — no test targets execute yet) | Before merging changes that touch product behaviour or infra configs |

### Running product tests locally

```bash
# Quick sanity check — no services started
make product-test-smoke

# Full suite — requires Docker and outbound npm access on first run
make product-test-full
```

From inside `product-tests/` you can also target individual capability groups:

```bash
cd product-tests

# MCP stdio transport (uses npx, no Docker service needed)
make test-mcp-stdio

# MCP remote / streamable-http transport (starts a Docker container)
make test-mcp-remote

# Explicit control
make check-prereqs
make services-up
go test ./tests/... -run TestCapabilityCatalog -count=1 -v
make services-down
```

### What smoke validates in CI

CI runs `make product-test-smoke` on every push and pull request. That target:

1. Checks that Go, Docker, and `npx` are available in the runner.
2. Runs `docker compose config --quiet` against `product-tests/docker-compose.yml`.
3. Runs `docker compose config --quiet` against `product-tests/mcp/remote/docker-compose.yml`.

No containers are started and no network traffic occurs. If either compose file is malformed the job fails immediately.

## Useful targeted test entry points

- config merge/validation: `go test ./pkg/config -run TestLoadEffective`
- CLI runtime resolution and embedded mode: `go test ./cmd/oascli -run TestRootCommand`
- runtime HTTP API and auth/policy flows: `go test ./internal/runtime -run TestServer`
- discovery and catalog integration: `go test ./pkg/catalog -run TestBuild`

## What to test for common changes

### Config changes

- schema validation errors
- merge behavior across scopes
- cross-reference validation

### Discovery/catalog changes

- source provenance
- normalized tool IDs and command names
- workflow binding errors
- overlay effects

### Execution/auth changes

- header/query rendering
- retries
- approval gates
- secret resolution behavior

### Runtime HTTP API changes

- success payloads
- plain-text error behavior
- audit side effects

When you add or change behavior, prefer tests in the owning package instead of only end-to-end coverage.
