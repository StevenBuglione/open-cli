# Native MCP Integration and OAuth Support Design

## Problem

`oas-cli-go` currently assumes that toolable services already exist as HTTP/OpenAPI surfaces. That leaves a major adoption gap for users who already have MCP servers and `.mcp.json`-style configuration but do not yet have an OpenAPI service catalog to point `oascli` at. At the same time, the runtime only supports static auth material for `http` and `apiKey` schemes, so OpenAPI `oauth2` and `openIdConnect` security schemes cannot be executed correctly.

The design must solve both gaps without forcing users into an external wrapper or a second configuration system.

## Goals

- Let users register MCP servers natively in `.cli.json` with configuration that is as close as possible to a normal `.mcp.json`.
- Make MCP-backed tools appear as normal `oascli` services and tools, so overlays, guidance, workflows, policy, caching, and audit remain first-class.
- Keep the canonical internal model aligned with existing `sources` and `services`, rather than inventing a second runtime model.
- Add real runtime support for OpenAPI `oauth2` and `openIdConnect` schemes, including token acquisition and refresh.
- Preserve multi-instance isolation by storing MCP runtime state and OAuth token state under existing per-instance paths.
- Update implementation, spec, conformance, and docs together.

## Non-Goals

- Making `mcpo` a required runtime dependency.
- Requiring users to run a separate HTTP proxy to use MCP servers.
- Introducing a second top-level config file that must be kept in sync with `.cli.json`.
- Deferring OAuth to static bearer-token injection only.

## Chosen Approach

The runtime will support **two input shapes with one canonical model**:

1. A native `sources.<name>` entry with `type: "mcp"`.
2. A compatibility `mcpServers` block whose entries mirror `.mcp.json` server definitions.

During config load, `mcpServers` entries are normalized into canonical `sources` and auto-generated `services`. After normalization, the rest of the system works with the existing `sources` / `services` model.

MCP tools are discovered over the MCP protocol, converted into a synthetic OpenAPI document for catalog normalization, and annotated with MCP execution metadata so the runtime can execute them natively instead of pretending they are real HTTP endpoints.

OAuth support is added as a real token-management subsystem that can satisfy OpenAPI `oauth2` / `openIdConnect` requirements and MCP server-level OAuth, with per-instance token storage and refresh.

## Alternatives Considered

### 1. Native MCP support with config compatibility

This is the chosen approach.

It keeps `.cli.json` easy for `.mcp.json` users, preserves the existing internal model, and avoids a Python or sidecar dependency. It does require new discovery, execution, and auth layers, but those changes land in clear seams that already exist in `oas-cli-go`.

### 2. Spawn `mcpo` as a managed sidecar and consume its generated OpenAPI

This would be faster to wire initially because `oas-cli-go` could continue to see only HTTP/OpenAPI. It was rejected because it makes `mcpo` a hidden runtime dependency, turns a native feature into process orchestration, complicates lifecycle management, and still does not solve OAuth for ordinary OpenAPI services inside `oas-cli-go`.

### 3. One-time MCP import to a static OpenAPI snapshot

This would be the smallest implementation, but it breaks the expectation that MCP tools are live and makes the user regenerate snapshots whenever tool schemas change. It also does not solve runtime MCP execution or OAuth.

## Architecture

The feature is split into five units with clear boundaries.

### 1. Config normalization

**Purpose:** Accept MCP-friendly config while preserving the canonical `sources` / `services` model.

**Primary locations:**

- `pkg/config/types.go`
- `pkg/config/cli.schema.json`
- `pkg/config/load.go`
- new `pkg/config/mcp.go`
- spec mirror: `oas-cli-spec/schemas/cli.schema.json`

**Behavior:**

- Add `type: "mcp"` to source definitions.
- Add top-level `mcpServers` as a compatibility block that mirrors `.mcp.json` server entries.
- Normalize each `mcpServers.<name>` entry into:
  - `sources.<name>` with `type: "mcp"`
  - `services.<name>` if the user did not define one explicitly
- Explicit `services.<name>` entries may attach overlays, skills, workflows, aliases, or policy-related metadata to an MCP-backed service.
- If both canonical `sources.<name>` and `mcpServers.<name>` exist, canonical `sources` win and loader validation rejects contradictory definitions.

**Supported MCP server fields:**

- `command`
- `args`
- `env`
- `type` (`stdio`, `sse`, `streamable-http`)
- `url`
- `headers`
- `disabledTools`
- `oauth`

`oauth` is transport-level authentication for the MCP server itself. It is distinct from per-tool OpenAPI security requirements. Transport OAuth is used when the runtime must authenticate before `ListTools` or `CallTool` can succeed.

**Canonical `.cli.json` example:**

```json
{
  "sources": {
    "filesystem": {
      "type": "mcp",
      "transport": {
        "type": "stdio",
        "command": "npx",
        "args": ["-y", "@modelcontextprotocol/server-filesystem", "/workspace"]
      },
      "disabledTools": ["delete_file"]
    }
  },
  "services": {
    "filesystem": {
      "source": "filesystem",
      "alias": "Local Filesystem",
      "workflows": ["./workflows/files.yaml"]
    }
  }
}
```

**Compatibility `.cli.json` example:**

```json
{
  "mcpServers": {
    "filesystem": {
      "command": "npx",
      "args": ["-y", "@modelcontextprotocol/server-filesystem", "/workspace"],
      "type": "stdio",
      "disabledTools": ["delete_file"]
    }
  },
  "services": {
    "filesystem": {
      "alias": "Local Filesystem",
      "workflows": ["./workflows/files.yaml"]
    }
  }
}
```

This gives users a near drop-in migration path while keeping internal config uniform.

**MCP transport OAuth example:**

```json
{
  "mcpServers": {
    "remoteDocs": {
      "type": "streamable-http",
      "url": "https://mcp.example.com/mcp",
      "oauth": {
        "mode": "authorizationCode",
        "issuer": "https://auth.example.com",
        "clientId": {
          "type": "env",
          "value": "REMOTE_MCP_CLIENT_ID"
        },
        "clientSecret": {
          "type": "osKeychain",
          "service": "oas-cli",
          "account": "remote-mcp-client-secret"
        },
        "scopes": ["mcp.read"],
        "callbackPort": 8790,
        "tokenStorage": "instance"
      }
    }
  }
}
```

**Precedence rules:**

- `oauth` authenticates the MCP transport itself and is consulted before discovery or execution.
- Per-tool OpenAPI `security` requirements are still resolved through `secrets`.
- If a tool requires both transport OAuth and tool-level auth, both are applied; transport auth gets the MCP session established, and tool-level auth is applied to the synthesized tool execution metadata.
- When `oauth` is configured, it owns the `Authorization` header for the transport. User-supplied `headers.Authorization` is rejected as a configuration error to avoid ambiguous precedence.

### 2. MCP transport client

**Purpose:** Speak MCP over supported transports and expose a stable Go interface for discovery and execution.

**Primary locations:**

- new `pkg/mcp/client/`
- new `pkg/mcp/client/stdio.go`
- new `pkg/mcp/client/sse.go`
- new `pkg/mcp/client/streamablehttp.go`

**Interface:**

- `ListTools(ctx) ([]ToolDescriptor, error)`
- `CallTool(ctx, name string, args any) (ToolResult, error)`
- `Close() error`

**Supported transports in the initial implementation:**

- `stdio`
- `sse`
- `streamable-http`

The transport client owns wire-level MCP concerns only. It does not know about OpenAPI normalization, policy, or workflow binding.

**Discovery-time auth:**

- `stdio` uses no transport auth; discovery starts the subprocess and calls `ListTools`.
- `sse` and `streamable-http` may require OAuth before discovery. The transport client asks the OAuth engine for a transport token before `ListTools` and reuses the same provider for later `CallTool` requests.
- Static non-Authorization headers from config are attached to both discovery and execution requests.

### 3. MCP-to-OpenAPI catalog adapter

**Purpose:** Turn MCP tool schemas into synthetic OpenAPI so the existing catalog builder can keep doing the heavy lifting.

**Primary locations:**

- new `pkg/mcp/openapi/`
- `pkg/catalog/build.go`
- `pkg/catalog/types.go`

**Behavior:**

- Discovery connects to each enabled MCP source and lists its tools.
- The adapter generates an in-memory OpenAPI 3.1 document with one operation per MCP tool.
- Each synthetic operation is marked with vendor extensions that preserve MCP execution identity:
  - source name
  - transport kind
  - original MCP tool name
  - disabled-tools filtering outcome
- Operation IDs are stable and derived as `<service>.<tool>` before slugification, so workflows and guidance can bind to a durable identifier.
- If two generated operations would collide after normalization, the runtime appends a deterministic short hash of `<source-name>::<original-tool-name>` to the synthetic operation ID while preserving the unmodified MCP tool name in backend metadata.

**Important constraint:** the generated OpenAPI is for **catalog normalization**, not for HTTP execution. The runtime must still know that these tools are MCP-backed.

### 4. Execution router

**Purpose:** Route a normalized tool execution request to either the existing HTTP executor or the new MCP executor.

**Primary locations:**

- `internal/runtime/server.go`
- `pkg/exec/exec.go`
- new `pkg/exec/mcp.go`
- `pkg/catalog/types.go`

**Behavior:**

- Extend normalized tool metadata with an execution backend descriptor:
  - `kind: "http"` or `kind: "mcp"`
  - backend-specific fields for MCP source and MCP tool name
- `internal/runtime/server.go` resolves the tool, auth, and policy exactly once.
- The execution router dispatches:
  - HTTP-backed tools to the existing `httpexec.Execute`
  - MCP-backed tools to `mcp.Execute`
- Audit records, retries, refresh, and error surfacing remain runtime concerns, not transport concerns.

**Connection lifecycle:**

- Discovery connections are short-lived.
- Execution connections may be pooled per source inside the runtime process when safe.
- Per-instance runtime state stores MCP connection metadata under existing instance-aware paths so simultaneous terminals and agents remain isolated.

### 5. OAuth and auth engine

**Purpose:** Replace static-token-only behavior with a real auth engine that can satisfy OpenAPI OAuth requirements and MCP server OAuth.

**Primary locations:**

- new `pkg/auth/`
- `internal/runtime/server.go`
- `pkg/catalog/build.go`
- `pkg/exec/exec.go`
- `pkg/config/types.go`

**Config model:**

- Keep static secret types (`env`, `file`, `exec`, `osKeychain`).
- Add a new secret type: `oauth2`.
- Add MCP-server-local `oauth` config for `.mcp.json` compatibility under MCP sources or `mcpServers`.

**`secrets` example for OpenAPI OAuth:**

```json
{
  "secrets": {
    "petstore_oauth": {
      "type": "oauth2",
      "mode": "authorizationCode",
      "issuer": "https://auth.example.com",
      "clientId": {
        "type": "env",
        "value": "PETSTORE_CLIENT_ID"
      },
      "clientSecret": {
        "type": "osKeychain",
        "service": "oas-cli",
        "account": "petstore-client-secret"
      },
      "scopes": ["pets.read", "pets.write"],
      "callbackPort": 8788,
      "tokenStorage": "instance"
    }
  }
}
```

**Supported OAuth behaviors:**

- `authorizationCode`: browser-based login with PKCE and token refresh
- `clientCredentials`: direct token exchange with renewal behavior based on token expiry
- `openIdConnect`: issuer discovery via `.well-known/openid-configuration`, then treated as either `authorizationCode` or `clientCredentials` based on explicit config

The implementation in this feature intentionally stops there. If an OpenAPI document declares `implicit` or `password`, catalog build records that metadata but runtime execution fails with a clear unsupported-flow error that points the user at the supported OAuth modes. This keeps the feature focused on the flows that are safe and still common in current deployments.

**Token-key derivation:**

- Token cache files live under the existing per-instance state directory.
- Each cached token key is derived from the stable tuple `{auth-kind, issuer, client-id, sorted-scopes, audience, scheme-name-or-source-name}`.
- The on-disk filename uses a readable slug prefix plus a hash suffix so collisions stay impossible while manual inspection remains practical.

**Storage and isolation:**

- OAuth tokens are stored under per-instance state directories, for example:
  - `<state>/oauth/<provider-key>.json`
- That preserves the existing multi-instance guarantees and prevents one terminal session from trampling another session’s refresh state.

## Data Flow

### MCP source discovery

1. Config loader merges scopes and normalizes `mcpServers` into canonical `sources` / `services`.
2. Catalog builder sees a source with `type: "mcp"`.
3. If the source has transport `oauth`, the OAuth engine acquires or refreshes the transport token before discovery.
4. MCP discovery connects to the server and lists tools.
5. Disabled tools are filtered before normalization.
6. MCP tool schemas are converted into synthetic OpenAPI.
7. Existing catalog normalization extracts parameters, request bodies, command names, guidance hooks, and workflow bindings.
8. Each normalized tool retains MCP execution metadata so runtime dispatch stays correct.

### MCP tool execution

1. `oascli` asks the runtime to execute a tool.
2. Runtime resolves the normalized tool and its auth requirements.
3. Policy and curation checks run as usual.
4. The execution router sees `backend.kind == "mcp"`.
5. If the source has transport `oauth`, the OAuth engine acquires or refreshes the transport token before the connection is opened.
6. The MCP executor connects or reuses a connection for the configured source.
7. The original MCP tool name is called with validated arguments.
8. Result content is normalized back into the existing CLI response model.

### OAuth-backed HTTP or MCP auth

1. Catalog build records the security requirements for a tool.
2. Runtime matches each requirement to a configured secret or MCP server auth block.
3. The OAuth engine loads or acquires an access token.
4. Tokens are refreshed or renewed as needed and stored under instance state.
5. The executor applies the resulting credential to the HTTP request or MCP transport.

## Error Handling

- Invalid MCP config fails at config load with field-specific validation errors.
- Unreachable MCP sources fail catalog build the same way unreachable OpenAPI sources do today; the failure names the source and transport.
- Invalid MCP tool schemas fail normalization with the tool name and server name.
- OAuth acquisition failures are surfaced explicitly with provider, flow, and endpoint context; no silent fallback to unauthenticated execution.
- Legacy OAuth flows (`password`, `implicit`) require explicit config and emit clear warnings in docs and runtime messages.
- Token cache corruption triggers a cache-reset-and-reauthorize path for only the affected provider, not the entire runtime.

## Testing Strategy

### Unit tests

- Config normalization tests for `mcpServers` to canonical `sources` / `services`
- Schema validation tests for MCP config, OAuth config, and new auth surfaces
- MCP OpenAPI generation tests that assert stable tool IDs, parameter mapping, and disabled-tools filtering
- Auth engine tests for token caching, refresh, discovery, and legacy-flow gating

### Integration tests

- Fake stdio MCP server
- Fake streamable-http MCP server
- Fake SSE MCP server
- Fake OAuth issuer covering authorization-code, client-credentials, and OIDC discovery
- Runtime execution tests that prove MCP-backed and HTTP-backed tools can coexist in one catalog

### Cross-repo verification

- `oas-cli-go`: Go tests, builds, docs build, and MCP/OAuth integration tests
- `oas-cli-spec`: schema and prose updates for `mcpServers`, `type: "mcp"`, OAuth secrets, cookie API keys, and mTLS config
- `oas-cli-conformance`: fixtures and expected outputs for MCP sources, generated services, OAuth-backed tools, and auth scheme coverage

## Documentation Changes

- Root `README.md`: add native MCP support and OAuth support to the feature summary
- Docusaurus docs:
  - configuration reference for `mcpServers`, `sources.type = mcp`, and OAuth secrets
  - runtime docs for MCP discovery and execution
  - security docs for OAuth, OIDC, cookie API keys, and mTLS
  - migration guide for moving from `.mcp.json` to `.cli.json`

## Rollout Notes

- Canonical config remains `sources` / `services`.
- Compatibility `mcpServers` exists to remove migration friction, not to create a second runtime model.
- Existing OpenAPI users keep working unchanged.
- MCP and OAuth state is per-instance from day one to preserve multi-terminal safety.
- The implementation should start from `origin/main` and land as one coordinated feature set across `oas-cli-go`, `oas-cli-spec`, and `oas-cli-conformance`.
