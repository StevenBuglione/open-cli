# OAS CLI Spec Completion Design

## Goal

Complete the remaining admitted gaps in `spec/ocli-spec.md` while also fixing any directly adjacent spec shortfalls exposed in the same codepaths. The primary targets are HTTP caching and revalidation, structured observability and tracing hooks, and a published conformance compatibility matrix.

## Scope

This design covers the three existing repositories:

- `open-cli` for executable behavior, runtime wiring, and tests
- `oas-cli-spec` for normative documentation, schemas, and examples
- `oas-cli-conformance` for compatibility publication, conformance artifacts, and CI validation

The work is intentionally bounded to the discovery, catalog-loading, remote-reference, runtime-request, refresh, and conformance-publication paths. If implementation reveals additional gaps outside those paths, they are out of scope unless they block correctness of the targeted work.

## Recommended Approach

Use a targeted completion pass with an adjacent-gap sweep:

1. Add real cache and observability infrastructure in `open-cli`.
2. Wire those subsystems through the fetch and runtime paths that the spec already defines.
3. Update `oas-cli-spec` only where external behavior, provenance, or examples need to match the implemented contract.
4. Publish and validate a compatibility matrix in `oas-cli-conformance`.

This keeps the work bounded while avoiding another partial implementation that leaves nearby contract gaps unresolved.

## Architecture

### Repository Responsibilities

- `open-cli` remains the reference implementation.
  - Add `pkg/cache` for persistent HTTP response caching, metadata tracking, and conditional revalidation.
  - Add `pkg/obs` for structured logging plus optional tracing hooks behind a narrow interface.
  - Route discovery, remote catalog input, overlay fetches, OpenAPI reference loading, runtime requests, and refresh behavior through these shared facilities.
- `oas-cli-spec` remains the normative contract.
  - Clarify cache and stale behavior where implementation makes it concrete.
  - Add or adjust schemas/examples only if externally visible outputs change.
- `oas-cli-conformance` remains the proof layer.
  - Publish a versioned compatibility matrix.
  - Extend CI and tests so the matrix and any externally visible behavior changes are validated in standalone environments.

### Subsystem Boundaries

The cache and observability packages are infrastructure layers, not business logic. Fetching code should depend on compact interfaces that answer:

- how a remote resource is retrieved under a cache policy
- what cache outcome and provenance were produced
- what structured event or tracing span should be emitted

This keeps discovery, catalog building, and runtime request handling readable and testable.

## Data Flow

### HTTP Caching and Revalidation

All supported remote fetches should flow through one cache-aware client path.

For each request:

1. The caller supplies URL, cache policy, and a stable cache key.
2. `pkg/cache` checks persistent metadata and cached content.
3. The result is classified as one of:
   - fresh hit
   - stale hit
   - revalidated hit
   - miss
4. If revalidation is needed, conditional headers such as `If-None-Match` and `If-Modified-Since` are sent.
5. On `304 Not Modified`, metadata is updated without replacing the cached body.
6. On `200 OK`, the cache entry is replaced with the new body and metadata.
7. On network failure, stale cached content may be served only when the policy allows it, and that result must be marked stale in provenance and observability output.

Each cache entry should retain:

- canonical source URL
- response body
- content type when available
- `ETag`
- `Last-Modified`
- parsed cache-control directives that affect freshness
- fetch time
- freshness deadline or stale marker
- last validation result
- provenance fields needed by the caller

### Runtime Refresh

The existing refresh path should become the manual trigger for bypassing or revalidating relevant cached resources. Refresh operations must report whether resources:

- were refreshed from origin
- were revalidated unchanged
- fell back to stale cache
- failed

Discovery documents, overlays, and remote OpenAPI references should all use the same refresh and cache behavior so the system remains internally consistent.

### Observability

Operational observability is distinct from audit:

- audit remains the compliance trail
- observability provides structured operational insight

Major request and fetch lifecycles should emit structured events with stable fields such as:

- service or discovery target
- operation identifier
- source URL or cache key
- cache outcome
- policy decision
- duration
- HTTP status
- error category
- request or operation correlation identifier

Tracing should remain optional and should wrap the same major flows:

- runtime request handling
- discovery fetch and parse steps
- catalog build steps
- refresh execution

The default configuration should stay lightweight with a no-op tracer path available when tracing is not enabled.

## Error Handling

The implementation should distinguish at least these failure classes:

- transport failure
- cache corruption
- cache policy denial
- schema or parse failure
- runtime execution failure

Cache corruption should fail closed by discarding invalid cache entries and refetching when online. If offline or origin-unavailable fallback is permitted, the system may serve stale content, but it must surface that state in provenance, structured events, and user-visible refresh results where applicable.

Conditional revalidation failures must not be reported as fresh hits. Refresh responses should expose enough detail for operators and tests to distinguish successful refresh, unchanged revalidation, stale fallback, and hard failure.

## Verification Strategy

Verification must be test-first and layered.

### `open-cli`

- unit tests for cache metadata parsing and persistence
- unit tests for freshness and stale policy decisions
- unit tests for conditional request header generation
- unit tests for `304` update behavior
- unit tests for stale fallback behavior
- unit tests for structured event emission and tracing hook invocation
- integration tests for discovery, remote reference loading, and refresh behavior using local HTTP fixtures

### `oas-cli-spec`

- schema and example validation for any externally visible output changes

### `oas-cli-conformance`

- tests for any externally visible normalized catalog or runtime behavior changes
- publication and consistency checks for the compatibility matrix
- standalone CI validation so the conformance repo does not rely on implicit sibling checkouts

Final verification should include the full existing repo verification commands and a live end-to-end conformance run against the reference runtime before completion is claimed.

## Deliverables

- persistent cache implementation with conditional revalidation support
- runtime and fetch-path wiring for manual refresh and stale handling
- structured observability hooks with optional tracing integration points
- compatibility matrix publication in `oas-cli-conformance`
- updated spec text, schemas, examples, and conformance artifacts as required by the implemented contract
- automated tests that prove the above behavior

## Acceptance Criteria

- remote discovery and reference fetches use a shared cache-aware path
- `ETag` and related validators are persisted and used for revalidation
- offline or origin-failing stale fallback is explicit and policy-controlled
- refresh behavior exercises the same cache and provenance model
- structured events are emitted for major runtime and fetch lifecycles
- tracing can be enabled without changing business logic call sites
- a published compatibility matrix exists and is validated by CI
- local verification and end-to-end conformance pass after the implementation
