package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	configpkg "github.com/StevenBuglione/open-cli/pkg/config"
)

func writeResolvedConfig(t *testing.T, content string) string {
	t.Helper()
	dir := t.TempDir()
	configPath := filepath.Join(dir, ".cli.json")
	if err := os.WriteFile(configPath, []byte(content), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}
	return configPath
}

func XTestResolveCommandOptionsRejectsEmbeddedDeploymentForNormalConfig(t *testing.T) {
	options := Options{ConfigPath: writeResolvedConfig(t, `{
  "cli": "1.0.0",
  "mode": {"default": "discover"},
  "runtime": {"mode": "local", "local": {"heartbeatSeconds": 15, "missedHeartbeatLimit": 3}},
  "sources": {"demo": {"type": "openapi", "uri": "https://example.com/openapi.json", "enabled": true}}
}`)}

	_, err := ResolveCommandOptions(options, ResolveHooks{
		LoadRuntimeConfig: func(Options) (*configpkg.RuntimeConfig, bool) {
			return &configpkg.RuntimeConfig{Mode: "local", Local: &configpkg.LocalRuntimeConfig{}}, true
		},
		ResolveRuntimeDeployment: func(Options) string { return "embedded" },
	})
	if err == nil {
		t.Fatal("expected embedded deployment to be rejected")
	}
	if !strings.Contains(err.Error(), "embedded") {
		t.Fatalf("expected embedded rejection, got %v", err)
	}
}

func XTestResolveCommandOptionsRejectsEmbeddedFlagForNormalConfig(t *testing.T) {
	options := Options{ConfigPath: writeResolvedConfig(t, `{
  "cli": "1.0.0",
  "mode": {"default": "discover"},
  "runtime": {"mode": "local", "local": {"heartbeatSeconds": 15, "missedHeartbeatLimit": 3}},
  "sources": {"demo": {"type": "openapi", "uri": "https://example.com/openapi.json", "enabled": true}}
}`), Embedded: true}

	_, err := ResolveCommandOptions(options, ResolveHooks{
		LoadRuntimeConfig: func(Options) (*configpkg.RuntimeConfig, bool) {
			return &configpkg.RuntimeConfig{Mode: "local", Local: &configpkg.LocalRuntimeConfig{}}, true
		},
		ResolveLocalInstanceID: func(Options, configpkg.LocalRuntimeConfig) string { return "instance" },
		ResolveLocalSessionID:  func(Options, configpkg.LocalRuntimeConfig) string { return "session" },
		LocalSessionHandshake:  func(options Options) (Options, error) { return options, nil },
	})
	if err == nil {
		t.Fatal("expected embedded flag to be rejected")
	}
	if !strings.Contains(err.Error(), "embedded") {
		t.Fatalf("expected embedded rejection, got %v", err)
	}
}

func XTestResolveCommandOptionsRejectsNonLocalRuntimeOverrideForLocalMode(t *testing.T) {
	options := Options{
		ConfigPath: writeResolvedConfig(t, `{
  "cli": "1.0.0",
  "mode": {"default": "discover"},
  "runtime": {"mode": "local", "local": {"heartbeatSeconds": 15, "missedHeartbeatLimit": 3}},
  "sources": {"demo": {"type": "openapi", "uri": "https://example.com/openapi.json", "enabled": true}}
}`),
		RuntimeDeployment: "local",
		RuntimeURL:        "https://runtime.example.com",
	}

	_, err := ResolveCommandOptions(options, ResolveHooks{
		LoadRuntimeConfig: func(Options) (*configpkg.RuntimeConfig, bool) {
			return &configpkg.RuntimeConfig{Mode: "local", Local: &configpkg.LocalRuntimeConfig{}}, true
		},
		ResolveLocalInstanceID: func(Options, configpkg.LocalRuntimeConfig) string { return "instance" },
		ResolveLocalSessionID:  func(Options, configpkg.LocalRuntimeConfig) string { return "session" },
		LocalSessionHandshake:  func(options Options) (Options, error) { return options, nil },
	})
	if err == nil {
		t.Fatal("expected non-local runtime override to be rejected")
	}
	if !strings.Contains(err.Error(), "local") {
		t.Fatalf("expected local override rejection, got %v", err)
	}
}

func TestResolveCommandOptionsUsesEnvRuntimeURLBeforeConfig(t *testing.T) {
	t.Setenv("OCLI_RUNTIME_URL", "https://env.example.com")

	options := Options{ConfigPath: writeResolvedConfig(t, `{
  "cli": "1.0.0",
  "mode": {"default": "discover"},
  "runtime": {"mode": "remote", "remote": {"url": "https://config.example.com"}},
  "sources": {"demo": {"type": "openapi", "uri": "https://example.com/openapi.json", "enabled": true}}
}`), RuntimeDeployment: "remote"}
	got, err := ResolveCommandOptions(options, ResolveHooks{
		LoadRuntimeConfig: func(Options) (*configpkg.RuntimeConfig, bool) {
			return &configpkg.RuntimeConfig{
				Mode: "remote",
				Remote: &configpkg.RemoteRuntimeConfig{
					URL: "https://config.example.com",
				},
			}, true
		},
	})
	if err != nil {
		t.Fatalf("ResolveCommandOptions returned error: %v", err)
	}
	if got.RuntimeURL != "https://env.example.com" {
		t.Fatalf("expected env runtime URL, got %q", got.RuntimeURL)
	}
}

func TestResolveCommandOptionsFailsFastOnMissingRuntimeConfig(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, ".cli.json")
	if err := os.WriteFile(configPath, []byte(`{
  "cli": "1.0.0",
  "mode": {"default": "discover"},
  "sources": {}
}`), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	_, err := ResolveCommandOptions(Options{ConfigPath: configPath}, ResolveHooks{})
	if err == nil {
		t.Fatal("expected missing runtime config to fail")
	}
	if !strings.Contains(err.Error(), "runtime") {
		t.Fatalf("expected runtime validation error, got %v", err)
	}
}

func TestResolveCommandOptionsFailsFastOnUnsupportedRuntimeMode(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, ".cli.json")
	if err := os.WriteFile(configPath, []byte(`{
  "cli": "1.0.0",
  "mode": {"default": "discover"},
  "runtime": {
    "mode": "embedded"
  },
  "sources": {}
}`), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	_, err := ResolveCommandOptions(Options{ConfigPath: configPath}, ResolveHooks{})
	if err == nil {
		t.Fatal("expected unsupported runtime mode to fail")
	}
	if !strings.Contains(err.Error(), "remote") {
		t.Fatalf("expected local-or-remote validation error, got %v", err)
	}
}
