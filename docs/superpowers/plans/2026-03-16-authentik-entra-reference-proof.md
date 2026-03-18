# Authentik Entra Reference Proof Implementation Plan

> **For agentic workers:** REQUIRED: Use superpowers:subagent-driven-development (if subagents available) or superpowers:executing-plans to implement this plan. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add one official Authentik-based reference deployment, document Entra federation into it, and verify both workload (`oauthClient`) and human (`browserLogin`) proof paths against the brokered runtime auth contract.

**Architecture:** Keep the runtime contract broker-neutral while making Authentik the single reference broker. Split execution into an automated Authentik workload-proof chunk that runs inside repo-managed test infrastructure, and a human-proof chunk that documents and verifies the Entra-federated browser flow with a reproducible runbook and captured evidence format.

**Tech Stack:** Go, existing `ocli`/`oclird` runtime auth stack, Docker Compose, Authentik, Microsoft Entra ID, Docusaurus docs, Go product tests

---

## File structure

### Reference deployment assets

- Create: `examples/runtime-auth-broker/authentik/compose.yaml`
  - Runs Authentik plus any minimal supporting services needed for local broker verification.
- Create: `examples/runtime-auth-broker/authentik/.env.example`
  - Documents required ports, secrets, issuer URLs, and non-secret environment knobs.
- Create: `examples/runtime-auth-broker/authentik/runtime.cli.json.tmpl`
  - Example runtime config wired to Authentik JWKS/browser/token endpoints.
- Create: `examples/runtime-auth-broker/authentik/README.md`
  - Local setup instructions for the Authentik reference broker.
- Create: `examples/runtime-auth-broker/authentik/entra-federation.md`
  - Human-run federation steps for Entra -> Authentik.
- Create: `examples/runtime-auth-broker/authentik/evidence-checklist.md`
  - Exact evidence operators must capture before claiming enterprise proof.

### Product-test harness

- Create: `product-tests/authentik/docker-compose.yml`
  - Dedicated compose stack for the Authentik reference proof.
- Create: `product-tests/authentik/.env.example`
  - Authentik test stack variables.
- Create: `product-tests/scripts/authentik-up.sh`
  - Thin wrapper around the Authentik compose stack.
- Create: `product-tests/scripts/authentik-down.sh`
  - Thin wrapper teardown.
- Modify: `product-tests/scripts/check-prereqs.sh`
  - Ensure prerequisites are sufficient for Authentik-based proof.
- Modify: `product-tests/Makefile`
  - Add Authentik-specific targets.
- Modify: `product-tests/README.md`
  - Document how to run the Authentik proof targets.
- Create: `product-tests/tests/helpers/authentik_runtime.go`
  - Shared helper for generating configs, waiting for Authentik readiness, and wiring runtime URLs.
- Create: `product-tests/tests/capability_runtime_auth_authentik_test.go`
  - Automated Authentik workload proof and fail-closed cases.

### Runtime/browser verification docs

- Modify: `website/docs/runtime/deployment-models.md`
  - Add reference deployment guidance.
- Modify: `website/docs/security/overview.md`
  - Clarify broker-neutral contract vs Authentik reference proof.
- Create: `website/docs/runtime/authentik-reference.md`
  - User-facing guide for the official reference deployment.
- Modify: `README.md`
  - Point to the Authentik reference proof and Entra federation docs.

---

## Chunk 1: Authentik reference broker and automated workload proof

### Task 1: Scaffold the Authentik reference deployment assets

**Files:**
- Create: `examples/runtime-auth-broker/authentik/compose.yaml`
- Create: `examples/runtime-auth-broker/authentik/.env.example`
- Create: `examples/runtime-auth-broker/authentik/runtime.cli.json.tmpl`
- Create: `examples/runtime-auth-broker/authentik/README.md`

- [ ] **Step 1: Copy the existing broker example shape into the new reference directory**

Read:
- `examples/runtime-auth-broker/reference/README.md`
- `examples/runtime-auth-broker/reference/runtime.cli.json`
- `examples/runtime-auth-broker/reference/broker-notes.md`

Expected: clear mapping of the existing contract example into an Authentik-specific reference folder.

- [ ] **Step 2: Write the failing deployment smoke contract in the README**

Add a “success looks like” section that requires:

```md
- Authentik serves discovery/JWKS/browser/token endpoints
- oclird validates Authentik-issued runtime tokens with oidc_jwks
- ocli oauthClient can acquire a runtime token and list the filtered catalog
- runtime info reports auth.required=true and tokenValidationProfiles including oidc_jwks
```

Expected: the README states concrete outcomes before any implementation wiring.

- [ ] **Step 3: Create minimal compose scaffolding**

Start with:

```yaml
services:
  authentik:
    image: ghcr.io/goauthentik/server:latest
```

and only add the minimum companion services Authentik actually requires.

Expected: `docker compose -f examples/runtime-auth-broker/authentik/compose.yaml config --quiet` passes.

- [ ] **Step 4: Add the runtime config template**

Template must include:

```json
"runtime": {
  "server": {
    "auth": {
      "validationProfile": "oidc_jwks"
    }
  }
}
```

plus placeholder values for issuer, JWKS URL, token URL, authorization URL, and browser client ID.

- [ ] **Step 5: Verify the compose and config examples**

Run:

```bash
docker compose -f examples/runtime-auth-broker/authentik/compose.yaml config --quiet
```

Expected: no output, exit code `0`.

- [ ] **Step 6: Document the endpoint inventory and readiness expectations**

The README must explicitly name the endpoints the stack is expected to expose:

```md
- /.well-known/openid-configuration
- /jwks.json (or Authentik-discovery equivalent)
- browser authorization endpoint
- token endpoint
```

Expected: operators know exactly what must be reachable before the runtime proof starts.

- [ ] **Step 7: Commit**

```bash
git add examples/runtime-auth-broker/authentik
git commit -m "docs: scaffold Authentik reference deployment"
```

### Task 2: Extend the product-test harness for Authentik

**Files:**
- Create: `product-tests/authentik/docker-compose.yml`
- Create: `product-tests/authentik/.env.example`
- Create: `product-tests/scripts/authentik-up.sh`
- Create: `product-tests/scripts/authentik-down.sh`
- Modify: `product-tests/scripts/check-prereqs.sh`
- Modify: `product-tests/Makefile`
- Modify: `product-tests/README.md`

- [ ] **Step 1: Write the failing harness target expectations in `product-tests/README.md`**

Add target names before implementing them:

```md
- make authentik-up
- make authentik-down
- make test-runtime-auth-authentik
```

- [ ] **Step 2: Add failing Make targets**

Add:

```make
authentik-up:
	@bash scripts/authentik-up.sh
```

and matching down/test targets.

Expected: the new targets exist but fail until scripts/compose files are added.

- [ ] **Step 3: Create thin wrapper scripts**

Use the same style as existing wrappers:

```bash
docker compose -f authentik/docker-compose.yml up -d
```

and

```bash
docker compose -f authentik/docker-compose.yml down --remove-orphans
```

- [ ] **Step 4: Update prerequisite checks**

Extend `product-tests/scripts/check-prereqs.sh` only if Authentik introduces a real extra requirement not already covered.

Expected: no speculative dependencies.

- [ ] **Step 5: Verify harness config wiring**

Run:

```bash
cd product-tests
make check-prereqs
docker compose -f authentik/docker-compose.yml config --quiet
```

Expected: both commands pass.

- [ ] **Step 6: Commit**

```bash
git add product-tests/authentik product-tests/scripts product-tests/Makefile product-tests/README.md
git commit -m "test: add Authentik product-test harness"
```

### Task 3: Add automated Authentik workload-proof coverage

**Files:**
- Create: `product-tests/tests/helpers/authentik_runtime.go`
- Create: `product-tests/tests/capability_runtime_auth_authentik_test.go`

- [ ] **Step 1: Write the failing product test first**

Cover these cases explicitly:

```go
func TestCapabilityRuntimeAuthAuthentikOAuthClient(t *testing.T)
func TestCapabilityRuntimeAuthAuthentikRejectsInsufficientScope(t *testing.T)
func TestCapabilityRuntimeAuthAuthentikRejectsWrongAudience(t *testing.T)
```

- [ ] **Step 2: Run the focused test to verify it fails**

Run:

```bash
go test ./product-tests/tests -run TestCapabilityRuntimeAuthAuthentik -count=1 -v
```

Expected: FAIL because helper/setup code does not exist yet.

- [ ] **Step 3: Implement the minimal helper**

`product-tests/tests/helpers/authentik_runtime.go` should do exactly these things:

- wait for Authentik readiness
- confirm discovery/JWKS and token endpoints are reachable before tests continue
- write a runtime config from `runtime.cli.json.tmpl`
- create or load the workload client credentials required by the test
- return the runtime URL, config path, and required env vars

- [ ] **Step 4: Implement the test to prove allow/deny behavior**

Assertions must verify:

- `GET /v1/runtime/info` reports the expected auth metadata for the Authentik deployment
- `catalog list` only returns authorized tools
- allowed tool execution succeeds
- unauthorized tool execution fails closed
- wrong audience, insufficient scope, expired token, or bad issuer is rejected

- [ ] **Step 5: Run the focused test to verify it passes**

Run:

```bash
go test ./product-tests/tests -run TestCapabilityRuntimeAuthAuthentik -count=1 -v -timeout 180s
```

Expected: PASS.

- [ ] **Step 6: Run the wider verification slice**

Run:

```bash
go test ./product-tests/tests -run 'TestCapabilityRuntimeAuthBroker|TestCapabilityRuntimeAuthAuthentik' -count=1 -v -timeout 180s
```

Expected: PASS.

- [ ] **Step 7: Commit**

```bash
git add product-tests/tests/helpers/authentik_runtime.go product-tests/tests/capability_runtime_auth_authentik_test.go
git commit -m "test: verify Authentik workload runtime auth"
```

## Chunk 2: Entra-federated human proof and user-facing docs

### Task 4: Document the Entra federation runbook

**Files:**
- Create: `examples/runtime-auth-broker/authentik/entra-federation.md`
- Create: `examples/runtime-auth-broker/authentik/evidence-checklist.md`

- [ ] **Step 1: Write the runbook skeleton with explicit inputs**

Require these operator-supplied values:

```md
- Entra tenant ID
- Entra client/application ID
- Entra redirect URI(s)
- Entra group/claim values used for runtime-scope mapping
- Authentik issuer URL
- runtime audience
- test user identity
```

- [ ] **Step 2: Document the concrete Entra -> Authentik setup procedure**

The runbook must include the specific operator actions to reproduce federation:

```md
1. create/register the Entra application
2. configure redirect URIs and required scopes/claims
3. configure Authentik upstream/provider settings for Entra
4. map Entra claims/groups into normalized runtime scopes
5. configure the Authentik application/provider that ocli will actually use
```

Expected: an operator can reproduce federation without reverse-engineering the example.

- [ ] **Step 3: Document the browser-login trust chain**

Include the exact sequence:

```md
1. ocli reads /v1/runtime/info
2. ocli reads /v1/auth/browser-config
3. browser redirects to Authentik
4. Authentik federates to Entra
5. Authentik issues runtime token
6. oclird validates token and enforces scopes
```

- [ ] **Step 4: Add the evidence checklist**

Require captured evidence for:

- successful browser sign-in
- filtered catalog output
- allowed tool execution
- denied tool execution
- runtime info/auth metadata
- claim-to-scope mapping used for the proof
- standardized artifact names/locations so multiple operators capture the same set

- [ ] **Step 5: Review the runbook for broker-neutral wording**

Expected: Authentik is described as the reference deployment, not a normative requirement.

- [ ] **Step 6: Commit**

```bash
git add examples/runtime-auth-broker/authentik/entra-federation.md examples/runtime-auth-broker/authentik/evidence-checklist.md
git commit -m "docs: add Entra federation runbook"
```

### Task 5: Add user-facing docs for the official reference proof

**Files:**
- Create: `website/docs/runtime/authentik-reference.md`
- Modify: `website/docs/runtime/deployment-models.md`
- Modify: `website/docs/security/overview.md`
- Modify: `README.md`

- [ ] **Step 1: Write the failing docs inventory**

List the claims each doc must cover before editing:

```md
- broker-neutral contract
- Authentik is the reference proof
- Entra is the upstream federation example
- browserLogin and oauthClient are both required proof paths
```

- [ ] **Step 2: Add the dedicated Authentik reference page**

The page should link to:

- local Authentik setup
- Entra federation runbook
- evidence checklist
- runtime config template

- [ ] **Step 3: Update overview docs**

Make sure overview docs do **not** imply:

- Authentik is mandatory
- Entra-specific behavior is normative
- only one of the two proof paths matters

- [ ] **Step 4: Build the docs site**

Run:

```bash
cd website
npm run build
```

Expected: `[SUCCESS] Generated static files in "build".`

- [ ] **Step 5: Commit**

```bash
git add README.md website/docs/runtime/authentik-reference.md website/docs/runtime/deployment-models.md website/docs/security/overview.md
git commit -m "docs: publish Authentik reference proof guide"
```

### Task 6: Run the final proof-verification stack and capture handoff notes

**Files:**
- Modify: `product-tests/README.md`
- Modify: `examples/runtime-auth-broker/authentik/README.md`

- [ ] **Step 1: Add the exact verification commands**

Document these commands exactly:

```bash
cd product-tests
make authentik-up
make test-runtime-auth-authentik
make authentik-down

cd ../website
npm run build
```

- [ ] **Step 2: Add the manual human-proof checklist**

Reference `examples/runtime-auth-broker/authentik/evidence-checklist.md` and document the exact manual flow the operator must run after the automated workload proof:

```md
- perform browserLogin against Authentik
- confirm Entra redirect/federation succeeds
- save runtime info output
- save filtered catalog output
- save allowed/denied execution outputs
```

- [ ] **Step 3: Execute the Entra-federated browser proof and capture evidence**

Run the documented browser flow against the real Authentik + Entra deployment and capture all artifacts required by `evidence-checklist.md`.

Expected:

```md
- browserLogin completes successfully
- runtime token is accepted by oclird
- filtered catalog output is captured
- allowed execution output is captured
- denied execution output is captured
```

If the required Entra tenant, application, or test identity is unavailable, stop execution here and mark the task blocked rather than claiming enterprise proof.

- [ ] **Step 4: Run the automated verification stack**

Run:

```bash
cd product-tests
make authentik-up
go test ./tests -run TestCapabilityRuntimeAuthAuthentik -count=1 -v -timeout 180s
make authentik-down
```

Expected: PASS.

- [ ] **Step 5: Run repo-wide regression checks for touched areas**

Run:

```bash
go test ./cmd/ocli ./internal/runtime ./product-tests/tests -count=1
```

Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add product-tests/README.md examples/runtime-auth-broker/authentik/README.md
git commit -m "docs: finalize Authentik proof verification"
```
