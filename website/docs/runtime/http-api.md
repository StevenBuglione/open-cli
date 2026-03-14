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

## `GET /v1/audit/events`

Returns the full audit log as an array.

```bash
curl -s http://127.0.0.1:8765/v1/audit/events | jq
```

There is no filtering, paging, or server-side query API in the current implementation.
