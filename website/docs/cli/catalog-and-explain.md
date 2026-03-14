---
title: Catalog and Explain
---

# Catalog and Explain

The safest way to understand an `oascli` setup is to inspect the catalog before executing tools.

## `catalog list`

`catalog list` prints the runtime response from `GET /v1/catalog/effective`:

```bash
./bin/oascli --embedded --config ./.cli.json catalog list --format pretty
```

The payload has two major sections:

- **`catalog`**: the full normalized catalog, including all services, tools, workflows, source provenance, and effective views
- **`view`**: the currently selected effective view after mode/profile selection

That split matters because the catalog is broader than the command tree you can currently invoke.

## `tool schema <tool-id>`

Use this command when you need machine-readable detail for one tool:

```bash
./bin/oascli --embedded --config ./.cli.json tool schema tickets:createTicket --format pretty
```

The schema includes fields such as:

- `id`, `serviceId`, `operationId`
- HTTP `method` and `path`
- `pathParams` and flag metadata
- request body contract and JSON schema
- extracted auth requirements
- safety metadata (`readOnly`, `destructive`, `requiresApproval`, `idempotent`)
- optional output, pagination, retry, and guidance hints

## `explain <tool-id>`

Use `explain` when skill manifests or overlays added operator guidance:

```bash
./bin/oascli --embedded --config ./.cli.json explain tickets:createTicket --format pretty
```

The response contains:

- `toolId`
- `guidance` (`whenToUse`, `avoidWhen`, and example commands)
- the tool summary and OpenAPI `operationId`

If no guidance was loaded, `explain` returns a minimal object containing only `toolId`.

## Important nuance: these commands look at the full catalog

`catalog list`, `tool schema`, and `explain` search the **full** catalog payload that came back from the runtime. Dynamic command generation, however, uses only the selected effective view.

That means you can sometimes inspect a tool by ID even when it is not present in the current curated command tree.

Practical implications:

- a tool may be visible to `tool schema` but absent from `oascli <service> <group> ...`
- `tool schema` does **not** imply that execution will be allowed
- approval requirements and curated restrictions are still enforced during execution

## Tool IDs vs command paths

Tool IDs are always based on the **service ID**, not the service alias. For example:

- command path: `oascli helpdesk tickets list-tickets`
- tool ID: `tickets:listTickets`

Use tool IDs with `tool schema`, `explain`, policy patterns, and workflow bindings.

## Suggested inspection order

1. `catalog list`
2. `tool schema <tool-id>`
3. `explain <tool-id>`
4. only then move on to [Tool execution](./tool-execution)
