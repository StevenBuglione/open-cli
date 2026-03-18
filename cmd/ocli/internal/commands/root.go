package commands

import (
	"os"

	cfgpkg "github.com/StevenBuglione/open-cli/cmd/ocli/internal/config"
	runtimepkg "github.com/StevenBuglione/open-cli/cmd/ocli/internal/runtime"
	"github.com/StevenBuglione/open-cli/internal/version"
	"github.com/spf13/cobra"
)

// RootHooks bundles the callback functions that NewRootCommand delegates to
// for operations defined outside the commands package.
type RootHooks struct {
	ResolveCommandOptions func(cfgpkg.Options) (cfgpkg.Options, error)
	NewRuntimeClient      func(cfgpkg.Options) (runtimepkg.Client, error)
	ShouldSendHeartbeat   func(*cobra.Command) bool
}

// NewRootCommand creates the top-level cobra command tree, resolves options,
// fetches the catalog, and registers all sub-commands.
func NewRootCommand(options cfgpkg.Options, args []string, hooks RootHooks) (*cobra.Command, error) {
	var err error
	options, err = hooks.ResolveCommandOptions(options)
	if err != nil {
		return nil, err
	}
	if options.Format == "" {
		options.Format = "json"
	}
	if options.Stdout == nil {
		options.Stdout = os.Stdout
	}
	if options.Stderr == nil {
		options.Stderr = os.Stderr
	}
	if options.Stdin == nil {
		options.Stdin = os.Stdin
	}

	client, err := hooks.NewRuntimeClient(options)
	if err != nil {
		return nil, err
	}
	response, err := client.FetchCatalog(runtimepkg.CatalogFetchOptions{
		ConfigPath:   options.ConfigPath,
		Mode:         options.Mode,
		AgentProfile: options.AgentProfile,
		RuntimeToken: options.RuntimeToken,
	})
	if err != nil {
		return nil, err
	}

	root := &cobra.Command{
		Use:           "ocli",
		Version:       version.Version,
		SilenceUsage:  true,
		SilenceErrors: true,
	}
	root.SetVersionTemplate(version.String() + "\n")
	if options.RuntimeDeployment == "local" && options.HeartbeatEnabled && options.SessionID != "" {
		root.PersistentPreRunE = func(cmd *cobra.Command, _ []string) error {
			if !hooks.ShouldSendHeartbeat(cmd) {
				return nil
			}
			_, err := client.Heartbeat(options.SessionID)
			return err
		}
		root.PersistentPostRunE = func(cmd *cobra.Command, _ []string) error {
			if !hooks.ShouldSendHeartbeat(cmd) {
				return nil
			}
			_, err := client.Heartbeat(options.SessionID)
			return err
		}
	}
	root.SetOut(options.Stdout)
	root.SetErr(options.Stderr)
	root.SetIn(options.Stdin)
	root.PersistentFlags().StringVar(&options.RuntimeURL, "runtime", options.RuntimeURL, "Runtime base URL")
	root.PersistentFlags().StringVar(&options.ConfigPath, "config", options.ConfigPath, "Path to .cli.json")
	root.PersistentFlags().StringVar(&options.Mode, "mode", options.Mode, "Execution mode")
	root.PersistentFlags().StringVar(&options.AgentProfile, "agent-profile", options.AgentProfile, "Agent profile")
	root.PersistentFlags().StringVar(&options.Format, "format", options.Format, "Output format")
	root.PersistentFlags().BoolVar(&options.Approval, "approval", options.Approval, "Grant approval for protected tools")
	root.PersistentFlags().StringVar(&options.InstanceID, "instance-id", options.InstanceID, "Instance id for isolated runtime resolution")
	root.PersistentFlags().StringVar(&options.StateDir, "state-dir", options.StateDir, "State directory root for runtime metadata")
	root.PersistentFlags().BoolVar(&options.Embedded, "embedded", options.Embedded, "Use the embedded runtime instead of an external daemon")

	root.AddCommand(NewCatalogCommand(options, response))
	root.AddCommand(NewToolCommand(options, response))
	root.AddCommand(NewExplainCommand(options, response))
	root.AddCommand(NewWorkflowCommand(options, client))
	root.AddCommand(NewRuntimeCommand(options, client))
	AddDynamicToolCommands(root, options, client, response.Catalog.Services, response.View.Tools)
	root.SetArgs(args)
	return root, nil
}
