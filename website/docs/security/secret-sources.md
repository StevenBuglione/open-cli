---
title: Secret Sources
---

# Secret Sources

Secrets are configured under `secrets` and resolved at execution time.

## Supported source types

| Type | Example | Runtime behavior |
| --- | --- | --- |
| `env` | `{"type":"env","value":"HELPDESK_TOKEN"}` | Reads `os.Getenv(value)` |
| `file` | `{"type":"file","value":"/run/secrets/helpdesk-token"}` | Reads the entire file contents |
| `osKeychain` | `{"type":"osKeychain","value":"helpdesk/token"}` | Uses the platform keychain helper |
| `exec` | `{"type":"exec","command":["sh","-lc","printf token"]}` | Runs a command and uses stdout |
| `oauth2` | see below | Acquires, caches, and reuses OAuth access tokens |

## `env`

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

Nuance: if the env var is unset, the runtime gets an empty string. That usually means auth is skipped or malformed, not that config loading fails.

## `file`

```json
{
  "secrets": {
    "bearerAuth": {
      "type": "file",
      "value": "/run/secrets/helpdesk-token"
    }
  }
}
```

Nuance: the runtime reads the file verbatim. Trailing newlines are preserved.

## `osKeychain`

```json
{
  "secrets": {
    "bearerAuth": {
      "type": "osKeychain",
      "value": "helpdesk/token"
    }
  }
}
```

Supported reference formats:

- `service/account`
- `service:account`

Current helpers:

- macOS: `security find-generic-password -s <service> -a <account> -w`
- Linux: `secret-tool lookup service <service> account <account>`

The default keychain resolver trims surrounding whitespace from command output.

## `exec`

`exec` secrets are disabled unless:

```json
{
  "policy": {
    "allowExecSecrets": true
  }
}
```

Example:

```json
{
  "policy": {
    "allowExecSecrets": true
  },
  "secrets": {
    "bearerAuth": {
      "type": "exec",
      "command": ["sh", "-lc", "printf token-from-exec"]
    }
  }
}
```

Important nuances:

- if `command` is omitted and `value` is set, `value` is treated as the executable path, not as a shell command line
- stdout is used as-is; trailing newlines are **not** trimmed
- there is no stdin wiring or timeout in the current implementation
- command failures propagate as secret-resolution errors, which usually causes auth to be skipped for that scheme

## Operational guidance

- prefer `env` or `osKeychain` for local developer workflows
- prefer `file` for container/secret-volume setups
- use `exec` only when you need dynamic retrieval and accept the extra risk/latency

## `oauth2`

`oauth2` secrets are used for OpenAPI `oauth2` and `openIdConnect` security schemes.

Supported runtime modes:

- `clientCredentials`
- `authorizationCode`

Example:

```json
{
  "secrets": {
    "pets.petstore_oauth": {
      "type": "oauth2",
      "mode": "clientCredentials",
      "clientId": {
        "type": "env",
        "value": "PETSTORE_CLIENT_ID"
      },
      "clientSecret": {
        "type": "osKeychain",
        "value": "oas-cli/petstore-secret"
      },
      "scopes": ["pets.read"],
      "tokenStorage": "instance"
    }
  }
}
```

Important runtime behavior:

- service-specific lookup wins before shared lookup, so `pets.petstore_oauth` overrides `petstore_oauth`
- `openIdConnect` uses OIDC discovery and then follows the configured mode
- tokens are cached per runtime instance under the instance state directory
- `tokenStorage: "memory"` keeps tokens in-process only
- `authorizationCode` uses a loopback callback on `127.0.0.1`
- `implicit` and `password` flows are not supported

## MCP `headerSecrets`

MCP `transport.headerSecrets` references top-level static secrets and injects them into remote MCP transport headers.

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
