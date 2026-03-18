# Local Managed Runtime Lifecycle Design

## Problem statement

`ocli` currently supports embedded execution and local daemon-style execution, but the lifecycle rules for when a managed local `oclird` should start, attach, stay warm, share, and shut down are not yet defined as a first-class part of `.cli.json`.

This is especially important for local MCP servers. If they are launched from `npx`, Docker, or other local transports, agents need a warm managed runtime so repeated tool calls do not pay repeated startup latency. At the same time, that managed runtime must not leave orphaned daemons or child MCP processes behind after the owning session exits.

## Goals

- Make local runtime behavior configurable in `.cli.json`.
- Auto-promote to a managed local daemon when local MCP sources are present.
- Keep local MCP transports warm across repeated tool calls.
- Define clean ownership, sharing, attach, and shutdown rules.
- Prevent orphaned `oclird` and MCP child processes.
- Reuse current instance/state isolation where possible.

## Non-goals

- Remote runtime authz and OAuth policy.
- Multi-cluster scheduling or hosted control planes.
- Non-local transport policy design beyond what affects local lifecycle.

## Selected approach

The selected model is:

- `.cli.json` owns local runtime mode selection
- `runtime.mode=auto` promotes to managed local runtime when local MCP sources are present
- managed local runtimes are session-scoped by default
- shared local runtimes are opt-in and keyed by `shareKey`
- cleanup is deterministic via heartbeats, leases, and explicit shutdown semantics

## Runtime resolution

Supported modes relevant to this spec:

- `auto`
- `embedded`
- `local`

Definitions:

- a **local MCP source** is an MCP source whose transport is executed or hosted from the local machine, including `stdio`-launched servers and locally managed Docker-backed MCP endpoints
- when the effective merged configuration contains at least one local MCP source, `runtime.mode=auto` promotes to managed local-daemon mode

Selection precedence:

1. explicit CLI runtime flags
2. effective merged `.cli.json` `runtime` block
3. legacy environment-variable runtime hints
4. default `auto`

## Configuration model

Representative shape:

```json
{
  "runtime": {
    "mode": "auto",
    "local": {
      "sessionScope": "terminal",
      "heartbeatSeconds": 15,
      "missedHeartbeatLimit": 3,
      "shutdown": "when-owner-exits",
      "share": "exclusive"
    }
  }
}
```

### Supported fields

- `runtime.mode`: `auto | embedded | local`
- `runtime.local.sessionScope`: `terminal | agent | shared-group`
- `runtime.local.heartbeatSeconds`: positive integer
- `runtime.local.missedHeartbeatLimit`: positive integer
- `runtime.local.shutdown`: `when-owner-exits | manual`
- `runtime.local.share`: `exclusive | group`
- `runtime.local.shareKey`: required only when `share=group`

### Valid combinations

- `sessionScope=terminal` -> `share=exclusive`, `shareKey` forbidden
- `sessionScope=agent` -> `share=exclusive`, `shareKey` forbidden
- `sessionScope=shared-group` -> `share=group`, `shareKey` required

### Merge rules

- runtime objects deep-merge across managed, user, project, and local scopes
- scalar fields are overridden by the narrowest scope that defines them
- arrays are replaced by the narrowest scope that defines them unless a future field explicitly declares additive behavior
- a narrower `runtime.mode` override does not discard sibling `runtime.local` fields inherited from wider scopes unless those fields are also overridden
- validation runs on the final merged runtime block

## Ownership and identity

### Session scopes

- `terminal`: one runtime per terminal session
- `agent`: one runtime per agent process or agent VM session
- `shared-group`: one runtime identified by `shareKey`, with multiple member sessions allowed to attach

For `shared-group`, the `shareKey` defines the runtime identity rather than any single creator session.

### Runtime identity and fingerprinting

Local runtime identity is derived from:

- the effective runtime-relevant config fingerprint
- the session identity for `terminal` and `agent`
- the `shareKey` for `shared-group`

The config fingerprint is computed from canonical JSON with sorted object keys and stable array ordering. Inputs are limited to fields that affect attach compatibility:

- effective `runtime.mode`
- effective `runtime.local` block
- local MCP source definitions, including transport type and launch metadata
- service-to-source mappings that expose those local MCP sources
- policy fields that affect local launch, attach, or execution gating

Excluded from the fingerprint:

- resolved secret values
- cached tokens
- transient PID/port/runtime registration data
- audit file locations

Secret references are included by stable reference name.

## Start, attach, and sharing rules

### Default behavior

- a session starts or attaches to its compatible managed local runtime automatically
- local MCP transports remain warm between calls
- `share=exclusive` is the default

### Attach rules

- attach succeeds only when the daemon reports the same config fingerprint the client computed
- in `exclusive` mode, a client may only attach to the runtime created for that session
- if another client attempts to attach to an exclusive runtime, return `runtime_attach_conflict`
- in `shared-group` mode, a client may attach only when config fingerprint, session scope, share mode, and `shareKey` all match
- attach fails closed when metadata does not match expectations

## Heartbeat and shutdown behavior

### Heartbeats

- the client renews the heartbeat every `heartbeatSeconds`
- the daemon marks the lease stale after `missedHeartbeatLimit` missed heartbeats
- if no request is in flight when the lease becomes stale, shutdown begins immediately
- if a request is in flight, shutdown waits for request completion or a short grace timer

### Shutdown semantics

- `shutdown=when-owner-exits` means the runtime is cleaned up automatically
- for `terminal` and `agent`, cleanup begins when that session lease expires
- for `shared-group`, cleanup begins when the last attached group member lease expires
- `shutdown=manual` means the runtime remains running until an explicit stop command or administrator cleanup occurs
- `shutdown=manual` does not disable heartbeats; it changes post-lease cleanup behavior from automatic termination to detached retention

## Client and daemon contract

The local attach contract must include:

- attach/connect requests carrying session identity, requested runtime mode, config fingerprint, and share-key claims when applicable
- a runtime-info handshake with `contractVersion`, `capabilities`, instance identity, runtime mode, config fingerprint, session scope, share mode, share-key presence, and lease metadata
- explicit heartbeat renewal and session-close operations
- structured errors

### Compatibility rules

- major contract version must match
- minor-version differences are allowed only when all client-required capabilities are advertised by the daemon
- otherwise fail with `contract_mismatch`

### Error taxonomy

- `runtime_start_failed`
- `runtime_attach_mismatch`
- `runtime_attach_conflict`
- `runtime_unreachable`
- `contract_mismatch`

## Failure and recovery

- if local runtime startup fails, return `runtime_start_failed`
- if attach metadata mismatches, return `runtime_attach_mismatch`
- if a managed daemon crashes mid-session, the next command may perform one restart attempt for the same owner session: clear stale registration, terminate any stale child-process tree still owned by the daemon, start a fresh daemon, re-run handshake once, then fail if any step fails
- daemon crash and lease-expiry shutdown are recorded as distinct audit outcomes

## Testing expectations

Implementation is complete when product tests cover:

- `auto` promotion to local-daemon mode when local MCP sources are present
- exclusive attach rules
- shared-group attach rules with matching and mismatching `shareKey`
- session-scope-specific identity behavior
- heartbeat expiry cleanup
- in-flight request grace behavior
- manual shutdown retention behavior
- single-restart path after daemon crash
- cleanup of managed MCP child processes
