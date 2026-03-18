package catalog

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"sort"
	"strings"

	"github.com/StevenBuglione/open-cli/pkg/cache"
	"github.com/StevenBuglione/open-cli/pkg/config"
	"github.com/StevenBuglione/open-cli/pkg/mcp/openapi"
	oasopenapi "github.com/StevenBuglione/open-cli/pkg/openapi"
	overlaypkg "github.com/StevenBuglione/open-cli/pkg/overlay"
	"github.com/getkin/kin-openapi/openapi3"
	"gopkg.in/yaml.v3"
)

type mcpDisabledValidationState struct {
	sourceID            string
	serviceID           string
	allByOperationID    map[string][]openapi.OperationRef
	allByOperationPath  map[string][]openapi.OperationRef
	liveByOperationID   map[string][]openapi.OperationRef
	liveByOperationPath map[string][]openapi.OperationRef
	allToolIDs          map[string]string
	liveToolIDs         map[string]string
}

func newMCPDisabledValidationState(sourceID, serviceID string, result *openapi.BuildResult) *mcpDisabledValidationState {
	if result == nil {
		return nil
	}
	state := &mcpDisabledValidationState{
		sourceID:            sourceID,
		serviceID:           serviceID,
		allByOperationID:    indexOperationRefs(result.AllOperations, func(ref openapi.OperationRef) string { return ref.OperationID }),
		allByOperationPath:  indexOperationRefs(result.AllOperations, operationPathKey),
		liveByOperationID:   indexOperationRefs(result.FilteredOperations, func(ref openapi.OperationRef) string { return ref.OperationID }),
		liveByOperationPath: indexOperationRefs(result.FilteredOperations, operationPathKey),
		allToolIDs:          map[string]string{},
		liveToolIDs:         map[string]string{},
	}
	for _, ref := range result.AllOperations {
		state.allToolIDs[state.toolIDFor(ref)] = ref.ToolName
	}
	return state
}

func indexOperationRefs(refs []openapi.OperationRef, keyFn func(openapi.OperationRef) string) map[string][]openapi.OperationRef {
	index := map[string][]openapi.OperationRef{}
	for _, ref := range refs {
		key := keyFn(ref)
		if key == "" {
			continue
		}
		index[key] = append(index[key], ref)
	}
	return index
}

func operationPathKey(ref openapi.OperationRef) string {
	if ref.Method == "" || ref.Path == "" {
		return ""
	}
	return ref.Method + " " + ref.Path
}

func (state *mcpDisabledValidationState) toolIDFor(ref openapi.OperationRef) string {
	return state.serviceID + ":" + ref.OperationID
}

func (state *mcpDisabledValidationState) setFinalTools(tools []Tool) {
	state.liveToolIDs = map[string]string{}
	for _, tool := range tools {
		state.liveToolIDs[tool.ID] = ""
		if tool.Backend != nil && tool.Backend.ToolName != "" {
			state.liveToolIDs[tool.ID] = tool.Backend.ToolName
		}
	}
}

func (state *mcpDisabledValidationState) disabledWorkflowReferenceError(ref string, workflow workflowSpec, step workflowStepSpec) error {
	if state == nil {
		return nil
	}
	var allMatches []openapi.OperationRef
	var liveMatches []openapi.OperationRef
	var label string
	switch {
	case step.OperationID != "":
		allMatches = state.allByOperationID[step.OperationID]
		liveMatches = state.liveByOperationID[step.OperationID]
		label = fmt.Sprintf("operationId %q", step.OperationID)
	case step.OperationPath != "":
		allMatches = state.allByOperationPath[step.OperationPath]
		liveMatches = state.liveByOperationPath[step.OperationPath]
		label = fmt.Sprintf("operationPath %q", step.OperationPath)
	default:
		return nil
	}
	if len(allMatches) == 0 || len(liveMatches) > 0 {
		return nil
	}
	return fmt.Errorf(
		`source %q service %q: workflow %q workflow %q step %q references %s, which maps only to %s`,
		state.sourceID,
		state.serviceID,
		ref,
		workflow.WorkflowID,
		step.StepID,
		label,
		disabledToolLabel(allMatches),
	)
}

var overlayTargetPattern = regexp.MustCompile(`^\$\.paths\[(?:'|")([^'"]+)(?:'|")\](?:\.(get|post|put|patch|delete|head|options|trace))?(?:[.\[]|$)`)

func (state *mcpDisabledValidationState) validateOverlayReference(ref string, actionIndex int, target string) error {
	if state == nil {
		return nil
	}
	pathValue, methodValue, ok := parseOverlayOperationTarget(target)
	if !ok {
		return nil
	}
	allMatches := state.overlayMatches(pathValue, methodValue, false)
	if len(allMatches) == 0 {
		return nil
	}
	liveMatches := state.overlayMatches(pathValue, methodValue, true)
	if len(liveMatches) > 0 {
		return nil
	}
	return fmt.Errorf(
		`source %q service %q: overlay %q action %d target %q references %s`,
		state.sourceID,
		state.serviceID,
		ref,
		actionIndex+1,
		target,
		disabledToolLabel(allMatches),
	)
}

func parseOverlayOperationTarget(target string) (pathValue string, methodValue string, ok bool) {
	matches := overlayTargetPattern.FindStringSubmatch(target)
	if len(matches) != 3 {
		return "", "", false
	}
	return matches[1], strings.ToUpper(matches[2]), true
}

func (state *mcpDisabledValidationState) overlayMatches(pathValue, methodValue string, surviving bool) []openapi.OperationRef {
	if pathValue == "" {
		return nil
	}
	if methodValue != "" {
		key := methodValue + " " + pathValue
		if surviving {
			return state.liveByOperationPath[key]
		}
		return state.allByOperationPath[key]
	}

	seen := map[string]struct{}{}
	var matches []openapi.OperationRef
	var source map[string][]openapi.OperationRef
	if surviving {
		source = state.liveByOperationPath
	} else {
		source = state.allByOperationPath
	}
	for key, refs := range source {
		if !strings.HasSuffix(key, " "+pathValue) {
			continue
		}
		for _, ref := range refs {
			composite := ref.ToolName + "|" + ref.Method + "|" + ref.Path
			if _, ok := seen[composite]; ok {
				continue
			}
			seen[composite] = struct{}{}
			matches = append(matches, ref)
		}
	}
	return matches
}

func disabledToolLabel(refs []openapi.OperationRef) string {
	names := map[string]struct{}{}
	for _, ref := range refs {
		if ref.ToolName == "" {
			continue
		}
		names[ref.ToolName] = struct{}{}
	}
	ordered := make([]string, 0, len(names))
	for name := range names {
		ordered = append(ordered, name)
	}
	sort.Strings(ordered)
	if len(ordered) == 1 {
		return fmt.Sprintf(`disabled MCP tool %q`, ordered[0])
	}
	quoted := make([]string, 0, len(ordered))
	for _, name := range ordered {
		quoted = append(quoted, fmt.Sprintf("%q", name))
	}
	return fmt.Sprintf("disabled MCP tools %s", strings.Join(quoted, ", "))
}

func validateDisabledMCPPolicyPatterns(cfg config.Config, states []*mcpDisabledValidationState, finalTools []Tool) error {
	if len(states) == 0 {
		return nil
	}
	finalIDs := map[string]struct{}{}
	for _, tool := range finalTools {
		finalIDs[tool.ID] = struct{}{}
	}
	for _, pattern := range cfg.Policy.ApprovalRequired {
		if err := validateDisabledMCPPattern(states, finalIDs, "policy.approvalRequired", pattern); err != nil {
			return err
		}
	}
	for _, pattern := range cfg.Policy.ManagedDeny {
		if err := validateDisabledMCPPattern(states, finalIDs, "policy.managedDeny", pattern); err != nil {
			return err
		}
	}
	toolSetNames := make([]string, 0, len(cfg.Curation.ToolSets))
	for name := range cfg.Curation.ToolSets {
		toolSetNames = append(toolSetNames, name)
	}
	sort.Strings(toolSetNames)
	for _, name := range toolSetNames {
		toolSet := cfg.Curation.ToolSets[name]
		for _, pattern := range toolSet.Allow {
			if err := validateDisabledMCPPattern(states, finalIDs, fmt.Sprintf(`toolSet %q allow`, name), pattern); err != nil {
				return err
			}
		}
		for _, pattern := range toolSet.Deny {
			if err := validateDisabledMCPPattern(states, finalIDs, fmt.Sprintf(`toolSet %q deny`, name), pattern); err != nil {
				return err
			}
		}
	}
	return nil
}

func validateDisabledMCPPattern(states []*mcpDisabledValidationState, finalIDs map[string]struct{}, artifact string, pattern string) error {
	for _, state := range states {
		matchedAll := state.matchingCandidateToolIDs(pattern, state.allToolIDs)
		if len(matchedAll) == 0 {
			continue
		}
		if anyPatternMatch(finalIDs, pattern) {
			continue
		}
		if !state.allCandidatesDisabled(matchedAll) {
			continue
		}
		return fmt.Errorf(
			`source %q service %q: %s pattern %q references %s`,
			state.sourceID,
			state.serviceID,
			artifact,
			pattern,
			state.disabledToolLabelForIDs(matchedAll),
		)
	}
	return nil
}

func (state *mcpDisabledValidationState) matchingCandidateToolIDs(pattern string, candidates map[string]string) []string {
	var matches []string
	for id := range candidates {
		if matchPattern(pattern, id) {
			matches = append(matches, id)
		}
	}
	sort.Strings(matches)
	return matches
}

func anyPatternMatch(ids map[string]struct{}, pattern string) bool {
	for id := range ids {
		if matchPattern(pattern, id) {
			return true
		}
	}
	return false
}

func (state *mcpDisabledValidationState) allCandidatesDisabled(ids []string) bool {
	if len(ids) == 0 {
		return false
	}
	for _, id := range ids {
		if _, ok := state.liveToolIDs[id]; ok {
			return false
		}
	}
	return true
}

func (state *mcpDisabledValidationState) disabledToolLabelForIDs(ids []string) string {
	names := map[string]struct{}{}
	for _, id := range ids {
		if name := state.allToolIDs[id]; name != "" {
			names[name] = struct{}{}
		}
	}
	var refs []openapi.OperationRef
	for name := range names {
		refs = append(refs, openapi.OperationRef{ToolName: name})
	}
	return disabledToolLabel(refs)
}

func applyMCPOverlays(ctx context.Context, baseDir string, document *openapi3.T, refs []string, fetcher *cache.Fetcher, policy cache.Policy, method string, validator *mcpDisabledValidationState) (*openapi3.T, []SourceFetch, error) {
	if len(refs) == 0 {
		return document, nil, nil
	}
	raw, err := documentToRawMap(document)
	if err != nil {
		return nil, nil, err
	}

	var fetches []SourceFetch
	for _, ref := range refs {
		resolved := oasopenapi.ResolveReference(baseDir, ref)
		data, fetchRecord, err := oasopenapi.ReadReference(ctx, resolved, fetcher, policy)
		if err != nil {
			return nil, nil, err
		}
		var doc overlaypkg.Document
		if err := yaml.Unmarshal(data, &doc); err != nil {
			return nil, nil, err
		}
		if validator != nil {
			for idx, action := range doc.Actions {
				if err := validator.validateOverlayReference(ref, idx, action.Target); err != nil {
					return nil, nil, err
				}
			}
		}
		raw, err = overlaypkg.Apply(raw, doc)
		if err != nil {
			return nil, nil, err
		}
		if fetchRecord != nil {
			fetches = append(fetches, sourceFetchFromOpenAPI(fetchRecord, method))
		}
	}
	updated, err := rawMapToDocument(raw)
	if err != nil {
		return nil, nil, err
	}
	return updated, fetches, nil
}

func documentToRawMap(document *openapi3.T) (map[string]any, error) {
	data, err := json.Marshal(document)
	if err != nil {
		return nil, err
	}
	var raw map[string]any
	if err := json.Unmarshal(data, &raw); err != nil {
		return nil, err
	}
	return raw, nil
}

func rawMapToDocument(raw map[string]any) (*openapi3.T, error) {
	data, err := json.Marshal(raw)
	if err != nil {
		return nil, err
	}
	var document openapi3.T
	if err := json.Unmarshal(data, &document); err != nil {
		return nil, err
	}
	return &document, nil
}
