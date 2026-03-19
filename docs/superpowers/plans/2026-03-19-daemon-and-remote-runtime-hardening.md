# Daemon And Remote Runtime Hardening Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Make normal `ocli` workflows daemon-only or remote-only, bind remote execution to a fixed runtime-owned config, remove silent embedded fallback, and harden execution, audit, and operator UX until live validation against real MCP servers and public APIs passes cleanly.

**Architecture:** The work is split into four bounded slices: config/runtime contract enforcement, remote runtime boundary hardening, execution-path reliability, and operator UX/product verification. The implementation should preserve existing runtime/server internals where possible, but the CLI contract becomes fail-closed and the server becomes authoritative for config identity in remote mode.

**Tech Stack:** Go, Cobra, kin-openapi, product tests, Docker-based MCP fixtures, npm MCP reference servers

---

### Task 1: Enforce Mandatory Runtime Contract

**Files:**
- Modify: `pkg/config/cli.schema.json`
- Modify: `pkg/config/schema.go`
- Modify: `pkg/config/load.go`
- Modify: `cmd/ocli/internal/runtime/deployment.go`
- Modify: `cmd/ocli/internal/config/resolve.go`
- Modify: `cmd/ocli/internal/commands/root.go`
- Modify: `cmd/ocli/internal/commands/search.go`
- Modify: `cmd/ocli/internal/commands/auth.go`
- Modify: `cmd/ocli/internal/commands/status.go`
- Test: `pkg/config/config_test.go`
- Test: `cmd/ocli/internal/commands/commands_test.go`

- [ ] **Step 1: Write failing config validation tests**

Add tests covering:
- missing `runtime` rejects config
- `runtime.mode: embedded` rejects config
- `runtime.mode: auto` rejects config
- `runtime.mode: local` accepts only local runtime URLs
- `runtime.mode: remote` accepts valid absolute HTTP(S) runtime URLs
- `--runtime` overrides `OCLI_RUNTIME_URL`, which overrides config
- failure when no runtime URL can be resolved
- rejection when a `local` override points to a non-local target
- rejection when a `remote` override attempts to target a local-only endpoint class

- [ ] **Step 2: Run the config tests to verify they fail**

Run: `PATH="$HOME/.local/go/bin:$HOME/go/bin:$PATH" go test ./pkg/config/... -run 'Test.*Runtime' -count=1`

Expected: FAIL because runtime is currently optional and embedded/auto still pass

- [ ] **Step 3: Implement schema and loader enforcement**

Update schema and validation so normal configs require a `runtime` object and reject `embedded` / `auto`. Keep `--demo` behavior out of normal config validation.

- [ ] **Step 4: Write failing CLI resolution tests**

Add command-resolution tests covering:
- `--runtime` no longer causes embedded execution with normal configs
- unreachable daemon/remote runtime fails hard
- `--embedded` is rejected for normal configs

- [ ] **Step 5: Run the CLI tests to verify they fail**

Run: `PATH="$HOME/.local/go/bin:$HOME/go/bin:$PATH" go test ./cmd/ocli/internal/commands/... -run 'Test.*Runtime.*|Test.*Embedded.*' -count=1`

Expected: FAIL because current resolution still permits embedded fallback

- [ ] **Step 6: Implement runtime selection hardening**

Update deployment/option resolution so:
- config mode is mandatory
- normal configs never resolve to embedded
- `--runtime` / `OCLI_RUNTIME_URL` follow strict precedence
- local mode rejects non-local targets
- remote mode requires an explicit reachable HTTP(S) target
- root/help/status/search/auth messaging no longer instructs users to use `--embedded`
- `--embedded` is removed or rejected consistently at the command layer

- [ ] **Step 7: Run targeted tests to verify they pass**

Run:
- `PATH="$HOME/.local/go/bin:$HOME/go/bin:$PATH" go test ./pkg/config/... -run 'Test.*Runtime' -count=1`
- `PATH="$HOME/.local/go/bin:$HOME/go/bin:$PATH" go test ./cmd/ocli/internal/commands/... -run 'Test.*Runtime.*|Test.*Embedded.*' -count=1`

Expected: PASS

- [ ] **Step 8: Commit**

```bash
git add pkg/config/cli.schema.json pkg/config/schema.go pkg/config/load.go \
  cmd/ocli/internal/runtime/deployment.go cmd/ocli/internal/config/resolve.go \
  cmd/ocli/internal/commands/root.go cmd/ocli/internal/commands/search.go \
  cmd/ocli/internal/commands/auth.go cmd/ocli/internal/commands/status.go \
  pkg/config/config_test.go cmd/ocli/internal/commands/commands_test.go
git commit -m "feat: require daemon or remote runtime config"
```

### Task 2: Bind Remote Runtime To Runtime-Owned Config Identity

**Files:**
- Modify: `internal/runtime/server.go`
- Modify: `internal/runtime/server_test.go`
- Modify: `cmd/ocli/internal/runtime/client.go`
- Modify: `cmd/ocli/internal/commands/status.go`
- Test: `product-tests/tests/capability_runtime_auth_authentik_test.go`
- Test: `product-tests/tests/capability_auth_policy_test.go`
- Test: `cmd/ocli/internal/commands/commands_test.go`

- [ ] **Step 1: Write failing tests for remote config identity enforcement**

Add tests covering:
- remote catalog ignores absent `config`
- remote catalog allows matching `config`
- remote catalog rejects mismatched `config`
- remote execute ignores absent `configPath`
- remote execute allows matching `configPath`
- remote execute rejects mismatched `configPath`
- remote workflow/refresh/audit/runtime-info endpoints ignore absent config selectors
- remote workflow/refresh/audit/runtime-info endpoints allow matching config selectors
- remote workflow/refresh/audit/runtime-info endpoints reject mismatched config selectors
- status reports actual connected runtime mode and URL

- [ ] **Step 2: Run the remote/runtime tests to verify they fail**

Run: `PATH="$HOME/.local/go/bin:$HOME/go/bin:$PATH" go test ./product-tests/tests/... -run 'Test.*Runtime.*|Test.*Auth.*Policy.*' -count=1`

Expected: FAIL because server currently loads caller-selected config paths

- [ ] **Step 3: Implement remote config binding**

Change the runtime server so remote-mode requests execute against the runtime-owned config identity and reject mismatched request-scoped config selection. Update client payload construction only as needed to preserve compatibility with local flows.

- [ ] **Step 4: Implement truthful status/runtime reporting**

Ensure status/runtime summaries reflect actual connected transport and URL instead of intended config posture.

- [ ] **Step 5: Run targeted tests to verify they pass**

Run:
- `PATH="$HOME/.local/go/bin:$HOME/go/bin:$PATH" go test ./cmd/ocli/internal/commands/... -run 'TestStatus.*Runtime' -count=1`
- `PATH="$HOME/.local/go/bin:$HOME/go/bin:$PATH" go test ./internal/runtime/... -run 'Test.*Remote.*Config.*|Test.*RuntimeInfo.*' -count=1`
- `PATH="$HOME/.local/go/bin:$HOME/go/bin:$PATH" go test ./product-tests/tests/... -run 'Test.*Runtime.*|Test.*Auth.*Policy.*' -count=1`

Expected: PASS

- [ ] **Step 6: Commit**

```bash
git add internal/runtime/server.go cmd/ocli/internal/runtime/client.go \
  internal/runtime/server_test.go cmd/ocli/internal/commands/status.go \
  product-tests/tests/capability_runtime_auth_authentik_test.go \
  product-tests/tests/capability_auth_policy_test.go \
  cmd/ocli/internal/commands/commands_test.go
git commit -m "feat: bind remote runtime to owned config identity"
```

### Task 3: Fix Execution Reliability, Dry-Run, And Audit Semantics

**Files:**
- Modify: `pkg/openapi/load.go`
- Modify: `pkg/catalog/build.go`
- Modify: `cmd/ocli/internal/commands/init.go`
- Modify: `cmd/ocli/internal/commands/dryrun.go`
- Modify: `cmd/ocli/internal/commands/dynamic.go`
- Modify: `internal/runtime/server.go`
- Modify: `pkg/audit/store.go`
- Test: `pkg/openapi/*_test.go`
- Test: `pkg/catalog/*_test.go`
- Test: `cmd/ocli/main_test.go`
- Test: `cmd/ocli/internal/commands/commands_test.go`
- Test: `product-tests/tests/capability_refresh_audit_test.go`

- [ ] **Step 1: Write failing tests for relative server execution**

Add tests covering:
- relative top-level server URLs normalize against remote spec origin
- normalized URLs are used during execution
- operation/path/document server precedence follows the spec contract
- server-variable default substitution works
- loading fails when a required server variable lacks a default
- URL-loaded specs with no `servers` fall back to the spec origin
- file-loaded specs with no `servers` preserve existing behavior
- init-generated config and runtime-loaded execution use the same normalization result

- [ ] **Step 2: Write failing tests for dry-run**

Add tests covering:
- dry-run prints request shape for auth-gated REST tools without resolving secrets
- dry-run prints MCP payload preview without executing the tool
- dry-run marks auth/approval posture as `required|not_required|unknown`
- unreachable remote runtime degrades to config/catalog-only preview when metadata is available
- unreachable remote runtime fails with `preview metadata unavailable` when metadata is insufficient
- unresolved REST base prints an explicit `base unresolved` note
- preview path performs no token acquisition, upstream HTTP, MCP calls, or daemon execution

- [ ] **Step 3: Write failing tests for audit semantics**

Add tests covering:
- execution failures record `execution_error`
- policy/authz denials record `authz_denial`
- successful execution records `tool_execution` with `reasonCode: "allowed"`
- empty audit API returns `[]`
- required audit fields are present
- denial reasons and execution-error reasons follow the event contract

- [ ] **Step 4: Run targeted tests to verify they fail**

Run:
- `PATH="$HOME/.local/go/bin:$HOME/go/bin:$PATH" go test ./pkg/openapi/... ./pkg/catalog/... -run 'Test.*Server.*|Test.*Relative.*|Test.*Normalize.*' -count=1`
- `PATH="$HOME/.local/go/bin:$HOME/go/bin:$PATH" go test ./cmd/ocli/internal/commands/... -run 'Test.*DryRun.*' -count=1`
- `PATH="$HOME/.local/go/bin:$HOME/go/bin:$PATH" go test ./product-tests/tests/... -run 'Test.*Audit.*|Test.*Refresh.*' -count=1`
- `PATH="$HOME/.local/go/bin:$HOME/go/bin:$PATH" go test ./cmd/ocli/... -run 'Test.*Init.*|Test.*Relative.*' -count=1`

Expected: FAIL

- [ ] **Step 5: Implement OpenAPI normalization**

Normalize relative server URLs against the spec origin and apply the same selection logic in init-generated configs and runtime-loaded execution.

- [ ] **Step 6: Implement pure preview dry-run**

Refactor dry-run so it shapes output from local/catalog metadata without auth resolution or live execution side effects.

- [ ] **Step 7: Implement audit event taxonomy fixes**

Separate denial vs execution-error classification and ensure empty audit responses serialize as `[]`.

- [ ] **Step 8: Run targeted tests to verify they pass**

Run the same commands from Step 4.

Expected: PASS

- [ ] **Step 9: Commit**

```bash
git add pkg/openapi/load.go pkg/catalog/build.go \
  cmd/ocli/internal/commands/init.go cmd/ocli/internal/commands/dryrun.go \
  cmd/ocli/internal/commands/dynamic.go internal/runtime/server.go \
  pkg/audit/store.go pkg/openapi/*_test.go pkg/catalog/*_test.go \
  cmd/ocli/main_test.go \
  cmd/ocli/internal/commands/commands_test.go \
  product-tests/tests/capability_refresh_audit_test.go
git commit -m "fix: harden execution preview and audit semantics"
```

### Task 4: Improve Introspection And MCP Command Usability

**Files:**
- Modify: `cmd/ocli/internal/commands/catalog.go`
- Modify: `cmd/ocli/internal/commands/dynamic.go`
- Modify: `cmd/ocli/internal/commands/util.go`
- Modify: `cmd/ocli/internal/commands/table.go`
- Test: `cmd/ocli/internal/commands/commands_test.go`
- Test: `cmd/ocli/main_test.go`

- [ ] **Step 1: Write failing tests for command-form explain/schema resolution**

Add tests covering:
- canonical tool ID still resolves
- command-form reference resolves
- ambiguous command-form reference returns a clear ambiguity error

- [ ] **Step 2: Write failing tests for MCP flag generation**

Add tests covering:
- top-level scalar MCP inputs become flags
- nested/complex inputs fall back to `--body`
- command rejects mixing generated flags with `--body`
- nullable scalars become flags
- enum/default metadata propagates to generated flags
- required vs optional top-level fields validate correctly
- reserved flag collisions fall back to `--body`
- mixed schemas keep scalar flags while retaining `--body`

- [ ] **Step 3: Run targeted tests to verify they fail**

Run:
- `PATH="$HOME/.local/go/bin:$HOME/go/bin:$PATH" go test ./cmd/ocli/internal/commands/... -run 'Test.*Explain.*|Test.*ToolSchema.*|Test.*MCP.*Flag.*' -count=1`
- `PATH="$HOME/.local/go/bin:$HOME/go/bin:$PATH" go test ./cmd/ocli/... -run 'Test.*MCP.*|Test.*Explain.*' -count=1`

Expected: FAIL

- [ ] **Step 4: Implement command-form tool resolution**

Add a deterministic resolver from service/group/command references to canonical tool IDs and use it in explain/tool-schema flows.

- [ ] **Step 5: Implement MCP scalar flag generation**

Generate first-class flags for top-level scalar MCP input properties while retaining `--body` for complex payloads and rejecting mixed input forms.

- [ ] **Step 6: Run targeted tests to verify they pass**

Run the same commands from Step 3.

Expected: PASS

- [ ] **Step 7: Commit**

```bash
git add cmd/ocli/internal/commands/catalog.go cmd/ocli/internal/commands/dynamic.go \
  cmd/ocli/internal/commands/util.go cmd/ocli/internal/commands/table.go \
  cmd/ocli/internal/commands/commands_test.go cmd/ocli/main_test.go
git commit -m "feat: improve tool introspection and mcp command usability"
```

### Task 5: Update Docs And Run Full Verification

**Files:**
- Modify: `README.md`
- Modify: `website/docs/runtime/*`
- Modify: `website/docs/security/*`
- Modify: `website/docs/cli/*`

- [ ] **Step 1: Update docs for the new runtime contract**

Document:
- runtime is mandatory
- normal configs support only `local` and `remote`
- no silent embedded fallback
- remote config identity is runtime-owned

- [ ] **Step 2: Build binaries from the final tree**

Run: `PATH="$HOME/.local/go/bin:$HOME/go/bin:$PATH" go build ./cmd/ocli ./cmd/oclird`

Expected: PASS

- [ ] **Step 3: Run full repository verification**

Run: `PATH="$HOME/.local/go/bin:$HOME/go/bin:$PATH" go test ./...`

Expected: PASS

- [ ] **Step 4: Run live CLI verification against real targets**

Run:
- public OpenAPI validation against petstore or another public spec with relative servers
- official MCP stdio validation using `@modelcontextprotocol/server-filesystem`
- official MCP streamable-http validation using `@modelcontextprotocol/server-everything`
- local daemon validation with `oclird`
- remote auth validation using the existing Authentik-backed product path when practical

Expected:
- normal configs fail without daemon/remote runtime
- public spec execution succeeds when configured through daemon/remote flow
- MCP stdio and streamable-http commands execute successfully
- status reports actual connected runtime mode
- dry-run prints previews without auth/network side effects

- [ ] **Step 5: Run CI-equivalent verification**

Run: `PATH="$HOME/.local/go/bin:$HOME/go/bin:$PATH" make verify`

Expected: PASS

- [ ] **Step 6: Commit**

```bash
git add README.md website/docs
git commit -m "docs: update daemon and remote runtime contract"
```

- [ ] **Step 7: Final integration commit if needed**

If the work spans multiple commits already, create only a final fixup commit for any last verification adjustments:

```bash
git add -A
git commit -m "fix: finalize daemon and remote runtime hardening"
```
