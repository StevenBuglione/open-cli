package commands

import (
	"fmt"

	cfgpkg "github.com/StevenBuglione/open-cli/cmd/ocli/internal/config"
	runtimepkg "github.com/StevenBuglione/open-cli/cmd/ocli/internal/runtime"
	"github.com/StevenBuglione/open-cli/pkg/catalog"
	configpkg "github.com/StevenBuglione/open-cli/pkg/config"
	policypkg "github.com/StevenBuglione/open-cli/pkg/policy"
	"github.com/spf13/cobra"
)

// NewCatalogCommand returns the "catalog" sub-command.
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
				return FormatError(
					fmt.Errorf("tool %q not found in catalog", args[0]),
					"The tool ID may be misspelled or filtered by curation rules",
					"Run 'ocli catalog list' to see available tools")
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
			result := buildExplainReport(options, response, tool)
			return WriteOutput(options.Stdout, options.Format, result)
		},
	}
}

type explainRuntimeSummary struct {
	Mode string `json:"mode"`
}

type explainReport struct {
	ToolID           string                    `json:"toolId"`
	Summary          string                    `json:"summary"`
	Method           string                    `json:"method"`
	Path             string                    `json:"path"`
	Service          string                    `json:"service"`
	Group            string                    `json:"group"`
	Command          string                    `json:"command"`
	Description      string                    `json:"description,omitempty"`
	PathParams       []catalog.Parameter       `json:"pathParams,omitempty"`
	Parameters       []catalog.Parameter       `json:"parameters,omitempty"`
	RequestBody      *catalog.RequestBody      `json:"requestBody,omitempty"`
	Guidance         *catalog.Guidance         `json:"guidance,omitempty"`
	Servers          []string                  `json:"servers,omitempty"`
	Safety           catalog.Safety            `json:"safety"`
	Auth             []catalog.AuthRequirement `json:"auth"`
	AuthAlternatives []catalog.AuthAlternative `json:"authAlternatives,omitempty"`
	ApprovalRequired bool                      `json:"approvalRequired"`
	ApprovalStatus   string                    `json:"approvalStatus"`
	Runtime          explainRuntimeSummary     `json:"runtime"`
	RuntimeAvailable bool                      `json:"runtimeAvailable"`
}

func buildExplainReport(options cfgpkg.Options, response runtimepkg.CatalogResponse, tool *catalog.Tool) explainReport {
	report := explainReport{
		ToolID:           tool.ID,
		Summary:          tool.Summary,
		Method:           tool.Method,
		Path:             tool.Path,
		Service:          tool.ServiceID,
		Group:            tool.Group,
		Command:          tool.Command,
		Safety:           tool.Safety,
		Runtime:          explainRuntimeSummary{Mode: runtimeMode(options)},
		RuntimeAvailable: explainRuntimeAvailable(options),
	}
	report.Auth, report.AuthAlternatives = explainAuthRequirements(tool)
	if tool.Description != "" {
		report.Description = tool.Description
	}
	if len(tool.PathParams) > 0 {
		report.PathParams = append([]catalog.Parameter(nil), tool.PathParams...)
	}
	if len(tool.Flags) > 0 {
		report.Parameters = append([]catalog.Parameter(nil), tool.Flags...)
	}
	if tool.RequestBody != nil {
		report.RequestBody = tool.RequestBody
	}
	if tool.Guidance != nil {
		report.Guidance = tool.Guidance
	}
	if len(tool.Servers) > 0 {
		report.Servers = append([]string(nil), tool.Servers...)
	}
	report.ApprovalStatus, report.ApprovalRequired = explainApprovalStatus(options, tool)
	return report
}

func explainAuthRequirements(tool *catalog.Tool) ([]catalog.AuthRequirement, []catalog.AuthAlternative) {
	if len(tool.Auth) > 0 {
		return append([]catalog.AuthRequirement(nil), tool.Auth...), nil
	}
	if len(tool.AuthAlternatives) == 0 {
		return []catalog.AuthRequirement{}, nil
	}
	if len(tool.AuthAlternatives) == 1 {
		return append([]catalog.AuthRequirement(nil), tool.AuthAlternatives[0].Requirements...), nil
	}
	return []catalog.AuthRequirement{}, append([]catalog.AuthAlternative(nil), tool.AuthAlternatives...)
}

func explainRuntimeAvailable(options cfgpkg.Options) bool {
	return options.Embedded || options.Demo || options.RuntimeDeployment != "" || options.RuntimeURL != ""
}

func explainApprovalStatus(options cfgpkg.Options, tool *catalog.Tool) (string, bool) {
	if tool.Safety.RequiresApproval {
		return "required", true
	}
	patterns := explainApprovalPatterns(options)
	if patterns == nil {
		return "unknown", false
	}
	if len(patterns) == 0 {
		return "not_required", false
	}
	if policypkg.MatchesAny(patterns, tool.ID) {
		return "required", true
	}
	return "not_required", false
}

func explainApprovalPatterns(options cfgpkg.Options) []string {
	if options.ConfigPath == "" {
		return nil
	}
	scopePaths := configpkg.DiscoverScopePaths(configpkg.LoadOptions{})
	if explainMatchesDiscoveredPath(options.ConfigPath, scopePaths) {
		patterns := []string{}
		for _, scope := range []configpkg.Scope{configpkg.ScopeManaged, configpkg.ScopeUser, configpkg.ScopeProject, configpkg.ScopeLocal} {
			path := scopePaths[scope]
			if path == "" {
				continue
			}
			raw, err := readConfigFile(path)
			if err != nil || raw == nil {
				continue
			}
			patterns = uniqueStrings(patterns, extractApprovalPatterns(raw))
		}
		return patterns
	}
	raw, err := readConfigFile(options.ConfigPath)
	if err != nil || raw == nil {
		return nil
	}
	return extractApprovalPatterns(raw)
}

func explainMatchesDiscoveredPath(configPath string, scopePaths map[configpkg.Scope]string) bool {
	for _, path := range scopePaths {
		if path == configPath {
			return true
		}
	}
	return false
}

func extractApprovalPatterns(raw map[string]any) []string {
	policyMap, _ := raw["policy"].(map[string]any)
	return stringSliceFromAny(policyMap["approvalRequired"])
}

func uniqueStrings(existing, additions []string) []string {
	seen := make(map[string]struct{}, len(existing)+len(additions))
	result := append([]string(nil), existing...)
	for _, value := range existing {
		seen[value] = struct{}{}
	}
	for _, value := range additions {
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		result = append(result, value)
	}
	return result
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
