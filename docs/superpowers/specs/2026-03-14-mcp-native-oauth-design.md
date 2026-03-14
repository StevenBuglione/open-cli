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
- `mcpServers` normalization only runs for names that are not already present in `sources`.
- If the same name appears in both `sources` and `mcpServers`, config load fails with an ambiguity error even if the values appear equivalent. The user must keep exactly one owner for each source name.
- Auto-generated services are created only when `services.<name>` does not already exist. Explicit services always own overlays, workflows, aliases, and future service-level options.
- If `services.<name>` exists, matches an MCP source name, and omits `source`, normalization injects `source: "<name>"` before schema validation. If it sets a different `source`, config load fails.

**Canonical `sources.<name>` MCP fields:**

- `transport.type` (`stdio`, `sse`, `streamable-http`)
- `transport.command`
- `transport.args`
- `transport.env`
- `transport.url`
- `transport.headers`
- `transport.headerSecrets`
- `disabledTools`
- `oauth`

`oauth` is transport-level authentication for the MCP server itself. It is distinct from per-tool OpenAPI security requirements. Transport OAuth is used when the runtime must authenticate before `ListTools` or `CallTool` can succeed.

The canonical `sources.<name>` shape is nested:

```json
{
  "type": "mcp",
  "transport": {
    "type": "streamable-http",
    "url": "https://mcp.example.com/mcp",
    "headers": {
      "X-Tenant": "docs"
    },
    "headerSecrets": {
      "X-API-Key": "remote_docs_api_key"
    }
  },
  "disabledTools": ["admin.delete"],
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
    "audience": "mcp.example.com",
    "callbackPort": 8790,
    "tokenStorage": "instance"
  }
}
```

The compatibility `mcpServers` shape stays flat for migration, and normalization rewrites it into the nested canonical source shape before any later validation or catalog work.

**Compatibility `mcpServers.<name>` fields:**

- `type` → normalized to `transport.type`
- `command` → normalized to `transport.command`
- `args` → normalized to `transport.args`
- `env` → normalized to `transport.env`
- `url` → normalized to `transport.url`
- `headers` → normalized to `transport.headers`
- `headerSecrets` → normalized to `transport.headerSecrets`
- `disabledTools` → preserved as `disabledTools`
- `oauth` → preserved as `oauth`

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
        "interactive": true,
        "tokenStorage": "instance"
      }
    }
  }
}
```

**Precedence rules:**

- `oauth` authenticates the MCP transport itself and is consulted before discovery or execution.
- MCP-generated operations in v1 do not synthesize additional per-tool `security` requirements; MCP auth is transport-level only in this feature set.
- When `oauth` is configured, it owns the `Authorization` header for the transport. User-supplied `headers.Authorization` is rejected as a configuration error to avoid ambiguous precedence.
- `transport.headerSecrets` resolves through the existing `secrets` map and is merged after literal headers. A literal header and a secret-backed header may not target the same header name.

**Service cardinality in v1:**

- Each MCP source owns exactly one service in v1.
- The service name is the source name.
- Additional `services.<name>` configuration may enrich that same service, but a second service pointing at the same MCP source is rejected.
- This keeps tool IDs, synthetic paths, workflow binding, and auth lookup deterministic.

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

`ToolDescriptor` is the transport-facing discovery shape:

- `name`: original MCP tool name
- `description`: human-readable summary
- `inputSchema`: MCP-declared JSON Schema for arguments
- `annotations`: optional MCP annotations preserved for future overlays or docs

`ToolResult` is the transport-facing execution shape:

- `structuredContent`: optional machine-readable result payload
- `content`: ordered MCP content array
- `isError`: whether the MCP server marked the result as an error payload

**Transport coverage by phase:**

- Phase 1: `stdio`, `streamable-http`
- Phase 2: `sse`

The transport client owns wire-level MCP concerns only. It does not know about OpenAPI normalization, policy, or workflow binding.

**Discovery-time auth:**

- `stdio` uses no transport auth; discovery starts the subprocess and calls `ListTools`.
- `sse` and `streamable-http` may require auth before discovery. The transport client asks the auth engine for a transport application plan before `ListTools` and reapplies the same config on later `CallTool` requests.
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
- The final post-collision operation ID is the externally visible tool ID used by workflows, guidance, policy, and audit records.

**Schema-mapping contract:**

- Every MCP tool becomes a synthetic `POST` operation.
- The operation path is `/_mcp/<service-slug>/<tool-slug>`.
- MCP tool input is always represented as a JSON request body, never as path or query parameters, because MCP tool invocation is RPC-style rather than resource-style.
- If the MCP input schema is an object, it becomes the request-body schema directly.
- If the MCP input schema is absent, the request body schema is an empty object.
- If the MCP input schema is a non-object JSON schema, it is wrapped as `{ "input": <original-schema> }` so the CLI still has a stable object-shaped body contract.
- The catalog metadata records whether wrapper mode was used. During execution, `pkg/exec/mcp.go` unwraps wrapped inputs back to the original primitive or array value before calling `CallTool`.
- Supported JSON Schema features are the subset already accepted by the existing OpenAPI normalization path for request bodies.
- Unsupported constructs such as cyclic `$ref` graphs or discriminator-free ambiguous unions fail catalog build with the source and tool name in the error.
- The synthetic response schema is a stable MCP envelope:
  - `structuredContent` for machine-readable structured payloads when present
  - `content` for the ordered MCP content array
  - `isError` when the MCP server marks the tool result as an error payload

This keeps MCP normalization predictable and avoids inventing fake REST path/query semantics.

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
- The initial implementation opens execution connections per request and closes them when the tool call completes.
- Connection pooling is explicitly deferred until the native execution path has test coverage across all transports.
- Runtime cancellation closes the active MCP connection or subprocess context immediately and leaves no persistent connection handles behind.
- MCP execution retries are disabled in v1 unless a future overlay explicitly marks a tool as safely retryable.

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
- Catalog build stores OAuth scheme metadata for each tool, including:
  - scheme name
  - scheme type (`oauth2` or `openIdConnect`)
  - declared flow type
  - authorization URL
  - token URL
  - refresh URL
  - OIDC discovery URL
  - required scopes for the tool
- Catalog build also stores normalized security alternatives on each HTTP-backed tool as:
  - `securityAlternatives[]`
  - each alternative has ordered `requirements[]`
  - each requirement carries scheme name, scheme type, application target (`header`, `query`, `cookie`, `tls`), and any OAuth flow metadata needed by runtime resolution

The runtime auth engine consumes that normalized contract directly; executors never re-parse raw OpenAPI security definitions.

**`secrets` example for OpenAPI OAuth:**

```json
{
  "secrets": {
    "pets.petstore_oauth": {
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
      "audience": "pets-api",
      "callbackPort": 8788,
      "interactive": true,
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

**OAuth field defaults and validation:**

- `redirectURI`
  - optional exact loopback redirect URI for `authorizationCode`
  - when present, it overrides `callbackPort`
  - when present, port fallback is disabled and the URI must be usable exactly as configured
- `interactive`
  - valid for `authorizationCode` only
  - default: `true`
  - when `false`, the runtime must not open a browser or wait for loopback auth; it fails immediately with a non-interactive-auth-required error
- `callbackPort`
  - valid for `authorizationCode` only
  - default: first free port in the inclusive range `8787-8899`
  - if the configured port is busy, runtime falls back to the next free port in that range only when `interactive` is `true`; otherwise it fails with a port-in-use error that names the provider
  - if the provider config pins an exact redirect URI, fallback is disabled and the configured port must be available
- `tokenStorage`
  - valid values: `instance`, `memory`
  - default: `instance`
  - `instance` writes tokens under the per-instance state directory
  - `memory` keeps tokens only in process memory and requires re-authentication after runtime restart
- `audience`
  - optional string
  - default: empty
  - included in token cache key derivation only when present

**Authorization-code flow behavior:**

- The runtime opens the system browser by default.
- If browser launch fails or the runtime is headless, it prints the authorization URL and waits for loopback completion instead of silently failing.
- Loopback callbacks bind to `127.0.0.1` only.
- Loopback wait timeout defaults to 120 seconds.
- Context cancellation or Ctrl-C aborts the auth attempt immediately.
- If no loopback port can be acquired in the allowed range, auth fails before any token request is attempted.
- If the provider returns no refresh token, the runtime treats the token as non-refreshable and reacquires it interactively on expiry.
- If an OpenAPI `oauth2` scheme declares multiple flows, the secret config must set `mode` explicitly and that mode must name one of the declared flows; otherwise execution fails with an ambiguous-flow error before token acquisition.

**OAuth resolution matrix:**

| Tool requirement | Runtime lookup key | Endpoint source | Effective scopes | Refresh behavior |
| --- | --- | --- | --- | --- |
| `oauth2` | `secrets[service-name.scheme-name]` first, then `secrets[scheme-name]` | explicit values in the secret first, then OpenAPI scheme metadata | union of secret default scopes and tool-required scopes | refresh token if present, otherwise reacquire by configured mode |
| `openIdConnect` | `secrets[service-name.scheme-name]` first, then `secrets[scheme-name]` | explicit issuer in the secret first, then `openIdConnectUrl` from scheme metadata, then OIDC discovery | union of secret default scopes and tool-required scopes | refresh token if present, otherwise reacquire by configured mode |
| MCP transport `oauth` | source-local `oauth` block | explicit values in the MCP source config | exactly the scopes declared in the source-local block | refresh token if present, otherwise reacquire by configured mode |

The documentation and examples must show both lookup shapes:

- shared secret: `secrets["petstore_oauth"]`
- service-specific override: `secrets["pets.petstore_oauth"]`

**OpenAPI security requirement handling:**

- A single security requirement object means logical AND across its schemes; all listed schemes must resolve successfully.
- Multiple security requirement objects mean logical OR; the runtime evaluates them in order and uses the first fully satisfiable alternative.
- If no alternative is satisfiable, execution fails with an auth-resolution error that lists the scheme names it attempted.
- `oauth2` / `openIdConnect` can compose with `apiKey`, `http`, or future cookie-based credentials in the same AND alternative only when their concrete application targets do not conflict.
- If two schemes in the same AND alternative try to write different values to the same header, query key, cookie name, or TLS slot, that alternative is rejected as unsatisfiable and the runtime moves to the next OR alternative.
- `Authorization` is treated as a single-valued target. A bearer token, basic auth credential, or custom API-key header named `Authorization` cannot coexist in the same AND alternative.
- The auth engine returns a normalized application plan of `{headers, query, cookies, tls, tokenMetadata}`; executors apply that plan without reinterpreting scheme semantics.
- MCP transport auth uses that same application-plan shape, but only the transport-safe subset (`headers`, `query`, `cookies`, `tls`) is allowed. The MCP transport client receives the plan and applies it during discovery and execution requests.

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
3. If the source has transport `oauth`, the auth engine resolves a transport application plan before discovery.
4. MCP discovery connects to the server and lists tools.
5. Disabled tools are filtered before normalization.
6. MCP tool schemas are converted into synthetic OpenAPI.
7. Existing catalog normalization extracts parameters, request bodies, command names, guidance hooks, and workflow bindings.
8. Each normalized tool retains MCP execution metadata so runtime dispatch stays correct.

### MCP tool execution

1. `oascli` asks the runtime to execute a tool.
2. Runtime resolves the normalized tool and its source transport auth metadata.
3. Policy and curation checks run as usual.
4. The execution router sees `backend.kind == "mcp"`.
5. If the source has transport `oauth`, the auth engine resolves a transport application plan before the connection is opened.
6. The MCP executor connects for the configured source.
7. The original MCP tool name is called with validated arguments.
8. Result content is normalized back into the existing CLI response model.

**MCP result normalization:**

- If the MCP response contains `structuredContent`, that value becomes `primary`.
- Otherwise, if the response contains exactly one text item, that text becomes `primary`.
- Otherwise, if the response contains only text items, `primary` is the newline-joined text sequence in response order.
- If the MCP response contains mixed content, the runtime returns a JSON object with:
  - `primary`: the value selected by the rules above, or `null` when no stable primary representation exists
  - `content`: the ordered raw MCP content array
- MCP tool errors are surfaced as execution failures with the MCP error code, message, and attached data when present.

### OAuth-backed HTTP auth and MCP transport auth

1. Catalog build records OpenAPI security requirements for HTTP-backed tools and transport OAuth metadata for MCP-backed sources.
2. Runtime matches HTTP tool requirements to configured `secrets` and matches MCP transport auth to the source-local `oauth` block.
3. The OAuth engine loads or acquires an access token.
4. Tokens are refreshed or renewed as needed and stored under instance state.
5. The executor applies the resulting credential to the HTTP request or to the MCP transport connection setup.

## Error Handling

- Invalid MCP config fails at config load with field-specific validation errors.
- Unreachable MCP sources fail catalog build the same way unreachable OpenAPI sources do today; the failure names the source and transport.
- Invalid MCP tool schemas fail normalization with the tool name and server name.
- OAuth acquisition failures are surfaced explicitly with provider, flow, and endpoint context; no silent fallback to unauthenticated execution.
- OpenAPI `implicit` and `password` flows fail with an explicit unsupported-flow error that names the scheme and the supported replacement modes.
- Token cache corruption triggers a cache-reset-and-reauthorize path for only the affected provider, not the entire runtime.

## Caching

- MCP sources participate in **catalog caching**, not tool-result caching, in v1.
- The catalog cache key for an MCP source includes the source name, transport kind, endpoint or command signature, disabled-tools list, and normalized auth identity inputs that affect discovery shape.
- Access tokens themselves are never part of a cache key.
- `refresh` invalidates the generated MCP catalog snapshot and rebuilds it through fresh discovery.
- Tool execution responses from MCP are never cached in v1.

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
- `oas-cli-spec`: schema and prose updates for `mcpServers`, `type: "mcp"`, transport OAuth, and OAuth-backed tool auth
- `oas-cli-conformance`: fixtures and expected outputs for MCP sources, generated services, OAuth-backed tools, and auth scheme coverage

## Documentation Changes

- Root `README.md`: add native MCP support and OAuth support to the feature summary
- Docusaurus docs:
  - configuration reference for `mcpServers`, `sources.type = mcp`, and OAuth secrets
  - runtime docs for MCP discovery and execution
  - security docs for OAuth, OIDC, and MCP transport auth
  - migration guide for moving from `.mcp.json` to `.cli.json`

## Rollout Notes

- Canonical config remains `sources` / `services`.
- Compatibility `mcpServers` exists to remove migration friction, not to create a second runtime model.
- Existing OpenAPI users keep working unchanged.
- MCP and OAuth state is per-instance from day one to preserve multi-terminal safety.
- The implementation should start from `origin/main` and land as one coordinated feature set across `oas-cli-go`, `oas-cli-spec`, and `oas-cli-conformance`.

## Implementation Phasing

The work is one feature set, but the implementation plan must phase it explicitly:

### Phase 1: foundations and core coverage

- config normalization for `mcpServers` and canonical `type: "mcp"`
- stdio MCP discovery and execution
- streamable-http MCP discovery and execution
- MCP-to-OpenAPI adapter and execution metadata
- OAuth `authorizationCode`, `clientCredentials`, and `openIdConnect`
- docs, spec, and conformance updates for the above

### Phase 2: transport completion

- SSE MCP transport on the same discovery/execution interfaces
- additional integration coverage for remote MCP auth, cancellation cleanup, and reconnect behavior

Phase 1 is the minimum bar for the first implementation push. Phase 2 follows on the same branch before declaring the full MCP transport surface complete.
