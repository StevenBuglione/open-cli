---
title: Normalized Tool Catalog
---

# Normalized Tool Catalog

The normalized catalog is the contract between discovery and the CLI/runtime surfaces.

## Service records

Each normalized service contains:

- `id`
- `alias`
- `sourceId`
- `title`
- `servers`

If you do not set an alias, the alias defaults to the service ID.

## Tool IDs

Tool IDs are built like this:

```text
<service-id>:<operation-id>
```

If an OpenAPI operation has no `operationId`, the current fallback is:

```text
<HTTP-METHOD>:<raw-path>
```

Example: `tickets:getTicket` or `tickets:get:/tickets/{id}`.

### Contributor note

There is no collision detection for duplicate tool IDs. Keep `operationId` values unique within a service.

## Command derivation

For each supported operation (`GET`, `POST`, `PUT`, `PATCH`, `DELETE`), the catalog builder computes:

- group
- command name
- aliases
- hidden/ignored state
- parameters
- request body schema
- auth requirements
- safety hints

## Supported OpenAPI extensions

The current builder understands these operation-level extensions:

- `x-cli-group`
- `x-cli-name`
- `x-cli-description`
- `x-cli-aliases`
- `x-cli-hidden`
- `x-cli-ignore`
- `x-cli-output`
- `x-cli-pagination`
- `x-cli-retry`
- `x-cli-safety`

And this parameter-level extension:

- `x-cli-name`

## Safety defaults

Without overrides:

- `GET` is read-only and idempotent
- `DELETE` is destructive and idempotent
- `PUT` is idempotent
- approval is off unless `x-cli-safety.requiresApproval` sets it or policy requires it

## Request body metadata

For operations with request bodies, the catalog stores:

- whether the body is required
- available content types
- a machine-readable schema snapshot for each content type

This is what powers `tool schema`.

## Auth extraction

Auth requirements are extracted from the operation or document security definitions and normalized into:

- security scheme name
- type
- scheme
- `in`
- parameter name

Actual secret resolution happens later during execution.

## Effective views

The catalog always contains:

- a `discover` view with every tool
- one additional view per configured agent profile

`ocli` uses the selected view to decide which dynamic commands to render.

## Workflows in the catalog

Arazzo workflows are stored as normalized workflow IDs plus step IDs bound to tool IDs. If a workflow references an ignored or missing operation, catalog build fails.
