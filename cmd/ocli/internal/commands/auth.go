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
				return NewUserError("Cannot log in", "Runtime is not available", "Use --embedded or start the daemon with oclird")
			}
			info, err := client.RuntimeInfo()
			if err != nil {
				return FormatError(err, "Failed to reach runtime", "Check runtime URL or use --embedded")
			}
			authInfo, _ := info["auth"].(map[string]any)
			browserEndpoint, _ := authInfo["browserLoginEndpoint"].(string)
			if browserEndpoint == "" {
				fmt.Fprintln(options.Stdout, "Runtime does not require browser authentication.")
				fmt.Fprintln(options.Stdout, "Per-service tokens are resolved automatically at execution time.")
				return nil
			}

			metadata, err := authpkg.FetchBrowserLoginMetadata(options.RuntimeURL, browserEndpoint)
			if err != nil {
				return FormatError(err, "Failed to fetch login metadata from runtime", "The runtime may not support browser-based login")
			}

			scopes, _ := authInfo["scopes"].([]any)
			var scopeStrings []string
			for _, scope := range scopes {
				if value, ok := scope.(string); ok {
					scopeStrings = append(scopeStrings, value)
				}
			}
			audience, _ := authInfo["audience"].(string)

			fmt.Fprintln(options.Stdout, "Opening browser for login...")
			_, err = authpkg.BrowserLoginTokenAcquirer(authpkg.BrowserLoginRequest{
				Metadata: metadata,
				Scopes:   scopeStrings,
				Audience: audience,
				StateDir: options.StateDir,
			})
			if err != nil {
				return FormatError(err, "Browser login failed", "Check your credentials and try again")
			}
			fmt.Fprintln(options.Stdout, "ok Login successful. Token cached for this session.")
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
				return NewUserError("No config file found", "Cannot determine auth requirements without a config", "Run 'ocli init <url>' to create a config")
			}
			raw, err := readConfigFile(options.ConfigPath)
			if err != nil {
				return FormatError(err, "Cannot read config", "Check that "+options.ConfigPath+" exists")
			}

			tw := tabwriter.NewWriter(options.Stdout, 0, 0, 2, ' ', 0)
			fmt.Fprintln(tw, "SERVICE\tAUTH TYPE\tCONFIGURED")

			secrets, _ := raw["secrets"].(map[string]any)
			sources, _ := raw["sources"].(map[string]any)
			services, _ := raw["services"].(map[string]any)

			for serviceName, value := range services {
				service, _ := value.(map[string]any)
				sourceName, _ := service["source"].(string)
				source, _ := sources[sourceName].(map[string]any)

				authType := "none"
				configured := "no auth required"
				if source != nil {
					if _, ok := source["oauth"]; ok {
						authType = "oauth2 (transport)"
						configured = "ok configured in source"
					}
				}
				for secretName, secretValue := range secrets {
					secret, ok := secretValue.(map[string]any)
					if !ok {
						continue
					}
					secretType, _ := secret["type"].(string)
					if secretName == serviceName || secretName == sourceName || secretName == serviceName+".oauth" || secretName == sourceName+".oauth" {
						authType = secretType
						configured = fmt.Sprintf("ok secret: %s", secretName)
					}
				}
				fmt.Fprintf(tw, "%s\t%s\t%s\n", serviceName, authType, configured)
			}
			return tw.Flush()
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
				return FormatError(err, "Failed to close session", "The runtime may not be running")
			}
			fmt.Fprintln(options.Stdout, "ok Session closed. Cached tokens cleared.")
			return nil
		},
	}
}
