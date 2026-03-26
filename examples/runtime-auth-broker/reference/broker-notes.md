# Broker notes

This reference broker is a contract example, not a required product choice.

## Upstream federation

The broker may accept identity from:

- Microsoft Entra ID
- Google
- GitHub

Those upstream identities are normalized before runtime token issuance. `ocli` does not need provider-specific logic for each one.

## Runtime token requirements

The broker-issued token must be acceptable to `open-cli-toolbox` under `validationProfile: "oidc_jwks"` and should include:

```json
{
  "iss": "https://broker.example.com",
  "aud": "open-cli-toolbox",
  "sub": "github:user-123",
  "scope": "bundle:tickets tool:tickets:listTickets",
  "exp": 1773599999
}
```

Machine actors may use `client_id` in addition to or instead of `sub`, but at least one principal identity must be present.

Delegated child tokens for sub-agents follow the same runtime-compatible shape. The only difference is that the broker mints them from a validated parent runtime token and constrains them to a narrower delegation envelope.

## Delegated child-token contract

Sub-agent delegation should stay inside the existing brokered runtime model:

1. a parent actor already has a valid runtime token for audience `open-cli-toolbox`
2. a delegating client, agent supervisor, or broker-adjacent orchestration layer requests a child token from the broker before starting a sub-agent
3. the broker validates the parent token and mints a new runtime-compatible child token
4. `open-cli-toolbox` validates the child token exactly like any other runtime token and enforces only the child scope set

The recommended request shape is an OAuth token-exchange style `POST /token` call. A dedicated broker-internal endpoint is acceptable if needed, but the external contract should stay equivalent to:

```http
POST /token
Content-Type: application/x-www-form-urlencoded

grant_type=urn:ietf:params:oauth:grant-type:token-exchange
&subject_token=<parent-runtime-token>
&subject_token_type=urn:ietf:params:oauth:token-type:access_token
&requested_token_type=urn:ietf:params:oauth:token-type:access_token
&audience=open-cli-toolbox
&scope=bundle:tickets tool:tickets:listTickets
&actor_id=subagent:triage-01
```

The broker may accept equivalent JSON or internal metadata in front of this exchange, but it should preserve the same semantics:

- the caller is asking for a child runtime token, not broader access
- the parent runtime token is the authority for the maximum delegation envelope
- the requested audience remains `open-cli-toolbox`
- the requested scopes are explicit and are checked before issuance

## Delegation subset rules

Delegation must be fail-closed.

Before issuing a child token, the broker should:

1. validate the parent token signature, issuer, audience, and expiry
2. parse the parent `scope` claim into the normalized runtime scope vocabulary already used by the runtime contract
3. derive the parent effective runtime scope envelope from that normalized set
4. verify that every requested child scope is contained within the parent effective envelope
5. reject the exchange if any requested scope is missing, malformed, or would widen access

In practice that means:

- a child token may keep the same scopes or reduce them
- a child token must never add a `bundle:*`, `profile:*`, or `tool:*` scope that the parent token does not already effectively hold
- explicit requested `tool:*` scopes must still fit inside the parent's effective runtime access
- local config, curated mode, and agent-profile restrictions may narrow the visible or executable set further, but they must never expand access beyond the child token scopes

If the parent token is invalid, expired, missing required identity claims, carries the wrong audience, or the subset check is inconclusive, the broker must refuse issuance instead of guessing.

## Recommended child claims

The child token presented to `open-cli-toolbox` should still look like a normal runtime token:

```json
{
  "iss": "https://broker.example.com",
  "aud": "open-cli-toolbox",
  "sub": "github:user-123",
  "client_id": "ocli-browser",
  "scope": "tool:tickets:listTickets",
  "exp": 1773596400,
  "act": {
    "sub": "github:user-123",
    "client_id": "ocli-browser"
  },
  "delegated_by": "github:user-123",
  "delegation_id": "delegation-01HXYZ..."
}
```

Lineage claims are recommended for audit and traceability:

- `act` to describe the acting parent principal when your broker supports it
- `delegated_by` as a simple string fallback when `act` is not practical
- `delegation_id` or `jti` for correlation, revocation logs, and audit joins

Those lineage claims do not grant permissions on their own. Authorization still derives from the child token `scope` claim as enforced by `open-cli-toolbox`.

## Security constraints

- child tokens should be short-lived; use minutes, not hours, and keep the TTL materially shorter than the parent token lifetime
- delegation must be explicit per child token request; do not mint ambient reusable child tokens without a requested scope set
- child tokens must keep `aud` fixed to `open-cli-toolbox`
- the broker should copy or narrow the parent principal identity, not replace it with an unrelated subject
- failures must stay explicit and fail-closed: no fallback to the parent token, no automatic scope widening, no best-effort partial issuance unless the narrower scope set is the one explicitly returned to the caller
- if lineage claims are omitted, issuance may still proceed, but the broker should treat audit lineage as a recommended control and not as authorization data

## Authorization behavior

- `bundle:*` and `profile:*` scopes expand the candidate tool set
- explicit `tool:*` scopes intersect that set
- deny rules still win
- catalog filtering and execution must share the same authorization envelope
- delegated child tokens are enforced the same way, but against the child token's narrower scope set

## Verification reference

The executable reference fixture used for verification lives in:

- `product-tests/tests/helpers/runtime_auth_broker.go`
- `product-tests/tests/capability_runtime_auth_broker_test.go`
