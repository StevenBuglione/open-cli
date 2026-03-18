package openapi

import (
	"testing"

	mcpclient "github.com/StevenBuglione/open-cli/pkg/mcp/client"
)

func TestBuildDocumentResultPreservesAllAndFilteredMCPToolRefs(t *testing.T) {
	result, err := BuildDocumentResult(
		"filesystem",
		"filesystemSource",
		"streamable-http",
		[]mcpclient.ToolDescriptor{
			{Name: "list_files"},
			{Name: "delete_file"},
		},
		[]string{"delete_file"},
	)
	if err != nil {
		t.Fatalf("BuildDocumentResult returned error: %v", err)
	}
	if len(result.AllOperations) != 2 {
		t.Fatalf("expected 2 total MCP operation refs, got %d", len(result.AllOperations))
	}
	if len(result.FilteredOperations) != 1 {
		t.Fatalf("expected 1 surviving MCP operation ref, got %d", len(result.FilteredOperations))
	}
	if result.FilteredOperations[0].ToolName != "list_files" {
		t.Fatalf("expected surviving tool list_files, got %#v", result.FilteredOperations[0])
	}
}

func TestBuildDocumentResultIgnoresDisabledToolWithInvalidSchema(t *testing.T) {
	result, err := BuildDocumentResult(
		"filesystem",
		"filesystemSource",
		"streamable-http",
		[]mcpclient.ToolDescriptor{
			{Name: "list_files"},
			{Name: "delete_file", InputSchema: map[string]any{"type": 7}},
		},
		[]string{"delete_file"},
	)
	if err != nil {
		t.Fatalf("BuildDocumentResult returned error: %v", err)
	}
	if len(result.FilteredOperations) != 1 || result.FilteredOperations[0].ToolName != "list_files" {
		t.Fatalf("expected only surviving valid tool, got %#v", result.FilteredOperations)
	}
}

func TestBuildDocumentResultDoesNotLetDisabledToolClaimSurvivingPath(t *testing.T) {
	result, err := BuildDocumentResult(
		"filesystem",
		"filesystemSource",
		"streamable-http",
		[]mcpclient.ToolDescriptor{
			{Name: "delete-file"},
			{Name: "delete_file"},
		},
		[]string{"delete-file"},
	)
	if err != nil {
		t.Fatalf("BuildDocumentResult returned error: %v", err)
	}
	if len(result.FilteredOperations) != 1 {
		t.Fatalf("expected exactly one surviving tool, got %#v", result.FilteredOperations)
	}
	if got := result.FilteredOperations[0].OperationID; got != "delete_file" {
		t.Fatalf("expected surviving tool to keep natural operationId, got %q", got)
	}
	if got := result.FilteredOperations[0].Path; got != "/_mcp/filesystem/delete-file" {
		t.Fatalf("expected surviving tool to keep natural path, got %q", got)
	}
	if got := result.Document.Paths.Value("/_mcp/filesystem/delete-file").Post.OperationID; got != "delete_file" {
		t.Fatalf("expected generated OpenAPI operationId to stay natural, got %q", got)
	}
}
