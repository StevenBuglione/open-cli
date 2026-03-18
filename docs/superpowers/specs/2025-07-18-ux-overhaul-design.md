# UX Overhaul & Architecture Cleanup — Design Spec

**Date:** 2025-07-18
**Status:** Approved
**Scope:** Full sweep — CLI UX, main.go refactoring, npm hardening, documentation

---

## Problem Statement

The open-cli core product (runtime, catalog discovery, dynamic CLI generation, policy enforcement) is solid. But the first-run experience is broken:

- `ocli --help` crashes without a config file and runtime connection
- No scaffolding exists — users must hand-write `.cli.json`
- Error messages expose raw Go network errors instead of actionable guidance
- Website documentation contradicts the npm package (says "source-only, no pre-built binaries")
- npm `install.js` has no retry logic, no validation, and silently tolerates partial failures
- `cmd/ocli/main.go` is 1,657 lines with mixed responsibilities

A new user installs via npm, runs `ocli --help`, gets `connection refused`, and gives up.

## Approach

Four parallel work streams, each independently shippable:

1. **CLI UX Layer** — Fix broken paths, add `init`/`demo`, improve errors
2. **main.go Refactoring** — Extract into `cmd/ocli/internal/` subpackages
3. **npm Package Hardening** — Match esbuild/turbo quality
4. **Documentation Updates** — Fix contradictions, add npm-first guides

---

## Stream 1: CLI UX Layer

### 1.1 Graceful Degradation (No Config / No Runtime)

The fundamental problem: `NewRootCommand()` immediately connects to the runtime and fetches the catalog. If either fails, the process exits before Cobra can dispatch `--help` or any subcommand.

**Fix: Decouple help from runtime bootstrap.**

- Move `--help` detection before runtime initialization in `main()`
- When no config is found and no runtime flags are set:
  - Show a welcome message with getting-started guidance
  - List available built-in commands (`init`, `completion`, `version`)
  - Point to `ocli init <url>` and `ocli --demo`
- When config exists but runtime connection fails:
  - Show the error with full technical details
  - Suggest: "Is oclird running? Try: `ocli --embedded --config .cli.json`"
- `ocli completion bash/zsh/fish/powershell` generates static completions for built-in commands without requiring a runtime

**Behavior matrix:**

| Scenario | Current | New |
|----------|---------|-----|
| `ocli` (no args, no config) | Connection refused | Welcome message + guidance |
| `ocli --help` (no config) | Connection refused | Full help with built-in commands |
| `ocli --help` (with config) | Works (if runtime up) | Works (shows dynamic commands too) |
| `ocli --embedded --help` | "config query parameter is required" | Clear "specify --config" message |
| `ocli completion bash` | Requires runtime | Works without runtime |

### 1.2 `ocli init`

**Usage:**
```
ocli init <url-or-file-path>    # Auto-detect and scaffold
ocli init                        # Interactive prompt for URL
ocli init --type openapi <url>   # Explicit source type
ocli init --type mcp <address>   # MCP server
```

**Behavior:**

1. Accept URL (http/https), file path (relative/absolute), or MCP address
2. Auto-detect source type:
   - File ending in `.yaml`/`.yml`/`.json` → OpenAPI
   - URL returning OpenAPI media type → OpenAPI
   - URL with `/mcp` or MCP transport indicators → MCP
   - `npx`/`node`/executable commands → MCP stdio
3. For OpenAPI sources:
   - Fetch/read the spec
   - Extract `info.title` → suggest as service alias (slugified)
   - Detect if spec has auth requirements → note in config comments
4. Generate minimal `.cli.json`:
   ```json
   {
     "cli": "1.0.0",
     "mode": { "default": "discover" },
     "sources": {
       "<alias>": {
         "type": "openapi",
         "uri": "<url-or-path>"
       }
     },
     "services": {
       "<alias>": { "source": "<alias>" }
     }
   }
   ```
5. Validate by loading catalog in embedded mode
6. Print: `✓ Created .cli.json with service "<alias>" (<N> tools discovered)`
7. Print: `Next: ocli --embedded catalog list --format pretty`

**When `ocli init` is run with no arguments:**
- Prompt: "Enter the URL or file path of your API spec:"
- After receiving input, proceed with auto-detection

**Error handling:**
- URL returns 404 → "Could not fetch <url>: HTTP 404. Check the URL and try again."
- File not found → "File not found: <path>. Provide an absolute path or path relative to current directory."
- Spec parse error → "Could not parse <path> as OpenAPI: <parse error>. Is this a valid OpenAPI 3.x document?"
- Already has `.cli.json` → "Found existing .cli.json. Use --force to overwrite, or edit it directly."

### 1.3 `ocli --demo`

**Behavior:**

1. Embed `product-tests/testdata/specs/api.yaml` using `go:embed` at build time
2. When `--demo` flag is set:
   - Write embedded spec to a temp file
   - Generate an in-memory config pointing to it
   - Boot embedded runtime
   - Show catalog with explanation
3. Print header: "Demo mode — using built-in sample API. Run `ocli init <your-api>` to use your own."
4. All normal commands work against the demo catalog
5. `ocli --demo catalog list --format pretty` shows the full dynamic command tree

**Implementation:**
- `cmd/ocli/internal/demo/embed.go` holds the `//go:embed` directive
- Demo config is constructed programmatically, not from a file
- The `--demo` flag is a persistent flag on the root command
- Demo mode sets `--embedded` implicitly

### 1.4 Error Messages

**Principle:** Always show full technical details. Wrap errors with actionable context.

**Error format:**
```
Error: <what happened>
Cause: <technical details>
Suggestion: <what to try next>
```

**Examples:**

```
Error: cannot connect to runtime
Cause: Get "http://127.0.0.1:8765/v1/catalog/effective": dial tcp 127.0.0.1:8765: connection refused
Suggestion: Start the runtime with `oclird`, or use `ocli --embedded --config .cli.json`
```

```
Error: no configuration found
Cause: no .cli.json in current directory or parent directories, and no --config flag provided
Suggestion: Run `ocli init <url>` to create a config, or try `ocli --demo` for a demo
```

```
Error: invalid configuration
Cause: .cli.json: sources.myapi.uri: required field missing
Suggestion: Each source needs a "uri" field. See: https://open-cli.dev/configuration
```

---

## Stream 2: main.go Refactoring

### 2.1 Target Structure

```
cmd/ocli/
├── main.go                  (~200 lines)
├── proc_linux.go            (existing, unchanged)
├── proc_other.go            (existing, unchanged)
└── internal/
    ├── runtime/
    │   ├── client.go        # newRuntimeClient, embedded/HTTP client creation
    │   ├── deployment.go    # resolveRuntimeDeployment, mode resolution logic
    │   └── session.go       # localSessionHandshake, heartbeat, cleanup
    ├── auth/
    │   ├── token.go         # resolveRuntimeToken, OAuth token acquisition
    │   └── config.go        # resolveAuthConfig, credential source resolution
    ├── config/
    │   └── resolve.go       # resolveCommandOptions, config file discovery
    ├── commands/
    │   ├── root.go          # NewRootCommand (thin orchestrator)
    │   ├── dynamic.go       # addDynamicToolCommands, buildToolCommand
    │   ├── catalog.go       # catalog list/explain subcommands
    │   ├── init.go          # ocli init command
    │   └── demo.go          # ocli --demo handling
    └── demo/
        └── embed.go         # go:embed for demo API spec
```

### 2.2 Extraction Plan

**Phase 1: Extract `runtime/`**
- Move `newRuntimeClient()` → `runtime/client.go`
- Move `resolveRuntimeDeployment()` → `runtime/deployment.go`
- Move `localSessionHandshake()`, heartbeat logic, cleanup → `runtime/session.go`
- These functions are already logically grouped in main.go (~400 lines)

**Phase 2: Extract `auth/`**
- Move `resolveRuntimeToken()` → `auth/token.go`
- Move `resolveAuthConfig()` → `auth/config.go`
- ~200 lines

**Phase 3: Extract `config/`**
- Move `resolveCommandOptions()` → `config/resolve.go`
- Move config file discovery logic → `config/resolve.go`
- ~150 lines

**Phase 4: Extract `commands/`**
- Move `addDynamicToolCommands()` → `commands/dynamic.go`
- Move catalog subcommands → `commands/catalog.go`
- Build new `commands/init.go` and `commands/demo.go`
- Move `NewRootCommand()` → `commands/root.go` (thin version)
- ~500 lines moved, new code for init/demo

**Phase 5: New `demo/`**
- `embed.go` with `//go:embed` for the test API spec

### 2.3 Dependency Graph

```
main.go
  └── commands/root.go
        ├── commands/dynamic.go
        ├── commands/catalog.go
        ├── commands/init.go
        ├── commands/demo.go
        │     └── demo/embed.go
        ├── config/resolve.go
        ├── runtime/client.go
        │     └── runtime/deployment.go
        │     └── runtime/session.go
        └── auth/token.go
              └── auth/config.go
```

No circular dependencies. Each package depends only on packages below it in the graph.

### 2.4 Rules

- Every exported function gets a doc comment
- No package exports anything that isn't needed by its consumers
- Existing behavior is preserved exactly — this is a pure structural refactor for phases 1-3
- Product tests (`product-tests/`) serve as integration tests and must pass after each phase
- New features (init, demo) are added in phase 4-5 after the structure is clean

---

## Stream 3: npm Package Hardening

### 3.1 `install.js` Rewrite

**Retry logic:**
```javascript
async function downloadWithRetry(url, maxRetries = 3) {
  for (let attempt = 1; attempt <= maxRetries; attempt++) {
    try {
      return await download(url);
    } catch (err) {
      if (attempt === maxRetries) throw err;
      const delay = Math.pow(2, attempt - 1) * 1000; // 1s, 2s, 4s
      console.error(`Attempt ${attempt}/${maxRetries} failed: ${err.message}. Retrying in ${delay/1000}s...`);
      await sleep(delay);
    }
  }
}
```

**Timeout:**
- 30 second timeout per download attempt
- Configurable via `OPEN_CLI_DOWNLOAD_TIMEOUT` environment variable
- On timeout: clear error message with suggestion to increase timeout

**Post-download validation:**
- After extraction, verify both `ocli` and `oclird` binaries exist
- Run `ocli --version` to verify binary is executable and not corrupted
- If validation fails: remove partial install, print clear error, exit 1

**Success messaging:**
```
open-cli: downloading v0.1.0 for darwin-arm64...
open-cli: ✓ installed ocli and oclird (v0.1.0, darwin-arm64)
open-cli: run `ocli --help` to get started
```

**Signal handlers:**
```javascript
const cleanup = () => { /* remove temp dir if it exists */ };
process.on('SIGINT', () => { cleanup(); process.exit(130); });
process.on('SIGTERM', () => { cleanup(); process.exit(143); });
```

### 3.2 Wrapper Script Improvements (`bin/ocli.js`, `bin/oclird.js`)

**Pre-flight check:**
```javascript
const bin = path.join(__dirname, name + ext);
if (!fs.existsSync(bin)) {
  console.error(`open-cli: binary not found at ${bin}`);
  console.error(`open-cli: run "npm install -g @sbuglione/open-cli" to reinstall`);
  process.exit(1);
}
```

**Differentiated error handling:**
```javascript
try {
  execFileSync(bin, process.argv.slice(2), { stdio: "inherit", windowsHide: true });
} catch (e) {
  if (e.status !== null) process.exit(e.status);
  if (e.code === 'EACCES') {
    console.error(`open-cli: permission denied on ${bin}`);
    console.error(`open-cli: try: chmod +x ${bin}`);
  } else {
    console.error(`open-cli: failed to execute ${bin}: ${e.message}`);
  }
  process.exit(1);
}
```

### 3.3 `package.json` Updates

Add:
```json
{
  "files": ["bin/ocli.js", "bin/oclird.js", "install.js", "README.md"],
  "engines": { "node": ">=16", "npm": ">=5.2.0" }
}
```

### 3.4 `npm/README.md` Rewrite

Structure:
1. **What is open-cli** — 2-sentence description
2. **Platform support** — table: OS × arch
3. **Installation** — `npm install -g @sbuglione/open-cli`
4. **What happens during install** — explains postinstall binary download
5. **Quick start** — 3 commands: install → init → catalog list
6. **Troubleshooting** — common errors with solutions
7. **Configuration** — link to full docs
8. **License** — GPL-3.0

---

## Stream 4: Documentation Updates

### 4.1 `website/docs/getting-started/installation.md`

**Current (WRONG):**
> "This repository uses source-based installation. There are no packaged installers or pre-built release binaries."

**New structure:**
1. **npm (Recommended)** — `npm install -g @sbuglione/open-cli`
2. **Binary download** — Links to GitHub Releases, platform table
3. **From source** — `go install` instructions for contributors
4. **Verify installation** — `ocli --version`
5. **Platform support** — matrix table

### 4.2 `website/docs/getting-started/quickstart.md`

**Changes:**
- Replace `./bin/ocli` with `ocli` throughout
- Add `ocli --demo` as zero-config tryout step before requiring a config
- Add `ocli init` as the config creation method
- Flow: install → `ocli --demo` → `ocli init <your-api>` → explore catalog → execute tools

### 4.3 Root `README.md`

- Add `ocli --demo` mention in getting started
- Add `ocli init` in workflow
- Keep existing installation section (already correct from v0.0.1 work)

### 4.4 `spec/examples/` Annotations

- Add `spec/examples/README.md` explaining each example config
- Add `spec/examples/minimal.cli.json` — absolute minimum viable config with comments
- Existing examples remain unchanged

---

## Non-Goals (Explicitly Out of Scope)

- Color/emoji output — user chose full technical details, no cosmetic styling
- Interactive TUI — stay with standard CLI patterns
- Homebrew formula — can be added later
- Platform-specific npm packages (optionalDependencies pattern) — keep single-package postinstall
- Changes to `oclird` — daemon is not part of UX overhaul
- Changes to `pkg/` packages — only `cmd/ocli/` is being refactored
- New test infrastructure — existing product-tests are sufficient for validation

---

## Success Criteria

1. `npm install -g @sbuglione/open-cli` → runs `ocli --version` successfully on all 6 platforms
2. `ocli` with no args shows helpful welcome, not a crash
3. `ocli --help` works without any config or runtime
4. `ocli --demo` shows a working catalog with no setup
5. `ocli init <url>` creates a valid `.cli.json` from any OpenAPI spec URL
6. `cmd/ocli/main.go` is under 250 lines
7. All existing product-tests pass
8. Website installation.md matches reality
9. npm install.js retries failed downloads and validates binaries
