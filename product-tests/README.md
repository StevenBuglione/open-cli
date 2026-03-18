# product-tests

End-to-end product tests for oascli. These tests spin up real infrastructure
(REST API, OAuth server, MCP servers) and exercise the CLI against them.

## Layout

```
product-tests/
  Makefile                  # Main entrypoints (smoke, full, per-capability targets)
  docker-compose.yml        # Core services (REST API, OAuth server)
  authentik/
    docker-compose.yml      # Authentik reference-broker stack for runtime auth proof
    .env.example            # Example environment for the Authentik product-test stack
  scripts/
    check-prereqs.sh        # Validate required tools are present
    services-up.sh          # Thin wrapper: docker compose up
    services-down.sh        # Thin wrapper: docker compose down
    authentik-up.sh         # Thin wrapper: Authentik reference stack up
    authentik-down.sh       # Thin wrapper: Authentik reference stack down
  mcp/
    remote/
      docker-compose.yml    # MCP remote-mode server (server-everything, streamable-http)
    stdio/
      filesystem.env        # Env vars for MCP filesystem stdio server
      time.env              # Env vars for timezone configuration
  services/
    oauthstub/              # Embedded OAuth stub service (Go)
    testapi/                # Embedded REST test API service (Go)
  testdata/
    configs/
      mcp-stdio.cli.json    # CLI config: filesystem server via stdio
      mcp-remote.cli.json   # CLI config: everything server via streamable-http
      multi-instance.cli.json
      rest-only.cli.json
    expected/
      catalog-mcp.json      # Expected catalog output for MCP sources
      catalog-rest.json     # Expected catalog output for REST sources
    openapi/
      testapi.openapi.yaml  # OpenAPI description for the test REST API
  tests/
    capability_auth_policy_test.go
    capability_catalog_test.go
    capability_execute_mcp_test.go
    capability_execute_rest_test.go
    capability_multiinstance_test.go
    capability_refresh_audit_test.go
    helpers/
      runtime.go            # Shared test helpers for starting the runtime
```

## Required Tools

- `go` ≥ 1.22
- `docker` with Compose plugin (`docker compose`)
- `npx` (Node.js) for MCP server packages

## Smoke vs full

| Mode | Command | What it does |
| --- | --- | --- |
| Smoke | `make smoke` | Checks prerequisites and validates all compose config files — no services started |
| Full | `make full` | Validates prerequisites and configs (smoke), then brings up and tears down services — placeholder; individual capability targets not yet wired in |

Additional reference-broker targets:

- `make authentik-up`
- `make authentik-down`
- `make test-runtime-auth-authentik`

Smoke runs in CI automatically on every push and PR. Full is for local pre-merge validation.

## Running

```sh
# From repo root
make product-test-smoke    # quick smoke pass (CI target)
make product-test-full     # full suite

# From this directory
make check-prereqs
make services-up
make authentik-up
make smoke
make authentik-down
make services-down
```

## Capability Tests

### Catalog

```sh
go test ./tests/... -run TestCapabilityCatalogREST -count=1 -v
go test ./tests/... -run TestCapabilityCatalogMCP -count=1 -v

# or run both with a prefix match
go test ./tests/... -run TestCapabilityCatalog -count=1 -v
```

### Auth and policy

```sh
go test ./tests/... -run TestCapabilityAuthAndPolicy -count=1 -v
```

### Execute — REST

Requires the core services to be up (`make services-up`).

```sh
go test ./tests/... -run TestCapabilityExecuteREST -count=1 -v
```

### Execute — MCP stdio

Launches `@modelcontextprotocol/server-filesystem` via `npx`. No extra services
needed — `npx` downloads the package on first run.

```sh
make test-mcp-stdio

# or directly
go test ./tests/... -run TestCapabilityExecuteMCPStdio -count=1 -v
```

### Execute — MCP remote (streamable-http)

Runs `@modelcontextprotocol/server-everything` in Docker on port **3001**.
Requires outbound npm access on the first Docker run.

```sh
make mcp-remote-up            # docker compose up -d
make test-mcp-remote          # up + run + down

# or manually
docker compose -f mcp/remote/docker-compose.yml up -d
go test ./tests/... -run TestCapabilityExecuteMCPRemote -count=1 -v
docker compose -f mcp/remote/docker-compose.yml down
```

Override the server address:

```sh
MCP_REMOTE_HOST=192.168.1.10:3001 go test ./tests/... -run TestCapabilityExecuteMCPRemote
```

### Refresh and audit

```sh
go test ./tests/... -run TestCapabilityRefresh -count=1 -v
go test ./tests/... -run TestCapabilityAudit -count=1 -v
```

### Multi-instance

```sh
go test ./tests/... -run TestMultiInstance -count=1 -v
```

### Runtime auth — Authentik reference proof

This target is the dedicated entrypoint for the Authentik-based runtime auth proof stack.

It is now the automated reference proof for the workload path and verifies:

- live Authentik discovery/JWKS/token reachability
- repo-managed provider/application bootstrap through Authentik
- real `oascli` `oauthClient` token acquisition against Authentik
- live `oidc_jwks` validation in `oasclird`
- fail-closed rejection for insufficient scope, wrong audience, expired token, and alternate issuer

The product harness uses a tiny local proxy in the test fixture so `oascli` and `oasclird` can reach the self-signed Authentik test stack without weakening the runtime contract. The validated token `iss` still comes from Authentik.

This fixture is intentionally **workload-only**: it bootstraps a confidential Authentik provider for `oauthClient` and does **not** advertise `/v1/auth/browser-config`. The browser proof is covered separately with the public-client reference template under `examples/runtime-auth-broker/authentik/`.

The live Authentik tests are also intentionally **opt-in**. Plain `go test ./...` skips them unless `OCLI_RUN_AUTHENTIK_TESTS=1` is set, which keeps the root verification lane deterministic. The dedicated `make test-runtime-auth-authentik` target sets that flag for you after starting the Authentik stack.

```sh
cd product-tests
make authentik-up
make test-runtime-auth-authentik
make authentik-down
```
