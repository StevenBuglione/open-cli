# Fleet Validation Gap Closure Implementation Plan

> **For agentic workers:** REQUIRED: Use superpowers:subagent-driven-development (if subagents available) or superpowers:executing-plans to implement this plan. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Close the remaining proof, coverage, and information-architecture gaps in the fleet-validation program so the branch can truthfully claim that fleet coverage is implemented, fully tested, committed, pushed, and ready to merge to `main`.

**Architecture:** Keep the current capability-matrix architecture, but tighten it in three directions: make the matrix and campaign lanes honest about what they prove, deepen coverage with failure-path and evidence-rich lanes, and turn the website/onboarding workstream from “scaffolded docs” into a real evaluation path with visible enterprise proof. This document is intentionally standalone because the earlier 2026-03-17 fleet spec/plan artifacts are not present in this worktree and therefore cannot be relied on as the execution source of truth.

**Tech Stack:** Go product tests, Python fleet runner/summarizer, YAML matrix files, Docusaurus docs, GitHub Actions, Docker Compose, MCP test servers, Authentik/Entra live proof references

---

## Current verified baseline

These items are already implemented on `feature/copilot-fleet-validation` and should be treated as the baseline to preserve while closing gaps:

- fleet rubric metadata is implemented in `product-tests/tests/helpers/findings.go`
- matrix loaders and validators exist in `product-tests/tests/helpers/fleet_matrix.go`
- reproducible fleet matrix exists in `product-tests/testdata/fleet/capability-matrix.yaml`
- live proof matrix exists in `product-tests/testdata/fleet/live-proof-matrix.yaml`
- website review rubric exists in `product-tests/testdata/fleet/website-review-rubric.yaml`
- reproducible lanes currently run through:
  - `TestCampaignLocalDaemonMatrix`
  - `TestCampaignRemoteRuntimeMatrix`
  - `TestCampaignMCPStdioMatrix`
  - `TestCampaignMCPRemoteMatrix`
  - `TestCampaignAgentOperator`
- runner/summarizer/Make/CI wiring exists and currently passes
- docs build passes

## Hard truths from the audits

These are the issues this plan must close.

### Product proof gaps

1. **False green in `mcp-remote-core`**
   - File: `product-tests/testdata/fleet/capability-matrix.yaml`
   - File: `product-tests/tests/campaign_mcp_remote_matrix_test.go`
   - Problem: lane metadata says `authPattern: transport-oauth`, but the test opens an unauthenticated remote MCP endpoint and never proves transport OAuth.
   - Required outcome: either implement real transport OAuth proof or relabel the lane honestly.

2. **Remote runtime lane is too shallow**
   - File: `product-tests/tests/campaign_remote_runtime_matrix_test.go`
   - Problem: proves one client-credentials token path, one allowed tool, one denied tool, and a `200` on browser metadata; does not prove payload semantics, browser flow readiness, token lifecycle, or richer auth failure handling.
   - Required outcome: strengthen assertions and add failure-path companion lanes.

3. **Local daemon lane is not a real daemon proof**
   - File: `product-tests/tests/campaign_local_daemon_matrix_test.go`
   - Problem: uses in-process test runtime helpers; does not prove real `oasclird` process lifecycle, restart behavior, or actual CLI/runtime attach behavior.
   - Required outcome: add at least one process-backed daemon fleet lane.

4. **Remote API lane overclaims**
   - File: `product-tests/tests/campaign_agent_operator_test.go`
   - Problem: proves happy-path CRUD and “audit count > 0,” but not auth failures, bad responses, schema-level correctness, or per-call audit integrity.
   - Required outcome: add failure-path operator campaigns and stronger audit assertions.

5. **MCP stdio lane is too shallow**
   - File: `product-tests/tests/campaign_mcp_stdio_matrix_test.go`
   - Problem: lists tools and executes `list_directory` once, but does not verify result semantics or error behavior in a meaningful way.
   - Required outcome: add richer assertions and an explicit failure case.

6. **Artifacts are harness-shaped, not evidence-rich**
   - File: `product-tests/scripts/run-agent-campaign.py`
   - Problem: every lane gets `transcript.log` and `rubric.json`, but the contents are often too sparse for post-hoc proof. There are no normalized snapshots of audit details, config fragments, request/response bodies, or proof payloads.
   - Required outcome: enrich artifacts or reduce claims.

7. **Selected lanes can still go green without proving anything**
   - File: `product-tests/tests/campaign_mcp_remote_matrix_test.go`
   - File: `product-tests/scripts/run-agent-campaign.py`
   - Problem: a selected lane can skip or emit zero meaningful criteria and still leave behind a rubric that looks green enough for the runner summary.
   - Required outcome: matrix execution must fail closed when a selected lane is skipped, emits zero criteria, emits zero rubrics, or emits multiple rubrics.

8. **Failure-path coverage is still missing**
   - Files: current campaign tests + `campaign_known_gaps_test.go`
   - Problem: important negative cases are either absent or parked as “known gaps.”
   - Required outcome: convert the highest-value gaps into executable fleet lanes or honest blocked items with owners.

### Website / information architecture gaps

9. **Docs still contradict runtime auth reality**
   - File: `website/docs/runtime/deployment-models.md`
   - File: `website/docs/runtime/overview.md`
   - Problem: docs still say the runtime has no built-in auth / bury the proof path behind a repo-internal example reference, which conflicts with the implemented brokered runtime-auth story.
   - Required outcome: align all runtime docs with the current auth model.

10. **Enterprise proof is still hidden**
   - File: `website/sidebars.ts`
   - File: `website/docs/runtime/authentik-reference.md`
   - File: `website/docs/development/fleet-validation.md`
   - Problem: the proof story exists, but it is still too buried in navigation and does not yet provide a clear evidence-first enterprise route.
   - Required outcome: create a visible enterprise-evaluation path in the docs IA.

11. **Website review workstream is defined but not executable**
    - File: `product-tests/testdata/fleet/website-review-rubric.yaml`
    - Problem: rubric exists, but there is no executable campaign or validation target using it.
    - Required outcome: create a website-review campaign with artifact output.

12. **Onboarding bridge is still incomplete**
    - File: `website/src/pages/index.tsx`
    - File: `website/docs/getting-started/intro.md`
    - Problem: first-time users still do not get a clean path from quickstart to daemon/auth/MCP/enterprise choices.
    - Required outcome: add explicit bridge pages and visible cross-links.

### Repository / process gaps

13. **Earlier fleet spec and implementation plan are missing from this worktree**
    - Problem: the original planning artifacts referenced in session notes are not present here, which makes historical validation harder.
    - Required outcome: treat this document as the canonical gap tracker, or restore the earlier spec/plan documents into the branch if they are meant to be part of the permanent repo history.

## File map for gap closure work

### Existing files that almost certainly need modification

- `product-tests/testdata/fleet/capability-matrix.yaml`
- `product-tests/testdata/fleet/live-proof-matrix.yaml`
- `product-tests/testdata/fleet/website-review-rubric.yaml`
- `product-tests/tests/campaign_local_daemon_matrix_test.go`
- `product-tests/tests/campaign_remote_runtime_matrix_test.go`
- `product-tests/tests/campaign_mcp_stdio_matrix_test.go`
- `product-tests/tests/campaign_mcp_remote_matrix_test.go`
- `product-tests/tests/campaign_agent_operator_test.go`
- `product-tests/tests/campaign_known_gaps_test.go`
- `product-tests/scripts/run-agent-campaign.py`
- `product-tests/scripts/summarize-findings.py`
- `product-tests/Makefile`
- `.github/workflows/ci.yml`
- `website/sidebars.ts`
- `website/src/pages/index.tsx`
- `website/docs/getting-started/intro.md`
- `website/docs/development/fleet-validation.md`
- `website/docs/development/testing.md`
- `website/docs/runtime/deployment-models.md`
- `website/docs/runtime/overview.md`
- `website/docs/runtime/authentik-reference.md`

### Files that should probably be created

- `product-tests/tests/campaign_website_review_test.go`
- `product-tests/tests/campaign_remote_runtime_failures_test.go`
- `product-tests/tests/campaign_remote_api_failures_test.go`
- `product-tests/tests/campaign_local_daemon_process_test.go`
- `product-tests/tests/testdata/website/` fixtures if needed
- `website/docs/getting-started/choose-your-path.md`
- `website/docs/runtime/enterprise-readiness.md`
- `website/docs/runtime/fleet-proof-artifacts.md` or equivalent if a dedicated proof-artifact page is cleaner than overloading `fleet-validation.md`

## Non-negotiable acceptance criteria

This plan is complete only when all of the following are true:

- the capability matrix makes no false claims about auth patterns or proof depth
- selected lanes cannot pass when skipped or when they emit zero proof criteria
- every green fleet lane has assertions that match its wording
- fleet artifact contracts are enforced and preserved
- the highest-value failure cases are represented by executable campaign lanes
- the website review rubric has an executable campaign consumer
- the docs have an explicit enterprise-evaluation path
- runtime docs no longer contradict the implemented auth story
- `make verify`
- `make product-test-fleet`
- `cd product-tests && make fleet-matrix-mcp-remote`
- any new website-review target
- `cd website && npm run build`
- all changes are committed on the feature branch
- the feature branch is pushed
- the branch is merged to `main`

## Chunk 1: Make the current fleet matrix honest

### Task 1: Resolve the `mcp-remote-core` false green

**Files:**
- Modify: `product-tests/testdata/fleet/capability-matrix.yaml`
- Modify: `product-tests/tests/campaign_mcp_remote_matrix_test.go`
- Modify: `product-tests/scripts/run-agent-campaign.py`
- Test: `product-tests/tests/campaign_mcp_remote_matrix_test.go`
- Verify: `cd product-tests && make fleet-matrix-mcp-remote`

- [ ] **Step 1: Decide whether to implement transport OAuth or relabel the lane**

Decision rule:

- If the repo already has a reusable MCP remote auth fixture, implement real auth proof.
- If not, relabel the lane honestly now and create a follow-up live-proof or future lane for transport OAuth.

- [ ] **Step 2: Write or update the failing test first**

If relabeling:

```go
func TestCapabilityMatrixMCPRemoteLaneIsHonest(t *testing.T) {
    matrix, err := helpers.LoadCapabilityMatrix(filepath.Join(repoRoot(t), "product-tests", "testdata", "fleet", "capability-matrix.yaml"))
    if err != nil {
        t.Fatalf("load matrix: %v", err)
    }
    lane := findLane(t, matrix, "mcp-remote-core")
    if lane.AuthPattern != "none" {
        t.Fatalf("expected honest auth pattern for current remote MCP lane, got %q", lane.AuthPattern)
    }
}
```

If implementing transport OAuth, write a test that proves an unauthenticated request fails and an authenticated request succeeds.

- [ ] **Step 3: Run the focused test to verify it fails**

Run:

```bash
cd /home/sbuglione/oascli/oas-cli-go/.worktrees/copilot-fleet-validation
go test ./product-tests/tests/... -run TestCapabilityMatrixMCPRemoteLaneIsHonest -count=1
```

Expected: FAIL until the lane metadata and/or implementation is corrected.

- [ ] **Step 4: Implement the minimal honest fix**

Preferred immediate fix if no auth fixture exists:

- change `authPattern: transport-oauth` to `authPattern: none`
- update test/rubric wording in `campaign_mcp_remote_matrix_test.go` so it describes what is actually proved
- update the runner to fail closed when:
  - a selected lane is skipped
  - a selected lane emits zero criteria
  - a selected lane emits zero rubrics
  - a selected lane emits multiple rubrics

- [ ] **Step 5: Re-run the remote MCP lane**

Run:

```bash
cd product-tests
make fleet-matrix-mcp-remote
```

Expected:

- lane passes
- rubric language no longer implies transport OAuth proof

- [ ] **Step 6: Commit**

```bash
git add product-tests/testdata/fleet/capability-matrix.yaml \
        product-tests/tests/campaign_mcp_remote_matrix_test.go \
        product-tests/scripts/run-agent-campaign.py
git commit -m "fix: make remote MCP fleet proof honest"
```

### Task 2: Tighten the remote runtime lane so the criteria match the assertions

**Files:**
- Modify: `product-tests/tests/campaign_remote_runtime_matrix_test.go`
- Test: `product-tests/tests/campaign_remote_runtime_matrix_test.go`

- [ ] **Step 1: Write failing checks for payload semantics**

Add assertions for:

- exactly which tool ID is returned in the filtered catalog
- response body contents for the authorized tool call
- response shape or fields from `/v1/auth/browser-config`, not just `200`

- [ ] **Step 2: Run the focused test**

Run:

```bash
go test ./product-tests/tests -run ^TestCampaignRemoteRuntimeMatrix$ -count=1 -v
```

Expected: FAIL until the new assertions are implemented.

- [ ] **Step 3: Implement the minimal stronger checks**

Examples:

```go
tool, _ := tools[0].(map[string]any)
fr.Check("catalog-tool-id", "catalog exposes the expected authorized tool", "tickets:listTickets", fmt.Sprintf("%v", tool["id"]), tool["id"] == "tickets:listTickets", "")
```

and decode the authorized execution response body instead of checking status only.

- [ ] **Step 4: Add at least one explicit auth failure companion test**

Create a dedicated failure-path campaign or subtest for:

- missing bearer token
- wrong scope
- expired token

Prefer a separate campaign file if it becomes longer than one focused scenario.

- [ ] **Step 5: Re-run the lane**

Run:

```bash
go test ./product-tests/tests -run 'TestCampaignRemoteRuntimeMatrix|TestCampaignRemoteRuntimeFailures' -count=1 -v
```

- [ ] **Step 6: Commit**

```bash
git add product-tests/tests/campaign_remote_runtime_matrix_test.go \
        product-tests/tests/campaign_remote_runtime_failures_test.go
git commit -m "test: deepen remote runtime fleet coverage"
```

### Task 3: Strengthen the remote API lane and audit proof

**Files:**
- Modify: `product-tests/tests/campaign_agent_operator_test.go`
- Create: `product-tests/tests/campaign_remote_api_failures_test.go`
- Test: `product-tests/tests/campaign_agent_operator_test.go`

- [x] **Step 1: Write failing tests for stronger audit proof**

Add checks for:

- audit count equals or exceeds the exact executed calls expected
- denied/error calls produce audit entries
- at least one audit entry contains the expected tool identifier

- [x] **Step 2: Run the focused tests**

Run:

```bash
go test ./product-tests/tests -run 'TestCampaignAgentOperator|TestCampaignRemoteAPIFailures' -count=1 -v
```

- [x] **Step 3: Implement the minimal stronger audit inspection**

Use existing helper access to the audit file and inspect actual event payloads rather than count only.

- [x] **Step 4: Add at least one failure-path campaign**

Scenarios:

- invalid path argument returns expected error code
- upstream 500 is surfaced
- non-JSON upstream response is handled explicitly

- [x] **Step 5: Re-run**

Run:

```bash
go test ./product-tests/tests -run 'TestCampaignAgentOperator|TestCampaignAgentOperatorWithAuth|TestCampaignRemoteAPIFailures' -count=1 -v
```

- [ ] **Step 6: Commit**

```bash
git add product-tests/tests/campaign_agent_operator_test.go \
        product-tests/tests/campaign_remote_api_failures_test.go
git commit -m "test: add failure and audit proof to remote API fleet lanes"
```

## Chunk 2: Add missing proof depth

### Task 4: Add a real process-backed local daemon lane

**Files:**
- Create: `product-tests/tests/campaign_local_daemon_process_test.go`
- Modify: `product-tests/testdata/fleet/capability-matrix.yaml`
- Test: `cmd/oasclird`, `cmd/oascli`, product tests

- [x] **Step 1: Write the failing process-backed campaign**

The test should:

1. start a real `oasclird` process against a temp config
2. invoke `oascli` against it with `--runtime`
3. prove at least one attach/use path
4. stop the daemon cleanly

- [x] **Step 2: Run it to verify it fails**

Run:

```bash
go test ./product-tests/tests -run ^TestCampaignLocalDaemonProcess$ -count=1 -v
```

- [x] **Step 3: Implement minimal process orchestration**

Use `os/exec` with temp dirs and explicit cleanup. Keep it small and deterministic.

- [x] **Step 4: Update the matrix if needed**

Either:

- replace the existing local-daemon lane, or
- add a new `local-daemon-process` lane and keep the current in-process lane as a lower-level lifecycle proof

- [x] **Step 5: Re-run the local daemon fleet coverage**

Run:

```bash
go test ./product-tests/tests -run 'TestCampaignLocalDaemon(Matrix|Process)' -count=1 -v
```

- [ ] **Step 6: Commit**

```bash
git add product-tests/tests/campaign_local_daemon_process_test.go \
        product-tests/testdata/fleet/capability-matrix.yaml
git commit -m "test: add process-backed local daemon fleet lane"
```

### Task 5: Convert the highest-value known gaps into real fleet coverage

**Files:**
- Modify: `product-tests/tests/campaign_known_gaps_test.go`
- Create: focused campaign files as needed

- [x] **Step 1: Prioritize the first wave**

Convert these first:

- non-JSON upstream response handling
- pagination/query forwarding
- invalid/expired/revoked token path
- concurrency result-isolation

- [x] **Step 2: Write one failing focused test per promoted gap**

Do not batch too much into one campaign. Keep each scenario explicit.

- [x] **Step 3: Run each focused test and verify it fails**

Example:

```bash
go test ./product-tests/tests -run ^TestCampaignRemoteAPINonJSONResponse$ -count=1 -v
```

- [x] **Step 4: Implement only the minimal product change or honest rubric change needed**

Important:

- if the product truly lacks the feature, decide whether to implement it or keep it as a blocked gap
- do not mark a scenario “passing” by weakening the wording without stakeholder approval

- [x] **Step 5: Update matrix / docs to reflect new lanes**

Revocation remains explicitly tracked as a known auth gap; Task 5 promoted invalid and expired token handling into executable fleet coverage without falsely claiming revocation support.

- [ ] **Step 6: Commit**

```bash
git add product-tests/tests/... product-tests/testdata/fleet/capability-matrix.yaml
git commit -m "test: promote key known gaps into fleet coverage"
```

### Task 6: Make artifacts useful as evidence

**Files:**
- Modify: `product-tests/scripts/run-agent-campaign.py`
- Modify: `product-tests/scripts/summarize-findings.py`
- Modify: `product-tests/testdata/fleet/capability-matrix.yaml`
- Modify: `product-tests/tests/helpers/findings.go` if artifact path preservation needs recorder changes
- Modify: `.github/workflows/ci.yml`
- Modify: campaign tests that should emit richer notes

- [x] **Step 1: Write a failing regression test for artifact richness**

Examples:

- ensure each lane can emit a stable artifact list beyond just transcript/rubric when applicable
- ensure summarizer surfaces artifact paths clearly
- ensure `expectedArtifacts` from the matrix is validated rather than ignored
- ensure runner-added metadata does not overwrite test-recorded artifact paths

- [x] **Step 2: Decide the minimal first-wave evidence set**

At least one of:

- audit summary artifact
- catalog snapshot artifact
- browser-config snapshot artifact
- denied-response artifact
- website-review result artifact once that workstream exists

- [x] **Step 3: Implement minimal artifact capture**

Do this only for lanes where the evidence materially improves the truthfulness of the result.

- preserve `FindingsRecorder.AddArtifactPath()` output
- compare actual artifacts against `expectedArtifacts`
- upload fleet artifacts from CI so proof survives after the run

- [x] **Step 4: Re-run the fleet lanes**

Run:

```bash
make product-test-fleet
cd product-tests && make fleet-matrix-mcp-remote
```

- [ ] **Step 5: Commit**

```bash
git add product-tests/scripts/run-agent-campaign.py \
        product-tests/scripts/summarize-findings.py \
        product-tests/testdata/fleet/capability-matrix.yaml \
        product-tests/tests/... \
        .github/workflows/ci.yml
git commit -m "feat: enforce and publish fleet evidence artifacts"
```

## Chunk 3: Turn the website workstream into a real evaluation path

### Task 7: Fix runtime-doc contradictions and stale links

**Files:**
- Modify: `website/docs/runtime/deployment-models.md`
- Modify: `website/docs/runtime/overview.md`
- Modify: `website/docs/security/overview.md` if cross-links need adjustment

- [ ] **Step 1: Write down the exact contradictions to remove**

Current contradictions:

- “no built-in auth” wording in `deployment-models.md`
- stale proof path in `runtime/overview.md` (`examples/runtime-auth-broker/reference/`)

- [ ] **Step 2: Patch docs to match the implemented runtime auth model**

Requirements:

- distinguish default localhost safety from optional runtime auth clearly
- point to `runtime/authentik-reference`
- explain that remote runtime auth exists and can be enabled server-side

- [ ] **Step 3: Build docs**

Run:

```bash
cd website && npm run build
```

- [ ] **Step 4: Commit**

```bash
git add website/docs/runtime/deployment-models.md \
        website/docs/runtime/overview.md \
        website/docs/security/overview.md
git commit -m "docs: align runtime docs with auth implementation"
```

### Task 8: Add the missing bridge and enterprise pages

**Files:**
- Create: `website/docs/getting-started/choose-your-path.md`
- Create: `website/docs/runtime/enterprise-readiness.md`
- Modify: `website/sidebars.ts`
- Modify: `website/docs/getting-started/intro.md`
- Modify: `website/docs/development/fleet-validation.md`
- Modify: `website/docs/runtime/overview.md`
- Modify: `website/docs/runtime/deployment-models.md`
- Modify: `website/src/pages/index.tsx`

- [ ] **Step 1: Write the bridge pages**

`choose-your-path.md` should route readers to:

- quickstart / embedded mode
- local daemon mode
- remote runtime / auth
- MCP integrations
- enterprise evaluation

`enterprise-readiness.md` should gather:

- deployment models
- security/auth
- Authentik reference proof
- fleet validation
- audit / operations pages

`fleet-validation.md` should also show at least one concrete sample:

- example `rubric.json`
- example `transcript.log`
- visible link to the Authentik evidence checklist or enterprise-readiness page

- [ ] **Step 2: Wire the pages into the IA**

Update sidebars so enterprise proof is not buried under Development.

Possible target structure:

- Runtime
  - deployment models
  - enterprise readiness
  - Authentik reference proof
- Development
  - testing
  - fleet validation (implementation view)

- [ ] **Step 3: Update homepage and intro links**

Make the role-based path visible from:

- homepage CTA/quick links
- intro persona section

- [ ] **Step 4: Build docs**

Run:

```bash
cd website && npm run build
```

- [ ] **Step 5: Commit**

```bash
git add website/docs/getting-started/choose-your-path.md \
        website/docs/runtime/enterprise-readiness.md \
        website/docs/development/fleet-validation.md \
        website/docs/runtime/overview.md \
        website/docs/runtime/deployment-models.md \
        website/sidebars.ts \
        website/docs/getting-started/intro.md \
        website/src/pages/index.tsx
git commit -m "docs: add onboarding and enterprise bridge pages"
```

### Task 9: Make the website review rubric executable

**Files:**
- Create: `product-tests/tests/campaign_website_review_test.go`
- Modify: `product-tests/testdata/fleet/website-review-rubric.yaml`
- Modify: `product-tests/Makefile`
- Modify: `.github/workflows/ci.yml`

- [ ] **Step 1: Write the failing website-review campaign**

The campaign should:

- read the website review rubric
- inspect a bounded set of expected links / pages / section presence
- emit a rubric artifact just like product fleet lanes

Keep it honest: it can verify IA/link/path structure before trying to verify content quality.

- [ ] **Step 2: Run it and confirm failure**

Run:

```bash
go test ./product-tests/tests -run ^TestCampaignWebsiteReview$ -count=1 -v
```

- [ ] **Step 3: Implement the minimal campaign**

Good first-wave checks:

- homepage links to quickstart and enterprise path
- intro links to choose-your-path and enterprise-readiness
- runtime sidebar contains the enterprise proof path
- fleet-validation page links to concrete proof surfaces

- [ ] **Step 4: Add a Make target and CI job**

Examples:

- `make product-test-website-review`
- dedicated CI job or inclusion in the reproducible fleet if the output shape fits

- [ ] **Step 5: Re-run**

Run:

```bash
go test ./product-tests/tests -run ^TestCampaignWebsiteReview$ -count=1 -v
cd website && npm run build
```

- [ ] **Step 6: Commit**

```bash
git add product-tests/tests/campaign_website_review_test.go \
        product-tests/testdata/fleet/website-review-rubric.yaml \
        product-tests/Makefile \
        .github/workflows/ci.yml
git commit -m "test: add executable website review campaign"
```

## Chunk 4: Final closure, push, and merge

### Task 10: Restore or replace missing planning artifacts

**Files:**
- Modify or create whichever is appropriate:
  - `docs/superpowers/specs/2026-03-17-copilot-fleet-validation-and-website-program-design.md`
  - `docs/superpowers/plans/2026-03-17-copilot-fleet-validation-and-website-program.md`
  - or keep this file as the official source of truth and note that explicitly

- [ ] **Step 1: Decide whether to restore the original files**

If they were meant to ship in the repo, restore them. If not, add a short note in this document and the session plan saying this file supersedes them for execution tracking.

- [ ] **Step 2: Commit if needed**

```bash
git add docs/superpowers/specs/... docs/superpowers/plans/...
git commit -m "docs: restore fleet planning artifacts"
```

### Task 11: Run the full closure verification

**Files:**
- No new files required

- [ ] **Step 1: Run baseline verification**

```bash
cd /home/sbuglione/oascli/oas-cli-go/.worktrees/copilot-fleet-validation
make verify
```

- [ ] **Step 2: Run fleet verification**

```bash
make product-test-fleet
cd product-tests && make fleet-matrix-mcp-remote
```

- [ ] **Step 3: Run website review verification**

```bash
go test ./product-tests/tests -run ^TestCampaignWebsiteReview$ -count=1 -v
cd website && npm run build
```

- [ ] **Step 4: Inspect artifact output**

Check:

- `/tmp/oascli-fleet/**/rubric.json`
- `/tmp/oascli-fleet/**/transcript.log`
- any new evidence artifacts

- [ ] **Step 5: Record what still remains**

If anything still cannot be proven automatically, add it explicitly to:

- `product-tests/testdata/fleet/live-proof-matrix.yaml`
- the docs pages that describe enterprise proof status

- [ ] **Step 6: Commit final verification changes**

```bash
git add .
git commit -m "chore: finalize fleet validation gap closure"
```

### Task 12: Push and merge

**Files:**
- No file changes required unless merge conflict resolution is needed

- [ ] **Step 1: Push the branch**

```bash
git push origin feature/copilot-fleet-validation
```

- [ ] **Step 2: Open or update the PR**

Use the PR description to summarize:

- proof gaps closed
- remaining manual/live-proof items
- exact verification commands run

- [ ] **Step 3: Merge to main**

Preferred:

```bash
gh pr merge --merge
```

Or use the repo’s preferred merge strategy.

- [ ] **Step 4: Verify main**

```bash
git checkout main
git pull
make verify
```

- [ ] **Step 5: Push main if needed**

```bash
git push origin main
```

## Suggested execution order

Use this order unless a dependency forces a different one:

1. Task 1
2. Task 2
3. Task 3
4. Task 4
5. Task 5
6. Task 6
7. Task 7
8. Task 8
9. Task 9
10. Task 10
11. Task 11
12. Task 12

## Definition of done

This gap-closure document can stop being the active tracker only when:

- every item above is checked off, or explicitly marked as intentionally deferred
- fleet lane wording matches actual proof
- website onboarding and enterprise evaluation paths are discoverable without insider knowledge
- the branch is merged to `main`
- the merged `main` branch is verified

Plan complete and saved to `docs/superpowers/plans/2026-03-17-fleet-validation-gap-closure.md`. Ready to execute?
