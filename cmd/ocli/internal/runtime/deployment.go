package runtime

import (
	"path/filepath"

	configpkg "github.com/StevenBuglione/open-cli/pkg/config"
)

// DeploymentOptions contains the fields needed for runtime deployment resolution.
type DeploymentOptions struct {
	ConfigPath string
}

// ResolveDeployment determines the runtime deployment mode from config.
func ResolveDeployment(opts DeploymentOptions) string {
	runtimeCfg, ok := LoadConfig(opts)
	if !ok {
		return ""
	}
	mode := "auto"
	if runtimeCfg != nil && runtimeCfg.Mode != "" {
		mode = runtimeCfg.Mode
	}
	switch mode {
	case "remote":
		return "remote"
	default:
		return ""
	}
}

// LoadConfig loads the runtime config from the given options.
func LoadConfig(opts DeploymentOptions) (*configpkg.RuntimeConfig, bool) {
	if opts.ConfigPath == "" {
		return nil, false
	}
	effective, err := configpkg.LoadEffective(configpkg.LoadOptions{
		ProjectPath: opts.ConfigPath,
		WorkingDir:  filepath.Dir(opts.ConfigPath),
	})
	if err != nil {
		return nil, false
	}
	return effective.Config.Runtime, true
}
