# product-tests

End-to-end product tests for oascli. These tests spin up real infrastructure
(REST API, OAuth server, MCP servers) and exercise the CLI against them.

## Layout

```
product-tests/
  Makefile                  # Main entrypoints
  docker-compose.yml        # Core services (REST API, OAuth server)
  scripts/
    check-prereqs.sh        # Validate required tools are present
    services-up.sh          # Thin wrapper: docker compose up
    services-down.sh        # Thin wrapper: docker compose down
  mcp/
    remote/
      docker-compose.yml    # MCP remote-mode server (server-everything, streamable-http)
    stdio/
      filesystem.env        # Env vars for MCP filesystem stdio server
      time.env              # Env vars for timezone configuration
  testdata/
    configs/
      mcp-stdio.cli.json    # CLI config: filesystem server via stdio
      mcp-remote.cli.json   # CLI config: everything server via streamable-http
  tests/
    capability_execute_mcp_test.go  # MCP capability tests (stdio + remote)
```

## Required Tools

- `go` ≥ 1.22
- `python3` ≥ 3.9
- `docker` with Compose plugin (`docker compose`)
- `npx` (Node.js) for MCP server packages

## Running

```sh
# From repo root
make product-test-smoke    # quick smoke pass
make product-test-full     # full suite

# From this directory
make check-prereqs
make services-up
make smoke
make services-down
```

## MCP Capability Tests

### Stdio transport

Launches `@modelcontextprotocol/server-filesystem` via `npx`. No extra services
needed — `npx` downloads the package on first run.

```sh
# from product-tests/
make test-mcp-stdio

# or directly
go test ../... -run TestCapabilityExecuteMCPStdio -count=1 -v
```

### Remote transport (streamable-http)

Runs `@modelcontextprotocol/server-everything` in Docker on port **3001**.
Requires outbound npm access on the first Docker run.

```sh
# from product-tests/
make mcp-remote-up            # docker compose up -d
make test-mcp-remote          # up + run + down

# or manually
docker compose -f mcp/remote/docker-compose.yml up -d
go test ../... -run TestCapabilityExecuteMCPRemote -count=1 -v
docker compose -f mcp/remote/docker-compose.yml down
```

Override the server address:

```sh
MCP_REMOTE_HOST=192.168.1.10:3001 go test ./product-tests/tests -run TestCapabilityExecuteMCPRemote
```
