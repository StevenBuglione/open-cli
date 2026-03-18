package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	authpkg "github.com/StevenBuglione/open-cli/cmd/ocli/internal/auth"
	runtimepkg "github.com/StevenBuglione/open-cli/cmd/ocli/internal/runtime"
	"github.com/StevenBuglione/open-cli/internal/version"
	"github.com/StevenBuglione/open-cli/pkg/catalog"
	configpkg "github.com/StevenBuglione/open-cli/pkg/config"
	toolsexec "github.com/StevenBuglione/open-cli/pkg/exec"
	"github.com/StevenBuglione/open-cli/pkg/instance"
	"github.com/spf13/cobra"
	"gopkg.in/yaml.v3"
)

type CommandOptions struct {
	RuntimeURL        string
	RuntimeDeployment string
	RuntimeToken      string
	RuntimeAuth       *runtimepkg.TokenSession
	ConfigPath        string
	Mode              string
	AgentProfile      string
	Format            string
	Approval          bool
	InstanceID        string
	SessionID         string
	HeartbeatEnabled  bool
	ConfigFingerprint string
	StateDir          string
	Embedded          bool
	Stdin             io.Reader
	Stdout            io.Writer
	Stderr            io.Writer
}

type runtimeCatalogResponse = runtimepkg.CatalogResponse

type runtimeBrowserLoginMetadata = authpkg.BrowserLoginMetadata
type runtimeBrowserLoginRequest = authpkg.BrowserLoginRequest

type executeRequest = runtimepkg.ExecuteRequest
type executeResponse = runtimepkg.ExecuteResponse
type runtimeClient = runtimepkg.Client
type runtimeSessionToken = runtimepkg.SessionToken
type runtimeTokenSession = runtimepkg.TokenSession

const tokenRefreshGrace = runtimepkg.TokenRefreshGrace

type runtimeHTTPError = runtimepkg.HTTPError

func newRuntimeTokenSession(token runtimeSessionToken, refresh func(context.Context) (runtimeSessionToken, error)) *runtimeTokenSession {
	return runtimepkg.NewTokenSession(token, refresh)
}

const defaultRuntimeURL = "http://127.0.0.1:8765"

var managedRuntimeStarter = startManagedRuntime
var terminalSessionIdentityProvider = detectTerminalSessionIdentity
var agentSessionIdentityProvider = detectAgentSessionIdentity
var localSessionHandshake = performLocalSessionHandshake
var cleanupManagedProcesses = toolsexec.CleanupManagedProcesses
var runtimeProcessAlive = toolsexec.ProcessAlive

func main() {
	for _, arg := range os.Args[1:] {
		if arg == "--version" || arg == "-v" {
			fmt.Println(version.String())
			return
		}
		if arg == "--" {
			break
		}
	}
	options := bootstrapFromArgs(os.Args[1:])
	command, err := NewRootCommand(options, os.Args[1:])
	if err != nil {
		_, _ = fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	if err := command.Execute(); err != nil {
		_, _ = fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func NewRootCommand(options CommandOptions, args []string) (*cobra.Command, error) {
	var err error
	options, err = resolveCommandOptions(options)
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

	client, err := newRuntimeClient(options)
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
			if !shouldSendLocalHeartbeat(cmd) {
				return nil
			}
			_, err := client.Heartbeat(options.SessionID)
			return err
		}
		root.PersistentPostRunE = func(cmd *cobra.Command, _ []string) error {
			if !shouldSendLocalHeartbeat(cmd) {
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

	root.AddCommand(newCatalogCommand(options, response))
	root.AddCommand(newToolCommand(options, response))
	root.AddCommand(newExplainCommand(options, response))
	root.AddCommand(newWorkflowCommand(options, client))
	root.AddCommand(newRuntimeCommand(options, client))
	addDynamicToolCommands(root, options, client, response.Catalog.Services, response.View.Tools)
	root.SetArgs(args)
	return root, nil
}

func newCatalogCommand(options CommandOptions, response runtimeCatalogResponse) *cobra.Command {
	command := &cobra.Command{
		Use: "catalog",
	}
	command.AddCommand(&cobra.Command{
		Use:   "list",
		Short: "List the effective catalog",
		RunE: func(cmd *cobra.Command, _ []string) error {
			return writeOutput(options.Stdout, options.Format, response)
		},
	})
	return command
}

func newToolCommand(options CommandOptions, response runtimeCatalogResponse) *cobra.Command {
	command := &cobra.Command{Use: "tool"}
	command.AddCommand(&cobra.Command{
		Use:   "schema <tool-id>",
		Args:  cobra.ExactArgs(1),
		Short: "Render machine-readable tool schema",
		RunE: func(cmd *cobra.Command, args []string) error {
			tool := findTool(response.Catalog.Tools, args[0])
			if tool == nil {
				return fmt.Errorf("tool %s not found", args[0])
			}
			return writeOutput(options.Stdout, options.Format, tool)
		},
	})
	return command
}

func newExplainCommand(options CommandOptions, response runtimeCatalogResponse) *cobra.Command {
	return &cobra.Command{
		Use:   "explain <tool-id>",
		Args:  cobra.ExactArgs(1),
		Short: "Show guidance for a tool",
		RunE: func(cmd *cobra.Command, args []string) error {
			tool := findTool(response.Catalog.Tools, args[0])
			if tool == nil {
				return fmt.Errorf("tool %s not found", args[0])
			}
			if tool.Guidance == nil {
				return writeOutput(options.Stdout, options.Format, map[string]any{"toolId": tool.ID})
			}
			return writeOutput(options.Stdout, options.Format, map[string]any{
				"toolId":    tool.ID,
				"guidance":  tool.Guidance,
				"summary":   tool.Summary,
				"operation": tool.OperationID,
			})
		},
	}
}

func newWorkflowCommand(options CommandOptions, client runtimeClient) *cobra.Command {
	command := &cobra.Command{Use: "workflow"}
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
			return writeOutput(options.Stdout, options.Format, result)
		},
	})
	return command
}

func newRuntimeCommand(options CommandOptions, client runtimeClient) *cobra.Command {
	command := &cobra.Command{Use: "runtime"}
	command.AddCommand(&cobra.Command{
		Use:   "info",
		Short: "Show runtime metadata",
		RunE: func(cmd *cobra.Command, args []string) error {
			info, err := client.RuntimeInfo()
			if err != nil {
				return err
			}
			return writeOutput(options.Stdout, options.Format, info)
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
			return writeOutput(options.Stdout, options.Format, result)
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
			return writeOutput(options.Stdout, options.Format, result)
		},
	})
	return command
}

func addDynamicToolCommands(root *cobra.Command, options CommandOptions, client runtimeClient, services []catalog.Service, tools []catalog.Tool) {
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
			serviceCommand = &cobra.Command{Use: serviceAlias}
			root.AddCommand(serviceCommand)
			serviceCommands[serviceAlias] = serviceCommand
		}

		groupKey := serviceAlias + ":" + tool.Group
		groupCommand := groupCommands[groupKey]
		if groupCommand == nil {
			groupCommand = &cobra.Command{Use: tool.Group}
			serviceCommand.AddCommand(groupCommand)
			groupCommands[groupKey] = groupCommand
		}

		toolCopy := tool
		command := &cobra.Command{
			Use:     tool.Command,
			Args:    cobra.ExactArgs(len(tool.PathParams)),
			Short:   commandSummary(toolCopy),
			Long:    toolCopy.Description,
			Hidden:  toolCopy.Hidden,
			Aliases: append([]string(nil), toolCopy.Aliases...),
			RunE: func(cmd *cobra.Command, args []string) error {
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
				body, err := loadBody(bodyRef, cmd.InOrStdin())
				if err != nil {
					return err
				}
				result, err := client.Execute(executeRequest{
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
					return err
				}
				if len(result.Body) > 0 && options.Format == "json" {
					_, err = options.Stdout.Write(append(result.Body, '\n'))
					return err
				}
				if result.Text != "" {
					_, err = fmt.Fprintln(options.Stdout, result.Text)
					return err
				}
				return writeOutput(options.Stdout, options.Format, result)
			},
		}
		for _, flag := range tool.Flags {
			command.Flags().String(flag.Name, "", "parameter "+flag.OriginalName)
		}
		command.Flags().String("body", "", "inline request body")
		groupCommand.AddCommand(command)
	}
}

func commandSummary(tool catalog.Tool) string {
	if tool.Description != "" {
		return tool.Description
	}
	return tool.Summary
}

func loadBody(bodyRef string, stdin io.Reader) ([]byte, error) {
	switch {
	case bodyRef == "":
		return nil, nil
	case bodyRef == "-":
		return io.ReadAll(stdin)
	case strings.HasPrefix(bodyRef, "@"):
		return os.ReadFile(strings.TrimPrefix(bodyRef, "@"))
	default:
		return []byte(bodyRef), nil
	}
}

func postJSON[T any](endpoint string, payload any, token string) (T, error) {
	return runtimepkg.PostJSON[T](endpoint, payload, token)
}

func getJSON[T any](endpoint, token string) (T, error) {
	return runtimepkg.GetJSON[T](endpoint, token)
}

func writeOutput(out io.Writer, format string, value any) error {
	switch format {
	case "", "json":
		data, err := json.Marshal(value)
		if err != nil {
			return err
		}
		_, err = out.Write(append(data, '\n'))
		return err
	case "yaml":
		data, err := yaml.Marshal(value)
		if err != nil {
			return err
		}
		_, err = out.Write(data)
		return err
	case "pretty":
		data, err := json.MarshalIndent(value, "", "  ")
		if err != nil {
			return err
		}
		_, err = out.Write(append(data, '\n'))
		return err
	default:
		return fmt.Errorf("unsupported format %q", format)
	}
}

func bootstrapFromArgs(args []string) CommandOptions {
	options := CommandOptions{
		Stdout: os.Stdout,
		Stderr: os.Stderr,
	}
	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "--runtime":
			if i+1 < len(args) {
				options.RuntimeURL = args[i+1]
				i++
			}
		case "--config":
			if i+1 < len(args) {
				options.ConfigPath = args[i+1]
				i++
			}
		case "--mode":
			if i+1 < len(args) {
				options.Mode = args[i+1]
				i++
			}
		case "--agent-profile":
			if i+1 < len(args) {
				options.AgentProfile = args[i+1]
				i++
			}
		case "--format":
			if i+1 < len(args) {
				options.Format = args[i+1]
				i++
			}
		case "--approval":
			options.Approval = true
		case "--instance-id":
			if i+1 < len(args) {
				options.InstanceID = args[i+1]
				i++
			}
		case "--state-dir":
			if i+1 < len(args) {
				options.StateDir = args[i+1]
				i++
			}
		case "--embedded":
			options.Embedded = true
		}
	}
	return options
}

func findTool(tools []catalog.Tool, id string) *catalog.Tool {
	for idx := range tools {
		if tools[idx].ID == id {
			return &tools[idx]
		}
	}
	return nil
}

func sortedServiceAliases(services []catalog.Service) []string {
	aliases := make([]string, 0, len(services))
	for _, service := range services {
		aliases = append(aliases, service.Alias)
	}
	sort.Strings(aliases)
	return aliases
}

func resolveCommandOptions(options CommandOptions) (CommandOptions, error) {
	var cachedRuntimeCfg *configpkg.RuntimeConfig
	cachedRuntimeCfgLoaded := false
	loadCachedRuntimeConfig := func() (*configpkg.RuntimeConfig, bool) {
		if cachedRuntimeCfgLoaded {
			return cachedRuntimeCfg, cachedRuntimeCfg != nil
		}
		cachedRuntimeCfg, cachedRuntimeCfgLoaded = loadRuntimeConfig(options)
		return cachedRuntimeCfg, cachedRuntimeCfg != nil
	}
	if options.InstanceID == "" {
		options.InstanceID = os.Getenv("OCLI_INSTANCE_ID")
	}
	if options.StateDir == "" {
		options.StateDir = os.Getenv("OCLI_STATE_DIR")
	}
	if !options.Embedded {
		options.Embedded = envBool("OCLI_EMBEDDED")
	}
	if options.Embedded {
		options.RuntimeDeployment = "embedded"
		return options, nil
	}
	if options.RuntimeDeployment == "" {
		options.RuntimeDeployment = resolveRuntimeDeployment(options)
	}
	if options.RuntimeDeployment == "embedded" {
		options.Embedded = true
		return options, nil
	}
	if options.RuntimeDeployment == "local" && options.InstanceID == "" {
		if runtimeCfg, ok := loadCachedRuntimeConfig(); ok && runtimeCfg.Local != nil {
			options.InstanceID = resolveLocalRuntimeInstanceID(options, *runtimeCfg.Local)
		}
	}
	if options.RuntimeDeployment == "local" && options.SessionID == "" {
		if runtimeCfg, ok := loadCachedRuntimeConfig(); ok && runtimeCfg.Local != nil {
			options.SessionID = resolveLocalSessionID(options, *runtimeCfg.Local)
		}
		if options.SessionID == "" {
			options.SessionID = options.InstanceID
		}
	}
	if options.Embedded {
		return options, nil
	}
	if options.RuntimeURL == "" {
		options.RuntimeURL = os.Getenv("OCLI_RUNTIME_URL")
	}
	if options.RuntimeURL == "" && options.RuntimeDeployment == "remote" {
		if runtimeCfg, ok := loadCachedRuntimeConfig(); ok && runtimeCfg.Remote != nil && runtimeCfg.Remote.URL != "" {
			options.RuntimeURL = runtimeCfg.Remote.URL
		}
	}
	if options.RuntimeToken == "" && options.RuntimeDeployment == "remote" {
		if runtimeCfg, ok := loadCachedRuntimeConfig(); ok && runtimeCfg.Remote != nil && runtimeCfg.Remote.OAuth != nil {
			paths, pathsErr := resolveInstancePaths(options)
			if pathsErr != nil {
				return options, pathsErr
			}
			token, session, err := authpkg.ResolveToken(authpkg.TokenResolveOptions{
				RuntimeURL: options.RuntimeURL,
				StateDir:   paths.StateDir,
			}, *runtimeCfg.Remote.OAuth)
			if err != nil {
				return options, err
			}
			options.RuntimeToken = token
			options.RuntimeAuth = session
		}
	}
	if options.RuntimeURL == "" {
		if runtimeURL, ok, err := resolveRuntimeURLFromInstance(options); err != nil {
			return options, err
		} else if ok {
			options.RuntimeURL = runtimeURL
		}
	}
	if options.RuntimeURL == "" && options.RuntimeDeployment == "local" {
		runtimeURL, err := managedRuntimeStarter(options)
		if err != nil {
			return options, err
		}
		options.RuntimeURL = runtimeURL
	}
	if options.RuntimeURL == "" {
		options.RuntimeURL = defaultRuntimeURL
	}
	if options.RuntimeDeployment == "local" && options.RuntimeURL != "" {
		handshaken, err := localSessionHandshake(options)
		if err != nil {
			return options, err
		}
		options = handshaken
	}
	return options, nil
}

func resolveRuntimeDeployment(options CommandOptions) string {
	return runtimepkg.ResolveDeployment(runtimepkg.DeploymentOptions{ConfigPath: options.ConfigPath})
}

func loadRuntimeConfig(options CommandOptions) (*configpkg.RuntimeConfig, bool) {
	return runtimepkg.LoadConfig(runtimepkg.DeploymentOptions{ConfigPath: options.ConfigPath})
}

func hasLocalMCPSource(cfg configpkg.Config) bool {
	return runtimepkg.HasLocalMCPSource(cfg)
}

func resolveLocalRuntimeInstanceID(options CommandOptions, local configpkg.LocalRuntimeConfig) string {
	baseID := instance.DeriveID("", options.ConfigPath)
	switch local.SessionScope {
	case "shared-group":
		if local.ShareKey == "" {
			return baseID
		}
		return instance.DeriveID(baseID+"-"+local.ShareKey, "")
	case "terminal":
		identity := terminalSessionIdentityProvider()
		if identity == "" {
			identity = "terminal"
		}
		return instance.DeriveID(baseID+"-"+identity, "")
	case "agent":
		identity := agentSessionIdentityProvider()
		if identity == "" {
			identity = terminalSessionIdentityProvider()
		}
		if identity == "" {
			identity = "agent"
		}
		return instance.DeriveID(baseID+"-"+identity, "")
	default:
		return baseID
	}
}

func resolveLocalSessionID(options CommandOptions, local configpkg.LocalRuntimeConfig) string {
	switch local.SessionScope {
	case "agent":
		if identity := agentSessionIdentityProvider(); identity != "" {
			return instance.DeriveID(identity, "")
		}
	case "terminal":
		if identity := terminalSessionIdentityProvider(); identity != "" {
			return instance.DeriveID(identity, "")
		}
	case "shared-group":
		if identity := agentSessionIdentityProvider(); identity != "" {
			return instance.DeriveID(identity, "")
		}
		if identity := terminalSessionIdentityProvider(); identity != "" {
			return instance.DeriveID(identity, "")
		}
	}
	if options.InstanceID != "" {
		return options.InstanceID
	}
	return instance.DeriveID("local-session-"+options.ConfigPath, "")
}

func performLocalSessionHandshake(options CommandOptions) (CommandOptions, error) {
	hsOpts := runtimepkg.HandshakeOptions{
		ConfigPath:        options.ConfigPath,
		ConfigFingerprint: options.ConfigFingerprint,
		RuntimeURL:        options.RuntimeURL,
		SessionID:         options.SessionID,
		InstanceID:        options.InstanceID,
		HeartbeatEnabled:  options.HeartbeatEnabled,
		Embedded:          options.Embedded,
		StateDir:          options.StateDir,
		RuntimeAuth:       options.RuntimeAuth,
	}
	newClient := func(h runtimepkg.HandshakeOptions) (runtimepkg.Client, error) {
		return runtimepkg.NewClient(runtimepkg.NewClientOptions{
			Embedded:          h.Embedded,
			RuntimeURL:        h.RuntimeURL,
			ConfigPath:        h.ConfigPath,
			InstanceID:        h.InstanceID,
			StateDir:          h.StateDir,
			SessionID:         h.SessionID,
			ConfigFingerprint: h.ConfigFingerprint,
			RuntimeAuth:       h.RuntimeAuth,
		})
	}
	result, err := runtimepkg.PerformLocalHandshake(hsOpts, newClient)
	if err != nil {
		return options, err
	}
	options.ConfigFingerprint = result.ConfigFingerprint
	options.SessionID = result.SessionID
	options.HeartbeatEnabled = result.HeartbeatEnabled
	return options, nil
}

func localRuntimeConfigFingerprint(options CommandOptions) string {
	return runtimepkg.ConfigFingerprint(options.ConfigPath)
}

func startManagedRuntime(options CommandOptions) (string, error) {
	paths, err := resolveInstancePaths(options)
	if err != nil {
		return "", err
	}
	binary, err := resolveDaemonBinary()
	if err != nil {
		return "", err
	}
	var runtimeCfg *configpkg.RuntimeConfig
	if loaded, ok := loadRuntimeConfig(options); ok {
		runtimeCfg = loaded
	}
	args := managedRuntimeArgs(options, runtimeCfg)
	cmd := exec.Command(binary, args...)
	cmd.Stdout = io.Discard
	cmd.Stderr = io.Discard
	configureManagedRuntimeCommand(cmd)
	if err := cmd.Start(); err != nil {
		return "", err
	}
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		info, err := instance.ReadRuntimeInfo(paths.RuntimePath)
		if err == nil && runtimeInfoReachable(info) {
			return info.URL, nil
		}
		time.Sleep(50 * time.Millisecond)
	}
	if cmd.Process != nil {
		_ = cmd.Process.Kill()
	}
	return "", fmt.Errorf("managed runtime did not become ready")
}

func managedRuntimeArgs(options CommandOptions, runtimeCfg *configpkg.RuntimeConfig) []string {
	args := []string{"--config", options.ConfigPath}
	if options.InstanceID != "" {
		args = append(args, "--instance-id", options.InstanceID)
	}
	if options.StateDir != "" {
		args = append(args, "--state-dir", options.StateDir)
	}
	if runtimeCfg == nil || runtimeCfg.Local == nil {
		return args
	}
	if runtimeCfg.Local.HeartbeatSeconds > 0 {
		args = append(args, "--heartbeat-seconds", strconv.Itoa(runtimeCfg.Local.HeartbeatSeconds))
	}
	if runtimeCfg.Local.MissedHeartbeatLimit > 0 {
		args = append(args, "--missed-heartbeat-limit", strconv.Itoa(runtimeCfg.Local.MissedHeartbeatLimit))
	}
	if runtimeCfg.Local.Shutdown != "" {
		args = append(args, "--shutdown", runtimeCfg.Local.Shutdown)
	}
	if runtimeCfg.Local.SessionScope != "" {
		args = append(args, "--session-scope", runtimeCfg.Local.SessionScope)
	}
	if runtimeCfg.Local.Share != "" {
		args = append(args, "--share", runtimeCfg.Local.Share)
	}
	if runtimeCfg.Local.ShareKey != "" {
		args = append(args, "--share-key-present", "true")
	}
	if options.ConfigFingerprint != "" {
		args = append(args, "--config-fingerprint", options.ConfigFingerprint)
	}
	return args
}

func resolveDaemonBinary() (string, error) {
	executable, err := os.Executable()
	if err == nil {
		sibling := filepath.Join(filepath.Dir(executable), "oclird")
		if _, statErr := os.Stat(sibling); statErr == nil {
			return sibling, nil
		}
	}
	path, err := exec.LookPath("oclird")
	if err != nil {
		return "", fmt.Errorf("resolve oclird binary: %w", err)
	}
	return path, nil
}

func newRuntimeClient(options CommandOptions) (runtimeClient, error) {
	return runtimepkg.NewClient(runtimepkg.NewClientOptions{
		Embedded:          options.Embedded,
		RuntimeURL:        options.RuntimeURL,
		ConfigPath:        options.ConfigPath,
		InstanceID:        options.InstanceID,
		StateDir:          options.StateDir,
		SessionID:         options.SessionID,
		ConfigFingerprint: options.ConfigFingerprint,
		RuntimeAuth:       options.RuntimeAuth,
	})
}

func resolveRuntimeURLFromInstance(options CommandOptions) (string, bool, error) {
	paths, err := resolveInstancePaths(options)
	if err != nil {
		return "", false, nil
	}
	info, err := instance.ReadRuntimeInfo(paths.RuntimePath)
	if err != nil || info.URL == "" {
		return "", false, nil
	}
	if !runtimeInfoReachable(info) {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		if err := cleanupManagedProcesses(ctx, paths.StateDir); err != nil {
			return "", false, err
		}
		_ = os.Remove(paths.RuntimePath)
		return "", false, nil
	}
	return info.URL, true, nil
}

func runtimeInfoReachable(info instance.RuntimeInfo) bool {
	if info.URL == "" {
		return false
	}
	if info.PID > 0 && !runtimeProcessAlive(info.PID) {
		return false
	}
	return runtimeURLReachable(info.URL)
}

func runtimeURLReachable(runtimeURL string) bool {
	parsed, err := url.Parse(runtimeURL)
	if err != nil || parsed.Host == "" {
		return false
	}
	conn, err := net.DialTimeout("tcp", parsed.Host, time.Second)
	if err != nil {
		return false
	}
	_ = conn.Close()
	return true
}

func resolveInstancePaths(options CommandOptions) (instance.Paths, error) {
	return instance.Resolve(instance.Options{
		InstanceID: options.InstanceID,
		ConfigPath: options.ConfigPath,
		StateRoot:  options.StateDir,
		CacheRoot:  runtimepkg.CacheRootForState(options.StateDir),
	})
}

func cacheRootForState(stateDir string) string {
	return runtimepkg.CacheRootForState(stateDir)
}

func detectTerminalSessionIdentity() string {
	if value := os.Getenv("OCLI_TERMINAL_SESSION_ID"); value != "" {
		return value
	}
	for _, fdPath := range []string{"/proc/self/fd/0", "/proc/self/fd/1", "/proc/self/fd/2"} {
		target, err := os.Readlink(fdPath)
		if err == nil && target != "" {
			return target
		}
	}
	return ""
}

func detectAgentSessionIdentity() string {
	for _, name := range []string{"OCLI_AGENT_SESSION_ID", "COPILOT_SESSION_ID"} {
		if value := os.Getenv(name); value != "" {
			return value
		}
	}
	return ""
}

func configureManagedRuntimeCommand(cmd *exec.Cmd) {
	configureManagedRuntimePlatform(cmd)
}

func shouldSendLocalHeartbeat(cmd *cobra.Command) bool {
	if cmd == nil {
		return false
	}
	return runtimepkg.ShouldSendHeartbeat(cmd.CommandPath())
}

func envBool(name string) bool {
	value := strings.TrimSpace(strings.ToLower(os.Getenv(name)))
	return value == "1" || value == "true" || value == "yes" || value == "on"
}
