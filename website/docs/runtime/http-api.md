---
title: HTTP API
---

# HTTP API

The runtime exposes a small JSON-over-HTTP API from `internal/runtime/server.go`.

:::note Error shape
Success responses are JSON. Error responses use Go's `http.Error`, so they are plain text rather than structured JSON.
:::

## `GET /v1/catalog/effective`

Returns the full catalog plus the selected effective view.

When `runtime.server.auth` is enabled, this endpoint requires `Authorization: Bearer ...` and the returned catalog is already filtered to the scopes authorized for that token.

### Query parameters

| Parameter | Meaning |
| --- | --- |
| `config` | Path to `.cli.json`. Required unless the daemon was started with a default config. |
| `mode` | Optional mode override. |
| `agentProfile` | Optional profile name. |

### Example

```bash
curl -s 'http://127.0.0.1:8765/v1/catalog/effective?config=/work/demo/.cli.json&agentProfile=support' | jq
```

### Response shape

```json
{
  "catalog": {
    "catalogVersion": "1.0.0",
    "sources": [],
    "services": [],
    "tools": [],
    "workflows": [],
    "effectiveViews": []
  },
  "view": {
    "name": "support",
    "mode": "curated",
    "tools": []
  }
}
```

## `POST /v1/tools/execute`

Executes one normalized tool.

### Request body

```json
{
  "configPath": "/work/demo/.cli.json",
  "mode": "curated",
  "agentProfile": "support",
  "toolId": "tickets:createTicket",
  "pathArgs": [],
  "flags": {
    "state": "open"
  },
  "body": "eyJ0aXRsZSI6IlByaW50ZXIgamFtIn0=",
  "approval": true
}
```

Notes:

- `body` is `[]byte` in Go, so JSON clients typically send it as base64 when talking to the runtime directly.
- the stock `oascli` client handles marshaling for you.
- the runtime accepts either the slugified CLI flag name or the original parameter name when it looks up values in `flags`.

### Success response

```json
{
  "statusCode": 201,
  "body": {
    "id": "T-100",
    "title": "Printer jam"
  }
}
```

If the upstream body is not valid JSON, the runtime returns:

```json
{
  "statusCode": 200,
  "text": "plain text response"
}
```

### Common error statuses

- `400` for bad JSON or config/catalog load failures
- `403` for policy or approval rejections
- `401` for missing or invalid runtime bearer auth when server-side runtime auth is enabled
- `404` if the tool ID does not exist
- `502` if upstream execution fails before an HTTP response is returned

## `POST /v1/workflows/run`

Validates a workflow and returns its step IDs.

```bash
curl -s \
  -H 'Content-Type: application/json' \
  -d '{"configPath":"/work/demo/.cli.json","workflowId":"triageTicket","approval":true}' \
  http://127.0.0.1:8765/v1/workflows/run | jq
```

Success response:

```json
{
  "workflowId": "triageTicket",
  "steps": ["list-open", "fetch"]
}
```

The runtime does not execute the workflow steps.

## `GET /v1/refresh` and `POST /v1/refresh`

Forces a rebuild with `ForceRefresh=true` and reports source fetch outcomes.

### GET form

```bash
curl -s 'http://127.0.0.1:8765/v1/refresh?config=/work/demo/.cli.json' | jq
```

### POST form

```bash
curl -s \
  -H 'Content-Type: application/json' \
  -d '{"configPath":"/work/demo/.cli.json"}' \
  http://127.0.0.1:8765/v1/refresh | jq
```

Example response:

```json
{
  "refreshedAt": "2026-03-14T12:00:00Z",
  "sources": [
    {
      "id": "ticketsSource",
      "uri": "https://api.example.com/openapi.json",
      "cacheOutcome": "revalidated_hit",
      "statusCode": 200,
      "stale": false
    }
  ]
}
```

When server-side runtime auth is enabled, refresh also requires a bearer token.

## `GET /v1/audit/events`

Returns the full audit log as an array.

```bash
curl -s http://127.0.0.1:8765/v1/audit/events | jq
```

There is no filtering, paging, or server-side query API in the current implementation.

When the runtime has server-side auth enabled and a default config is known, this endpoint also requires bearer auth.

Each audit record is newline-delimited JSON with fields such as:

- `eventType` for the high-level lifecycle category (`tool_execution`, `authz_denial`, `catalog_filtered`, `authenticated_connect`, `authn_failure`, `token_refresh`, `session_close`, `session_expiry`)
- `principal` when server-side runtime auth resolved an authenticated subject
- `sessionId` for local lifecycle events
- the existing execution fields like `toolId`, `serviceId`, `decision`, `reasonCode`, `statusCode`, and `latencyMs`

## `GET /v1/auth/browser-config`

Returns the browser-login metadata that `oascli` uses for `runtime.remote.oauth.mode: "browserLogin"`.

Example response:

```json
{
  "authorizationURL": "https://auth.example.com/authorize",
  "tokenURL": "https://auth.example.com/token",
  "clientId": "oascli-browser",
  "audience": "oasclird"
}
```

## `GET /v1/runtime/info`

Returns basic runtime metadata for discovery and diagnostics.

Example response:

```json
{
  "instanceId": "team-a",
  "url": "http://127.0.0.1:18765",
  "auditPath": "/state/instances/team-a/audit.log",
  "stateDir": "/state/instances/team-a",
  "cacheDir": "/cache/instances/team-a/http",
  "lifecycle": {
    "capabilities": ["heartbeat", "sessionClose"],
    "heartbeatSeconds": 15,
    "missedHeartbeatLimit": 3,
    "shutdown": "when-owner-exits",
    "sessionScope": "terminal",
    "shareMode": "exclusive",
    "configFingerprint": "sha256:...",
    "activeSessions": 1
  }
}
```

The `lifecycle` block is present for lease-aware managed local runtimes and is what `oascli` uses to validate reuse before attaching.

## `POST /v1/runtime/heartbeat`

Registers or renews a local managed-runtime session lease.

### Request body

```json
{
  "sessionId": "terminal:pts-12",
  "configFingerprint": "sha256:..."
}
```

Common error responses:

- `409 runtime_attach_conflict` when the runtime is exclusive and a different session tries to attach
- `409 runtime_attach_mismatch` when the caller's effective local-runtime config fingerprint differs from the daemon's

## `POST /v1/runtime/stop`

Invokes the runtime shutdown hook when the daemon was started with one configured.

Success response:

```json
{
  "stopped": true
}
```

## `POST /v1/runtime/session-close`

Closes the current runtime session, removes its active lease, and clears session-scoped auth cache state under the runtime state directory.

Success response:

```json
{
  "closed": true
}
```
