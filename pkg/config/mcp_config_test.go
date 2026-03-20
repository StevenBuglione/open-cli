package config_test

import (
	"strings"
	"testing"

	"github.com/StevenBuglione/open-cli/pkg/config"
)

func requireValidationDiagnostic(t *testing.T, err error, path, contains string) {
	t.Helper()
	if err == nil {
		t.Fatalf("expected validation error")
	}

	validationErr, ok := err.(*config.ValidationError)
	if !ok {
		t.Fatalf("expected ValidationError, got %T", err)
	}

	for _, diagnostic := range validationErr.Diagnostics {
		if diagnostic.Path != path {
			continue
		}
		if contains != "" && !strings.Contains(diagnostic.Message, contains) {
			continue
		}
		return
	}

	t.Fatalf("expected diagnostic %q containing %q, got %#v", path, contains, validationErr.Diagnostics)
}

func TestLoadEffectiveSupportsCanonicalMCPSources(t *testing.T) {
	dir := t.TempDir()
	projectPath := writeJSON(t, dir, ".cli.json", `{
	  "cli": "1.0.0",
	  "mode": { "default": "discover" },
	  "runtime": {
	    "mode": "local"
	  },
	  "sources": {
	    "remoteDocs": {
	      "type": "mcp",
	      "transport": {
	        "type": "streamable-http",
	        "url": "https://mcp.example.com/mcp",
	        "headers": {
	          "X-Tenant": "docs"
	        },
	        "headerSecrets": {
	          "X-API-Key": "remote_docs_api_key"
	        }
	      },
	      "disabledTools": ["admin.delete"],
	      "oauth": {
	        "mode": "clientCredentials",
	        "issuer": "https://auth.example.com",
	        "clientId": {
	          "type": "env",
	          "value": "REMOTE_MCP_CLIENT_ID"
	        },
	        "clientSecret": {
	          "type": "env",
	          "value": "REMOTE_MCP_CLIENT_SECRET"
	        },
	        "scopes": ["mcp.read"],
	        "audience": "mcp.example.com",
	        "tokenStorage": "instance"
	      }
	    }
	  },
	  "services": {
	    "remoteDocs": {
	      "source": "remoteDocs",
	      "alias": "Remote Docs"
	    }
	  }
	}`)

	effective, err := config.LoadEffective(config.LoadOptions{ProjectPath: projectPath, WorkingDir: dir})
	if err != nil {
		t.Fatalf("LoadEffective returned error: %v", err)
	}

	source := effective.Config.Sources["remoteDocs"]
	if source.Type != "mcp" {
		t.Fatalf("expected mcp source type, got %q", source.Type)
	}
	if source.Transport == nil {
		t.Fatalf("expected transport to be populated")
	}
	if source.Transport.Type != "streamable-http" {
		t.Fatalf("expected streamable-http transport, got %q", source.Transport.Type)
	}
	if source.Transport.URL != "https://mcp.example.com/mcp" {
		t.Fatalf("expected streamable-http url to round-trip, got %q", source.Transport.URL)
	}
	if got := source.Transport.Headers["X-Tenant"]; got != "docs" {
		t.Fatalf("expected headers to round-trip, got %#v", source.Transport.Headers)
	}
	if got := source.Transport.HeaderSecrets["X-API-Key"]; got != "remote_docs_api_key" {
		t.Fatalf("expected headerSecrets to round-trip, got %#v", source.Transport.HeaderSecrets)
	}
	if got := source.DisabledTools; len(got) != 1 || got[0] != "admin.delete" {
		t.Fatalf("expected disabledTools to round-trip, got %#v", got)
	}
	if source.OAuth == nil {
		t.Fatalf("expected oauth config to be populated")
	}
	if source.OAuth.Mode != "clientCredentials" {
		t.Fatalf("expected oauth mode clientCredentials, got %q", source.OAuth.Mode)
	}
	if source.OAuth.ClientID == nil || source.OAuth.ClientID.Type != "env" || source.OAuth.ClientID.Value != "REMOTE_MCP_CLIENT_ID" {
		t.Fatalf("expected oauth clientId to round-trip, got %#v", source.OAuth.ClientID)
	}
	if source.OAuth.ClientSecret == nil || source.OAuth.ClientSecret.Type != "env" || source.OAuth.ClientSecret.Value != "REMOTE_MCP_CLIENT_SECRET" {
		t.Fatalf("expected oauth clientSecret to round-trip, got %#v", source.OAuth.ClientSecret)
	}
	if effective.Config.Services["remoteDocs"].Source != "remoteDocs" {
		t.Fatalf("expected service source remoteDocs, got %q", effective.Config.Services["remoteDocs"].Source)
	}
}

func TestLoadEffectiveAutoCreatesServiceForCanonicalMCPSource(t *testing.T) {
	dir := t.TempDir()
	projectPath := writeJSON(t, dir, ".cli.json", `{
	  "cli": "1.0.0",
	  "mode": { "default": "discover" },
	  "runtime": {
	    "mode": "local"
	  },
	  "sources": {
	    "filesystem": {
	      "type": "mcp",
	      "transport": {
	        "type": "stdio",
	        "command": "npx"
	      }
	    }
	  }
	}`)

	effective, err := config.LoadEffective(config.LoadOptions{ProjectPath: projectPath, WorkingDir: dir})
	if err != nil {
		t.Fatalf("LoadEffective returned error: %v", err)
	}

	service, ok := effective.Config.Services["filesystem"]
	if !ok {
		t.Fatalf("expected normalization to auto-create services.filesystem")
	}
	if service.Source != "filesystem" {
		t.Fatalf("expected auto-created service source filesystem, got %q", service.Source)
	}
}

func TestLoadEffectiveInjectsSourceForCanonicalMCPService(t *testing.T) {
	dir := t.TempDir()
	projectPath := writeJSON(t, dir, ".cli.json", `{
	  "cli": "1.0.0",
	  "mode": { "default": "discover" },
	  "runtime": {
	    "mode": "local"
	  },
	  "sources": {
	    "filesystem": {
	      "type": "mcp",
	      "transport": {
	        "type": "stdio",
	        "command": "npx"
	      }
	    }
	  },
	  "services": {
	    "filesystem": {
	      "alias": "Local Filesystem"
	    }
	  }
	}`)

	effective, err := config.LoadEffective(config.LoadOptions{ProjectPath: projectPath, WorkingDir: dir})
	if err != nil {
		t.Fatalf("LoadEffective returned error: %v", err)
	}

	if effective.Config.Services["filesystem"].Source != "filesystem" {
		t.Fatalf("expected normalization to inject source into services.filesystem, got %q", effective.Config.Services["filesystem"].Source)
	}
}

func TestLoadEffectiveNormalizesMCPServersAndInjectsServiceSource(t *testing.T) {
	dir := t.TempDir()
	projectPath := writeJSON(t, dir, ".cli.json", `{
	  "cli": "1.0.0",
	  "mode": { "default": "discover" },
	  "runtime": {
	    "mode": "local"
	  },
	  "mcpServers": {
	    "filesystem": {
	      "type": "stdio",
	      "command": "npx",
	      "args": ["-y", "@modelcontextprotocol/server-filesystem", "/workspace"],
	      "disabledTools": ["delete_file"]
	    }
	  },
	  "services": {
	    "filesystem": {
	      "alias": "Local Filesystem",
	      "workflows": ["./workflows/files.yaml"]
	    }
	  }
	}`)

	effective, err := config.LoadEffective(config.LoadOptions{ProjectPath: projectPath, WorkingDir: dir})
	if err != nil {
		t.Fatalf("LoadEffective returned error: %v", err)
	}

	source := effective.Config.Sources["filesystem"]
	if source.Type != "mcp" {
		t.Fatalf("expected mcp source type, got %q", source.Type)
	}
	if source.Transport == nil || source.Transport.Type != "stdio" {
		t.Fatalf("expected stdio transport, got %#v", source.Transport)
	}
	if source.Transport.Command != "npx" {
		t.Fatalf("expected command npx, got %q", source.Transport.Command)
	}
	if got := source.Transport.Args; len(got) != 3 || got[2] != "/workspace" {
		t.Fatalf("expected args to normalize from mcpServers, got %#v", got)
	}
	if got := source.DisabledTools; len(got) != 1 || got[0] != "delete_file" {
		t.Fatalf("expected disabledTools to normalize from mcpServers, got %#v", got)
	}

	service := effective.Config.Services["filesystem"]
	if service.Source != "filesystem" {
		t.Fatalf("expected normalized service source filesystem, got %q", service.Source)
	}
	if service.Alias != "Local Filesystem" {
		t.Fatalf("expected alias to be preserved, got %q", service.Alias)
	}
	if got := service.Workflows; len(got) != 1 || got[0] != "./workflows/files.yaml" {
		t.Fatalf("expected workflows to be preserved, got %#v", got)
	}
}

func TestLoadEffectiveAutoCreatesServiceForMCPServer(t *testing.T) {
	dir := t.TempDir()
	projectPath := writeJSON(t, dir, ".cli.json", `{
	  "cli": "1.0.0",
	  "mode": { "default": "discover" },
	  "runtime": {
	    "mode": "local"
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

	service, ok := effective.Config.Services["filesystem"]
	if !ok {
		t.Fatalf("expected normalization to auto-create services.filesystem")
	}
	if service.Source != "filesystem" {
		t.Fatalf("expected auto-created service source filesystem, got %q", service.Source)
	}
}

func TestLoadEffectiveRejectsAmbiguousMCPSourceNames(t *testing.T) {
	dir := t.TempDir()
	managedPath := writeJSON(t, dir, "managed.json", `{
	  "cli": "1.0.0",
	  "mode": { "default": "discover" },
	  "sources": {
	    "filesystem": {
	      "type": "openapi",
	      "uri": "https://managed.example.com/openapi.json"
	    }
	  }
	}`)
	projectPath := writeJSON(t, dir, ".cli.json", `{
	  "mcpServers": {
	    "filesystem": {
	      "type": "stdio",
	      "command": "npx"
	    }
	  }
	}`)

	_, err := config.LoadEffective(config.LoadOptions{
		ManagedPath: managedPath,
		ProjectPath: projectPath,
		WorkingDir:  dir,
	})
	requireValidationDiagnostic(t, err, "mcpServers.filesystem", "ambiguous")
}

func TestLoadEffectiveRejectsMCPServiceSourceConflicts(t *testing.T) {
	dir := t.TempDir()
	projectPath := writeJSON(t, dir, ".cli.json", `{
	  "cli": "1.0.0",
	  "mode": { "default": "discover" },
	  "runtime": {
	    "mode": "local"
	  },
	  "services": {
	    "filesystem": {
	      "source": "other",
	      "alias": "Local Filesystem"
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
	requireValidationDiagnostic(t, err, "services.filesystem.source", `must reference "filesystem"`)
}

func TestLoadEffectiveRejectsCanonicalMCPServiceSourceConflicts(t *testing.T) {
	dir := t.TempDir()
	projectPath := writeJSON(t, dir, ".cli.json", `{
	  "cli": "1.0.0",
	  "mode": { "default": "discover" },
	  "runtime": {
	    "mode": "local"
	  },
	  "sources": {
	    "filesystem": {
	      "type": "mcp",
	      "transport": {
	        "type": "stdio",
	        "command": "npx"
	      }
	    },
	    "other": {
	      "type": "openapi",
	      "uri": "https://example.com/openapi.json"
	    }
	  },
	  "services": {
	    "filesystem": {
	      "source": "other"
	    }
	  }
	}`)

	_, err := config.LoadEffective(config.LoadOptions{ProjectPath: projectPath, WorkingDir: dir})
	requireValidationDiagnostic(t, err, "services.filesystem.source", `must reference "filesystem"`)
}

func TestLoadEffectiveRejectsInvalidMCPTransportConfig(t *testing.T) {
	testCases := []struct {
		name     string
		document string
		path     string
		message  string
	}{
		{
			name: "stdio requires command",
			document: `{
			  "cli": "1.0.0",
			  "mode": { "default": "discover" },
			  "sources": {
			    "filesystem": {
			      "type": "mcp",
			      "transport": {
			        "type": "stdio"
			      }
			    }
			  }
			}`,
			path:    "sources.filesystem.transport.command",
			message: "required for stdio transport",
		},
		{
			name: "stdio forbids url",
			document: `{
			  "cli": "1.0.0",
			  "mode": { "default": "discover" },
			  "sources": {
			    "filesystem": {
			      "type": "mcp",
			      "transport": {
			        "type": "stdio",
			        "command": "npx",
			        "url": "https://mcp.example.com"
			      }
			    }
			  }
			}`,
			path:    "sources.filesystem.transport.url",
			message: "not allowed for stdio transport",
		},
		{
			name: "sse requires url",
			document: `{
			  "cli": "1.0.0",
			  "mode": { "default": "discover" },
			  "runtime": {
			    "mode": "local"
			  },
			  "sources": {
			    "remoteDocs": {
			      "type": "mcp",
			      "transport": {
			        "type": "sse"
			      }
			    }
			  }
			}`,
			path:    "sources.remoteDocs.transport.url",
			message: "required for sse transport",
		},
		{
			name: "sse forbids oauth after mcpServers normalization",
			document: `{
			  "cli": "1.0.0",
			  "mode": { "default": "discover" },
			  "mcpServers": {
			    "remoteDocs": {
			      "type": "sse",
			      "url": "https://mcp.example.com/sse",
			      "oauth": {
			        "mode": "clientCredentials",
			        "issuer": "https://auth.example.com",
			        "clientId": {
			          "type": "env",
			          "value": "REMOTE_MCP_CLIENT_ID"
			        },
			        "clientSecret": {
			          "type": "env",
			          "value": "REMOTE_MCP_CLIENT_SECRET"
			        }
			      }
			    }
			  }
			}`,
			path:    "sources.remoteDocs.oauth",
			message: "not allowed for sse transport",
		},
		{
			name: "streamable-http forbids command",
			document: `{
			  "cli": "1.0.0",
			  "mode": { "default": "discover" },
			  "sources": {
			    "remoteDocs": {
			      "type": "mcp",
			      "transport": {
			        "type": "streamable-http",
			        "url": "https://mcp.example.com/mcp",
			        "command": "npx"
			      }
			    }
			  }
			}`,
			path:    "sources.remoteDocs.transport.command",
			message: "not allowed for streamable-http transport",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			projectPath := writeJSON(t, dir, ".cli.json", tc.document)

			_, err := config.LoadEffective(config.LoadOptions{ProjectPath: projectPath, WorkingDir: dir})
			requireValidationDiagnostic(t, err, tc.path, tc.message)
		})
	}
}

func TestLoadEffectiveRejectsInvalidMCPSourceRelationships(t *testing.T) {
	testCases := []struct {
		name     string
		document string
		path     string
		message  string
	}{
		{
			name: "non-mcp source forbids transport",
			document: `{
			  "cli": "1.0.0",
			  "mode": { "default": "discover" },
			  "sources": {
			    "tickets": {
			      "type": "openapi",
			      "uri": "https://example.com/openapi.json",
			      "transport": {
			        "type": "stdio",
			        "command": "npx"
			      }
			    }
			  }
			}`,
			path:    "sources.tickets.transport",
			message: "only allowed for mcp sources",
		},
		{
			name: "mcp source forbids uri",
			document: `{
			  "cli": "1.0.0",
			  "mode": { "default": "discover" },
			  "sources": {
			    "filesystem": {
			      "type": "mcp",
			      "uri": "https://example.com/not-valid",
			      "transport": {
			        "type": "stdio",
			        "command": "npx"
			      }
			    }
			  }
			}`,
			path:    "sources.filesystem.uri",
			message: "is not allowed for mcp sources",
		},
		{
			name: "oauth owns authorization header",
			document: `{
			  "cli": "1.0.0",
			  "mode": { "default": "discover" },
			  "sources": {
			    "remoteDocs": {
			      "type": "mcp",
			      "transport": {
			        "type": "streamable-http",
			        "url": "https://mcp.example.com/mcp",
			        "headers": {
			          "Authorization": "Bearer literal"
			        }
			      },
			      "oauth": {
			        "mode": "clientCredentials",
			        "issuer": "https://auth.example.com",
			        "clientId": {
			          "type": "env",
			          "value": "REMOTE_MCP_CLIENT_ID"
			        },
			        "clientSecret": {
			          "type": "env",
			          "value": "REMOTE_MCP_CLIENT_SECRET"
			        }
			      }
			    }
			  }
			}`,
			path:    "sources.remoteDocs.transport.headers.Authorization",
			message: "owned by oauth",
		},
		{
			name: "header and headerSecrets cannot overlap",
			document: `{
			  "cli": "1.0.0",
			  "mode": { "default": "discover" },
			  "sources": {
			    "remoteDocs": {
			      "type": "mcp",
			      "transport": {
			        "type": "streamable-http",
			        "url": "https://mcp.example.com/mcp",
			        "headers": {
			          "X-API-Key": "literal"
			        },
			        "headerSecrets": {
			          "x-api-key": "api_key_secret"
			        }
			      }
			    }
			  }
			}`,
			path:    "sources.remoteDocs.transport.headerSecrets.x-api-key",
			message: "duplicates transport.headers",
		},
		{
			name: "mcp source allows only one service",
			document: `{
			  "cli": "1.0.0",
			  "mode": { "default": "discover" },
			  "runtime": {
			    "mode": "local"
			  },
			  "sources": {
			    "remoteDocs": {
			      "type": "mcp",
			      "transport": {
			        "type": "streamable-http",
			        "url": "https://mcp.example.com/mcp"
			      }
			    }
			  },
			  "services": {
			    "remoteDocs": {
			      "source": "remoteDocs"
			    },
			    "docsAlias": {
			      "source": "remoteDocs"
			    }
			  }
			}`,
			path:    "services.docsAlias.source",
			message: "second service pointing at mcp source",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			projectPath := writeJSON(t, dir, ".cli.json", tc.document)

			_, err := config.LoadEffective(config.LoadOptions{ProjectPath: projectPath, WorkingDir: dir})
			requireValidationDiagnostic(t, err, tc.path, tc.message)
		})
	}
}

func TestLoadEffectiveClearsExclusiveFieldsWhenSourceTypeChanges(t *testing.T) {
	dir := t.TempDir()
	managedPath := writeJSON(t, dir, "managed.json", `{
	  "cli": "1.0.0",
	  "mode": { "default": "discover" },
	  "sources": {
	    "filesystem": {
	      "type": "openapi",
	      "uri": "https://managed.example.com/openapi.json"
	    },
	    "remoteDocs": {
	      "type": "mcp",
	      "transport": {
	        "type": "streamable-http",
	        "url": "https://managed.example.com/mcp"
	      },
	      "disabledTools": ["admin.delete"],
	      "oauth": {
	        "mode": "clientCredentials",
	        "issuer": "https://auth.example.com",
	        "clientId": {
	          "type": "env",
	          "value": "REMOTE_MCP_CLIENT_ID"
	        },
	        "clientSecret": {
	          "type": "env",
	          "value": "REMOTE_MCP_CLIENT_SECRET"
	        }
	      }
	    }
	  }
	}`)
	projectPath := writeJSON(t, dir, ".cli.json", `{
	  "runtime": {
	    "mode": "local"
	  },
	  "sources": {
	    "filesystem": {
	      "type": "mcp",
	      "transport": {
	        "type": "stdio",
	        "command": "npx"
	      }
	    },
	    "remoteDocs": {
	      "type": "openapi",
	      "uri": "https://project.example.com/openapi.json"
	    }
	  }
	}`)

	effective, err := config.LoadEffective(config.LoadOptions{
		ManagedPath: managedPath,
		ProjectPath: projectPath,
		WorkingDir:  dir,
	})
	if err != nil {
		t.Fatalf("LoadEffective returned error: %v", err)
	}

	filesystem := effective.Config.Sources["filesystem"]
	if filesystem.URI != "" {
		t.Fatalf("expected URI to be cleared when source switches to mcp, got %q", filesystem.URI)
	}
	if filesystem.Transport == nil || filesystem.Transport.Command != "npx" {
		t.Fatalf("expected mcp transport to be retained after type switch, got %#v", filesystem.Transport)
	}

	remoteDocs := effective.Config.Sources["remoteDocs"]
	if remoteDocs.Transport != nil {
		t.Fatalf("expected transport to be cleared when source switches to openapi, got %#v", remoteDocs.Transport)
	}
	if len(remoteDocs.DisabledTools) != 0 {
		t.Fatalf("expected disabledTools to be cleared when source switches to openapi, got %#v", remoteDocs.DisabledTools)
	}
	if remoteDocs.OAuth != nil {
		t.Fatalf("expected oauth to be cleared when source switches to openapi, got %#v", remoteDocs.OAuth)
	}
	if remoteDocs.URI != "https://project.example.com/openapi.json" {
		t.Fatalf("expected openapi uri after type switch, got %q", remoteDocs.URI)
	}
}

func TestLoadEffectiveAllowsHigherScopeToRepairMCPServiceSource(t *testing.T) {
	dir := t.TempDir()
	managedPath := writeJSON(t, dir, "managed.json", `{
	  "cli": "1.0.0",
	  "mode": { "default": "discover" },
	  "sources": {
	    "demo": {
	      "type": "mcp",
	      "transport": {
	        "type": "stdio",
	        "command": "npx"
	      }
	    }
	  },
	  "services": {
	    "demo": {
	      "source": "other"
	    }
	  }
	}`)
	projectPath := writeJSON(t, dir, ".cli.json", `{
	  "runtime": {
	    "mode": "local"
	  },
	  "services": {
	    "demo": {
	      "source": "demo",
	      "alias": "Demo"
	    }
	  }
	}`)

	effective, err := config.LoadEffective(config.LoadOptions{
		ManagedPath: managedPath,
		ProjectPath: projectPath,
		WorkingDir:  dir,
	})
	if err != nil {
		t.Fatalf("LoadEffective returned error: %v", err)
	}

	service := effective.Config.Services["demo"]
	if service.Source != "demo" {
		t.Fatalf("expected higher scope to repair source to demo, got %q", service.Source)
	}
	if service.Alias != "Demo" {
		t.Fatalf("expected alias to merge from higher scope, got %q", service.Alias)
	}
}

func TestLoadEffectiveClearsExclusiveFieldsWhenMCPTransportTypeChanges(t *testing.T) {
	dir := t.TempDir()
	managedPath := writeJSON(t, dir, "managed.json", `{
	  "cli": "1.0.0",
	  "mode": { "default": "discover" },
	  "sources": {
	    "demo": {
	      "type": "mcp",
	      "transport": {
	        "type": "streamable-http",
	        "url": "https://managed.example.com/mcp"
	      },
	      "oauth": {
	        "mode": "clientCredentials",
	        "issuer": "https://auth.example.com",
	        "clientId": {
	          "type": "env",
	          "value": "DEMO_CLIENT_ID"
	        },
	        "clientSecret": {
	          "type": "env",
	          "value": "DEMO_CLIENT_SECRET"
	        }
	      }
	    }
	  }
	}`)
	projectPath := writeJSON(t, dir, ".cli.json", `{
	  "runtime": {
	    "mode": "local"
	  },
	  "sources": {
	    "demo": {
	      "transport": {
	        "type": "stdio",
	        "command": "npx"
	      }
	    }
	  }
	}`)

	effective, err := config.LoadEffective(config.LoadOptions{
		ManagedPath: managedPath,
		ProjectPath: projectPath,
		WorkingDir:  dir,
	})
	if err != nil {
		t.Fatalf("LoadEffective returned error: %v", err)
	}

	transport := effective.Config.Sources["demo"].Transport
	if transport == nil {
		t.Fatalf("expected transport to be preserved")
	}
	if transport.Type != "stdio" {
		t.Fatalf("expected stdio transport after override, got %q", transport.Type)
	}
	if transport.URL != "" {
		t.Fatalf("expected url to be cleared when transport switches to stdio, got %q", transport.URL)
	}
	if transport.Command != "npx" {
		t.Fatalf("expected command npx after override, got %q", transport.Command)
	}
	if effective.Config.Sources["demo"].OAuth != nil {
		t.Fatalf("expected oauth to be cleared when transport switches away from streamable-http, got %#v", effective.Config.Sources["demo"].OAuth)
	}
}

func TestLoadEffectiveRejectsInvalidMCPOAuthConfig(t *testing.T) {
	testCases := []struct {
		name     string
		document string
		path     string
		message  string
	}{
		{
			name: "authorizationCode is not allowed for mcp transport oauth",
			document: `{
			  "cli": "1.0.0",
			  "mode": { "default": "discover" },
			  "sources": {
			    "remoteDocs": {
			      "type": "mcp",
			      "transport": {
			        "type": "streamable-http",
			        "url": "https://mcp.example.com/mcp"
			      },
			      "oauth": {
			        "mode": "authorizationCode",
			        "issuer": "https://auth.example.com"
			      }
			    }
			  }
			}`,
			path:    "sources.remoteDocs.oauth.mode",
			message: "must be clientCredentials for mcp transport oauth",
		},
		{
			name: "clientCredentials requires clientId",
			document: `{
			  "cli": "1.0.0",
			  "mode": { "default": "discover" },
			  "sources": {
			    "remoteDocs": {
			      "type": "mcp",
			      "transport": {
			        "type": "streamable-http",
			        "url": "https://mcp.example.com/mcp"
			      },
			      "oauth": {
			        "mode": "clientCredentials",
			        "issuer": "https://auth.example.com",
			        "clientSecret": {
			          "type": "env",
			          "value": "REMOTE_MCP_CLIENT_SECRET"
			        }
			      }
			    }
			  }
			}`,
			path:    "sources.remoteDocs.oauth.clientId",
			message: "required for clientCredentials oauth",
		},
		{
			name: "clientCredentials requires clientSecret",
			document: `{
			  "cli": "1.0.0",
			  "mode": { "default": "discover" },
			  "sources": {
			    "remoteDocs": {
			      "type": "mcp",
			      "transport": {
			        "type": "streamable-http",
			        "url": "https://mcp.example.com/mcp"
			      },
			      "oauth": {
			        "mode": "clientCredentials",
			        "issuer": "https://auth.example.com",
			        "clientId": {
			          "type": "env",
			          "value": "REMOTE_MCP_CLIENT_ID"
			        }
			      }
			    }
			  }
			}`,
			path:    "sources.remoteDocs.oauth.clientSecret",
			message: "required for clientCredentials oauth",
		},
		{
			name: "clientCredentials forbids callbackPort",
			document: `{
			  "cli": "1.0.0",
			  "mode": { "default": "discover" },
			  "sources": {
			    "remoteDocs": {
			      "type": "mcp",
			      "transport": {
			        "type": "streamable-http",
			        "url": "https://mcp.example.com/mcp"
			      },
			      "oauth": {
			        "mode": "clientCredentials",
			        "issuer": "https://auth.example.com",
			        "clientId": {
			          "type": "env",
			          "value": "REMOTE_MCP_CLIENT_ID"
			        },
			        "clientSecret": {
			          "type": "env",
			          "value": "REMOTE_MCP_CLIENT_SECRET"
			        },
			        "callbackPort": 9999
			      }
			    }
			  }
			}`,
			path:    "sources.remoteDocs.oauth.callbackPort",
			message: "not allowed for clientCredentials oauth",
		},
		{
			name: "openIdConnect is not a direct mode",
			document: `{
			  "cli": "1.0.0",
			  "mode": { "default": "discover" },
			  "sources": {
			    "remoteDocs": {
			      "type": "mcp",
			      "transport": {
			        "type": "streamable-http",
			        "url": "https://mcp.example.com/mcp"
			      },
			      "oauth": {
			        "mode": "openIdConnect",
			        "issuer": "https://auth.example.com",
			        "clientId": {
			          "type": "env",
			          "value": "REMOTE_MCP_CLIENT_ID"
			        }
			      }
			    }
			  }
			}`,
			path:    "sources.remoteDocs.oauth.mode",
			message: "not a direct oauth mode",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			projectPath := writeJSON(t, dir, ".cli.json", tc.document)

			_, err := config.LoadEffective(config.LoadOptions{ProjectPath: projectPath, WorkingDir: dir})
			requireValidationDiagnostic(t, err, tc.path, tc.message)
		})
	}
}

func TestLoadEffectiveClearsExclusiveFieldsWhenMCPOAuthModeChanges(t *testing.T) {
	dir := t.TempDir()
	managedPath := writeJSON(t, dir, "managed.json", `{
	  "cli": "1.0.0",
	  "mode": { "default": "discover" },
	  "sources": {
	    "remoteDocs": {
	      "type": "mcp",
	      "transport": {
	        "type": "streamable-http",
	        "url": "https://managed.example.com/mcp"
	      },
	      "oauth": {
	        "mode": "authorizationCode",
	        "issuer": "https://auth.example.com",
	        "clientId": {
	          "type": "env",
	          "value": "REMOTE_MCP_CLIENT_ID"
	        },
	        "callbackPort": 7777
	      }
	    }
	  }
	}`)
	projectPath := writeJSON(t, dir, ".cli.json", `{
	  "runtime": {
	    "mode": "local"
	  },
	  "sources": {
	    "remoteDocs": {
	      "oauth": {
	        "mode": "clientCredentials",
	        "clientSecret": {
	          "type": "env",
	          "value": "REMOTE_MCP_CLIENT_SECRET"
	        }
	      }
	    }
	  }
	}`)

	effective, err := config.LoadEffective(config.LoadOptions{
		ManagedPath: managedPath,
		ProjectPath: projectPath,
		WorkingDir:  dir,
	})
	if err != nil {
		t.Fatalf("LoadEffective returned error: %v", err)
	}

	oauth := effective.Config.Sources["remoteDocs"].OAuth
	if oauth == nil {
		t.Fatalf("expected oauth config to be preserved")
	}
	if oauth.Mode != "clientCredentials" {
		t.Fatalf("expected oauth mode clientCredentials, got %q", oauth.Mode)
	}
	if oauth.CallbackPort != nil {
		t.Fatalf("expected callbackPort to be cleared when oauth switches to clientCredentials, got %#v", oauth.CallbackPort)
	}
	if oauth.ClientID == nil || oauth.ClientID.Value != "REMOTE_MCP_CLIENT_ID" {
		t.Fatalf("expected shared clientId to survive mode switch, got %#v", oauth.ClientID)
	}
	if oauth.ClientSecret == nil || oauth.ClientSecret.Value != "REMOTE_MCP_CLIENT_SECRET" {
		t.Fatalf("expected clientSecret from overriding scope, got %#v", oauth.ClientSecret)
	}
}
