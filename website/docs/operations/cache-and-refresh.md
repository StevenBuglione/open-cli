---
title: Cache and Refresh
---

# Cache and Refresh

Remote discovery and OpenAPI fetches go through `pkg/cache`.

## Storage format

Each cached item is stored as two files keyed by a SHA-256 digest of the cache key:

- `<digest>.json` for metadata
- `<digest>.body` for the response body

The metadata captures:

- request URL and method
- headers
- `ETag`
- `Last-Modified`
- `Cache-Control`
- cache timestamps and expiry
- response status code
- stale marker

## Cache key rules

The default key is:

```text
<METHOD>:<URL>
```

If the request has an `Accept` header, it becomes:

```text
<METHOD>:<URL>:<Accept>
```

That matters for API catalogs, which request `application/linkset+json`.

## Fetch lifecycle

The fetcher behaves like this:

1. try cache load
2. if `manualOnly` and a cached entry exists, use cache
3. if cached entry is still fresh, use cache
4. otherwise send a conditional request if validators exist
5. on `304`, refresh metadata and reuse the body
6. on `2xx`, replace cache with the new response
7. on error with cached data and stale-on-error enabled, return stale cache
8. otherwise return an error

## Corrupt cache handling

If metadata or body files are inconsistent, the store treats the entry as corrupt and deletes it. The next successful fetch recreates the entry.

## Refresh endpoint

Use refresh when you want to force revalidation or a fresh network read:

```bash
curl -s 'http://127.0.0.1:8765/v1/refresh?config=/work/demo/.cli.json' | jq
```

Refresh sets `ForceRefresh=true`, which skips the normal fresh-cache shortcut.

## Operational nuance

- stale fallback is enabled by the catalog builder for source fetches
- `manualOnly` only prevents network traffic when a cached entry already exists
- cache files are not compressed
- metadata and body are written atomically but separately
