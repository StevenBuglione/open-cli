---
title: Fleet Validation
---

# Fleet Validation

**Read this whether you are an operator evaluating production-readiness or a contributor adding capability proof.** For operators: the fleet matrix is the executable evidence that the runtime works the way the docs describe. For contributors: the matrix is how new capability becomes part of the baseline record.

`open-cli` now has two complementary validation layers for product-style proof:

- a **reproducible fleet matrix** for CI-safe lanes we can execute in ephemeral environments
- a **live proof matrix** for enterprise-only flows that require real external identity or remote runtime infrastructure

This split keeps baseline verification honest. We do not pretend CI can fully simulate browser federation, real Entra tenancy, or externally managed runtimes.

## Reproducible fleet matrix

The executable source of truth lives in:

- `product-tests/testdata/fleet/capability-matrix.yaml`

Current CI-safe lanes cover:

- hosted runtime deployment and multi-instance behavior
- remote runtime authorization filtering
- runtime auth failure paths such as missing, expired, and tampered bearer tokens
- MCP stdio execution
- remote API operator workflows
- remote API failure, pagination, concurrency-isolation, and non-JSON response handling

Run the default reproducible fleet locally from the repo root:

```bash
make product-test-fleet
```

Or from inside `product-tests/`:

```bash
make fleet-matrix-ci
```

Artifacts are written under `/tmp/ocli-fleet/` by default. Each lane gets its own directory with:

- `transcript.log`
- `rubric.json`
- additional evidence files when the lane needs them, such as `browser-config.json` or `forbidden-response.json`

Example `rubric.json` excerpt:

```json
{
  "campaign": "remote-runtime-matrix",
  "capability": "remote-runtime",
  "artifactPaths": [
    "browser-config.json",
    "rubric.json",
    "transcript.log"
  ],
  "pass": true
}
```

Example `transcript.log` excerpt:

```text
[run-agent-campaign] lane='remote-runtime-oauth-client' pattern='^TestCampaignRemoteRuntimeMatrix$' timeout=120s
[run-agent-campaign] wrote rubric → /tmp/ocli-fleet/remote-runtime-oauth-client/rubric.json
```

If you are reviewing enterprise-oriented proof rather than implementation mechanics, continue to [Enterprise readiness](../runtime/enterprise-readiness) or the [Authentik reference proof](../runtime/authentik-reference).

## Remote MCP lane

The remote MCP lane is separate because it needs a real streamable-HTTP MCP server:

```bash
make product-test-fleet-mcp-remote
```

That target starts the containerized MCP remote fixture, runs the lane, summarizes the rubric, and tears the service down.

## Live proof matrix

The enterprise-only source of truth lives in:

- `product-tests/testdata/fleet/live-proof-matrix.yaml`

Use this matrix for flows that need real external systems, such as:

- Authentik brokered to Microsoft Entra for browser login
- externally hosted remote runtime proofs

Those lanes should produce operator evidence against the referenced checklist instead of pretending they are baseline CI checks.

## How to add a new lane

1. Add a lane entry to `product-tests/testdata/fleet/capability-matrix.yaml`.
2. Point `goTestPattern` at one rubric-emitting `TestCampaign...` function.
3. Keep the lane executable in an ephemeral environment unless it truly needs a live proof.
4. If the lane needs external infrastructure, add it to `live-proof-matrix.yaml` instead and attach an evidence checklist.

## Why this matters

This program is designed to test the product the way operators actually use it:

- multiple runtime attachment modes
- real auth patterns
- real MCP transports
- real remote APIs
- artifact-backed campaign summaries that are readable by both engineers and reviewers

**For operators**: rubric artifacts give you machine-readable and human-readable proof that the runtime passed the relevant capability lane. They are the evidence behind [Enterprise readiness](../runtime/enterprise-readiness) claims.

**For contributors**: every new capability should have a corresponding lane or be tracked as an explicit live-only proof in `live-proof-matrix.yaml`. A capability that only passes unit tests is not a fully verified lane.

## What rubric artifacts prove and what they do not

Rubric artifacts prove that a capability lane passed under the conditions of that run. They do not prove:

- that a revoked token will be rejected (revocation is a tracked gap with no proof lane)
- that live external systems (Entra, real Authentik tenants) are configured and working (live proof lanes cover these — rubric artifacts from CI do not)
- that audit log rotation, network isolation, or SIEM integration work (these are operator-owned; no CI lane covers them)

If you are building an evidence package for an enterprise review, capture both the CI rubric artifacts (from `make product-test-fleet`) and operator-run live proof artifacts (from `live-proof-matrix.yaml`). CI artifacts alone are not a complete evidence package for a remote runtime deployment.

For the evaluator-facing path through those artifacts, see [Enterprise readiness](../runtime/enterprise-readiness).
