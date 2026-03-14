---
title: API Catalog Discovery
---

# API Catalog Discovery

`apiCatalog` sources implement the repository's RFC 9727-style discovery path.

## Config shape

```json
{
  "sources": {
    "publisher": {
      "type": "apiCatalog",
      "uri": "https://publisher.example.com/.well-known/api-catalog",
      "enabled": true
    }
  }
}
```

## Fetch behavior

For an API catalog source, the discovery package:

1. sends `GET` to the configured URL
2. sets `Accept: application/linkset+json`
3. parses a JSON linkset document
4. follows `rel="api-catalog"` links recursively
5. records `rel="item"` links as discovered services

The API catalog reader accepts either top-level:

- `linkset`
- `links`

arrays in the returned JSON.

## Example linkset

```json
{
  "linkset": [
    {
      "href": "https://publisher.example.com/support",
      "rel": "item"
    },
    {
      "href": "https://publisher.example.com/partner-catalog",
      "rel": "api-catalog"
    }
  ]
}
```

## What happens next

Each discovered `item` is treated as a **service root**, not as an OpenAPI document directly. That means the runtime then runs the RFC 8631 service discovery flow against the discovered URL.

## Recursion and cycles

The current implementation keeps track of visited catalogs and detects simple cycles.

Important nuance:

- cycles are treated as warnings internally, not hard failures
- those warnings are not currently surfaced through `oascli` or the runtime HTTP API

## Duplicate services

Discovered service URLs are de-duplicated before they are expanded.

## Operational guidance

Use `apiCatalog` when you want one source of truth for many services. Use `serviceRoot` or `openapi` when you want tighter control over each API.

See [Service discovery and overlays](./service-discovery-and-overlays) for the next stage in the chain.
