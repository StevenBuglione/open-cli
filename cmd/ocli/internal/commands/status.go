package commands

import (
	"fmt"
	"io"
	"sort"
	"strings"

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
			writeStatus(options.Stdout, options, client, runtimeUnavailable)
			return nil
		},
	}
}

func writeStatus(w io.Writer, options cfgpkg.Options, client runtimepkg.Client, runtimeUnavailable bool) {
	if runtimeUnavailable {
		fmt.Fprintln(w, "Runtime:  x not running")
	} else {
		info, err := client.RuntimeInfo()
		if err != nil {
			fmt.Fprintln(w, "Runtime:  x error fetching info")
		} else {
			mode := "unknown"
			switch {
			case options.Embedded:
				mode = "embedded"
			case options.RuntimeDeployment == "local":
				mode = "local daemon"
			case options.RuntimeDeployment == "remote":
				mode = "remote"
			}
			version, _ := info["version"].(string)
			if version == "" {
				version = "unknown"
			}
			fmt.Fprintf(w, "Runtime:  ok %s (v%s)\n", mode, version)
		}
	}

	if options.ConfigPath != "" {
		fmt.Fprintf(w, "Config:   %s\n", options.ConfigPath)
	} else {
		fmt.Fprintln(w, "Config:   none")
	}

	if options.ConfigPath != "" {
		raw, err := readConfigFile(options.ConfigPath)
		if err == nil {
			if sources, ok := raw["sources"].(map[string]any); ok {
				typeCounts := map[string]int{}
				for _, value := range sources {
					source, ok := value.(map[string]any)
					if !ok {
						continue
					}
					enabled := true
					if flag, ok := source["enabled"].(bool); ok {
						enabled = flag
					}
					if !enabled {
						continue
					}
					sourceType, _ := source["type"].(string)
					if sourceType == "" {
						sourceType = "unknown"
					}
					typeCounts[sourceType]++
				}
				total := 0
				var parts []string
				for sourceType, count := range typeCounts {
					total += count
					parts = append(parts, fmt.Sprintf("%d %s", count, sourceType))
				}
				sort.Strings(parts)
				if total > 0 {
					fmt.Fprintf(w, "Sources:  %d active (%s)\n", total, strings.Join(parts, ", "))
				} else {
					fmt.Fprintln(w, "Sources:  0 active")
				}
			}
		}
	}

	paths := configpkg.DiscoverScopePaths(configpkg.LoadOptions{})
	for _, scope := range []configpkg.Scope{configpkg.ScopeManaged, configpkg.ScopeUser, configpkg.ScopeProject, configpkg.ScopeLocal} {
		if path, ok := paths[scope]; ok {
			fmt.Fprintf(w, "  %s: %s\n", scope, path)
		}
	}

	if runtimeUnavailable {
		fmt.Fprintln(w)
		fmt.Fprintln(w, "Suggestion: Run with --embedded or start the daemon with oclird")
	}
}
