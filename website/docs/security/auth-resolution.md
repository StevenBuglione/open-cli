---
title: Auth Resolution
---

# Auth Resolution

Auth resolution happens inside the runtime just before tool execution.

## Step 1: extract auth requirements from OpenAPI

The catalog builder inspects operation-level or document-level security requirements and records entries such as:

- scheme name (`bearerAuth`)
- type (`http`, `apiKey`, `oauth2`, `openIdConnect`)
- scheme (`bearer`, `basic`, ...)
- location (`header`, `query`, `cookie`)
- parameter name for API keys
- required scopes for OAuth-backed schemes
- OAuth flow metadata such as token and authorization endpoints

## Step 2: match by security scheme name

At execution time, the runtime looks up each auth requirement by name in `config.secrets`.

Lookup order is:

1. `secrets["<service>.<scheme>"]`
2. `secrets["<scheme>"]`

Example:

```yaml
components:
  securitySchemes:
    bearerAuth:
      type: http
      scheme: bearer
```

must line up with:

```json
{
  "secrets": {
    "bearerAuth": {
      "type": "env",
      "value": "HELPDESK_TOKEN"
    }
  }
}
```

## Step 3: resolve secret values

The runtime resolves the configured secret source only when a tool is actually executed.

That means:

- missing env vars are not caught during config load
- unreadable files are not caught during config load
- a broken exec secret is not caught during config load

## Supported applied auth schemes

The current executor automatically applies:

- HTTP bearer auth
- HTTP basic auth
- API keys in headers
- API keys in query parameters
- OAuth bearer tokens acquired through `oauth2` or `openIdConnect`

## Important limitations

### Cookie auth is not auto-applied

`apiKey` security schemes with `in: cookie` are normalized into catalog metadata, but the execution layer does not currently apply them automatically as auth.

### Unsupported OAuth flows fail explicitly

`authorizationCode` and `clientCredentials` are supported.

`implicit` and `password` are intentionally rejected with runtime errors.

### Alternative security requirements are flattened

OpenAPI security requirement objects can express alternatives. The current extractor flattens and de-duplicates all referenced scheme names, then applies any secret it can resolve.

In practice, that means the runtime may apply multiple schemes at once instead of choosing one alternative branch.

### Secret failures do not all behave the same way

If a secret lookup fails for `file`, `osKeychain`, or `exec`, the runtime usually omits that auth scheme from the outgoing request.

`env` is different: an unset environment variable resolves to an empty string, so the runtime still applies the auth scheme with an empty value.

In practice that can produce:

- `Authorization: Bearer ` for bearer auth
- `Authorization: Basic ` for basic auth with an empty credential payload
- an empty API key header or query value

The tool may then fail at the upstream API with `401` or `403`.

## Recommendation

- keep security scheme names stable
- prefer one clear auth method per operation where possible
- test `tool schema` plus a real execution path together when introducing auth

For source-specific secret behavior, continue with [Secret sources](./secret-sources).
