// Package capability_test contains product-level integration tests for MCP server
// capabilities. Tests in this file exercise real MCP servers using the
// stdio and streamable-http transports.
//
// Stdio tests launch @modelcontextprotocol/server-filesystem via npx and require
// Node.js / npx to be installed.
//
// Remote tests connect to @modelcontextprotocol/server-everything via
// streamable-http and require the docker service to be running:
//
//	docker compose -f product-tests/mcp/remote/docker-compose.yml up -d
//
// The remote server address can be overridden with the MCP_REMOTE_HOST env var
// (default: localhost:3001).
package capability_test

import (
	"context"
	"net"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"

	"github.com/StevenBuglione/oas-cli-go/pkg/config"
	mcpclient "github.com/StevenBuglione/oas-cli-go/pkg/mcp/client"
)

// TestCapabilityExecuteMCPStdio validates stdio MCP transport using
// @modelcontextprotocol/server-filesystem launched via npx.
// Covers: catalog discovery, successful tool execution, unknown-tool failure.
func TestCapabilityExecuteMCPStdio(t *testing.T) {
	if _, err := exec.LookPath("npx"); err != nil {
		t.Skip("npx required for MCP stdio product tests: install Node.js")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()

	source := config.Source{
		Type: "mcp",
		Transport: &config.MCPTransport{
			Type:    "stdio",
			Command: "npx",
			Args:    []string{"-y", "@modelcontextprotocol/server-filesystem", "/tmp"},
		},
	}

	t.Run("catalog_discovery", func(t *testing.T) {
		c, err := mcpclient.Open(source, nil, config.PolicyConfig{}, t.TempDir(), nil, ctx)
		if err != nil {
			t.Fatalf("Open: %v", err)
		}
		defer c.Close()

		tools, err := c.ListTools(ctx)
		if err != nil {
			t.Fatalf("ListTools: %v", err)
		}
		if len(tools) == 0 {
			t.Fatal("expected at least one tool from filesystem server")
		}

		names := make([]string, len(tools))
		for i, tool := range tools {
			names[i] = tool.Name
		}
		t.Logf("filesystem tools (%d): %s", len(tools), strings.Join(names, ", "))

		var found bool
		for _, tool := range tools {
			if tool.Name == "list_directory" {
				found = true
				break
			}
		}
		if !found {
			t.Fatalf("expected list_directory in filesystem tool catalog, got: %v", names)
		}
	})

	t.Run("tool_execution_success", func(t *testing.T) {
		c, err := mcpclient.Open(source, nil, config.PolicyConfig{}, t.TempDir(), nil, ctx)
		if err != nil {
			t.Fatalf("Open: %v", err)
		}
		defer c.Close()

		result, err := c.CallTool(ctx, "list_directory", map[string]any{"path": "/tmp"})
		if err != nil {
			t.Fatalf("CallTool list_directory /tmp: %v", err)
		}
		if result.IsError {
			t.Fatalf("expected successful result, got MCP error: %+v", result)
		}
		t.Logf("list_directory result content items: %d", len(result.Content))
	})

	t.Run("unknown_tool_failure", func(t *testing.T) {
		c, err := mcpclient.Open(source, nil, config.PolicyConfig{}, t.TempDir(), nil, ctx)
		if err != nil {
			t.Fatalf("Open: %v", err)
		}
		defer c.Close()

		// MCP servers may report unknown-tool failures either as JSON-RPC errors
		// (Go error) or as a tool result with IsError=true. Both are valid per spec.
		result, err := c.CallTool(ctx, "nonexistent_tool_xyz", map[string]any{})
		if err != nil {
			t.Logf("got expected RPC error for unknown tool: %v", err)
			return
		}
		if result.IsError {
			t.Logf("got expected tool-level error for unknown tool; content: %v", result.Content)
			return
		}
		t.Fatal("expected failure (RPC error or IsError=true) for nonexistent tool, got success")
	})
}

// TestCapabilityExecuteMCPRemote validates the streamable-http MCP transport
// using @modelcontextprotocol/server-everything running in Docker.
//
// Start the service before running this test:
//
//	docker compose -f product-tests/mcp/remote/docker-compose.yml up -d
//
// The test skips automatically when the remote service is unreachable.
func TestCapabilityExecuteMCPRemote(t *testing.T) {
	addr := mcpRemoteAddr()
	if !isTCPOpen(addr) {
		t.Skipf(
			"remote MCP service unreachable at %s — start it with:\n"+
				"  docker compose -f product-tests/mcp/remote/docker-compose.yml up -d",
			addr,
		)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	source := config.Source{
		Type: "mcp",
		Transport: &config.MCPTransport{
			Type: "streamable-http",
			URL:  "http://" + addr + "/mcp",
		},
	}

	t.Run("catalog_discovery", func(t *testing.T) {
		c, err := mcpclient.Open(source, nil, config.PolicyConfig{}, t.TempDir(), nil, ctx)
		if err != nil {
			t.Fatalf("Open: %v", err)
		}
		defer c.Close()

		tools, err := c.ListTools(ctx)
		if err != nil {
			t.Fatalf("ListTools: %v", err)
		}
		if len(tools) == 0 {
			t.Fatal("expected at least one tool from remote everything server")
		}
		t.Logf("remote server tool count: %d", len(tools))
	})

	t.Run("tool_execution_success", func(t *testing.T) {
		c, err := mcpclient.Open(source, nil, config.PolicyConfig{}, t.TempDir(), nil, ctx)
		if err != nil {
			t.Fatalf("Open: %v", err)
		}
		defer c.Close()

		result, err := c.CallTool(ctx, "echo", map[string]any{"message": "hello from product test"})
		if err != nil {
			t.Fatalf("CallTool echo: %v", err)
		}
		if result.IsError {
			t.Fatalf("expected successful result, got MCP error: %+v", result)
		}
		t.Logf("echo result content items: %d", len(result.Content))
	})

	t.Run("unknown_tool_failure", func(t *testing.T) {
		c, err := mcpclient.Open(source, nil, config.PolicyConfig{}, t.TempDir(), nil, ctx)
		if err != nil {
			t.Fatalf("Open: %v", err)
		}
		defer c.Close()

		// MCP servers may report unknown-tool failures either as JSON-RPC errors
		// (Go error) or as a tool result with IsError=true. Both are valid per spec.
		result, err := c.CallTool(ctx, "nonexistent_tool_xyz", map[string]any{})
		if err != nil {
			t.Logf("got expected RPC error for unknown tool: %v", err)
			return
		}
		if result.IsError {
			t.Logf("got expected tool-level error for unknown tool; content: %v", result.Content)
			return
		}
		t.Fatal("expected failure (RPC error or IsError=true) for nonexistent tool, got success")
	})
}

// mcpRemoteAddr returns the TCP address of the remote MCP service.
// Override with MCP_REMOTE_HOST env var (e.g. "192.168.1.10:3001").
func mcpRemoteAddr() string {
	if host := os.Getenv("MCP_REMOTE_HOST"); host != "" {
		return host
	}
	return "localhost:3001"
}

// isTCPOpen reports whether the given TCP address is accepting connections.
func isTCPOpen(addr string) bool {
	conn, err := net.DialTimeout("tcp", addr, 2*time.Second)
	if err != nil {
		return false
	}
	conn.Close()
	return true
}
