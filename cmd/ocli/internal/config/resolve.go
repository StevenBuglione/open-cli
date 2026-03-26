package config

import (
	"fmt"
	"io"
	"net"
	"net/url"
	"os"
	"path/filepath"
	"strings"

	authpkg "github.com/StevenBuglione/open-cli/cmd/ocli/internal/auth"
	runtimepkg "github.com/StevenBuglione/open-cli/cmd/ocli/internal/runtime"
	configpkg "github.com/StevenBuglione/open-cli/pkg/config"
	"github.com/StevenBuglione/open-cli/pkg/instance"
)

// Options holds the fully-resolved command options used throughout the CLI.
type Options struct {
	RuntimeURL               string
	RuntimeDeployment        string
	RuntimeRequestConfigPath string
	RuntimeToken             string
	RuntimeAuth              *runtimepkg.TokenSession
	ConfigPath               string
	Mode                     string
	AgentProfile             string
	AuthActorID              string
	Format                   string
	Approval                 bool
	InstanceID               string
	SessionID                string
	HeartbeatEnabled         bool
	ConfigFingerprint        string
	StateDir                 string
	Embedded                 bool
	Demo                     bool
	Stdin                    io.Reader
	Stdout                   io.Writer
	Stderr                   io.Writer
}

const defaultRuntimeURL = "http://127.0.0.1:8765"

// ResolveHooks bundles the callback functions that ResolveCommandOptions
// delegates to for operations that live outside this package.
type ResolveHooks struct {
	LoadRuntimeConfig         func(Options) (*configpkg.RuntimeConfig, bool)
	ResolveRuntimeDeployment  func(Options) string
	ResolveLocalInstanceID    func(Options, configpkg.LocalRuntimeConfig) string
	ResolveLocalSessionID     func(Options, configpkg.LocalRuntimeConfig) string
	ResolveAgentSessionID     func() string
	ResolveInstancePaths      func(Options) (instance.Paths, error)
	ResolveRuntimeURLFromInst func(Options) (string, bool, error)
	StartManagedRuntime       func(Options) (string, error)
	LocalSessionHandshake     func(Options) (Options, error)
}

// EnvBool returns true when the named environment variable holds a truthy
// value (1, true, yes, on — case-insensitive).
func EnvBool(name string) bool {
	value := strings.TrimSpace(strings.ToLower(os.Getenv(name)))
	return value == "1" || value == "true" || value == "yes" || value == "on"
}

// DiscoverConfigPath searches standard locations for a .cli.json config file.
// Priority: project (.cli.json in CWD) > user (~/.config/oas-cli/.cli.json) > managed (/etc/oas-cli/.cli.json).
func DiscoverConfigPath() string {
	paths := configpkg.DiscoverScopePaths(configpkg.LoadOptions{})
	for _, scope := range []configpkg.Scope{configpkg.ScopeProject, configpkg.ScopeLocal, configpkg.ScopeUser, configpkg.ScopeManaged} {
		if p, ok := paths[scope]; ok {
			return p
		}
	}
	return ""
}

// BootstrapFromArgs performs a quick pre-parse of the raw CLI arguments to
// populate the initial Options before Cobra takes over.
func BootstrapFromArgs(args []string) Options {
	options := Options{
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
		case "--demo":
			options.Demo = true
		}
	}
	// Auto-discover config when --config was not explicitly provided.
	if options.ConfigPath == "" && !options.Demo {
		options.ConfigPath = DiscoverConfigPath()
	}
	return options
}

// ResolveCommandOptions fills in any options that were not explicitly provided
// by the user, resolving deployment mode, tokens, runtime URL, etc.
func ResolveCommandOptions(options Options, hooks ResolveHooks) (Options, error) {
	var cachedRuntimeCfg *configpkg.RuntimeConfig
	cachedRuntimeCfgLoaded := false
	loadCachedRuntimeConfig := func() (*configpkg.RuntimeConfig, bool) {
		if cachedRuntimeCfgLoaded {
			return cachedRuntimeCfg, cachedRuntimeCfg != nil
		}
		cachedRuntimeCfg, cachedRuntimeCfgLoaded = hooks.LoadRuntimeConfig(options)
		return cachedRuntimeCfg, cachedRuntimeCfg != nil
	}
	if !options.Demo && strings.TrimSpace(options.ConfigPath) != "" {
		if _, err := configpkg.LoadEffective(configpkg.LoadOptions{
			ProjectPath: options.ConfigPath,
			WorkingDir:  filepath.Dir(options.ConfigPath),
		}); err != nil {
			return options, err
		}
	}
	if options.InstanceID == "" {
		options.InstanceID = os.Getenv("OCLI_INSTANCE_ID")
	}
	if options.StateDir == "" {
		options.StateDir = os.Getenv("OCLI_STATE_DIR")
	}
	if options.AuthActorID == "" && hooks.ResolveAgentSessionID != nil {
		options.AuthActorID = hooks.ResolveAgentSessionID()
	}
	if options.Demo {
		return options, fmt.Errorf("demo mode has been removed; connect ocli to a remote open-cli-toolbox server instead")
	}
	if options.Embedded || EnvBool("OCLI_EMBEDDED") {
		return options, fmt.Errorf("embedded mode has been removed; connect ocli to a remote open-cli-toolbox server instead")
	}
	if options.RuntimeDeployment == "" {
		options.RuntimeDeployment = hooks.ResolveRuntimeDeployment(options)
	}
	if options.RuntimeDeployment != "" && options.RuntimeDeployment != "remote" {
		return options, fmt.Errorf("runtime.mode %q is no longer supported; configure a remote open-cli-toolbox server instead", options.RuntimeDeployment)
	}
	options.RuntimeDeployment = "remote"
	options.RuntimeRequestConfigPath = options.ConfigPath
	if options.RuntimeURL == "" {
		options.RuntimeURL = os.Getenv("OCLI_RUNTIME_URL")
	}
	if options.RuntimeURL == "" {
		if runtimeCfg, ok := loadCachedRuntimeConfig(); ok && runtimeCfg.Remote != nil && runtimeCfg.Remote.URL != "" {
			options.RuntimeURL = runtimeCfg.Remote.URL
		}
	}
	if options.RuntimeToken == "" {
		if runtimeCfg, ok := loadCachedRuntimeConfig(); ok && runtimeCfg.Remote != nil && runtimeCfg.Remote.OAuth != nil {
			paths, pathsErr := hooks.ResolveInstancePaths(options)
			if pathsErr != nil {
				return options, pathsErr
			}
			token, session, err := authpkg.ResolveToken(authpkg.TokenResolveOptions{
				RuntimeURL:   options.RuntimeURL,
				StateDir:     paths.StateDir,
				ConfigPath:   options.RuntimeRequestConfigPath,
				AgentProfile: options.AgentProfile,
				ActorID:      options.AuthActorID,
			}, *runtimeCfg.Remote.OAuth)
			if err != nil {
				return options, err
			}
			options.RuntimeToken = token
			options.RuntimeAuth = session
		}
	}
	if options.RuntimeURL == "" {
		return options, fmt.Errorf("runtime.remote.url or --runtime is required for remote operation")
	}
	if err := validateRuntimeTarget(options); err != nil {
		return options, err
	}
	return options, nil
}

func validateRuntimeTarget(options Options) error {
	if options.RuntimeURL == "" {
		return nil
	}
	u, err := url.Parse(options.RuntimeURL)
	if err != nil {
		return fmt.Errorf("invalid runtime URL: %w", err)
	}
	host := strings.ToLower(u.Hostname())
	if host == "localhost" {
		return nil
	}
	if ip := net.ParseIP(host); ip != nil && ip.IsLoopback() {
		return nil
	}
	if strings.TrimSpace(u.Scheme) == "" || strings.TrimSpace(u.Host) == "" {
		return fmt.Errorf("runtime URL must include scheme and host")
	}
	return nil
}
