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
				fmt.Fprintln(w, "Config files:")
				for _, scope := range []configpkg.Scope{configpkg.ScopeManaged, configpkg.ScopeUser, configpkg.ScopeProject, configpkg.ScopeLocal} {
					if path, ok := paths[scope]; ok {
						fmt.Fprintf(w, "  %s: %s (active)\n", scope, path)
					} else {
						fmt.Fprintf(w, "  %s: (not found)\n", scope)
					}
				}
				if options.ConfigPath != "" {
					raw, err := readConfigFile(options.ConfigPath)
					if err == nil {
						if sources, ok := raw["sources"].(map[string]any); ok {
							fmt.Fprintln(w)
							tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
							fmt.Fprintln(tw, "SOURCE\tTYPE\tURI\tENABLED")
							for name, value := range sources {
								source, _ := value.(map[string]any)
								sourceType, _ := source["type"].(string)
								uri, _ := source["uri"].(string)
								enabled := true
								if flag, ok := source["enabled"].(bool); ok {
									enabled = flag
								}
								if uri == "" {
									if transport, ok := source["transport"].(map[string]any); ok {
										command, _ := transport["command"].(string)
										transportType, _ := transport["type"].(string)
										uri = fmt.Sprintf("%s: %s", transportType, command)
									}
								}
								fmt.Fprintf(tw, "%s\t%s\t%s\t%v\n", name, sourceType, uri, enabled)
							}
							tw.Flush()
						}
					}
				}
				return nil
			}

			if options.ConfigPath == "" {
				return NewUserError(
					"No config file found",
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
	var sourceType, uri, transport, command, args, mcpURL, alias string
	var global bool
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
				transportMap := map[string]any{"type": transport}
				switch transport {
				case "stdio":
					if command == "" {
						return NewUserError("Missing --command", "MCP stdio transport requires a command", "Add --command <executable>")
					}
					transportMap["command"] = command
					if args != "" {
						transportMap["args"] = strings.Split(args, ",")
					}
				case "sse", "streamable-http":
					if mcpURL == "" {
						return NewUserError("Missing --url", "MCP transport requires a URL", "Add --url <endpoint>")
					}
					transportMap["url"] = mcpURL
				default:
					return NewUserError(
						fmt.Sprintf("Unknown transport %q", transport),
						"MCP sources require --transport stdio, sse, or streamable-http",
						"Use --transport stdio for local MCP servers")
				}
				source["transport"] = transportMap
			default:
				return NewUserError(
					fmt.Sprintf("Unknown source type %q", sourceType),
					"Supported types: openapi, mcp, apiCatalog, serviceRoot",
					"Use --type openapi for OpenAPI specs")
			}
			sources[name] = source

			services := ensureMap(raw, "services")
			serviceAlias := alias
			if serviceAlias == "" {
				serviceAlias = name
			}
			services[name] = map[string]any{"source": name, "alias": serviceAlias}

			return writeConfigFile(configPath, raw, cmd.OutOrStdout(), fmt.Sprintf("Added source %q (%s)", name, sourceType))
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
			configPath, err := configFilePath(global)
			if err != nil {
				return err
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
			if services, ok := raw["services"].(map[string]any); ok {
				for serviceName, value := range services {
					service, ok := value.(map[string]any)
					if ok && service["source"] == name {
						delete(services, serviceName)
					}
				}
			}
			return writeConfigFile(configPath, raw, cmd.OutOrStdout(), fmt.Sprintf("Removed source %q", name))
		},
	}
	cmd.Flags().BoolVar(&global, "global", false, "Modify ~/.config/oas-cli/ config")
	return cmd
}

func newConfigAddSecretCommand() *cobra.Command {
	var secretType, mode, tokenURL, clientID, clientSecret, scopes, envValue string
	var global bool
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
				if clientSecret != "" {
					secret["clientSecret"] = map[string]any{"type": "env", "value": clientSecret}
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

			return writeConfigFile(configPath, raw, cmd.OutOrStdout(), fmt.Sprintf("Added secret %q (%s)", name, secretType))
		},
	}
	cmd.Flags().StringVar(&secretType, "type", "", "Secret type: oauth2, env")
	_ = cmd.MarkFlagRequired("type")
	cmd.Flags().StringVar(&mode, "mode", "clientCredentials", "OAuth mode: authorizationCode, clientCredentials")
	cmd.Flags().StringVar(&tokenURL, "token-url", "", "OAuth token URL")
	cmd.Flags().StringVar(&clientID, "client-id-env", "", "Env var name for OAuth client ID")
	cmd.Flags().StringVar(&clientSecret, "client-secret-env", "", "Env var name for OAuth client secret")
	cmd.Flags().StringVar(&scopes, "scopes", "", "Comma-separated OAuth scopes")
	cmd.Flags().StringVar(&envValue, "env-value", "", "Environment variable name (for --type env)")
	cmd.Flags().BoolVar(&global, "global", false, "Write to ~/.config/oas-cli/ config")
	return cmd
}

func loadOrCreateConfig(global bool) (map[string]any, string, error) {
	configPath, err := configFilePath(global)
	if err != nil {
		return nil, "", err
	}
	raw, err := readConfigFile(configPath)
	if err != nil {
		raw = map[string]any{
			"cli":  "1.0.0",
			"mode": map[string]any{"default": "discover"},
		}
	}
	return raw, configPath, nil
}

func configFilePath(global bool) (string, error) {
	if !global {
		return ".cli.json", nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	dir := filepath.Join(home, ".config", "oas-cli")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	return filepath.Join(dir, ".cli.json"), nil
}

func ensureMap(raw map[string]any, key string) map[string]any {
	if existing, ok := raw[key].(map[string]any); ok {
		return existing
	}
	result := map[string]any{}
	raw[key] = result
	return result
}

func writeConfigFile(path string, raw map[string]any, w interface{ Write([]byte) (int, error) }, msg string) error {
	data, err := json.MarshalIndent(raw, "", "  ")
	if err != nil {
		return err
	}
	if err := os.WriteFile(path, append(data, '\n'), 0o644); err != nil {
		return err
	}
	_, err = fmt.Fprintf(w, "%s in %s\n", msg, path)
	return err
}
