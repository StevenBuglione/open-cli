# oas-cli-go

`oas-cli-go` is the Go reference implementation of OAS-CLI. It turns OpenAPI documents, discovery metadata, native MCP servers, overlays, and policy into a local command surface that humans and agents can use safely.

- **Full docs:** https://stevenbuglione.github.io/oas-cli-go/
- **Getting started:** https://stevenbuglione.github.io/oas-cli-go/docs/getting-started/intro
- **Development guide:** https://stevenbuglione.github.io/oas-cli-go/docs/development/overview
- **Verification:** `make verify` for the Go implementation, plus `cd website && npm ci && npm run build` when docs change

## What this repository ships

The project is intentionally split into two binaries:

| Binary | Purpose | Use it when |
| --- | --- | --- |
| `oascli` | Operator-facing CLI that renders the effective catalog, exposes dynamic commands, and forwards execution requests. | You want to inspect services, explain tools, render schemas, run workflows, or execute a tool. |
| `oasclird` | Local runtime daemon that loads config, performs discovery, normalizes catalogs, resolves auth, enforces policy, executes upstream HTTP calls, and records audit events. | You want a reusable runtime process instead of starting one inside each CLI invocation. |

The common flow looks like this:

1. `oascli` resolves a runtime target or starts an embedded runtime.
2. The runtime loads `.cli.json`, merges scopes, and validates the effective config.
3. Discovery loads OpenAPI descriptions, overlays, workflows, and related metadata.
4. `oascli` renders catalog-driven commands while `oasclird` remains the enforcement point for policy, auth, retries, cache state, and audit logging.

If you want the deeper model, start with the docs site: https://stevenbuglione.github.io/oas-cli-go/

## Native MCP and OAuth support

The current implementation supports both traditional OpenAPI sources and native MCP sources.

- MCP transports: `stdio`, legacy `sse`, and `streamable-http`
- `.mcp.json`-style compatibility through top-level `mcpServers`
- OpenAPI `oauth2` and `openIdConnect` runtime auth
- MCP `streamable-http` transport auth with `clientCredentials` `oauth` and `transport.headerSecrets`
- per-instance OAuth token caching under the runtime state directory

Example:

```json
{
  "cli": "1.0.0",
  "mode": { "default": "discover" },
  "sources": {
    "filesystem": {
      "type": "mcp",
      "transport": {
        "type": "stdio",
        "command": "npx",
        "args": ["-y", "@modelcontextprotocol/server-filesystem", "/workspace"]
      }
    },
    "remoteDocs": {
      "type": "mcp",
      "transport": {
        "type": "streamable-http",
        "url": "https://mcp.example.com/mcp",
        "headerSecrets": {
          "X-API-Key": "mcp.apiKey"
        }
      },
      "oauth": {
        "mode": "clientCredentials",
        "tokenURL": "https://auth.example.com/oauth/token",
        "clientId": { "type": "env", "value": "MCP_CLIENT_ID" },
        "clientSecret": { "type": "env", "value": "MCP_CLIENT_SECRET" }
      }
    }
  },
  "secrets": {
    "mcp.apiKey": { "type": "env", "value": "MCP_API_KEY" }
  }
}
```

## Install from source

There are no packaged installers in this repository today; the documented path is to build from source.

### Prerequisites

- Go **1.25.1+**
- Node.js **18+** only if you need to build the docs site under `website/`

### Build the binaries

From the repository root:

```bash
go build -o ./bin/oascli ./cmd/oascli
go build -o ./bin/oasclird ./cmd/oasclird
```

Or install them into your Go bin directory:

```bash
go install ./cmd/oascli
go install ./cmd/oasclird
```

## Run it

### Daemon mode

Start the runtime:

```bash
go run ./cmd/oasclird --config /path/to/.cli.json --addr 127.0.0.1:8765
```

In another shell, point the CLI at that runtime:

```bash
go run ./cmd/oascli --runtime http://127.0.0.1:8765 --config /path/to/.cli.json catalog list --format pretty
```

### Embedded mode

For one-off inspection or local experimentation, run the runtime in-process:

```bash
go run ./cmd/oascli --embedded --config /path/to/.cli.json catalog list --format pretty
```

For a fuller walkthrough, see the quickstart: https://stevenbuglione.github.io/oas-cli-go/docs/getting-started/quickstart

## Verify changes

### Go implementation

```bash
make verify
```

That target formats Go code, runs `go test ./...`, and builds both binaries.

### Spec and conformance

`spec/` is the single source of truth for the OAS-CLI contract.  `conformance/` holds the language-neutral fixtures that validate implementations against those schemas.

```bash
make verify-spec          # validate spec examples against schemas
make verify-conformance   # run conformance fixtures against spec/schemas
make verify-all           # verify + verify-spec + verify-conformance
```

Both targets install their own Python dependencies via `pip install -q -r requirements.txt` before running.

### Docs site

When `README.md`, `website/`, or repo-facing docs change, also verify the Docusaurus site:

```bash
cd website
npm ci
npm run build
```

## Where the full docs live

The Docusaurus site is the long-form documentation for this repo:

- Introduction: https://stevenbuglione.github.io/oas-cli-go/docs/getting-started/intro
- Installation: https://stevenbuglione.github.io/oas-cli-go/docs/getting-started/installation
- CLI and runtime behavior: https://stevenbuglione.github.io/oas-cli-go/docs/cli/overview
- Configuration, discovery, and security: https://stevenbuglione.github.io/oas-cli-go/docs/configuration/overview
- Contributor guidance: https://stevenbuglione.github.io/oas-cli-go/docs/development/overview

## Repository guide

- `cmd/oascli`: CLI entrypoint and runtime client
- `cmd/oasclird`: daemon entrypoint
- `internal/runtime`: runtime HTTP API and wiring
- `pkg/`: reusable packages for config, discovery, catalog building, policy, execution, caching, audit, and observability
- `spec/`: normative OAS-CLI specification and JSON schemas (single source of truth for the public contract)
- `conformance/`: language-neutral conformance fixtures and expected outputs
- `website/`: Docusaurus site content, navigation, and landing page
- `.github/workflows/`: CI and Pages automation

If you change behavior, update the owning Go package tests and the relevant docs page in the same change whenever possible.
