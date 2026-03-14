# OAS-CLI Config 0.1.0

## File Locations

Recommended scope locations:

- Managed: `/etc/oas-cli/.cli.json`
- User: `$XDG_CONFIG_HOME/oas-cli/.cli.json`
- Project: `<repo>/.cli.json`
- Local: `<repo>/.cli.local.json`

## Precedence

Scopes merge in this order:

1. Managed
2. User
3. Project
4. Local

Later scopes override earlier mutable values, but Managed denies remain absolute.

## Merge Rules

- keyed maps merge by key
- explicit `enabled: false` disables a source or service without deleting it
- allow and deny arrays append uniquely across scopes
- managed denies are preserved separately and remain non-overridable
- `mcpServers` compatibility entries normalize into canonical `sources` + `services` before final validation

## Source Types

Canonical `sources.*.type` values are:

- `apiCatalog`
- `serviceRoot`
- `openapi`
- `mcp`

`openapi`, `apiCatalog`, and `serviceRoot` sources require `uri`.

`mcp` sources require `transport` instead of `uri`.

## Native MCP Sources

An MCP source is configured directly under `sources`:

```json
{
  "sources": {
    "filesystem": {
      "type": "mcp",
      "enabled": true,
      "transport": {
        "type": "stdio",
        "command": "npx",
        "args": ["-y", "@modelcontextprotocol/server-filesystem", "/workspace"]
      },
      "disabledTools": ["delete_file"]
    }
  },
  "services": {
    "filesystem": {
      "source": "filesystem",
      "alias": "filesystem"
    }
  }
}
```

Supported MCP transport types are:

| Transport | Required fields | Allowed extra fields | Forbidden fields |
| --- | --- | --- | --- |
| `stdio` | `transport.command` | `transport.args`, `transport.env`, `disabledTools` | `transport.url`, `transport.headers`, `transport.headerSecrets`, `oauth` |
| `sse` | `transport.url` | `transport.headers`, `transport.headerSecrets`, `disabledTools` | `transport.command`, `transport.args`, `transport.env`, `oauth` |
| `streamable-http` | `transport.url` | `transport.headers`, `transport.headerSecrets`, `disabledTools`, `oauth` | `transport.command`, `transport.args`, `transport.env` |

Current MCP stdio framing is newline-delimited JSON-RPC messages.

## `.mcp.json` Compatibility via `mcpServers`

Implementations MAY also accept top-level `mcpServers` for compatibility with existing MCP-oriented workflows.

Each `mcpServers.<name>` entry is normalized into:

- `sources.<name>` with `type: "mcp"`
- `services.<name>` bound to that source when no explicit service exists yet

This lets existing MCP configs move into OAS-CLI with minimal structural change while keeping the canonical merged form centered on `sources` and `services`.

## MCP Transport Authentication

`streamable-http` MCP sources MAY define source-local `oauth` and `transport.headerSecrets`.

- `oauth` authenticates the MCP transport itself
- source-local MCP transport `oauth.mode` is `clientCredentials`
- `transport.headerSecrets` resolves header values through top-level `secrets`
- when `oauth` is present, it owns the `Authorization` header for that transport

Example:

```json
{
  "sources": {
    "remoteDocs": {
      "type": "mcp",
      "transport": {
        "type": "streamable-http",
        "url": "https://mcp.example.com/mcp",
        "headerSecrets": {
          "X-API-Key": "mcp.apiKey"
        }
      },
      "oauth": {
        "mode": "clientCredentials",
        "tokenURL": "https://auth.example.com/oauth/token",
        "clientId": { "type": "env", "value": "MCP_CLIENT_ID" },
        "clientSecret": { "type": "env", "value": "MCP_CLIENT_SECRET" },
        "scopes": ["mcp.read"]
      }
    }
  },
  "secrets": {
    "mcp.apiKey": {
      "type": "env",
      "value": "MCP_API_KEY"
    }
  }
}
```

## Secret References

Static secret values MUST NOT be embedded directly in project config. Supported reference types are:

- `env`
- `file`
- `osKeychain`
- `exec`

`exec` resolution MUST be disabled unless explicitly enabled by policy.

## OAuth Secrets for OpenAPI Security Schemes

Top-level `secrets` MAY also contain `type: "oauth2"` entries for OpenAPI `oauth2` and `openIdConnect` execution.

Supported runtime modes are:

- `authorizationCode`
- `clientCredentials`

An OAuth secret may declare:

- `issuer`
- `authorizationURL`
- `tokenURL`
- `clientId`
- `clientSecret`
- `scopes`
- `audience`
- `interactive`
- `callbackPort`
- `redirectURI`
- `tokenStorage`

Example:

```json
{
  "secrets": {
    "pets.petstore_oauth": {
      "type": "oauth2",
      "mode": "clientCredentials",
      "clientId": { "type": "env", "value": "PETSTORE_CLIENT_ID" },
      "clientSecret": { "type": "osKeychain", "value": "oas-cli/petstore-secret" },
      "scopes": ["pets.read"]
    }
  }
}
```

## Refresh Policy

Each source MAY define a `refresh` object with these fields:

- `maxAgeSeconds`: advisory freshness override for HTTP-fetched discovery documents when origin cache headers are absent or more permissive local policy is required
- `manualOnly`: when `true`, automatic revalidation SHOULD be suppressed and stale cached content SHOULD be retained until an explicit refresh trigger is invoked

Refresh policy is source-scoped and follows normal scope precedence rules.
