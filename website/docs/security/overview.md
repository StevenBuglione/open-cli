---
title: Security Overview
---

# Security Overview

Security in `oas-cli-go` has two distinct layers:

1. **runtime access**: who is allowed to call `oasclird`
2. **upstream API execution**: which credentials are applied when a tool calls the target API

The current implementation focuses heavily on the second layer and leaves the first layer to deployment controls.

## Runtime trust boundary

`oasclird` does **not** authenticate its own HTTP callers. By default it binds to localhost, which is the main built-in safety mechanism.

If you expose the runtime on a broader network, add your own controls such as:

- firewall rules
- reverse proxy auth
- SSH tunneling
- container/network isolation

## Upstream auth model

Upstream auth comes from three pieces working together:

- OpenAPI security schemes normalized into tool auth requirements
- `secrets` entries in config keyed by security scheme name
- runtime secret and OAuth resolution at execution time

For MCP `streamable-http` sources there is a second auth layer:

- source-local `oauth` authenticates the MCP transport itself with `clientCredentials`
- `transport.headerSecrets` inject static header values resolved through top-level `secrets`

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
