# OCLI Security UX Hardening Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Make `ocli` more credible as a secure, operator-friendly replacement for direct MCP usage by fixing `init` naming, surfacing runtime/auth posture in `status`, and exposing tool preflight security details in `explain` and `auth status`.

**Architecture:** Keep all changes in the CLI layer. Extend existing command constructors and output helpers rather than introducing new runtime APIs or new schema. Use test-first changes in `commands_test.go` and `main_test.go`, then implement the smallest command-layer updates needed to satisfy the spec.

**Tech Stack:** Go 1.26, Cobra, existing command/runtime/auth/config packages, stdlib JSON/tabwriter

---

## File Map

### Modified Files
| File | Responsibility |
|------|---------------|
| `cmd/ocli/internal/commands/init.go` | deterministic service-name derivation and improved init messaging |
| `cmd/ocli/internal/commands/status.go` | richer runtime/auth/approval status reporting |
| `cmd/ocli/internal/commands/catalog.go` | richer `explain` security/preflight output |
| `cmd/ocli/internal/commands/auth.go` | `auth status` posture split between config-only and runtime-session views |
| `cmd/ocli/internal/commands/table.go` | human-readable rendering for richer status/explain payloads as needed |
| `cmd/ocli/internal/commands/commands_test.go` | command-layer red/green coverage for naming, status, explain, auth status |
| `cmd/ocli/main_test.go` | root-command integration coverage for terminal-mode and remote OAuth behaviors |

### Verification Commands
```bash
export PATH="$HOME/.local/go/bin:$HOME/go/bin:$PATH"
go build ./cmd/ocli ./cmd/oclird
go test ./cmd/ocli/internal/commands/...
go test ./cmd/ocli -run 'TestRootCommandUsesOAuthClientRemoteRuntimeBearerToken|TestRootCommandCompletesRemoteBrowserLoginAuthorizationCodeFlow'
go test ./...
./bin/ocli --demo status
./bin/ocli --demo search create
./bin/ocli --demo explain demo:createItem
```

---

## Task 1: Init Naming Hardening

**Files:**
- Modify: `cmd/ocli/internal/commands/commands_test.go`
- Modify: `cmd/ocli/internal/commands/init.go`

- [ ] **Step 1: Write failing tests for generic-name fallbacks**

Add command-level tests covering:
- `openapi.json` + meaningful `info.title` -> non-generic derived name
- generic filename + generic/missing title + URL host -> host-based name
- generic filename + generic/missing title + local file -> `service`

- [ ] **Step 2: Run command tests to verify they fail**

Run: `export PATH="$HOME/.local/go/bin:$HOME/go/bin:$PATH"; go test ./cmd/ocli/internal/commands/...`
Expected: FAIL in the new init naming tests

- [ ] **Step 3: Implement deterministic naming logic in `init.go`**

Add helpers that:
- sanitize names consistently
- detect generic names
- strip generic title boilerplate
- derive host fallback from first DNS label

- [ ] **Step 4: Run command tests to verify they pass**

Run: `export PATH="$HOME/.local/go/bin:$HOME/go/bin:$PATH"; go test ./cmd/ocli/internal/commands/...`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add cmd/ocli/internal/commands/init.go cmd/ocli/internal/commands/commands_test.go
git commit -m "fix: harden init service name derivation"
```

---

## Task 2: Status and Auth Posture

**Files:**
- Modify: `cmd/ocli/internal/commands/commands_test.go`
- Modify: `cmd/ocli/internal/commands/status.go`
- Modify: `cmd/ocli/internal/commands/auth.go`
- Modify: `cmd/ocli/internal/commands/table.go`

- [ ] **Step 1: Write failing tests for richer `status` and `auth status` output**

Add tests covering:
- runtime available with auth metadata
- runtime unavailable
- partial auth metadata degraded to `unknown`/`null`
- approval-gated-tool detection
- scope-path rendering
- `auth status` posture split (`configOnly`, `runtimeSession`, `unknown`)

- [ ] **Step 2: Run command tests to verify they fail**

Run: `export PATH="$HOME/.local/go/bin:$HOME/go/bin:$PATH"; go test ./cmd/ocli/internal/commands/...`
Expected: FAIL in the new status/auth tests

- [ ] **Step 3: Implement richer `status` and `auth status`**

Update `status.go` to produce:
- terminal summary lines with runtime/auth/approval posture
- stable structured object fields for runtime/config/sources/auth/approval/scopePaths

Update `auth.go` to:
- distinguish config-only posture from runtime-backed session posture
- degrade explicitly when runtime/config evidence is incomplete

- [ ] **Step 4: Add/adjust table rendering if needed**

Keep terminal output compact and deterministic.

- [ ] **Step 5: Run command tests to verify they pass**

Run: `export PATH="$HOME/.local/go/bin:$HOME/go/bin:$PATH"; go test ./cmd/ocli/internal/commands/...`
Expected: PASS

- [ ] **Step 6: Commit**

```bash
git add cmd/ocli/internal/commands/status.go cmd/ocli/internal/commands/auth.go cmd/ocli/internal/commands/table.go cmd/ocli/internal/commands/commands_test.go
git commit -m "feat: surface runtime auth and approval posture"
```

---

## Task 3: Explain Preflight Security View

**Files:**
- Modify: `cmd/ocli/internal/commands/commands_test.go`
- Modify: `cmd/ocli/internal/commands/catalog.go`
- Modify: `cmd/ocli/internal/commands/table.go`

- [ ] **Step 1: Write failing tests for `explain` security output**

Cover:
- auth requirements present in structured output
- approval required
- approval unknown when context is insufficient
- terminal/structured parity for the new security fields

- [ ] **Step 2: Run command tests to verify they fail**

Run: `export PATH="$HOME/.local/go/bin:$HOME/go/bin:$PATH"; go test ./cmd/ocli/internal/commands/...`
Expected: FAIL in the new explain tests

- [ ] **Step 3: Extend `explain` output in `catalog.go`**

Add stable fields:
- `auth`
- `approvalRequired`
- `approvalStatus`
- `runtime`
- `runtimeAvailable`

Use best-effort policy/runtime context without introducing new runtime calls.

- [ ] **Step 4: Keep terminal rendering readable**

If needed, update `table.go` so terminal-mode explain output shows the new security posture clearly.

- [ ] **Step 5: Run command tests to verify they pass**

Run: `export PATH="$HOME/.local/go/bin:$HOME/go/bin:$PATH"; go test ./cmd/ocli/internal/commands/...`
Expected: PASS

- [ ] **Step 6: Commit**

```bash
git add cmd/ocli/internal/commands/catalog.go cmd/ocli/internal/commands/table.go cmd/ocli/internal/commands/commands_test.go
git commit -m "feat: add security preflight details to explain"
```

---

## Task 4: Root-Level Integration and Live Verification

**Files:**
- Modify: `cmd/ocli/main_test.go`
- Modify: any files needed from previous tasks

- [ ] **Step 1: Write/extend root-command integration tests**

Add root-command coverage for:
- terminal-mode `--demo status`
- terminal-mode `--demo explain demo:createItem`
- remote OAuth flows still passing with richer status/auth/explain surfaces

- [ ] **Step 2: Run targeted integration tests to verify they fail if coverage is new**

Run: `export PATH="$HOME/.local/go/bin:$HOME/go/bin:$PATH"; go test ./cmd/ocli -run 'TestRootCommandUsesOAuthClientRemoteRuntimeBearerToken|TestRootCommandCompletesRemoteBrowserLoginAuthorizationCodeFlow'`
Expected: PASS or targeted failures only from new assertions

- [ ] **Step 3: Fix any integration issues**

Adjust command-layer code only. Do not add new runtime endpoints.

- [ ] **Step 4: Run full verification**

Run:
```bash
export PATH="$HOME/.local/go/bin:$HOME/go/bin:$PATH"
go build ./cmd/ocli ./cmd/oclird
go test ./...
go build -o bin/ocli ./cmd/ocli
./bin/ocli --demo status
./bin/ocli --demo search create
./bin/ocli --demo explain demo:createItem
```

Expected:
- build succeeds
- full test suite passes
- live CLI commands succeed and visibly surface the new status/explain behavior

- [ ] **Step 5: Commit and push**

```bash
git add -A
git commit -m "feat: harden ocli security UX and onboarding"
git push origin main
```
