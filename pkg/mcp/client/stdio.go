package client

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os/exec"
	"strings"
	"sync"

	"github.com/StevenBuglione/oas-cli-go/pkg/config"
)

type stdioClient struct {
	cmd    *exec.Cmd
	stdin  io.WriteCloser
	stdout *bufio.Reader

	mu          sync.Mutex
	nextID      int64
	initialized bool
}

type rpcRequest struct {
	JSONRPC string `json:"jsonrpc"`
	ID      int64  `json:"id,omitempty"`
	Method  string `json:"method"`
	Params  any    `json:"params,omitempty"`
}

type rpcResponse struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      int64           `json:"id,omitempty"`
	Result  json.RawMessage `json:"result,omitempty"`
	Error   *rpcError       `json:"error,omitempty"`
}

type rpcError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

func Open(source config.Source, secrets map[string]config.Secret, policy config.PolicyConfig, stateDir string, httpClient *http.Client, ctx context.Context) (Client, error) {
	if source.Transport == nil {
		return nil, fmt.Errorf("mcp source requires transport configuration")
	}
	if httpClient == nil {
		httpClient = http.DefaultClient
	}

	switch source.Transport.Type {
	case "stdio":
		return openStdio(source, ctx)
	case "sse":
		return openSSE(source, secrets, policy, stateDir, httpClient, ctx)
	case "streamable-http":
		return openStreamableHTTP(source, secrets, policy, stateDir, httpClient, ctx)
	default:
		return nil, fmt.Errorf("mcp transport %q not implemented", source.Transport.Type)
	}
}

func openStdio(source config.Source, ctx context.Context) (Client, error) {
	transport := source.Transport
	cmd := exec.CommandContext(ctx, transport.Command, transport.Args...)
	if len(transport.Env) > 0 {
		cmd.Env = append(cmd.Env, envPairs(transport.Env)...)
	}

	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, err
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, err
	}
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Start(); err != nil {
		return nil, err
	}

	client := &stdioClient{
		cmd:    cmd,
		stdin:  stdin,
		stdout: bufio.NewReader(stdout),
		nextID: 1,
	}
	if err := client.ensureInitialized(); err != nil {
		_ = client.Close()
		if stderr.Len() > 0 {
			return nil, fmt.Errorf("%w: %s", err, strings.TrimSpace(stderr.String()))
		}
		return nil, err
	}
	return client, nil
}

func (client *stdioClient) ListTools(context.Context) ([]ToolDescriptor, error) {
	client.mu.Lock()
	defer client.mu.Unlock()

	var result struct {
		Tools []ToolDescriptor `json:"tools"`
	}
	if err := client.request("tools/list", nil, &result); err != nil {
		return nil, err
	}
	return result.Tools, nil
}

func (client *stdioClient) CallTool(_ context.Context, name string, args any) (ToolResult, error) {
	client.mu.Lock()
	defer client.mu.Unlock()

	var result ToolResult
	if err := client.request("tools/call", map[string]any{
		"name":      name,
		"arguments": args,
	}, &result); err != nil {
		return ToolResult{}, err
	}
	return result, nil
}

func (client *stdioClient) Close() error {
	client.mu.Lock()
	defer client.mu.Unlock()

	if client.stdin != nil {
		_ = client.stdin.Close()
		client.stdin = nil
	}
	if client.cmd == nil || client.cmd.Process == nil {
		return nil
	}
	return client.cmd.Wait()
}

func (client *stdioClient) ensureInitialized() error {
	client.mu.Lock()
	defer client.mu.Unlock()

	if client.initialized {
		return nil
	}

	var result map[string]any
	if err := client.request("initialize", map[string]any{
		"protocolVersion": "2025-03-26",
		"capabilities":    map[string]any{},
		"clientInfo": map[string]any{
			"name":    "oas-cli-go",
			"version": "1.0.0",
		},
	}, &result); err != nil {
		return err
	}
	if err := client.notify("notifications/initialized", map[string]any{}); err != nil {
		return err
	}
	client.initialized = true
	return nil
}

func (client *stdioClient) request(method string, params any, target any) error {
	id := client.nextID
	client.nextID++
	if err := client.writeMessage(rpcRequest{
		JSONRPC: "2.0",
		ID:      id,
		Method:  method,
		Params:  params,
	}); err != nil {
		return err
	}
	response, err := client.readResponse()
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

func (client *stdioClient) notify(method string, params any) error {
	return client.writeMessage(rpcRequest{
		JSONRPC: "2.0",
		Method:  method,
		Params:  params,
	})
}

func (client *stdioClient) writeMessage(message any) error {
	data, err := json.Marshal(message)
	if err != nil {
		return err
	}
	_, err = client.stdin.Write(append(data, '\n'))
	return err
}

func (client *stdioClient) readResponse() (*rpcResponse, error) {
	line, err := client.stdout.ReadString('\n')
	if err != nil {
		return nil, err
	}
	payload := []byte(strings.TrimSpace(line))
	var response rpcResponse
	if err := json.Unmarshal(payload, &response); err != nil {
		return nil, err
	}
	return &response, nil
}

func envPairs(values map[string]string) []string {
	pairs := make([]string, 0, len(values))
	for key, value := range values {
		pairs = append(pairs, key+"="+value)
	}
	return pairs
}
