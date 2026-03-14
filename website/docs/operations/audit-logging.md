---
title: Audit Logging
---

# Audit Logging

Most `POST /v1/tools/execute` requests are written to an append-only audit file after the runtime successfully loads the catalog and resolves the target tool.

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

## What does not get audited

- malformed execute requests that fail JSON decoding
- catalog-load failures before a tool is resolved
- unknown tool IDs that fail before policy/execution handling
- catalog fetches
- refresh requests
- workflow validation requests

## Caveats

- writes are append-only and mutex-protected within a process
- there is no built-in rotation or retention policy
- there is no explicit `fsync`, so crash durability is best-effort
- `authScheme` and `requestSize` fields exist but are not meaningfully populated today

## Access methods

You can read audit data either:

- from disk directly
- through `GET /v1/audit/events`

The HTTP endpoint returns the full file content as an array. There is no server-side filtering.
