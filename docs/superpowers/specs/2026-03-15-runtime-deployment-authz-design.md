# Runtime Deployment and Remote Authorization Design

## Problem statement

`ocli` and `oclird` currently support embedded execution, local daemon execution, and remote-style execution patterns, but the effective runtime behavior is still determined by a mix of flags, environment variables, daemon discovery, and operational convention. That creates ambiguity for agent-first usage, especially when local MCP servers need to stay warm across many tool calls and when future remote execution must enforce least-privilege access.

We need a runtime model that:

- makes execution mode a first-class part of `.cli.json`
- keeps zero-config behavior for the common case
- automatically promotes to a managed local daemon when local MCP servers are present
- supports remote `oclird` instances as real policy-enforcing execution boundaries
- uses ephemeral OAuth2 session tokens to restrict what a given agent can discover and execute
- cleans up local managed runtimes when the owning terminal or session exits

## Goals

- Preserve simple, low-friction local usage for agents.
- Ensure local MCP-backed services stay warm and responsive across repeated tool calls.
- Avoid orphaned `oclird` and MCP child processes.
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
- auto-promote to a managed local `oclird` when local MCP servers are present
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
- `local` always uses a managed local `oclird`.
- `remote` always delegates discovery and execution to a configured remote `oclird`.

Runtime selection must follow a deterministic precedence order:

1. explicit CLI runtime flags
2. effective merged `.cli.json` `runtime` block
3. legacy environment-variable runtime hints
4. default `auto`

Flags remain explicit escape hatches for debugging and emergency override, but normal operation should flow from `.cli.json`. Environment variables remain a compatibility layer rather than the preferred control plane.

Operational definitions:

- a "local MCP server" means an MCP source whose transport is executed or hosted from the local machine, including `stdio`-launched servers and locally managed Docker-backed MCP endpoints
- `auto` promotion is triggered when the effective merged configuration includes at least one such local MCP source
- "session identity" means a generated client session ID bound to the current terminal, agent process, or explicitly declared shared-runtime key

### 2. Local managed runtime behavior

When the effective runtime mode is local, `ocli` must ensure a compatible `oclird` instance is running and attach to it automatically.

Local managed runtime requirements:

- local MCP servers remain warm between tool calls
- `oclird` supervises the MCP processes and transports it launched
- ownership is session-scoped, not merely machine-scoped
- the runtime records an owner identity, session ID, and lease or heartbeat
- if the owning terminal/session exits or heartbeats expire, the managed runtime shuts down and cleans up its MCP children
- accidental sharing is not allowed
- shared local runtimes must be explicitly requested by configuration or instance identity

Default local ownership model:

- every terminal or agent session gets its own managed local runtime by default
- the runtime identity is derived from the effective config fingerprint plus session identity
- a heartbeat is renewed by the client while the session is alive
- after a small missed-heartbeat threshold, the daemon transitions to shutdown

Runtime identity and fingerprinting:

- local runtime identity is derived from the effective runtime-relevant config fingerprint plus session identity, and plus `shareKey` when group sharing is enabled
- the config fingerprint is computed from canonical JSON with sorted object keys and stable array ordering
- fingerprint inputs are limited to fields that affect attach compatibility: `runtime.mode`, the effective `runtime.local` block, local MCP source definitions including transport type and launch metadata, service-to-source mappings that expose those local MCP sources, and policy fields that affect local launch, attach, or execution gating
- secret values are excluded from the fingerprint; secret references are included by stable reference name
- attach succeeds only when the daemon reports the same fingerprint the client computed

Concrete fingerprint example:

- included: MCP stdio command, args, working directory, env secret-ref names, Docker image/compose target names, service bindings, `share`, `shareKey`, heartbeat policy
- excluded: resolved secret values, cached tokens, audit file locations, transient PID/port data

Heartbeat and lease behavior:

- the client renews the heartbeat every `heartbeatSeconds`
- the daemon marks the lease stale after `missedHeartbeatLimit` missed heartbeats
- if no request is in flight when the lease becomes stale, shutdown begins immediately
- if a request is in flight, shutdown waits for the request to complete or for a short grace timer to expire
- daemon crash and lease expiry are recorded as distinct audit outcomes

Attach and sharing rules:

- `share=exclusive` is the default
- in `exclusive` mode, a client may only attach to the runtime it created for that session
- a second terminal with the same config gets a distinct managed runtime rather than silently attaching
- `share=group` allows attach only when an explicit share key matches
- attach must fail closed when runtime metadata does not match the caller's expected config fingerprint, share mode, or owner rules

Sharing interface:

- `runtime.local.share` is an enum with values `exclusive` or `group`
- when `share=group`, `runtime.local.shareKey` is required
- the share key participates in runtime identity derivation and attach eligibility
- clients may attach only when config fingerprint, share mode, and share key all match

This makes cleanup deterministic and prevents a second terminal from accidentally keeping a stale daemon alive.

This preserves low latency for agent tool execution while preventing orphaned daemons from accumulating after sessions die.

Supported local session scopes for phase 1:

- `terminal`: a runtime owned by one terminal session
- `agent`: a runtime owned by one agent process or agent VM session
- `shared-group`: a runtime attachable by clients that present the same `shareKey`

Valid local scope combinations:

- `sessionScope=terminal` -> `share=exclusive`, `shareKey` forbidden
- `sessionScope=agent` -> `share=exclusive`, `shareKey` forbidden
- `sessionScope=shared-group` -> `share=group`, `shareKey` required

For `shared-group`, the `shareKey` defines the shared runtime identity and member sessions attach to that shared runtime.

Manual shutdown semantics:

- `shutdown=when-owner-exits` means the daemon terminates on lease expiry for the owning session or group owner
- for `sessionScope=shared-group`, `shutdown=when-owner-exits` means shutdown begins when the last attached group member lease expires
- `shutdown=manual` means the daemon remains running until an explicit stop command or administrator cleanup occurs
- `shutdown=manual` does not disable heartbeats; it changes cleanup policy after lease expiry from automatic terminate to detached/manual retention

### 3. Remote runtime authorization model

Remote `oclird` is treated as the execution boundary and policy enforcement point.

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

Scope evaluation rules:

- the remote daemon is the authority that computes the final authorization envelope
- scope filtering occurs server-side during catalog materialization and is re-checked again at execution time
- scope strings are typed by prefix such as `bundle:`, `tool:`, and `profile:`
- bundle and profile scopes are expanded server-side into concrete tool grants according to policy
- clients must not pre-expand or self-authorize based on scope names alone
- if a scope grants access to a bundle but server policy removes a tool from that bundle, the tool remains hidden and unexecutable

Authorization resolution order:

1. start from tools present in the effective instance configuration
2. expand profile scopes into candidate tool sets
3. expand bundle scopes into candidate tool sets
4. intersect those candidate sets with explicit `tool:` scopes when such scopes are present
5. apply server-side deny rules last

Resolution rules:

- deny rules always win
- scopes can only expose tools that already exist in effective config
- catalog filtering and execution authorization must use the same resolved authorization envelope
- bundle and profile expansions union together before explicit `tool:` filtering is applied
- if only explicit `tool:` scopes are present, the authorization envelope is the matching configured tool set after deny rules are applied

#### Remote token lifecycle

Remote runtime access tokens are session-scoped credentials.

Acquisition and lifecycle rules:

- the client presents a pre-issued ephemeral bearer token for the agent/session, or obtains one from a configured OAuth2 or token-exchange source before first remote use
- the token must carry the audience and scopes required for the target remote runtime
- refresh credentials, when supported, are stored only in the same ephemeral session scope as the access token and are never promoted to longer-lived workspace or machine storage
- refresh is allowed only for the lifetime of the owning session
- on `authn_failed` caused by expiry, the client may attempt one refresh when refresh configuration exists
- if expiry is detected during preflight, the client refreshes before sending the command; if expiry is discovered mid-command, the in-flight command fails with `authn_failed` and the next command may attempt the single refresh path
- if refresh is unavailable, revoked, or fails, the command fails closed and requires a new session token
- revocation or expiry invalidates any cached remote authorization envelope for that session
- remote daemons do not mint broader replacement tokens on behalf of the client

### 4. Ephemeral remote session state

Per-agent remote session state must be ephemeral.

Requirements:

- session tokens are short-lived
- any derived downstream credentials are isolated to that remote session
- per-session auth caches are not shared across agents
- cached remote auth state is wiped when the session/VM ends
- authorization failures should be surfaced as precise authz/authn errors
- an empty authorized tool set returns an empty catalog successfully rather than an internal error
- remote session records are keyed by remote session ID and destroyed on explicit session close, lease expiry, or token revocation

Where practical, tools outside the authorized envelope should be absent from catalog responses rather than merely failing later at execution time.

### 5. Configuration shape

The exact schema can evolve during implementation, but the interface should be stable enough for planning. The intended configuration direction is:

```json
{
  "runtime": {
    "mode": "auto",
    "local": {
      "sessionScope": "terminal",
      "heartbeatSeconds": 15,
      "missedHeartbeatLimit": 3,
      "shutdown": "when-owner-exits",
      "share": "exclusive",
      "shareKey": "team-a"
    },
    "remote": {
      "url": "https://runtime.example.com",
      "oauth": {
        "audience": "oclird",
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

Planning-level field expectations:

- `runtime.mode` is required only when overriding `auto`
- `runtime.local` is optional and only applies to `auto` or `local`
- `runtime.remote.url` is required when `mode=remote`
- `runtime.remote.oauth.scopes` is optional but strongly expected for least-privilege remote deployments
- `runtime.local.share` must validate against a closed enum such as `exclusive` or `group`
- `runtime.local.shareKey` is forbidden unless `share=group`
- `runtime.local.shutdown` must validate against a closed enum such as `when-owner-exits` or `manual`
- `runtime.local.sessionScope` must validate against the closed enum `terminal`, `agent`, or `shared-group`

Merge rules:

- runtime objects deep-merge across managed, user, project, and local scopes
- scalar fields are overridden by the narrowest scope that defines them
- arrays are replaced by the narrowest scope that defines them unless a future field explicitly declares additive behavior
- `runtime.mode` from a narrower scope overrides wider-scope defaults without discarding deep-merged sibling `local` or `remote` objects unless the narrower scope overrides those fields too
- validation runs on the final merged runtime block

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

Audit destinations:

- embedded and local-daemon modes continue to emit per-instance local audit records
- remote mode emits server-side audit records keyed by remote session principal and instance identity
- client-side connection failures should still produce local diagnostic events even when execution never reaches the remote daemon
- audit events must distinguish start, attach, attach-conflict, crash, lease-expiry shutdown, token refresh, authn failure, and authz denial

### 8. Client and daemon contract

The `ocli` and `oclird` boundary needs a minimal, explicit contract so runtime resolution, attach rules, and authorization are plannable as independent units.

The contract must include:

- a runtime-info handshake that exposes contract version, instance identity, runtime mode, config fingerprint, share mode, share key presence, and owner/session metadata
- attach and connect requests that carry session identity, requested runtime mode, config fingerprint, and share-key claims when applicable
- catalog responses that carry the effective authorization envelope version used to filter results
- execute responses and failures that use structured error categories such as `runtime_start_failed`, `runtime_attach_mismatch`, `runtime_unreachable`, `authn_failed`, `authz_denied`, and `contract_mismatch`
- version negotiation rules where unsupported required capabilities fail closed instead of silently degrading behavior
- heartbeat renewal, lease-state reporting, and explicit session-close operations for managed local and remote sessions

The client must validate handshake metadata before attaching to a local managed runtime or trusting a remote daemon for catalog and execution.

Compatibility rules:

- the handshake includes `contractVersion` and `capabilities`
- attach or connect is allowed only when the major contract version matches
- minor-version differences are allowed only when every client-required capability is advertised by the daemon
- otherwise the client fails with `contract_mismatch`

Error taxonomy meanings:

- `runtime_start_failed`: daemon could not be launched or initialized
- `runtime_attach_mismatch`: runtime metadata did not match fingerprint, share mode, or contract requirements
- `runtime_attach_conflict`: caller attempted to attach to an exclusive runtime owned by another session
- `runtime_unreachable`: expected runtime endpoint could not be contacted
- `authn_failed`: authentication missing, expired, revoked, or refresh failed
- `authz_denied`: caller authenticated but lacks authorization for the requested tool or catalog entry
- `contract_mismatch`: runtime and client protocol versions or capabilities are incompatible

### 9. Failure and recovery behavior

Failure behavior must be explicit because agents need deterministic outcomes.

Local runtime failures:

- if `local` mode or `auto` promotion requires a daemon and startup fails, the command fails with a runtime-start error
- if attach metadata does not match the expected config fingerprint or share rules, the client must fail closed rather than attach to the wrong daemon
- if a second client attempts to attach to an `exclusive` runtime, return `runtime_attach_conflict`
- if a managed daemon crashes mid-session, the next client command may perform one restart attempt for the same owner session: clear stale runtime registration, terminate any stale child-process tree it owns if still present, start a fresh daemon, re-run handshake once, then surface the error if any step fails

Remote runtime failures:

- if the remote daemon is unreachable, return a runtime-unreachable error rather than silently falling back to local execution
- if the remote token is expired or revoked, return an authn error and require token refresh or re-issuance
- if the remote token is valid but lacks sufficient scope, return an authz error and do not leak unauthorized tool details beyond what policy allows
- remote mode must never silently downgrade to embedded or local execution

Version and contract mismatches:

- if the client and daemon disagree on runtime contract version or required capabilities, fail with a clear compatibility error
- catalog filtering and execution authorization must use the same effective authorization envelope to avoid discover/execute drift

## Authorization examples

- token has `bundle:payments`, config exposes payment tools, and policy allows them -> those payment tools appear
- token has `bundle:payments`, but policy denies `payments.refund` -> all allowed payment tools except `payments.refund` appear
- token has `profile:read-only` and `tool:users.get` -> only the intersection of the profile expansion and the explicit tool scope appears
- token has no matching scopes for a configured tool -> the tool is absent from catalog and direct execution returns `authz_denied`

## Operational defaults

The intended defaults are:

- no `runtime` block: behave as `auto`
- `auto` + no local MCP: stay embedded
- `auto` + local MCP present: use managed local `oclird`
- explicit `local`: auto-start or attach to a matching local daemon
- explicit `remote`: use the configured remote runtime and enforce remote authz
- local managed runtimes are owner-scoped and cleaned up when the owner exits
- remote session state is ephemeral and isolated per agent/session
- remote mode never falls back to local or embedded execution on failure

## Testing expectations

Implementation should be considered complete only when product tests cover:

- embedded-only execution flows
- automatic promotion from `auto` to local-daemon mode for local MCP configs
- local daemon warm MCP reuse across repeated tool calls
- owner-session teardown and cleanup of managed local daemons
- explicit shared-vs-exclusive local runtime behavior
- attach rejection on config fingerprint or share-rule mismatch
- attach rejection on share-key mismatch
- single-restart behavior after managed-daemon crash
- remote catalog filtering by bundle, tool, and profile scopes
- remote execution denial outside granted scopes
- ephemeral remote auth cache wipe on session end
- remote token expiry and revocation handling
- version-skew and contract-mismatch behavior
- handshake validation for local attach and remote connect paths
- audit output reflecting runtime mode and principal context

## Open implementation notes

- Current instance derivation and per-instance state isolation should be reused rather than replaced.
- Existing embedded and daemon paths should be unified under a single runtime resolution layer.
- Remote runtime authorization should be enforced before discovery as well as before execution.
- The runtime block must remain compatible with current configuration scope merging.

## Outcome

This design keeps `ocli` easy for agents to use by default, makes local MCP-backed workflows fast and persistent when needed, and establishes a path toward remotely hosted, least-privilege `oclird` deployments for microVM-based agent execution.
