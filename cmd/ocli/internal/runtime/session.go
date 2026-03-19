package runtime

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"path/filepath"
	"strings"
	"sync"
	"time"

	embeddedruntime "github.com/StevenBuglione/open-cli/internal/runtime"
	configpkg "github.com/StevenBuglione/open-cli/pkg/config"
)

// TokenRefreshGrace is the duration before token expiry when a refresh is attempted.
const TokenRefreshGrace = 30 * time.Second

// SessionToken holds a runtime access token and its expiry.
type SessionToken struct {
	AccessToken string
	ExpiresAt   time.Time
}

// IsExpiring returns true if the token will expire within the given grace period.
func (token SessionToken) IsExpiring(grace time.Duration) bool {
	if token.ExpiresAt.IsZero() {
		return false
	}
	return time.Now().After(token.ExpiresAt.Add(-grace))
}

// TokenSession manages a runtime session token with optional refresh support.
type TokenSession struct {
	mu        sync.Mutex
	token     SessionToken
	refresh   func(context.Context) (SessionToken, error)
	refreshed bool
}

// NewTokenSession creates a new TokenSession.
func NewTokenSession(token SessionToken, refresh func(context.Context) (SessionToken, error)) *TokenSession {
	return &TokenSession{token: token, refresh: refresh}
}

// TokenForPreflight returns a valid token, refreshing if needed.
func (session *TokenSession) TokenForPreflight(ctx context.Context, grace time.Duration) (string, error) {
	if session == nil {
		return "", nil
	}
	session.mu.Lock()
	defer session.mu.Unlock()

	if !session.token.IsExpiring(grace) {
		return session.token.AccessToken, nil
	}
	if session.refresh == nil || session.refreshed {
		return "", &HTTPError{StatusCode: http.StatusUnauthorized, Body: "authn_failed"}
	}
	refreshedToken, err := session.refresh(ctx)
	if err != nil {
		return "", &HTTPError{StatusCode: http.StatusUnauthorized, Body: "authn_failed"}
	}
	session.refreshed = true
	session.token = refreshedToken
	return session.token.AccessToken, nil
}

// HandleAuthnFailed marks the token as expired to force a refresh on the next request.
func (session *TokenSession) HandleAuthnFailed() error {
	if session == nil {
		return &HTTPError{StatusCode: http.StatusUnauthorized, Body: "authn_failed"}
	}
	session.mu.Lock()
	defer session.mu.Unlock()
	if session.refresh == nil || session.refreshed {
		return &HTTPError{StatusCode: http.StatusUnauthorized, Body: "authn_failed"}
	}
	session.token.ExpiresAt = time.Unix(0, 0)
	return nil
}

// HandshakeOptions contains the fields needed for local session handshake.
type HandshakeOptions struct {
	ConfigPath        string
	ConfigFingerprint string
	RuntimeURL        string
	SessionID         string
	InstanceID        string
	HeartbeatEnabled  bool
	Embedded          bool
	StateDir          string
	RuntimeAuth       *TokenSession
}

// PerformLocalHandshake performs a local session handshake with the runtime.
// It takes a NewClientFunc to avoid circular dependencies.
func PerformLocalHandshake(opts HandshakeOptions, newClient func(HandshakeOptions) (Client, error)) (HandshakeOptions, error) {
	if opts.ConfigFingerprint == "" {
		opts.ConfigFingerprint = ConfigFingerprint(opts.ConfigPath)
	}
	client, err := newClient(opts)
	if err != nil {
		return opts, err
	}
	info, err := client.RuntimeInfo()
	if err != nil {
		return opts, err
	}
	if err := validateRuntimeContract(info, []string{"catalog"}); err != nil {
		return opts, err
	}
	lifecycle, _ := info["lifecycle"].(map[string]any)
	if !lifecycleCapabilityEnabled(lifecycle, "heartbeat") {
		opts.HeartbeatEnabled = false
		return opts, nil
	}
	if fingerprint, _ := lifecycle["configFingerprint"].(string); fingerprint != "" && opts.ConfigFingerprint != "" && fingerprint != opts.ConfigFingerprint {
		return opts, fmt.Errorf("runtime_attach_mismatch")
	}
	if opts.SessionID == "" {
		opts.SessionID = opts.InstanceID
	}
	if _, err := client.Heartbeat(opts.SessionID); err != nil {
		return opts, err
	}
	opts.HeartbeatEnabled = true
	return opts, nil
}

func validateRuntimeContract(info map[string]any, requiredCapabilities []string) error {
	contractVersion, _ := info["contractVersion"].(string)
	if strings.TrimSpace(contractVersion) == "" {
		return nil
	}
	server := embeddedruntime.HandshakeInfo{
		ContractVersion: contractVersion,
		Capabilities:    stringSlice(info["capabilities"]),
	}
	client := embeddedruntime.HandshakeInfo{
		ContractVersion:      embeddedruntime.CurrentContractVersion,
		RequiredCapabilities: append([]string(nil), requiredCapabilities...),
	}
	return embeddedruntime.CheckCompatibility(client, server)
}

func lifecycleCapabilityEnabled(lifecycle map[string]any, capability string) bool {
	if lifecycle == nil {
		return false
	}
	switch capabilities := lifecycle["capabilities"].(type) {
	case []any:
		for _, item := range capabilities {
			if text, ok := item.(string); ok && text == capability {
				return true
			}
		}
	case []string:
		for _, item := range capabilities {
			if item == capability {
				return true
			}
		}
	}
	return false
}

func stringSlice(value any) []string {
	switch items := value.(type) {
	case []string:
		return append([]string(nil), items...)
	case []any:
		result := make([]string, 0, len(items))
		for _, item := range items {
			if text, ok := item.(string); ok && strings.TrimSpace(text) != "" {
				result = append(result, text)
			}
		}
		return result
	default:
		return nil
	}
}

// ConfigFingerprint computes a SHA-256 fingerprint of local runtime config.
func ConfigFingerprint(configPath string) string {
	if configPath == "" {
		return ""
	}
	effective, err := configpkg.LoadEffective(configpkg.LoadOptions{
		ProjectPath: configPath,
		WorkingDir:  filepath.Dir(configPath),
	})
	if err != nil || effective.Config.Runtime == nil || effective.Config.Runtime.Local == nil {
		return ""
	}
	localSources := map[string]configpkg.Source{}
	for sourceID, source := range effective.Config.Sources {
		if source.Type == "mcp" && source.Transport != nil && source.Transport.Type == "stdio" {
			localSources[sourceID] = source
		}
	}
	localServices := map[string]configpkg.Service{}
	for serviceID, service := range effective.Config.Services {
		if _, ok := localSources[service.Source]; ok {
			localServices[serviceID] = service
		}
	}
	data, err := json.Marshal(struct {
		RuntimeMode string                        `json:"runtimeMode"`
		Local       *configpkg.LocalRuntimeConfig `json:"local"`
		Sources     map[string]configpkg.Source   `json:"sources,omitempty"`
		Services    map[string]configpkg.Service  `json:"services,omitempty"`
		Policy      configpkg.PolicyConfig        `json:"policy,omitempty"`
	}{
		RuntimeMode: effective.Config.Runtime.Mode,
		Local:       effective.Config.Runtime.Local,
		Sources:     localSources,
		Services:    localServices,
		Policy:      effective.Config.Policy,
	})
	if err != nil {
		return ""
	}
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

// ShouldSendHeartbeat decides whether heartbeat should be sent for the given command.
func ShouldSendHeartbeat(commandPath string) bool {
	switch commandPath {
	case "ocli runtime stop", "ocli runtime session-close":
		return false
	default:
		return true
	}
}
