---
title: Audit Logging
---

# Audit Logging

**Read this if** you need to understand what the runtime records, how to consume audit data, or what the current gaps and caveats are before relying on audit for compliance. This page answers: what event types exist, where the file lives, and what does not get audited today.

Runtime activity is written to an append-only audit file. Execution events still make up most records, but the runtime also records auth and session lifecycle transitions.

## Default location

For a resolved instance, the default audit path is:

```text
<state-root>/instances/<instance-id>/audit.log
```

You can override it in daemon mode with:

```bash
oclird --audit-path /var/log/ocli/team-a.audit.log
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

## Compliance notes

These caveats have direct compliance implications:

- **Retention** — there is no expiry or purge mechanism. If your compliance framework requires retention limits, you must enforce them outside the daemon (e.g., with `logrotate` or a log pipeline that archives and rotates the file).
- **Durability** — best-effort `fsync` means crash or power loss could cause the final few events to be lost. If you need guaranteed write durability, mount the audit path on a storage layer with its own durability guarantees.
- **Query / filtering** — the HTTP endpoint returns the full file content. For large deployments, reading and filtering on disk is more practical than polling the HTTP endpoint.
- **Token revocation coverage** — the audit log records auth events at validation time. It does not record post-issuance revocation because revocation is not implemented. Events in the log do not prove the token was still valid when read back later.

## If you are trying to…

| Goal | Go to |
| --- | --- |
| Override the audit file path in daemon mode | See `--audit-path` flag in this page |
| Understand what is missing from audit coverage | See [Caveats](#caveats) in this page |
| Route audit data to an external sink | Read the file directly or poll `GET /v1/audit/events`; there is no push exporter today |
| Review the full HTTP API surface | [HTTP API](../runtime/http-api) |

## Access methods

You can read audit data either:

- from disk directly
- through `GET /v1/audit/events`

The HTTP endpoint returns the full file content as an array. There is no server-side filtering.
