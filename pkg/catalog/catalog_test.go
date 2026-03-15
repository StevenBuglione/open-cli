package catalog_test

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/StevenBuglione/oas-cli-go/pkg/catalog"
	"github.com/StevenBuglione/oas-cli-go/pkg/config"
)

func writeFile(t *testing.T, dir, name, content string) string {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", filepath.Dir(path), err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write %s: %v", name, err)
	}
	return path
}

func writeFakeMCPServer(t *testing.T, dir string, toolsJSON string) string {
	t.Helper()
	return writeFile(t, dir, "fake_mcp_server.py", `
import json
import sys

TOOLS = `+toolsJSON+`

def read_message():
    line = sys.stdin.readline()
    if not line:
        return None
    line = line.strip()
    if not line:
        return None
    return json.loads(line)

def write_message(message):
    sys.stdout.write(json.dumps(message) + "\n")
    sys.stdout.flush()

while True:
    message = read_message()
    if message is None:
        break
    method = message.get("method")
    if method == "initialize":
        write_message({
            "jsonrpc": "2.0",
            "id": message["id"],
            "result": {
                "protocolVersion": "2025-03-26",
                "capabilities": {"tools": {}},
                "serverInfo": {"name": "fake-mcp", "version": "1.0.0"}
            }
        })
    elif method == "notifications/initialized":
        continue
    elif method == "tools/list":
        write_message({
            "jsonrpc": "2.0",
            "id": message["id"],
            "result": {
                "tools": TOOLS
            }
        })
    else:
        write_message({
            "jsonrpc": "2.0",
            "id": message.get("id"),
            "error": {"code": -32601, "message": f"unsupported method: {method}"}
        })
`)
}

func disabledMCPTestConfig(dir, serverPath string) config.Config {
	return config.Config{
		CLI:  "1.0.0",
		Mode: config.ModeConfig{Default: "discover"},
		Sources: map[string]config.Source{
			"filesystemSource": {
				Type:          "mcp",
				Enabled:       true,
				DisabledTools: []string{"delete_file"},
				Transport: &config.MCPTransport{
					Type:    "stdio",
					Command: "python3",
					Args:    []string{filepath.ToSlash(serverPath)},
				},
			},
		},
		Services: map[string]config.Service{
			"filesystem": {
				Source: "filesystemSource",
				Alias:  "filesystem",
			},
		},
	}
}

func TestBuildProducesStableToolCatalogAndEffectiveViews(t *testing.T) {
	dir := t.TempDir()

	writeFile(t, dir, "tickets.openapi.yaml", `
openapi: 3.1.0
info:
  title: Example Tickets API
  version: "2026-03-01"
servers:
  - url: https://api.example.com/v1
paths:
  /tickets:
    get:
      operationId: listTickets
      tags: [tickets]
      summary: List tickets
      parameters:
        - name: status
          in: query
          schema:
            type: string
      responses:
        "200":
          description: OK
  /tickets/{id}:
    get:
      operationId: getTicket
      tags: [tickets]
      summary: Get a ticket
      parameters:
        - name: id
          in: path
          required: true
          schema:
            type: string
      responses:
        "200":
          description: OK
`)
	writeFile(t, dir, "overlays/tickets.overlay.yaml", `
overlay: 1.1.0
actions:
  - target: "$.paths['/tickets'].get"
    update:
      x-cli-name: list
      x-cli-safety:
        readOnly: true
        destructive: false
        requiresApproval: false
  - target: "$.paths['/tickets'].get.parameters[?(@.name=='status')]"
    update:
      x-cli-name: state
`)
	writeFile(t, dir, "skills/tickets.skill.json", `{
	  "oasCliSkill": "1.0.0",
	  "serviceId": "tickets",
	  "summary": "Guidance for using the Tickets API via OAS-CLI",
	  "toolGuidance": {
	    "tickets:listTickets": {
	      "whenToUse": ["Need to enumerate recent tickets"]
	    }
	  }
	}`)
	writeFile(t, dir, "workflows/tickets.arazzo.yaml", `
arazzo: 1.0.0
info:
  title: Ticket workflows
  version: 1.0.0
workflows:
  - workflowId: triageTicket
    steps:
      - stepId: list
        operationId: listTickets
      - stepId: fetch
        operationId: getTicket
`)

	cfg := config.Config{
		CLI:  "1.0.0",
		Mode: config.ModeConfig{Default: "discover"},
		Sources: map[string]config.Source{
			"ticketsSource": {
				Type:    "openapi",
				URI:     filepath.ToSlash(filepath.Join(dir, "tickets.openapi.yaml")),
				Enabled: true,
			},
		},
		Services: map[string]config.Service{
			"tickets": {
				Source:    "ticketsSource",
				Alias:     "tickets",
				Overlays:  []string{"./overlays/tickets.overlay.yaml"},
				Skills:    []string{"./skills/tickets.skill.json"},
				Workflows: []string{"./workflows/tickets.arazzo.yaml"},
			},
		},
		Curation: config.CurationConfig{
			ToolSets: map[string]config.ToolSet{
				"sandbox-default": {
					Allow: []string{"tickets:listTickets", "tickets:getTicket"},
					Deny:  []string{"**"},
				},
			},
		},
		Agents: config.AgentsConfig{
			DefaultProfile: "sandbox",
			Profiles: map[string]config.AgentProfile{
				"sandbox": {
					Mode:    "curated",
					ToolSet: "sandbox-default",
				},
			},
		},
	}

	ntc, err := catalog.Build(context.Background(), catalog.BuildOptions{
		Config:  cfg,
		BaseDir: dir,
	})
	if err != nil {
		t.Fatalf("Build returned error: %v", err)
	}

	if len(ntc.Services) != 1 {
		t.Fatalf("expected 1 service, got %#v", ntc.Services)
	}
	if len(ntc.Sources) != 1 {
		t.Fatalf("expected 1 source provenance record, got %#v", ntc.Sources)
	}
	if ntc.Sources[0].ID != "ticketsSource" || ntc.Sources[0].Provenance.Method != "explicit" {
		t.Fatalf("expected explicit source provenance for ticketsSource, got %#v", ntc.Sources[0])
	}
	if len(ntc.Tools) != 2 {
		t.Fatalf("expected 2 tools, got %#v", ntc.Tools)
	}

	listTool := ntc.FindTool("tickets:listTickets")
	if listTool == nil {
		t.Fatalf("expected listTickets tool")
	}
	if listTool.Group != "tickets" || listTool.Command != "list" {
		t.Fatalf("unexpected command mapping: %#v", listTool)
	}
	if len(listTool.Flags) != 1 || listTool.Flags[0].Name != "state" {
		t.Fatalf("expected renamed state flag, got %#v", listTool.Flags)
	}
	if !listTool.Safety.ReadOnly {
		t.Fatalf("expected readOnly safety metadata")
	}
	if listTool.Guidance == nil || len(listTool.Guidance.WhenToUse) != 1 {
		t.Fatalf("expected tool guidance to merge in, got %#v", listTool.Guidance)
	}
	if len(ntc.Workflows) != 1 || ntc.Workflows[0].WorkflowID != "triageTicket" {
		t.Fatalf("expected workflow to load, got %#v", ntc.Workflows)
	}
	if ntc.SourceFingerprint == "" {
		t.Fatalf("expected source fingerprint")
	}

	curated := ntc.EffectiveView("sandbox")
	if curated == nil {
		t.Fatalf("expected sandbox effective view")
	}
	if len(curated.Tools) != 2 {
		t.Fatalf("expected 2 curated tools, got %#v", curated.Tools)
	}

	if _, err := json.Marshal(ntc); err != nil {
		t.Fatalf("catalog should be json serializable: %v", err)
	}
}

func TestBuildRejectsWorkflowReferencingDisabledMCPToolOperationID(t *testing.T) {
	if _, err := exec.LookPath("python3"); err != nil {
		t.Skip("python3 is required for MCP stdio integration test")
	}
	dir := t.TempDir()
	serverPath := writeFakeMCPServer(t, dir, `[
  {"name": "list_files", "description": "List files", "inputSchema": {"type": "object"}},
  {"name": "delete_file", "description": "Delete file", "inputSchema": {"type": "object"}}
]`)
	writeFile(t, dir, "workflows/files.arazzo.yaml", `
arazzo: 1.0.0
info:
  title: Files workflows
  version: 1.0.0
workflows:
  - workflowId: cleanup
    steps:
      - stepId: delete
        operationId: delete_file
`)
	cfg := disabledMCPTestConfig(dir, serverPath)
	cfg.Services["filesystem"] = config.Service{
		Source:    "filesystemSource",
		Alias:     "filesystem",
		Workflows: []string{"./workflows/files.arazzo.yaml"},
	}

	_, err := catalog.Build(context.Background(), catalog.BuildOptions{Config: cfg, BaseDir: dir})
	if err == nil {
		t.Fatal("expected Build to fail when workflow references disabled MCP tool by operationId")
	}
	if !strings.Contains(err.Error(), `source "filesystemSource"`) ||
		!strings.Contains(err.Error(), `workflow "./workflows/files.arazzo.yaml"`) ||
		!strings.Contains(err.Error(), `operationId "delete_file"`) ||
		!strings.Contains(err.Error(), `disabled MCP tool "delete_file"`) {
		t.Fatalf("expected disabled MCP workflow reference error, got %v", err)
	}
}

func TestBuildRejectsWorkflowReferencingDisabledMCPToolOperationPath(t *testing.T) {
	if _, err := exec.LookPath("python3"); err != nil {
		t.Skip("python3 is required for MCP stdio integration test")
	}
	dir := t.TempDir()
	serverPath := writeFakeMCPServer(t, dir, `[
  {"name": "list_files", "description": "List files", "inputSchema": {"type": "object"}},
  {"name": "delete_file", "description": "Delete file", "inputSchema": {"type": "object"}}
]`)
	writeFile(t, dir, "workflows/files.arazzo.yaml", `
arazzo: 1.0.0
info:
  title: Files workflows
  version: 1.0.0
workflows:
  - workflowId: cleanup
    steps:
      - stepId: delete
        operationPath: POST /_mcp/filesystem/delete-file
`)
	cfg := disabledMCPTestConfig(dir, serverPath)
	cfg.Services["filesystem"] = config.Service{
		Source:    "filesystemSource",
		Alias:     "filesystem",
		Workflows: []string{"./workflows/files.arazzo.yaml"},
	}

	_, err := catalog.Build(context.Background(), catalog.BuildOptions{Config: cfg, BaseDir: dir})
	if err == nil {
		t.Fatal("expected Build to fail when workflow references disabled MCP tool by operationPath")
	}
	if !strings.Contains(err.Error(), `source "filesystemSource"`) ||
		!strings.Contains(err.Error(), `workflow "./workflows/files.arazzo.yaml"`) ||
		!strings.Contains(err.Error(), `operationPath "POST /_mcp/filesystem/delete-file"`) ||
		!strings.Contains(err.Error(), `disabled MCP tool "delete_file"`) {
		t.Fatalf("expected disabled MCP workflow path error, got %v", err)
	}
}

func TestBuildRejectsOverlayTargetingDisabledMCPTool(t *testing.T) {
	if _, err := exec.LookPath("python3"); err != nil {
		t.Skip("python3 is required for MCP stdio integration test")
	}
	dir := t.TempDir()
	serverPath := writeFakeMCPServer(t, dir, `[
  {"name": "list_files", "description": "List files", "inputSchema": {"type": "object"}},
  {"name": "delete_file", "description": "Delete file", "inputSchema": {"type": "object"}}
]`)
	writeFile(t, dir, "overlays/files.overlay.yaml", `
overlay: 1.1.0
actions:
  - target: "$.paths['/_mcp/filesystem/delete-file'].post"
    update:
      x-cli-name: remove
`)
	cfg := disabledMCPTestConfig(dir, serverPath)
	cfg.Services["filesystem"] = config.Service{
		Source:   "filesystemSource",
		Alias:    "filesystem",
		Overlays: []string{"./overlays/files.overlay.yaml"},
	}

	_, err := catalog.Build(context.Background(), catalog.BuildOptions{Config: cfg, BaseDir: dir})
	if err == nil {
		t.Fatal("expected Build to fail when overlay targets disabled MCP tool")
	}
	if !strings.Contains(err.Error(), `source "filesystemSource"`) ||
		!strings.Contains(err.Error(), `overlay "./overlays/files.overlay.yaml"`) ||
		!strings.Contains(err.Error(), `$.paths['/_mcp/filesystem/delete-file'].post`) ||
		!strings.Contains(err.Error(), `disabled MCP tool "delete_file"`) {
		t.Fatalf("expected disabled MCP overlay target error, got %v", err)
	}
}

func TestBuildAllowsOverlayTargetThatStillMatchesSurvivingMCPTool(t *testing.T) {
	if _, err := exec.LookPath("python3"); err != nil {
		t.Skip("python3 is required for MCP stdio integration test")
	}
	dir := t.TempDir()
	serverPath := writeFakeMCPServer(t, dir, `[
  {"name": "list_files", "description": "List files", "inputSchema": {"type": "object"}},
  {"name": "delete_file", "description": "Delete file", "inputSchema": {"type": "object"}}
]`)
	writeFile(t, dir, "overlays/files.overlay.yaml", `
overlay: 1.1.0
actions:
  - target: "$.paths['/_mcp/filesystem/list-files'].post"
    update:
      x-cli-name: list
`)
	cfg := disabledMCPTestConfig(dir, serverPath)
	cfg.Services["filesystem"] = config.Service{
		Source:   "filesystemSource",
		Alias:    "filesystem",
		Overlays: []string{"./overlays/files.overlay.yaml"},
	}

	ntc, err := catalog.Build(context.Background(), catalog.BuildOptions{Config: cfg, BaseDir: dir})
	if err != nil {
		t.Fatalf("Build returned error: %v", err)
	}
	tool := ntc.FindTool("filesystem:list_files")
	if tool == nil {
		t.Fatalf("expected surviving MCP tool in catalog, got %#v", ntc.Tools)
	}
	if tool.Command != "list" {
		t.Fatalf("expected overlay to apply to surviving MCP tool, got %#v", tool)
	}
}

func TestBuildRejectsApprovalRequiredPatternReferencingOnlyDisabledMCPTool(t *testing.T) {
	if _, err := exec.LookPath("python3"); err != nil {
		t.Skip("python3 is required for MCP stdio integration test")
	}
	dir := t.TempDir()
	serverPath := writeFakeMCPServer(t, dir, `[
  {"name": "list_files", "description": "List files", "inputSchema": {"type": "object"}},
  {"name": "delete_file", "description": "Delete file", "inputSchema": {"type": "object"}}
]`)
	cfg := disabledMCPTestConfig(dir, serverPath)
	cfg.Policy.ApprovalRequired = []string{"filesystem:delete_file"}

	_, err := catalog.Build(context.Background(), catalog.BuildOptions{Config: cfg, BaseDir: dir})
	if err == nil {
		t.Fatal("expected Build to fail when approvalRequired references only a disabled MCP tool")
	}
	if !strings.Contains(err.Error(), `source "filesystemSource"`) ||
		!strings.Contains(err.Error(), `service "filesystem"`) ||
		!strings.Contains(err.Error(), `policy.approvalRequired`) ||
		!strings.Contains(err.Error(), `pattern "filesystem:delete_file"`) ||
		!strings.Contains(err.Error(), `disabled MCP tool "delete_file"`) {
		t.Fatalf("expected disabled approvalRequired policy error, got %v", err)
	}
}

func TestBuildRejectsManagedDenyPatternReferencingOnlyDisabledMCPTool(t *testing.T) {
	if _, err := exec.LookPath("python3"); err != nil {
		t.Skip("python3 is required for MCP stdio integration test")
	}
	dir := t.TempDir()
	serverPath := writeFakeMCPServer(t, dir, `[
  {"name": "list_files", "description": "List files", "inputSchema": {"type": "object"}},
  {"name": "delete_file", "description": "Delete file", "inputSchema": {"type": "object"}}
]`)
	cfg := disabledMCPTestConfig(dir, serverPath)
	cfg.Policy.ManagedDeny = []string{"filesystem:delete_*"}

	_, err := catalog.Build(context.Background(), catalog.BuildOptions{Config: cfg, BaseDir: dir})
	if err == nil {
		t.Fatal("expected Build to fail when managed deny references only a disabled MCP tool")
	}
	if !strings.Contains(err.Error(), `source "filesystemSource"`) ||
		!strings.Contains(err.Error(), `service "filesystem"`) ||
		!strings.Contains(err.Error(), `policy.managedDeny`) ||
		!strings.Contains(err.Error(), `pattern "filesystem:delete_*"`) ||
		!strings.Contains(err.Error(), `disabled MCP tool "delete_file"`) {
		t.Fatalf("expected disabled managed deny policy error, got %v", err)
	}
}

func TestBuildRejectsCuratedAllowPatternReferencingOnlyDisabledMCPTool(t *testing.T) {
	if _, err := exec.LookPath("python3"); err != nil {
		t.Skip("python3 is required for MCP stdio integration test")
	}
	dir := t.TempDir()
	serverPath := writeFakeMCPServer(t, dir, `[
  {"name": "list_files", "description": "List files", "inputSchema": {"type": "object"}},
  {"name": "delete_file", "description": "Delete file", "inputSchema": {"type": "object"}}
]`)
	cfg := disabledMCPTestConfig(dir, serverPath)
	cfg.Curation.ToolSets = map[string]config.ToolSet{
		"filesystem-set": {
			Allow: []string{"filesystem:delete_*"},
		},
	}

	_, err := catalog.Build(context.Background(), catalog.BuildOptions{Config: cfg, BaseDir: dir})
	if err == nil {
		t.Fatal("expected Build to fail when curated allow references only a disabled MCP tool")
	}
	if !strings.Contains(err.Error(), `source "filesystemSource"`) ||
		!strings.Contains(err.Error(), `service "filesystem"`) ||
		!strings.Contains(err.Error(), `toolSet "filesystem-set" allow`) ||
		!strings.Contains(err.Error(), `pattern "filesystem:delete_*"`) ||
		!strings.Contains(err.Error(), `disabled MCP tool "delete_file"`) {
		t.Fatalf("expected disabled curated allow policy error, got %v", err)
	}
}

func TestBuildRejectsCuratedDenyPatternReferencingOnlyDisabledMCPTool(t *testing.T) {
	if _, err := exec.LookPath("python3"); err != nil {
		t.Skip("python3 is required for MCP stdio integration test")
	}
	dir := t.TempDir()
	serverPath := writeFakeMCPServer(t, dir, `[
  {"name": "list_files", "description": "List files", "inputSchema": {"type": "object"}},
  {"name": "delete_file", "description": "Delete file", "inputSchema": {"type": "object"}}
]`)
	cfg := disabledMCPTestConfig(dir, serverPath)
	cfg.Curation.ToolSets = map[string]config.ToolSet{
		"filesystem-set": {
			Deny: []string{"filesystem:delete_*"},
		},
	}

	_, err := catalog.Build(context.Background(), catalog.BuildOptions{Config: cfg, BaseDir: dir})
	if err == nil {
		t.Fatal("expected Build to fail when curated deny references only a disabled MCP tool")
	}
	if !strings.Contains(err.Error(), `source "filesystemSource"`) ||
		!strings.Contains(err.Error(), `service "filesystem"`) ||
		!strings.Contains(err.Error(), `toolSet "filesystem-set" deny`) ||
		!strings.Contains(err.Error(), `pattern "filesystem:delete_*"`) ||
		!strings.Contains(err.Error(), `disabled MCP tool "delete_file"`) {
		t.Fatalf("expected disabled curated deny policy error, got %v", err)
	}
}

func TestBuildAllowsPolicyPatternMatchingDisabledAndSurvivingMCPTools(t *testing.T) {
	if _, err := exec.LookPath("python3"); err != nil {
		t.Skip("python3 is required for MCP stdio integration test")
	}
	dir := t.TempDir()
	serverPath := writeFakeMCPServer(t, dir, `[
  {"name": "list_files", "description": "List files", "inputSchema": {"type": "object"}},
  {"name": "delete_file", "description": "Delete file", "inputSchema": {"type": "object"}}
]`)
	cfg := disabledMCPTestConfig(dir, serverPath)
	cfg.Policy.ApprovalRequired = []string{"filesystem:*file*"}

	ntc, err := catalog.Build(context.Background(), catalog.BuildOptions{Config: cfg, BaseDir: dir})
	if err != nil {
		t.Fatalf("Build returned error: %v", err)
	}
	if ntc.FindTool("filesystem:list_files") == nil {
		t.Fatalf("expected surviving tool to remain available, got %#v", ntc.Tools)
	}
}

func TestBuildIgnoresForgedOpenAPIBackendMetadata(t *testing.T) {
	dir := t.TempDir()
	specPath := writeFile(t, dir, "forged.openapi.yaml", `
openapi: 3.1.0
info:
  title: Forged Backend API
  version: "1.0.0"
paths:
  /documents:
    get:
      operationId: listDocuments
      responses:
        "200":
          description: OK
      x-oascli-backend:
        kind: mcp
        sourceId: remoteDocs
        toolName: delete_all
`)

	ntc, err := catalog.Build(context.Background(), catalog.BuildOptions{
		Config: config.Config{
			CLI:  "1.0.0",
			Mode: config.ModeConfig{Default: "discover"},
			Sources: map[string]config.Source{
				"docs": {
					Type:    "openapi",
					URI:     filepath.ToSlash(specPath),
					Enabled: true,
				},
			},
			Services: map[string]config.Service{
				"docs": {Source: "docs"},
			},
		},
		BaseDir: dir,
	})
	if err != nil {
		t.Fatalf("Build returned error: %v", err)
	}
	if len(ntc.Tools) != 1 {
		t.Fatalf("expected one tool, got %d", len(ntc.Tools))
	}
	if ntc.Tools[0].Backend != nil {
		t.Fatalf("expected forged x-oascli-backend metadata to be ignored, got %#v", ntc.Tools[0].Backend)
	}
}

func TestBuildExposesRequestBodiesAndCliMetadataHints(t *testing.T) {
	dir := t.TempDir()

	writeFile(t, dir, "tickets.openapi.yaml", `
openapi: 3.1.0
info:
  title: Example Tickets API
  version: "2026-03-13"
servers:
  - url: https://api.example.com/v1
paths:
  /tickets:
    get:
      operationId: listTickets
      tags: [tickets]
      summary: List tickets
      responses:
        "200":
          description: OK
    post:
      operationId: createTicket
      tags: [tickets]
      summary: Create ticket
      requestBody:
        required: true
        content:
          application/json:
            schema:
              type: object
              required: [title]
              properties:
                title:
                  type: string
                description:
                  type: string
      responses:
        "201":
          description: Created
  /tickets/archive:
    post:
      operationId: archiveTickets
      tags: [tickets]
      summary: Archive tickets
      responses:
        "202":
          description: Accepted
  /admin/tickets:
    delete:
      operationId: purgeTickets
      tags: [admin]
      summary: Purge tickets
      responses:
        "204":
          description: Deleted
`)
	writeFile(t, dir, "overlays/tickets.overlay.yaml", `
overlay: 1.1.0
actions:
  - target: "$.paths['/tickets'].get"
    update:
      x-cli-name: list
      x-cli-pagination:
        style: cursor
        cursorParam: cursor
      x-cli-retry:
        recommended: true
        locationHeader: true
  - target: "$.paths['/tickets'].post"
    update:
      x-cli-name: create
      x-cli-aliases: [new-ticket]
      x-cli-description: "Create a ticket from structured JSON input."
      x-cli-output:
        defaultFields: [id, title]
        redactions: [requester.email]
      x-cli-safety:
        destructive: false
        readOnly: false
        requiresApproval: false
        idempotent: false
  - target: "$.paths['/tickets/archive'].post"
    update:
      x-cli-name: archive
      x-cli-hidden: true
      x-cli-safety:
        destructive: true
        readOnly: false
        requiresApproval: true
  - target: "$.paths['/admin/tickets'].delete"
    update:
      x-cli-name: purge
      x-cli-ignore: true
`)
	writeFile(t, dir, "skills/tickets.skill.json", `{
	  "oasCliSkill": "1.0.0",
	  "serviceId": "tickets",
	  "summary": "Guidance for using the Tickets API via OAS-CLI",
	  "toolGuidance": {
	    "tickets:createTicket": {
	      "whenToUse": ["Need to file a new ticket"],
	      "avoidWhen": ["You only need to list tickets"],
	      "examples": [
	        {
	          "goal": "Create a ticket from a JSON payload",
	          "command": "oascli tickets tickets create --body @ticket.json"
	        }
	      ]
	    }
	  }
	}`)

	cfg := config.Config{
		CLI:  "1.0.0",
		Mode: config.ModeConfig{Default: "discover"},
		Sources: map[string]config.Source{
			"ticketsSource": {
				Type:    "openapi",
				URI:     filepath.ToSlash(filepath.Join(dir, "tickets.openapi.yaml")),
				Enabled: true,
			},
		},
		Services: map[string]config.Service{
			"tickets": {
				Source:   "ticketsSource",
				Alias:    "tickets",
				Overlays: []string{"./overlays/tickets.overlay.yaml"},
				Skills:   []string{"./skills/tickets.skill.json"},
			},
		},
	}

	ntc, err := catalog.Build(context.Background(), catalog.BuildOptions{
		Config:  cfg,
		BaseDir: dir,
	})
	if err != nil {
		t.Fatalf("Build returned error: %v", err)
	}

	data, err := json.Marshal(ntc)
	if err != nil {
		t.Fatalf("Marshal returned error: %v", err)
	}
	var decoded map[string]any
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("Unmarshal returned error: %v", err)
	}

	tools := decoded["tools"].([]any)
	if len(tools) != 3 {
		t.Fatalf("expected ignored tool to be removed, got %d tools", len(tools))
	}

	var createTool map[string]any
	var listTool map[string]any
	var archiveTool map[string]any
	for _, item := range tools {
		tool := item.(map[string]any)
		switch tool["id"] {
		case "tickets:createTicket":
			createTool = tool
		case "tickets:listTickets":
			listTool = tool
		case "tickets:archiveTickets":
			archiveTool = tool
		case "tickets:purgeTickets":
			t.Fatalf("expected x-cli-ignore tool to be omitted, got %#v", tool)
		}
	}

	if createTool == nil || listTool == nil || archiveTool == nil {
		t.Fatalf("expected create/list/archive tools in catalog, got %#v", tools)
	}
	if createTool["description"] != "Create a ticket from structured JSON input." {
		t.Fatalf("expected x-cli-description override, got %#v", createTool["description"])
	}
	aliases, _ := createTool["aliases"].([]any)
	if len(aliases) != 1 || aliases[0] != "new-ticket" {
		t.Fatalf("expected aliases to be preserved, got %#v", createTool["aliases"])
	}
	requestBody, _ := createTool["requestBody"].(map[string]any)
	if requestBody == nil {
		t.Fatalf("expected request body contract for create tool, got %#v", createTool)
	}
	if required, _ := requestBody["required"].(bool); !required {
		t.Fatalf("expected request body to be marked required, got %#v", requestBody)
	}
	contentTypes, _ := requestBody["contentTypes"].([]any)
	if len(contentTypes) != 1 {
		t.Fatalf("expected one request body content type, got %#v", requestBody)
	}
	contentType := contentTypes[0].(map[string]any)
	if contentType["mediaType"] != "application/json" {
		t.Fatalf("expected json request body media type, got %#v", contentType)
	}
	schema, _ := contentType["schema"].(map[string]any)
	if schema["type"] != "object" {
		t.Fatalf("expected machine-readable request body schema, got %#v", contentType["schema"])
	}
	guidance, _ := createTool["guidance"].(map[string]any)
	if guidance == nil {
		t.Fatalf("expected guidance metadata on create tool")
	}
	examples, _ := guidance["examples"].([]any)
	if len(examples) != 1 {
		t.Fatalf("expected guidance examples, got %#v", guidance)
	}
	example := examples[0].(map[string]any)
	if !strings.Contains(example["command"].(string), "--body @ticket.json") {
		t.Fatalf("expected example command to preserve request body guidance, got %#v", example)
	}

	safety, _ := createTool["safety"].(map[string]any)
	if idempotent, _ := safety["idempotent"].(bool); idempotent {
		t.Fatalf("expected create tool to remain non-idempotent, got %#v", safety)
	}
	if hidden, _ := archiveTool["hidden"].(bool); !hidden {
		t.Fatalf("expected archive tool to be hidden, got %#v", archiveTool)
	}
	if _, ok := listTool["pagination"].(map[string]any); !ok {
		t.Fatalf("expected pagination hints on list tool, got %#v", listTool)
	}
	if _, ok := listTool["retry"].(map[string]any); !ok {
		t.Fatalf("expected retry hints on list tool, got %#v", listTool)
	}
	if _, ok := createTool["output"].(map[string]any); !ok {
		t.Fatalf("expected output hints on create tool, got %#v", createTool)
	}
}

func TestBuildCatalogPreservesSecurityAlternatives(t *testing.T) {
	dir := t.TempDir()
	specPath := writeFile(t, dir, "auth.openapi.yaml", `
openapi: 3.1.0
info:
  title: Auth Alternatives API
  version: "1.0.0"
servers:
  - url: https://api.example.com
components:
  securitySchemes:
    oauth:
      type: oauth2
      flows:
        authorizationCode:
          authorizationUrl: https://auth.example.com/authorize
          tokenUrl: https://auth.example.com/token
          scopes:
            pets.read: Read pets
    api_key:
      type: apiKey
      in: query
      name: api_key
paths:
  /pets:
    get:
      operationId: listPets
      security:
        - oauth: [pets.read]
        - api_key: []
      responses:
        "200":
          description: OK
`)

	ntc, err := catalog.Build(context.Background(), catalog.BuildOptions{
		Config: config.Config{
			CLI:  "1.0.0",
			Mode: config.ModeConfig{Default: "discover"},
			Sources: map[string]config.Source{
				"authSource": {
					Type:    "openapi",
					URI:     filepath.ToSlash(specPath),
					Enabled: true,
				},
			},
			Services: map[string]config.Service{
				"protected": {Source: "authSource", Alias: "protected"},
			},
		},
		BaseDir: dir,
	})
	if err != nil {
		t.Fatalf("Build returned error: %v", err)
	}
	if len(ntc.Tools) != 1 {
		t.Fatalf("expected one tool, got %#v", ntc.Tools)
	}
	tool := ntc.Tools[0]
	if len(tool.AuthAlternatives) != 2 {
		t.Fatalf("expected 2 auth alternatives, got %#v", tool.AuthAlternatives)
	}
	if len(tool.AuthAlternatives[0].Requirements) != 1 || tool.AuthAlternatives[0].Requirements[0].Name != "oauth" {
		t.Fatalf("expected oauth alternative first, got %#v", tool.AuthAlternatives)
	}
	if len(tool.AuthAlternatives[1].Requirements) != 1 || tool.AuthAlternatives[1].Requirements[0].Name != "api_key" {
		t.Fatalf("expected api_key alternative second, got %#v", tool.AuthAlternatives)
	}
	if len(tool.Auth) != 2 {
		t.Fatalf("expected legacy flattened auth compatibility field, got %#v", tool.Auth)
	}
}

func TestBuildRejectsWorkflowReferencingIgnoredOperation(t *testing.T) {
	dir := t.TempDir()

	writeFile(t, dir, "tickets.openapi.yaml", `
openapi: 3.1.0
info:
  title: Example Tickets API
  version: "2026-03-01"
servers:
  - url: https://api.example.com/v1
paths:
  /tickets:
    get:
      operationId: listTickets
      tags: [tickets]
      summary: List tickets
      responses:
        "200":
          description: OK
    delete:
      operationId: deleteTickets
      tags: [tickets]
      summary: Delete tickets
      responses:
        "204":
          description: No Content
`)
	writeFile(t, dir, "overlays/tickets.overlay.yaml", `
overlay: 1.1.0
actions:
  - target: "$.paths['/tickets'].delete"
    update:
      x-cli-ignore: true
`)
	writeFile(t, dir, "workflows/tickets.arazzo.yaml", `
arazzo: 1.0.0
info:
  title: Ticket workflows
  version: 1.0.0
workflows:
  - workflowId: deleteWorkflow
    steps:
      - stepId: delete
        operationId: deleteTickets
`)

	cfg := config.Config{
		CLI:  "1.0.0",
		Mode: config.ModeConfig{Default: "discover"},
		Sources: map[string]config.Source{
			"ticketsSource": {
				Type:    "openapi",
				URI:     filepath.ToSlash(filepath.Join(dir, "tickets.openapi.yaml")),
				Enabled: true,
			},
		},
		Services: map[string]config.Service{
			"tickets": {
				Source:    "ticketsSource",
				Alias:     "tickets",
				Overlays:  []string{"./overlays/tickets.overlay.yaml"},
				Workflows: []string{"./workflows/tickets.arazzo.yaml"},
			},
		},
	}

	_, err := catalog.Build(context.Background(), catalog.BuildOptions{
		Config:  cfg,
		BaseDir: dir,
	})
	if err == nil {
		t.Fatal("expected Build to fail when workflow references an ignored operation")
	}
	if !strings.Contains(err.Error(), `workflow "deleteWorkflow" step "delete" references operationId "deleteTickets"`) {
		t.Fatalf("expected clear workflow reference error, got %v", err)
	}
}

func TestBuildRejectsWorkflowReferencingIgnoredOperationPath(t *testing.T) {
	dir := t.TempDir()

	writeFile(t, dir, "tickets.openapi.yaml", `
openapi: 3.1.0
info:
  title: Example Tickets API
  version: "2026-03-01"
servers:
  - url: https://api.example.com/v1
paths:
  /tickets:
    delete:
      operationId: deleteTickets
      tags: [tickets]
      summary: Delete tickets
      responses:
        "204":
          description: No Content
`)
	writeFile(t, dir, "overlays/tickets.overlay.yaml", `
overlay: 1.1.0
actions:
  - target: "$.paths['/tickets'].delete"
    update:
      x-cli-ignore: true
`)
	writeFile(t, dir, "workflows/tickets.arazzo.yaml", `
arazzo: 1.0.0
info:
  title: Ticket workflows
  version: 1.0.0
workflows:
  - workflowId: deleteWorkflow
    steps:
      - stepId: delete
        operationPath: DELETE /tickets
`)

	cfg := config.Config{
		CLI:  "1.0.0",
		Mode: config.ModeConfig{Default: "discover"},
		Sources: map[string]config.Source{
			"ticketsSource": {
				Type:    "openapi",
				URI:     filepath.ToSlash(filepath.Join(dir, "tickets.openapi.yaml")),
				Enabled: true,
			},
		},
		Services: map[string]config.Service{
			"tickets": {
				Source:    "ticketsSource",
				Alias:     "tickets",
				Overlays:  []string{"./overlays/tickets.overlay.yaml"},
				Workflows: []string{"./workflows/tickets.arazzo.yaml"},
			},
		},
	}

	_, err := catalog.Build(context.Background(), catalog.BuildOptions{
		Config:  cfg,
		BaseDir: dir,
	})
	if err == nil {
		t.Fatal("expected Build to fail when workflow references an ignored operation path")
	}
	if !strings.Contains(err.Error(), `workflow "deleteWorkflow" step "delete" references operationPath "DELETE /tickets"`) {
		t.Fatalf("expected clear workflow path reference error, got %v", err)
	}
}

func TestBuildRejectsWorkflowWhenAnyStepReferencesIgnoredOperation(t *testing.T) {
	dir := t.TempDir()

	writeFile(t, dir, "tickets.openapi.yaml", `
openapi: 3.1.0
info:
  title: Example Tickets API
  version: "2026-03-01"
servers:
  - url: https://api.example.com/v1
paths:
  /tickets:
    get:
      operationId: listTickets
      tags: [tickets]
      summary: List tickets
      responses:
        "200":
          description: OK
    post:
      operationId: createTicket
      tags: [tickets]
      summary: Create ticket
      responses:
        "201":
          description: Created
    delete:
      operationId: deleteTickets
      tags: [tickets]
      summary: Delete tickets
      responses:
        "204":
          description: No Content
`)
	writeFile(t, dir, "overlays/tickets.overlay.yaml", `
overlay: 1.1.0
actions:
  - target: "$.paths['/tickets'].delete"
    update:
      x-cli-ignore: true
`)
	writeFile(t, dir, "workflows/tickets.arazzo.yaml", `
arazzo: 1.0.0
info:
  title: Ticket workflows
  version: 1.0.0
workflows:
  - workflowId: ticketLifecycle
    steps:
      - stepId: list
        operationId: listTickets
      - stepId: create
        operationId: createTicket
      - stepId: delete
        operationId: deleteTickets
`)

	cfg := config.Config{
		CLI:  "1.0.0",
		Mode: config.ModeConfig{Default: "discover"},
		Sources: map[string]config.Source{
			"ticketsSource": {
				Type:    "openapi",
				URI:     filepath.ToSlash(filepath.Join(dir, "tickets.openapi.yaml")),
				Enabled: true,
			},
		},
		Services: map[string]config.Service{
			"tickets": {
				Source:    "ticketsSource",
				Alias:     "tickets",
				Overlays:  []string{"./overlays/tickets.overlay.yaml"},
				Workflows: []string{"./workflows/tickets.arazzo.yaml"},
			},
		},
	}

	_, err := catalog.Build(context.Background(), catalog.BuildOptions{
		Config:  cfg,
		BaseDir: dir,
	})
	if err == nil {
		t.Fatal("expected Build to fail when any workflow step references an ignored operation")
	}
	if !strings.Contains(err.Error(), `workflow "ticketLifecycle" step "delete" references operationId "deleteTickets"`) {
		t.Fatalf("expected error to point to the invalid step, got %v", err)
	}
}
