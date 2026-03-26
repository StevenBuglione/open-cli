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
					"Check the configured open-cli-toolbox runtime URL")
			}

			pattern := args[0]
			service, _ := cmd.Flags().GetString("service")

			tools := response.View.Tools
			if service != "" {
				tools = FilterTools(tools, service, "", "")
			}
			matches := SearchTools(tools, pattern)
			if len(matches) == 0 {
				_, err := fmt.Fprintf(options.Stderr, "No tools matching %q. Run 'ocli catalog list' to see all tools.\n", pattern)
				return err
			}

			result := *response
			result.View.Tools = matches
			return WriteOutput(options.Stdout, options.Format, result)
		},
	}
	cmd.Flags().String("service", "", "Limit search to one service")
	return cmd
}
