package exec

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/StevenBuglione/open-cli/pkg/catalog"
	"github.com/StevenBuglione/open-cli/pkg/config"
	mcpclient "github.com/StevenBuglione/open-cli/pkg/mcp/client"
)

type MCPRequest struct {
	Tool              catalog.Tool
	Source            config.Source
	Secrets           map[string]config.Secret
	Policy            config.PolicyConfig
	StateDir          string
	HTTPClient        *http.Client
	Body              []byte
	ProcessSupervisor interface {
		Track(pid int) error
		Release(pid int) error
	}
}

func ExecuteMCP(ctx context.Context, request MCPRequest) (*Result, error) {
	args, err := decodeMCPArguments(request.Tool, request.Body)
	if err != nil {
		return nil, err
	}
	client, err := mcpclient.Open(request.Source, request.Secrets, request.Policy, request.StateDir, request.HTTPClient, ctx)
	if err != nil {
		return nil, err
	}
	if reporter, ok := client.(mcpclient.ProcessReporter); ok && request.ProcessSupervisor != nil {
		if pid := reporter.ProcessID(); pid > 0 {
			if err := request.ProcessSupervisor.Track(pid); err != nil {
				_ = client.Close()
				return nil, err
			}
			defer func() {
				_ = client.Close()
				_ = request.ProcessSupervisor.Release(pid)
			}()
		} else {
			defer client.Close()
		}
	} else {
		defer client.Close()
	}

	toolName := request.Tool.OperationID
	if request.Tool.Backend != nil && request.Tool.Backend.ToolName != "" {
		toolName = request.Tool.Backend.ToolName
	}
	result, err := client.CallTool(ctx, toolName, args)
	if err != nil {
		return nil, err
	}

	body, err := json.Marshal(result)
	if err != nil {
		return nil, err
	}

	return &Result{
		StatusCode: http.StatusOK,
		Headers:    http.Header{"Content-Type": []string{"application/json"}},
		Body:       body,
	}, nil
}

func decodeMCPArguments(tool catalog.Tool, body []byte) (any, error) {
	if len(body) == 0 {
		return map[string]any{}, nil
	}

	var args any
	if err := json.Unmarshal(body, &args); err != nil {
		return nil, err
	}
	if shouldUnwrapMCPInput(tool, args) {
		objectArgs, ok := args.(map[string]any)
		if !ok {
			return nil, fmt.Errorf("wrapped MCP input must be an object containing input")
		}
		input, ok := objectArgs["input"]
		if !ok {
			return nil, fmt.Errorf("wrapped MCP input must include input")
		}
		return input, nil
	}
	return args, nil
}

func shouldUnwrapMCPInput(tool catalog.Tool, args any) bool {
	if tool.Backend != nil {
		return tool.Backend.InputWrapped
	}
	objectArgs, ok := args.(map[string]any)
	if !ok {
		return false
	}
	if len(objectArgs) != 1 {
		return false
	}
	if _, ok := objectArgs["input"]; !ok {
		return false
	}
	if tool.RequestBody == nil || len(tool.RequestBody.ContentTypes) == 0 {
		return false
	}
	schema := tool.RequestBody.ContentTypes[0].Schema
	if schema == nil {
		return false
	}
	if schemaType, _ := schema["type"].(string); schemaType != "object" {
		return false
	}
	properties, ok := schema["properties"].(map[string]any)
	if !ok || len(properties) != 1 {
		return false
	}
	if _, ok := properties["input"]; !ok {
		return false
	}
	required, ok := schema["required"].([]any)
	return ok && len(required) == 1 && required[0] == "input"
}
