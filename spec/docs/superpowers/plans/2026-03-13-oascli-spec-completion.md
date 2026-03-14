# OAS CLI Spec Completion Implementation Plan

> **For agentic workers:** REQUIRED: Use superpowers:subagent-driven-development (if subagents available) or superpowers:executing-plans to implement this plan. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Fully complete the remaining `spec/oascli-spec.md` gaps by adding shared HTTP caching and refresh behavior, structured observability hooks, and a published conformance compatibility matrix, while fixing adjacent spec shortfalls exposed in the same execution paths.

**Architecture:** Add cache and observability as shared infrastructure in `oas-cli-go`, then wire those packages through discovery, OpenAPI loading, catalog build, and runtime refresh flows so one fetch path governs provenance and revalidation. Update `oas-cli-spec` only where the public contract changes, and extend `oas-cli-conformance` so the compatibility matrix and any visible output changes are validated in standalone CI.

**Tech Stack:** Go (`net/http`, Cobra, kin-openapi), Python 3.12 (`jsonschema`, `PyYAML`), JSON Schema 2020-12, GitHub Actions

---

## File Structure

### `oas-cli-go`

- Create: `pkg/cache/types.go`
  - shared cache result, metadata, and policy types
- Create: `pkg/cache/store.go`
  - persistent on-disk metadata and body storage
- Create: `pkg/cache/http.go`
  - cache-aware HTTP fetcher with conditional revalidation
- Create: `pkg/cache/cache_test.go`
  - unit tests for expiry, revalidation, stale fallback, and corruption recovery
- Create: `pkg/obs/obs.go`
  - structured event logger and optional tracer interfaces with a no-op implementation
- Create: `pkg/obs/obs_test.go`
  - event capture and tracing hook tests
- Modify: `pkg/openapi/load.go`
  - route remote reads through the cache-aware fetcher
- Modify: `pkg/discovery/types.go`
  - expand provenance types with cache and fetch detail fields
- Modify: `pkg/discovery/api_catalog.go`
  - use shared fetcher and record per-fetch provenance
- Modify: `pkg/discovery/service.go`
  - use shared fetcher for HEAD/GET and record cache outcomes
- Modify: `pkg/discovery/discovery_test.go`
  - cover conditional requests, stale behavior, and provenance shape
- Modify: `pkg/catalog/build.go`
  - construct and pass shared fetch/cache context through discovery and OpenAPI loading
- Modify: `pkg/catalog/types.go`
  - expose any newly required provenance fields in the NTC only if they are part of the public contract
- Modify: `pkg/catalog/catalog_test.go`
  - validate updated catalog provenance and stable outputs
- Modify: `pkg/catalog/discovery_integration_test.go`
  - integration coverage for cached discovery and refresh-sensitive behavior
- Modify: `internal/runtime/server.go`
  - add cache/obs wiring, real `/v1/refresh`, and structured lifecycle events
- Modify: `internal/runtime/server_test.go`
  - refresh endpoint, stale fallback, and observability coverage
- Modify: `cmd/oasclird/main.go`
  - plumb cache and observability options into the runtime entrypoint if needed by the final design

### `oas-cli-spec`

- Modify: `spec/core.md`
  - add normative cache, refresh, provenance, and observability language
- Modify: `spec/config.md`
  - clarify `refresh` policy semantics and any new configuration knobs
- Modify: `schemas/ntc.schema.json`
  - reflect any externally visible provenance additions
- Create: `schemas/compatibility-matrix.schema.json`
  - machine-readable schema for the published conformance compatibility matrix
- Modify: `examples/ntc.json`
  - keep example aligned with the public NTC contract
- Create: `examples/compatibility-matrix.json`
  - example matrix document validated in CI
- Modify: `README.md`
  - document the new schema and validation scope

### `oas-cli-conformance`

- Create: `compatibility-matrix.json`
  - published compatibility data consumed by tests/CI
- Create: `COMPATIBILITY.md`
  - human-readable published matrix for releases
- Modify: `README.md`
  - document compatibility artifacts and validation behavior
- Modify: `scripts/run_conformance.py`
  - validate the compatibility matrix in addition to fixture and NTC checks
- Modify: `tests/test_run_conformance.py`
  - add runner tests for matrix validation and failure modes
- Modify: `expected/tickets.ntc.json`
  - update only if the public NTC contract changes
- Modify: `.github/workflows/ci.yml`
  - ensure standalone CI validates schemas, fixtures, and compatibility artifacts together

## Chunk 1: Setup and Baseline Verification

### Task 1: Prepare isolated repos and capture the clean baseline

**Files:**
- Modify if needed: `/home/sbuglione/oascli/oas-cli-go/.gitignore`
- Modify if needed: `/home/sbuglione/oascli/oas-cli-spec/.gitignore`
- Modify if needed: `/home/sbuglione/oascli/oas-cli-conformance/.gitignore`

- [ ] **Step 1: Create isolated worktrees for each repo before code changes**

Use `superpowers:using-git-worktrees`.

Recommended branch names:

```bash
oas-cli-go: feature/spec-completion
oas-cli-spec: feature/spec-completion
oas-cli-conformance: feature/spec-completion
```

- [ ] **Step 2: Run baseline verification in each repo**

Run:

```bash
git -C /path/to/oas-cli-go status --short --branch
git -C /path/to/oas-cli-go make verify
git -C /path/to/oas-cli-spec status --short --branch
cd /path/to/oas-cli-spec && python3 scripts/validate_examples.py
git -C /path/to/oas-cli-conformance status --short --branch
cd /path/to/oas-cli-conformance && python3 scripts/run_conformance.py --schema-root /path/to/oas-cli-spec/schemas
```

Expected:

- all three repos start from a known clean baseline
- existing verification passes before new work begins

- [ ] **Step 3: Commit any repo-local `.gitignore` worktree setup if the skill requires it**

Run:

```bash
git add .gitignore
git commit -m "chore: ignore local worktrees"
```

Expected:

- no repo accidentally tracks worktree contents

## Chunk 2: Shared Cache Infrastructure in `oas-cli-go`

### Task 2: Write failing tests for cache semantics first

**Files:**
- Create: `/home/sbuglione/oascli/oas-cli-go/pkg/cache/cache_test.go`

- [ ] **Step 1: Write the failing cache tests**

Add tests that cover:

```go
func TestFetcherReturnsFreshHitWithoutNetwork(t *testing.T) {}
func TestFetcherSendsIfNoneMatchAndUpdatesValidatedAtOn304(t *testing.T) {}
func TestFetcherFallsBackToStaleWhenOriginFailsAndPolicyAllows(t *testing.T) {}
func TestFetcherDropsCorruptEntriesAndRefetches(t *testing.T) {}
```

- [ ] **Step 2: Run the new package tests to verify they fail**

Run:

```bash
cd /path/to/oas-cli-go && go test ./pkg/cache -run TestFetcher -v
```

Expected:

- FAIL because `pkg/cache` does not exist yet

### Task 3: Implement the minimal cache package

**Files:**
- Create: `/home/sbuglione/oascli/oas-cli-go/pkg/cache/types.go`
- Create: `/home/sbuglione/oascli/oas-cli-go/pkg/cache/store.go`
- Create: `/home/sbuglione/oascli/oas-cli-go/pkg/cache/http.go`
- Test: `/home/sbuglione/oascli/oas-cli-go/pkg/cache/cache_test.go`

- [ ] **Step 1: Define the cache types**

Add concrete types for:

```go
type Policy struct {
    MaxAge time.Duration
    ManualOnly bool
    AllowStaleOnError bool
    ForceRefresh bool
}

type Metadata struct {
    Key string
    URL string
    ETag string
    LastModified string
    CachedAt time.Time
    ExpiresAt time.Time
    Stale bool
    LastStatus int
}

type Result struct {
    Body []byte
    Metadata Metadata
    Outcome string
}
```

- [ ] **Step 2: Implement persistent storage**

Persist metadata and response bodies under a deterministic cache directory keyed by request identity. Include safe read, write, delete, and prune behavior.

- [ ] **Step 3: Implement the HTTP fetcher**

Implement a cache-aware fetch path that:

- returns fresh cached bodies when valid
- performs conditional revalidation using `If-None-Match` and `If-Modified-Since`
- updates metadata on `304 Not Modified`
- replaces entries on `200 OK`
- serves stale content on transport failure only when policy allows

- [ ] **Step 4: Run the cache tests**

Run:

```bash
cd /path/to/oas-cli-go && go test ./pkg/cache -v
```

Expected:

- PASS

- [ ] **Step 5: Commit**

Run:

```bash
git -C /path/to/oas-cli-go add pkg/cache
git -C /path/to/oas-cli-go commit -m "feat: add shared HTTP cache"
```

## Chunk 3: Wire Cache Through Discovery and Catalog Build

### Task 4: Add failing integration tests for cached discovery and OpenAPI loading

**Files:**
- Modify: `/home/sbuglione/oascli/oas-cli-go/pkg/discovery/discovery_test.go`
- Modify: `/home/sbuglione/oascli/oas-cli-go/pkg/catalog/discovery_integration_test.go`
- Modify: `/home/sbuglione/oascli/oas-cli-go/pkg/catalog/catalog_test.go`

- [ ] **Step 1: Write the failing discovery and catalog tests**

Add tests for:

```go
func TestDiscoverAPICatalogRecordsCacheAwareFetchProvenance(t *testing.T) {}
func TestDiscoverServiceRootRevalidatesHeadOrGetResponses(t *testing.T) {}
func TestBuildReusesCachedRemoteOpenAPIAndMarksStaleFallback(t *testing.T) {}
```

- [ ] **Step 2: Run only the targeted tests and verify they fail**

Run:

```bash
cd /path/to/oas-cli-go && go test ./pkg/discovery ./pkg/catalog -run 'TestDiscover|TestBuildReusesCached' -v
```

Expected:

- FAIL because discovery and catalog code are not cache-aware yet

### Task 5: Pass cache context through discovery, OpenAPI loading, and the NTC

**Files:**
- Modify: `/home/sbuglione/oascli/oas-cli-go/pkg/openapi/load.go`
- Modify: `/home/sbuglione/oascli/oas-cli-go/pkg/discovery/types.go`
- Modify: `/home/sbuglione/oascli/oas-cli-go/pkg/discovery/api_catalog.go`
- Modify: `/home/sbuglione/oascli/oas-cli-go/pkg/discovery/service.go`
- Modify: `/home/sbuglione/oascli/oas-cli-go/pkg/catalog/build.go`
- Modify: `/home/sbuglione/oascli/oas-cli-go/pkg/catalog/types.go`
- Test: `/home/sbuglione/oascli/oas-cli-go/pkg/discovery/discovery_test.go`
- Test: `/home/sbuglione/oascli/oas-cli-go/pkg/catalog/discovery_integration_test.go`
- Test: `/home/sbuglione/oascli/oas-cli-go/pkg/catalog/catalog_test.go`

- [ ] **Step 1: Introduce a shared remote-read dependency**

Refactor these call sites so remote HTTP access can flow through the new cache package instead of direct `http.DefaultClient` reads.

- [ ] **Step 2: Expand provenance types**

Record at least URL, method, timestamp, cache outcome, and cache validator fields for fetched discovery documents. Keep the NTC stable unless the spec requires the new fields to be externally visible.

- [ ] **Step 3: Update OpenAPI and metadata loading**

Ensure:

- remote OpenAPI documents use the cache-aware fetcher
- remote skill manifests and workflows use the same fetch path
- discovery fetches reuse the same store and policy interpretation

- [ ] **Step 4: Run the targeted package tests**

Run:

```bash
cd /path/to/oas-cli-go && go test ./pkg/discovery ./pkg/catalog ./pkg/openapi -v
```

Expected:

- PASS

- [ ] **Step 5: Commit**

Run:

```bash
git -C /path/to/oas-cli-go add pkg/openapi/load.go pkg/discovery pkg/catalog
git -C /path/to/oas-cli-go commit -m "feat: wire cache through discovery and catalog build"
```

## Chunk 4: Observability and Runtime Refresh

### Task 6: Add failing tests for runtime refresh and observability hooks

**Files:**
- Create: `/home/sbuglione/oascli/oas-cli-go/pkg/obs/obs_test.go`
- Modify: `/home/sbuglione/oascli/oas-cli-go/internal/runtime/server_test.go`

- [ ] **Step 1: Write the failing tests**

Add tests that assert:

```go
func TestObserverCapturesStructuredEventFields(t *testing.T) {}
func TestServerRefreshEndpointRevalidatesCachedSources(t *testing.T) {}
func TestServerRefreshEndpointReportsStaleFallback(t *testing.T) {}
```

- [ ] **Step 2: Run the targeted tests and verify they fail**

Run:

```bash
cd /path/to/oas-cli-go && go test ./pkg/obs ./internal/runtime -run 'TestObserver|TestServerRefresh' -v
```

Expected:

- FAIL because the observer package and refresh implementation do not exist yet

### Task 7: Implement `pkg/obs` and wire it through the runtime

**Files:**
- Create: `/home/sbuglione/oascli/oas-cli-go/pkg/obs/obs.go`
- Create: `/home/sbuglione/oascli/oas-cli-go/pkg/obs/obs_test.go`
- Modify: `/home/sbuglione/oascli/oas-cli-go/internal/runtime/server.go`
- Modify: `/home/sbuglione/oascli/oas-cli-go/internal/runtime/server_test.go`
- Modify if needed: `/home/sbuglione/oascli/oas-cli-go/cmd/oasclird/main.go`

- [ ] **Step 1: Add a narrow observer interface**

Use a shape like:

```go
type Event struct {
    Name string
    Service string
    Operation string
    URL string
    CacheOutcome string
    StatusCode int
    Duration time.Duration
    ErrorCategory string
    RequestID string
}

type Observer interface {
    Emit(context.Context, Event)
    StartSpan(context.Context, string, map[string]string) (context.Context, func(error))
}
```

- [ ] **Step 2: Add a no-op default implementation and a test recorder**

The runtime should be able to run without any tracing backend configured.

- [ ] **Step 3: Implement `/v1/refresh`**

Use the same cache-aware catalog loading path to:

- force revalidation or refresh of configured remote sources
- return structured per-source results
- emit audit and observer events that differentiate refresh success, unchanged revalidation, stale fallback, and failure

- [ ] **Step 4: Emit structured events across request execution**

Instrument:

- effective catalog loads
- tool execution requests
- workflow execution requests
- refresh requests

- [ ] **Step 5: Run the runtime and observer tests**

Run:

```bash
cd /path/to/oas-cli-go && go test ./pkg/obs ./internal/runtime -v
```

Expected:

- PASS

- [ ] **Step 6: Run full Go verification**

Run:

```bash
cd /path/to/oas-cli-go && make verify
```

Expected:

- PASS

- [ ] **Step 7: Commit**

Run:

```bash
git -C /path/to/oas-cli-go add pkg/obs internal/runtime cmd/oasclird
git -C /path/to/oas-cli-go commit -m "feat: add runtime refresh and observability hooks"
```

## Chunk 5: Update the Published Spec Contract

### Task 8: Add failing validation inputs for new public artifacts

**Files:**
- Create: `/home/sbuglione/oascli/oas-cli-spec/schemas/compatibility-matrix.schema.json`
- Create: `/home/sbuglione/oascli/oas-cli-spec/examples/compatibility-matrix.json`
- Modify if needed: `/home/sbuglione/oascli/oas-cli-spec/examples/ntc.json`

- [ ] **Step 1: Draft the new schema/example pair**

The compatibility example should encode:

- suite version
- spec version or revision
- implementation repo and release or commit
- feature bucket statuses
- notes or known exceptions

- [ ] **Step 2: Run example validation and verify it fails before docs/schema updates are complete**

Run:

```bash
cd /path/to/oas-cli-spec && python3 scripts/validate_examples.py
```

Expected:

- FAIL until the validation script and schema set are updated consistently

### Task 9: Update the spec, schemas, and examples

**Files:**
- Modify: `/home/sbuglione/oascli/oas-cli-spec/spec/core.md`
- Modify: `/home/sbuglione/oascli/oas-cli-spec/spec/config.md`
- Modify: `/home/sbuglione/oascli/oas-cli-spec/schemas/ntc.schema.json`
- Create: `/home/sbuglione/oascli/oas-cli-spec/schemas/compatibility-matrix.schema.json`
- Modify: `/home/sbuglione/oascli/oas-cli-spec/examples/ntc.json`
- Create: `/home/sbuglione/oascli/oas-cli-spec/examples/compatibility-matrix.json`
- Modify: `/home/sbuglione/oascli/oas-cli-spec/README.md`

- [ ] **Step 1: Update the normative text**

Add explicit language for:

- per-fetch provenance and cache metadata
- manual refresh semantics
- stale offline behavior
- structured logs and optional tracing hooks
- published compatibility matrix expectations

- [ ] **Step 2: Update public schemas and examples**

Keep example documents and schemas synchronized with the actual contract. If the NTC does not need new external fields, leave its public shape stable and only add the compatibility matrix schema/example.

- [ ] **Step 3: Run validation**

Run:

```bash
cd /path/to/oas-cli-spec && python3 scripts/validate_examples.py
```

Expected:

- PASS

- [ ] **Step 4: Commit**

Run:

```bash
git -C /path/to/oas-cli-spec add spec README.md schemas examples
git -C /path/to/oas-cli-spec commit -m "docs: publish cache and compatibility spec updates"
```

## Chunk 6: Publish and Validate the Compatibility Matrix in `oas-cli-conformance`

### Task 10: Add failing conformance tests for the compatibility matrix

**Files:**
- Modify: `/home/sbuglione/oascli/oas-cli-conformance/tests/test_run_conformance.py`

- [ ] **Step 1: Add tests for matrix validation**

Add tests that assert:

```python
def test_validate_compatibility_matrix_passes(tmp_path): ...
def test_validate_compatibility_matrix_fails_when_required_fields_missing(tmp_path): ...
def test_readme_mentions_compatibility_matrix(): ...
```

- [ ] **Step 2: Run the targeted tests and verify they fail**

Run:

```bash
cd /path/to/oas-cli-conformance && python3 -m unittest tests.test_run_conformance -v
```

Expected:

- FAIL because the matrix and validation code do not exist yet

### Task 11: Publish the matrix and extend the runner

**Files:**
- Create: `/home/sbuglione/oascli/oas-cli-conformance/compatibility-matrix.json`
- Create: `/home/sbuglione/oascli/oas-cli-conformance/COMPATIBILITY.md`
- Modify: `/home/sbuglione/oascli/oas-cli-conformance/README.md`
- Modify: `/home/sbuglione/oascli/oas-cli-conformance/scripts/run_conformance.py`
- Modify: `/home/sbuglione/oascli/oas-cli-conformance/tests/test_run_conformance.py`
- Modify if needed: `/home/sbuglione/oascli/oas-cli-conformance/expected/tickets.ntc.json`
- Modify if needed: `/home/sbuglione/oascli/oas-cli-conformance/.github/workflows/ci.yml`

- [ ] **Step 1: Publish the machine-readable and human-readable matrix**

Keep the matrix versioned and explicit about supported feature buckets:

- cache and revalidation
- refresh endpoint
- observability hooks
- conformance fixture coverage

- [ ] **Step 2: Extend `run_conformance.py`**

Validate:

- fixture shapes
- expected NTC schema
- compatibility matrix schema
- README or documentation linkage to the published matrix

- [ ] **Step 3: Run conformance tests and fixture validation**

Run:

```bash
cd /path/to/oas-cli-conformance && python3 -m unittest tests.test_run_conformance -v
cd /path/to/oas-cli-conformance && python3 scripts/run_conformance.py --schema-root /path/to/oas-cli-spec/schemas
```

Expected:

- PASS

- [ ] **Step 4: Commit**

Run:

```bash
git -C /path/to/oas-cli-conformance add README.md COMPATIBILITY.md compatibility-matrix.json scripts tests expected .github/workflows/ci.yml
git -C /path/to/oas-cli-conformance commit -m "docs: publish conformance compatibility matrix"
```

## Chunk 7: End-to-End Verification, Push, and Post-Push Checks

### Task 12: Run the complete verification suite after all code and docs land

**Files:**
- Verify only: all modified files across the three repos

- [ ] **Step 1: Re-run the full repo verification commands**

Run:

```bash
cd /path/to/oas-cli-go && make verify
cd /path/to/oas-cli-spec && python3 scripts/validate_examples.py
cd /path/to/oas-cli-conformance && python3 -m unittest tests.test_run_conformance -v
cd /path/to/oas-cli-conformance && python3 scripts/run_conformance.py --schema-root /path/to/oas-cli-spec/schemas
```

Expected:

- PASS across all three repos

- [ ] **Step 2: Run live end-to-end conformance against the runtime**

Run the reference runtime and compare a generated catalog against the conformance expected output.

Example:

```bash
cd /path/to/oas-cli-go && go build ./cmd/oasclird ./cmd/oascli
# start oasclird in a background shell
cd /path/to/oas-cli-conformance && python3 scripts/run_conformance.py --schema-root /path/to/oas-cli-spec/schemas --candidate /tmp/generated.ntc.json
```

Expected:

- generated candidate matches expected output
- refresh and cache behavior has direct test coverage even if not fully visible in the candidate artifact

- [ ] **Step 3: Push each repo branch and monitor GitHub Actions**

Run:

```bash
git -C /path/to/oas-cli-go push -u origin <branch>
git -C /path/to/oas-cli-spec push -u origin <branch>
git -C /path/to/oas-cli-conformance push -u origin <branch>
```

Expected:

- all remote CI workflows pass

- [ ] **Step 4: Merge or fast-forward to `main` only after CI passes**

Use non-interactive git commands. Do not amend or rewrite user-owned history.

