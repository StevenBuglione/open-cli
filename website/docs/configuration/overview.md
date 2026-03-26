---
title: Configuration Overview
---

# Configuration Overview

**Read this if** you are an operator writing or tuning `.cli.json` for your environment. This page answers: what does each top-level config block control, how do sources and services relate, and what is the minimum viable config vs a fully curated one.

`ocli` and `open-cli-toolbox` are driven by **JSON config files** named `.cli.json` (plus optional scope variants described in [Scope merging](./scope-merging)).

## What the config controls

A config file answers these questions:

- where runtime execution happens (`runtime`)
- where discovery starts (`sources`)
- how native MCP servers are registered (`sources.type="mcp"` or `mcpServers`)
- which discovered or explicit APIs become named services (`services`)
- how tools are filtered for different audiences (`curation` and `agents`)
- which tools require approval or secret execution (`policy`)
- how upstream auth secrets are resolved (`secrets`)

## A complete working example

```json
{
  "cli": "1.0.0",
  "mode": { "default": "curated" },
  "sources": {
    "ticketsSource": {
      "type": "openapi",
      "uri": "./tickets.openapi.yaml",
      "enabled": true,
      "refresh": {
        "maxAgeSeconds": 300
      }
    }
  },
  "services": {
    "tickets": {
      "source": "ticketsSource",
      "alias": "helpdesk",
      "overlays": ["./overlays/tickets.overlay.yaml"],
      "skills": ["./skills/tickets.skill.json"],
      "workflows": ["./workflows/tickets.arazzo.yaml"]
    }
  },
  "curation": {
    "toolSets": {
      "support": {
        "allow": [
          "tickets:listTickets",
          "tickets:getTicket",
          "tickets:createTicket"
        ],
        "deny": ["**"]
      }
    }
  },
  "agents": {
    "profiles": {
      "support": {
        "mode": "curated",
        "toolSet": "support"
      }
    },
    "defaultProfile": "support"
  },
  "policy": {
    "approvalRequired": ["tickets:createTicket"]
  },
  "secrets": {
    "bearerAuth": {
      "type": "env",
      "value": "HELPDESK_TOKEN"
    }
  }
}
```

## Reading the example top to bottom

### `cli`

A required version-like string. The loader validates that it is present and non-empty.

### `mode.default`

The default runtime mode. Current meaningful values are:

- `discover`
- `curated`

### `runtime`

Controls how `ocli` reaches execution:

- `remote`: the only supported value; point `ocli` at a hosted `open-cli-toolbox` runtime

`runtime.local` is retained only for legacy schema compatibility and is rejected by the current CLI.

`runtime.remote` carries the remote runtime base URL and optional runtime-auth configuration. The current CLI supports:

- `providedToken`: forward a bearer token referenced by `tokenRef` such as `env:OAS_REMOTE_TOKEN`
- `oauthClient`: acquire a client-credentials token for the remote runtime before calling it
- `browserLogin`: fetch runtime-hosted browser-login metadata and complete an authorization-code + PKCE flow

`runtime.server.auth` configures remote-runtime enforcement on `open-cli-toolbox`. The runtime supports generalized validation profiles such as `oidc_jwks` and `oauth2_introspection`, while preserving the same fail-closed behavior: authenticate bearer tokens, derive runtime scopes, filter catalogs, and reject out-of-scope execution.

### `sources`

Discovery starting points. Supported source types are:

- `openapi`
- `serviceRoot`
- `apiCatalog`
- `mcp`

`mcp` sources use `transport` instead of `uri` and support:

- `stdio` for local MCP processes
- legacy `sse` for older HTTP+SSE MCP servers
- `streamable-http` for current MCP HTTP transports

Non-MCP sources (`openapi`, `serviceRoot`, and `apiCatalog`) use `uri` and do not accept MCP-only fields such as `transport`, `disabledTools`, or source-local `oauth`.

Each source can also define refresh behavior. For MCP sources, `disabledTools` removes specific discovered MCP tools before normalization.

That removal is **fail-closed**, not cosmetic. If a service overlay, workflow, or policy pattern still references a disabled MCP tool, catalog build now fails with a source-scoped error instead of silently ignoring the reference.

### `services`

Named services attach operator intent to a source:

- human-friendly alias
- overlays
- skill manifests
- Arazzo workflows

Explicit `services` entries are the operator-controlled way to attach those extra documents. Metadata discovery can also contribute overlays, skills, and workflows for discovered services when the upstream service metadata advertises them.

### `curation` and `agents`

These two blocks work together:

- `curation.toolSets` defines allow/deny patterns
- `agents.profiles` picks a mode and tool set
- `agents.defaultProfile` decides which curated profile to use by default

### `policy`

Runtime execution gates that are independent from the dynamic command tree, such as `approvalRequired` and `allowExecSecrets`.

### `secrets`

Maps OpenAPI security scheme names to secret resolution instructions.

Top-level `secrets` also carry `type: "oauth2"` entries for OpenAPI `oauth2` / `openIdConnect` execution and static values referenced from MCP `transport.headerSecrets`.

## `mcpServers` compatibility

If you already have `.mcp.json`-style config, use top-level `mcpServers` and let the loader normalize it into canonical `sources` + `services`.

```json
{
  "cli": "1.0.0",
  "mode": { "default": "discover" },
  "mcpServers": {
    "filesystem": {
      "type": "stdio",
      "command": "npx",
      "args": ["-y", "@modelcontextprotocol/server-filesystem", "/workspace"]
    }
  }
}
```

That compatibility form is great for migration, but the normalized runtime shape is still source-centric.

## Relative path resolution

Relative references such as:

- `sources.*.uri` for `openapi` sources
- `services.*.overlays`
- `services.*.skills`
- `services.*.workflows`

are resolved from the effective config base directory. In the common case, that means the directory containing your project `.cli.json` or `.cli.local.json`.

## Minimal config vs fully curated config

A minimal config only needs:

```json
{
  "cli": "1.0.0",
  "mode": { "default": "discover" },
  "sources": {
    "ticketsSource": {
      "type": "openapi",
      "uri": "./tickets.openapi.yaml",
      "enabled": true
    }
  }
}
```

If you stop there, unreferenced sources are still processed directly. The runtime will derive a service ID automatically, but you will not get an explicit alias unless you add a `services` entry. Overlays, skills, and workflows can still arrive indirectly from discovered service metadata when that source type supports them.

## If you are trying to…

| Goal | Go to |
| --- | --- |
| Understand how multiple `.cli.json` files merge at runtime | [Scope merging](./scope-merging) |
| Control which tools each agent profile can see | [Modes and profiles](./modes-and-profiles) |
| Look up every field and default value | [Config schema](./config-schema) |
| Connect a remote runtime with auth | [Deployment models](../runtime/deployment-models) |
| Understand how secrets map to OpenAPI security schemes | [Secret sources](../security/secret-sources) |
| Set up approval for sensitive tool calls | [Policy and approval](../security/policy-and-approval) |

## Recommended reading order

- [Scope merging](./scope-merging) for multi-scope behavior
- [Modes and profiles](./modes-and-profiles) for curation logic
- [Config schema](./config-schema) for the field-by-field reference
