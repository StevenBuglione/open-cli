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

## Cross-repository compatibility verification

If you change config semantics, catalog output, schema-facing behavior, or anything else that affects the public contract, also run the conformance suite from the companion repositories:

```bash
python3 /path/to/oas-cli-conformance/scripts/run_conformance.py \
  --schema-root /path/to/oas-cli-spec/schemas \
  --candidate /path/to/generated.ntc.json
```

At minimum, contributors should know that `make verify` proves the Go implementation is healthy, but it does **not** by itself prove that the generated artifacts still match the published OAS-CLI contract.

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
