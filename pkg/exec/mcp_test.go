package exec_test

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"testing"

	"github.com/StevenBuglione/open-cli/pkg/catalog"
	"github.com/StevenBuglione/open-cli/pkg/config"
	toolsexec "github.com/StevenBuglione/open-cli/pkg/exec"
)

type recordingSupervisor struct {
	mu       sync.Mutex
	tracked  []int
	released []int
}

func (s *recordingSupervisor) Track(pid int) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.tracked = append(s.tracked, pid)
	return nil
}

func (s *recordingSupervisor) Release(pid int) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.released = append(s.released, pid)
	return nil
}

func TestExecuteMCPUnwrapsWrappedPrimitiveInput(t *testing.T) {
	if _, err := exec.LookPath("python3"); err != nil {
		t.Skip("python3 is required for MCP exec integration test")
	}

	dir := t.TempDir()
	serverPath := writeExecFile(t, dir, "fake_mcp_server.py", `
import json
import sys

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
    elif method == "tools/call":
        arguments = message["params"]["arguments"]
        write_message({
            "jsonrpc": "2.0",
            "id": message["id"],
            "result": {
                "structuredContent": {"arguments": arguments},
                "isError": False
            }
        })
    else:
        write_message({
            "jsonrpc": "2.0",
            "id": message.get("id"),
            "result": {"tools": []}
        })
`)

	result, err := toolsexec.ExecuteMCP(context.Background(), toolsexec.MCPRequest{
		Tool: catalog.Tool{
			ID:          "docs:echo",
			OperationID: "echo",
			RequestBody: &catalog.RequestBody{
				ContentTypes: []catalog.RequestBodyContent{{
					MediaType: "application/json",
					Schema: map[string]any{
						"type": "object",
						"properties": map[string]any{
							"input": map[string]any{
								"type": "string",
							},
						},
						"required": []any{"input"},
					},
				}},
			},
		},
		Source: config.Source{
			Type: "mcp",
			Transport: &config.MCPTransport{
				Type:    "stdio",
				Command: "python3",
				Args:    []string{filepath.ToSlash(serverPath)},
			},
		},
		Body: []byte(`{"input":"hello"}`),
	})
	if err != nil {
		t.Fatalf("ExecuteMCP returned error: %v", err)
	}

	var payload map[string]any
	if err := json.Unmarshal(result.Body, &payload); err != nil {
		t.Fatalf("decode MCP result: %v", err)
	}
	structured, ok := payload["structuredContent"].(map[string]any)
	if !ok {
		t.Fatalf("expected structuredContent in MCP result, got %#v", payload)
	}
	if structured["arguments"] != "hello" {
		t.Fatalf("expected wrapped primitive input to be unwrapped to string, got %#v", structured["arguments"])
	}
}

func TestExecuteMCPRejectsMalformedWrappedInput(t *testing.T) {
	cases := []struct {
		name string
		body string
	}{
		{name: "scalar body", body: `"hello"`},
		{name: "missing input field", body: `{}`},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := toolsexec.ExecuteMCP(context.Background(), toolsexec.MCPRequest{
				Tool: catalog.Tool{
					ID:          "docs:echo",
					OperationID: "echo",
					Backend: &catalog.ToolBackend{
						Kind:         "mcp",
						InputWrapped: true,
					},
				},
				Body: []byte(tc.body),
			})
			if err == nil {
				t.Fatalf("expected malformed wrapped input to be rejected")
			}
		})
	}
}

func TestExecuteMCPTracksAndReleasesManagedStdioProcess(t *testing.T) {
	if _, err := exec.LookPath("python3"); err != nil {
		t.Skip("python3 is required for MCP exec integration test")
	}

	dir := t.TempDir()
	serverPath := writeExecFile(t, dir, "fake_tracked_mcp_server.py", `
import json
import sys

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
    elif method == "tools/call":
        write_message({
            "jsonrpc": "2.0",
            "id": message["id"],
            "result": {
                "structuredContent": {"ok": True},
                "isError": False
            }
        })
    else:
        write_message({
            "jsonrpc": "2.0",
            "id": message.get("id"),
            "result": {"tools": []}
        })
`)

	sup := &recordingSupervisor{}

	_, err := toolsexec.ExecuteMCP(context.Background(), toolsexec.MCPRequest{
		Tool: catalog.Tool{
			ID:          "docs:echo",
			OperationID: "echo",
		},
		Source: config.Source{
			Type: "mcp",
			Transport: &config.MCPTransport{
				Type:    "stdio",
				Command: "python3",
				Args:    []string{filepath.ToSlash(serverPath)},
			},
		},
		ProcessSupervisor: sup,
	})
	if err != nil {
		t.Fatalf("ExecuteMCP returned error: %v", err)
	}

	sup.mu.Lock()
	defer sup.mu.Unlock()
	if len(sup.tracked) != 1 {
		t.Fatalf("expected one tracked managed pid, got %#v", sup.tracked)
	}
	if len(sup.released) != 1 {
		t.Fatalf("expected one released managed pid, got %#v", sup.released)
	}
	if sup.released[0] != sup.tracked[0] || sup.tracked[0] <= 0 {
		t.Fatalf("expected same positive pid to be tracked and released, got tracked=%#v released=%#v", sup.tracked, sup.released)
	}
}

func writeExecFile(t *testing.T, dir, name, content string) string {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write %s: %v", name, err)
	}
	return path
}
