package client

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"

	"github.com/StevenBuglione/oas-cli-go/pkg/config"
)

type streamableHTTPClient struct {
	httpClient  *http.Client
	endpointURL string
	headers     http.Header

	mu          sync.Mutex
	nextID      int64
	initialized bool
	sessionID   string
}

type sseClient struct {
	httpClient   *http.Client
	connectURL   string
	headers      http.Header
	messageURL   string
	streamBody   io.ReadCloser
	streamReader *bufio.Reader

	mu          sync.Mutex
	nextID      int64
	initialized bool
}

type sseEvent struct {
	Event string
	Data  string
}

func openStreamableHTTP(source config.Source, secrets map[string]config.Secret, policy config.PolicyConfig, stateDir string, httpClient *http.Client, ctx context.Context) (Client, error) {
	headers, err := resolveTransportHeaders(ctx, source, secrets, policy, stateDir, httpClient)
	if err != nil {
		return nil, err
	}
	if httpClient == nil {
		httpClient = http.DefaultClient
	}
	client := &streamableHTTPClient{
		httpClient:  httpClient,
		endpointURL: source.Transport.URL,
		headers:     headerMap(headers),
		nextID:      1,
	}
	if err := client.ensureInitialized(ctx); err != nil {
		return nil, err
	}
	return client, nil
}

func openSSE(source config.Source, secrets map[string]config.Secret, policy config.PolicyConfig, stateDir string, httpClient *http.Client, ctx context.Context) (Client, error) {
	headers, err := resolveTransportHeaders(ctx, source, secrets, policy, stateDir, httpClient)
	if err != nil {
		return nil, err
	}
	if httpClient == nil {
		httpClient = http.DefaultClient
	}
	client := &sseClient{
		httpClient: httpClient,
		connectURL: source.Transport.URL,
		headers:    headerMap(headers),
		nextID:     1,
	}
	if err := client.ensureConnected(ctx); err != nil {
		return nil, err
	}
	if err := client.ensureInitialized(ctx); err != nil {
		_ = client.Close()
		return nil, err
	}
	return client, nil
}

func (client *streamableHTTPClient) ListTools(ctx context.Context) ([]ToolDescriptor, error) {
	client.mu.Lock()
	defer client.mu.Unlock()

	var result struct {
		Tools []ToolDescriptor `json:"tools"`
	}
	if err := client.request(ctx, "tools/list", nil, &result, true); err != nil {
		return nil, err
	}
	return result.Tools, nil
}

func (client *streamableHTTPClient) CallTool(ctx context.Context, name string, args any) (ToolResult, error) {
	client.mu.Lock()
	defer client.mu.Unlock()

	var result ToolResult
	if err := client.request(ctx, "tools/call", map[string]any{
		"name":      name,
		"arguments": args,
	}, &result, true); err != nil {
		return ToolResult{}, err
	}
	return result, nil
}

func (client *streamableHTTPClient) Close() error {
	client.mu.Lock()
	defer client.mu.Unlock()
	if client.sessionID == "" {
		return nil
	}
	req, err := http.NewRequest(http.MethodDelete, client.endpointURL, nil)
	if err != nil {
		return err
	}
	req.Header = client.headers.Clone()
	req.Header.Set("Mcp-Session-Id", client.sessionID)
	resp, err := client.httpClient.Do(req)
	if resp != nil && resp.Body != nil {
		resp.Body.Close()
	}
	return err
}

func (client *streamableHTTPClient) ensureInitialized(ctx context.Context) error {
	if client.initialized {
		return nil
	}
	var result map[string]any
	if err := client.request(ctx, "initialize", map[string]any{
		"protocolVersion": "2025-03-26",
		"capabilities":    map[string]any{},
		"clientInfo": map[string]any{
			"name":    "oas-cli-go",
			"version": "1.0.0",
		},
	}, &result, false); err != nil {
		return err
	}
	if err := client.notify(ctx, "notifications/initialized", map[string]any{}); err != nil {
		return err
	}
	client.initialized = true
	return nil
}

func (client *streamableHTTPClient) notify(ctx context.Context, method string, params any) error {
	payload := rpcRequest{JSONRPC: "2.0", Method: method, Params: params}
	resp, err := client.doPOST(ctx, payload, true)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusAccepted && resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("%s: unexpected status %d: %s", method, resp.StatusCode, strings.TrimSpace(string(body)))
	}
	return nil
}

func (client *streamableHTTPClient) request(ctx context.Context, method string, params any, target any, allowReinit bool) error {
	id := client.nextID
	client.nextID++
	payload := rpcRequest{JSONRPC: "2.0", ID: id, Method: method, Params: params}

	response, err := client.doRequest(ctx, payload, allowReinit)
	if err != nil {
		return err
	}
	if response.Error != nil {
		return fmt.Errorf("%s: %s", method, response.Error.Message)
	}
	if target == nil {
		return nil
	}
	return json.Unmarshal(response.Result, target)
}

func (client *streamableHTTPClient) doRequest(ctx context.Context, payload rpcRequest, allowReinit bool) (*rpcResponse, error) {
	resp, err := client.doPOST(ctx, payload, true)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNotFound && allowReinit && client.sessionID != "" {
		client.sessionID = ""
		client.initialized = false
		if err := client.ensureInitialized(ctx); err != nil {
			return nil, err
		}
		return client.doRequest(ctx, payload, false)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("%s: unexpected status %d: %s", payload.Method, resp.StatusCode, strings.TrimSpace(string(body)))
	}
	if sessionID := resp.Header.Get("Mcp-Session-Id"); sessionID != "" {
		client.sessionID = sessionID
	}
	return readHTTPRPCResponse(resp, payload.ID)
}

func (client *streamableHTTPClient) doPOST(ctx context.Context, payload rpcRequest, withAccept bool) (*http.Response, error) {
	data, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, client.endpointURL, strings.NewReader(string(data)))
	if err != nil {
		return nil, err
	}
	req.Header = client.headers.Clone()
	req.Header.Set("Content-Type", "application/json")
	if withAccept {
		req.Header.Set("Accept", "application/json, text/event-stream")
	}
	if client.sessionID != "" {
		req.Header.Set("Mcp-Session-Id", client.sessionID)
	}
	return client.httpClient.Do(req)
}

func (client *sseClient) ListTools(ctx context.Context) ([]ToolDescriptor, error) {
	client.mu.Lock()
	defer client.mu.Unlock()

	var result struct {
		Tools []ToolDescriptor `json:"tools"`
	}
	if err := client.request(ctx, "tools/list", nil, &result, true); err != nil {
		return nil, err
	}
	return result.Tools, nil
}

func (client *sseClient) CallTool(ctx context.Context, name string, args any) (ToolResult, error) {
	client.mu.Lock()
	defer client.mu.Unlock()

	var result ToolResult
	if err := client.request(ctx, "tools/call", map[string]any{
		"name":      name,
		"arguments": args,
	}, &result, false); err != nil {
		return ToolResult{}, err
	}
	return result, nil
}

func (client *sseClient) Close() error {
	if client.streamBody != nil {
		err := client.streamBody.Close()
		client.streamBody = nil
		client.streamReader = nil
		return err
	}
	return nil
}

func (client *sseClient) ensureInitialized(ctx context.Context) error {
	if client.initialized {
		return nil
	}
	var result map[string]any
	if err := client.request(ctx, "initialize", map[string]any{
		"protocolVersion": "2024-11-05",
		"capabilities":    map[string]any{},
		"clientInfo": map[string]any{
			"name":    "oas-cli-go",
			"version": "1.0.0",
		},
	}, &result, true); err != nil {
		return err
	}
	if err := client.notify(ctx, "notifications/initialized", map[string]any{}); err != nil {
		return err
	}
	client.initialized = true
	return nil
}

func (client *sseClient) ensureConnected(ctx context.Context) error {
	if client.streamReader != nil && client.messageURL != "" {
		return nil
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, client.connectURL, nil)
	if err != nil {
		return err
	}
	req.Header = client.headers.Clone()
	req.Header.Set("Accept", "text/event-stream")
	resp, err := client.httpClient.Do(req)
	if err != nil {
		return err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		body, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		return fmt.Errorf("sse connect: unexpected status %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}

	client.streamBody = resp.Body
	client.streamReader = bufio.NewReader(resp.Body)
	for {
		event, err := readSSEEvent(client.streamReader)
		if err != nil {
			return err
		}
		if event.Event != "endpoint" {
			continue
		}
		messageURL, err := resolveEndpointURL(client.connectURL, strings.TrimSpace(event.Data))
		if err != nil {
			return err
		}
		client.messageURL = messageURL
		return nil
	}
}

func (client *sseClient) notify(ctx context.Context, method string, params any) error {
	payload := rpcRequest{JSONRPC: "2.0", Method: method, Params: params}
	resp, err := client.doPOST(ctx, payload)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusAccepted && resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("%s: unexpected status %d: %s", method, resp.StatusCode, strings.TrimSpace(string(body)))
	}
	return nil
}

func (client *sseClient) request(ctx context.Context, method string, params any, target any, allowReplay bool) error {
	if err := client.ensureConnected(ctx); err != nil {
		return err
	}

	id := client.nextID
	client.nextID++
	payload := rpcRequest{JSONRPC: "2.0", ID: id, Method: method, Params: params}

	for attempt := 0; attempt < 2; attempt++ {
		resp, err := client.doPOST(ctx, payload)
		if err != nil {
			return err
		}
		resp.Body.Close()

		response, err := client.waitForResponse(id)
		if err == nil {
			if response.Error != nil {
				return fmt.Errorf("%s: %s", method, response.Error.Message)
			}
			if target == nil {
				return nil
			}
			return json.Unmarshal(response.Result, target)
		}
		if !allowReplay || attempt == 1 {
			return err
		}
		_ = client.Close()
		client.messageURL = ""
		if err := client.ensureConnected(ctx); err != nil {
			return err
		}
	}
	return fmt.Errorf("%s: response not received", method)
}

func (client *sseClient) doPOST(ctx context.Context, payload rpcRequest) (*http.Response, error) {
	data, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, client.messageURL, strings.NewReader(string(data)))
	if err != nil {
		return nil, err
	}
	req.Header = client.headers.Clone()
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	return client.httpClient.Do(req)
}

func (client *sseClient) waitForResponse(id int64) (*rpcResponse, error) {
	for {
		event, err := readSSEEvent(client.streamReader)
		if err != nil {
			return nil, err
		}
		switch event.Event {
		case "endpoint":
			messageURL, err := resolveEndpointURL(client.connectURL, strings.TrimSpace(event.Data))
			if err != nil {
				return nil, err
			}
			client.messageURL = messageURL
		case "", "message":
			var response rpcResponse
			if err := json.Unmarshal([]byte(event.Data), &response); err != nil {
				return nil, err
			}
			if response.ID == id {
				return &response, nil
			}
		}
	}
}

func readHTTPRPCResponse(resp *http.Response, id int64) (*rpcResponse, error) {
	contentType := resp.Header.Get("Content-Type")
	if strings.Contains(contentType, "text/event-stream") {
		defer resp.Body.Close()
		reader := bufio.NewReader(resp.Body)
		for {
			event, err := readSSEEvent(reader)
			if err != nil {
				return nil, err
			}
			if event.Event != "" && event.Event != "message" {
				continue
			}
			var response rpcResponse
			if err := json.Unmarshal([]byte(event.Data), &response); err != nil {
				return nil, err
			}
			if response.ID == id {
				return &response, nil
			}
		}
	}

	var response rpcResponse
	if err := json.NewDecoder(resp.Body).Decode(&response); err != nil {
		return nil, err
	}
	return &response, nil
}

func readSSEEvent(reader *bufio.Reader) (*sseEvent, error) {
	event := &sseEvent{}
	var dataLines []string
	for {
		line, err := reader.ReadString('\n')
		if err != nil {
			return nil, err
		}
		line = strings.TrimRight(line, "\r\n")
		if line == "" {
			event.Data = strings.Join(dataLines, "\n")
			return event, nil
		}
		if strings.HasPrefix(line, "event:") {
			event.Event = strings.TrimSpace(strings.TrimPrefix(line, "event:"))
			continue
		}
		if strings.HasPrefix(line, "data:") {
			dataLines = append(dataLines, strings.TrimSpace(strings.TrimPrefix(line, "data:")))
		}
	}
}

func resolveEndpointURL(baseValue, endpoint string) (string, error) {
	baseURL, err := url.Parse(baseValue)
	if err != nil {
		return "", err
	}
	endpointURL, err := url.Parse(endpoint)
	if err != nil {
		return "", err
	}
	return baseURL.ResolveReference(endpointURL).String(), nil
}

func headerMap(values map[string]string) http.Header {
	headers := http.Header{}
	for key, value := range values {
		headers.Set(key, value)
	}
	return headers
}
