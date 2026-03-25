---
title: Modes and Profiles
---

# Modes and Profiles

The runtime distinguishes between a broad **discover** view and curated, profile-specific views.

## `discover`

`discover` exposes every normalized tool in the catalog, subject to execution-time policy such as managed deny rules and approval checks.

Use this when you want:

- maximum visibility into the API surface
- exploratory development
- schema inspection before curation is finalized

## `curated`

`curated` narrows the visible tool set to the tool set selected by an agent profile.

A common pattern is:

```json
{
  "mode": { "default": "curated" },
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
  }
}
```

In the current implementation, `allow + deny: ["**"]` is the easiest way to express an allowlist.

## How the runtime chooses a view

The selection rules are slightly different for catalog viewing and policy checks, but they lead to the same practical guidance.

### Effective catalog selection

When building the `view` returned by `GET /v1/catalog/effective`:

1. if `agentProfile` is explicitly provided and exists, use that profile's effective view
2. otherwise, if no explicit mode was provided, start from `mode.default`
3. if the resulting mode is `curated` and `agents.defaultProfile` exists, use that profile's view
4. otherwise, use the `discover` view

### Policy selection

When deciding whether execution is allowed:

1. start with the explicit `mode`, or fall back to `mode.default`
2. if an explicit `agentProfile` exists and that profile has its own `mode`, the profile mode wins
3. the explicit profile's tool set is used when present
4. otherwise, if the resulting mode is `curated`, the default profile's tool set is used

## Important nuance: explicit profile beats explicit mode

If you supply `--agent-profile support`, the support profile effectively decides the view even if you also pass `--mode discover`.

That is current implementation behavior, not just documentation guidance.

## Pattern matching rules

Tool set patterns use Go's `path.Match` behavior plus one implementation-specific shortcut:

- `*` and `?` behave like `path.Match`
- `**` is treated as "match everything"
- if `deny` contains `**` and `allow` is non-empty, the `**` deny is skipped so the allowlist can still work

## Discoverability vs enforcement

Remember the separation:

- the selected view controls which dynamic commands `ocli` renders
- execution-time policy still runs inside the runtime

So a tool can be hidden from the CLI tree, inspected by ID, or rejected at execution time depending on the combination of view and policy.

## Profiles can restrict access, not mint it

In remote-runtime deployments with bearer auth enabled, the validated runtime token remains the outer authorization envelope.

That means:

- local config can hide tools or add deny rules
- curated mode can narrow the visible catalog
- an explicit or default agent profile can narrow the selected tool set

But none of those layers can grant access outside the scopes already present on the runtime token being enforced by `oclird`.

This matters for delegated child tokens too: a broker may mint a narrower child token for a sub-agent, and local config or profiles may narrow that child further, but the config/profile layer is never a scope-expansion mechanism.
