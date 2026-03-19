package commands

import (
	"fmt"

	cfgpkg "github.com/StevenBuglione/open-cli/cmd/ocli/internal/config"
	runtimepkg "github.com/StevenBuglione/open-cli/cmd/ocli/internal/runtime"
	"github.com/spf13/cobra"
)

// NewCatalogCommand returns the "catalog" sub-command.
func NewCatalogCommand(options cfgpkg.Options, response runtimepkg.CatalogResponse) *cobra.Command {
	command := &cobra.Command{
		Use:   "catalog",
		Short: "Inspect the tool catalog",
	}
	command.AddCommand(&cobra.Command{
		Use:   "list",
		Short: "List the effective catalog",
		RunE: func(cmd *cobra.Command, _ []string) error {
			return WriteOutput(options.Stdout, options.Format, response)
		},
	})
	return command
}

// NewToolCommand returns the "tool" sub-command.
func NewToolCommand(options cfgpkg.Options, response runtimepkg.CatalogResponse) *cobra.Command {
	command := &cobra.Command{Use: "tool", Short: "Tool schema and metadata"}
	command.AddCommand(&cobra.Command{
		Use:   "schema <tool-id>",
		Args:  cobra.ExactArgs(1),
		Short: "Render machine-readable tool schema",
		RunE: func(cmd *cobra.Command, args []string) error {
			tool := FindTool(response.Catalog.Tools, args[0])
			if tool == nil {
				return fmt.Errorf("tool %s not found", args[0])
			}
			return WriteOutput(options.Stdout, options.Format, tool)
		},
	})
	return command
}

// NewExplainCommand returns the "explain" sub-command.
func NewExplainCommand(options cfgpkg.Options, response runtimepkg.CatalogResponse) *cobra.Command {
	return &cobra.Command{
		Use:   "explain <tool-id>",
		Args:  cobra.ExactArgs(1),
		Short: "Show detailed information about a tool",
		RunE: func(cmd *cobra.Command, args []string) error {
			tool := FindTool(response.Catalog.Tools, args[0])
			if tool == nil {
				return FormatError(
					fmt.Errorf("tool %q not found in catalog", args[0]),
					"The tool ID may be misspelled or filtered by curation rules",
					"Run 'ocli catalog list' to see available tools")
			}
			result := map[string]any{
				"toolId":  tool.ID,
				"summary": tool.Summary,
				"method":  tool.Method,
				"path":    tool.Path,
				"service": tool.ServiceID,
				"group":   tool.Group,
				"command": tool.Command,
				"safety":  tool.Safety,
			}
			if tool.Description != "" {
				result["description"] = tool.Description
			}
			if len(tool.PathParams) > 0 {
				result["pathParams"] = tool.PathParams
			}
			if len(tool.Flags) > 0 {
				result["parameters"] = tool.Flags
			}
			if tool.RequestBody != nil {
				result["requestBody"] = tool.RequestBody
			}
			if tool.Guidance != nil {
				result["guidance"] = tool.Guidance
			}
			if len(tool.Servers) > 0 {
				result["servers"] = tool.Servers
			}
			return WriteOutput(options.Stdout, options.Format, result)
		},
	}
}

// NewWorkflowCommand returns the "workflow" sub-command.
func NewWorkflowCommand(options cfgpkg.Options, client runtimepkg.Client) *cobra.Command {
	command := &cobra.Command{Use: "workflow", Short: "Run multi-step workflows"}
	command.AddCommand(&cobra.Command{
		Use:   "run <workflow-id>",
		Args:  cobra.ExactArgs(1),
		Short: "Run a workflow through the runtime",
		RunE: func(cmd *cobra.Command, args []string) error {
			result, err := client.RunWorkflow(map[string]any{
				"configPath":   options.ConfigPath,
				"mode":         options.Mode,
				"agentProfile": options.AgentProfile,
				"workflowId":   args[0],
				"approval":     options.Approval,
			})
			if err != nil {
				return err
			}
			return WriteOutput(options.Stdout, options.Format, result)
		},
	})
	return command
}

// NewRuntimeCommand returns the "runtime" sub-command.
func NewRuntimeCommand(options cfgpkg.Options, client runtimepkg.Client) *cobra.Command {
	command := &cobra.Command{Use: "runtime", Short: "Manage the runtime daemon"}
	command.AddCommand(&cobra.Command{
		Use:   "info",
		Short: "Show runtime metadata",
		RunE: func(cmd *cobra.Command, args []string) error {
			info, err := client.RuntimeInfo()
			if err != nil {
				return err
			}
			return WriteOutput(options.Stdout, options.Format, info)
		},
	})
	command.AddCommand(&cobra.Command{
		Use:   "stop",
		Short: "Stop the runtime",
		RunE: func(cmd *cobra.Command, args []string) error {
			result, err := client.Stop()
			if err != nil {
				return err
			}
			return WriteOutput(options.Stdout, options.Format, result)
		},
	})
	command.AddCommand(&cobra.Command{
		Use:   "session-close",
		Short: "Close the runtime session and clear session auth state",
		RunE: func(cmd *cobra.Command, args []string) error {
			result, err := client.SessionClose()
			if err != nil {
				return err
			}
			return WriteOutput(options.Stdout, options.Format, result)
		},
	})
	return command
}
