---
title: Security Overview
---

# Security Overview

Security in `oas-cli-go` has two distinct layers:

1. **runtime access**: who is allowed to call `oasclird`
2. **upstream API execution**: which credentials are applied when a tool calls the target API

The current implementation focuses heavily on the second layer and leaves the first layer to deployment controls.

## Runtime trust boundary

`oasclird` can run either:

- unauthenticated on loopback-only local deployments
- with runtime auth enabled through `runtime.server.auth`

The current server-side auth surface is profile-based rather than introspection-only. `oasclird` can validate runtime bearer tokens with `oauth2_introspection` or `oidc_jwks`, derive runtime scopes from the validated token, filter catalogs by those scopes, and reject out-of-scope execution requests.

The repository’s official reference proof for that runtime-auth boundary uses Authentik as an example broker, with Microsoft Entra ID documented as the upstream federation example. That reference proof does **not** make Authentik or Entra normative; the contract remains broker-neutral.

See [Authentik reference proof](../runtime/authentik-reference).

If you expose the runtime on a broader network, do not rely on bind address alone. Use runtime auth and deployment controls such as:

- firewall rules
- reverse proxy auth
- SSH tunneling
- container/network isolation

## Upstream auth model

Upstream auth comes from three pieces working together:

- OpenAPI security schemes normalized into tool auth requirements
- `secrets` entries in config keyed by security scheme name
- runtime secret and OAuth resolution at execution time

For MCP `streamable-http` and legacy `sse` sources there is a second auth layer:

- source-local `oauth` authenticates the MCP transport itself with `clientCredentials`
- `transport.headerSecrets` inject static header values resolved through top-level `secrets`

Transport OAuth tokens are cached per runtime instance. If the provider returns a refresh token, later MCP requests refresh the transport token before expiry, reuse the refreshed token across concurrent clients that share the same instance state, and fail closed with a reauthorization-required error after the single refresh budget is exhausted or refresh fails.

See [Auth resolution](./auth-resolution) and [Secret sources](./secret-sources).

## Policy model

Execution policy is evaluated inside the runtime, not the CLI. That means:

- hiding a command in the CLI is not your only line of defense
- approval and curated restrictions are checked again during execution
- denied requests can still be audited

See [Policy and approval](./policy-and-approval).

## Current implementation caveats to know early

- runtime execution supports static auth, `oauth2`, and `openIdConnect`
- supported OAuth runtime modes are `clientCredentials` and `authorizationCode`
- OpenAPI `implicit` and `password` flows are intentionally rejected at runtime
- missing secrets usually cause auth to be skipped rather than raising an immediate config error
- `exec` secrets are disabled unless explicitly allowed by policy
- only managed-scope deny rules are enforced directly as hard denies today

Those are not hypothetical edge cases; they follow the code as currently implemented.
