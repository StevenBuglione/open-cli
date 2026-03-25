package commands

import (
	"fmt"
	"io"
	"sort"
	"strings"
	"text/tabwriter"

	authpkg "github.com/StevenBuglione/open-cli/cmd/ocli/internal/auth"
	cfgpkg "github.com/StevenBuglione/open-cli/cmd/ocli/internal/config"
	runtimepkg "github.com/StevenBuglione/open-cli/cmd/ocli/internal/runtime"
	"github.com/spf13/cobra"
)

type authStatusReport struct {
	Posture  string              `json:"posture"`
	Runtime  authRuntimeSummary  `json:"runtime"`
	Config   authConfigSummary   `json:"config"`
	Services []authStatusService `json:"services"`
}

type authRuntimeSummary struct {
	Available bool    `json:"available"`
	Mode      string  `json:"mode"`
	Posture   string  `json:"posture"`
	Version   *string `json:"version"`
}

type authConfigSummary struct {
	ActivePath *string `json:"activePath"`
	Posture    string  `json:"posture"`
}

type authStatusService struct {
	Name       string `json:"name"`
	AuthType   string `json:"authType"`
	Configured string `json:"configured"`
}

// NewAuthCommand returns the "auth" parent command.
func NewAuthCommand(options cfgpkg.Options, client runtimepkg.Client, runtimeUnavailable bool) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "auth",
		Short: "Manage authentication",
	}
	cmd.AddCommand(newAuthLoginCommand(options, client, runtimeUnavailable))
	cmd.AddCommand(newAuthStatusCommand(options, client, runtimeUnavailable))
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
				return NewUserError("Cannot log in", "Runtime is not available", "Start the daemon with oclird")
			}
			info, err := client.RuntimeInfo()
			if err != nil {
				return FormatError(err, "Failed to reach runtime", "Check runtime URL or start the daemon with oclird")
			}
			authInfo, _ := info["auth"].(map[string]any)
			browserEndpoint, _ := authInfo["browserLoginEndpoint"].(string)
			if browserEndpoint == "" {
				fmt.Fprintln(options.Stdout, "Runtime does not require browser authentication.")
				fmt.Fprintln(options.Stdout, "Per-service tokens are resolved automatically at execution time.")
				return nil
			}

			metadata, err := authpkg.FetchBrowserLoginMetadata(options.RuntimeURL, browserEndpoint, options.RuntimeRequestConfigPath)
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

func newAuthStatusCommand(options cfgpkg.Options, client runtimepkg.Client, runtimeUnavailable bool) *cobra.Command {
	return &cobra.Command{
		Use:   "status",
		Short: "Show authentication configuration",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return writeAuthStatus(options.Stdout, options, client, runtimeUnavailable)
		},
	}
}

func writeAuthStatus(w io.Writer, options cfgpkg.Options, client runtimepkg.Client, runtimeUnavailable bool) error {
	report := buildAuthStatusReport(options, client, runtimeUnavailable)
	if options.Format == "table" {
		writeAuthStatusTerminal(w, report)
		return nil
	}
	return WriteOutput(w, options.Format, report)
}

func buildAuthStatusReport(options cfgpkg.Options, client runtimepkg.Client, runtimeUnavailable bool) authStatusReport {
	report := authStatusReport{
		Posture: "unknown",
		Runtime: authRuntimeSummary{
			Mode:    runtimeMode(options),
			Posture: "unknown",
		},
		Config: authConfigSummary{
			ActivePath: stringPtr(options.ConfigPath),
			Posture:    "unknown",
		},
		Services: []authStatusService{},
	}

	configInformative := false
	if options.ConfigPath != "" {
		if raw, err := readConfigFile(options.ConfigPath); err == nil {
			report.Services = authServicesFromRawConfig(raw)
			if configHasAuthEvidence(raw) {
				configInformative = true
				report.Config.Posture = "configOnly"
			}
		}
	}

	if runtimeUnavailable {
		if configInformative {
			report.Posture = "configOnly"
		}
		return report
	}

	info, err := client.RuntimeInfo()
	if err != nil {
		if configInformative {
			report.Posture = "configOnly"
		}
		return report
	}

	report.Runtime.Available = true
	if version, ok := stringFromMap(info, "version"); ok {
		report.Runtime.Version = stringPtr(version)
	}

	authInfo, _ := info["auth"].(map[string]any)
	runtimeHasAuth := len(authInfo) > 0
	if runtimeHasAuth {
		report.Runtime.Posture = "runtimeSession"
		report.Posture = "runtimeSession"
	} else {
		report.Runtime.Posture = "unknown"
	}

	if configInformative {
		report.Config.Posture = "configOnly"
	} else {
		report.Config.Posture = "unknown"
	}

	if report.Config.Posture == "configOnly" && report.Posture != "runtimeSession" {
		report.Posture = "configOnly"
	}

	if report.Config.Posture == "unknown" && !runtimeHasAuth {
		report.Posture = "unknown"
	}

	if runtimeHasAuth {
		report.Posture = "runtimeSession"
	}

	return report
}

func writeAuthStatusTerminal(w io.Writer, report authStatusReport) {
	fmt.Fprintf(w, "Posture: %s\n", report.Posture)

	runtimeLine := "Runtime:  unavailable"
	if report.Runtime.Available {
		version := "unknown"
		if report.Runtime.Version != nil && strings.TrimSpace(*report.Runtime.Version) != "" {
			version = *report.Runtime.Version
		}
		runtimeLine = fmt.Sprintf("Runtime:  available %s (v%s)", report.Runtime.Mode, version)
	}
	fmt.Fprintln(w, runtimeLine)

	if report.Config.Posture == "configOnly" {
		if report.Config.ActivePath != nil {
			fmt.Fprintf(w, "Config:   configOnly %s\n", *report.Config.ActivePath)
		} else {
			fmt.Fprintln(w, "Config:   configOnly")
		}
	} else if report.Config.ActivePath != nil {
		fmt.Fprintf(w, "Config:   %s\n", *report.Config.ActivePath)
	} else {
		fmt.Fprintln(w, "Config:   unknown")
	}

	tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
	fmt.Fprintln(tw, "SERVICE\tAUTH TYPE\tCONFIGURED")
	for _, service := range report.Services {
		fmt.Fprintf(tw, "%s\t%s\t%s\n", service.Name, service.AuthType, service.Configured)
	}
	_ = tw.Flush()
}

func authServicesFromRawConfig(raw map[string]any) []authStatusService {
	secrets, _ := raw["secrets"].(map[string]any)
	sources, _ := raw["sources"].(map[string]any)
	services, _ := raw["services"].(map[string]any)

	names := make([]string, 0, len(services))
	for serviceName := range services {
		names = append(names, serviceName)
	}
	sort.Strings(names)

	result := make([]authStatusService, 0, len(names))
	for _, serviceName := range names {
		value, _ := services[serviceName]
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
		for _, secretName := range []string{
			serviceName + ".oauth",
			sourceName + ".oauth",
			serviceName,
			sourceName,
		} {
			secretValue, ok := secrets[secretName]
			if !ok {
				continue
			}
			secret, ok := secretValue.(map[string]any)
			if !ok {
				continue
			}
			secretType, _ := secret["type"].(string)
			authType = secretType
			configured = fmt.Sprintf("ok secret: %s", secretName)
			break
		}
		result = append(result, authStatusService{
			Name:       serviceName,
			AuthType:   authType,
			Configured: configured,
		})
	}
	return result
}

func configHasAuthEvidence(raw map[string]any) bool {
	if raw == nil {
		return false
	}
	if stringFromRawConfig(raw, "runtime", "remote", "oauth", "mode") != "" {
		return true
	}
	if len(stringSliceFromRawConfig(raw, "runtime", "remote", "oauth", "scopes")) > 0 {
		return true
	}
	if sources, _ := raw["sources"].(map[string]any); len(sources) > 0 {
		for _, value := range sources {
			source, _ := value.(map[string]any)
			if source == nil {
				continue
			}
			if _, ok := source["oauth"]; ok {
				return true
			}
		}
	}
	if secrets, _ := raw["secrets"].(map[string]any); len(secrets) > 0 {
		return true
	}
	return false
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
