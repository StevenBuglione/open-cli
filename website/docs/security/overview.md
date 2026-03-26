---
title: Security Overview
---

# Security Overview

**Read this if** you are responsible for the auth model, secret management, or understanding when and how `open-cli-toolbox` becomes an authorization boundary. This page answers: what are the two distinct security layers, when is runtime auth enforced, and what are the real current caveats around OAuth and missing secrets.

Security in `open-cli` has two distinct layers:

1. **runtime access**: who is allowed to call `open-cli-toolbox`
2. **upstream API execution**: which credentials are applied when a tool calls the target API

## Runtime trust boundary

`open-cli-toolbox` can run on loopback for local evaluation or behind shared network infrastructure for team and enterprise use.

The current server-side auth surface is profile-based rather than introspection-only. `open-cli-toolbox` can validate runtime bearer tokens with `oauth2_introspection` or `oidc_jwks`, derive runtime scopes from the validated token, filter catalogs by those scopes, and reject out-of-scope execution requests.

The repository’s official reference proof for that runtime-auth boundary uses Authentik as an example broker, with Microsoft Entra ID documented as the upstream federation example. That reference proof does **not** make Authentik or Entra normative; the contract remains broker-neutral.

See [Authentik reference proof](../runtime/authentik-reference).

If you expose the runtime on a broader network, do not rely on bind address alone. Use runtime auth and deployment controls such as:

- firewall rules
- reverse proxy auth
- SSH tunneling
- container/network isolation

See also [Deployment models](../runtime/deployment-models) for the hosted-runtime deployment story and [Runtime overview](../runtime/overview) for the runtime handshake and browser-config surfaces.

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

**Token revocation is a tracked gap.** The runtime validates tokens at presentation time (expiry, signature, issuer, audience, scope). It does not perform revocation checks or call an introspection endpoint. If you need post-issuance revocation, you must enforce it outside the runtime (short expiry windows, network controls, or a proxy that calls your IdP's introspection endpoint).

## If you are trying to…

| Goal | Go to |
| --- | --- |
| Understand how OpenAPI security schemes become runtime credentials | [Auth resolution](./auth-resolution) |
| Configure `env`, `file`, or `exec` secret sources | [Secret sources](./secret-sources) |
| Set up approval requirements for sensitive tools | [Policy and approval](./policy-and-approval) |
| Set up the runtime as a full authorization boundary end to end | [Enterprise readiness](../runtime/enterprise-readiness) |
| See the broker-neutral worked example with Authentik | [Authentik reference proof](../runtime/authentik-reference) |
| Configure MCP transport OAuth and header secrets | [Secret sources – MCP transport](./secret-sources#mcp-headersecrets) |
