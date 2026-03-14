---
title: Repository Layout
---

# Repository Layout

The main implementation lives in a compact set of directories.

## Top-level structure

```text
.github/
  workflows/
    ci.yml              Go verification, spec/conformance validation, docs build
    docs-pages.yml      GitHub Pages deployment for website/
cmd/
  oascli/               CLI entrypoint and runtime client
  oasclird/             daemon entrypoint
internal/
  runtime/              HTTP API handlers and runtime wiring
pkg/
  audit/                audit log storage
  cache/                HTTP cache store and fetcher
  catalog/              normalized catalog build logic
  config/               config types, schema, discovery, merge, validation
  discovery/            RFC 9727 and RFC 8631 discovery helpers
  exec/                 upstream HTTP execution and auth application
  instance/             instance ID derivation and state/cache path resolution
  obs/                  observer abstraction used by runtime and cache code
  openapi/              document loading, overlay application, reference resolution
  overlay/              overlay document model and JSONPath-based patch engine
  policy/               execution-time policy decisions
spec/                   normative OAS-CLI specification and JSON schemas
  spec/                 prose specifications (core, config, profiles)
  schemas/              machine-readable JSON schemas for published artifacts
  examples/             example documents validated in spec CI
  scripts/              spec validation scripts
conformance/            language-neutral conformance fixtures and expected outputs
  fixtures/             discovery, OpenAPI, overlay, workflow, and config inputs
  expected/             expected normalized outputs
  scripts/              conformance runner
  compatibility-matrix.json  machine-readable suite/spec/implementation compatibility
website/
  docs/                 Docusaurus content
  src/pages/            docs landing page and any custom React pages
  src/css/              site-specific styling
  sidebars.ts           documentation navigation
  docusaurus.config.ts  site URL, baseUrl, navbar, footer, and docs settings
  package.json          docs scripts and Node dependency entrypoint
Makefile                verify, verify-spec, verify-conformance, verify-all targets
README.md               repository front door and install/verify summary
```

## Where to look first

| If you need to change... | Start here |
| --- | --- |
| CLI flags, output, runtime client behavior | `cmd/oascli/main.go` |
| daemon startup, flags, registry writing | `cmd/oasclird/main.go` |
| runtime endpoints | `internal/runtime/server.go` |
| config schema or merge behavior | `pkg/config/` |
| discovery behavior | `pkg/discovery/` |
| catalog normalization | `pkg/catalog/build.go` and `pkg/catalog/types.go` |
| auth application or retries | `pkg/exec/exec.go` |
| cache behavior | `pkg/cache/` |
| audit storage | `pkg/audit/store.go` |
| instance paths and registry files | `pkg/instance/instance.go` |
| normative spec or JSON schemas | `spec/` |
| conformance fixtures or expected outputs | `conformance/` |
| docs content | `website/docs/` |
| docs navigation and site metadata | `website/sidebars.ts` and `website/docusaurus.config.ts` |
| CI or Pages deployment | `.github/workflows/ci.yml` and `.github/workflows/docs-pages.yml` |
| repo landing page and contribution expectations | `README.md` and `website/docs/development/` |

## Tests by area

The repo keeps tests close to the packages they exercise:

- `cmd/oascli/main_test.go`
- `internal/runtime/server_test.go`
- `pkg/config/config_test.go`
- `pkg/catalog/*.go` tests
- `pkg/discovery/discovery_test.go`
- `pkg/cache/cache_test.go`
- `pkg/overlay/overlay_test.go`
