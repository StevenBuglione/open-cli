package client_test

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/StevenBuglione/oas-cli-go/pkg/config"
	mcpclient "github.com/StevenBuglione/oas-cli-go/pkg/mcp/client"
)

func TestOpenStdioUsesNewlineDelimitedJSONRPC(t *testing.T) {
	if _, err := exec.LookPath("python3"); err != nil {
		t.Skip("python3 is required for stdio transport integration test")
	}

	dir := t.TempDir()
	serverPath := writeClientFile(t, dir, "fake_stdio_server.py", `
import json
import sys

for line in sys.stdin:
    line = line.strip()
    if not line:
        continue
    message = json.loads(line)
    method = message.get("method")
    if method == "initialize":
        sys.stdout.write(json.dumps({
            "jsonrpc": "2.0",
            "id": message["id"],
            "result": {
                "protocolVersion": "2025-03-26",
                "capabilities": {"tools": {}},
                "serverInfo": {"name": "fake-mcp", "version": "1.0.0"}
            }
        }) + "\n")
        sys.stdout.flush()
    elif method == "notifications/initialized":
        continue
    elif method == "tools/list":
        sys.stdout.write(json.dumps({
            "jsonrpc": "2.0",
            "id": message["id"],
            "result": {
                "tools": [
                    {
                        "name": "echo",
                        "description": "Echo",
                        "inputSchema": {"type": "string"}
                    }
                ]
            }
        }) + "\n")
        sys.stdout.flush()
`)

	client, err := mcpclient.Open(config.Source{
		Type: "mcp",
		Transport: &config.MCPTransport{
			Type:    "stdio",
			Command: "python3",
			Args:    []string{serverPath},
		},
	}, nil, config.PolicyConfig{}, "", nil, context.Background())
	if err != nil {
		t.Fatalf("Open returned error: %v", err)
	}
	defer client.Close()

	tools, err := client.ListTools(context.Background())
	if err != nil {
		t.Fatalf("ListTools returned error: %v", err)
	}
	if len(tools) != 1 || tools[0].Name != "echo" {
		t.Fatalf("expected newline-delimited stdio tool listing, got %#v", tools)
	}
}

func writeClientFile(t *testing.T, dir, name, content string) string {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write %s: %v", name, err)
	}
	return path
}
