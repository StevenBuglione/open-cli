package catalog

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"path"
	"sort"
	"strings"
	"time"

	"github.com/StevenBuglione/oas-cli-go/pkg/cache"
	"github.com/StevenBuglione/oas-cli-go/pkg/config"
	"github.com/StevenBuglione/oas-cli-go/pkg/discovery"
	mcpclient "github.com/StevenBuglione/oas-cli-go/pkg/mcp/client"
	mcpopenapi "github.com/StevenBuglione/oas-cli-go/pkg/mcp/openapi"
	"github.com/StevenBuglione/oas-cli-go/pkg/openapi"
	"github.com/getkin/kin-openapi/openapi3"
	"gopkg.in/yaml.v3"
)

type skillManifest struct {
	ToolGuidance map[string]Guidance `json:"toolGuidance"`
}

type workflowDocument struct {
	Workflows []workflowSpec `json:"workflows" yaml:"workflows"`
}

type workflowSpec struct {
	WorkflowID string             `json:"workflowId" yaml:"workflowId"`
	Steps      []workflowStepSpec `json:"steps" yaml:"steps"`
}

type workflowStepSpec struct {
	StepID        string `json:"stepId" yaml:"stepId"`
	OperationID   string `json:"operationId" yaml:"operationId"`
	OperationPath string `json:"operationPath" yaml:"operationPath"`
}

func Build(ctx context.Context, options BuildOptions) (*NormalizedCatalog, error) {
	cfg, ok := options.Config.(config.Config)
	if !ok {
		return nil, fmt.Errorf("build options require config.Config")
	}

	var store *cache.FileStore
	var err error
	if options.CacheDir != "" {
		store, err = cache.NewFileStore(options.CacheDir)
		if err != nil {
			return nil, err
		}
	}
	fetcher := cache.NewFetcher(cache.FetcherOptions{
		Store:    store,
		Client:   options.HTTPClient,
		Observer: options.Observer,
	})

	catalog := &NormalizedCatalog{
		CatalogVersion: "1.0.0",
		GeneratedAt:    time.Now().UTC(),
	}
	fingerprint := sha256.New()
	sourceRecords := map[string]*SourceRecord{}
	var mcpValidationStates []*mcpDisabledValidationState

	referencedSources := map[string]bool{}
	serviceIDs := sortedKeys(cfg.Services)
	for _, serviceID := range serviceIDs {
		serviceConfig := cfg.Services[serviceID]
		sourceConfig, ok := cfg.Sources[serviceConfig.Source]
		if !ok || !sourceConfig.Enabled {
			continue
		}
		referencedSources[serviceConfig.Source] = true
		fetches, validationState, err := buildServiceCatalog(ctx, catalog, &cfg, options.BaseDir, serviceID, serviceConfig, sourceConfig, fingerprint, fetcher, cachePolicyForSource(sourceConfig, options.ForceRefresh), options.StateDir, options.HTTPClient)
		if err != nil {
			return nil, err
		}
		if validationState != nil {
			mcpValidationStates = append(mcpValidationStates, validationState)
		}
		recordSource(sourceRecords, serviceConfig.Source, sourceConfig.Type, sourceConfig.URI, provenanceMethodForSourceType(sourceConfig.Type), fetches)
	}

	for sourceID, sourceConfig := range cfg.Sources {
		if referencedSources[sourceID] || !sourceConfig.Enabled {
			continue
		}
		policy := cachePolicyForSource(sourceConfig, options.ForceRefresh)
		switch sourceConfig.Type {
		case "apiCatalog":
			result, err := discovery.DiscoverAPICatalog(ctx, fetcher, sourceConfig.URI, policy)
			if err != nil {
				return nil, err
			}
			recordSource(sourceRecords, sourceID, sourceConfig.Type, sourceConfig.URI, string(discovery.ProvenanceRFC9727), sourceFetchesFromDiscovery(result.Provenance.Fetches))
			for _, discoveredService := range result.Services {
				discoveredConfig := config.Service{Source: sourceID}
				fetches, validationState, err := buildServiceCatalog(ctx, catalog, &cfg, options.BaseDir, "", discoveredConfig, config.Source{
					Type:    "serviceRoot",
					URI:     discoveredService.URL,
					Enabled: true,
					Refresh: sourceConfig.Refresh,
				}, fingerprint, fetcher, policy, options.StateDir, options.HTTPClient)
				if err != nil {
					return nil, err
				}
				if validationState != nil {
					mcpValidationStates = append(mcpValidationStates, validationState)
				}
				recordSource(sourceRecords, sourceID, sourceConfig.Type, sourceConfig.URI, string(discovery.ProvenanceRFC9727), fetches)
			}
		case "serviceRoot":
			fetches, validationState, err := buildServiceCatalog(ctx, catalog, &cfg, options.BaseDir, "", config.Service{Source: sourceID}, sourceConfig, fingerprint, fetcher, policy, options.StateDir, options.HTTPClient)
			if err != nil {
				return nil, err
			}
			if validationState != nil {
				mcpValidationStates = append(mcpValidationStates, validationState)
			}
			recordSource(sourceRecords, sourceID, sourceConfig.Type, sourceConfig.URI, string(discovery.ProvenanceRFC8631), fetches)
		case "openapi":
			fetches, validationState, err := buildServiceCatalog(ctx, catalog, &cfg, options.BaseDir, "", config.Service{Source: sourceID}, sourceConfig, fingerprint, fetcher, policy, options.StateDir, options.HTTPClient)
			if err != nil {
				return nil, err
			}
			if validationState != nil {
				mcpValidationStates = append(mcpValidationStates, validationState)
			}
			recordSource(sourceRecords, sourceID, sourceConfig.Type, sourceConfig.URI, string(discovery.ProvenanceExplicit), fetches)
		}
	}

	catalog.Sources = flattenSourceRecords(sourceRecords)
	sort.Slice(catalog.Tools, func(i, j int) bool {
		return catalog.Tools[i].ID < catalog.Tools[j].ID
	})
	if err := validateDisabledMCPPolicyPatterns(cfg, mcpValidationStates, catalog.Tools); err != nil {
		return nil, err
	}

	catalog.SourceFingerprint = hex.EncodeToString(fingerprint.Sum(nil))
	catalog.EffectiveViews = buildEffectiveViews(cfg, catalog.Tools)
	return catalog, nil
}

func recordSource(records map[string]*SourceRecord, id, sourceType, uri, method string, fetches []SourceFetch) {
	record, ok := records[id]
	if !ok {
		record = &SourceRecord{
			ID:   id,
			Type: sourceType,
			URI:  uri,
			Provenance: SourceProvenance{
				Method: method,
				At:     time.Now().UTC(),
			},
		}
		records[id] = record
	}
	record.Provenance.Fetches = append(record.Provenance.Fetches, fetches...)
	if len(record.Provenance.Fetches) > 0 {
		record.Provenance.At = record.Provenance.Fetches[0].FetchedAt
	}
}

func provenanceMethodForSourceType(sourceType string) string {
	switch sourceType {
	case "apiCatalog":
		return string(discovery.ProvenanceRFC9727)
	case "serviceRoot":
		return string(discovery.ProvenanceRFC8631)
	default:
		return string(discovery.ProvenanceExplicit)
	}
}

func flattenSourceRecords(records map[string]*SourceRecord) []SourceRecord {
	ids := make([]string, 0, len(records))
	for id := range records {
		ids = append(ids, id)
	}
	sort.Strings(ids)

	result := make([]SourceRecord, 0, len(ids))
	for _, id := range ids {
		result = append(result, *records[id])
	}
	return result
}

func cachePolicyForSource(source config.Source, forceRefresh bool) cache.Policy {
	policy := cache.Policy{
		AllowStaleOnError: true,
		ForceRefresh:      forceRefresh,
	}
	if source.Refresh != nil {
		policy.MaxAge = time.Duration(source.Refresh.MaxAgeSeconds) * time.Second
		policy.ManualOnly = source.Refresh.ManualOnly
	}
	return policy
}

func sourceFetchesFromDiscovery(fetches []discovery.FetchRecord) []SourceFetch {
	result := make([]SourceFetch, 0, len(fetches))
	for _, fetch := range fetches {
		result = append(result, SourceFetch{
			URL:           fetch.URL,
			FetchedAt:     fetch.FetchedAt,
			Method:        string(fetch.Method),
			RequestMethod: fetch.RequestMethod,
			StatusCode:    fetch.StatusCode,
			CacheOutcome:  fetch.CacheOutcome,
			ETag:          fetch.ETag,
			LastModified:  fetch.LastModified,
			CacheControl:  fetch.CacheControl,
			ExpiresAt:     fetch.ExpiresAt,
			Stale:         fetch.Stale,
		})
	}
	return result
}

func sourceFetchesFromOpenAPIFetches(fetches []openapi.FetchRecord, method string) []SourceFetch {
	result := make([]SourceFetch, 0, len(fetches))
	for _, fetch := range fetches {
		result = append(result, sourceFetchFromOpenAPI(&fetch, method))
	}
	return result
}

func sourceFetchFromOpenAPI(fetchRecord *openapi.FetchRecord, method string) SourceFetch {
	fetch := SourceFetch{
		URL:           fetchRecord.Metadata.URL,
		FetchedAt:     fetchedAt(fetchRecord.Metadata),
		Method:        method,
		RequestMethod: fetchRecord.Metadata.Method,
		StatusCode:    fetchRecord.Metadata.StatusCode,
		CacheOutcome:  string(fetchRecord.Outcome),
		ETag:          fetchRecord.Metadata.ETag,
		LastModified:  fetchRecord.Metadata.LastModified,
		CacheControl:  fetchRecord.Metadata.CacheControl,
		Stale:         fetchRecord.Metadata.Stale,
	}
	if !fetchRecord.Metadata.ExpiresAt.IsZero() {
		expiresAt := fetchRecord.Metadata.ExpiresAt
		fetch.ExpiresAt = &expiresAt
	}
	return fetch
}

func fetchedAt(metadata cache.Metadata) time.Time {
	if !metadata.LastValidatedAt.IsZero() {
		return metadata.LastValidatedAt
	}
	return metadata.CachedAt
}

func loadGuidance(baseDir string, refs []string, fetcher *cache.Fetcher, policy cache.Policy, method string) (map[string]Guidance, []SourceFetch, error) {
	guidance := map[string]Guidance{}
	var fetches []SourceFetch
	for _, ref := range refs {
		data, fetchRecord, err := openapi.ReadReference(context.Background(), openapi.ResolveReference(baseDir, ref), fetcher, policy)
		if err != nil {
			return nil, nil, err
		}

		var manifest skillManifest
		if err := json.Unmarshal(data, &manifest); err != nil {
			return nil, nil, err
		}
		for key, value := range manifest.ToolGuidance {
			guidance[key] = value
		}
		if fetchRecord != nil {
			fetches = append(fetches, sourceFetchFromOpenAPI(fetchRecord, method))
		}
	}
	return guidance, fetches, nil
}

func loadWorkflows(baseDir string, refs []string, bindings map[string]string, fetcher *cache.Fetcher, policy cache.Policy, method string, validator *mcpDisabledValidationState) ([]Workflow, []SourceFetch, error) {
	var workflows []Workflow
	var fetches []SourceFetch
	for _, ref := range refs {
		data, fetchRecord, err := openapi.ReadReference(context.Background(), openapi.ResolveReference(baseDir, ref), fetcher, policy)
		if err != nil {
			return nil, nil, err
		}

		var document workflowDocument
		if err := yaml.Unmarshal(data, &document); err != nil {
			return nil, nil, err
		}
		for _, workflow := range document.Workflows {
			current := Workflow{WorkflowID: workflow.WorkflowID}
			for _, step := range workflow.Steps {
				toolID := bindings[step.OperationID]
				if toolID == "" {
					toolID = bindings[step.OperationPath]
				}
				if toolID == "" {
					if err := validator.disabledWorkflowReferenceError(ref, workflow, step); err != nil {
						return nil, nil, err
					}
					return nil, nil, fmt.Errorf("workflow %q step %q references %s, but no matching tool is available in the catalog", workflow.WorkflowID, step.StepID, workflowReferenceLabel(step))
				}
				current.Steps = append(current.Steps, WorkflowStep{
					StepID: step.StepID,
					ToolID: toolID,
				})
			}
			workflows = append(workflows, current)
		}
		if fetchRecord != nil {
			fetches = append(fetches, sourceFetchFromOpenAPI(fetchRecord, method))
		}
	}
	return workflows, fetches, nil
}

func workflowReferenceLabel(step workflowStepSpec) string {
	switch {
	case step.OperationID != "":
		return fmt.Sprintf("operationId %q", step.OperationID)
	case step.OperationPath != "":
		return fmt.Sprintf("operationPath %q", step.OperationPath)
	default:
		return "an empty workflow reference"
	}
}

func buildTools(service Service, document *openapi3.T, guidance map[string]Guidance, bindings map[string]string, allowBackendMetadata bool) ([]Tool, error) {
	var tools []Tool
	paths := document.Paths.Map()
	sortedPaths := make([]string, 0, len(paths))
	for path := range paths {
		sortedPaths = append(sortedPaths, path)
	}
	sort.Strings(sortedPaths)

	for _, rawPath := range sortedPaths {
		item := paths[rawPath]
		ops := []struct {
			method string
			op     *openapi3.Operation
		}{
			{method: httpMethod("GET"), op: item.Get},
			{method: httpMethod("POST"), op: item.Post},
			{method: httpMethod("PUT"), op: item.Put},
			{method: httpMethod("PATCH"), op: item.Patch},
			{method: httpMethod("DELETE"), op: item.Delete},
		}
		for _, entry := range ops {
			if entry.op == nil {
				continue
			}
			operationID := entry.op.OperationID
			if operationID == "" {
				operationID = strings.ToLower(entry.method) + ":" + rawPath
			}
			toolID := service.ID + ":" + operationID
			if operationBoolExtension(entry.op, "x-cli-ignore") {
				continue
			}
			bindings[operationID] = toolID
			bindings[entry.method+" "+rawPath] = toolID

			group := operationExtension(entry.op, "x-cli-group")
			if group == "" && len(entry.op.Tags) > 0 {
				group = slugify(entry.op.Tags[0])
			}
			if group == "" {
				group = firstPathSegment(rawPath)
			}
			if group == "" {
				group = "misc"
			}

			command := operationExtension(entry.op, "x-cli-name")
			if command == "" {
				command = normalizeCommandName(operationID)
			}
			if command == "" {
				command = inferCommand(entry.method, rawPath)
			}

			pathParams, flags := extractParameters(item.Parameters, entry.op.Parameters)
			safety := deriveSafety(entry.method, entry.op)
			description := operationExtension(entry.op, "x-cli-description")
			if description == "" {
				description = entry.op.Description
			}

			var backend *ToolBackend
			if allowBackendMetadata {
				backend = operationStructExtension[ToolBackend](entry.op, "x-oascli-backend")
			}
			tool := Tool{
				ID:               toolID,
				ServiceID:        service.ID,
				OperationID:      operationID,
				Method:           entry.method,
				Path:             rawPath,
				Group:            group,
				Command:          command,
				Aliases:          operationStringSliceExtension(entry.op, "x-cli-aliases"),
				Summary:          entry.op.Summary,
				Description:      description,
				Hidden:           operationBoolExtension(entry.op, "x-cli-hidden"),
				PathParams:       pathParams,
				Flags:            flags,
				RequestBody:      extractRequestBody(entry.op),
				AuthAlternatives: extractAuthAlternatives(document, entry.op),
				Safety:           safety,
				Output:           operationStructExtension[OutputHints](entry.op, "x-cli-output"),
				Pagination:       operationStructExtension[PaginationHints](entry.op, "x-cli-pagination"),
				Retry:            operationStructExtension[RetryHints](entry.op, "x-cli-retry"),
				Servers:          service.Servers,
				Backend:          backend,
			}
			if currentGuidance, ok := guidance[tool.ID]; ok {
				tool.Guidance = &currentGuidance
			}
			tool.Auth = flattenLegacyAuth(tool.AuthAlternatives)
			tools = append(tools, tool)
		}
	}

	return tools, nil
}

func buildServiceCatalog(ctx context.Context, ntc *NormalizedCatalog, cfg *config.Config, baseDir, serviceID string, serviceConfig config.Service, sourceConfig config.Source, fingerprint hashWriter, fetcher *cache.Fetcher, policy cache.Policy, stateDir string, httpClient *http.Client) ([]SourceFetch, *mcpDisabledValidationState, error) {
	method := provenanceMethodForSourceType(sourceConfig.Type)
	if sourceConfig.Type == "mcp" {
		return buildMCPServiceCatalog(ctx, ntc, cfg, baseDir, serviceID, serviceConfig, sourceConfig, fingerprint, fetcher, policy, stateDir, httpClient)
	}
	openapiRef, metadataRefs, fetches, err := resolveServiceSource(ctx, baseDir, sourceConfig, fetcher, policy)
	if err != nil {
		return nil, nil, err
	}

	serviceConfig.Overlays = append([]string(nil), serviceConfig.Overlays...)
	serviceConfig.Skills = uniqueRefs(serviceConfig.Skills, metadataRefs.skills)
	serviceConfig.Workflows = uniqueRefs(serviceConfig.Workflows, metadataRefs.workflows)
	serviceConfig.Overlays = uniqueRefs(serviceConfig.Overlays, metadataRefs.overlays)

	document, err := openapi.LoadDocument(ctx, baseDir, openapiRef, serviceConfig.Overlays, fetcher, policy)
	if err != nil {
		return nil, nil, err
	}
	fingerprint.Write([]byte(document.Fingerprint))
	fetches = append(fetches, sourceFetchesFromOpenAPIFetches(document.Fetches, method)...)

	if serviceID == "" {
		serviceID = deriveServiceID(document.Document, sourceConfig.URI, sourceConfig.Type)
	}

	guidance, guidanceFetches, err := loadGuidance(baseDir, serviceConfig.Skills, fetcher, policy, method)
	if err != nil {
		return nil, nil, err
	}
	fetches = append(fetches, guidanceFetches...)
	alias := serviceConfig.Alias
	if alias == "" {
		alias = serviceID
	}
	service := Service{
		ID:       serviceID,
		Alias:    alias,
		SourceID: serviceConfig.Source,
		Title:    document.Document.Info.Title,
		Servers:  extractServers(document.Document),
	}
	ntc.Services = append(ntc.Services, service)

	operationBindings := map[string]string{}
	tools, err := buildTools(service, document.Document, guidance, operationBindings, false)
	if err != nil {
		return nil, nil, err
	}
	ntc.Tools = append(ntc.Tools, tools...)

	workflows, workflowFetches, err := loadWorkflows(baseDir, serviceConfig.Workflows, operationBindings, fetcher, policy, method, nil)
	if err != nil {
		return nil, nil, err
	}
	fetches = append(fetches, workflowFetches...)
	ntc.Workflows = append(ntc.Workflows, workflows...)
	return fetches, nil, nil
}

func buildMCPServiceCatalog(ctx context.Context, ntc *NormalizedCatalog, cfg *config.Config, baseDir, serviceID string, serviceConfig config.Service, sourceConfig config.Source, fingerprint hashWriter, fetcher *cache.Fetcher, policy cache.Policy, stateDir string, httpClient *http.Client) ([]SourceFetch, *mcpDisabledValidationState, error) {
	client, err := mcpclient.Open(sourceConfig, cfg.Secrets, cfg.Policy, stateDir, httpClient, ctx)
	if err != nil {
		return nil, nil, err
	}
	defer client.Close()

	descriptors, err := client.ListTools(ctx)
	if err != nil {
		return nil, nil, err
	}

	buildResult, err := mcpopenapi.BuildDocumentResult(serviceID, serviceConfig.Source, sourceConfig.Transport.Type, descriptors, sourceConfig.DisabledTools)
	if err != nil {
		return nil, nil, err
	}
	validation := newMCPDisabledValidationState(serviceConfig.Source, serviceID, buildResult)
	document := buildResult.Document
	document, overlayFetches, err := applyMCPOverlays(ctx, baseDir, document, serviceConfig.Overlays, fetcher, policy, provenanceMethodForSourceType(sourceConfig.Type), validation)
	if err != nil {
		return nil, nil, err
	}

	data, err := json.Marshal(document)
	if err == nil {
		fingerprint.Write(data)
	}

	method := provenanceMethodForSourceType(sourceConfig.Type)
	guidance, guidanceFetches, err := loadGuidance(baseDir, serviceConfig.Skills, fetcher, policy, method)
	if err != nil {
		return nil, nil, err
	}

	alias := serviceConfig.Alias
	if alias == "" {
		alias = serviceID
	}
	service := Service{
		ID:       serviceID,
		Alias:    alias,
		SourceID: serviceConfig.Source,
		Title:    document.Info.Title,
	}
	ntc.Services = append(ntc.Services, service)

	operationBindings := map[string]string{}
	tools, err := buildTools(service, document, guidance, operationBindings, true)
	if err != nil {
		return nil, nil, err
	}
	validation.setFinalTools(tools)
	ntc.Tools = append(ntc.Tools, tools...)

	workflows, workflowFetches, err := loadWorkflows(baseDir, serviceConfig.Workflows, operationBindings, fetcher, policy, method, validation)
	if err != nil {
		return nil, nil, err
	}
	ntc.Workflows = append(ntc.Workflows, workflows...)
	return append(overlayFetches, append(guidanceFetches, workflowFetches...)...), validation, nil
}

type metadataReferences struct {
	overlays  []string
	skills    []string
	workflows []string
}

func resolveServiceSource(ctx context.Context, baseDir string, source config.Source, fetcher *cache.Fetcher, policy cache.Policy) (string, metadataReferences, []SourceFetch, error) {
	switch source.Type {
	case "openapi":
		return source.URI, metadataReferences{}, nil, nil
	case "serviceRoot":
		result, err := discovery.DiscoverServiceRoot(ctx, fetcher, source.URI, policy)
		if err != nil {
			return "", metadataReferences{}, nil, err
		}
		fetches := sourceFetchesFromDiscovery([]discovery.FetchRecord{result.Provenance})
		refs, metadataFetches, err := loadMetadataReferences(ctx, result.MetadataURL, fetcher, policy, string(discovery.ProvenanceRFC8631))
		if err != nil {
			return "", metadataReferences{}, nil, err
		}
		fetches = append(fetches, metadataFetches...)
		return result.OpenAPIURL, refs, fetches, nil
	case "apiCatalog":
		return "", metadataReferences{}, nil, fmt.Errorf("apiCatalog sources must be expanded before resolution")
	default:
		return "", metadataReferences{}, nil, fmt.Errorf("unsupported source type %q", source.Type)
	}
}

func loadMetadataReferences(ctx context.Context, ref string, fetcher *cache.Fetcher, policy cache.Policy, method string) (metadataReferences, []SourceFetch, error) {
	if ref == "" {
		return metadataReferences{}, nil, nil
	}
	data, fetchRecord, err := openapi.ReadReference(ctx, ref, fetcher, policy)
	if err != nil {
		return metadataReferences{}, nil, err
	}

	var document struct {
		Linkset []struct {
			Href string `json:"href"`
			Rel  string `json:"rel"`
		} `json:"linkset"`
	}
	if err := json.Unmarshal(data, &document); err != nil {
		return metadataReferences{}, nil, err
	}

	var refs metadataReferences
	for _, link := range document.Linkset {
		resolvedHref, err := resolveMetadataHref(ref, link.Href)
		if err != nil {
			return metadataReferences{}, nil, err
		}
		switch {
		case strings.Contains(link.Rel, "skill-manifest"):
			refs.skills = append(refs.skills, resolvedHref)
		case strings.Contains(link.Rel, "workflows"):
			refs.workflows = append(refs.workflows, resolvedHref)
		case strings.Contains(link.Rel, "schema-overlay"):
			refs.overlays = append(refs.overlays, resolvedHref)
		}
	}
	var fetches []SourceFetch
	if fetchRecord != nil {
		fetches = append(fetches, sourceFetchFromOpenAPI(fetchRecord, method))
	}
	return refs, fetches, nil
}

func resolveMetadataHref(baseRef, href string) (string, error) {
	base, err := url.Parse(baseRef)
	if err != nil {
		return "", err
	}
	relative, err := url.Parse(href)
	if err != nil {
		return "", err
	}
	return base.ResolveReference(relative).String(), nil
}

func deriveServiceID(document *openapi3.T, sourceURI, sourceType string) string {
	for _, path := range sortedPathKeys(document.Paths.Map()) {
		segment := firstPathSegment(path)
		if segment != "" {
			return segment
		}
	}
	title := slugify(document.Info.Title)
	if title != "" {
		return title
	}
	return slugify(path.Base(sourceURI))
}

func sortedPathKeys(items map[string]*openapi3.PathItem) []string {
	keys := make([]string, 0, len(items))
	for key := range items {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

type hashWriter interface {
	Write([]byte) (int, error)
}

func uniqueRefs(existing, additional []string) []string {
	seen := map[string]struct{}{}
	var values []string
	for _, ref := range append(existing, additional...) {
		if ref == "" {
			continue
		}
		if _, ok := seen[ref]; ok {
			continue
		}
		seen[ref] = struct{}{}
		values = append(values, ref)
	}
	return values
}

func extractAuthAlternatives(document *openapi3.T, operation *openapi3.Operation) []AuthAlternative {
	security := operation.Security
	if security == nil {
		security = &document.Security
	}
	if security == nil {
		return nil
	}

	var alternatives []AuthAlternative
	for _, item := range *security {
		alternative := AuthAlternative{}
		for schemeName := range item {
			schemeRef := document.Components.SecuritySchemes[schemeName]
			if schemeRef == nil || schemeRef.Value == nil {
				continue
			}
			alternative.Requirements = append(alternative.Requirements, AuthRequirement{
				Name:             schemeName,
				Type:             schemeRef.Value.Type,
				Scheme:           schemeRef.Value.Scheme,
				In:               schemeRef.Value.In,
				ParamName:        schemeRef.Value.Name,
				Scopes:           append([]string(nil), item[schemeName]...),
				OAuthFlows:       extractOAuthFlows(schemeRef.Value),
				OpenIDConnectURL: schemeRef.Value.OpenIdConnectUrl,
			})
		}
		if len(alternative.Requirements) > 0 {
			alternatives = append(alternatives, alternative)
		}
	}
	return alternatives
}

func flattenLegacyAuth(alternatives []AuthAlternative) []AuthRequirement {
	seen := map[string]struct{}{}
	var requirements []AuthRequirement
	for _, alternative := range alternatives {
		for _, requirement := range alternative.Requirements {
			key := requirement.Type + "|" + requirement.Name + "|" + requirement.In + "|" + requirement.ParamName + "|" + requirement.Scheme
			if _, ok := seen[key]; ok {
				continue
			}
			seen[key] = struct{}{}
			requirements = append(requirements, requirement)
		}
	}
	return requirements
}

func extractOAuthFlows(scheme *openapi3.SecurityScheme) []OAuthFlow {
	if scheme == nil || scheme.Flows == nil {
		return nil
	}

	var flows []OAuthFlow
	appendFlow := func(mode string, flow *openapi3.OAuthFlow) {
		if flow == nil {
			return
		}
		flows = append(flows, OAuthFlow{
			Mode:             mode,
			AuthorizationURL: flow.AuthorizationURL,
			TokenURL:         flow.TokenURL,
			RefreshURL:       flow.RefreshURL,
		})
	}

	appendFlow("authorizationCode", scheme.Flows.AuthorizationCode)
	appendFlow("clientCredentials", scheme.Flows.ClientCredentials)
	appendFlow("implicit", scheme.Flows.Implicit)
	appendFlow("password", scheme.Flows.Password)
	return flows
}

func extractServers(document *openapi3.T) []string {
	var servers []string
	for _, server := range document.Servers {
		servers = append(servers, server.URL)
	}
	return servers
}

func extractParameters(pathParameters, operationParameters openapi3.Parameters) ([]Parameter, []Parameter) {
	var pathParams []Parameter
	var flags []Parameter
	for _, parameter := range append(pathParameters, operationParameters...) {
		if parameter == nil || parameter.Value == nil {
			continue
		}

		name := parameter.Value.Name
		if override := parameterExtension(parameter.Value, "x-cli-name"); override != "" {
			name = override
		}

		current := Parameter{
			Name:         slugify(name),
			OriginalName: parameter.Value.Name,
			Location:     parameter.Value.In,
			Required:     parameter.Value.Required,
		}

		if parameter.Value.In == "path" {
			pathParams = append(pathParams, current)
			continue
		}
		flags = append(flags, current)
	}
	return pathParams, flags
}

func buildEffectiveViews(cfg config.Config, tools []Tool) []EffectiveView {
	views := []EffectiveView{
		{
			Name:  "discover",
			Mode:  "discover",
			Tools: append([]Tool(nil), tools...),
		},
	}

	profileNames := sortedKeys(cfg.Agents.Profiles)
	for _, name := range profileNames {
		profile := cfg.Agents.Profiles[name]
		toolSet := cfg.Curation.ToolSets[profile.ToolSet]
		view := EffectiveView{Name: name, Mode: profile.Mode}
		for _, tool := range tools {
			if toolAllowed(tool.ID, toolSet) {
				view.Tools = append(view.Tools, tool)
			}
		}
		views = append(views, view)
	}

	return views
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

func deriveSafety(method string, operation *openapi3.Operation) Safety {
	safety := Safety{
		ReadOnly:         method == "GET" || method == "HEAD" || method == "OPTIONS",
		Destructive:      method == "DELETE",
		RequiresApproval: false,
		Idempotent:       method == "GET" || method == "HEAD" || method == "OPTIONS" || method == "PUT" || method == "DELETE",
	}

	if raw, ok := operation.Extensions["x-cli-safety"]; ok {
		data, err := json.Marshal(raw)
		if err == nil {
			_ = json.Unmarshal(data, &safety)
		}
	}
	return safety
}

func operationExtension(operation *openapi3.Operation, key string) string {
	if raw, ok := operation.Extensions[key]; ok {
		switch typed := raw.(type) {
		case string:
			return typed
		}
	}
	return ""
}

func operationStringSliceExtension(operation *openapi3.Operation, key string) []string {
	value, ok := decodeOperationExtension[[]string](operation, key)
	if !ok {
		return nil
	}
	return value
}

func operationBoolExtension(operation *openapi3.Operation, key string) bool {
	value, ok := decodeOperationExtension[bool](operation, key)
	return ok && value
}

func operationStructExtension[T any](operation *openapi3.Operation, key string) *T {
	value, ok := decodeOperationExtension[T](operation, key)
	if !ok {
		return nil
	}
	return &value
}

func decodeOperationExtension[T any](operation *openapi3.Operation, key string) (T, bool) {
	var zero T
	raw, ok := operation.Extensions[key]
	if !ok {
		return zero, false
	}
	data, err := json.Marshal(raw)
	if err != nil {
		return zero, false
	}
	var value T
	if err := json.Unmarshal(data, &value); err != nil {
		return zero, false
	}
	return value, true
}

func parameterExtension(parameter *openapi3.Parameter, key string) string {
	if raw, ok := parameter.Extensions[key]; ok {
		switch typed := raw.(type) {
		case string:
			return typed
		}
	}
	return ""
}

func extractRequestBody(operation *openapi3.Operation) *RequestBody {
	if operation.RequestBody == nil || operation.RequestBody.Value == nil {
		return nil
	}
	body := &RequestBody{Required: operation.RequestBody.Value.Required}
	contentTypes := make([]string, 0, len(operation.RequestBody.Value.Content))
	for mediaType := range operation.RequestBody.Value.Content {
		contentTypes = append(contentTypes, mediaType)
	}
	sort.Strings(contentTypes)
	for _, mediaType := range contentTypes {
		media := operation.RequestBody.Value.Content[mediaType]
		content := RequestBodyContent{MediaType: mediaType}
		if media != nil && media.Schema != nil {
			content.Schema = marshalSchema(media.Schema)
		}
		body.ContentTypes = append(body.ContentTypes, content)
	}
	return body
}

func marshalSchema(ref *openapi3.SchemaRef) map[string]any {
	if ref == nil {
		return nil
	}
	data, err := json.Marshal(ref)
	if err != nil {
		return nil
	}
	var schema map[string]any
	if err := json.Unmarshal(data, &schema); err != nil {
		return nil
	}
	return schema
}

func normalizeCommandName(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}
	return slugify(value)
}

func inferCommand(method, rawPath string) string {
	switch method {
	case "GET":
		if strings.Contains(rawPath, "{") {
			return "get"
		}
		return "list"
	case "POST":
		return "create"
	case "PUT":
		return "update"
	case "PATCH":
		return "patch"
	case "DELETE":
		return "delete"
	default:
		return "run"
	}
}

func firstPathSegment(rawPath string) string {
	parts := strings.Split(strings.Trim(rawPath, "/"), "/")
	for _, part := range parts {
		if part == "" || strings.HasPrefix(part, "{") {
			continue
		}
		return slugify(part)
	}
	return ""
}

func slugify(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return value
	}
	value = strings.ReplaceAll(value, "_", "-")
	var builder strings.Builder
	for _, char := range value {
		switch {
		case char >= 'A' && char <= 'Z':
			if builder.Len() > 0 {
				builder.WriteByte('-')
			}
			builder.WriteRune(char + ('a' - 'A'))
		case (char >= 'a' && char <= 'z') || (char >= '0' && char <= '9'):
			builder.WriteRune(char)
		case char == '-' || char == ' ' || char == '.':
			if builder.Len() > 0 && builder.String()[builder.Len()-1] != '-' {
				builder.WriteByte('-')
			}
		}
	}
	return strings.Trim(builder.String(), "-")
}

func sortedKeys[T any](items map[string]T) []string {
	keys := make([]string, 0, len(items))
	for key := range items {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

func httpMethod(method string) string {
	return method
}
