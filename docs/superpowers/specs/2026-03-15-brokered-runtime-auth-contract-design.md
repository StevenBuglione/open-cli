# Brokered Remote Runtime Auth Contract Design

## Problem

The current remote-runtime design assumes that `oclird` can validate remote bearer tokens using an RFC7662-style introspection endpoint. That is too narrow for the real provider landscape:

- Microsoft Entra ID does not expose a standard RFC7662 introspection endpoint for normal access tokens.
- Google and GitHub also do not give us one uniform runtime-token validation model.
- Different organizations will want to use different brokers, reverse proxies, token validators, or custom `oclird` implementations.

We still need a standard contract so that:

- `ocli` knows how to acquire a runtime token and talk to a compatible runtime
- `oclird` implementations know what token semantics and metadata they must expose
- organizations are free to choose their own internal validation/broker implementation as long as they honor the external contract

## Goals

- Standardize the **client-visible remote auth contract** for `ocli`.
- Decouple that contract from any one daemon-side validation mechanism.
- Support brokered login flows that can federate Microsoft, Google, GitHub, or other upstream IdPs.
- Keep remote runtime least-privilege authorization based on runtime scopes such as `bundle:*`, `tool:*`, and `profile:*`.
- Provide one concrete reference example that proves the contract is usable in practice.

## Non-goals

- Mandating one broker product or one hosted control plane.
- Requiring every organization to run the same `oclird` implementation.
- Standardizing how an organization maps upstream identities into internal RBAC.
- Supporting direct use of every upstream provider's native token format inside `oclird`.

## Selected approach

Standardize the **runtime access token contract** and the **runtime auth metadata contract**.

The important distinction is:

- upstream login providers such as Microsoft, Google, and GitHub are **identity sources**
- a broker, issuer, or compatible runtime deployment turns those identities into a **runtime-compatible bearer token**
- `ocli` only needs to understand the standard runtime auth metadata and bearer-token acquisition flow
- `oclird` only needs to enforce the standard runtime token semantics and observable authorization behavior

The daemon's internal token-validation implementation remains operator-defined. A compatible implementation may use:

- OIDC issuer discovery + JWKS validation
- RFC7662 introspection
- a trusted upstream auth proxy
- another implementation-defined mechanism

As long as the observable client-facing contract remains the same.

## Design principles

### 1. Contract over implementation

`ocli` should not need to know whether a runtime validates tokens through JWKS, introspection, or a reverse proxy.

It should only rely on:

- runtime auth metadata
- structured bearer-auth behavior
- scope semantics
- handshake and error contracts

### 2. Runtime tokens are not raw upstream IdP tokens

The standard unit of authorization is a **runtime token**, not a provider-native token from Entra, Google, or GitHub.

A runtime token is a bearer token that satisfies the runtime contract:

- valid for the intended runtime audience
- minted by or normalized through an operator-chosen broker/issuer
- carries runtime-relevant identity and scope information
- bounded to the agent/session lifetime expected by the runtime deployment

This lets organizations federate any upstream IdP into a standard runtime contract.

### 3. One reference example, many implementations

The project should provide one working reference example of:

- a brokered login setup
- external-provider federation
- runtime token issuance
- runtime validation

But the spec must make clear that this example is illustrative, not normative.

## External compatibility model

There are three layers:

1. **Upstream identity providers**
   - Microsoft Entra ID
   - Google
   - GitHub
   - others

2. **Runtime auth broker / issuer**
   - a broker, gateway, auth service, or runtime-integrated issuer that turns upstream identity into a runtime-compatible token

3. **Compatible runtime**
   - any `oclird` implementation that honors this contract

`ocli` interoperates with layer 2 and layer 3 through the standard contract. It does not need provider-specific logic for each upstream IdP beyond normal OAuth2/OIDC browser or client-credential flows.

## Client-visible auth contract

### Remote runtime auth modes

The client contract keeps these high-level acquisition modes:

- `providedToken`
- `oauthClient`
- `browserLogin`

These modes are broker-facing, not provider-facing.

#### `providedToken`

The operator provides a pre-issued runtime token. `ocli` forwards it as-is.

#### `oauthClient`

`ocli` obtains a runtime token from a configured token endpoint using client credentials or another non-interactive broker-supported flow.

#### `browserLogin`

`ocli` learns browser-login metadata from the runtime and completes an authorization-code + PKCE flow against the broker/issuer selected by that runtime deployment.

## Runtime metadata contract

### `GET /v1/auth/browser-config`

The runtime must expose browser-login metadata sufficient for `ocli` to authenticate against the broker/issuer chosen by that runtime.

Minimum fields:

```json
{
  "authorizationURL": "https://auth.example.com/authorize",
  "tokenURL": "https://auth.example.com/token",
  "clientId": "ocli-browser",
  "audience": "oclird"
}
```

Additional recommended fields for the generalized contract:

```json
{
  "issuer": "https://auth.example.com",
  "scopesSupported": ["openid", "profile", "email", "bundle:payments"],
  "tokenValidationProfile": "oidc_jwks"
}
```

Rules:

- `authorizationURL`, `tokenURL`, and `clientId` are the minimum portable browser-login surface
- `issuer` is recommended whenever the deployment uses OIDC discovery or JWT/JWKS validation
- `tokenValidationProfile` is informational for diagnostics and interoperability, not a client-side validation instruction

### `GET /v1/runtime/info`

The runtime handshake surface must advertise auth-relevant compatibility information in a stable way.

Recommended auth block:

```json
{
  "contractVersion": "1.1",
  "capabilities": ["catalog", "execute", "refresh", "audit", "browser-login"],
  "auth": {
    "required": true,
    "audience": "oclird",
    "scopePrefixes": ["bundle:", "tool:", "profile:"],
    "tokenValidationProfiles": ["oidc_jwks"],
    "browserLogin": true
  }
}
```

Rules:

- the runtime must clearly state whether bearer auth is required
- the runtime must state the audience expected for compatible runtime tokens
- the runtime must state the scope-prefix vocabulary it understands
- the runtime may advertise one or more validation profiles for operator diagnostics

## Runtime token contract

A compatible runtime token must carry enough information for the runtime to compute the authorization envelope.

### Required semantic fields

Regardless of whether the token is a JWT, an opaque token, or the result of proxy validation, the runtime must derive these semantics:

- issuer identity
- audience
- principal identity
- expiry / validity
- runtime scopes

### Standard runtime claims model

For deployments that mint JWT runtime tokens directly, the normalized claim contract is:

- `iss`: broker or issuer identifier
- `aud`: target runtime audience
- `sub`: principal or session subject
- `exp`: expiry
- `scope`: space-separated runtime scopes

Illustrative JWT payload:

```json
{
  "iss": "https://auth.example.com",
  "aud": "oclird",
  "sub": "user-or-session",
  "exp": 1773599999,
  "scope": "bundle:payments tool:users.get"
}
```

Optional:

- `client_id`: confidential client identity for machine actors
- `sid`: session identifier
- `azp`: authorized party
- `groups`: organization-specific grouping information

Rules:

- `scope` is the standard portable runtime-scope carrier
- provider-specific upstream claims such as Entra `scp`, Google-specific claims, or GitHub-specific attributes are not part of the runtime contract
- a broker may transform upstream claims into standard runtime `scope` values before issuing the runtime token

## Runtime scope contract

The standard runtime scope vocabulary remains:

- `bundle:<service-id>`
- `tool:<tool-id>`
- `profile:<profile-name>`

Rules:

- bundle and profile scopes union together first
- explicit `tool:` scopes intersect the unioned candidate set when present
- deny rules always win
- only configured tools may be exposed, even when a token presents broader scopes

This keeps the remote authorization envelope portable across daemon implementations.

## Daemon-side compatibility contract

A compatible runtime implementation may validate tokens however it wants internally, but it must preserve these observable behaviors:

### Required observable behaviors

- protected endpoints require `Authorization: Bearer ...`
- invalid, expired, revoked, or otherwise unauthorized tokens fail with `authn_failed`
- unauthorized discovery/execution fails according to the existing authz rules
- catalog filtering and execution checks use the same authorization envelope
- empty authorized catalogs return success with an empty list
- remote mode never silently falls back to embedded or local execution

### Supported validation profiles

The spec recognizes these interoperable validation profiles:

#### `oidc_jwks`

The runtime validates a broker-issued JWT access token using:

- issuer identity
- OIDC discovery and/or configured JWKS
- signature verification
- audience
- expiry
- normalized runtime `scope`

This is the preferred profile for brokered federation scenarios such as Microsoft, Google, and GitHub login through a central broker.

#### `oauth2_introspection`

The runtime validates the token through an RFC7662-compatible introspection endpoint and derives:

- active/inactive state
- audience
- principal
- scopes

This remains valid, but it is not the only allowed implementation profile.

#### `external_assertion`

An implementation may rely on a trusted auth proxy or gateway, as long as the resulting runtime behavior is equivalent and the deployment is explicit about the trust boundary.

This profile is implementation-defined and is not the recommended reference example.

## Error and handshake contract

The existing remote-runtime error taxonomy remains:

- `runtime_unreachable`
- `authn_failed`
- `authz_denied`
- `contract_mismatch`

Additional rule:

- the client must not need provider-specific parsing logic to understand remote auth failures

Remote handshake responses should surface:

- contract version
- advertised capabilities
- principal or session identity when resolved
- authorization-envelope metadata/version
- auth metadata sufficient for diagnostics

## Reference example

The project should provide one concrete reference example:

### Recommended example

A brokered OIDC deployment where:

- Microsoft Entra ID, Google, and GitHub are upstream login providers
- a central broker federates those providers
- the broker issues a standard runtime JWT access token
- `ocli` uses `browserLogin` or `oauthClient` against that broker
- `oclird` validates the broker-issued token through `oidc_jwks`

### Why this example

This proves:

- the contract works with common third-party login systems
- the runtime does not depend on provider-native token validation quirks
- organizations can substitute their own broker or compatible runtime implementation later

### Example-specific notes

The example broker is not normative. The contract only requires that the example deployment:

- expose OIDC/OAuth endpoints usable by `ocli`
- mint runtime-compatible tokens
- preserve runtime audience and scope semantics
- let `oclird` validate tokens with a standard validation profile

## Configuration direction

The server-side auth model should evolve from an implementation-specific mode name toward a contract-oriented shape.

Illustrative direction:

```json
{
  "runtime": {
    "server": {
      "auth": {
        "required": true,
        "validationProfile": "oidc_jwks",
        "audience": "oclird",
        "issuer": "https://auth.example.com",
        "jwksURL": "https://auth.example.com/.well-known/jwks.json",
        "authorizationURL": "https://auth.example.com/authorize",
        "tokenURL": "https://auth.example.com/token",
        "browserClientId": "ocli-browser"
      }
    }
  }
}
```

An introspection-based deployment may use a different profile and fields:

```json
{
  "runtime": {
    "server": {
      "auth": {
        "required": true,
        "validationProfile": "oauth2_introspection",
        "audience": "oclird",
        "introspectionURL": "https://auth.example.com/introspect",
        "clientId": { "type": "env", "value": "OAS_INTROSPECT_CLIENT_ID" },
        "clientSecret": { "type": "env", "value": "OAS_INTROSPECT_CLIENT_SECRET" }
      }
    }
  }
}
```

## Security and portability rules

- `ocli` must not hard-code Microsoft-, Google-, or GitHub-specific token semantics into the runtime contract
- the runtime contract must be broker-oriented and standards-oriented
- organizations may implement a custom runtime or auth bridge as long as the observable behavior matches this contract
- the reference example must not imply exclusivity or product lock-in

## Testing expectations

This design is complete when the eventual implementation verifies:

- browser login against the reference broker
- non-interactive token acquisition against the reference broker
- remote catalog filtering and execution auth using broker-issued runtime scopes
- token expiry and invalid-token behavior under the selected validation profile
- compatibility of the runtime metadata contract and handshake
- the reference example working with upstream federation from Microsoft, Google, and GitHub

## Consequence for current design

The current introspection-only remote-runtime implementation is a valid implementation profile, but it is **not sufficient as the only contract**.

The standard must move up one layer:

- from "the daemon introspects tokens this one way"
- to "the client and runtime agree on a standard runtime auth contract, while the daemon's internal validation mechanism is pluggable"
