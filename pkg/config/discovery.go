package config

import (
	"os"
	"path/filepath"
)

func DiscoverScopePaths(options LoadOptions) map[Scope]string {
	paths := map[Scope]string{}

	if options.ManagedPath != "" {
		paths[ScopeManaged] = options.ManagedPath
	} else {
		managedDir := options.ManagedDir
		if managedDir == "" {
			managedDir = "/etc/oas-cli"
		}
		addIfExists(paths, ScopeManaged, filepath.Join(managedDir, ".cli.json"))
	}

	if options.UserPath != "" {
		paths[ScopeUser] = options.UserPath
	} else {
		userConfigDir := options.UserConfigDir
		if userConfigDir == "" {
			userConfigDir = os.Getenv("XDG_CONFIG_HOME")
		}
		if userConfigDir == "" {
			homeDir, err := os.UserHomeDir()
			if err == nil {
				userConfigDir = filepath.Join(homeDir, ".config")
			}
		}
		if userConfigDir != "" {
			addIfExists(paths, ScopeUser, filepath.Join(userConfigDir, "oas-cli", ".cli.json"))
		}
	}

	workingDir := options.WorkingDir
	if workingDir == "" {
		if cwd, err := os.Getwd(); err == nil {
			workingDir = cwd
		}
	}
	projectDir := workingDir
	if options.ProjectPath != "" {
		projectDir = filepath.Dir(options.ProjectPath)
	}
	if options.ProjectPath != "" {
		paths[ScopeProject] = options.ProjectPath
	} else if projectDir != "" {
		addIfExists(paths, ScopeProject, filepath.Join(projectDir, ".cli.json"))
	}
	if options.LocalPath != "" {
		paths[ScopeLocal] = options.LocalPath
	} else if projectDir != "" {
		addIfExists(paths, ScopeLocal, filepath.Join(projectDir, ".cli.local.json"))
	}

	return paths
}

func addIfExists(paths map[Scope]string, scope Scope, candidate string) {
	if _, err := os.Stat(candidate); err == nil {
		paths[scope] = candidate
	}
}
