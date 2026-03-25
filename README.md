# open-cli

**`ocli`** and **`oclird`** turn OpenAPI descriptions and MCP servers into a local, policy-aware command surface. Discovery, auth resolution, policy enforcement, and audit logging happen inside the runtime — not spread across callers.

This is the Go reference implementation of the [Open CLI specification](spec/).

**Full docs:** https://open-cli.dev/

---

## Installation

### npm (recommended)

```bash
npm install -g @sbuglione/open-cli
```

This installs both `ocli` and `oclird` globally. The package automatically downloads the correct binary for your platform (macOS, Linux, Windows — x64 and arm64).

### Download a release binary

Pre-built binaries for every platform are attached to each [GitHub Release](https://github.com/StevenBuglione/open-cli/releases). Download the archive for your OS/architecture, extract it, and place the binaries on your `PATH`.

### Build from source

Requires Go 1.25.1+:

```bash
go install github.com/StevenBuglione/open-cli/cmd/ocli@latest
go install github.com/StevenBuglione/open-cli/cmd/oclird@latest
```

---

## Two binaries, one execution model

The project ships two binaries with a deliberate split:

| Binary | Role |
|--------|------|
| `ocli` | Operator-facing CLI. Renders the effective catalog, exposes dynamic commands derived from your OpenAPI or MCP sources, and forwards execution requests. |
| `oclird` | Runtime daemon. Loads config, performs discovery, normalizes catalogs, resolves auth, enforces policy, executes upstream HTTP requests, and records audit events. |

`ocli` always needs a runtime. In **embedded mode** it starts one in-process — no separate process required. In **local daemon mode** it connects to a running `oclird`. In either case the same runtime server logic executes.

---

## Quick Start

### Try the demo (no setup needed)

    ocli --demo catalog list --format pretty

This uses a built-in sample API to show how open-cli works.

### Set up your own API

    ocli init https://petstore3.swagger.io/api/v3/openapi.json

This creates a `.cli.json` configuration from your OpenAPI spec.

### Manual configuration

**Prerequisites:** Install via npm (`npm install -g @sbuglione/open-cli`) or [download a release binary](https://github.com/StevenBuglione/open-cli/releases).

Create a minimal `.cli.json` pointing at an OpenAPI document:

```json
{
  "cli": "1.0.0",
  "mode": { "default": "discover" },
  "sources": {
    "ticketsSource": {
      "type": "openapi",
      "uri": "./tickets.openapi.yaml",
      "enabled": true
    }
  },
  "services": {
    "tickets": {
      "source": "ticketsSource",
      "alias": "helpdesk"
    }
  }
}
```

Inspect the catalog — no daemon, no upstream calls:

```bash
ocli --embedded --config ./.cli.json catalog list --format pretty
```

This prints the normalized catalog: service aliases, tools, and generated command names derived from your OpenAPI document. Nothing contacts any upstream service.

Inspect a specific tool before executing it:

```bash
ocli --embedded --config ./.cli.json tool schema tickets:listTickets --format pretty
ocli --embedded --config ./.cli.json explain tickets:listTickets --format pretty
```

Preview the generated command tree:

```bash
ocli --embedded --config ./.cli.json helpdesk tickets --help
```

For a complete walkthrough including a sample OpenAPI document, see the [quickstart](https://open-cli.dev/docs/getting-started/quickstart).

---

## Deployment modes

| Mode | Description | When to use |
|------|-------------|-------------|
| **Embedded** | Runtime runs in-process per invocation. No daemon required. | Local dev, scripting, CI |
| **Local daemon** | Single `oclird` shared across CLI invocations. Warmed catalog cache. | Local MCP servers, persistent session, shared cache |
| **Remote runtime** | Centrally hosted `oclird` with network-controlled access. | Team-shared enforcement point, brokered access control |

**Starting a local daemon:**

```bash
oclird --config ./.cli.json --addr 127.0.0.1:8765

# In another shell:
ocli --runtime http://127.0.0.1:8765 --config ./.cli.json catalog list --format pretty
```

**Config-driven selection** — avoid flags by declaring mode in `.cli.json`:

```json
{
  "runtime": {
    "mode": "auto",
    "local": {
      "sessionScope": "terminal",
      "shutdown": "when-owner-exits",
      "share": "exclusive"
    }
  }
}
```

`mode: auto` stays embedded unless local MCP sources are present, in which case `ocli` promotes to local-daemon mode automatically. Managed local runtimes register a session lease and shut down when the owning session exits.

---

## Auth, policy, and audit

Auth and policy enforcement live inside the runtime, not in the CLI layer.

**Per-request auth resolution** — OpenAPI `oauth2` and `openIdConnect` flows; MCP `streamable-http` with `clientCredentials` OAuth and `headerSecrets`; MCP transports `stdio`, legacy `sse`, and `streamable-http`; per-instance token caching under the runtime state directory.

**Remote runtime bearer auth** — when `oclird` is deployed with `runtime.server.auth` configured, it:

- validates bearer tokens against `oidc_jwks` or `oauth2_introspection`
- filters the visible catalog by `bundle:*`, `profile:*`, and `tool:*` scopes
- re-checks execution against the resolved authorization envelope
- records audit events for connect, auth failures, authz denials, token refresh, session lifecycle, and tool execution
- exposes the audit log at `GET /v1/audit/events`

Remote client auth modes — `providedToken` (forward a bearer token from an env reference), `oauthClient` (acquire a client-credentials token), and `browserLogin` (authorization-code + PKCE flow against the runtime's broker).

**Reference proof** — the repository ships an Authentik-based reference proof for both `oauthClient` and `browserLogin` runtime auth paths:

- Repo assets: `examples/runtime-auth-broker/authentik/`
- Docs: [Authentik reference proof](https://open-cli.dev/docs/runtime/authentik-reference)
- Microsoft Entra is documented as an upstream federation target in that same proof

---

## Where to go next

| Goal | Link |
|------|------|
| Quickstart with a sample OpenAPI document | [Quickstart](https://open-cli.dev/docs/getting-started/quickstart) |
| Choose embedded, local daemon, or remote runtime | [Deployment models](https://open-cli.dev/docs/runtime/deployment-models) |
| Configuration reference | [Configuration overview](https://open-cli.dev/docs/configuration/overview) |
| Full CLI command model | [CLI overview](https://open-cli.dev/docs/cli/overview) |
| Auth, policy, and secret sources | [Security overview](https://open-cli.dev/docs/security/overview) |
| Enterprise readiness checklist | [Enterprise overview](https://open-cli.dev/docs/enterprise/overview) |
| Contributing and development | [Development guide](https://open-cli.dev/docs/development/overview) |

---

## Repository layout

```
cmd/ocli        CLI entrypoint and runtime client
cmd/oclird      Daemon entrypoint
internal/runtime  Runtime HTTP API and wiring
pkg/              Config, discovery, catalog, policy, execution, caching, audit, observability
spec/             Normative Open CLI specification and JSON schemas (single source of truth)
conformance/      Language-neutral conformance fixtures and expected outputs
website/          Docusaurus site content, navigation, and landing page
.github/          CI and Pages automation
```

---

## Verification

```bash
make verify             # format Go code, run tests, build both binaries
make verify-spec        # validate spec examples against schemas
make verify-conformance # run conformance fixtures against spec/schemas
make verify-all         # all three
```

Spec and conformance targets create a `.venv` and install Python dependencies automatically — no system-level pip required.

**Product tests** — end-to-end capability tests in `product-tests/`:

```bash
make product-test-smoke  # validate infra configs only, no services started (runs in CI)
make product-test-full   # bring up services and run all capability tests (requires Docker)
```

**Docs site** — when `website/` or repo-facing docs change:

```bash
cd website && npm ci && npm run build
```

See [`product-tests/README.md`](product-tests/README.md) for the full list of product test targets.

If you change behavior, update the owning Go package tests and the relevant docs page in the same commit.
