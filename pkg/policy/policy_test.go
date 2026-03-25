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

func TestDecideDiscoverModeAllowsAll(t *testing.T) {
	cfg := config.Config{}
	tool := catalog.Tool{ID: "svc:op"}
	d := policy.Decide(cfg, tool, policy.Context{Mode: "discover"})
	if !d.Allowed {
		t.Fatalf("discover mode should allow all, got %+v", d)
	}
}

func TestDecideManagedDenyBeatsDiscoverMode(t *testing.T) {
	cfg := config.Config{
		Policy: config.PolicyConfig{ManagedDeny: []string{"svc:dangerous"}},
	}
	tool := catalog.Tool{ID: "svc:dangerous"}
	d := policy.Decide(cfg, tool, policy.Context{Mode: "discover"})
	if d.Allowed || d.ReasonCode != "managed_deny" {
		t.Fatalf("managed deny should beat discover mode, got %+v", d)
	}
}

func TestDecideCuratedDenyUnlistedTool(t *testing.T) {
	cfg := config.Config{
		Curation: config.CurationConfig{
			ToolSets: map[string]config.ToolSet{
				"ts": {Allow: []string{"svc:allowed"}},
			},
		},
		Agents: config.AgentsConfig{
			Profiles: map[string]config.AgentProfile{
				"p": {Mode: "curated", ToolSet: "ts"},
			},
		},
	}
	tool := catalog.Tool{ID: "svc:other"}
	d := policy.Decide(cfg, tool, policy.Context{AgentProfile: "p"})
	if d.Allowed || d.ReasonCode != "curated_deny" {
		t.Fatalf("expected curated_deny, got %+v", d)
	}
}

func TestDecideToolSafetyRequiresApproval(t *testing.T) {
	cfg := config.Config{}
	tool := catalog.Tool{ID: "svc:op", Safety: catalog.Safety{RequiresApproval: true}}
	d := policy.Decide(cfg, tool, policy.Context{Mode: "discover"})
	if d.Allowed || d.ReasonCode != "approval_required" {
		t.Fatalf("expected approval_required from tool safety, got %+v", d)
	}
	d2 := policy.Decide(cfg, tool, policy.Context{Mode: "discover", ApprovalGranted: true})
	if !d2.Allowed {
		t.Fatalf("expected allowed with approval granted, got %+v", d2)
	}
}

func TestDecideUnknownAgentProfileFallsToDefaultMode(t *testing.T) {
	cfg := config.Config{}
	tool := catalog.Tool{ID: "svc:op"}
	d := policy.Decide(cfg, tool, policy.Context{AgentProfile: "nonexistent"})
	if !d.Allowed {
		t.Fatalf("unknown profile with no managed deny should allow, got %+v", d)
	}
}

func TestMatchesAnyWildcard(t *testing.T) {
	tests := []struct {
		name     string
		patterns []string
		value    string
		want     bool
	}{
		{"double star matches all", []string{"**"}, "anything:here", true},
		{"glob matches prefix", []string{"svc:*"}, "svc:op", true},
		{"glob no match", []string{"svc:*"}, "other:op", false},
		{"exact match", []string{"svc:op"}, "svc:op", true},
		{"exact no match", []string{"svc:op"}, "svc:other", false},
		{"empty patterns", []string{}, "svc:op", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := policy.MatchesAny(tt.patterns, tt.value)
			if got != tt.want {
				t.Errorf("MatchesAny(%v, %q) = %v, want %v", tt.patterns, tt.value, got, tt.want)
			}
		})
	}
}

func TestToolAllowed(t *testing.T) {
	tests := []struct {
		name    string
		toolID  string
		toolSet config.ToolSet
		want    bool
	}{
		{"empty toolset allows all", "svc:op", config.ToolSet{}, true},
		{"allow list includes tool", "svc:op", config.ToolSet{Allow: []string{"svc:op"}}, true},
		{"allow list excludes tool", "svc:other", config.ToolSet{Allow: []string{"svc:op"}}, false},
		{"deny list blocks tool", "svc:op", config.ToolSet{Deny: []string{"svc:op"}}, false},
		{"allow overrides deny star", "svc:op", config.ToolSet{Allow: []string{"svc:op"}, Deny: []string{"**"}}, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := policy.ToolAllowed(tt.toolID, tt.toolSet)
			if got != tt.want {
				t.Errorf("ToolAllowed(%q, ...) = %v, want %v", tt.toolID, got, tt.want)
			}
		})
	}
}
