package commands

import (
	"fmt"

	cfgpkg "github.com/StevenBuglione/open-cli/cmd/ocli/internal/config"
	runtimepkg "github.com/StevenBuglione/open-cli/cmd/ocli/internal/runtime"
	"github.com/StevenBuglione/open-cli/pkg/catalog"
	"github.com/spf13/cobra"
)

// AddDynamicToolCommands registers one cobra sub-command per tool, grouped by
// service alias and tool group.
func AddDynamicToolCommands(root *cobra.Command, options cfgpkg.Options, client runtimepkg.Client, services []catalog.Service, tools []catalog.Tool) {
	serviceCommands := map[string]*cobra.Command{}
	groupCommands := map[string]*cobra.Command{}
	serviceAliases := map[string]string{}
	for _, service := range services {
		serviceAliases[service.ID] = service.Alias
	}

	for _, tool := range tools {
		serviceAlias := serviceAliases[tool.ServiceID]
		if serviceAlias == "" {
			serviceAlias = tool.ServiceID
		}
		serviceCommand := serviceCommands[serviceAlias]
		if serviceCommand == nil {
			serviceCommand = &cobra.Command{
				Use:   serviceAlias,
				Short: fmt.Sprintf("Commands for the %s service", serviceAlias),
			}
			root.AddCommand(serviceCommand)
			serviceCommands[serviceAlias] = serviceCommand
		}

		groupKey := serviceAlias + ":" + tool.Group
		groupCommand := groupCommands[groupKey]
		if groupCommand == nil {
			if tool.Group == serviceAlias || tool.Group == "" {
				// Skip group level when it would stutter with service name
				groupCommand = serviceCommand
			} else {
				groupCommand = &cobra.Command{
					Use:   tool.Group,
					Short: fmt.Sprintf("%s operations", tool.Group),
				}
				serviceCommand.AddCommand(groupCommand)
			}
			groupCommands[groupKey] = groupCommand
		}

		toolCopy := tool
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
				if IsTerminalReader(cmd.InOrStdin()) {
					return nil
				}
				return fmt.Errorf("accepts %d arg(s), received %d", expectedArgs, len(args))
			},
			RunE: func(cmd *cobra.Command, args []string) error {
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

				flags := map[string]string{}
				for _, flag := range toolCopy.Flags {
					value, err := cmd.Flags().GetString(flag.Name)
					if err != nil {
						return err
					}
					if value != "" {
						flags[flag.Name] = value
					}
				}
				bodyRef, _ := cmd.Flags().GetString("body")
				body, err := LoadBody(bodyRef, cmd.InOrStdin())
				if err != nil {
					return err
				}
				dryRun, _ := cmd.Flags().GetBool("dry-run")
				if dryRun {
					WriteDryRun(options.Stdout, toolCopy, args, flags, body)
					return nil
				}
				result, err := client.Execute(runtimepkg.ExecuteRequest{
					ConfigPath:   options.ConfigPath,
					Mode:         options.Mode,
					AgentProfile: options.AgentProfile,
					ToolID:       toolCopy.ID,
					PathArgs:     args,
					Flags:        flags,
					Body:         body,
					Approval:     options.Approval,
				})
				if err != nil {
					return FormatError(err,
						fmt.Sprintf("Failed to execute tool %s", toolCopy.ID),
						"Check that the target API server is running and reachable")
				}
				if len(result.Body) > 0 && options.Format == "json" {
					_, err = options.Stdout.Write(append(result.Body, '\n'))
					return err
				}
				if result.Text != "" {
					_, err = fmt.Fprintln(options.Stdout, result.Text)
					return err
				}
				return WriteOutput(options.Stdout, options.Format, result)
			},
		}
		for _, flag := range tool.Flags {
			command.Flags().String(flag.Name, "", "parameter "+flag.OriginalName)
		}
		command.Flags().String("body", "", "inline request body")
		command.Flags().Bool("dry-run", false, "Show the request without executing")
		groupCommand.AddCommand(command)
	}
}
