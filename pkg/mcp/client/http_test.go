package client_test

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

	"github.com/StevenBuglione/oas-cli-go/pkg/config"
	mcpclient "github.com/StevenBuglione/oas-cli-go/pkg/mcp/client"
)

func TestOpenStreamableHTTPSupportsSessionListAndCall(t *testing.T) {
	var sawSessionHeader string
	var server *httptest.Server
	server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/mcp" {
			http.NotFound(w, r)
			return
		}
		if r.Method != http.MethodPost {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		if got := r.Header.Get("Accept"); !strings.Contains(got, "application/json") || !strings.Contains(got, "text/event-stream") {
			t.Fatalf("expected streamable-http accept header, got %q", got)
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
		case "tools/list":
			sawSessionHeader = r.Header.Get("Mcp-Session-Id")
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
		case "tools/call":
			sawSessionHeader = r.Header.Get("Mcp-Session-Id")
			params := message["params"].(map[string]any)
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]any{
				"jsonrpc": "2.0",
				"id":      message["id"],
				"result": map[string]any{
					"structuredContent": map[string]any{"arguments": params["arguments"]},
					"content":           []map[string]any{{"type": "text", "text": "ok"}},
				},
			})
		case "notifications/initialized":
			w.WriteHeader(http.StatusAccepted)
			return
		default:
			t.Fatalf("unexpected MCP method %q", method)
		}
	}))
	defer server.Close()

	client, err := mcpclient.Open(config.Source{
		Type: "mcp",
		Transport: &config.MCPTransport{
			Type: "streamable-http",
			URL:  server.URL + "/mcp",
		},
	}, nil, config.PolicyConfig{}, "", server.Client(), context.Background())
	if err != nil {
		t.Fatalf("Open returned error: %v", err)
	}
	defer client.Close()

	tools, err := client.ListTools(context.Background())
	if err != nil {
		t.Fatalf("ListTools returned error: %v", err)
	}
	if sawSessionHeader != "session-1" {
		t.Fatalf("expected session header on streamable-http follow-up request, got %q", sawSessionHeader)
	}
	if len(tools) != 1 || tools[0].Name != "echo" {
		t.Fatalf("unexpected tools response: %#v", tools)
	}

	result, err := client.CallTool(context.Background(), "echo", "hello")
	if err != nil {
		t.Fatalf("CallTool returned error: %v", err)
	}
	if result.StructuredContent.(map[string]any)["arguments"] != "hello" {
		t.Fatalf("unexpected tool call result: %#v", result)
	}
}

func TestOpenSSEUsesEndpointEventListAndCall(t *testing.T) {
	var (
		mu         sync.Mutex
		streamChan chan string
	)
	var server *httptest.Server
	server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/sse":
			w.Header().Set("Content-Type", "text/event-stream")
			w.Header().Set("Cache-Control", "no-cache")
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
			var payload map[string]any
			switch method {
			case "initialize":
				payload = map[string]any{
					"jsonrpc": "2.0",
					"id":      message["id"],
					"result": map[string]any{
						"protocolVersion": "2024-11-05",
						"capabilities":    map[string]any{"tools": map[string]any{}},
						"serverInfo":      map[string]any{"name": "legacy-sse", "version": "1.0.0"},
					},
				}
			case "tools/list":
				payload = map[string]any{
					"jsonrpc": "2.0",
					"id":      message["id"],
					"result": map[string]any{
						"tools": []map[string]any{{
							"name":        "echo",
							"description": "Echo",
							"inputSchema": map[string]any{"type": "string"},
						}},
					},
				}
			case "tools/call":
				params := message["params"].(map[string]any)
				payload = map[string]any{
					"jsonrpc": "2.0",
					"id":      message["id"],
					"result": map[string]any{
						"structuredContent": map[string]any{"arguments": params["arguments"]},
						"content":           []map[string]any{{"type": "text", "text": "ok"}},
					},
				}
			case "notifications/initialized":
				w.WriteHeader(http.StatusAccepted)
				return
			default:
				t.Fatalf("unexpected legacy SSE MCP method %q", method)
			}

			data, err := json.Marshal(payload)
			if err != nil {
				t.Fatalf("marshal SSE payload: %v", err)
			}
			mu.Lock()
			ch := streamChan
			mu.Unlock()
			if ch == nil {
				t.Fatalf("SSE stream was not initialized before POST")
			}
			ch <- "event: message\ndata: " + string(data) + "\n\n"
			w.WriteHeader(http.StatusAccepted)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	client, err := mcpclient.Open(config.Source{
		Type: "mcp",
		Transport: &config.MCPTransport{
			Type: "sse",
			URL:  server.URL + "/sse",
		},
	}, nil, config.PolicyConfig{}, "", server.Client(), context.Background())
	if err != nil {
		t.Fatalf("Open returned error: %v", err)
	}
	defer client.Close()

	tools, err := client.ListTools(context.Background())
	if err != nil {
		t.Fatalf("ListTools returned error: %v", err)
	}
	if len(tools) != 1 || tools[0].Name != "echo" {
		t.Fatalf("unexpected tools response: %#v", tools)
	}

	result, err := client.CallTool(context.Background(), "echo", "hello")
	if err != nil {
		t.Fatalf("CallTool returned error: %v", err)
	}
	if result.StructuredContent.(map[string]any)["arguments"] != "hello" {
		t.Fatalf("unexpected tool call result: %#v", result)
	}
}

func TestOpenStreamableHTTPUsesProvidedHTTPClientForTransportAndOAuth(t *testing.T) {
	if err := os.Setenv("MCP_TLS_CLIENT_ID", "tls-client"); err != nil {
		t.Fatalf("set env: %v", err)
	}
	if err := os.Setenv("MCP_TLS_CLIENT_SECRET", "tls-secret"); err != nil {
		t.Fatalf("set env: %v", err)
	}
	t.Cleanup(func() {
		_ = os.Unsetenv("MCP_TLS_CLIENT_ID")
		_ = os.Unsetenv("MCP_TLS_CLIENT_SECRET")
	})

	var tokenCalls int
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/oauth/token":
			tokenCalls++
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]any{
				"access_token": "tls-token",
				"token_type":   "Bearer",
				"expires_in":   3600,
			})
		case "/mcp":
			if r.Method == http.MethodDelete {
				w.WriteHeader(http.StatusNoContent)
				return
			}
			if got := r.Header.Get("Authorization"); got != "Bearer tls-token" {
				t.Fatalf("expected oauth header, got %q", got)
			}
			var message map[string]any
			if err := json.NewDecoder(r.Body).Decode(&message); err != nil {
				t.Fatalf("decode request body: %v", err)
			}
			switch message["method"] {
			case "initialize":
				w.Header().Set("Content-Type", "application/json")
				w.Header().Set("Mcp-Session-Id", "tls-session")
				_ = json.NewEncoder(w).Encode(map[string]any{
					"jsonrpc": "2.0",
					"id":      message["id"],
					"result": map[string]any{
						"protocolVersion": "2025-03-26",
						"capabilities":    map[string]any{"tools": map[string]any{}},
						"serverInfo":      map[string]any{"name": "tls-mcp", "version": "1.0.0"},
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
				t.Fatalf("unexpected MCP method %q", message["method"])
			}
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	client, err := mcpclient.Open(config.Source{
		Type: "mcp",
		Transport: &config.MCPTransport{
			Type: "streamable-http",
			URL:  server.URL + "/mcp",
		},
		OAuth: &config.OAuthConfig{
			Mode:     "clientCredentials",
			TokenURL: server.URL + "/oauth/token",
			ClientID: &config.SecretRef{Type: "env", Value: "MCP_TLS_CLIENT_ID"},
			ClientSecret: &config.SecretRef{
				Type:  "env",
				Value: "MCP_TLS_CLIENT_SECRET",
			},
		},
	}, nil, config.PolicyConfig{}, t.TempDir(), server.Client(), context.Background())
	if err != nil {
		t.Fatalf("Open returned error: %v", err)
	}
	defer client.Close()

	if _, err := client.ListTools(context.Background()); err != nil {
		t.Fatalf("ListTools returned error: %v", err)
	}
	if tokenCalls != 1 {
		t.Fatalf("expected one oauth token request via provided client, got %d", tokenCalls)
	}
}

func TestOpenStreamableHTTPSupportsTransportOAuthIssuerDiscovery(t *testing.T) {
	if err := os.Setenv("MCP_ISSUER_CLIENT_ID", "issuer-client"); err != nil {
		t.Fatalf("set env: %v", err)
	}
	if err := os.Setenv("MCP_ISSUER_CLIENT_SECRET", "issuer-secret"); err != nil {
		t.Fatalf("set env: %v", err)
	}
	t.Cleanup(func() {
		_ = os.Unsetenv("MCP_ISSUER_CLIENT_ID")
		_ = os.Unsetenv("MCP_ISSUER_CLIENT_SECRET")
	})

	var tokenCalls int
	var server *httptest.Server
	server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/issuer/.well-known/openid-configuration":
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]any{
				"token_endpoint": server.URL + "/oauth/token",
			})
		case "/oauth/token":
			tokenCalls++
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]any{
				"access_token": "issuer-token",
				"token_type":   "Bearer",
				"expires_in":   3600,
			})
		case "/mcp":
			if r.Method == http.MethodDelete {
				w.WriteHeader(http.StatusNoContent)
				return
			}
			if got := r.Header.Get("Authorization"); got != "Bearer issuer-token" {
				t.Fatalf("expected oauth header, got %q", got)
			}
			var message map[string]any
			if err := json.NewDecoder(r.Body).Decode(&message); err != nil {
				t.Fatalf("decode request body: %v", err)
			}
			switch message["method"] {
			case "initialize":
				w.Header().Set("Content-Type", "application/json")
				w.Header().Set("Mcp-Session-Id", "issuer-session")
				_ = json.NewEncoder(w).Encode(map[string]any{
					"jsonrpc": "2.0",
					"id":      message["id"],
					"result": map[string]any{
						"protocolVersion": "2025-03-26",
						"capabilities":    map[string]any{"tools": map[string]any{}},
						"serverInfo":      map[string]any{"name": "issuer-mcp", "version": "1.0.0"},
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
				t.Fatalf("unexpected MCP method %q", message["method"])
			}
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	client, err := mcpclient.Open(config.Source{
		Type: "mcp",
		Transport: &config.MCPTransport{
			Type: "streamable-http",
			URL:  server.URL + "/mcp",
		},
		OAuth: &config.OAuthConfig{
			Mode:         "clientCredentials",
			Issuer:       server.URL + "/issuer",
			ClientID:     &config.SecretRef{Type: "env", Value: "MCP_ISSUER_CLIENT_ID"},
			ClientSecret: &config.SecretRef{Type: "env", Value: "MCP_ISSUER_CLIENT_SECRET"},
		},
	}, nil, config.PolicyConfig{}, t.TempDir(), server.Client(), context.Background())
	if err != nil {
		t.Fatalf("Open returned error: %v", err)
	}
	defer client.Close()

	if _, err := client.ListTools(context.Background()); err != nil {
		t.Fatalf("ListTools returned error: %v", err)
	}
	if tokenCalls != 1 {
		t.Fatalf("expected issuer discovery to resolve exactly one token request, got %d", tokenCalls)
	}
}
