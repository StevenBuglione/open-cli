---
title: Discovery and Catalog Overview
---

# Discovery and Catalog Overview

Discovery is how `oasclird` finds API descriptions. Catalog building is how it turns those descriptions into stable tool metadata for `oascli`.

## The pipeline

```text
config sources
  -> discovery (apiCatalog / serviceRoot / openapi)
  -> OpenAPI load
  -> overlay application
  -> skill manifest load
  -> workflow load
  -> normalized services + tools + workflows
  -> effective views
```

## Source types

The current implementation supports three source types:

- **`openapi`**: load a document directly from a local path, `file://` URI, or HTTP(S) URL
- **`serviceRoot`**: discover OpenAPI and metadata URLs from RFC 8631 `Link` headers
- **`apiCatalog`**: walk an RFC 9727 API catalog and expand discovered service roots

## What the normalized catalog contains

A built catalog includes:

- source provenance and fetch history
- named services and aliases
- normalized tools
- workflows
- effective views (`discover` plus one per profile)
- a source fingerprint

## Why normalization exists

Normalization gives the CLI a stable shape even when the original OpenAPI documents vary wildly.

Examples of what gets normalized:

- tool IDs (`serviceId:operationId`)
- command names
- groups
- safety hints
- auth requirements
- request body schema metadata
- guidance from skill manifests

## Referenced vs unreferenced sources

If a source is referenced by `services.<id>.source`, the service config controls aliasing and extra metadata.

If a source is **not** referenced by a service, the catalog builder still processes it directly when it is enabled. In that case, service IDs are derived automatically from the document.

## Caching and provenance

Remote discovery and document fetches go through the cache layer. Provenance records show:

- where the source came from
- when it was fetched
- request method used (`HEAD`, `GET`, etc.)
- cache outcome
- status code and validators such as `ETag`

For deeper details, continue with:

- [API catalog discovery](./api-catalog-discovery)
- [Service discovery and overlays](./service-discovery-and-overlays)
- [Normalized tool catalog](./normalized-tool-catalog)
