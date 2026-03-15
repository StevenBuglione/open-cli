package config

import (
	"fmt"
	"strings"
)

type ModeConfig struct {
	Default string `json:"default"`
}

type RuntimeConfig struct {
	Mode   string               `json:"mode,omitempty"`
	Local  *LocalRuntimeConfig  `json:"local,omitempty"`
	Remote *RemoteRuntimeConfig `json:"remote,omitempty"`
	Server *RuntimeServerConfig `json:"server,omitempty"`
}

type LocalRuntimeConfig struct {
	SessionScope         string `json:"sessionScope,omitempty"`
	HeartbeatSeconds     int    `json:"heartbeatSeconds,omitempty"`
	MissedHeartbeatLimit int    `json:"missedHeartbeatLimit,omitempty"`
	Shutdown             string `json:"shutdown,omitempty"`
	Share                string `json:"share,omitempty"`
	ShareKey             string `json:"shareKey,omitempty"`
}

type RemoteRuntimeConfig struct {
	URL   string             `json:"url,omitempty"`
	OAuth *RemoteOAuthConfig `json:"oauth,omitempty"`
}

type RemoteOAuthConfig struct {
	Mode         string                   `json:"mode,omitempty"`
	Audience     string                   `json:"audience,omitempty"`
	Scopes       []string                 `json:"scopes,omitempty"`
	TokenRef     string                   `json:"tokenRef,omitempty"`
	Client       *RemoteOAuthClientConfig `json:"client,omitempty"`
	BrowserLogin *BrowserLoginConfig      `json:"browserLogin,omitempty"`
}

type RemoteOAuthClientConfig struct {
	TokenURL     string     `json:"tokenURL,omitempty"`
	ClientID     *SecretRef `json:"clientId,omitempty"`
	ClientSecret *SecretRef `json:"clientSecret,omitempty"`
}

type BrowserLoginConfig struct {
	CallbackPort int `json:"callbackPort,omitempty"`
}

type RuntimeServerConfig struct {
	Auth *RuntimeServerAuthConfig `json:"auth,omitempty"`
}

type RuntimeServerAuthConfig struct {
	Mode             string     `json:"mode,omitempty"`
	Audience         string     `json:"audience,omitempty"`
	IntrospectionURL string     `json:"introspectionURL,omitempty"`
	AuthorizationURL string     `json:"authorizationURL,omitempty"`
	TokenURL         string     `json:"tokenURL,omitempty"`
	BrowserClientID  string     `json:"browserClientId,omitempty"`
	ClientID         *SecretRef `json:"clientId,omitempty"`
	ClientSecret     *SecretRef `json:"clientSecret,omitempty"`
}

type Source struct {
	Type          string         `json:"type"`
	URI           string         `json:"uri,omitempty"`
	Enabled       bool           `json:"enabled"`
	Refresh       *RefreshPolicy `json:"refresh,omitempty"`
	Transport     *MCPTransport  `json:"transport,omitempty"`
	DisabledTools []string       `json:"disabledTools,omitempty"`
	OAuth         *OAuthConfig   `json:"oauth,omitempty"`
}

type RefreshPolicy struct {
	MaxAgeSeconds int  `json:"maxAgeSeconds,omitempty"`
	ManualOnly    bool `json:"manualOnly,omitempty"`
}

type MCPTransport struct {
	Type          string            `json:"type"`
	Command       string            `json:"command,omitempty"`
	Args          []string          `json:"args,omitempty"`
	Env           map[string]string `json:"env,omitempty"`
	URL           string            `json:"url,omitempty"`
	Headers       map[string]string `json:"headers,omitempty"`
	HeaderSecrets map[string]string `json:"headerSecrets,omitempty"`
}

type OAuthConfig struct {
	Mode             string     `json:"mode,omitempty"`
	Issuer           string     `json:"issuer,omitempty"`
	AuthorizationURL string     `json:"authorizationURL,omitempty"`
	TokenURL         string     `json:"tokenURL,omitempty"`
	ClientID         *SecretRef `json:"clientId,omitempty"`
	ClientSecret     *SecretRef `json:"clientSecret,omitempty"`
	Scopes           []string   `json:"scopes,omitempty"`
	Audience         string     `json:"audience,omitempty"`
	Interactive      *bool      `json:"interactive,omitempty"`
	CallbackPort     *int       `json:"callbackPort,omitempty"`
	RedirectURI      string     `json:"redirectURI,omitempty"`
	TokenStorage     string     `json:"tokenStorage,omitempty"`
}

type Service struct {
	Source    string   `json:"source"`
	Alias     string   `json:"alias,omitempty"`
	Overlays  []string `json:"overlays,omitempty"`
	Skills    []string `json:"skills,omitempty"`
	Workflows []string `json:"workflows,omitempty"`
}

type ToolSet struct {
	Allow []string `json:"allow,omitempty"`
	Deny  []string `json:"deny,omitempty"`
}

type CurationConfig struct {
	ToolSets map[string]ToolSet `json:"toolSets,omitempty"`
}

type AgentProfile struct {
	Mode    string `json:"mode,omitempty"`
	ToolSet string `json:"toolSet,omitempty"`
}

type AgentsConfig struct {
	Profiles       map[string]AgentProfile `json:"profiles,omitempty"`
	DefaultProfile string                  `json:"defaultProfile,omitempty"`
}

type PolicyConfig struct {
	Deny             []string `json:"deny,omitempty"`
	ManagedDeny      []string `json:"-"`
	ApprovalRequired []string `json:"approvalRequired,omitempty"`
	AllowExecSecrets bool     `json:"allowExecSecrets,omitempty"`
}

type Secret struct {
	Type    string   `json:"type"`
	Value   string   `json:"value,omitempty"`
	Command []string `json:"command,omitempty"`
	OAuthConfig
}

type SecretRef struct {
	Type    string   `json:"type"`
	Value   string   `json:"value,omitempty"`
	Command []string `json:"command,omitempty"`
}

type Config struct {
	CLI      string             `json:"cli"`
	Mode     ModeConfig         `json:"mode"`
	Runtime  *RuntimeConfig     `json:"runtime,omitempty"`
	Sources  map[string]Source  `json:"sources"`
	Services map[string]Service `json:"services,omitempty"`
	Curation CurationConfig     `json:"curation,omitempty"`
	Agents   AgentsConfig       `json:"agents,omitempty"`
	Policy   PolicyConfig       `json:"policy,omitempty"`
	Secrets  map[string]Secret  `json:"secrets,omitempty"`
}

type LoadOptions struct {
	ManagedPath   string
	UserPath      string
	ProjectPath   string
	LocalPath     string
	ManagedDir    string
	UserConfigDir string
	WorkingDir    string
}

type Scope string

const (
	ScopeManaged Scope = "managed"
	ScopeUser    Scope = "user"
	ScopeProject Scope = "project"
	ScopeLocal   Scope = "local"
)

type EffectiveConfig struct {
	Config     Config
	ScopePaths map[Scope]string
	BaseDir    string
}

type Diagnostic struct {
	Path    string `json:"path"`
	Message string `json:"message"`
}

type ValidationError struct {
	Diagnostics []Diagnostic
}

func (e *ValidationError) Error() string {
	if len(e.Diagnostics) == 0 {
		return "configuration validation failed"
	}

	var builder strings.Builder
	builder.WriteString("configuration validation failed")
	for _, diagnostic := range e.Diagnostics {
		builder.WriteString(fmt.Sprintf("; %s: %s", diagnostic.Path, diagnostic.Message))
	}
	return builder.String()
}
