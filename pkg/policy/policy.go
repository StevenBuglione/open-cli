package policy

import (
	"path"

	"github.com/StevenBuglione/oas-cli-go/pkg/catalog"
	"github.com/StevenBuglione/oas-cli-go/pkg/config"
)

type Context struct {
	Mode            string
	AgentProfile    string
	ApprovalGranted bool
}

type Decision struct {
	Allowed    bool   `json:"allowed"`
	ReasonCode string `json:"reasonCode"`
}

func Decide(cfg config.Config, tool catalog.Tool, ctx Context) Decision {
	if matchesAny(cfg.Policy.ManagedDeny, tool.ID) {
		return Decision{Allowed: false, ReasonCode: "managed_deny"}
	}

	mode, toolSet := resolveModeAndToolSet(cfg, ctx)
	if mode == "curated" && !toolAllowed(tool.ID, toolSet) {
		return Decision{Allowed: false, ReasonCode: "curated_deny"}
	}

	if (tool.Safety.RequiresApproval || matchesAny(cfg.Policy.ApprovalRequired, tool.ID)) && !ctx.ApprovalGranted {
		return Decision{Allowed: false, ReasonCode: "approval_required"}
	}

	return Decision{Allowed: true, ReasonCode: "allowed"}
}

func ToolAllowed(toolID string, toolSet config.ToolSet) bool {
	return toolAllowed(toolID, toolSet)
}

func MatchesAny(patterns []string, value string) bool {
	return matchesAny(patterns, value)
}

func resolveModeAndToolSet(cfg config.Config, ctx Context) (string, config.ToolSet) {
	mode := ctx.Mode
	if mode == "" {
		mode = cfg.Mode.Default
	}
	if ctx.AgentProfile != "" {
		if profile, ok := cfg.Agents.Profiles[ctx.AgentProfile]; ok {
			if profile.Mode != "" {
				mode = profile.Mode
			}
			return mode, cfg.Curation.ToolSets[profile.ToolSet]
		}
	}
	if cfg.Agents.DefaultProfile != "" {
		if profile, ok := cfg.Agents.Profiles[cfg.Agents.DefaultProfile]; ok && mode == "curated" {
			return mode, cfg.Curation.ToolSets[profile.ToolSet]
		}
	}
	return mode, config.ToolSet{}
}

func toolAllowed(toolID string, toolSet config.ToolSet) bool {
	if len(toolSet.Allow) > 0 && !matchesAny(toolSet.Allow, toolID) {
		return false
	}
	for _, pattern := range toolSet.Deny {
		if pattern == "**" && len(toolSet.Allow) > 0 {
			continue
		}
		if matchPattern(pattern, toolID) {
			return false
		}
	}
	return true
}

func matchesAny(patterns []string, value string) bool {
	for _, pattern := range patterns {
		if matchPattern(pattern, value) {
			return true
		}
	}
	return false
}

func matchPattern(pattern, value string) bool {
	if pattern == "**" {
		return true
	}
	matched, err := path.Match(pattern, value)
	if err != nil {
		return pattern == value
	}
	return matched
}
