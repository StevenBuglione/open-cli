# OCLI Security UX Hardening Design

## Goal

Strengthen `ocli` as a secure, operator-friendly alternative to direct MCP usage by improving onboarding quality, security-state visibility, and pre-execution clarity in both demo and remote OAuth-backed modes.

## Scope

This design addresses four concrete pain points observed from live CLI usage:

1. `init` derives poor default service names from common spec URLs.
2. `status` does not expose enough runtime and auth posture to support the security story.
3. Users cannot inspect auth and approval posture for a tool before executing it.
4. Remote OAuth security features exist in the implementation but are not surfaced clearly in the CLI.

This work is limited to the CLI layer under `cmd/ocli/internal/commands/` and existing runtime-facing APIs. It does not introduce new runtime endpoints or new `.cli.json` schema requirements.

## Design

### 1. Smarter `init` Naming

`init` should derive a human-meaningful service name from both the URL path and the OpenAPI document metadata.

Priority order:

1. Explicit, cleaned path basename when it is meaningful.
2. OpenAPI `info.title` when the URL basename is generic (`openapi`, `swagger`, `api`, `spec`, `index`).
3. Existing fallback sanitization.

The derived name must remain stable, lowercase, and shell-friendly. Generic filenames like `openapi.json` should no longer produce `ocli openapi ...` unless the spec title is also generic.

### 2. Security-Focused `status`

`status` should become the top-level operator health summary for the runtime and active config.

It should report:

- Runtime availability and mode (`embedded`, `local daemon`, `remote`)
- Runtime version when available
- Active config path
- Active source counts by type
- Runtime auth summary when exposed by runtime info
- Whether browser login is configured when relevant
- Whether approval-gated tools are present in the active catalog
- Known config scope paths

The output must remain compact in terminal mode and machine-readable in JSON/YAML/pretty mode.

### 3. Tool Preflight / Security Introspection

Users need a way to answer “what will happen if I run this?” without actually running the tool.

Instead of inventing a new top-level command, extend `explain` with a preflight/security view that includes:

- Tool auth requirements from the catalog
- Tool safety flags
- Whether approval is required by safety or policy
- The current runtime mode
- Whether the runtime is available

This keeps the UX discoverable: `catalog` finds tools, `search` narrows them, `explain` tells you operational and security details.

### 4. Stronger Remote OAuth Story in CLI Output

The remote OAuth model already exists in the implementation and tests. The CLI should expose it more directly.

Changes:

- `status` should summarize runtime auth mode and key metadata when the runtime reports it.
- `auth status` should distinguish config-only auth posture from runtime-backed session posture.
- `explain` should show auth alternatives and approval posture clearly in terminal and structured output.

This makes the security model legible without reading config files or source code.

## Files

### Modified Files

- `cmd/ocli/internal/commands/init.go`
  Improve service-name derivation and `init` messaging.
- `cmd/ocli/internal/commands/status.go`
  Add runtime auth and approval posture reporting.
- `cmd/ocli/internal/commands/catalog.go`
  Extend `explain` output with security/preflight fields.
- `cmd/ocli/internal/commands/table.go`
  Add human-readable table rendering for richer `status` / `explain` payloads if needed.
- `cmd/ocli/internal/commands/commands_test.go`
  Add red-green coverage for naming, status summaries, and explain security output.
- `cmd/ocli/main_test.go`
  Add end-to-end CLI coverage where root-command integration matters.

## Non-Goals

- No new remote runtime API endpoints
- No token storage redesign
- No change to the underlying policy engine
- No attempt to replace all MCP transport features in this pass

## Verification

Verification must include:

- Unit tests for `init` name derivation, `status`, and `explain`
- `go build ./cmd/ocli ./cmd/oclird`
- `go test ./...`
- Live CLI usage:
  - `ocli --demo status`
  - `ocli --demo search create`
  - `ocli --demo explain <tool-id>`
  - `ocli init <real OpenAPI URL>`

## Risks

- Runtime auth metadata is partially dynamic, so `status` must degrade cleanly when fields are absent.
- `explain` should not overpromise policy resolution if the active runtime/catalog context is unavailable.
- Smarter `init` naming must remain deterministic and not surprise users with unstable aliases.
