---
title: Refresh and Audit
---

# Refresh and Audit

Two runtime features matter most for operators after the first successful catalog build:

- **refresh** tells you what the runtime fetched and whether cache was reused
- **audit** tells you which tool executions were allowed or denied

## Refresh behavior

`/v1/refresh` rebuilds the catalog with `ForceRefresh=true` and reports per-source outcomes.

Possible cache outcomes today:

| Outcome | Meaning |
| --- | --- |
| `miss` | No cached response existed; a network fetch succeeded. |
| `revalidated_hit` | Cached response was revalidated with `If-None-Match` or `If-Modified-Since` and reused after `304 Not Modified`. |
| `refreshed` | Cached response existed (or was corrupt), and a new network fetch replaced it. |
| `stale_hit` | The network failed, so the runtime returned stale cached data. |

`fresh_hit` is a real cache outcome during normal catalog loads, but not for `/v1/refresh`, because refresh always forces a refetch/revalidation path.

### Important nuance: stale fallback is on by default for catalog sources

The catalog builder always uses `AllowStaleOnError: true` for source fetches. In practice that means a build can still succeed when the upstream discovery/OpenAPI endpoint is unavailable, as long as a cached copy already exists.

The refresh response surfaces that with `stale: true`.

## Manual-only sources

A source can set:

```json
{
  "refresh": {
    "manualOnly": true
  }
}
```

Current behavior is subtle:

- if a cached entry already exists, normal catalog loads reuse it and skip the network
- if no cache entry exists yet, the first load still has to fetch the source
- `/v1/refresh` always forces a fetch attempt

## Audit behavior

Audit entries are appended for **tool execution attempts** handled by `POST /v1/tools/execute` after the runtime has successfully decoded the request, loaded the catalog, and resolved the target tool.

That includes:

- allowed executions
- denied executions (`managed_deny`, `curated_deny`, `approval_required`)
- execution failures recorded as `execution_error`

It does **not** currently record separate events for:

- malformed execute requests that fail before tool resolution
- catalog-load failures before a tool is resolved
- unknown tool IDs
- `catalog/effective`
- `refresh`
- `workflow run`

## Audit record shape

Each line in the on-disk file is one JSON object. Fields include:

- `timestamp`
- `agentProfile`
- `toolId`
- `serviceId`
- `targetBaseUrl`
- `decision`
- `reasonCode`
- `method`
- `path`
- `statusCode`
- `latencyMs`
- `retryCount`

Two fields exist but are not populated meaningfully today:

- `authScheme` is empty
- `requestSize` is always `0`

## Operational caveats

- Audit storage is append-only newline-delimited JSON.
- Writes are mutex-protected within one process, but there is no `fsync` for crash durability.
- `/v1/audit/events` returns the entire file; large logs should be rotated externally.

See [Audit logging](../operations/audit-logging) and [Cache and refresh](../operations/cache-and-refresh) for storage details.
