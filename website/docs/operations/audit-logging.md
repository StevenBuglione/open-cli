---
title: Audit Logging
---

# Audit Logging

Runtime activity is written to an append-only audit file. Execution events still make up most records, but the runtime also records auth and session lifecycle transitions.

## Default location

For a resolved instance, the default audit path is:

```text
<state-root>/instances/<instance-id>/audit.log
```

You can override it in daemon mode with:

```bash
oasclird --audit-path /var/log/oascli/team-a.audit.log
```

## File format

The file is newline-delimited JSON. One event per line.

Example event:

```json
{
  "timestamp": "2026-03-14T12:00:00Z",
  "eventType": "tool_execution",
  "agentProfile": "support",
  "toolId": "tickets:createTicket",
  "serviceId": "tickets",
  "targetBaseUrl": "https://api.example.com",
  "decision": "allowed",
  "reasonCode": "allowed",
  "method": "POST",
  "path": "/tickets",
  "statusCode": 201,
  "latencyMs": 84,
  "retryCount": 0
}
```

## What gets audited

- successful tool executions
- denied tool executions
- execution failures after policy allowed the call
- authenticated catalog connects when runtime bearer auth is enabled
- authn failures on protected runtime HTTP surfaces
- refresh requests that completed under authenticated runtime access
- local `session-close` requests
- local lease expiry events

The `eventType` field distinguishes the main runtime categories:

- `tool_execution`
- `authz_denial`
- `catalog_filtered`
- `authenticated_connect`
- `authn_failure`
- `token_refresh`
- `session_close`
- `session_expiry`

## What does not get audited

- malformed execute requests that fail JSON decoding
- catalog-load failures before a tool is resolved
- unknown tool IDs that fail before policy/execution handling
- workflow validation requests

## Caveats

- writes are append-only and mutex-protected within a process
- there is no built-in rotation or retention policy
- there is no explicit `fsync`, so crash durability is best-effort
- `authScheme` and `requestSize` fields exist but are not meaningfully populated today
- there is still no server-side filtering, paging, or query language on `GET /v1/audit/events`

## Access methods

You can read audit data either:

- from disk directly
- through `GET /v1/audit/events`

The HTTP endpoint returns the full file content as an array. There is no server-side filtering.
