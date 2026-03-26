package main

import (
	"context"
	"fmt"
	"io"
	"net"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"time"

	authpkg "github.com/StevenBuglione/open-cli/cmd/ocli/internal/auth"
	cmdspkg "github.com/StevenBuglione/open-cli/cmd/ocli/internal/commands"
	cfgpkg "github.com/StevenBuglione/open-cli/cmd/ocli/internal/config"
	runtimepkg "github.com/StevenBuglione/open-cli/cmd/ocli/internal/runtime"
	"github.com/StevenBuglione/open-cli/internal/version"
	"github.com/StevenBuglione/open-cli/pkg/catalog"
	configpkg "github.com/StevenBuglione/open-cli/pkg/config"
	toolsexec "github.com/StevenBuglione/open-cli/pkg/exec"
	"github.com/StevenBuglione/open-cli/pkg/instance"
	"github.com/spf13/cobra"
)

type CommandOptions = cfgpkg.Options

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
	if options.Demo {
		_, _ = fmt.Fprintln(os.Stderr, "demo mode has been removed; connect ocli to a remote open-cli-toolbox server instead")
		os.Exit(1)
	}
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
	return cmdspkg.NewRootCommand(options, args, cmdspkg.RootHooks{
		ResolveCommandOptions: resolveCommandOptions,
		NewRuntimeClient: func(opts cfgpkg.Options) (runtimepkg.Client, error) {
			return newRuntimeClient(opts)
		},
	})
}

func postJSON[T any](endpoint string, payload any, token string) (T, error) {
	return runtimepkg.PostJSON[T](endpoint, payload, token)
}

func getJSON[T any](endpoint, token string) (T, error) {
	return runtimepkg.GetJSON[T](endpoint, token)
}

func writeOutput(out io.Writer, format string, value any) error {
	return cmdspkg.WriteOutput(out, format, value)
}

func bootstrapFromArgs(args []string) CommandOptions {
	return cfgpkg.BootstrapFromArgs(args)
}

func findTool(tools []catalog.Tool, id string) *catalog.Tool {
	return cmdspkg.FindTool(tools, id)
}

func sortedServiceAliases(services []catalog.Service) []string {
	return cmdspkg.SortedServiceAliases(services)
}

func resolveCommandOptions(options CommandOptions) (CommandOptions, error) {
	return cfgpkg.ResolveCommandOptions(options, cfgpkg.ResolveHooks{
		LoadRuntimeConfig:         loadRuntimeConfig,
		ResolveRuntimeDeployment:  resolveRuntimeDeployment,
		ResolveAgentSessionID:     agentSessionIdentityProvider,
		ResolveInstancePaths:      resolveInstancePaths,
	})
}

func resolveRuntimeDeployment(options CommandOptions) string {
	return runtimepkg.ResolveDeployment(runtimepkg.DeploymentOptions{ConfigPath: options.ConfigPath})
}

func loadRuntimeConfig(options CommandOptions) (*configpkg.RuntimeConfig, bool) {
	return runtimepkg.LoadConfig(runtimepkg.DeploymentOptions{ConfigPath: options.ConfigPath})
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
	return cfgpkg.EnvBool(name)
}

func setupDemoConfig(options CommandOptions) (CommandOptions, error) {
	return options, fmt.Errorf("demo mode has been removed; connect ocli to a remote open-cli-toolbox server instead")
}
