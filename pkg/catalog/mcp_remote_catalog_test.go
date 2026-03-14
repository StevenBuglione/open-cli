package catalog_test

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"testing"

	"github.com/StevenBuglione/oas-cli-go/pkg/catalog"
	"github.com/StevenBuglione/oas-cli-go/pkg/config"
)

func TestBuildSupportsStreamableHTTPMCPSources(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/mcp" {
			http.NotFound(w, r)
			return
		}
		if r.Method == http.MethodDelete {
			if got := r.Header.Get("Mcp-Session-Id"); got != "session-1" {
				t.Fatalf("expected session header on delete, got %q", got)
			}
			w.WriteHeader(http.StatusNoContent)
			return
		}
		var message map[string]any
		if err := json.NewDecoder(r.Body).Decode(&message); err != nil {
			t.Fatalf("decode request body: %v", err)
		}
		method, _ := message["method"].(string)
		switch method {
		case "initialize":
			w.Header().Set("Content-Type", "application/json")
			w.Header().Set("Mcp-Session-Id", "session-1")
			_ = json.NewEncoder(w).Encode(map[string]any{
				"jsonrpc": "2.0",
				"id":      message["id"],
				"result": map[string]any{
					"protocolVersion": "2025-03-26",
					"capabilities":    map[string]any{"tools": map[string]any{}},
					"serverInfo":      map[string]any{"name": "streamable", "version": "1.0.0"},
				},
			})
		case "notifications/initialized":
			w.WriteHeader(http.StatusAccepted)
		case "tools/list":
			if got := r.Header.Get("Mcp-Session-Id"); got != "session-1" {
				t.Fatalf("expected session header, got %q", got)
			}
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]any{
				"jsonrpc": "2.0",
				"id":      message["id"],
				"result": map[string]any{
					"tools": []map[string]any{{
						"name":        "echo",
						"description": "Echo",
						"inputSchema": map[string]any{"type": "string"},
					}},
				},
			})
		default:
			t.Fatalf("unexpected method %q", method)
		}
	}))
	defer server.Close()

	cfg := config.Config{
		CLI:  "1.0.0",
		Mode: config.ModeConfig{Default: "discover"},
		Sources: map[string]config.Source{
			"remote": {
				Type:    "mcp",
				Enabled: true,
				Transport: &config.MCPTransport{
					Type: "streamable-http",
					URL:  server.URL + "/mcp",
				},
			},
		},
		Services: map[string]config.Service{
			"remote": {Source: "remote", Alias: "remote"},
		},
	}

	ntc, err := catalog.Build(context.Background(), catalog.BuildOptions{Config: cfg})
	if err != nil {
		t.Fatalf("Build returned error: %v", err)
	}
	if ntc.FindTool("remote:echo") == nil {
		t.Fatalf("expected echo tool from streamable-http source, got %#v", ntc.Tools)
	}
}

func TestBuildSupportsLegacySSEMCPSources(t *testing.T) {
	var (
		mu         sync.Mutex
		streamChan chan string
	)
	var server *httptest.Server
	server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/sse":
			w.Header().Set("Content-Type", "text/event-stream")
			flusher, ok := w.(http.Flusher)
			if !ok {
				t.Fatal("response writer does not support flushing")
			}
			ch := make(chan string, 8)
			mu.Lock()
			streamChan = ch
			mu.Unlock()
			fmt.Fprintf(w, "event: endpoint\ndata: %s/messages\n\n", server.URL)
			flusher.Flush()
			for {
				select {
				case <-r.Context().Done():
					return
				case message := <-ch:
					fmt.Fprint(w, message)
					flusher.Flush()
				}
			}
		case "/messages":
			var message map[string]any
			if err := json.NewDecoder(r.Body).Decode(&message); err != nil {
				t.Fatalf("decode request body: %v", err)
			}
			method, _ := message["method"].(string)
			if method == "notifications/initialized" {
				w.WriteHeader(http.StatusAccepted)
				return
			}
			data, err := json.Marshal(map[string]any{
				"jsonrpc": "2.0",
				"id":      message["id"],
				"result": map[string]any{
					"protocolVersion": "2024-11-05",
					"capabilities":    map[string]any{"tools": map[string]any{}},
					"serverInfo":      map[string]any{"name": "legacy-sse", "version": "1.0.0"},
					"tools": []map[string]any{{
						"name":        "echo",
						"description": "Echo",
						"inputSchema": map[string]any{"type": "string"},
					}},
				},
			})
			if method == "tools/list" {
				data, err = json.Marshal(map[string]any{
					"jsonrpc": "2.0",
					"id":      message["id"],
					"result": map[string]any{
						"tools": []map[string]any{{
							"name":        "echo",
							"description": "Echo",
							"inputSchema": map[string]any{"type": "string"},
						}},
					},
				})
			}
			if err != nil {
				t.Fatalf("marshal response: %v", err)
			}
			mu.Lock()
			ch := streamChan
			mu.Unlock()
			ch <- "event: message\ndata: " + strings.ReplaceAll(string(data), "\n", "") + "\n\n"
			w.WriteHeader(http.StatusAccepted)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	cfg := config.Config{
		CLI:  "1.0.0",
		Mode: config.ModeConfig{Default: "discover"},
		Sources: map[string]config.Source{
			"remote": {
				Type:    "mcp",
				Enabled: true,
				Transport: &config.MCPTransport{
					Type: "sse",
					URL:  server.URL + "/sse",
				},
			},
		},
		Services: map[string]config.Service{
			"remote": {Source: "remote", Alias: "remote"},
		},
	}

	ntc, err := catalog.Build(context.Background(), catalog.BuildOptions{Config: cfg})
	if err != nil {
		t.Fatalf("Build returned error: %v", err)
	}
	if ntc.FindTool("remote:echo") == nil {
		t.Fatalf("expected echo tool from SSE source, got %#v", ntc.Tools)
	}
}

func TestBuildSupportsStreamableHTTPMCPSourceOAuthAndHeaderSecrets(t *testing.T) {
	if err := os.Setenv("MCP_CLIENT_ID", "mcp-client"); err != nil {
		t.Fatalf("set env: %v", err)
	}
	if err := os.Setenv("MCP_CLIENT_SECRET", "mcp-secret"); err != nil {
		t.Fatalf("set env: %v", err)
	}
	if err := os.Setenv("MCP_API_KEY", "header-secret-123"); err != nil {
		t.Fatalf("set env: %v", err)
	}
	t.Cleanup(func() {
		_ = os.Unsetenv("MCP_CLIENT_ID")
		_ = os.Unsetenv("MCP_CLIENT_SECRET")
		_ = os.Unsetenv("MCP_API_KEY")
	})

	var tokenCalls int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/oauth/token":
			tokenCalls++
			if err := r.ParseForm(); err != nil {
				t.Fatalf("parse token form: %v", err)
			}
			if got := r.Form.Get("client_id"); got != "mcp-client" {
				t.Fatalf("expected mcp client id, got %q", got)
			}
			if got := r.Form.Get("client_secret"); got != "mcp-secret" {
				t.Fatalf("expected mcp client secret, got %q", got)
			}
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]any{
				"access_token": "mcp-oauth-token",
				"token_type":   "Bearer",
				"expires_in":   3600,
			})
		case "/mcp":
			if r.Method == http.MethodDelete {
				w.WriteHeader(http.StatusNoContent)
				return
			}
			if got := r.Header.Get("Authorization"); got != "Bearer mcp-oauth-token" {
				t.Fatalf("expected oauth auth header, got %q", got)
			}
			if got := r.Header.Get("X-API-Key"); got != "header-secret-123" {
				t.Fatalf("expected header secret, got %q", got)
			}
			var message map[string]any
			if err := json.NewDecoder(r.Body).Decode(&message); err != nil {
				t.Fatalf("decode request body: %v", err)
			}
			method, _ := message["method"].(string)
			switch method {
			case "initialize":
				w.Header().Set("Content-Type", "application/json")
				w.Header().Set("Mcp-Session-Id", "session-auth")
				_ = json.NewEncoder(w).Encode(map[string]any{
					"jsonrpc": "2.0",
					"id":      message["id"],
					"result": map[string]any{
						"protocolVersion": "2025-03-26",
						"capabilities":    map[string]any{"tools": map[string]any{}},
						"serverInfo":      map[string]any{"name": "streamable", "version": "1.0.0"},
					},
				})
			case "notifications/initialized":
				w.WriteHeader(http.StatusAccepted)
			case "tools/list":
				w.Header().Set("Content-Type", "application/json")
				_ = json.NewEncoder(w).Encode(map[string]any{
					"jsonrpc": "2.0",
					"id":      message["id"],
					"result": map[string]any{
						"tools": []map[string]any{{
							"name":        "echo",
							"description": "Echo",
							"inputSchema": map[string]any{"type": "string"},
						}},
					},
				})
			default:
				t.Fatalf("unexpected method %q", method)
			}
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	cfg := config.Config{
		CLI:  "1.0.0",
		Mode: config.ModeConfig{Default: "discover"},
		Sources: map[string]config.Source{
			"remote": {
				Type:    "mcp",
				Enabled: true,
				Transport: &config.MCPTransport{
					Type: "streamable-http",
					URL:  server.URL + "/mcp",
					HeaderSecrets: map[string]string{
						"X-API-Key": "mcp.apiKey",
					},
				},
				OAuth: &config.OAuthConfig{
					Mode:     "clientCredentials",
					TokenURL: server.URL + "/oauth/token",
					ClientID: &config.SecretRef{Type: "env", Value: "MCP_CLIENT_ID"},
					ClientSecret: &config.SecretRef{
						Type:  "env",
						Value: "MCP_CLIENT_SECRET",
					},
				},
			},
		},
		Services: map[string]config.Service{
			"remote": {Source: "remote", Alias: "remote"},
		},
		Secrets: map[string]config.Secret{
			"mcp.apiKey": {Type: "env", Value: "MCP_API_KEY"},
		},
	}

	ntc, err := catalog.Build(context.Background(), catalog.BuildOptions{Config: cfg})
	if err != nil {
		t.Fatalf("Build returned error: %v", err)
	}
	if ntc.FindTool("remote:echo") == nil {
		t.Fatalf("expected echo tool from authenticated streamable-http source, got %#v", ntc.Tools)
	}
	if tokenCalls != 1 {
		t.Fatalf("expected one transport oauth token request, got %d", tokenCalls)
	}
}
