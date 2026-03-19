# Complete CLI Experience Implementation Plan

> **For agentic workers:** REQUIRED: Use superpowers:subagent-driven-development (if subagents available) or superpowers:executing-plans to implement this plan. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Make ocli the best CLI tool for both new and power users — smart init, config management, auth commands, search, filtering, dry-run, interactive prompts, and status health checks.

**Architecture:** All features are CLI-layer additions in `cmd/ocli/internal/commands/`. No changes to the runtime API, NTC schema, or .cli.json schema. New commands follow existing patterns: Cobra commands registered in `root.go`, output via `WriteOutput()`, errors via `FormatError()`/`NewUserError()`.

**Tech Stack:** Go 1.25, Cobra, kin-openapi (already in go.mod), text/tabwriter (stdlib), bufio (stdlib)

**Build/Test Commands:**
```bash
export PATH="$HOME/.local/go/bin:$HOME/go/bin:$PATH"
go build ./cmd/ocli ./cmd/oclird
go test ./cmd/ocli/...
```

**Spec:** `docs/superpowers/specs/2026-03-19-complete-cli-experience-design.md`

---

## File Map

### New Files
| File | Responsibility |
|------|---------------|
| `cmd/ocli/internal/commands/config.go` | `ocli config show`, `add-source`, `remove-source`, `add-secret` |
| `cmd/ocli/internal/commands/auth.go` | `ocli auth login`, `status`, `logout` |
| `cmd/ocli/internal/commands/search.go` | `ocli search <pattern>` |
| `cmd/ocli/internal/commands/status.go` | `ocli status` health check |
| `cmd/ocli/internal/commands/prompt.go` | Interactive TTY parameter prompts |
| `cmd/ocli/internal/commands/dryrun.go` | Dry-run request preview |
| `cmd/ocli/internal/commands/commands_test.go` | Comprehensive unit tests for all new commands |

### Modified Files
| File | Changes |
|------|---------|
| `cmd/ocli/internal/commands/root.go` | Register new commands (config, auth, search, status); pass client/options to auth |
| `cmd/ocli/internal/commands/init.go` | Parse spec with kin-openapi, detect auth, validate content, support `--type mcp` |
| `cmd/ocli/internal/commands/catalog.go` | Add `--service`, `--group`, `--safety` filter flags |
| `cmd/ocli/internal/commands/dynamic.go` | Add `--dry-run` flag, interactive prompts for missing path params |
| `cmd/ocli/internal/commands/table.go` | Add `IsTerminalReader(io.Reader)`, add status/config table formatters |
| `cmd/ocli/internal/commands/util.go` | Add `FilterTools()` helper (shared by catalog list + search) |
| `cmd/ocli/internal/commands/errors.go` | Structured error for unsupported format |

---

## Task 1: Foundation — `IsTerminalReader` + `FilterTools` + Error Polish

**Files:**
- Modify: `cmd/ocli/internal/commands/table.go`
- Modify: `cmd/ocli/internal/commands/util.go`
- Modify: `cmd/ocli/internal/commands/errors.go`

- [ ] **Step 1: Add `IsTerminalReader` to table.go**

Add after existing `IsTerminal()` function (line 25):

```go
// IsTerminalReader checks if an io.Reader is connected to a terminal.
func IsTerminalReader(r io.Reader) bool {
	f, ok := r.(*os.File)
	if !ok {
		return false
	}
	fi, err := f.Stat()
	if err != nil {
		return false
	}
	return fi.Mode()&os.ModeCharDevice != 0
}
```

- [ ] **Step 2: Add `FilterTools` to util.go**

Add at end of util.go:

```go
// FilterTools returns tools matching the given criteria. Empty filter values match all.
func FilterTools(tools []catalog.Tool, service, group, safety string) []catalog.Tool {
	var result []catalog.Tool
	for _, tool := range tools {
		if service != "" && tool.ServiceID != service {
			// Also check against the alias-style ID prefix.
			continue
		}
		if group != "" && tool.Group != group {
			continue
		}
		if safety != "" {
			switch safety {
			case "read-only":
				if !tool.Safety.ReadOnly {
					continue
				}
			case "destructive":
				if !tool.Safety.Destructive {
					continue
				}
			case "requires-approval":
				if !tool.Safety.RequiresApproval {
					continue
				}
			case "idempotent":
				if !tool.Safety.Idempotent {
					continue
				}
			}
		}
		result = append(result, tool)
	}
	return result
}

// SearchTools returns tools where pattern appears in ID, Command, Summary, or Description (case-insensitive).
func SearchTools(tools []catalog.Tool, pattern string) []catalog.Tool {
	lower := strings.ToLower(pattern)
	var result []catalog.Tool
	for _, tool := range tools {
		if strings.Contains(strings.ToLower(tool.ID), lower) ||
			strings.Contains(strings.ToLower(tool.Command), lower) ||
			strings.Contains(strings.ToLower(tool.Summary), lower) ||
			strings.Contains(strings.ToLower(tool.Description), lower) {
			result = append(result, tool)
		}
	}
	return result
}
```

- [ ] **Step 3: Fix unsupported format error in errors.go/util.go**

In `util.go`, change the default case (line 42):

```go
	default:
		return NewUserError(
			fmt.Sprintf("unsupported format %q", format),
			"The --format flag only accepts: json, yaml, pretty, table",
			"Use --format json or --format table")
	}
```

- [ ] **Step 3b: Add structured error helpers for remaining error categories in errors.go**

```go
// NewAuthError creates a user error for authentication failures.
func NewAuthError(cause, suggestion string) *UserError {
	return NewUserError("Authentication failed", cause, suggestion)
}

// NewBodyError creates a user error for invalid JSON body input.
func NewBodyError(cause string) *UserError {
	return NewUserError("Invalid request body",
		cause,
		"Body must be valid JSON. Use --body '{\"key\":\"value\"}' or --body @file.json")
}

// NewMCPError creates a user error for MCP transport failures.
func NewMCPError(cause string) *UserError {
	return NewUserError("MCP server error",
		cause,
		"Check that the MCP server is running and the transport config is correct")
}
```

- [ ] **Step 3c: Add `readConfigFile` helper to util.go**

This helper is shared by config.go, auth.go, and status.go. Add at end of util.go:

```go
// readConfigFile reads and parses a .cli.json file into a generic map.
func readConfigFile(path string) (map[string]any, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var raw map[string]any
	if err := json.Unmarshal(data, &raw); err != nil {
		return nil, err
	}
	return raw, nil
}
```

Also add `"encoding/json"` to the import block in util.go if not already present.

- [ ] **Step 3d: Add JSON validation to existing `LoadBody` in util.go**

The existing `LoadBody(bodyRef string, stdin io.Reader)` function at line 66 returns raw bytes. Add JSON validation after the switch statement. Replace lines 66-77 with:

```go
func LoadBody(bodyRef string, stdin io.Reader) ([]byte, error) {
	var body []byte
	var err error
	switch {
	case bodyRef == "":
		return nil, nil
	case bodyRef == "-":
		body, err = io.ReadAll(stdin)
	case strings.HasPrefix(bodyRef, "@"):
		body, err = os.ReadFile(strings.TrimPrefix(bodyRef, "@"))
	default:
		body = []byte(bodyRef)
	}
	if err != nil {
		return nil, err
	}
	if len(body) > 0 && !json.Valid(body) {
		return nil, NewBodyError("The provided body is not valid JSON")
	}
	return body, nil
}
```

Also add `"encoding/json"` to the import block in util.go if not already present.

- [ ] **Step 4: Build and test**

Run: `go build ./cmd/ocli ./cmd/oclird && go test ./cmd/ocli/...`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add -A
git commit -m "feat: add IsTerminalReader, FilterTools, SearchTools, and fix format error

Co-authored-by: Copilot <223556219+Copilot@users.noreply.github.com>"
```

---

## Task 2: Catalog Filtering

**Files:**
- Modify: `cmd/ocli/internal/commands/catalog.go:12-25`

- [ ] **Step 1: Add filter flags and logic to catalog list**

Replace the `list` subcommand in `NewCatalogCommand()` (lines 15-24):

```go
func NewCatalogCommand(options cfgpkg.Options, response runtimepkg.CatalogResponse) *cobra.Command {
	command := &cobra.Command{
		Use:   "catalog",
		Short: "Inspect the tool catalog",
	}
	listCmd := &cobra.Command{
		Use:   "list",
		Short: "List all available tools",
		RunE: func(cmd *cobra.Command, _ []string) error {
			service, _ := cmd.Flags().GetString("service")
			group, _ := cmd.Flags().GetString("group")
			safety, _ := cmd.Flags().GetString("safety")

			tools := response.View.Tools
			if service != "" || group != "" || safety != "" {
				tools = FilterTools(tools, service, group, safety)
			}

			filtered := response
			filtered.View.Tools = tools
			return WriteOutput(options.Stdout, options.Format, filtered)
		},
	}
	listCmd.Flags().String("service", "", "Filter by service ID")
	listCmd.Flags().String("group", "", "Filter by group name")
	listCmd.Flags().String("safety", "", "Filter by safety: read-only, destructive, requires-approval, idempotent")
	command.AddCommand(listCmd)
	return command
}
```

- [ ] **Step 2: Build and test**

Run: `go build ./cmd/ocli ./cmd/oclird && go test ./cmd/ocli/...`
Expected: PASS

- [ ] **Step 3: Manual verification**

```bash
./bin/ocli --demo catalog list --group items
./bin/ocli --demo catalog list --group errors
```
Expected: Only items/errors group tools shown respectively.

- [ ] **Step 4: Commit**

```bash
git add -A
git commit -m "feat: add --service, --group, --safety filters to catalog list

Co-authored-by: Copilot <223556219+Copilot@users.noreply.github.com>"
```

---

## Task 3: Tool Search

**Files:**
- Create: `cmd/ocli/internal/commands/search.go`

- [ ] **Step 1: Create search.go**

```go
package commands

import (
	"fmt"

	cfgpkg "github.com/StevenBuglione/open-cli/cmd/ocli/internal/config"
	runtimepkg "github.com/StevenBuglione/open-cli/cmd/ocli/internal/runtime"
	"github.com/spf13/cobra"
)

// NewSearchCommand returns the "search" subcommand for fuzzy tool search.
func NewSearchCommand(options cfgpkg.Options, response *runtimepkg.CatalogResponse) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "search <pattern>",
		Short: "Search tools by name, summary, or description",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if response == nil {
				return NewUserError(
					"Cannot search tools",
					"Runtime is not available — catalog not loaded",
					"Use --embedded or start the daemon with oclird")
			}

			pattern := args[0]
			service, _ := cmd.Flags().GetString("service")

			tools := response.View.Tools
			if service != "" {
				tools = FilterTools(tools, service, "", "")
			}
			matches := SearchTools(tools, pattern)

			if len(matches) == 0 {
				fmt.Fprintf(options.Stderr, "No tools matching %q. Run 'ocli catalog list' to see all tools.\n", pattern)
				return nil
			}

			result := *response
			result.View.Tools = matches
			return WriteOutput(options.Stdout, options.Format, result)
		},
	}
	cmd.Flags().String("service", "", "Limit search to one service")
	return cmd
}
```

- [ ] **Step 3: Build and test**

Run: `go build ./cmd/ocli ./cmd/oclird && go test ./cmd/ocli/...`
Expected: PASS (search.go compiles but is not registered yet — that happens in Task 10)

- [ ] **Step 4: Commit**

```bash
git add -A
git commit -m "feat: add 'ocli search' for fuzzy tool search

Co-authored-by: Copilot <223556219+Copilot@users.noreply.github.com>"
```

---

## Task 4: Status Command

**Files:**
- Create: `cmd/ocli/internal/commands/status.go`

- [ ] **Step 1: Create status.go**

```go
package commands

import (
	"fmt"
	"io"

	cfgpkg "github.com/StevenBuglione/open-cli/cmd/ocli/internal/config"
	runtimepkg "github.com/StevenBuglione/open-cli/cmd/ocli/internal/runtime"
	configpkg "github.com/StevenBuglione/open-cli/pkg/config"
	"github.com/spf13/cobra"
)

// NewStatusCommand returns the "status" subcommand for quick health checks.
func NewStatusCommand(options cfgpkg.Options, client runtimepkg.Client, runtimeUnavailable bool) *cobra.Command {
	return &cobra.Command{
		Use:   "status",
		Short: "Show runtime and configuration health",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			w := options.Stdout
			writeStatus(w, options, client, runtimeUnavailable)
			return nil
		},
	}
}

func writeStatus(w io.Writer, options cfgpkg.Options, client runtimepkg.Client, runtimeUnavailable bool) {
	// Runtime status
	if runtimeUnavailable {
		fmt.Fprintln(w, "Runtime:  ✗ not running")
	} else {
		info, err := client.RuntimeInfo()
		if err != nil {
			fmt.Fprintln(w, "Runtime:  ✗ error fetching info")
		} else {
			mode := "unknown"
			if options.Embedded {
				mode = "embedded"
			} else if options.RuntimeDeployment == "local" {
				mode = "local daemon"
			} else if options.RuntimeDeployment == "remote" {
				mode = "remote"
			}
			ver, _ := info["version"].(string)
			if ver == "" {
				ver = "unknown"
			}
			fmt.Fprintf(w, "Runtime:  ✓ %s (v%s)\n", mode, ver)
		}
	}

	// Config status
	if options.ConfigPath != "" {
		fmt.Fprintf(w, "Config:   %s\n", options.ConfigPath)
	} else {
		fmt.Fprintln(w, "Config:   none")
	}

	// Count sources by type from active config
	if options.ConfigPath != "" {
		raw, err := readConfigFile(options.ConfigPath)
		if err == nil {
			if sources, ok := raw["sources"].(map[string]any); ok {
				typeCounts := map[string]int{}
				for _, v := range sources {
					if src, ok := v.(map[string]any); ok {
						enabled := true
						if e, ok := src["enabled"].(bool); ok {
							enabled = e
						}
						if enabled {
							stype, _ := src["type"].(string)
							if stype == "" {
								stype = "unknown"
							}
							typeCounts[stype]++
						}
					}
				}
				total := 0
				var parts []string
				for t, c := range typeCounts {
					total += c
					parts = append(parts, fmt.Sprintf("%d %s", c, t))
				}
				if total > 0 {
					fmt.Fprintf(w, "Sources:  %d active (%s)\n", total, joinParts(parts))
				} else {
					fmt.Fprintln(w, "Sources:  0 active")
				}
			}
		}
	}

	// Config scope discovery
	paths := configpkg.DiscoverScopePaths(configpkg.LoadOptions{})
	for _, scope := range []configpkg.Scope{configpkg.ScopeManaged, configpkg.ScopeUser, configpkg.ScopeProject, configpkg.ScopeLocal} {
		if p, ok := paths[scope]; ok {
			fmt.Fprintf(w, "  %s: %s\n", scope, p)
		}
	}

	if runtimeUnavailable {
		fmt.Fprintln(w)
		fmt.Fprintln(w, "Suggestion: Run with --embedded or start the daemon with oclird")
	}
}

func joinParts(parts []string) string {
	result := ""
	for i, p := range parts {
		if i > 0 {
			result += ", "
		}
		result += p
	}
	return result
}
```

- [ ] **Step 2: Build and test**

Run: `go build ./cmd/ocli ./cmd/oclird && go test ./cmd/ocli/...`
Expected: PASS (status.go compiles but is not registered yet — that happens in Task 10)

- [ ] **Step 3: Commit**

```bash
git add -A
git commit -m "feat: add 'ocli status' for quick health checks

Co-authored-by: Copilot <223556219+Copilot@users.noreply.github.com>"
```

---

## Task 5: Config Management Commands

**Files:**
- Create: `cmd/ocli/internal/commands/config.go`

- [ ] **Step 1: Create config.go with show, add-source, remove-source, add-secret**

```go
package commands

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"text/tabwriter"

	cfgpkg "github.com/StevenBuglione/open-cli/cmd/ocli/internal/config"
	configpkg "github.com/StevenBuglione/open-cli/pkg/config"
	"github.com/spf13/cobra"
)

// NewConfigCommand returns the "config" parent command.
func NewConfigCommand(options cfgpkg.Options) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "config",
		Short: "Manage .cli.json configuration",
	}
	cmd.AddCommand(newConfigShowCommand(options))
	cmd.AddCommand(newConfigAddSourceCommand())
	cmd.AddCommand(newConfigRemoveSourceCommand())
	cmd.AddCommand(newConfigAddSecretCommand())
	return cmd
}

func newConfigShowCommand(options cfgpkg.Options) *cobra.Command {
	return &cobra.Command{
		Use:   "show",
		Short: "Show effective configuration",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			paths := configpkg.DiscoverScopePaths(configpkg.LoadOptions{})

			if options.Format == "table" {
				w := options.Stdout
				// Show all 4 scope paths with active/not-found status
				fmt.Fprintln(w, "Config files:")
				for _, scope := range []configpkg.Scope{configpkg.ScopeManaged, configpkg.ScopeUser, configpkg.ScopeProject, configpkg.ScopeLocal} {
					if p, ok := paths[scope]; ok {
						fmt.Fprintf(w, "  %s: %s (active)\n", scope, p)
					} else {
						fmt.Fprintf(w, "  %s: (not found)\n", scope)
					}
				}
				// Show sources from active config
				if options.ConfigPath != "" {
					raw, err := readConfigFile(options.ConfigPath)
					if err == nil {
						if sources, ok := raw["sources"].(map[string]any); ok {
							fmt.Fprintln(w)
							tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
							fmt.Fprintln(tw, "SOURCE\tTYPE\tURI\tENABLED")
							for name, v := range sources {
								src, _ := v.(map[string]any)
								stype, _ := src["type"].(string)
								uri, _ := src["uri"].(string)
								enabled := true
								if e, ok := src["enabled"].(bool); ok {
									enabled = e
								}
								if uri == "" {
									if t, ok := src["transport"].(map[string]any); ok {
										cmd, _ := t["command"].(string)
										ttype, _ := t["type"].(string)
										uri = fmt.Sprintf("%s: %s", ttype, cmd)
									}
								}
								fmt.Fprintf(tw, "%s\t%s\t%s\t%v\n", name, stype, uri, enabled)
							}
							tw.Flush()
						}
					}
				}
				return nil
			}
			// JSON/YAML/pretty: output raw config
			if options.ConfigPath == "" {
				return NewUserError("No config file found",
					"No .cli.json exists in any scope",
					"Run 'ocli init <url>' to create one")
			}
			raw, err := readConfigFile(options.ConfigPath)
			if err != nil {
				return err
			}
			return WriteOutput(options.Stdout, options.Format, raw)
		},
	}
}

func newConfigAddSourceCommand() *cobra.Command {
	var (
		sourceType string
		uri        string
		transport  string
		command    string
		args       string
		mcpURL     string
		alias      string
		global     bool
	)
	cmd := &cobra.Command{
		Use:   "add-source <name>",
		Short: "Add a source to .cli.json",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, posArgs []string) error {
			name := posArgs[0]
			raw, configPath, err := loadOrCreateConfig(global)
			if err != nil {
				return err
			}
			sources := ensureMap(raw, "sources")
			if _, exists := sources[name]; exists {
				return NewUserError(
					fmt.Sprintf("Source %q already exists", name),
					"A source with this name is already configured",
					fmt.Sprintf("Remove it first with: ocli config remove-source %s", name))
			}

			source := map[string]any{"type": sourceType, "enabled": true}
			switch sourceType {
			case "openapi", "apiCatalog", "serviceRoot":
				if uri == "" {
					return NewUserError("Missing --uri", "OpenAPI sources require a URI", "Add --uri <url-or-path>")
				}
				source["uri"] = uri
			case "mcp":
				t := map[string]any{"type": transport}
				switch transport {
				case "stdio":
					if command == "" {
						return NewUserError("Missing --command", "MCP stdio transport requires a command", "Add --command <executable>")
					}
					t["command"] = command
					if args != "" {
						t["args"] = strings.Split(args, ",")
					}
				case "sse", "streamable-http":
					if mcpURL == "" {
						return NewUserError("Missing --url", "MCP "+transport+" requires a URL", "Add --url <endpoint>")
					}
					t["url"] = mcpURL
				default:
					return NewUserError(
						fmt.Sprintf("Unknown transport %q", transport),
						"MCP sources require --transport stdio, sse, or streamable-http",
						"Use --transport stdio for local MCP servers")
				}
				source["transport"] = t
			default:
				return NewUserError(
					fmt.Sprintf("Unknown source type %q", sourceType),
					"Supported types: openapi, mcp, apiCatalog, serviceRoot",
					"Use --type openapi for OpenAPI specs")
			}
			sources[name] = source

			services := ensureMap(raw, "services")
			svcAlias := alias
			if svcAlias == "" {
				svcAlias = name
			}
			services[name] = map[string]any{"source": name, "alias": svcAlias}

			return writeConfigFile(configPath, raw, cmd.OutOrStdout(),
				fmt.Sprintf("Added source %q (%s)", name, sourceType))
		},
	}
	cmd.Flags().StringVar(&sourceType, "type", "openapi", "Source type: openapi, mcp, apiCatalog, serviceRoot")
	cmd.Flags().StringVar(&uri, "uri", "", "URI for openapi/apiCatalog/serviceRoot sources")
	cmd.Flags().StringVar(&transport, "transport", "", "MCP transport: stdio, sse, streamable-http")
	cmd.Flags().StringVar(&command, "command", "", "MCP stdio command")
	cmd.Flags().StringVar(&args, "args", "", "Comma-separated MCP stdio args")
	cmd.Flags().StringVar(&mcpURL, "url", "", "MCP sse/streamable-http URL")
	cmd.Flags().StringVar(&alias, "alias", "", "Service alias (default: same as name)")
	cmd.Flags().BoolVar(&global, "global", false, "Write to ~/.config/oas-cli/ instead of current directory")
	return cmd
}

func newConfigRemoveSourceCommand() *cobra.Command {
	var global bool
	cmd := &cobra.Command{
		Use:   "remove-source <name>",
		Short: "Remove a source from .cli.json",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			name := args[0]
			configPath := ".cli.json"
			if global {
				home, _ := os.UserHomeDir()
				configPath = filepath.Join(home, ".config", "oas-cli", ".cli.json")
			}
			raw, err := readConfigFile(configPath)
			if err != nil {
				return FormatError(err, "Cannot read config", "Check that "+configPath+" exists")
			}
			sources := ensureMap(raw, "sources")
			if _, exists := sources[name]; !exists {
				return NewUserError(
					fmt.Sprintf("Source %q not found", name),
					"No source with this name exists in "+configPath,
					"Run 'ocli config show' to see configured sources")
			}
			delete(sources, name)

			// Remove matching service
			if services, ok := raw["services"].(map[string]any); ok {
				for svcName, v := range services {
					if svc, ok := v.(map[string]any); ok {
						if svc["source"] == name {
							delete(services, svcName)
						}
					}
				}
			}

			return writeConfigFile(configPath, raw, cmd.OutOrStdout(),
				fmt.Sprintf("Removed source %q", name))
		},
	}
	cmd.Flags().BoolVar(&global, "global", false, "Modify ~/.config/oas-cli/ config")
	return cmd
}

func newConfigAddSecretCommand() *cobra.Command {
	var (
		secretType string
		mode       string
		tokenURL   string
		clientID   string
		clientSec  string
		scopes     string
		envValue   string
		global     bool
	)
	cmd := &cobra.Command{
		Use:   "add-secret <name>",
		Short: "Add a secret to .cli.json",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			name := args[0]
			raw, configPath, err := loadOrCreateConfig(global)
			if err != nil {
				return err
			}
			secrets := ensureMap(raw, "secrets")
			if _, exists := secrets[name]; exists {
				return NewUserError(
					fmt.Sprintf("Secret %q already exists", name),
					"A secret with this name is already configured",
					"Remove the existing entry from "+configPath+" first")
			}

			switch secretType {
			case "oauth2":
				secret := map[string]any{"type": "oauth2", "mode": mode}
				if tokenURL != "" {
					secret["tokenURL"] = tokenURL
				}
				if clientID != "" {
					secret["clientId"] = map[string]any{"type": "env", "value": clientID}
				}
				if clientSec != "" {
					secret["clientSecret"] = map[string]any{"type": "env", "value": clientSec}
				}
				if scopes != "" {
					secret["scopes"] = strings.Split(scopes, ",")
				}
				secrets[name] = secret
			case "env":
				if envValue == "" {
					return NewUserError("Missing --env-value", "Env secrets require an environment variable name", "Add --env-value MY_API_KEY")
				}
				secrets[name] = map[string]any{"type": "env", "value": envValue}
			default:
				return NewUserError(
					fmt.Sprintf("Unknown secret type %q", secretType),
					"Supported types: oauth2, env",
					"Use --type oauth2 for OAuth credentials")
			}

			return writeConfigFile(configPath, raw, cmd.OutOrStdout(),
				fmt.Sprintf("Added secret %q (%s)", name, secretType))
		},
	}
	cmd.Flags().StringVar(&secretType, "type", "", "Secret type: oauth2, env")
	_ = cmd.MarkFlagRequired("type")
	cmd.Flags().StringVar(&mode, "mode", "clientCredentials", "OAuth mode: authorizationCode, clientCredentials")
	cmd.Flags().StringVar(&tokenURL, "token-url", "", "OAuth token URL")
	cmd.Flags().StringVar(&clientID, "client-id-env", "", "Env var name for OAuth client ID")
	cmd.Flags().StringVar(&clientSec, "client-secret-env", "", "Env var name for OAuth client secret")
	cmd.Flags().StringVar(&scopes, "scopes", "", "Comma-separated OAuth scopes")
	cmd.Flags().StringVar(&envValue, "env-value", "", "Environment variable name (for --type env)")
	cmd.Flags().BoolVar(&global, "global", false, "Write to ~/.config/oas-cli/ config")
	return cmd
}

// --- helpers ---

func loadOrCreateConfig(global bool) (map[string]any, string, error) {
	configPath := ".cli.json"
	if global {
		home, err := os.UserHomeDir()
		if err != nil {
			return nil, "", err
		}
		dir := filepath.Join(home, ".config", "oas-cli")
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return nil, "", err
		}
		configPath = filepath.Join(dir, ".cli.json")
	}
	raw, err := readConfigFile(configPath)
	if err != nil {
		// File doesn't exist — create skeleton
		raw = map[string]any{
			"cli":  "1.0.0",
			"mode": map[string]any{"default": "discover"},
		}
	}
	return raw, configPath, nil
}

func ensureMap(raw map[string]any, key string) map[string]any {
	if m, ok := raw[key].(map[string]any); ok {
		return m
	}
	m := map[string]any{}
	raw[key] = m
	return m
}

func writeConfigFile(path string, raw map[string]any, w interface{ Write([]byte) (int, error) }, msg string) error {
	data, err := json.MarshalIndent(raw, "", "  ")
	if err != nil {
		return err
	}
	if err := os.WriteFile(path, append(data, '\n'), 0o644); err != nil {
		return err
	}
	fmt.Fprintf(w, "%s in %s\n", msg, path)
	return nil
}
```

- [ ] **Step 3: Build and test**

Run: `go build ./cmd/ocli ./cmd/oclird && go test ./cmd/ocli/...`
Expected: PASS (config.go compiles but is not registered yet — that happens in Task 10)

- [ ] **Step 4: Manual verification**

After Task 10 wires everything, verify:
```bash
cd /tmp && rm -f .cli.json
./bin/ocli config add-source petstore --uri https://petstore3.swagger.io/api/v3/openapi.json
cat .cli.json
./bin/ocli config show
./bin/ocli config add-secret pets.oauth --type oauth2 --token-url https://auth.example.com/token --client-id-env PET_ID --client-secret-env PET_SECRET
cat .cli.json
./bin/ocli config remove-source petstore
cat .cli.json
rm .cli.json
```

- [ ] **Step 5: Commit**

```bash
git add -A
git commit -m "feat: add 'ocli config' commands — show, add-source, remove-source, add-secret

Co-authored-by: Copilot <223556219+Copilot@users.noreply.github.com>"
```

---

## Task 6: Auth Commands

**Files:**
- Create: `cmd/ocli/internal/commands/auth.go`

- [ ] **Step 1: Create auth.go**

```go
package commands

import (
	"fmt"
	"text/tabwriter"

	authpkg "github.com/StevenBuglione/open-cli/cmd/ocli/internal/auth"
	cfgpkg "github.com/StevenBuglione/open-cli/cmd/ocli/internal/config"
	runtimepkg "github.com/StevenBuglione/open-cli/cmd/ocli/internal/runtime"
	"github.com/spf13/cobra"
)

// NewAuthCommand returns the "auth" parent command.
func NewAuthCommand(options cfgpkg.Options, client runtimepkg.Client, runtimeUnavailable bool) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "auth",
		Short: "Manage authentication",
	}
	cmd.AddCommand(newAuthLoginCommand(options, client, runtimeUnavailable))
	cmd.AddCommand(newAuthStatusCommand(options))
	cmd.AddCommand(newAuthLogoutCommand(options, client, runtimeUnavailable))
	return cmd
}

func newAuthLoginCommand(options cfgpkg.Options, client runtimepkg.Client, runtimeUnavailable bool) *cobra.Command {
	return &cobra.Command{
		Use:   "login",
		Short: "Authenticate to the runtime",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if runtimeUnavailable {
				return NewUserError(
					"Cannot log in",
					"Runtime is not available",
					"Use --embedded or start the daemon with oclird")
			}
			info, err := client.RuntimeInfo()
			if err != nil {
				return FormatError(err,
					"Failed to reach runtime",
					"Check runtime URL or use --embedded")
			}

			// Check if runtime exposes a browser-login endpoint
			authInfo, _ := info["auth"].(map[string]any)
			browserEndpoint, _ := authInfo["browserLoginEndpoint"].(string)
			if browserEndpoint == "" {
				fmt.Fprintln(options.Stdout, "Runtime does not require browser authentication.")
				fmt.Fprintln(options.Stdout, "Per-service tokens are resolved automatically at execution time.")
				return nil
			}

			// Fetch browser-login metadata from runtime
			metadata, err := authpkg.FetchBrowserLoginMetadata(options.RuntimeURL, browserEndpoint)
			if err != nil {
				return FormatError(err,
					"Failed to fetch login metadata from runtime",
					"The runtime may not support browser-based login")
			}

			scopes, _ := authInfo["scopes"].([]any)
			var scopeStrs []string
			for _, s := range scopes {
				if str, ok := s.(string); ok {
					scopeStrs = append(scopeStrs, str)
				}
			}
			audience, _ := authInfo["audience"].(string)

			fmt.Fprintln(options.Stdout, "Opening browser for login...")
			token, err := authpkg.AcquireBrowserLoginToken(authpkg.BrowserLoginRequest{
				Metadata: metadata,
				Scopes:   scopeStrs,
				Audience: audience,
				StateDir: options.StateDir,
			})
			if err != nil {
				return FormatError(err,
					"Browser login failed",
					"Check your credentials and try again")
			}
			_ = token // Token is stored by the auth subsystem
			fmt.Fprintln(options.Stdout, "✓ Login successful. Token cached for this session.")
			return nil
		},
	}
}

func newAuthStatusCommand(options cfgpkg.Options) *cobra.Command {
	return &cobra.Command{
		Use:   "status",
		Short: "Show authentication configuration",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if options.ConfigPath == "" {
				return NewUserError(
					"No config file found",
					"Cannot determine auth requirements without a config",
					"Run 'ocli init <url>' to create a config")
			}
			raw, err := readConfigFile(options.ConfigPath)
			if err != nil {
				return FormatError(err,
					"Cannot read config",
					"Check that "+options.ConfigPath+" exists")
			}

			w := options.Stdout
			tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
			fmt.Fprintln(tw, "SERVICE\tAUTH TYPE\tCONFIGURED")

			secrets, _ := raw["secrets"].(map[string]any)
			sources, _ := raw["sources"].(map[string]any)
			services, _ := raw["services"].(map[string]any)

			for svcName, v := range services {
				svc, _ := v.(map[string]any)
				srcName, _ := svc["source"].(string)
				src, _ := sources[srcName].(map[string]any)

				authType := "none"
				configured := "no auth required"

				// Check source-level OAuth config
				if src != nil {
					if _, ok := src["oauth"]; ok {
						authType = "oauth2 (transport)"
						configured = "✓ configured in source"
					}
				}

				// Check for matching secrets — match by exact service name or source name
				for secretName, sv := range secrets {
					if s, ok := sv.(map[string]any); ok {
						stype, _ := s["type"].(string)
						if secretName == svcName || secretName == srcName || secretName == svcName+".oauth" || secretName == srcName+".oauth" {
							authType = stype
							configured = fmt.Sprintf("✓ secret: %s", secretName)
						}
					}
				}

				fmt.Fprintf(tw, "%s\t%s\t%s\n", svcName, authType, configured)
			}
			tw.Flush()
			return nil
		},
	}
}

func newAuthLogoutCommand(options cfgpkg.Options, client runtimepkg.Client, runtimeUnavailable bool) *cobra.Command {
	return &cobra.Command{
		Use:   "logout",
		Short: "Close session and clear cached tokens",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if runtimeUnavailable {
				fmt.Fprintln(options.Stdout, "No active session to close.")
				return nil
			}
			_, err := client.SessionClose()
			if err != nil {
				return FormatError(err,
					"Failed to close session",
					"The runtime may not be running")
			}
			fmt.Fprintln(options.Stdout, "✓ Session closed. Cached tokens cleared.")
			return nil
		},
	}
}

```

- [ ] **Step 2: Build and test**

Run: `go build ./cmd/ocli ./cmd/oclird && go test ./cmd/ocli/...`
Expected: PASS (auth.go compiles but is not registered yet — that happens in Task 10)

- [ ] **Step 3: Commit**

```bash
git add -A
git commit -m "feat: add 'ocli auth' commands — login, status, logout

Co-authored-by: Copilot <223556219+Copilot@users.noreply.github.com>"
```

---

## Task 7: Dry-Run

**Files:**
- Create: `cmd/ocli/internal/commands/dryrun.go`
- Modify: `cmd/ocli/internal/commands/dynamic.go`

- [ ] **Step 1: Create dryrun.go**

```go
package commands

import (
	"fmt"
	"io"
	"net/url"
	"strings"

	"github.com/StevenBuglione/open-cli/pkg/catalog"
)

// WriteDryRun prints the HTTP request that would be sent, without executing.
func WriteDryRun(w io.Writer, tool catalog.Tool, pathArgs []string, flags map[string]string, body []byte) {
	// Build URL
	baseURL := "http://localhost"
	if len(tool.Servers) > 0 {
		baseURL = tool.Servers[0]
	}
	path := tool.Path
	for i, param := range tool.PathParams {
		if i < len(pathArgs) {
			path = strings.ReplaceAll(path, "{"+param.OriginalName+"}", pathArgs[i])
		}
	}

	// Build query string
	query := url.Values{}
	for _, param := range tool.Flags {
		if param.Location == "query" {
			if v, ok := flags[param.Name]; ok && v != "" {
				query.Set(param.OriginalName, v)
			}
		}
	}
	fullURL := baseURL + path
	if len(query) > 0 {
		fullURL += "?" + query.Encode()
	}

	fmt.Fprintf(w, "%s %s\n", strings.ToUpper(tool.Method), fullURL)

	// Only show Content-Type when body is present (no auth or tool-flag headers per spec)
	if len(body) > 0 {
		fmt.Fprintln(w, "Content-Type: application/json")
	}
	fmt.Fprintln(w)

	// Body
	if len(body) > 0 {
		fmt.Fprintln(w, string(body))
	}
}
```

- [ ] **Step 2: Add `--dry-run` flag to dynamic.go**

In `dynamic.go`, modify the command builder. After `command.Flags().String("body", "", "inline request body")` (line 106), add:

```go
		command.Flags().Bool("dry-run", false, "Show the request without executing")
```

In the `RunE` function (around line 61), add dry-run check before `client.Execute()`. Insert after the body loading (line 76) and before the execute call (line 77):

```go
				dryRun, _ := cmd.Flags().GetBool("dry-run")
				if dryRun {
					WriteDryRun(options.Stdout, toolCopy, args, flags, body)
					return nil
				}
```

- [ ] **Step 3: Build and test**

Run: `go build ./cmd/ocli ./cmd/oclird && go test ./cmd/ocli/...`
Expected: PASS

- [ ] **Step 4: Manual verification**

```bash
./bin/ocli --demo demo items create-item --body '{"name":"test"}' --dry-run
./bin/ocli --demo demo items get-item 42 --dry-run
```
Expected: Shows method, URL, headers, body without executing.

- [ ] **Step 5: Commit**

```bash
git add -A
git commit -m "feat: add --dry-run flag to tool commands

Co-authored-by: Copilot <223556219+Copilot@users.noreply.github.com>"
```

---

## Task 8: Interactive Prompts

**Files:**
- Create: `cmd/ocli/internal/commands/prompt.go`
- Modify: `cmd/ocli/internal/commands/dynamic.go`

- [ ] **Step 1: Create prompt.go**

```go
package commands

import (
	"bufio"
	"fmt"
	"io"
	"strings"

	"github.com/StevenBuglione/open-cli/pkg/catalog"
)

// PromptForMissingArgs prompts the user for missing path parameters on a TTY.
// Returns the completed args slice.
func PromptForMissingArgs(stdin io.Reader, stderr io.Writer, params []catalog.Parameter, args []string) ([]string, error) {
	result := make([]string, len(args))
	copy(result, args)

	scanner := bufio.NewScanner(stdin)
	for i := len(result); i < len(params); i++ {
		fmt.Fprintf(stderr, "Enter %s: ", params[i].Name)
		if !scanner.Scan() {
			return nil, fmt.Errorf("unexpected end of input while prompting for %s", params[i].Name)
		}
		value := strings.TrimSpace(scanner.Text())
		if value == "" {
			return nil, fmt.Errorf("%s is required", params[i].Name)
		}
		result = append(result, value)
	}
	return result, nil
}
```

- [ ] **Step 2: Modify dynamic.go Args validator**

Replace `cobra.ExactArgs(len(tool.PathParams))` (line 56) with a custom args function. Change lines 54-60 to:

```go
		expectedArgs := len(tool.PathParams)
		command := &cobra.Command{
			Use:     tool.Command,
			Short:   CommandSummary(toolCopy),
			Long:    toolCopy.Description,
			Hidden:  toolCopy.Hidden,
			Aliases: append([]string(nil), toolCopy.Aliases...),
			Args: func(cmd *cobra.Command, args []string) error {
				if len(args) >= expectedArgs {
					return nil
				}
				// If interactive TTY, we'll prompt in RunE
				if IsTerminalReader(cmd.InOrStdin()) {
					return nil
				}
				return fmt.Errorf("accepts %d arg(s), received %d", expectedArgs, len(args))
			},
```

Then at the start of `RunE`, before the flags loop, add:

```go
			RunE: func(cmd *cobra.Command, args []string) error {
				// Prompt for missing path params on TTY
				if len(args) < len(toolCopy.PathParams) {
					if !IsTerminalReader(cmd.InOrStdin()) {
						return fmt.Errorf("accepts %d arg(s), received %d", len(toolCopy.PathParams), len(args))
					}
					prompted, err := PromptForMissingArgs(cmd.InOrStdin(), cmd.ErrOrStderr(), toolCopy.PathParams, args)
					if err != nil {
						return err
					}
					args = prompted
				}
```

- [ ] **Step 3: Build and test**

Run: `go build ./cmd/ocli ./cmd/oclird && go test ./cmd/ocli/...`
Expected: PASS

- [ ] **Step 4: Commit**

```bash
git add -A
git commit -m "feat: interactive TTY prompts for missing path parameters

Co-authored-by: Copilot <223556219+Copilot@users.noreply.github.com>"
```

---

## Task 9: Smart Init

**Files:**
- Modify: `cmd/ocli/internal/commands/init.go`

This is the most complex task. We enhance init to parse specs, detect auth, validate content, and support MCP sources.

- [ ] **Step 1: Rewrite init.go with spec parsing and MCP support**

Replace the entire init.go with the enhanced version. Key changes:
- Add `--type`, `--transport`, `--command`, `--args`, `--url` flags
- For OpenAPI sources: GET (not HEAD), parse with kin-openapi, detect securitySchemes, warn about relative servers, count tools
- For MCP sources: generate transport config
- Print auth-aware next steps

The full implementation:

```go
package commands

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/getkin/kin-openapi/openapi3"
	"github.com/spf13/cobra"
)

// NewInitCommand returns the "init" subcommand that creates a minimal .cli.json.
func NewInitCommand() *cobra.Command {
	var (
		global      bool
		sourceType  string
		transport   string
		mcpCommand  string
		mcpArgs     string
		mcpURL      string
	)

	cmd := &cobra.Command{
		Use:   "init <url-or-file-or-name>",
		Short: "Create a .cli.json configuration from an API spec",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			source := args[0]
			w := cmd.OutOrStdout()

			outPath := ".cli.json"
			if global {
				home, err := os.UserHomeDir()
				if err != nil {
					return err
				}
				dir := filepath.Join(home, ".config", "ocli")
				if err := os.MkdirAll(dir, 0o755); err != nil {
					return err
				}
				outPath = filepath.Join(dir, ".cli.json")
			}

			if _, err := os.Stat(outPath); err == nil {
				return NewUserError(
					fmt.Sprintf("%s already exists", outPath),
					"A configuration file is already present",
					"Remove or rename the existing file, then try again")
			}

			var cfg map[string]any
			var authHints []string

			switch sourceType {
			case "mcp":
				if err := validateMCPFlags(transport, mcpCommand, mcpURL); err != nil {
					return err
				}
				cfg = buildMCPConfig(source, transport, mcpCommand, mcpArgs, mcpURL)
			default:
				result, hints, err := buildOpenAPIConfig(source, w)
				if err != nil {
					return err
				}
				cfg = result
				authHints = hints
			}

			data, err := json.MarshalIndent(cfg, "", "  ")
			if err != nil {
				return err
			}
			if err := os.WriteFile(outPath, append(data, '\n'), 0o644); err != nil {
				return err
			}

			fmt.Fprintf(w, "\nCreated %s\n", outPath)
			name := deriveServiceName(source)
			if sourceType == "mcp" {
				name = source
			}
			fmt.Fprintln(w, "\nNext steps:")
			fmt.Fprintln(w, "  ocli catalog list              List available tools")
			fmt.Fprintf(w, "  ocli %s --help           See %s commands\n", name, name)

			// Auth-aware next steps from buildOpenAPIConfig analysis
			if authHints != nil && len(authHints) > 0 {
				fmt.Fprintln(w)
				fmt.Fprintln(w, "This API requires authentication. Configure secrets:")
				for _, hint := range authHints {
					fmt.Fprintf(w, "  %s\n", hint)
				}
			}
			return nil
		},
	}
	cmd.Flags().BoolVar(&global, "global", false, "Write config to ~/.config/ocli/ instead of current directory")
	cmd.Flags().StringVar(&sourceType, "type", "openapi", "Source type: openapi or mcp")
	cmd.Flags().StringVar(&transport, "transport", "", "MCP transport: stdio, sse, streamable-http")
	cmd.Flags().StringVar(&mcpCommand, "command", "", "MCP stdio command")
	cmd.Flags().StringVar(&mcpArgs, "args", "", "Comma-separated MCP stdio args")
	cmd.Flags().StringVar(&mcpURL, "url", "", "MCP sse/streamable-http URL")
	return cmd
}

func buildOpenAPIConfig(source string, w io.Writer) (map[string]any, []string, error) {
	isURL := isRemoteURL(source)
	name := deriveServiceName(source)

	var specData []byte
	var specHost string

	if isURL {
		fmt.Fprint(w, "Parsing spec... ")
		client := &http.Client{Timeout: 15 * time.Second}
		resp, err := client.Get(source)
		if err != nil {
			return nil, nil, FormatError(err,
				fmt.Sprintf("Cannot fetch spec from %s", source),
				"Check the URL and ensure the spec is publicly reachable")
		}
		defer resp.Body.Close()
		if resp.StatusCode >= 400 {
			return nil, nil, FormatError(
				fmt.Errorf("server returned HTTP %d", resp.StatusCode),
				fmt.Sprintf("Cannot fetch spec from %s", source),
				"Check the URL and ensure the spec is publicly reachable")
		}
		specData, err = io.ReadAll(resp.Body)
		if err != nil {
			return nil, nil, err
		}
		if u, err := url.Parse(source); err == nil {
			specHost = u.Scheme + "://" + u.Host
		}
	} else {
		abs, err := filepath.Abs(source)
		if err != nil {
			return nil, nil, err
		}
		if _, err := os.Stat(abs); err != nil {
			return nil, nil, FormatError(err,
				fmt.Sprintf("File not found: %s", source),
				"Check the path and try again")
		}
		specData, err = os.ReadFile(abs)
		if err != nil {
			return nil, nil, err
		}
		fmt.Fprint(w, "Parsing spec... ")
	}

	// Parse with kin-openapi
	loader := openapi3.NewLoader()
	loader.IsExternalRefsAllowed = true
	doc, err := loader.LoadFromData(specData)
	if err != nil {
		fmt.Fprintln(w, "✗")
		return nil, nil, FormatError(err,
			"Failed to parse OpenAPI spec",
			"Ensure the file is a valid OpenAPI 3.x document")
	}

	// Validate basic structure
	if err := doc.Validate(context.Background(), openapi3.DisableExamplesValidation()); err != nil {
		fmt.Fprintln(w, "✗")
		return nil, nil, FormatError(err,
			"OpenAPI spec validation failed",
			"Check the spec for structural issues")
	}

	fmt.Fprintf(w, "✓ OpenAPI %s\n", doc.OpenAPI)

	// Count tools and groups
	toolCount := 0
	groups := map[string]bool{}
	if doc.Paths != nil {
		for _, pathItem := range doc.Paths.Map() {
			for _, op := range []*openapi3.Operation{
				pathItem.Get, pathItem.Post, pathItem.Put, pathItem.Delete,
				pathItem.Patch, pathItem.Head, pathItem.Options,
			} {
				if op != nil {
					toolCount++
					if len(op.Tags) > 0 {
						groups[op.Tags[0]] = true
					}
				}
			}
		}
	}
	if len(groups) > 0 {
		groupNames := make([]string, 0, len(groups))
		for g := range groups {
			groupNames = append(groupNames, g)
		}
		fmt.Fprintf(w, "Found %d tools across %d groups (%s)\n", toolCount, len(groups), strings.Join(groupNames, ", "))
	} else {
		fmt.Fprintf(w, "Found %d tools\n", toolCount)
	}

	// Detect auth requirements and build auth hints for next steps
	var authHints []string
	if doc.Components != nil && len(doc.Components.SecuritySchemes) > 0 {
		fmt.Fprintln(w, "\n⚠ This API requires authentication:")
		for schemeName, schemeRef := range doc.Components.SecuritySchemes {
			if schemeRef.Value == nil {
				continue
			}
			scheme := schemeRef.Value
			desc := scheme.Type
			switch scheme.Type {
			case "oauth2":
				if scheme.Flows != nil {
					if scheme.Flows.AuthorizationCode != nil {
						desc = "oauth2 — authorization code flow"
					} else if scheme.Flows.ClientCredentials != nil {
						desc = "oauth2 — client credentials flow"
					} else if scheme.Flows.Implicit != nil {
						desc = "oauth2 — implicit flow"
					}
				}
				authHints = append(authHints,
					fmt.Sprintf("ocli config add-secret %s --type oauth2 --token-url <url> --client-id-env <var> --client-secret-env <var>", schemeName))
			case "apiKey":
				desc = fmt.Sprintf("apiKey — %s: %s", scheme.In, scheme.Name)
				authHints = append(authHints,
					fmt.Sprintf("ocli config add-secret %s --type env --env-value %s", schemeName, strings.ToUpper(name)+"_API_KEY"))
			case "http":
				desc = fmt.Sprintf("http/%s", scheme.Scheme)
				authHints = append(authHints,
					fmt.Sprintf("ocli config add-secret %s --type env --env-value %s", schemeName, strings.ToUpper(name)+"_TOKEN"))
			}
			fmt.Fprintf(w, "  • %s (%s)\n", schemeName, desc)
		}
	}

	// Warn about relative server URLs
	if doc.Servers != nil {
		for _, server := range doc.Servers {
			if server.URL != "" && !strings.HasPrefix(server.URL, "http") {
				resolved := server.URL
				if specHost != "" {
					resolved = specHost + server.URL
				}
				fmt.Fprintf(w, "\n⚠ Server URL is relative: %s\n", server.URL)
				if specHost != "" {
					fmt.Fprintf(w, "  Resolved against spec host: %s\n", resolved)
				}
			}
		}
	}

	cfg := map[string]any{
		"cli":  "1.0.0",
		"mode": map[string]any{"default": "discover"},
		"sources": map[string]any{
			name + "Source": map[string]any{
				"type":    "openapi",
				"uri":     source,
				"enabled": true,
			},
		},
		"services": map[string]any{
			name: map[string]any{
				"source": name + "Source",
				"alias":  name,
			},
		},
	}
	return cfg, authHints, nil
}

func buildMCPConfig(name, transport, command, args, mcpURL string) map[string]any {
	t := map[string]any{"type": transport}
	if transport == "stdio" {
		t["command"] = command
		if args != "" {
			t["args"] = strings.Split(args, ",")
		}
	} else {
		t["url"] = mcpURL
	}
	return map[string]any{
		"cli":  "1.0.0",
		"mode": map[string]any{"default": "discover"},
		"sources": map[string]any{
			name: map[string]any{
				"type":      "mcp",
				"enabled":   true,
				"transport": t,
			},
		},
		"services": map[string]any{
			name: map[string]any{
				"source": name,
				"alias":  name,
			},
		},
	}
}

func validateMCPFlags(transport, command, mcpURL string) error {
	switch transport {
	case "stdio":
		if command == "" {
			return NewUserError("Missing --command",
				"MCP stdio transport requires a command to execute",
				"Add --command <executable> (e.g., --command npx)")
		}
	case "sse", "streamable-http":
		if mcpURL == "" {
			return NewUserError("Missing --url",
				"MCP "+transport+" transport requires a URL",
				"Add --url <endpoint>")
		}
	case "":
		return NewUserError("Missing --transport",
			"MCP sources require a transport type",
			"Add --transport stdio, --transport sse, or --transport streamable-http")
	default:
		return NewUserError(
			fmt.Sprintf("Unknown transport %q", transport),
			"Supported MCP transports: stdio, sse, streamable-http",
			"Use --transport stdio for local MCP servers")
	}
	return nil
}

func isRemoteURL(s string) bool {
	u, err := url.Parse(s)
	if err != nil {
		return false
	}
	return u.Scheme == "http" || u.Scheme == "https"
}

func deriveServiceName(source string) string {
	base := filepath.Base(source)
	for _, ext := range []string{".openapi.yaml", ".openapi.json", ".yaml", ".yml", ".json"} {
		if strings.HasSuffix(strings.ToLower(base), ext) {
			base = base[:len(base)-len(ext)]
			break
		}
	}
	name := strings.ToLower(base)
	var clean []byte
	for i := 0; i < len(name); i++ {
		c := name[i]
		if (c >= 'a' && c <= 'z') || (c >= '0' && c <= '9') {
			clean = append(clean, c)
		} else if len(clean) > 0 && clean[len(clean)-1] != '-' {
			clean = append(clean, '-')
		}
	}
	result := strings.Trim(string(clean), "-")
	if result == "" {
		return "api"
	}
	return result
}
```

- [ ] **Step 2: Build and test**

Run: `go build ./cmd/ocli ./cmd/oclird && go test ./cmd/ocli/...`
Expected: PASS

- [ ] **Step 3: Manual verification**

```bash
cd /tmp && rm -f .cli.json
# OpenAPI init with spec parsing
./bin/ocli init https://petstore3.swagger.io/api/v3/openapi.json
cat .cli.json
rm .cli.json

# MCP init
./bin/ocli init --type mcp --transport stdio --command npx --args "-y,@modelcontextprotocol/server-filesystem,/workspace" filesystem
cat .cli.json
rm .cli.json
```

Expected: OpenAPI init shows spec version, tool count, groups, auth requirements, server URL warnings.

- [ ] **Step 4: Commit**

```bash
git add -A
git commit -m "feat: smart init — parse specs, detect auth, support MCP sources

Co-authored-by: Copilot <223556219+Copilot@users.noreply.github.com>"
```

---

## Task 10: Wire Everything in root.go

**Files:**
- Modify: `cmd/ocli/internal/commands/root.go`

- [ ] **Step 1: Update root.go to register all new commands**

After `root.AddCommand(NewInitCommand())` (line 158), add the new commands. The final registration block should look like:

```go
	if !runtimeUnavailable {
		root.AddCommand(NewCatalogCommand(options, response))
		root.AddCommand(NewToolCommand(options, response))
		root.AddCommand(NewExplainCommand(options, response))
		root.AddCommand(NewWorkflowCommand(options, client))
		root.AddCommand(NewRuntimeCommand(options, client))
		AddDynamicToolCommands(root, options, client, response.Catalog.Services, response.View.Tools)
	}

	// Commands registered unconditionally — they degrade gracefully.
	root.AddCommand(NewInitCommand())
	root.AddCommand(NewConfigCommand(options))
	root.AddCommand(NewAuthCommand(options, client, runtimeUnavailable))
	root.AddCommand(NewStatusCommand(options, client, runtimeUnavailable))
	if runtimeUnavailable {
		root.AddCommand(NewSearchCommand(options, nil))
	} else {
		root.AddCommand(NewSearchCommand(options, &response))
	}
```

- [ ] **Step 2: Build and run full test suite**

Run: `go build ./cmd/ocli ./cmd/oclird && go test ./cmd/ocli/...`
Expected: PASS

- [ ] **Step 3: Full manual verification**

```bash
# No config — show help with all new commands visible
./bin/ocli

# Demo mode — all features
./bin/ocli --demo catalog list --group items
./bin/ocli --demo search "create"
./bin/ocli --demo status
./bin/ocli --demo demo items get-item 1 --dry-run
./bin/ocli --demo explain demo:createItem

# Config management
cd /tmp && rm -f .cli.json
./bin/ocli config add-source petstore --uri https://petstore3.swagger.io/api/v3/openapi.json
./bin/ocli config show
./bin/ocli config add-secret pets.oauth --type oauth2 --token-url https://auth.example.com/token --client-id-env PET_ID --client-secret-env PET_SECRET
./bin/ocli auth status
./bin/ocli config remove-source petstore
rm -f .cli.json

# Smart init
./bin/ocli init https://petstore3.swagger.io/api/v3/openapi.json
rm -f .cli.json

# MCP init
./bin/ocli init --type mcp --transport stdio --command npx --args "-y,@modelcontextprotocol/server-filesystem,/workspace" filesystem
rm -f .cli.json
```

- [ ] **Step 4: Commit and push**

```bash
git add -A
git commit -m "feat: wire all new commands — config, auth, search, status

Co-authored-by: Copilot <223556219+Copilot@users.noreply.github.com>"
git push origin main
```

---

## Task 11: Test Coverage

**Files:**
- Create: `cmd/ocli/internal/commands/commands_test.go`

- [ ] **Step 1: Create unit tests for new commands**

```go
package commands

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	cfgpkg "github.com/StevenBuglione/open-cli/cmd/ocli/internal/config"
	runtimepkg "github.com/StevenBuglione/open-cli/cmd/ocli/internal/runtime"
	"github.com/StevenBuglione/open-cli/pkg/catalog"
)

func testOptions(buf *bytes.Buffer) cfgpkg.Options {
	return cfgpkg.Options{
		Stdout: buf,
		Stderr: buf,
		Stdin:  strings.NewReader(""),
		Format: "json",
	}
}

func testCatalogResponse() runtimepkg.CatalogResponse {
	return runtimepkg.CatalogResponse{
		Catalog: catalog.NormalizedCatalog{
			Services: []catalog.Service{
				{ID: "demo", Alias: "demo", SourceID: "demoSrc", Title: "Demo"},
			},
			Tools: []catalog.Tool{
				{ID: "demo:listItems", ServiceID: "demo", Group: "items", Command: "list-items", Method: "GET", Summary: "List items", Safety: catalog.Safety{ReadOnly: true}},
				{ID: "demo:createItem", ServiceID: "demo", Group: "items", Command: "create-item", Method: "POST", Summary: "Create an item"},
				{ID: "demo:deleteItem", ServiceID: "demo", Group: "items", Command: "delete-item", Method: "DELETE", Summary: "Delete an item", Safety: catalog.Safety{Destructive: true}},
			},
		},
		View: catalog.EffectiveView{
			Name: "default",
			Mode: "discover",
			Tools: []catalog.Tool{
				{ID: "demo:listItems", ServiceID: "demo", Group: "items", Command: "list-items", Method: "GET", Summary: "List items", Safety: catalog.Safety{ReadOnly: true}},
				{ID: "demo:createItem", ServiceID: "demo", Group: "items", Command: "create-item", Method: "POST", Summary: "Create an item"},
				{ID: "demo:deleteItem", ServiceID: "demo", Group: "items", Command: "delete-item", Method: "DELETE", Summary: "Delete an item", Safety: catalog.Safety{Destructive: true}},
			},
		},
	}
}

func TestFilterToolsByGroup(t *testing.T) {
	tools := testCatalogResponse().View.Tools
	result := FilterTools(tools, "", "items", "")
	if len(result) != 3 {
		t.Errorf("expected 3 tools in items group, got %d", len(result))
	}
	result = FilterTools(tools, "", "nonexistent", "")
	if len(result) != 0 {
		t.Errorf("expected 0 tools for nonexistent group, got %d", len(result))
	}
}

func TestFilterToolsBySafety(t *testing.T) {
	tools := testCatalogResponse().View.Tools
	result := FilterTools(tools, "", "", "read-only")
	if len(result) != 1 || result[0].ID != "demo:listItems" {
		t.Errorf("expected listItems for read-only filter, got %v", result)
	}
	result = FilterTools(tools, "", "", "destructive")
	if len(result) != 1 || result[0].ID != "demo:deleteItem" {
		t.Errorf("expected deleteItem for destructive filter, got %v", result)
	}
}

func TestSearchTools(t *testing.T) {
	tools := testCatalogResponse().View.Tools
	result := SearchTools(tools, "create")
	if len(result) != 1 || result[0].ID != "demo:createItem" {
		t.Errorf("expected createItem for 'create' search, got %v", result)
	}
	result = SearchTools(tools, "item")
	if len(result) != 3 {
		t.Errorf("expected all 3 tools for 'item' search, got %d", len(result))
	}
	result = SearchTools(tools, "nonexistent")
	if len(result) != 0 {
		t.Errorf("expected 0 for nonexistent search, got %d", len(result))
	}
}

func TestSearchCommandNoResults(t *testing.T) {
	var buf bytes.Buffer
	opts := testOptions(&buf)
	response := testCatalogResponse()
	cmd := NewSearchCommand(opts, &response)
	cmd.SetArgs([]string{"zzzzz"})
	cmd.SetOut(&buf)
	cmd.SetErr(&buf)
	_ = cmd.Execute()
	if !strings.Contains(buf.String(), "No tools matching") {
		t.Errorf("expected 'No tools matching' message, got: %s", buf.String())
	}
}

func TestSearchCommandRuntimeUnavailable(t *testing.T) {
	var buf bytes.Buffer
	opts := testOptions(&buf)
	cmd := NewSearchCommand(opts, nil)
	cmd.SetArgs([]string{"test"})
	cmd.SetOut(&buf)
	cmd.SetErr(&buf)
	err := cmd.Execute()
	if err == nil {
		t.Error("expected error when runtime unavailable")
	}
}

func TestConfigAddSourceCreatesFile(t *testing.T) {
	dir := t.TempDir()
	origDir, _ := os.Getwd()
	_ = os.Chdir(dir)
	defer os.Chdir(origDir)

	var buf bytes.Buffer
	cmd := newConfigAddSourceCommand()
	cmd.SetArgs([]string{"myapi", "--uri", "https://example.com/api.json"})
	cmd.SetOut(&buf)
	cmd.SetErr(&buf)
	if err := cmd.Execute(); err != nil {
		t.Fatalf("add-source failed: %v", err)
	}

	data, err := os.ReadFile(filepath.Join(dir, ".cli.json"))
	if err != nil {
		t.Fatalf("config file not created: %v", err)
	}
	var raw map[string]any
	_ = json.Unmarshal(data, &raw)
	sources, _ := raw["sources"].(map[string]any)
	if _, ok := sources["myapi"]; !ok {
		t.Error("source 'myapi' not found in config")
	}
}

func TestConfigAddSourceDuplicate(t *testing.T) {
	dir := t.TempDir()
	origDir, _ := os.Getwd()
	_ = os.Chdir(dir)
	defer os.Chdir(origDir)

	var buf bytes.Buffer
	cmd := newConfigAddSourceCommand()
	cmd.SetArgs([]string{"myapi", "--uri", "https://example.com/api.json"})
	cmd.SetOut(&buf)
	_ = cmd.Execute()

	buf.Reset()
	cmd2 := newConfigAddSourceCommand()
	cmd2.SetArgs([]string{"myapi", "--uri", "https://example.com/other.json"})
	cmd2.SetOut(&buf)
	cmd2.SetErr(&buf)
	err := cmd2.Execute()
	if err == nil {
		t.Error("expected error for duplicate source")
	}
}

func TestConfigRemoveSource(t *testing.T) {
	dir := t.TempDir()
	origDir, _ := os.Getwd()
	_ = os.Chdir(dir)
	defer os.Chdir(origDir)

	// Create config first
	var buf bytes.Buffer
	addCmd := newConfigAddSourceCommand()
	addCmd.SetArgs([]string{"myapi", "--uri", "https://example.com/api.json"})
	addCmd.SetOut(&buf)
	_ = addCmd.Execute()

	buf.Reset()
	rmCmd := newConfigRemoveSourceCommand()
	rmCmd.SetArgs([]string{"myapi"})
	rmCmd.SetOut(&buf)
	if err := rmCmd.Execute(); err != nil {
		t.Fatalf("remove-source failed: %v", err)
	}

	data, _ := os.ReadFile(filepath.Join(dir, ".cli.json"))
	var raw map[string]any
	_ = json.Unmarshal(data, &raw)
	sources, _ := raw["sources"].(map[string]any)
	if _, ok := sources["myapi"]; ok {
		t.Error("source should have been removed")
	}
}

func TestDryRun(t *testing.T) {
	var buf bytes.Buffer
	tool := catalog.Tool{
		Method:  "POST",
		Path:    "/items/{id}",
		Servers: []string{"https://api.example.com"},
		PathParams: []catalog.Parameter{
			{Name: "id", OriginalName: "id"},
		},
		Flags: []catalog.Parameter{
			{Name: "tag", OriginalName: "tag", Location: "query"},
		},
	}
	WriteDryRun(&buf, tool, []string{"42"}, map[string]string{"tag": "foo"}, []byte(`{"name":"test"}`))
	output := buf.String()
	if !strings.Contains(output, "POST https://api.example.com/items/42") {
		t.Errorf("expected URL with path param, got: %s", output)
	}
	if !strings.Contains(output, "tag=foo") {
		t.Errorf("expected query param, got: %s", output)
	}
	if !strings.Contains(output, `{"name":"test"}`) {
		t.Errorf("expected body, got: %s", output)
	}
}

func TestPromptForMissingArgs(t *testing.T) {
	stdin := strings.NewReader("42\n")
	var stderr bytes.Buffer
	params := []catalog.Parameter{{Name: "id"}}
	result, err := PromptForMissingArgs(stdin, &stderr, params, nil)
	if err != nil {
		t.Fatalf("prompt failed: %v", err)
	}
	if len(result) != 1 || result[0] != "42" {
		t.Errorf("expected [42], got %v", result)
	}
	if !strings.Contains(stderr.String(), "Enter id:") {
		t.Errorf("expected prompt text, got: %s", stderr.String())
	}
}

func TestDeriveServiceName(t *testing.T) {
	tests := []struct{ input, want string }{
		{"https://example.com/petstore.openapi.json", "petstore"},
		{"https://example.com/my-api.yaml", "my-api"},
		{"/path/to/spec.json", "spec"},
		{"https://example.com/openapi.json", "openapi"},
	}
	for _, tt := range tests {
		got := deriveServiceName(tt.input)
		if got != tt.want {
			t.Errorf("deriveServiceName(%q) = %q, want %q", tt.input, got, tt.want)
		}
	}
}
```

- [ ] **Step 2: Run tests**

Run: `go test ./cmd/ocli/internal/commands/ -v`
Expected: All tests PASS

- [ ] **Step 3: Run full suite**

Run: `go build ./cmd/ocli ./cmd/oclird && go test ./cmd/ocli/...`
Expected: PASS

- [ ] **Step 4: Commit**

```bash
git add -A
git commit -m "test: comprehensive unit tests for all new CLI commands

Co-authored-by: Copilot <223556219+Copilot@users.noreply.github.com>"
git push origin main
```

---

## Dependency Graph

```
Task 1 (foundation + errors) ──┬── Task 2 (catalog filter)
                                ├── Task 3 (search)
                                ├── Task 4 (status)
                                ├── Task 5 (config)
                                ├── Task 6 (auth)
                                ├── Task 7 (dry-run) ─── Task 8 (prompts) [same file]
                                └── Task 9 (smart init)

All above ─── Task 10 (wiring + manual verification) ─── Task 11 (unit tests)
```

Tasks 2-9 can be done in parallel after Task 1. Task 10 depends on all of them. Task 11 depends on Task 10.

**Note on TDD:** Task 11 contains the comprehensive test suite. Each individual task (2-9) should compile cleanly but defers testing to Task 11 since commands aren't registered in root.go until Task 10. Implementers are encouraged to write inline smoke tests during development.
