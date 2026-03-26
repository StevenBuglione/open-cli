---
title: Config Schema
---

# Config Schema

The published JSON Schema for external validators lives in the in-repo spec subproject:

- `spec/schemas/cli.schema.json`

This implementation also carries a local copy at `pkg/config/cli.schema.json`, and the loader embeds that copy for runtime validation. In practice, keep both in sync and treat the `spec/schemas/` version as the public contract.

## Top-level fields

| Field | Type | Required | Notes |
| --- | --- | --- | --- |
| `cli` | string | yes | Must be non-empty. |
| `mode` | object | yes | Requires `default`. |
| `runtime` | object | no | Hosted runtime selection and runtime-auth settings. |
| `sources` | object | yes | Must contain at least one source after final merge. |
| `mcpServers` | object | no | Compatibility input normalized into canonical MCP sources and services. |
| `services` | object | no | Optional named services. |
| `curation` | object | no | Tool sets for curated views. |
| `agents` | object | no | Profiles and default profile. |
| `policy` | object | no | Approval and secret-exec policy. |
| `secrets` | object | no | Secret references keyed by security scheme name. |

## `sources.*`

| Field | Type | Required | Values / meaning |
| --- | --- | --- | --- |
| `type` | string | yes | `apiCatalog`, `serviceRoot`, `openapi`, or `mcp` |
| `uri` | string | depends | Required for non-MCP sources |
| `enabled` | boolean | no | Defaults to enabled for newly introduced sources |
| `refresh.maxAgeSeconds` | integer | no | Must be `>= 0` |
| `refresh.manualOnly` | boolean | no | Reuse cache until a forced refresh is requested |
| `transport` | object | for `mcp` | MCP transport configuration |
| `disabledTools` | string[] | no | MCP tools removed before normalization |
| `oauth` | object | streamable-http only | Source-local MCP transport OAuth (`clientCredentials` only) |

For `openapi`, `serviceRoot`, and `apiCatalog` sources, `uri` is the only discovery endpoint field. MCP-only fields (`transport`, `disabledTools`, and source-local `oauth`) are rejected by the schema.

## `runtime`

| Field | Type | Meaning |
| --- | --- | --- |
| `mode` | string | Must be `remote` in the current CLI |
| `local` | object | Legacy schema field retained for compatibility; rejected by current semantic validation |
| `remote` | object | Remote runtime URL and runtime-auth configuration |

## `runtime.remote`

| Field | Type | Meaning |
| --- | --- | --- |
| `url` | string | Base URL for the remote runtime. Required when `runtime.mode` is `remote`. |
| `oauth` | object | Optional runtime-auth configuration for the remote runtime itself |

## `runtime.remote.oauth`

| Field | Type | Meaning |
| --- | --- | --- |
| `mode` | string | `providedToken`, `oauthClient`, or `browserLogin` |
| `audience` | string | Optional OAuth audience for runtime token requests |
| `scopes` | string[] | Optional runtime scopes such as service bundles or tool grants |
| `tokenRef` | string | Required for `providedToken`; currently supports `env:NAME` references |
| `client.tokenURL` | string | Token endpoint for `oauthClient` mode |
| `client.clientId` | object | Secret ref for the OAuth client ID in `oauthClient` mode |
| `client.clientSecret` | object | Secret ref for the OAuth client secret in `oauthClient` mode |
| `browserLogin.callbackPort` | integer | Optional fixed localhost callback port for `browserLogin` mode |

## `runtime.server.auth`

| Field | Type | Meaning |
| --- | --- | --- |
| `validationProfile` | string | `oidc_jwks` or `oauth2_introspection` |
| `mode` | string | Legacy alias for `oauth2Introspection`; accepted on input and normalized to `validationProfile: "oauth2_introspection"` |
| `issuer` | string | Required issuer for `oidc_jwks` validation |
| `jwksURL` | string | Required JWKS endpoint for `oidc_jwks` validation |
| `audience` | string | Required audience for both `oidc_jwks` and `oauth2_introspection` validation |
| `introspectionURL` | string | RFC 7662-style token introspection endpoint for `oauth2_introspection` |
| `clientId` / `clientSecret` | object | Optional secret refs used when the introspection endpoint itself requires client auth |
| `authorizationURL` | string | Optional browser-login authorization endpoint exposed via runtime metadata |
| `tokenURL` | string | Optional browser-login token endpoint exposed via runtime metadata |
| `browserClientId` | string | Optional public OAuth client ID exposed via runtime metadata |

`runtime.server.auth.validationProfile: "oidc_jwks"` requires `issuer`, `jwksURL`, and `audience`.

`runtime.server.auth.validationProfile: "oauth2_introspection"` requires both `audience` and `introspectionURL`.

## `sources.*.transport`

| Field | `stdio` | `sse` | `streamable-http` |
| --- | --- | --- | --- |
| `type` | required | required | required |
| `command` | required | forbidden | forbidden |
| `args` | optional | forbidden | forbidden |
| `env` | optional | forbidden | forbidden |
| `url` | forbidden | required | required |
| `headers` | forbidden | optional | optional |
| `headerSecrets` | forbidden | optional | optional |

Current stdio framing is newline-delimited JSON-RPC.

## `mcpServers.*`

`mcpServers` is a compatibility input for `.mcp.json`-style configuration. Each entry is normalized into:

- `sources.<name>` with `type: "mcp"`
- `services.<name>` when an explicit service is not already present

## `services.*`

| Field | Type | Required | Meaning |
| --- | --- | --- | --- |
| `source` | string | yes | Source ID to bind to this service |
| `alias` | string | no | Top-level CLI command name |
| `overlays` | string[] | no | Overlay document refs |
| `skills` | string[] | no | Skill manifest refs |
| `workflows` | string[] | no | Arazzo workflow refs |

## `curation.toolSets.*`

| Field | Type | Meaning |
| --- | --- | --- |
| `allow` | string[] | If non-empty, only matching tools are visible in the tool set |
| `deny` | string[] | Matching tools are removed from the tool set |

## `agents`

| Field | Type | Meaning |
| --- | --- | --- |
| `defaultProfile` | string | Profile used when the active mode is curated and no explicit profile is provided |
| `profiles.*.mode` | string | `discover` or `curated` |
| `profiles.*.toolSet` | string | Name of a tool set under `curation.toolSets` |

## `policy`

| Field | Type | Meaning |
| --- | --- | --- |
| `deny` | string[] | Merged into effective config; see runtime nuance below |
| `approvalRequired` | string[] | Tool patterns that require approval |
| `allowExecSecrets` | boolean | Enables `exec` secret resolution |

## `secrets.*`

| Field | Type | Meaning |
| --- | --- | --- |
| `type` | string | `env`, `file`, `osKeychain`, `exec`, or `oauth2` |
| `value` | string | Env var name, file path, keychain ref, or fallback command |
| `command` | string[] | Explicit argv for `exec` secrets |
| `mode` | string | For `oauth2`: `authorizationCode` or `clientCredentials` |
| `issuer` / `authorizationURL` / `tokenURL` | string | OAuth endpoint configuration |
| `clientId` / `clientSecret` | object | Nested static secret refs for OAuth clients |
| `scopes` / `audience` | string[] / string | Optional OAuth request shaping |
| `interactive`, `callbackPort`, `redirectURI`, `tokenStorage` | mixed | Authorization-code and token caching controls |

## Loader behavior that matters in practice

### JSON only

Config files are decoded with Go's JSON decoder. `.cli.json` is literal JSON, not YAML.

### Unknown fields are rejected

The loader calls `DisallowUnknownFields()` during JSON decoding. In practice, that means stray keys still fail even in places where the JSON Schema is intentionally permissive for forward compatibility.

### Cross-reference validation exists

After schema validation, the loader also checks that every `services.<id>.source` points at a known source.

It also validates MCP transport constraints, OAuth ownership of the transport `Authorization` header, and MCP/OAuth compatibility rules such as "SSE forbids source-local OAuth".

It also validates runtime-specific rules such as:

- `runtime.mode: "remote"` requires `runtime.remote.url`
- any other `runtime.mode` value is rejected (`must be remote`)
- `runtime.local` is rejected as unsupported
- `runtime.remote.oauth.mode: "providedToken"` requires `runtime.remote.oauth.tokenRef`
- `runtime.remote.oauth.mode: "oauthClient"` requires `client.tokenURL`, `client.clientId`, and `client.clientSecret`
- `runtime.server.auth.validationProfile: "oidc_jwks"` requires `issuer`, `jwksURL`, and `audience`
- `runtime.server.auth.validationProfile: "oauth2_introspection"` requires `audience` and `introspectionURL`
- legacy `runtime.server.auth.mode: "oauth2Introspection"` is still accepted and normalizes to `validationProfile: "oauth2_introspection"`

### Final validation is stricter than per-scope validation

Individual scope files may omit required top-level fields. The final merged config may not.

## Runtime nuance: `policy.deny`

The schema exposes `policy.deny`, and the loader merges it into the effective config. The current execution-time policy engine, however, only enforces the internally tracked managed deny list directly. Use curated tool sets and managed scope policy for hard deny behavior in the current release.
