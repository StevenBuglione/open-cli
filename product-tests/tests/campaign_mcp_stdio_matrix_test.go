package tests_test

import (
	"context"
	"fmt"
	"os/exec"
	"strings"
	"testing"
	"time"

	"github.com/StevenBuglione/open-cli/pkg/config"
	mcpclient "github.com/StevenBuglione/open-cli/pkg/mcp/client"
	helpers "github.com/StevenBuglione/open-cli/product-tests/tests/helpers"
)

func TestCampaignMCPStdioMatrix(t *testing.T) {
	fr := helpers.NewFindingsRecorder("mcp-stdio-matrix")
	fr.SetLaneMetadata("product-validation", "mcp-stdio", "ci-containerized", "none")
	defer fr.MustEmitToTest(t)

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

	t.Run("catalog-discovery", func(t *testing.T) {
		c, err := mcpclient.Open(source, nil, config.PolicyConfig{}, t.TempDir(), nil, ctx)
		if err != nil {
			fr.CheckBool("catalog-open", "stdio MCP connection opens successfully", false, err.Error())
			t.Fail()
			return
		}
		defer c.Close()

		tools, err := c.ListTools(ctx)
		if err != nil {
			fr.CheckBool("catalog-list-tools", "stdio MCP tools can be listed", false, err.Error())
			t.Fail()
			return
		}
		fr.Check("catalog-non-empty", "stdio MCP catalog exposes tools", ">0", fmt.Sprintf("%d", len(tools)), len(tools) > 0, "")

		var found bool
		names := make([]string, len(tools))
		for i, tool := range tools {
			names[i] = tool.Name
			if tool.Name == "list_directory" {
				found = true
			}
		}
		fr.CheckBool("catalog-has-list-directory", "filesystem MCP server exposes list_directory", found, strings.Join(names, ", "))
	})

	t.Run("tool-execution", func(t *testing.T) {
		c, err := mcpclient.Open(source, nil, config.PolicyConfig{}, t.TempDir(), nil, ctx)
		if err != nil {
			fr.CheckBool("tool-open", "stdio MCP execution connection opens successfully", false, err.Error())
			t.Fail()
			return
		}
		defer c.Close()

		result, err := c.CallTool(ctx, "list_directory", map[string]any{"path": "/tmp"})
		if err != nil {
			fr.CheckBool("tool-call", "stdio MCP tool call returns successfully", false, err.Error())
			t.Fail()
			return
		}
		fr.CheckBool("tool-execution-success", "stdio MCP tool execution succeeds", !result.IsError, fmt.Sprintf("content items=%d", len(result.Content)))
	})
}
