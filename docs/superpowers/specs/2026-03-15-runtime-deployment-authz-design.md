# Runtime Deployment and Remote Authorization Design

## Problem statement

`oascli` and `oasclird` currently support embedded execution, local daemon execution, and remote-style execution patterns, but the effective runtime behavior is still determined by a mix of flags, environment variables, daemon discovery, and operational convention. That creates ambiguity for agent-first usage, especially when local MCP servers need to stay warm across many tool calls and when future remote execution must enforce least-privilege access.

We need a runtime model that:

- makes execution mode a first-class part of `.cli.json`
- keeps zero-config behavior for the common case
- automatically promotes to a managed local daemon when local MCP servers are present
- supports remote `oasclird` instances as real policy-enforcing execution boundaries
- uses ephemeral OAuth2 session tokens to restrict what a given agent can discover and execute
- cleans up local managed runtimes when the owning terminal or session exits

## Goals

- Preserve simple, low-friction local usage for agents.
- Ensure local MCP-backed services stay warm and responsive across repeated tool calls.
- Avoid orphaned `oasclird` and MCP child processes.
- Make remote execution a first-class deployment mode in `.cli.json`.
- Enforce fine-grained remote authorization before both catalog discovery and execution.
- Support future microVM-based agent isolation with short-lived access tokens.

## Non-goals

- Implementing the full remote control plane in this spec.
- Designing a multi-cluster runtime scheduler.
- Defining every OAuth2 grant type supported by upstream providers.
- Replacing existing source, service, or policy models outside the runtime/authz additions described here.

## Selected approach

This design chooses an embedded-first configuration model with explicit runtime overrides:

- default to embedded execution when `.cli.json` does not require a persistent runtime
- auto-promote to a managed local `oasclird` when local MCP servers are present
- allow explicit `runtime.mode=local` or `runtime.mode=remote`
- use ephemeral OAuth2 session tokens for remote runtime access
- compute visible tools as the intersection of granted scopes, configured services/tools, and server policy/profile rules

This approach keeps the default experience simple while still giving operators a clear path to local warm runtimes and remote least-privilege execution.

## Design

### 1. Runtime resolution model

Add a top-level `runtime` block to `.cli.json` so runtime deployment becomes part of normal scoped configuration instead of being hidden in flags and environment variables.

Supported modes:

- `auto`
- `embedded`
- `local`
- `remote`

Resolution semantics:

- If `runtime` is absent, the effective behavior is equivalent to `auto`.
- `auto` keeps embedded execution for simple cases that do not require a persistent runtime.
- `auto` must promote to managed local-daemon mode when the effective configuration contains local MCP server definitions.
- `embedded` always executes in-process.
- `local` always uses a managed local `oasclird`.
- `remote` always delegates discovery and execution to a configured remote `oasclird`.

CLI flags may still exist as explicit escape hatches, but `.cli.json` becomes the primary source of truth for runtime behavior.

### 2. Local managed runtime behavior

When the effective runtime mode is local, `oascli` must ensure a compatible `oasclird` instance is running and attach to it automatically.

Local managed runtime requirements:

- local MCP servers remain warm between tool calls
- `oasclird` supervises the MCP processes and transports it launched
- ownership is session-scoped, not merely machine-scoped
- the runtime records an owner identity plus a lease or heartbeat
- if the owning terminal/session exits, the managed runtime shuts down and cleans up its MCP children
- accidental sharing is not allowed
- shared local runtimes must be explicitly requested by configuration or instance identity

This preserves low latency for agent tool execution while preventing orphaned daemons from accumulating after sessions die.

### 3. Remote runtime authorization model

Remote `oasclird` is treated as the execution boundary and policy enforcement point.

Every remote agent session should authenticate with an ephemeral OAuth2 access token minted specifically for that agent/session, such as a microVM lifetime token. That token is the primary identity for remote runtime access.

The remote daemon must authorize both:

- catalog discovery
- tool execution

before returning data to the client.

Visible tools are computed as the intersection of:

- OAuth2-granted scopes from the agent/session token
- configured services and allowed tool mappings in `.cli.json`
- server-side policy and agent-profile rules

The authorization model must support all of the following scope dimensions:

- service bundle scopes
- individual tool scopes
- policy/profile scopes

Example mental model:

- `bundle:payments`
- `tool:users.get`
- `profile:read-only`

A remote token may include one or more of these, but the daemon must still intersect them with server policy and effective config before exposing tools.

### 4. Ephemeral remote session state

Per-agent remote session state must be ephemeral.

Requirements:

- session tokens are short-lived
- any derived downstream credentials are isolated to that remote session
- per-session auth caches are not shared across agents
- cached remote auth state is wiped when the session/VM ends
- authorization failures should be surfaced as precise authz/authn errors

Where practical, tools outside the authorized envelope should be absent from catalog responses rather than merely failing later at execution time.

### 5. Configuration shape

The exact schema can evolve during implementation, but the intended configuration direction is:

```json
{
  "runtime": {
    "mode": "auto",
    "local": {
      "managed": true,
      "sessionScope": "terminal",
      "shutdown": "when-owner-exits",
      "share": "explicit"
    },
    "remote": {
      "url": "https://runtime.example.com",
      "oauth": {
        "audience": "oasclird",
        "scopes": [
          "bundle:payments",
          "tool:users.get",
          "profile:read-only"
        ]
      }
    }
  }
}
```

Design constraints for this block:

- mergeable across managed, user, project, and local scopes
- explicit enough for operators to set org-level defaults
- narrowable by project or local overrides
- compatible with current instance derivation and state isolation logic

### 6. Catalog and execution semantics

The catalog shown to an agent must reflect what it can actually execute in the selected runtime mode.

Expected behavior:

- embedded mode shows tools the embedded runtime can execute
- local mode shows tools available from the attached managed local daemon
- remote mode shows only tools the remote daemon authorizes for that token/session

If a token becomes invalid or lacks sufficient scope:

- affected tools should disappear from the visible catalog when possible, or
- execution should fail with a precise authorization error rather than a generic transport failure

### 7. Audit and observability

Audit events should capture enough context to distinguish how execution occurred and under what authority.

At minimum, the design should extend auditing to record:

- runtime mode
- effective instance identity
- remote principal or session identity when applicable
- whether execution occurred under embedded, local-daemon, or remote-daemon control

This makes future compliance and operator debugging materially easier.

## Operational defaults

The intended defaults are:

- no `runtime` block: behave as `auto`
- `auto` + no local MCP: stay embedded
- `auto` + local MCP present: use managed local `oasclird`
- explicit `local`: auto-start or attach to a matching local daemon
- explicit `remote`: use the configured remote runtime and enforce remote authz
- local managed runtimes are owner-scoped and cleaned up when the owner exits
- remote session state is ephemeral and isolated per agent/session

## Testing expectations

Implementation should be considered complete only when product tests cover:

- embedded-only execution flows
- automatic promotion from `auto` to local-daemon mode for local MCP configs
- local daemon warm MCP reuse across repeated tool calls
- owner-session teardown and cleanup of managed local daemons
- explicit shared-vs-exclusive local runtime behavior
- remote catalog filtering by bundle, tool, and profile scopes
- remote execution denial outside granted scopes
- ephemeral remote auth cache wipe on session end
- audit output reflecting runtime mode and principal context

## Open implementation notes

- Current instance derivation and per-instance state isolation should be reused rather than replaced.
- Existing embedded and daemon paths should be unified under a single runtime resolution layer.
- Remote runtime authorization should be enforced before discovery as well as before execution.
- The runtime block must remain compatible with current configuration scope merging.

## Outcome

This design keeps `oascli` easy for agents to use by default, makes local MCP-backed workflows fast and persistent when needed, and establishes a path toward remotely hosted, least-privilege `oasclird` deployments for microVM-based agent execution.
