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

1. Clean the source path basename:
   - Ignore query string and fragment
   - Use the last non-empty path segment
   - Strip common spec suffixes: `.openapi.yaml`, `.openapi.yml`, `.openapi.json`, `.swagger.json`, `.swagger.yaml`, `.yaml`, `.yml`, `.json`
   - Normalize to lowercase kebab-case via the existing sanitization rules
2. Treat the cleaned basename as generic when it resolves to one of:
   - `openapi`
   - `swagger`
   - `api`
   - `spec`
   - `index`
3. If the basename is non-generic, use it.
4. Otherwise, derive from OpenAPI `info.title`:
   - First strip generic boilerplate terms from both ends of the title:
     - `swagger`
     - `openapi`
     - `api`
     - version suffixes like `2`, `3`, `3.0`, `3.0.0`
   - Normalize the remaining title to lowercase kebab-case with the same sanitization rules
   - Reject it if it is empty or still generic
5. Otherwise, if the source is a URL, derive from the host:
   - Parse the hostname
   - Drop a leading `www`
   - Use the first remaining DNS label (`petstore3.swagger.io` -> `petstore3`)
   - Normalize to lowercase kebab-case
   - Reject it if it is empty or still generic
6. Otherwise, fall back to `service`.

The derived name must remain stable, lowercase, and shell-friendly. Generic filenames like `openapi.json` should no longer produce `ocli openapi ...` unless the spec title is also generic.

Examples:

- `https://petstore3.swagger.io/api/v3/openapi.json` + title `Swagger Petstore - OpenAPI 3.0` => `petstore`
- `https://example.com/specs/payments-api.yaml` => `payments-api`
- `/tmp/openapi.json` + title `Billing Service` => `billing-service`
- `/tmp/api.json` + missing/empty title => `service`

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

Terminal-mode contract:

- Always print:
  - runtime summary line
  - config summary line
  - source summary line when config is available
- Print auth summary only when runtime info exposes auth metadata
- Print approval summary only when catalog context is available
- Never fail just because optional runtime auth fields are missing; print `unknown` rather than erroring

Structured-mode contract:

- Return a single object with stable top-level keys:
  - `runtime`
  - `config`
  - `sources`
  - `auth`
  - `approval`
  - `scopePaths`
- Missing context must be represented explicitly with null/empty/`unknown` values rather than omitted ad hoc

Structured `status` shape:

```json
{
  "runtime": {
    "available": true,
    "mode": "embedded|local|remote|unknown",
    "version": "string|null"
  },
  "config": {
    "activePath": "string|null"
  },
  "sources": {
    "totalActive": 0,
    "byType": {
      "openapi": 0,
      "mcp": 0
    }
  },
  "auth": {
    "mode": "providedToken|oauthClient|browserLogin|none|unknown",
    "required": "boolean|null",
    "audience": "string|null",
    "scopes": ["..."],
    "browserLoginConfigured": "boolean|null"
  },
  "approval": {
    "hasApprovalGatedTools": "boolean|null",
    "status": "required|not_required|unknown"
  },
  "scopePaths": {
    "managed": "string|null",
    "user": "string|null",
    "project": "string|null",
    "local": "string|null"
  }
}
```

### 3. Tool Preflight / Security Introspection

Users need a way to answer “what will happen if I run this?” without actually running the tool.

Instead of inventing a new top-level command, extend `explain` with a preflight/security view that includes:

- Tool auth requirements from the catalog
- Tool safety flags
- Whether approval is required by safety or policy
- The current runtime mode
- Whether the runtime is available

Terminal-mode contract:

- Keep the existing explain summary fields
- Add explicit security lines/fields for:
  - auth requirements
  - approval requirement
  - runtime mode
  - runtime availability

Structured-mode contract:

- Add stable keys:
  - `auth`
  - `approvalRequired`
  - `runtime`
  - `runtimeAvailable`

Structured `explain` additions:

```json
{
  "auth": [
    {
      "name": "string",
      "type": "string",
      "scheme": "string|null",
      "scopes": ["..."]
    }
  ],
  "approvalRequired": true,
  "approvalStatus": "required|not_required|unknown",
  "runtime": {
    "mode": "embedded|local|remote|unknown"
  },
  "runtimeAvailable": true
}
```

This keeps the UX discoverable: `catalog` finds tools, `search` narrows them, `explain` tells you operational and security details.

Degraded behavior:

- If runtime is unavailable, `runtimeAvailable` must be `false` and approval resolution must be best-effort from available config/catalog context.
- If policy context cannot be resolved, explain must say approval is `unknown` rather than claiming `false`.

### 4. Stronger Remote OAuth Story in CLI Output

The remote OAuth model already exists in the implementation and tests. The CLI should expose it more directly.

Changes:

- `status` should summarize runtime auth mode and key metadata when the runtime reports it.
- `auth status` should distinguish config-only auth posture from runtime-backed session posture:
  - `configOnly`
  - `runtimeSession`
  - `unknown`
- `explain` should show auth alternatives and approval posture clearly in terminal and structured output.

`auth status` source of truth:

- If runtime is available and exposes active auth/session posture, prefer runtime-backed posture.
- If runtime is unavailable but config exists, report `configOnly`.
- If neither runtime nor config provides enough information, report `unknown`.
- If runtime metadata conflicts with config, show both, but runtime-backed session posture wins for the top-level status summary.

This makes the security model legible without reading config files or source code.

## Files

### Modified Files

- `cmd/ocli/internal/commands/init.go`
  Improve service-name derivation and `init` messaging.
- `cmd/ocli/internal/commands/status.go`
  Add runtime auth and approval posture reporting.
- `cmd/ocli/internal/commands/catalog.go`
  Extend `explain` output with security/preflight fields.
- `cmd/ocli/internal/commands/auth.go`
  Extend `auth status` with config-only versus runtime-session posture.
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
  - `ocli init <fixture spec path>`

Deterministic verification requirements:

- Add fixture-backed tests for `init` naming, including a generic URL basename with a meaningful `info.title`
- Add fixture-backed tests for `init` naming when the basename is generic and `info.title` is missing/generic, proving the host/final fallback path
- Live CLI checks must include explicit assertions on output, not just command success
- Do not rely on a public external URL for primary verification; external URLs may be used only as optional smoke checks after local verification passes
- Add `status` coverage for:
  - runtime available with auth metadata
  - runtime unavailable
  - partial runtime auth metadata
  - approval-gated-tool detection
  - known scope-path reporting
  - browser-login configuration reporting
- Add `explain` coverage for:
  - approval required
  - approval unknown
  - auth requirements present
  - terminal/structured security-field parity
- Add `auth status` coverage proving the posture split between config-only and runtime-backed session states

## Risks

- Runtime auth metadata is partially dynamic, so `status` must degrade cleanly when fields are absent.
- `explain` should not overpromise policy resolution if the active runtime/catalog context is unavailable.
- Smarter `init` naming must remain deterministic and not surprise users with unstable aliases.
