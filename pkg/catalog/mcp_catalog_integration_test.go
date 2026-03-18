package catalog_test

import (
	"context"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/StevenBuglione/open-cli/pkg/catalog"
	"github.com/StevenBuglione/open-cli/pkg/config"
)

func TestBuildSupportsMCPStdioSources(t *testing.T) {
	if _, err := exec.LookPath("python3"); err != nil {
		t.Skip("python3 is required for MCP stdio integration test")
	}

	dir := t.TempDir()
	serverPath := writeFile(t, dir, "fake_mcp_server.py", `
import json
import sys

TOOLS = [
    {
        "name": "list_docs",
        "description": "List docs",
        "inputSchema": {
            "type": "object",
            "properties": {
                "query": {"type": "string"}
            },
            "required": ["query"]
        }
    },
    {
        "name": "echo",
        "description": "Echo a raw string",
        "inputSchema": {
            "type": "string"
        }
    },
    {
        "name": "dangerous_delete",
        "description": "Delete a document",
        "inputSchema": {
            "type": "object",
            "properties": {
                "id": {"type": "string"}
            },
            "required": ["id"]
        }
    }
]

def read_message():
    line = sys.stdin.readline()
    if not line:
        return None
    line = line.strip()
    if not line:
        return None
    return json.loads(line)

def write_message(message):
    sys.stdout.write(json.dumps(message) + "\n")
    sys.stdout.flush()

while True:
    message = read_message()
    if message is None:
        break
    method = message.get("method")
    if method == "initialize":
        write_message({
            "jsonrpc": "2.0",
            "id": message["id"],
            "result": {
                "protocolVersion": "2025-03-26",
                "capabilities": {"tools": {}},
                "serverInfo": {"name": "fake-mcp", "version": "1.0.0"}
            }
        })
    elif method == "notifications/initialized":
        continue
    elif method == "tools/list":
        write_message({
            "jsonrpc": "2.0",
            "id": message["id"],
            "result": {
                "tools": TOOLS
            }
        })
    else:
        write_message({
            "jsonrpc": "2.0",
            "id": message.get("id"),
            "error": {"code": -32601, "message": f"unsupported method: {method}"}
        })
`)

	cfg := config.Config{
		CLI:  "1.0.0",
		Mode: config.ModeConfig{Default: "discover"},
		Sources: map[string]config.Source{
			"docs": {
				Type:    "mcp",
				Enabled: true,
				Transport: &config.MCPTransport{
					Type:    "stdio",
					Command: "python3",
					Args:    []string{filepath.ToSlash(serverPath)},
				},
				DisabledTools: []string{"dangerous_delete"},
			},
		},
		Services: map[string]config.Service{
			"docs": {
				Source: "docs",
				Alias:  "docs",
			},
		},
	}

	ntc, err := catalog.Build(context.Background(), catalog.BuildOptions{
		Config:  cfg,
		BaseDir: dir,
	})
	if err != nil {
		t.Fatalf("Build returned error: %v", err)
	}

	if len(ntc.Services) != 1 {
		t.Fatalf("expected 1 service, got %#v", ntc.Services)
	}
	if len(ntc.Tools) != 2 {
		t.Fatalf("expected 2 enabled MCP tools, got %#v", ntc.Tools)
	}
	if ntc.FindTool("docs:dangerous_delete") != nil {
		t.Fatalf("expected disabled MCP tool to be filtered from catalog")
	}

	echoTool := ntc.FindTool("docs:echo")
	if echoTool == nil {
		t.Fatalf("expected echo MCP tool to be present")
	}
	if echoTool.Backend == nil {
		t.Fatalf("expected MCP backend metadata on synthetic tool")
	}
	if echoTool.Backend.Kind != "mcp" || echoTool.Backend.SourceID != "docs" || echoTool.Backend.ToolName != "echo" {
		t.Fatalf("unexpected MCP backend metadata: %#v", echoTool.Backend)
	}
	if !echoTool.Backend.InputWrapped {
		t.Fatalf("expected primitive MCP tool input to record wrapper metadata")
	}
	if echoTool.Backend.Transport != "stdio" {
		t.Fatalf("expected stdio transport backend metadata, got %#v", echoTool.Backend)
	}
	if echoTool.RequestBody == nil || len(echoTool.RequestBody.ContentTypes) != 1 {
		t.Fatalf("expected wrapped request body for primitive MCP tool input, got %#v", echoTool.RequestBody)
	}
	schema := echoTool.RequestBody.ContentTypes[0].Schema
	properties, ok := schema["properties"].(map[string]any)
	if !ok {
		t.Fatalf("expected wrapped object schema properties, got %#v", schema)
	}
	inputProperty, ok := properties["input"].(map[string]any)
	if !ok || inputProperty["type"] != "string" {
		t.Fatalf("expected primitive MCP input schema to be wrapped under input, got %#v", schema)
	}
}
