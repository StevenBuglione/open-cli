package policy_test

import (
	"testing"

	"github.com/StevenBuglione/open-cli/pkg/catalog"
	"github.com/StevenBuglione/open-cli/pkg/config"
	"github.com/StevenBuglione/open-cli/pkg/policy"
)

func TestDecideHonorsManagedDenyCuratedModeAndApproval(t *testing.T) {
	cfg := config.Config{
		Curation: config.CurationConfig{
			ToolSets: map[string]config.ToolSet{
				"sandbox": {
					Allow: []string{"tickets:listTickets", "tickets:createTicket"},
					Deny:  []string{"**", "tickets:deleteTicket"},
				},
			},
		},
		Agents: config.AgentsConfig{
			Profiles: map[string]config.AgentProfile{
				"sandbox": {
					Mode:    "curated",
					ToolSet: "sandbox",
				},
			},
		},
		Policy: config.PolicyConfig{
			ManagedDeny:      []string{"tickets:deleteTicket"},
			ApprovalRequired: []string{"tickets:createTicket"},
		},
	}

	listTool := catalog.Tool{ID: "tickets:listTickets"}
	createTool := catalog.Tool{ID: "tickets:createTicket"}
	deleteTool := catalog.Tool{ID: "tickets:deleteTicket"}

	if decision := policy.Decide(cfg, listTool, policy.Context{AgentProfile: "sandbox"}); !decision.Allowed {
		t.Fatalf("expected list tool to be allowed, got %#v", decision)
	}

	if decision := policy.Decide(cfg, createTool, policy.Context{AgentProfile: "sandbox"}); decision.Allowed || decision.ReasonCode != "approval_required" {
		t.Fatalf("expected approval_required, got %#v", decision)
	}

	if decision := policy.Decide(cfg, createTool, policy.Context{AgentProfile: "sandbox", ApprovalGranted: true}); !decision.Allowed {
		t.Fatalf("expected approved create tool, got %#v", decision)
	}

	if decision := policy.Decide(cfg, deleteTool, policy.Context{Mode: "discover"}); decision.Allowed || decision.ReasonCode != "managed_deny" {
		t.Fatalf("expected managed deny, got %#v", decision)
	}
}
