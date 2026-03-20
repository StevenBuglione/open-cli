package commands

import (
	"fmt"
	"io"
	"sort"
	"strings"

	cfgpkg "github.com/StevenBuglione/open-cli/cmd/ocli/internal/config"
	runtimepkg "github.com/StevenBuglione/open-cli/cmd/ocli/internal/runtime"
	configpkg "github.com/StevenBuglione/open-cli/pkg/config"
	"github.com/spf13/cobra"
)

type statusReport struct {
	Runtime    statusRuntimeSummary    `json:"runtime"`
	Config     statusConfigSummary     `json:"config"`
	Sources    statusSourceSummary     `json:"sources"`
	Auth       statusAuthSummary       `json:"auth"`
	Approval   statusApprovalSummary   `json:"approval"`
	ScopePaths statusScopePathsSummary `json:"scopePaths"`
}

type statusRuntimeSummary struct {
	Available bool    `json:"available"`
	Mode      string  `json:"mode"`
	Version   *string `json:"version"`
}

type statusConfigSummary struct {
	ActivePath *string `json:"activePath"`
}

type statusSourceSummary struct {
	TotalActive int            `json:"totalActive"`
	ByType      map[string]int `json:"byType"`
}

type statusAuthSummary struct {
	Mode                   string   `json:"mode"`
	Required               *bool    `json:"required"`
	Audience               *string  `json:"audience"`
	Scopes                 []string `json:"scopes"`
	BrowserLoginConfigured *bool    `json:"browserLoginConfigured"`
}

type statusApprovalSummary struct {
	HasApprovalGatedTools *bool  `json:"hasApprovalGatedTools"`
	Status                string `json:"status"`
}

type statusScopePathsSummary struct {
	Managed *string `json:"managed"`
	User    *string `json:"user"`
	Project *string `json:"project"`
	Local   *string `json:"local"`
}

// NewStatusCommand returns the "status" subcommand for quick health checks.
func NewStatusCommand(options cfgpkg.Options, client runtimepkg.Client, runtimeUnavailable bool) *cobra.Command {
	return &cobra.Command{
		Use:   "status",
		Short: "Show runtime and configuration health",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return writeStatus(options.Stdout, options, client, runtimeUnavailable)
		},
	}
}

func writeStatus(w io.Writer, options cfgpkg.Options, client runtimepkg.Client, runtimeUnavailable bool) error {
	report, runtimeAuthPresent := buildStatusReport(options, client, runtimeUnavailable)
	if options.Format == "table" {
		writeStatusTerminal(w, report, runtimeAuthPresent, runtimeUnavailable)
		return nil
	}
	return WriteOutput(w, options.Format, report)
}

func buildStatusReport(options cfgpkg.Options, client runtimepkg.Client, runtimeUnavailable bool) (statusReport, bool) {
	report := statusReport{
		Runtime: statusRuntimeSummary{Mode: runtimeMode(options)},
		Config:  statusConfigSummary{ActivePath: stringPtr(options.ConfigPath)},
		Sources: statusSourceSummary{ByType: map[string]int{}},
		Auth: statusAuthSummary{
			Mode:   "unknown",
			Scopes: []string{},
		},
		Approval:   statusApprovalSummary{Status: "unknown"},
		ScopePaths: scopePathSummary(configpkg.DiscoverScopePaths(configpkg.LoadOptions{})),
	}

	var rawConfig map[string]any
	if options.ConfigPath != "" {
		if raw, err := readConfigFile(options.ConfigPath); err == nil {
			rawConfig = raw
			report.Sources = countEnabledSources(raw)
		}
	}

	var runtimeAuth map[string]any
	if !runtimeUnavailable {
		info, err := client.RuntimeInfo()
		if err == nil {
			report.Runtime.Available = true
			if version, ok := stringFromMap(info, "version"); ok {
				report.Runtime.Version = stringPtr(version)
			}
			if authInfo, ok := info["auth"].(map[string]any); ok {
				runtimeAuth = authInfo
			}
			populateStatusAuth(&report.Auth, rawConfig, runtimeAuth)
			if response, err := client.FetchCatalog(runtimepkg.CatalogFetchOptions{
				ConfigPath:   options.ConfigPath,
				Mode:         options.Mode,
				AgentProfile: options.AgentProfile,
				RuntimeToken: options.RuntimeToken,
			}); err == nil {
				report.Approval = approvalSummaryFromCatalog(response)
			}
			return report, len(runtimeAuth) > 0
		}
	}

	populateStatusAuth(&report.Auth, rawConfig, nil)
	return report, false
}

func writeStatusTerminal(w io.Writer, report statusReport, runtimeAuthPresent bool, runtimeUnavailable bool) {
	if !report.Runtime.Available {
		if runtimeUnavailable {
			fmt.Fprintln(w, "Runtime:  x not running")
		} else {
			fmt.Fprintln(w, "Runtime:  x error fetching info")
		}
	} else {
		version := "unknown"
		if report.Runtime.Version != nil && strings.TrimSpace(*report.Runtime.Version) != "" {
			version = *report.Runtime.Version
		}
		fmt.Fprintf(w, "Runtime:  ok %s (v%s)\n", report.Runtime.Mode, version)
	}

	if report.Config.ActivePath != nil {
		fmt.Fprintf(w, "Config:   %s\n", *report.Config.ActivePath)
	} else {
		fmt.Fprintln(w, "Config:   none")
	}

	if report.Config.ActivePath != nil {
		fmt.Fprintf(w, "Sources:  %d active", report.Sources.TotalActive)
		if report.Sources.TotalActive > 0 {
			parts := make([]string, 0, len(report.Sources.ByType))
			for sourceType, count := range report.Sources.ByType {
				parts = append(parts, fmt.Sprintf("%d %s", count, sourceType))
			}
			sort.Strings(parts)
			fmt.Fprintf(w, " (%s)", strings.Join(parts, ", "))
		}
		fmt.Fprintln(w)
	}

	if runtimeAuthPresent {
		parts := []string{fmt.Sprintf("mode=%s", report.Auth.Mode)}
		if report.Auth.Required != nil {
			parts = append(parts, fmt.Sprintf("required=%t", *report.Auth.Required))
		}
		if report.Auth.Audience != nil && *report.Auth.Audience != "" {
			parts = append(parts, fmt.Sprintf("audience=%s", *report.Auth.Audience))
		}
		if len(report.Auth.Scopes) > 0 {
			parts = append(parts, fmt.Sprintf("scopes=%s", strings.Join(report.Auth.Scopes, " ")))
		}
		if report.Auth.BrowserLoginConfigured != nil {
			parts = append(parts, fmt.Sprintf("browserLoginConfigured=%t", *report.Auth.BrowserLoginConfigured))
		}
		fmt.Fprintf(w, "Auth:     %s\n", strings.Join(parts, ", "))
	}

	if report.Approval.Status != "unknown" && report.Approval.HasApprovalGatedTools != nil {
		if *report.Approval.HasApprovalGatedTools {
			fmt.Fprintln(w, "Approval: required")
		} else {
			fmt.Fprintln(w, "Approval: not required")
		}
	}

	fmt.Fprintln(w, "Scope paths:")
	for _, entry := range []struct {
		label string
		value *string
	}{
		{label: "managed", value: report.ScopePaths.Managed},
		{label: "user", value: report.ScopePaths.User},
		{label: "project", value: report.ScopePaths.Project},
		{label: "local", value: report.ScopePaths.Local},
	} {
		if entry.value != nil {
			fmt.Fprintf(w, "  %s: %s\n", entry.label, *entry.value)
		}
	}

	if runtimeUnavailable {
		fmt.Fprintln(w)
		fmt.Fprintln(w, "Suggestion: Start the daemon with oclird")
	}
}

func populateStatusAuth(auth *statusAuthSummary, rawConfig map[string]any, runtimeAuth map[string]any) {
	auth.Mode = deriveStatusAuthMode(rawConfig, runtimeAuth)
	auth.Required = boolFromMap(runtimeAuth, "required")
	auth.Audience = stringPtrFromMap(runtimeAuth, "audience")
	if auth.Audience == nil {
		auth.Audience = stringPtrFromRawConfig(rawConfig, "runtime", "remote", "oauth", "audience")
	}
	auth.Scopes = stringSliceFromMap(runtimeAuth, "scopes")
	if len(auth.Scopes) == 0 {
		auth.Scopes = stringSliceFromRawConfig(rawConfig, "runtime", "remote", "oauth", "scopes")
	}
	if len(auth.Scopes) == 0 {
		auth.Scopes = []string{}
	}
	auth.BrowserLoginConfigured = boolFromNestedMap(runtimeAuth, "browserLogin", "configured")
}

func deriveStatusAuthMode(rawConfig map[string]any, runtimeAuth map[string]any) string {
	if mode := stringFromRawConfig(rawConfig, "runtime", "remote", "oauth", "mode"); mode != "" {
		return mode
	}
	if browserLoginConfigured := boolFromNestedMap(runtimeAuth, "browserLogin", "configured"); browserLoginConfigured != nil && *browserLoginConfigured {
		return "browserLogin"
	}
	if required := boolFromMap(runtimeAuth, "required"); required != nil {
		if !*required {
			return "none"
		}
		if len(stringSliceFromMap(runtimeAuth, "scopes")) > 0 {
			return "oauthClient"
		}
	}
	return "unknown"
}

func runtimeMode(options cfgpkg.Options) string {
	switch {
	case options.Embedded:
		return "embedded"
	case options.RuntimeDeployment == "local":
		return "local"
	case options.RuntimeDeployment == "remote":
		return "remote"
	default:
		return "unknown"
	}
}

func countEnabledSources(raw map[string]any) statusSourceSummary {
	result := statusSourceSummary{ByType: map[string]int{}}
	sources, _ := raw["sources"].(map[string]any)
	for _, value := range sources {
		source, ok := value.(map[string]any)
		if !ok {
			continue
		}
		enabled := true
		if flag, ok := source["enabled"].(bool); ok {
			enabled = flag
		}
		if !enabled {
			continue
		}
		sourceType, _ := source["type"].(string)
		if sourceType == "" {
			sourceType = "unknown"
		}
		result.ByType[sourceType]++
		result.TotalActive++
	}
	return result
}

func approvalSummaryFromCatalog(response runtimepkg.CatalogResponse) statusApprovalSummary {
	tools := response.View.Tools
	if len(tools) == 0 {
		tools = response.Catalog.Tools
	}
	hasApproval := false
	for _, tool := range tools {
		if tool.Safety.RequiresApproval {
			hasApproval = true
			break
		}
	}
	status := "not_required"
	if hasApproval {
		status = "required"
	}
	return statusApprovalSummary{
		HasApprovalGatedTools: boolPtr(hasApproval),
		Status:                status,
	}
}

func scopePathSummary(paths map[configpkg.Scope]string) statusScopePathsSummary {
	return statusScopePathsSummary{
		Managed: stringPtr(paths[configpkg.ScopeManaged]),
		User:    stringPtr(paths[configpkg.ScopeUser]),
		Project: stringPtr(paths[configpkg.ScopeProject]),
		Local:   stringPtr(paths[configpkg.ScopeLocal]),
	}
}

func stringPtr(value string) *string {
	if strings.TrimSpace(value) == "" {
		return nil
	}
	return &value
}

func boolPtr(value bool) *bool {
	return &value
}

func stringFromMap(m map[string]any, key string) (string, bool) {
	if m == nil {
		return "", false
	}
	value, ok := m[key]
	if !ok {
		return "", false
	}
	text, ok := value.(string)
	if !ok {
		return "", false
	}
	text = strings.TrimSpace(text)
	if text == "" {
		return "", false
	}
	return text, true
}

func stringPtrFromMap(m map[string]any, key string) *string {
	if value, ok := stringFromMap(m, key); ok {
		return stringPtr(value)
	}
	return nil
}

func boolFromMap(m map[string]any, key string) *bool {
	if m == nil {
		return nil
	}
	value, ok := m[key]
	if !ok {
		return nil
	}
	switch typed := value.(type) {
	case bool:
		return &typed
	case string:
		switch strings.TrimSpace(strings.ToLower(typed)) {
		case "true", "1", "yes", "on":
			v := true
			return &v
		case "false", "0", "no", "off":
			v := false
			return &v
		}
	}
	return nil
}

func boolFromNestedMap(m map[string]any, parent, key string) *bool {
	if m == nil {
		return nil
	}
	nested, _ := m[parent].(map[string]any)
	return boolFromMap(nested, key)
}

func stringSliceFromMap(m map[string]any, key string) []string {
	if m == nil {
		return nil
	}
	value, ok := m[key]
	if !ok {
		return nil
	}
	return stringSliceFromAny(value)
}

func stringSliceFromAny(value any) []string {
	switch typed := value.(type) {
	case []string:
		return append([]string(nil), typed...)
	case []any:
		result := make([]string, 0, len(typed))
		for _, item := range typed {
			text, ok := item.(string)
			if !ok {
				continue
			}
			text = strings.TrimSpace(text)
			if text != "" {
				result = append(result, text)
			}
		}
		return result
	default:
		return nil
	}
}

func stringFromRawConfig(raw map[string]any, path ...string) string {
	current := any(raw)
	for _, key := range path {
		asMap, ok := current.(map[string]any)
		if !ok {
			return ""
		}
		current = asMap[key]
	}
	text, _ := current.(string)
	return strings.TrimSpace(text)
}

func stringPtrFromRawConfig(raw map[string]any, path ...string) *string {
	if value := stringFromRawConfig(raw, path...); value != "" {
		return stringPtr(value)
	}
	return nil
}

func stringSliceFromRawConfig(raw map[string]any, path ...string) []string {
	current := any(raw)
	for _, key := range path {
		asMap, ok := current.(map[string]any)
		if !ok {
			return nil
		}
		current = asMap[key]
	}
	return stringSliceFromAny(current)
}
