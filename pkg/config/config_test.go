package config_test

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/StevenBuglione/open-cli/pkg/config"
)

func writeJSON(t *testing.T, dir, name, content string) string {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write %s: %v", name, err)
	}
	return path
}

func requireStringField(t *testing.T, value any, fieldName, want string) {
	t.Helper()

	field := reflect.ValueOf(value).FieldByName(fieldName)
	if !field.IsValid() {
		t.Fatalf("expected %s field to be present", fieldName)
	}
	if got := field.String(); got != want {
		t.Fatalf("expected %s %q, got %q", fieldName, want, got)
	}
}

func TestLoadEffectiveMergesScopesAndPreservesManagedDenies(t *testing.T) {
	dir := t.TempDir()

	managedPath := writeJSON(t, dir, "managed.json", `{
	  "cli": "1.0.0",
	  "mode": { "default": "discover" },
	  "sources": {
	    "ticketsService": {
	      "type": "serviceRoot",
	      "uri": "https://managed.example.com/api",
	      "enabled": true
	    }
	  },
	  "curation": {
	    "toolSets": {
	      "sandbox-default": {
	        "allow": ["tickets:listTickets", "tickets:getTicket"],
	        "deny": ["tickets:deleteTicket"]
	      }
	    }
	  },
	  "policy": {
	    "deny": ["tickets:deleteTicket"]
	  }
	}`)
	userPath := writeJSON(t, dir, "user.json", `{
	  "sources": {
	    "ticketsService": { "enabled": false },
	    "billingService": {
	      "type": "openapi",
	      "uri": "file:///tmp/billing.openapi.json"
	    }
	  },
	  "policy": {
	    "deny": ["billing:deleteInvoice"]
	  }
	}`)
	projectPath := writeJSON(t, dir, "project.json", `{
	  "cli": "1.0.0",
	  "mode": { "default": "curated" },
	  "services": {
	    "tickets": {
	      "source": "ticketsService",
	      "alias": "tickets"
	    }
	  },
	  "agents": {
	    "profiles": {
	      "sandbox": {
	        "mode": "curated",
	        "toolSet": "sandbox-default"
	      }
	    },
	    "defaultProfile": "sandbox"
	  }
	}`)
	localPath := writeJSON(t, dir, "local.json", `{
	  "sources": {
	    "ticketsService": { "enabled": true }
	  },
	  "services": {
	    "tickets": {
	      "overlays": ["./tickets.overlay.yaml"]
	    }
	  }
	}`)

	effective, err := config.LoadEffective(config.LoadOptions{
		ManagedPath: managedPath,
		UserPath:    userPath,
		ProjectPath: projectPath,
		LocalPath:   localPath,
	})
	if err != nil {
		t.Fatalf("LoadEffective returned error: %v", err)
	}

	if effective.Config.Mode.Default != "curated" {
		t.Fatalf("expected curated default mode, got %q", effective.Config.Mode.Default)
	}
	if !effective.Config.Sources["ticketsService"].Enabled {
		t.Fatalf("expected ticketsService to be re-enabled by local scope")
	}
	if effective.Config.Sources["billingService"].URI != "file:///tmp/billing.openapi.json" {
		t.Fatalf("expected billing source to merge in from user scope")
	}
	if got := effective.Config.Policy.ManagedDeny; len(got) != 1 || got[0] != "tickets:deleteTicket" {
		t.Fatalf("expected managed deny to be preserved, got %#v", got)
	}
	if got := effective.Config.Policy.Deny; len(got) != 2 {
		t.Fatalf("expected 2 deny patterns after merge, got %#v", got)
	}
	if got := effective.Config.Services["tickets"].Overlays; len(got) != 1 || got[0] != "./tickets.overlay.yaml" {
		t.Fatalf("expected overlay override to merge, got %#v", got)
	}
	if effective.Config.Agents.DefaultProfile != "sandbox" {
		t.Fatalf("expected default profile sandbox, got %q", effective.Config.Agents.DefaultProfile)
	}
}

func TestLoadEffectiveReturnsFieldDiagnostics(t *testing.T) {
	dir := t.TempDir()
	projectPath := writeJSON(t, dir, "project.json", `{
	  "cli": "1.0.0",
	  "mode": { "default": "discover" },
	  "sources": {
	    "broken": {
	      "type": "openapi"
	    }
	  }
	}`)

	_, err := config.LoadEffective(config.LoadOptions{ProjectPath: projectPath})
	if err == nil {
		t.Fatalf("expected validation error")
	}

	validationErr, ok := err.(*config.ValidationError)
	if !ok {
		t.Fatalf("expected ValidationError, got %T", err)
	}
	if len(validationErr.Diagnostics) == 0 {
		t.Fatalf("expected diagnostics")
	}
	if validationErr.Diagnostics[0].Path != "sources.broken.uri" {
		t.Fatalf("expected sources.broken.uri diagnostic, got %#v", validationErr.Diagnostics)
	}
}

func TestDiscoverScopePaths(t *testing.T) {
	root := t.TempDir()
	managedDir := filepath.Join(root, "etc", "oas-cli")
	userConfigDir := filepath.Join(root, "xdg")
	projectDir := filepath.Join(root, "project")

	for _, dir := range []string{managedDir, filepath.Join(userConfigDir, "oas-cli"), projectDir} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatalf("mkdir %s: %v", dir, err)
		}
	}

	managedPath := writeJSON(t, managedDir, ".cli.json", `{"cli":"1.0.0","mode":{"default":"discover"},"sources":{"svc":{"type":"openapi","uri":"file:///managed.json"}}}`)
	userPath := writeJSON(t, filepath.Join(userConfigDir, "oas-cli"), ".cli.json", `{"sources":{"svc":{"enabled":false}}}`)
	projectPath := writeJSON(t, projectDir, ".cli.json", `{"services":{"svc":{"source":"svc"}}}`)
	localPath := writeJSON(t, projectDir, ".cli.local.json", `{"sources":{"svc":{"enabled":true}}}`)

	paths := config.DiscoverScopePaths(config.LoadOptions{
		ManagedDir:    managedDir,
		UserConfigDir: userConfigDir,
		WorkingDir:    projectDir,
	})

	if paths[config.ScopeManaged] != managedPath {
		t.Fatalf("expected managed path %q, got %q", managedPath, paths[config.ScopeManaged])
	}
	if paths[config.ScopeUser] != userPath {
		t.Fatalf("expected user path %q, got %q", userPath, paths[config.ScopeUser])
	}
	if paths[config.ScopeProject] != projectPath {
		t.Fatalf("expected project path %q, got %q", projectPath, paths[config.ScopeProject])
	}
	if paths[config.ScopeLocal] != localPath {
		t.Fatalf("expected local path %q, got %q", localPath, paths[config.ScopeLocal])
	}
}

func TestLoadEffectiveUsesSchemaValidation(t *testing.T) {
	dir := t.TempDir()
	projectPath := writeJSON(t, dir, ".cli.json", `{
	  "cli": "1.0.0",
	  "mode": { "default": "discover" },
	  "sources": {
	    "broken": {
	      "type": "not-a-valid-source-type",
	      "uri": "https://example.com/openapi.json"
	    }
	  }
	}`)

	_, err := config.LoadEffective(config.LoadOptions{ProjectPath: projectPath, WorkingDir: dir})
	if err == nil {
		t.Fatalf("expected schema validation error")
	}

	validationErr, ok := err.(*config.ValidationError)
	if !ok {
		t.Fatalf("expected ValidationError, got %T", err)
	}
	if len(validationErr.Diagnostics) == 0 {
		t.Fatalf("expected diagnostics")
	}
	if validationErr.Diagnostics[0].Path != "sources.broken.type" {
		t.Fatalf("expected schema diagnostic for sources.broken.type, got %#v", validationErr.Diagnostics)
	}
}

func TestLoadEffectiveRejectsNegativeRefreshMaxAge(t *testing.T) {
	dir := t.TempDir()
	projectPath := writeJSON(t, dir, ".cli.json", `{
	  "cli": "1.0.0",
	  "mode": { "default": "discover" },
	  "sources": {
	    "tickets": {
	      "type": "openapi",
	      "uri": "https://example.com/openapi.json",
	      "refresh": {
	        "maxAgeSeconds": -1
	      }
	    }
	  }
	}`)

	_, err := config.LoadEffective(config.LoadOptions{ProjectPath: projectPath, WorkingDir: dir})
	if err == nil {
		t.Fatalf("expected schema validation error")
	}

	validationErr, ok := err.(*config.ValidationError)
	if !ok {
		t.Fatalf("expected ValidationError, got %T", err)
	}
	if len(validationErr.Diagnostics) == 0 {
		t.Fatalf("expected diagnostics")
	}
	if validationErr.Diagnostics[0].Path != "sources.tickets.refresh.maxAgeSeconds" {
		t.Fatalf("expected refresh schema diagnostic, got %#v", validationErr.Diagnostics)
	}
}

func TestLoadEffectiveLoadsRuntimeLocalConfiguration(t *testing.T) {
	dir := t.TempDir()
	projectPath := writeJSON(t, dir, ".cli.json", `{
	  "cli": "1.0.0",
	  "mode": { "default": "discover" },
	  "runtime": {
	    "mode": "auto",
	    "local": {
	      "sessionScope": "shared-group",
	      "heartbeatSeconds": 15,
	      "missedHeartbeatLimit": 3,
	      "shutdown": "manual",
	      "share": "group",
	      "shareKey": "team-a"
	    }
	  },
	  "mcpServers": {
	    "filesystem": {
	      "type": "stdio",
	      "command": "npx"
	    }
	  }
	}`)

	effective, err := config.LoadEffective(config.LoadOptions{ProjectPath: projectPath, WorkingDir: dir})
	if err != nil {
		t.Fatalf("LoadEffective returned error: %v", err)
	}

	if effective.Config.Runtime == nil {
		t.Fatalf("expected runtime configuration to be loaded")
	}
	if effective.Config.Runtime.Mode != "auto" {
		t.Fatalf("expected runtime mode auto, got %q", effective.Config.Runtime.Mode)
	}
	if effective.Config.Runtime.Local == nil {
		t.Fatalf("expected local runtime configuration")
	}
	if effective.Config.Runtime.Local.SessionScope != "shared-group" {
		t.Fatalf("expected sessionScope shared-group, got %q", effective.Config.Runtime.Local.SessionScope)
	}
	if effective.Config.Runtime.Local.Share != "group" {
		t.Fatalf("expected share group, got %q", effective.Config.Runtime.Local.Share)
	}
	if effective.Config.Runtime.Local.ShareKey != "team-a" {
		t.Fatalf("expected shareKey team-a, got %q", effective.Config.Runtime.Local.ShareKey)
	}
}

func TestLoadEffectiveRejectsLocalRuntimeWithZeroHeartbeatSeconds(t *testing.T) {
	dir := t.TempDir()
	projectPath := writeJSON(t, dir, ".cli.json", `{
	  "cli": "1.0.0",
	  "mode": { "default": "discover" },
	  "runtime": {
	    "mode": "local",
	    "local": {
	      "sessionScope": "terminal",
	      "heartbeatSeconds": 0,
	      "missedHeartbeatLimit": 3,
	      "shutdown": "when-owner-exits",
	      "share": "exclusive"
	    }
	  },
	  "mcpServers": {
	    "filesystem": {
	      "type": "stdio",
	      "command": "npx"
	    }
	  }
	}`)

	_, err := config.LoadEffective(config.LoadOptions{ProjectPath: projectPath, WorkingDir: dir})
	requireValidationDiagnostic(t, err, "runtime.local.heartbeatSeconds", "positive integer")
}

func TestLoadEffectiveRejectsLocalRuntimeWithZeroMissedHeartbeatLimit(t *testing.T) {
	dir := t.TempDir()
	projectPath := writeJSON(t, dir, ".cli.json", `{
	  "cli": "1.0.0",
	  "mode": { "default": "discover" },
	  "runtime": {
	    "mode": "local",
	    "local": {
	      "sessionScope": "terminal",
	      "heartbeatSeconds": 15,
	      "missedHeartbeatLimit": 0,
	      "shutdown": "when-owner-exits",
	      "share": "exclusive"
	    }
	  },
	  "mcpServers": {
	    "filesystem": {
	      "type": "stdio",
	      "command": "npx"
	    }
	  }
	}`)

	_, err := config.LoadEffective(config.LoadOptions{ProjectPath: projectPath, WorkingDir: dir})
	requireValidationDiagnostic(t, err, "runtime.local.missedHeartbeatLimit", "positive integer")
}

func TestLoadEffectiveMergesHeartbeatFieldsAcrossScopes(t *testing.T) {
	dir := t.TempDir()
	projectPath := writeJSON(t, dir, ".cli.json", `{
	  "cli": "1.0.0",
	  "mode": { "default": "discover" },
	  "runtime": {
	    "mode": "local",
	    "local": {
	      "sessionScope": "shared-group",
	      "heartbeatSeconds": 15,
	      "missedHeartbeatLimit": 3,
	      "shutdown": "manual",
	      "share": "group",
	      "shareKey": "team-a"
	    }
	  },
	  "mcpServers": {
	    "filesystem": {
	      "type": "stdio",
	      "command": "npx"
	    }
	  }
	}`)
	localPath := writeJSON(t, dir, ".cli.local.json", `{
	  "runtime": {
	    "local": {
	      "shareKey": "team-b"
	    }
	  }
	}`)

	effective, err := config.LoadEffective(config.LoadOptions{
		ProjectPath: projectPath,
		LocalPath:   localPath,
		WorkingDir:  dir,
	})
	if err != nil {
		t.Fatalf("LoadEffective returned error: %v", err)
	}
	if effective.Config.Runtime == nil || effective.Config.Runtime.Local == nil {
		t.Fatalf("expected local runtime configuration")
	}
	if effective.Config.Runtime.Local.HeartbeatSeconds != 15 {
		t.Fatalf("expected heartbeatSeconds 15 after merge, got %d", effective.Config.Runtime.Local.HeartbeatSeconds)
	}
	if effective.Config.Runtime.Local.MissedHeartbeatLimit != 3 {
		t.Fatalf("expected missedHeartbeatLimit 3 after merge, got %d", effective.Config.Runtime.Local.MissedHeartbeatLimit)
	}
	if effective.Config.Runtime.Local.ShareKey != "team-b" {
		t.Fatalf("expected local shareKey override team-b, got %q", effective.Config.Runtime.Local.ShareKey)
	}
}

func TestLoadEffectiveRejectsManualShutdownForExclusiveSessionScopes(t *testing.T) {
	dir := t.TempDir()
	projectPath := writeJSON(t, dir, ".cli.json", `{
	  "cli": "1.0.0",
	  "mode": { "default": "discover" },
	  "runtime": {
	    "mode": "local",
	    "local": {
	      "sessionScope": "terminal",
	      "shutdown": "manual",
	      "share": "exclusive"
	    }
	  },
	  "mcpServers": {
	    "filesystem": {
	      "type": "stdio",
	      "command": "npx"
	    }
	  }
	}`)

	_, err := config.LoadEffective(config.LoadOptions{ProjectPath: projectPath, WorkingDir: dir})
	requireValidationDiagnostic(t, err, "runtime.local.shutdown", "shared-group")
}

func TestLoadEffectiveRejectsRemoteRuntimeWithoutURL(t *testing.T) {
	dir := t.TempDir()
	projectPath := writeJSON(t, dir, ".cli.json", `{
	  "cli": "1.0.0",
	  "mode": { "default": "discover" },
	  "runtime": {
	    "mode": "remote",
	    "remote": {
	      "oauth": {
	        "mode": "providedToken",
	        "tokenRef": "env:OAS_REMOTE_TOKEN"
	      }
	    }
	  },
	  "sources": {
	    "tickets": {
	      "type": "openapi",
	      "uri": "https://example.com/openapi.json"
	    }
	  },
	  "services": {
	    "tickets": {
	      "source": "tickets"
	    }
	  }
	}`)

	_, err := config.LoadEffective(config.LoadOptions{ProjectPath: projectPath, WorkingDir: dir})
	requireValidationDiagnostic(t, err, "runtime.remote.url", "required")
}

func TestLoadEffectiveRejectsOAuthClientRemoteRuntimeWithoutClientSecret(t *testing.T) {
	dir := t.TempDir()
	projectPath := writeJSON(t, dir, ".cli.json", `{
	  "cli": "1.0.0",
	  "mode": { "default": "discover" },
	  "runtime": {
	    "mode": "remote",
	    "remote": {
	      "url": "https://runtime.example.com",
	      "oauth": {
	        "mode": "oauthClient",
	        "client": {
	          "tokenURL": "https://auth.example.com/token",
	          "clientId": { "type": "env", "value": "OAS_REMOTE_CLIENT_ID" }
	        }
	      }
	    }
	  },
	  "sources": {
	    "tickets": {
	      "type": "openapi",
	      "uri": "https://example.com/openapi.json"
	    }
	  },
	  "services": {
	    "tickets": {
	      "source": "tickets"
	    }
	  }
	}`)

	_, err := config.LoadEffective(config.LoadOptions{ProjectPath: projectPath, WorkingDir: dir})
	requireValidationDiagnostic(t, err, "runtime.remote.oauth.client.clientSecret", "required")
}

func TestLoadEffectiveRejectsSharedGroupLocalRuntimeWithoutShareKey(t *testing.T) {
	dir := t.TempDir()
	projectPath := writeJSON(t, dir, ".cli.json", `{
	  "cli": "1.0.0",
	  "mode": { "default": "discover" },
	  "runtime": {
	    "mode": "local",
	    "local": {
	      "sessionScope": "shared-group",
	      "share": "group",
	      "shutdown": "manual"
	    }
	  },
	  "mcpServers": {
	    "filesystem": {
	      "type": "stdio",
	      "command": "npx"
	    }
	  }
	}`)

	_, err := config.LoadEffective(config.LoadOptions{ProjectPath: projectPath, WorkingDir: dir})
	requireValidationDiagnostic(t, err, "runtime.local.shareKey", "required")
}

func TestLoadEffectiveRejectsTerminalLocalRuntimeWithGroupSharing(t *testing.T) {
	dir := t.TempDir()
	projectPath := writeJSON(t, dir, ".cli.json", `{
	  "cli": "1.0.0",
	  "mode": { "default": "discover" },
	  "runtime": {
	    "mode": "local",
	    "local": {
	      "sessionScope": "terminal",
	      "share": "group",
	      "shareKey": "team-a",
	      "shutdown": "when-owner-exits"
	    }
	  },
	  "mcpServers": {
	    "filesystem": {
	      "type": "stdio",
	      "command": "npx"
	    }
	  }
	}`)

	_, err := config.LoadEffective(config.LoadOptions{ProjectPath: projectPath, WorkingDir: dir})
	requireValidationDiagnostic(t, err, "runtime.local.share", "exclusive")
}

func TestLoadEffectiveLoadsRemoteRuntimeOAuthConfiguration(t *testing.T) {
	dir := t.TempDir()
	projectPath := writeJSON(t, dir, ".cli.json", `{
	  "cli": "1.0.0",
	  "mode": { "default": "discover" },
	  "runtime": {
	    "mode": "remote",
	    "remote": {
	      "url": "https://runtime.example.com",
	      "oauth": {
	        "mode": "browserLogin",
	        "audience": "oclird",
	        "scopes": ["bundle:payments", "tool:users.get"],
	        "browserLogin": {
	          "callbackPort": 8123
	        }
	      }
	    }
	  },
	  "sources": {
	    "tickets": {
	      "type": "openapi",
	      "uri": "https://example.com/openapi.json"
	    }
	  },
	  "services": {
	    "tickets": {
	      "source": "tickets"
	    }
	  }
	}`)

	effective, err := config.LoadEffective(config.LoadOptions{ProjectPath: projectPath, WorkingDir: dir})
	if err != nil {
		t.Fatalf("LoadEffective returned error: %v", err)
	}

	if effective.Config.Runtime == nil || effective.Config.Runtime.Remote == nil {
		t.Fatalf("expected remote runtime configuration to be loaded")
	}
	if effective.Config.Runtime.Remote.URL != "https://runtime.example.com" {
		t.Fatalf("expected remote url to load, got %q", effective.Config.Runtime.Remote.URL)
	}
	if effective.Config.Runtime.Remote.OAuth == nil {
		t.Fatalf("expected remote oauth config to load")
	}
	if effective.Config.Runtime.Remote.OAuth.Mode != "browserLogin" {
		t.Fatalf("expected remote oauth mode browserLogin, got %q", effective.Config.Runtime.Remote.OAuth.Mode)
	}
	if effective.Config.Runtime.Remote.OAuth.BrowserLogin == nil || effective.Config.Runtime.Remote.OAuth.BrowserLogin.CallbackPort != 8123 {
		t.Fatalf("expected browser login callback port 8123, got %#v", effective.Config.Runtime.Remote.OAuth.BrowserLogin)
	}
}

func TestLoadEffectiveLoadsRemoteRuntimeOIDCJWKSConfiguration(t *testing.T) {
	dir := t.TempDir()
	projectPath := writeJSON(t, dir, ".cli.json", `{
	  "cli": "1.0.0",
	  "mode": { "default": "discover" },
	  "runtime": {
	    "server": {
	      "auth": {
	        "validationProfile": "oidc_jwks",
	        "audience": "oclird",
	        "issuer": "https://broker.example.com",
	        "jwksURL": "https://broker.example.com/.well-known/jwks.json"
	      }
	    }
	  },
	  "sources": {
	    "tickets": {
	      "type": "openapi",
	      "uri": "https://example.com/openapi.json"
	    }
	  },
	  "services": {
	    "tickets": {
	      "source": "tickets"
	    }
	  }
	}`)

	effective, err := config.LoadEffective(config.LoadOptions{ProjectPath: projectPath, WorkingDir: dir})
	if err != nil {
		t.Fatalf("LoadEffective returned error: %v", err)
	}

	if effective.Config.Runtime == nil || effective.Config.Runtime.Server == nil || effective.Config.Runtime.Server.Auth == nil {
		t.Fatalf("expected runtime server auth configuration to load")
	}

	auth := *effective.Config.Runtime.Server.Auth
	requireStringField(t, auth, "ValidationProfile", "oidc_jwks")
	requireStringField(t, auth, "Audience", "oclird")
	requireStringField(t, auth, "Issuer", "https://broker.example.com")
	requireStringField(t, auth, "JWKSURL", "https://broker.example.com/.well-known/jwks.json")
}

func TestLoadEffectiveRejectsRemoteRuntimeOIDCJWKSConfigurationWithoutAudience(t *testing.T) {
	dir := t.TempDir()
	projectPath := writeJSON(t, dir, ".cli.json", `{
	  "cli": "1.0.0",
	  "mode": { "default": "discover" },
	  "runtime": {
	    "server": {
	      "auth": {
	        "validationProfile": "oidc_jwks",
	        "issuer": "https://broker.example.com",
	        "jwksURL": "https://broker.example.com/.well-known/jwks.json"
	      }
	    }
	  },
	  "sources": {
	    "tickets": {
	      "type": "openapi",
	      "uri": "https://example.com/openapi.json"
	    }
	  },
	  "services": {
	    "tickets": {
	      "source": "tickets"
	    }
	  }
	}`)

	_, err := config.LoadEffective(config.LoadOptions{ProjectPath: projectPath, WorkingDir: dir})
	requireValidationDiagnostic(t, err, "runtime.server.auth.audience", "required when runtime.server.auth.validationProfile is oidc_jwks")
}

func TestLoadEffectiveHigherPrecedenceLegacyRuntimeAuthModeOverridesLowerValidationProfile(t *testing.T) {
	dir := t.TempDir()
	userPath := writeJSON(t, dir, "user.json", `{
	  "runtime": {
	    "server": {
	      "auth": {
	        "validationProfile": "oidc_jwks"
	      }
	    }
	  }
	}`)
	projectPath := writeJSON(t, dir, ".cli.json", `{
	  "cli": "1.0.0",
	  "mode": { "default": "discover" },
	  "runtime": {
	    "server": {
	      "auth": {
	        "mode": "oauth2Introspection",
	        "audience": "oclird",
	        "introspectionURL": "https://auth.example.com/oauth/introspect"
	      }
	    }
	  },
	  "sources": {
	    "tickets": {
	      "type": "openapi",
	      "uri": "https://example.com/openapi.json"
	    }
	  },
	  "services": {
	    "tickets": {
	      "source": "tickets"
	    }
	  }
	}`)

	effective, err := config.LoadEffective(config.LoadOptions{
		UserPath:    userPath,
		ProjectPath: projectPath,
		WorkingDir:  dir,
	})
	if err != nil {
		t.Fatalf("LoadEffective returned error: %v", err)
	}

	if effective.Config.Runtime == nil || effective.Config.Runtime.Server == nil || effective.Config.Runtime.Server.Auth == nil {
		t.Fatalf("expected runtime server auth configuration to load")
	}

	auth := *effective.Config.Runtime.Server.Auth
	if auth.Mode != "oauth2Introspection" {
		t.Fatalf("expected legacy auth mode oauth2Introspection, got %q", auth.Mode)
	}
	requireStringField(t, auth, "ValidationProfile", "oauth2_introspection")
	if auth.Audience != "oclird" {
		t.Fatalf("expected audience oclird, got %q", auth.Audience)
	}
	if auth.IntrospectionURL != "https://auth.example.com/oauth/introspect" {
		t.Fatalf("expected introspection URL to be preserved, got %q", auth.IntrospectionURL)
	}
}

func TestLoadEffectiveHigherPrecedenceValidationProfileDoesNotOverrideLegacyRuntimeAuthMode(t *testing.T) {
	dir := t.TempDir()
	userPath := writeJSON(t, dir, "user.json", `{
	  "runtime": {
	    "server": {
	      "auth": {
	        "mode": "oauth2Introspection",
	        "audience": "oclird",
	        "introspectionURL": "https://auth.example.com/oauth/introspect"
	      }
	    }
	  }
	}`)
	projectPath := writeJSON(t, dir, ".cli.json", `{
	  "cli": "1.0.0",
	  "mode": { "default": "discover" },
	  "runtime": {
	    "server": {
	      "auth": {
	        "validationProfile": "oidc_jwks"
	      }
	    }
	  },
	  "sources": {
	    "tickets": {
	      "type": "openapi",
	      "uri": "https://example.com/openapi.json"
	    }
	  },
	  "services": {
	    "tickets": {
	      "source": "tickets"
	    }
	  }
	}`)

	effective, err := config.LoadEffective(config.LoadOptions{
		UserPath:    userPath,
		ProjectPath: projectPath,
		WorkingDir:  dir,
	})
	if err != nil {
		t.Fatalf("LoadEffective returned error: %v", err)
	}

	if effective.Config.Runtime == nil || effective.Config.Runtime.Server == nil || effective.Config.Runtime.Server.Auth == nil {
		t.Fatalf("expected runtime server auth configuration to load")
	}

	auth := *effective.Config.Runtime.Server.Auth
	if auth.Mode != "oauth2Introspection" {
		t.Fatalf("expected legacy auth mode oauth2Introspection, got %q", auth.Mode)
	}
	requireStringField(t, auth, "ValidationProfile", "oauth2_introspection")
	if auth.Audience != "oclird" {
		t.Fatalf("expected audience oclird, got %q", auth.Audience)
	}
	if auth.IntrospectionURL != "https://auth.example.com/oauth/introspect" {
		t.Fatalf("expected introspection URL to be preserved, got %q", auth.IntrospectionURL)
	}
}

func TestLoadEffectivePreservesRemoteRuntimeOAuth2IntrospectionConfiguration(t *testing.T) {
	dir := t.TempDir()
	projectPath := writeJSON(t, dir, ".cli.json", `{
	  "cli": "1.0.0",
	  "mode": { "default": "discover" },
	  "runtime": {
	    "server": {
	      "auth": {
	        "mode": "oauth2Introspection",
	        "audience": "oclird",
	        "introspectionURL": "https://auth.example.com/oauth/introspect",
	        "authorizationURL": "https://auth.example.com/oauth/authorize",
	        "tokenURL": "https://auth.example.com/oauth/token",
	        "browserClientId": "runtime-browser-client",
	        "clientId": { "type": "env", "value": "OAS_RUNTIME_CLIENT_ID" },
	        "clientSecret": { "type": "env", "value": "OAS_RUNTIME_CLIENT_SECRET" }
	      }
	    }
	  },
	  "sources": {
	    "tickets": {
	      "type": "openapi",
	      "uri": "https://example.com/openapi.json"
	    }
	  },
	  "services": {
	    "tickets": {
	      "source": "tickets"
	    }
	  }
	}`)

	effective, err := config.LoadEffective(config.LoadOptions{ProjectPath: projectPath, WorkingDir: dir})
	if err != nil {
		t.Fatalf("LoadEffective returned error: %v", err)
	}

	if effective.Config.Runtime == nil || effective.Config.Runtime.Server == nil || effective.Config.Runtime.Server.Auth == nil {
		t.Fatalf("expected runtime server auth configuration to load")
	}

	auth := *effective.Config.Runtime.Server.Auth
	if auth.Mode != "oauth2Introspection" {
		t.Fatalf("expected legacy auth mode oauth2Introspection, got %q", auth.Mode)
	}
	requireStringField(t, auth, "ValidationProfile", "oauth2_introspection")
	if auth.Audience != "oclird" {
		t.Fatalf("expected audience oclird, got %q", auth.Audience)
	}
	if auth.IntrospectionURL != "https://auth.example.com/oauth/introspect" {
		t.Fatalf("expected introspection URL to be preserved, got %q", auth.IntrospectionURL)
	}
	if auth.AuthorizationURL != "https://auth.example.com/oauth/authorize" {
		t.Fatalf("expected authorization URL to be preserved, got %q", auth.AuthorizationURL)
	}
	if auth.TokenURL != "https://auth.example.com/oauth/token" {
		t.Fatalf("expected token URL to be preserved, got %q", auth.TokenURL)
	}
	if auth.BrowserClientID != "runtime-browser-client" {
		t.Fatalf("expected browser client ID to be preserved, got %q", auth.BrowserClientID)
	}
	if auth.ClientID == nil || auth.ClientID.Type != "env" || auth.ClientID.Value != "OAS_RUNTIME_CLIENT_ID" {
		t.Fatalf("expected clientId to be preserved, got %#v", auth.ClientID)
	}
	if auth.ClientSecret == nil || auth.ClientSecret.Type != "env" || auth.ClientSecret.Value != "OAS_RUNTIME_CLIENT_SECRET" {
		t.Fatalf("expected clientSecret to be preserved, got %#v", auth.ClientSecret)
	}
}
