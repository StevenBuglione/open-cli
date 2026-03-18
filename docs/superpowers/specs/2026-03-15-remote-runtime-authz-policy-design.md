# Remote Runtime Authorization and Policy Design

## Problem statement

Remote `oclird` needs to act as a real execution boundary for agents. That means it cannot simply proxy a full tool catalog to every caller. It must authenticate each agent session, compute what that session may discover and execute, and enforce least-privilege access using short-lived credentials.

This is the foundation for future microVM-based agent execution where each agent receives an ephemeral token that authorizes only a bounded set of tools.

## Goals

- Make remote runtime mode a first-class part of `.cli.json`.
- Authenticate remote agent sessions with ephemeral OAuth2 credentials.
- Support fine-grained visibility and execution control using service bundles, tool scopes, and profile scopes.
- Enforce authorization at both catalog and execution time.
- Keep per-agent auth state isolated and ephemeral.
- Emit audit records rich enough for security and compliance review.

## Non-goals

- Local managed-runtime lifecycle.
- Hosted scheduler or control-plane architecture.
- Full OAuth provider matrix beyond what is needed for remote runtime authz.

## Selected approach

The selected model is:

- `runtime.mode=remote` routes discovery and execution through a remote `oclird`
- each agent/session uses an ephemeral OAuth2 bearer token
- the server computes the final authorization envelope
- bundle scopes and profile scopes union together before explicit `tool:` filtering and final deny rules
- unauthorized tools should be absent from the visible catalog whenever possible

## Configuration model

Representative shape:

```json
{
  "runtime": {
    "mode": "remote",
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

### Supported fields

- `runtime.mode`: `remote`
- `runtime.remote.url`: required
- `runtime.remote.oauth.audience`: expected audience for remote runtime tokens
- `runtime.remote.oauth.scopes`: requested or asserted runtime scopes

### Merge rules

- runtime objects deep-merge across managed, user, project, and local scopes
- scalar fields are overridden by the narrowest scope that defines them
- arrays are replaced by the narrowest scope that defines them unless a future field explicitly declares additive behavior
- a narrower `runtime.mode` override does not discard sibling `runtime.remote` fields inherited from wider scopes unless those fields are also overridden
- validation runs on the final merged runtime block

## Authentication model

Each remote agent session authenticates with an ephemeral bearer token minted for that session, such as a microVM-lifetime token.

### Token lifecycle

- the client presents a pre-issued ephemeral token, or obtains one from a configured OAuth2/token-exchange source before first remote use
- the token must carry the audience and scopes required for the target runtime
- refresh credentials, when supported, live only in the same ephemeral session scope as the access token
- refresh is allowed only for the lifetime of the owning session
- on `authn_failed` caused by expiry, the client may attempt one refresh when refresh configuration exists
- if expiry is detected mid-command, the in-flight command fails with `authn_failed`; the next command may take the single refresh path
- if refresh is unavailable, revoked, or fails, the command fails closed and requires a new session token
- remote daemons do not mint broader replacement tokens on behalf of the client

## Authorization model

The remote daemon is the authority that computes the final authorization envelope.

### Scope dimensions

- `bundle:*` scopes for service bundles
- `tool:*` scopes for individual tools
- `profile:*` scopes for policy/profile bundles

### Resolution algorithm

1. start from tools present in the effective instance configuration
2. expand profile scopes into candidate tool sets
3. expand bundle scopes into candidate tool sets
4. union profile and bundle candidates
5. if explicit `tool:` scopes are present, intersect the unioned candidate set with those explicit tools
6. apply server-side deny rules last

Rules:

- deny rules always win
- scopes can only expose tools that already exist in effective config
- catalog filtering and execution authorization must use the same resolved authorization envelope
- if only explicit `tool:` scopes are present, the authorization envelope is the matching configured tool set after deny rules

### Examples

- token has `bundle:payments`, config exposes payment tools, and policy allows them -> payment tools appear
- token has `bundle:payments`, but policy denies `payments.refund` -> all allowed payment tools except `payments.refund` appear
- token has `profile:read-only` and `tool:users.get` -> only the intersection of the unioned candidate set and explicit tool scopes appears
- token has no matching scopes for a configured tool -> the tool is absent from catalog and direct execution returns `authz_denied`

## Catalog and execution behavior

- remote mode returns only tools authorized for that token/session
- authorization is checked before catalog response and again before execution
- if the final authorized tool set is empty, catalog returns success with an empty list rather than an internal error
- remote mode never silently falls back to local or embedded execution

## Session state and audit

### Session state

- per-session auth caches are isolated by remote session ID
- cached auth state is destroyed on explicit session close, lease expiry, token revocation, or administrator cleanup
- derived downstream credentials are isolated to the same session boundary

### Audit

Remote audit records must distinguish:

- authenticated connect
- token refresh
- catalog filtering
- authn failure
- authz denial
- session close
- session expiry

## Client and daemon contract

The remote contract must include:

- connect requests carrying session identity, contract version, requested capabilities, and bearer-token-backed share/identity claims where applicable
- a handshake that returns `contractVersion`, `capabilities`, remote principal/session identity, and authorization-envelope metadata
- catalog responses carrying the authorization-envelope version used for filtering
- structured error responses
- explicit session-close operations

### Compatibility rules

- major contract version must match
- minor-version differences are allowed only when all client-required capabilities are advertised by the daemon
- otherwise fail with `contract_mismatch`

### Error taxonomy

- `runtime_unreachable`
- `authn_failed`
- `authz_denied`
- `contract_mismatch`

## Testing expectations

Implementation is complete when product tests cover:

- scoped remote catalog filtering by bundle, tool, and profile scopes
- deny rules overriding otherwise-allowing scopes
- empty authorized catalog behavior
- execution denial outside granted scope
- token expiry and single-refresh behavior
- token revocation behavior
- session-close and auth-cache wipe behavior
- audit output for authn, authz, refresh, and session lifecycle events
