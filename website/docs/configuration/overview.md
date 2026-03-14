---
title: Configuration Overview
---

# Configuration Overview

`oascli` and `oasclird` are driven by **JSON config files** named `.cli.json` (plus optional scope variants described in [Scope merging](./scope-merging)).

## What the config controls

A config file answers these questions:

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

Each source can also define refresh behavior. For MCP sources, `disabledTools` can hide specific discovered MCP tools before normalization.

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

## Recommended reading order

- [Scope merging](./scope-merging) for multi-scope behavior
- [Modes and profiles](./modes-and-profiles) for curation logic
- [Config schema](./config-schema) for the field-by-field reference
