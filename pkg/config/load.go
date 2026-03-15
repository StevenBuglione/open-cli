package config

import (
	"os"
	"path/filepath"
)

type rawConfig struct {
	CLI        string                  `json:"cli"`
	Mode       *ModeConfig             `json:"mode,omitempty"`
	Runtime    *rawRuntimeConfig       `json:"runtime,omitempty"`
	Sources    map[string]rawSource    `json:"sources,omitempty"`
	MCPServers map[string]rawMCPServer `json:"mcpServers,omitempty"`
	Services   map[string]rawService   `json:"services,omitempty"`
	Curation   *rawCurationConfig      `json:"curation,omitempty"`
	Agents     *rawAgentsConfig        `json:"agents,omitempty"`
	Policy     *rawPolicyConfig        `json:"policy,omitempty"`
	Secrets    map[string]Secret       `json:"secrets,omitempty"`
}

type rawSource struct {
	Type          *string          `json:"type,omitempty"`
	URI           *string          `json:"uri,omitempty"`
	Enabled       *bool            `json:"enabled,omitempty"`
	Refresh       *RefreshPolicy   `json:"refresh,omitempty"`
	Transport     *rawMCPTransport `json:"transport,omitempty"`
	DisabledTools []string         `json:"disabledTools,omitempty"`
	OAuth         *rawOAuthConfig  `json:"oauth,omitempty"`
}

type rawRuntimeConfig struct {
	Mode   *string                 `json:"mode,omitempty"`
	Local  *rawLocalRuntimeConfig  `json:"local,omitempty"`
	Remote *rawRemoteRuntimeConfig `json:"remote,omitempty"`
	Server *rawRuntimeServerConfig `json:"server,omitempty"`
}

type rawLocalRuntimeConfig struct {
	SessionScope         *string `json:"sessionScope,omitempty"`
	HeartbeatSeconds     *int    `json:"heartbeatSeconds,omitempty"`
	MissedHeartbeatLimit *int    `json:"missedHeartbeatLimit,omitempty"`
	Shutdown             *string `json:"shutdown,omitempty"`
	Share                *string `json:"share,omitempty"`
	ShareKey             *string `json:"shareKey,omitempty"`
}

type rawRemoteRuntimeConfig struct {
	URL   *string               `json:"url,omitempty"`
	OAuth *rawRemoteOAuthConfig `json:"oauth,omitempty"`
}

type rawRemoteOAuthConfig struct {
	Mode         *string                     `json:"mode,omitempty"`
	Audience     *string                     `json:"audience,omitempty"`
	Scopes       []string                    `json:"scopes,omitempty"`
	TokenRef     *string                     `json:"tokenRef,omitempty"`
	Client       *rawRemoteOAuthClientConfig `json:"client,omitempty"`
	BrowserLogin *rawBrowserLoginConfig      `json:"browserLogin,omitempty"`
}

type rawRemoteOAuthClientConfig struct {
	TokenURL     *string    `json:"tokenURL,omitempty"`
	ClientID     *SecretRef `json:"clientId,omitempty"`
	ClientSecret *SecretRef `json:"clientSecret,omitempty"`
}

type rawBrowserLoginConfig struct {
	CallbackPort *int `json:"callbackPort,omitempty"`
}

type rawRuntimeServerConfig struct {
	Auth *rawRuntimeServerAuthConfig `json:"auth,omitempty"`
}

type rawRuntimeServerAuthConfig struct {
	Mode             *string    `json:"mode,omitempty"`
	Audience         *string    `json:"audience,omitempty"`
	IntrospectionURL *string    `json:"introspectionURL,omitempty"`
	AuthorizationURL *string    `json:"authorizationURL,omitempty"`
	TokenURL         *string    `json:"tokenURL,omitempty"`
	BrowserClientID  *string    `json:"browserClientId,omitempty"`
	ClientID         *SecretRef `json:"clientId,omitempty"`
	ClientSecret     *SecretRef `json:"clientSecret,omitempty"`
}

type rawService struct {
	Source    *string  `json:"source,omitempty"`
	Alias     *string  `json:"alias,omitempty"`
	Overlays  []string `json:"overlays,omitempty"`
	Skills    []string `json:"skills,omitempty"`
	Workflows []string `json:"workflows,omitempty"`
}

type rawCurationConfig struct {
	ToolSets map[string]rawToolSet `json:"toolSets,omitempty"`
}

type rawToolSet struct {
	Allow []string `json:"allow,omitempty"`
	Deny  []string `json:"deny,omitempty"`
}

type rawAgentsConfig struct {
	Profiles       map[string]rawAgentProfile `json:"profiles,omitempty"`
	DefaultProfile *string                    `json:"defaultProfile,omitempty"`
}

type rawAgentProfile struct {
	Mode    *string `json:"mode,omitempty"`
	ToolSet *string `json:"toolSet,omitempty"`
}

type rawPolicyConfig struct {
	Deny             []string `json:"deny,omitempty"`
	ApprovalRequired []string `json:"approvalRequired,omitempty"`
	AllowExecSecrets *bool    `json:"allowExecSecrets,omitempty"`
}

func LoadEffective(options LoadOptions) (*EffectiveConfig, error) {
	effective := &EffectiveConfig{
		Config: Config{
			Sources:  map[string]Source{},
			Services: map[string]Service{},
			Curation: CurationConfig{ToolSets: map[string]ToolSet{}},
			Agents:   AgentsConfig{Profiles: map[string]AgentProfile{}},
			Secrets:  map[string]Secret{},
		},
		ScopePaths: map[Scope]string{},
	}
	sourceNames := map[string]struct{}{}
	mcpServerNames := map[string]struct{}{}

	discoveredPaths := DiscoverScopePaths(options)
	scopedPaths := []struct {
		scope Scope
		path  string
	}{
		{scope: ScopeManaged, path: discoveredPaths[ScopeManaged]},
		{scope: ScopeUser, path: discoveredPaths[ScopeUser]},
		{scope: ScopeProject, path: discoveredPaths[ScopeProject]},
		{scope: ScopeLocal, path: discoveredPaths[ScopeLocal]},
	}

	for _, entry := range scopedPaths {
		if entry.path == "" {
			continue
		}

		raw, err := loadRaw(entry.path)
		if err != nil {
			return nil, err
		}
		if err := validateMCPSourceAmbiguity(raw, sourceNames, mcpServerNames); err != nil {
			return nil, err
		}
		if err := normalizeMCPServers(&raw); err != nil {
			return nil, err
		}
		if err := normalizeMCPSources(effective.Config, &raw); err != nil {
			return nil, err
		}
		effective.ScopePaths[entry.scope] = entry.path
		effective.Config.merge(entry.scope, raw)
	}

	if projectPath := discoveredPaths[ScopeLocal]; projectPath != "" {
		effective.BaseDir = filepath.Dir(projectPath)
	} else if projectPath := discoveredPaths[ScopeProject]; projectPath != "" {
		effective.BaseDir = filepath.Dir(projectPath)
	} else if options.WorkingDir != "" {
		effective.BaseDir = options.WorkingDir
	}
	if err := validateConfig(effective.Config); err != nil {
		return nil, err
	}
	if err := validateCrossReferences(effective.Config); err != nil {
		return nil, err
	}

	return effective, nil
}

func loadRaw(path string) (rawConfig, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return rawConfig{}, err
	}
	if err := validateDocument(data, true); err != nil {
		return rawConfig{}, err
	}
	return decodeRawConfig(data)
}

func (cfg *Config) merge(scope Scope, raw rawConfig) {
	if raw.CLI != "" {
		cfg.CLI = raw.CLI
	}
	if raw.Mode != nil && raw.Mode.Default != "" {
		cfg.Mode = *raw.Mode
	}
	if raw.Runtime != nil {
		current := cfg.Runtime
		if current == nil {
			current = &RuntimeConfig{}
		}
		if raw.Runtime.Mode != nil {
			current.Mode = *raw.Runtime.Mode
		}
		if raw.Runtime.Local != nil {
			local := current.Local
			if local == nil {
				local = &LocalRuntimeConfig{}
			}
			if raw.Runtime.Local.SessionScope != nil {
				local.SessionScope = *raw.Runtime.Local.SessionScope
			}
			if raw.Runtime.Local.HeartbeatSeconds != nil {
				local.HeartbeatSeconds = *raw.Runtime.Local.HeartbeatSeconds
			}
			if raw.Runtime.Local.MissedHeartbeatLimit != nil {
				local.MissedHeartbeatLimit = *raw.Runtime.Local.MissedHeartbeatLimit
			}
			if raw.Runtime.Local.Shutdown != nil {
				local.Shutdown = *raw.Runtime.Local.Shutdown
			}
			if raw.Runtime.Local.Share != nil {
				local.Share = *raw.Runtime.Local.Share
			}
			if raw.Runtime.Local.ShareKey != nil {
				local.ShareKey = *raw.Runtime.Local.ShareKey
			}
			current.Local = local
		}
		if raw.Runtime.Remote != nil {
			remote := current.Remote
			if remote == nil {
				remote = &RemoteRuntimeConfig{}
			}
			if raw.Runtime.Remote.URL != nil {
				remote.URL = *raw.Runtime.Remote.URL
			}
			if raw.Runtime.Remote.OAuth != nil {
				oauth := remote.OAuth
				if oauth == nil {
					oauth = &RemoteOAuthConfig{}
				}
				if raw.Runtime.Remote.OAuth.Mode != nil {
					oauth.Mode = *raw.Runtime.Remote.OAuth.Mode
				}
				if raw.Runtime.Remote.OAuth.Audience != nil {
					oauth.Audience = *raw.Runtime.Remote.OAuth.Audience
				}
				if raw.Runtime.Remote.OAuth.Scopes != nil {
					oauth.Scopes = append([]string(nil), raw.Runtime.Remote.OAuth.Scopes...)
				}
				if raw.Runtime.Remote.OAuth.TokenRef != nil {
					oauth.TokenRef = *raw.Runtime.Remote.OAuth.TokenRef
				}
				if raw.Runtime.Remote.OAuth.Client != nil {
					client := oauth.Client
					if client == nil {
						client = &RemoteOAuthClientConfig{}
					}
					if raw.Runtime.Remote.OAuth.Client.TokenURL != nil {
						client.TokenURL = *raw.Runtime.Remote.OAuth.Client.TokenURL
					}
					if raw.Runtime.Remote.OAuth.Client.ClientID != nil {
						ref := *raw.Runtime.Remote.OAuth.Client.ClientID
						client.ClientID = &ref
					}
					if raw.Runtime.Remote.OAuth.Client.ClientSecret != nil {
						ref := *raw.Runtime.Remote.OAuth.Client.ClientSecret
						client.ClientSecret = &ref
					}
					oauth.Client = client
				}
				if raw.Runtime.Remote.OAuth.BrowserLogin != nil {
					browserLogin := oauth.BrowserLogin
					if browserLogin == nil {
						browserLogin = &BrowserLoginConfig{}
					}
					if raw.Runtime.Remote.OAuth.BrowserLogin.CallbackPort != nil {
						browserLogin.CallbackPort = *raw.Runtime.Remote.OAuth.BrowserLogin.CallbackPort
					}
					oauth.BrowserLogin = browserLogin
				}
				remote.OAuth = oauth
			}
			current.Remote = remote
		}
		if raw.Runtime.Server != nil {
			serverCfg := current.Server
			if serverCfg == nil {
				serverCfg = &RuntimeServerConfig{}
			}
			if raw.Runtime.Server.Auth != nil {
				auth := serverCfg.Auth
				if auth == nil {
					auth = &RuntimeServerAuthConfig{}
				}
				if raw.Runtime.Server.Auth.Mode != nil {
					auth.Mode = *raw.Runtime.Server.Auth.Mode
				}
				if raw.Runtime.Server.Auth.Audience != nil {
					auth.Audience = *raw.Runtime.Server.Auth.Audience
				}
				if raw.Runtime.Server.Auth.IntrospectionURL != nil {
					auth.IntrospectionURL = *raw.Runtime.Server.Auth.IntrospectionURL
				}
				if raw.Runtime.Server.Auth.AuthorizationURL != nil {
					auth.AuthorizationURL = *raw.Runtime.Server.Auth.AuthorizationURL
				}
				if raw.Runtime.Server.Auth.TokenURL != nil {
					auth.TokenURL = *raw.Runtime.Server.Auth.TokenURL
				}
				if raw.Runtime.Server.Auth.BrowserClientID != nil {
					auth.BrowserClientID = *raw.Runtime.Server.Auth.BrowserClientID
				}
				if raw.Runtime.Server.Auth.ClientID != nil {
					ref := *raw.Runtime.Server.Auth.ClientID
					auth.ClientID = &ref
				}
				if raw.Runtime.Server.Auth.ClientSecret != nil {
					ref := *raw.Runtime.Server.Auth.ClientSecret
					auth.ClientSecret = &ref
				}
				serverCfg.Auth = auth
			}
			current.Server = serverCfg
		}
		cfg.Runtime = current
	}

	for key, source := range raw.Sources {
		current := cfg.Sources[key]
		isNewSource := isZeroSource(current)
		if source.Type != nil {
			resetSourceExclusiveFields(&current, *source.Type)
			current.Type = *source.Type
		}
		transportTypeChanged := false
		newTransportType := ""
		if source.Transport != nil && source.Transport.Type != nil {
			newTransportType = *source.Transport.Type
			transportTypeChanged = current.Transport == nil || current.Transport.Type != newTransportType
		}
		if source.URI != nil {
			current.URI = *source.URI
		}
		if source.Enabled != nil {
			current.Enabled = *source.Enabled
		} else if isNewSource {
			current.Enabled = true
		}
		if source.Refresh != nil {
			current.Refresh = source.Refresh
		}
		current.Transport = mergeRawTransport(current.Transport, source.Transport)
		if source.DisabledTools != nil {
			current.DisabledTools = copyStrings(source.DisabledTools)
		}
		if transportTypeChanged && newTransportType != "streamable-http" && source.OAuth == nil {
			current.OAuth = nil
		}
		current.OAuth = mergeRawOAuth(current.OAuth, source.OAuth)
		cfg.Sources[key] = current
	}

	for key, service := range raw.Services {
		current := cfg.Services[key]
		if service.Source != nil {
			current.Source = *service.Source
		}
		if service.Alias != nil {
			current.Alias = *service.Alias
		}
		if service.Overlays != nil {
			current.Overlays = append([]string(nil), service.Overlays...)
		}
		if service.Skills != nil {
			current.Skills = append([]string(nil), service.Skills...)
		}
		if service.Workflows != nil {
			current.Workflows = append([]string(nil), service.Workflows...)
		}
		cfg.Services[key] = current
	}

	if raw.Curation != nil {
		if cfg.Curation.ToolSets == nil {
			cfg.Curation.ToolSets = map[string]ToolSet{}
		}
		for key, rawToolSet := range raw.Curation.ToolSets {
			current := cfg.Curation.ToolSets[key]
			current.Allow = uniqueStrings(current.Allow, rawToolSet.Allow)
			current.Deny = uniqueStrings(current.Deny, rawToolSet.Deny)
			cfg.Curation.ToolSets[key] = current
		}
	}

	if raw.Agents != nil {
		if cfg.Agents.Profiles == nil {
			cfg.Agents.Profiles = map[string]AgentProfile{}
		}
		if raw.Agents.DefaultProfile != nil {
			cfg.Agents.DefaultProfile = *raw.Agents.DefaultProfile
		}
		for key, rawProfile := range raw.Agents.Profiles {
			current := cfg.Agents.Profiles[key]
			if rawProfile.Mode != nil {
				current.Mode = *rawProfile.Mode
			}
			if rawProfile.ToolSet != nil {
				current.ToolSet = *rawProfile.ToolSet
			}
			cfg.Agents.Profiles[key] = current
		}
	}

	if raw.Policy != nil {
		cfg.Policy.Deny = uniqueStrings(cfg.Policy.Deny, raw.Policy.Deny)
		if scope == ScopeManaged {
			cfg.Policy.ManagedDeny = uniqueStrings(cfg.Policy.ManagedDeny, raw.Policy.Deny)
		}
		cfg.Policy.ApprovalRequired = uniqueStrings(cfg.Policy.ApprovalRequired, raw.Policy.ApprovalRequired)
		if raw.Policy.AllowExecSecrets != nil {
			cfg.Policy.AllowExecSecrets = *raw.Policy.AllowExecSecrets
		}
	}

	for key, secret := range raw.Secrets {
		cfg.Secrets[key] = secret
	}
}

func validateCrossReferences(cfg Config) error {
	var diagnostics []Diagnostic
	for key, service := range cfg.Services {
		if service.Source == "" {
			diagnostics = append(diagnostics, Diagnostic{Path: "services." + key + ".source", Message: "is required"})
			continue
		}
		if _, ok := cfg.Sources[service.Source]; !ok {
			diagnostics = append(diagnostics, Diagnostic{Path: "services." + key + ".source", Message: "references unknown source"})
			continue
		}
		if cfg.Sources[service.Source].Type == "mcp" && key != service.Source {
			diagnostics = append(diagnostics, Diagnostic{
				Path:    "services." + key + ".source",
				Message: "is a second service pointing at mcp source " + service.Source,
			})
		}
	}
	for sourceName, source := range cfg.Sources {
		if source.Type != "mcp" {
			continue
		}
		service, ok := cfg.Services[sourceName]
		if !ok || service.Source == "" {
			diagnostics = append(diagnostics, Diagnostic{
				Path:    "services." + sourceName + ".source",
				Message: "must reference \"" + sourceName + "\" for mcp source",
			})
			continue
		}
		if service.Source != sourceName {
			diagnostics = append(diagnostics, Diagnostic{
				Path:    "services." + sourceName + ".source",
				Message: "must reference \"" + sourceName + "\" for mcp source",
			})
		}
	}
	if len(diagnostics) > 0 {
		return &ValidationError{Diagnostics: diagnostics}
	}
	return nil
}

func uniqueStrings(existing, additional []string) []string {
	seen := map[string]struct{}{}
	var merged []string

	for _, item := range existing {
		if item == "" {
			continue
		}
		if _, ok := seen[item]; ok {
			continue
		}
		seen[item] = struct{}{}
		merged = append(merged, item)
	}
	for _, item := range additional {
		if item == "" {
			continue
		}
		if _, ok := seen[item]; ok {
			continue
		}
		seen[item] = struct{}{}
		merged = append(merged, item)
	}

	return merged
}

func resetSourceExclusiveFields(source *Source, newType string) {
	if newType == "mcp" {
		source.URI = ""
		return
	}

	source.Transport = nil
	source.DisabledTools = nil
	source.OAuth = nil
}
