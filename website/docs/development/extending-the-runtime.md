---
title: Extending the Runtime
---

# Extending the Runtime

Most feature work follows the same package boundaries already present in the repo.

## Adding a new config field

Touch at least these places:

1. `pkg/config/types.go` for the public type
2. `pkg/config/cli.schema.json` for validation
3. `pkg/config/load.go` for merge behavior
4. `pkg/config/config_test.go` for merge/validation coverage
5. relevant docs under `website/docs/configuration/`

## Adding a new source type

You will usually need:

1. schema enum update in `pkg/config/cli.schema.json`
2. config type support in `pkg/config/types.go`
3. discovery implementation in `pkg/discovery/` or an adjacent package
4. catalog integration in `pkg/catalog/build.go`
5. tests covering direct use and catalog build integration

## Adding a new runtime endpoint

The runtime HTTP surface lives in `internal/runtime/server.go`.

Typical steps:

1. add the route in `Handler()`
2. add request/response structs if needed
3. implement the handler
4. add tests in `internal/runtime/server_test.go`
5. document the endpoint in `website/docs/runtime/http-api.md`

## Adding a new auth or secret mechanism

Depending on which part changes:

- auth extraction shape: `pkg/catalog/build.go`
- secret resolution: `internal/runtime/server.go`
- request application: `pkg/exec/exec.go`
- policy gate for secret sources: `pkg/config/*` plus `pkg/policy/` if needed

Be explicit about whether the new behavior is catalog metadata only or actively applied during execution.

## Adding a new CLI metadata extension

If you introduce a new `x-cli-*` extension:

1. parse it in `pkg/catalog/build.go`
2. store it in `pkg/catalog/types.go` if it needs a public normalized field
3. expose it through `tool schema` / `catalog list`
4. add catalog tests showing how overlays or OpenAPI set it
5. document it in `website/docs/discovery-catalog/normalized-tool-catalog.md`

## Observability integrations

If you want tracing/export hooks, build them behind `pkg/obs.Observer` and pass them into runtime/cache constructors. The current code already uses that interface consistently.

## Documentation updates that should ship with feature work

When behavior changes, update docs in the same change:

1. user-facing CLI/runtime/config changes belong under the matching section in `website/docs/`
2. repo front door or contributor workflow changes usually belong in `README.md` and `website/docs/development/`
3. docs navigation or site-wide metadata changes live in `website/sidebars.ts` and `website/docusaurus.config.ts`
4. verify docs with `cd website && npm ci && npm run build`

## Guardrails for contributors

- keep policy enforcement inside the runtime, not just the CLI
- keep config loading/merge behavior in `pkg/config`
- keep reusable discovery/catalog logic out of `cmd/`
- update docs when behavior changes, especially for quirks and unsupported cases
