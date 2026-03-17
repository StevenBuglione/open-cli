package tests_test

import (
	"context"
	"fmt"
	"path/filepath"
	goruntime "runtime"
	"testing"
	"time"

	"github.com/StevenBuglione/oas-cli-go/pkg/config"
	mcpclient "github.com/StevenBuglione/oas-cli-go/pkg/mcp/client"
	helpers "github.com/StevenBuglione/oas-cli-go/product-tests/tests/helpers"
)

func TestCampaignMCPRemoteMatrixLaneUsesHonestAuthPattern(t *testing.T) {
	t.Parallel()

	matrix, err := helpers.LoadCapabilityMatrix(filepath.Join(repoRootForMCPRemoteMatrixTest(t), "product-tests", "testdata", "fleet", "capability-matrix.yaml"))
	if err != nil {
		t.Fatalf("load capability matrix: %v", err)
	}

	for _, lane := range matrix.Lanes {
		if lane.ID != "mcp-remote-core" {
			continue
		}
		if lane.AuthPattern != "none" {
			t.Fatalf("mcp-remote-core authPattern = %q, want %q until transport OAuth is actually proved", lane.AuthPattern, "none")
		}
		return
	}

	t.Fatal("mcp-remote-core lane missing from capability matrix")
}

func TestCampaignMCPRemoteMatrix(t *testing.T) {
	fr := helpers.NewFindingsRecorder("mcp-remote-matrix")
	fr.SetLaneMetadata("product-validation", "mcp-remote", "ci-containerized", "none")
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
			fr.CheckBool("catalog-open", "unauthenticated remote MCP connection opens successfully", false, err.Error())
			t.Fail()
			return
		}
		defer c.Close()

		tools, err := c.ListTools(ctx)
		if err != nil {
			fr.CheckBool("catalog-list-tools", "unauthenticated remote MCP tools can be listed", false, err.Error())
			t.Fail()
			return
		}
		fr.Check("catalog-non-empty", "unauthenticated remote MCP catalog exposes tools", ">0", fmt.Sprintf("%d", len(tools)), len(tools) > 0, "")
	})

	t.Run("tool-execution", func(t *testing.T) {
		c, err := mcpclient.Open(source, nil, config.PolicyConfig{}, t.TempDir(), nil, ctx)
		if err != nil {
			fr.CheckBool("tool-open", "unauthenticated remote MCP execution connection opens successfully", false, err.Error())
			t.Fail()
			return
		}
		defer c.Close()

		result, err := c.CallTool(ctx, "echo", map[string]any{"message": "hello from matrix"})
		if err != nil {
			fr.CheckBool("tool-call", "unauthenticated remote MCP tool call returns successfully", false, err.Error())
			t.Fail()
			return
		}
		fr.CheckBool("tool-execution-success", "unauthenticated remote MCP tool execution succeeds", !result.IsError, fmt.Sprintf("content items=%d", len(result.Content)))
	})
}

func repoRootForMCPRemoteMatrixTest(t *testing.T) string {
	t.Helper()

	_, file, _, ok := goruntime.Caller(0)
	if !ok {
		t.Fatal("resolve source location")
	}
	return filepath.Clean(filepath.Join(filepath.Dir(file), "..", ".."))
}
