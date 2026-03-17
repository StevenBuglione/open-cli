package tests_test

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/StevenBuglione/oas-cli-go/pkg/config"
	mcpclient "github.com/StevenBuglione/oas-cli-go/pkg/mcp/client"
	helpers "github.com/StevenBuglione/oas-cli-go/product-tests/tests/helpers"
)

func TestCampaignMCPRemoteMatrix(t *testing.T) {
	fr := helpers.NewFindingsRecorder("mcp-remote-matrix")
	fr.SetLaneMetadata("product-validation", "mcp-remote", "ci-containerized", "transport-oauth")
	defer fr.MustEmitToTest(t)

	addr := mcpRemoteAddr()
	if !isTCPOpen(addr) {
		t.Skipf("remote MCP service unreachable at %s", addr)
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

	t.Run("catalog-discovery", func(t *testing.T) {
		c, err := mcpclient.Open(source, nil, config.PolicyConfig{}, t.TempDir(), nil, ctx)
		if err != nil {
			fr.CheckBool("catalog-open", "remote MCP connection opens successfully", false, err.Error())
			t.Fail()
			return
		}
		defer c.Close()

		tools, err := c.ListTools(ctx)
		if err != nil {
			fr.CheckBool("catalog-list-tools", "remote MCP tools can be listed", false, err.Error())
			t.Fail()
			return
		}
		fr.Check("catalog-non-empty", "remote MCP catalog exposes tools", ">0", fmt.Sprintf("%d", len(tools)), len(tools) > 0, "")
	})

	t.Run("tool-execution", func(t *testing.T) {
		c, err := mcpclient.Open(source, nil, config.PolicyConfig{}, t.TempDir(), nil, ctx)
		if err != nil {
			fr.CheckBool("tool-open", "remote MCP execution connection opens successfully", false, err.Error())
			t.Fail()
			return
		}
		defer c.Close()

		result, err := c.CallTool(ctx, "echo", map[string]any{"message": "hello from matrix"})
		if err != nil {
			fr.CheckBool("tool-call", "remote MCP tool call returns successfully", false, err.Error())
			t.Fail()
			return
		}
		fr.CheckBool("tool-execution-success", "remote MCP tool execution succeeds", !result.IsError, fmt.Sprintf("content items=%d", len(result.Content)))
	})
}
