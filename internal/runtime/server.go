package runtime

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	stdruntime "runtime"
	"strconv"
	"strings"
	"time"

	"github.com/StevenBuglione/oas-cli-go/pkg/audit"
	"github.com/StevenBuglione/oas-cli-go/pkg/catalog"
	"github.com/StevenBuglione/oas-cli-go/pkg/config"
	httpexec "github.com/StevenBuglione/oas-cli-go/pkg/exec"
	"github.com/StevenBuglione/oas-cli-go/pkg/obs"
	"github.com/StevenBuglione/oas-cli-go/pkg/policy"
)

type Options struct {
	AuditPath         string
	CacheDir          string
	DefaultConfigPath string
	HTTPClient        *http.Client
	Observer          obs.Observer
	KeychainResolver  func(string) (string, error)
}

type Server struct {
	auditStore        *audit.FileStore
	client            *http.Client
	cacheDir          string
	defaultConfigPath string
	observer          obs.Observer
	keychainResolver  func(string) (string, error)
}

type effectiveCatalogResponse struct {
	Catalog *catalog.NormalizedCatalog `json:"catalog"`
	View    *catalog.EffectiveView     `json:"view"`
}

type executeToolRequest struct {
	ConfigPath   string            `json:"configPath"`
	Mode         string            `json:"mode,omitempty"`
	AgentProfile string            `json:"agentProfile,omitempty"`
	ToolID       string            `json:"toolId"`
	PathArgs     []string          `json:"pathArgs,omitempty"`
	Flags        map[string]string `json:"flags,omitempty"`
	Body         []byte            `json:"body,omitempty"`
	Approval     bool              `json:"approval,omitempty"`
}

type executeToolResponse struct {
	StatusCode int             `json:"statusCode"`
	Body       json.RawMessage `json:"body,omitempty"`
	Text       string          `json:"text,omitempty"`
}

type workflowRunRequest struct {
	ConfigPath   string `json:"configPath"`
	Mode         string `json:"mode,omitempty"`
	AgentProfile string `json:"agentProfile,omitempty"`
	WorkflowID   string `json:"workflowId"`
	Approval     bool   `json:"approval,omitempty"`
}

type refreshRequest struct {
	ConfigPath string `json:"configPath"`
}

type refreshSourceResult struct {
	ID           string `json:"id"`
	URI          string `json:"uri"`
	CacheOutcome string `json:"cacheOutcome,omitempty"`
	StatusCode   int    `json:"statusCode,omitempty"`
	Stale        bool   `json:"stale,omitempty"`
}

type refreshResponse struct {
	RefreshedAt time.Time             `json:"refreshedAt"`
	Sources     []refreshSourceResult `json:"sources"`
}

func NewServer(options Options) *Server {
	if options.AuditPath == "" {
		options.AuditPath = filepath.Join(".cache", "audit.log")
	}
	if options.CacheDir == "" {
		options.CacheDir = filepath.Join(".cache", "http")
	}
	if options.HTTPClient == nil {
		options.HTTPClient = http.DefaultClient
	}
	if options.Observer == nil {
		options.Observer = obs.NewNop()
	}
	return &Server{
		auditStore:        audit.NewFileStore(options.AuditPath),
		client:            options.HTTPClient,
		cacheDir:          options.CacheDir,
		defaultConfigPath: options.DefaultConfigPath,
		observer:          options.Observer,
		keychainResolver:  options.KeychainResolver,
	}
}

func (server *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/v1/catalog/effective", server.handleEffectiveCatalog)
	mux.HandleFunc("/v1/tools/execute", server.handleExecuteTool)
	mux.HandleFunc("/v1/workflows/run", server.handleWorkflowRun)
	mux.HandleFunc("/v1/refresh", server.handleRefresh)
	mux.HandleFunc("/v1/audit/events", server.handleAuditEvents)
	return mux
}

func (server *Server) handleEffectiveCatalog(w http.ResponseWriter, r *http.Request) {
	requestID := requestID(r)
	start := time.Now()
	ctx, finish := server.observer.StartSpan(r.Context(), "runtime.catalog.effective", map[string]string{
		"requestId": requestID,
	})
	ctx = obs.WithRequestID(ctx, requestID)
	var finishErr error
	defer func() { finish(finishErr) }()

	cfg, ntc, err := server.loadCatalog(ctx, r.URL.Query().Get("config"), false)
	if err != nil {
		finishErr = err
		server.observer.Emit(ctx, obs.Event{Name: "runtime.catalog.effective", Operation: "catalog.effective", Duration: time.Since(start), ErrorCategory: "catalog_load_error", RequestID: requestID})
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	mode := r.URL.Query().Get("mode")
	agentProfile := r.URL.Query().Get("agentProfile")
	view := selectView(cfg.Config, ntc, mode, agentProfile)
	server.observer.Emit(ctx, obs.Event{Name: "runtime.catalog.effective", Operation: "catalog.effective", StatusCode: http.StatusOK, Duration: time.Since(start), RequestID: requestID})
	writeJSON(w, http.StatusOK, effectiveCatalogResponse{Catalog: ntc, View: view})
}

func (server *Server) handleExecuteTool(w http.ResponseWriter, r *http.Request) {
	requestID := requestID(r)
	start := time.Now()
	ctx, finish := server.observer.StartSpan(r.Context(), "runtime.tools.execute", map[string]string{
		"requestId": requestID,
	})
	ctx = obs.WithRequestID(ctx, requestID)
	var finishErr error
	defer func() { finish(finishErr) }()

	var request executeToolRequest
	if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
		finishErr = err
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	cfg, ntc, err := server.loadCatalog(ctx, request.ConfigPath, false)
	if err != nil {
		finishErr = err
		server.observer.Emit(ctx, obs.Event{Name: "runtime.tools.execute", Operation: "tools.execute", Duration: time.Since(start), ErrorCategory: "catalog_load_error", RequestID: requestID})
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	tool := ntc.FindTool(request.ToolID)
	if tool == nil {
		finishErr = fmt.Errorf("tool not found")
		server.observer.Emit(ctx, obs.Event{Name: "runtime.tools.execute", Operation: request.ToolID, Duration: time.Since(start), ErrorCategory: "tool_not_found", RequestID: requestID})
		http.Error(w, "tool not found", http.StatusNotFound)
		return
	}

	decision := policy.Decide(cfg.Config, *tool, policy.Context{
		Mode:            request.Mode,
		AgentProfile:    request.AgentProfile,
		ApprovalGranted: request.Approval,
	})
	if !decision.Allowed {
		server.recordEvent(*tool, request.AgentProfile, decision, 0, 0, 0)
		server.observer.Emit(ctx, obs.Event{Name: "runtime.tools.execute", Service: tool.ServiceID, Operation: tool.ID, Duration: time.Since(start), ErrorCategory: decision.ReasonCode, RequestID: requestID})
		http.Error(w, decision.ReasonCode, http.StatusForbidden)
		return
	}

	execStart := time.Now()
	result, err := httpexec.Execute(ctx, server.client, httpexec.Request{
		Tool:     *tool,
		PathArgs: request.PathArgs,
		Flags:    request.Flags,
		Body:     request.Body,
		Auth:     server.resolveAuth(cfg.Config, *tool),
	})
	if err != nil {
		finishErr = err
		server.recordEvent(*tool, request.AgentProfile, policy.Decision{Allowed: false, ReasonCode: "execution_error"}, 0, 0, 0)
		server.observer.Emit(ctx, obs.Event{Name: "runtime.tools.execute", Service: tool.ServiceID, Operation: tool.ID, Duration: time.Since(execStart), ErrorCategory: "execution_error", RequestID: requestID})
		http.Error(w, err.Error(), http.StatusBadGateway)
		return
	}

	latency := time.Since(execStart)
	server.recordEvent(*tool, request.AgentProfile, decision, result.StatusCode, result.RetryCount, latency)
	server.observer.Emit(ctx, obs.Event{Name: "runtime.tools.execute", Service: tool.ServiceID, Operation: tool.ID, StatusCode: result.StatusCode, Duration: latency, RequestID: requestID})

	response := executeToolResponse{StatusCode: result.StatusCode}
	if json.Valid(result.Body) {
		response.Body = append([]byte(nil), result.Body...)
	} else {
		response.Text = string(result.Body)
	}
	writeJSON(w, http.StatusOK, response)
}

func (server *Server) handleWorkflowRun(w http.ResponseWriter, r *http.Request) {
	requestID := requestID(r)
	start := time.Now()
	ctx, finish := server.observer.StartSpan(r.Context(), "runtime.workflows.run", map[string]string{
		"requestId": requestID,
	})
	ctx = obs.WithRequestID(ctx, requestID)
	var finishErr error
	defer func() { finish(finishErr) }()

	var request workflowRunRequest
	if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
		finishErr = err
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	cfg, ntc, err := server.loadCatalog(ctx, request.ConfigPath, false)
	if err != nil {
		finishErr = err
		server.observer.Emit(ctx, obs.Event{Name: "runtime.workflows.run", Operation: request.WorkflowID, Duration: time.Since(start), ErrorCategory: "catalog_load_error", RequestID: requestID})
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	for _, workflow := range ntc.Workflows {
		if workflow.WorkflowID != request.WorkflowID {
			continue
		}
		var steps []string
		for _, step := range workflow.Steps {
			tool := ntc.FindTool(step.ToolID)
			if tool == nil {
				finishErr = fmt.Errorf("workflow references unknown tool")
				http.Error(w, "workflow references unknown tool", http.StatusBadRequest)
				return
			}
			decision := policy.Decide(cfg.Config, *tool, policy.Context{
				Mode:            request.Mode,
				AgentProfile:    request.AgentProfile,
				ApprovalGranted: request.Approval,
			})
			if !decision.Allowed {
				server.observer.Emit(ctx, obs.Event{Name: "runtime.workflows.run", Operation: request.WorkflowID, Duration: time.Since(start), ErrorCategory: decision.ReasonCode, RequestID: requestID})
				http.Error(w, decision.ReasonCode, http.StatusForbidden)
				return
			}
			steps = append(steps, step.StepID)
		}
		server.observer.Emit(ctx, obs.Event{Name: "runtime.workflows.run", Operation: request.WorkflowID, StatusCode: http.StatusOK, Duration: time.Since(start), RequestID: requestID})
		writeJSON(w, http.StatusOK, map[string]any{
			"workflowId": workflow.WorkflowID,
			"steps":      steps,
		})
		return
	}

	finishErr = fmt.Errorf("workflow not found")
	server.observer.Emit(ctx, obs.Event{Name: "runtime.workflows.run", Operation: request.WorkflowID, Duration: time.Since(start), ErrorCategory: "workflow_not_found", RequestID: requestID})
	http.Error(w, "workflow not found", http.StatusNotFound)
}

func (server *Server) handleRefresh(w http.ResponseWriter, r *http.Request) {
	requestID := requestID(r)
	start := time.Now()
	ctx, finish := server.observer.StartSpan(r.Context(), "runtime.refresh", map[string]string{
		"requestId": requestID,
	})
	ctx = obs.WithRequestID(ctx, requestID)
	var finishErr error
	defer func() { finish(finishErr) }()

	var request refreshRequest
	if r.ContentLength > 0 {
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			finishErr = err
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
	}
	if request.ConfigPath == "" {
		request.ConfigPath = r.URL.Query().Get("config")
	}

	_, ntc, err := server.loadCatalog(ctx, request.ConfigPath, true)
	if err != nil {
		finishErr = err
		server.observer.Emit(ctx, obs.Event{Name: "runtime.refresh", Operation: "refresh", Duration: time.Since(start), ErrorCategory: "refresh_error", RequestID: requestID})
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	response := refreshResponse{RefreshedAt: time.Now().UTC()}
	for _, source := range ntc.Sources {
		item := refreshSourceResult{
			ID:  source.ID,
			URI: source.URI,
		}
		if len(source.Provenance.Fetches) > 0 {
			last := source.Provenance.Fetches[len(source.Provenance.Fetches)-1]
			item.CacheOutcome = last.CacheOutcome
			item.StatusCode = last.StatusCode
			item.Stale = last.Stale
		}
		response.Sources = append(response.Sources, item)
	}

	server.observer.Emit(ctx, obs.Event{Name: "runtime.refresh", Operation: "refresh", StatusCode: http.StatusOK, Duration: time.Since(start), RequestID: requestID})
	writeJSON(w, http.StatusOK, response)
}

func (server *Server) handleAuditEvents(w http.ResponseWriter, _ *http.Request) {
	events, err := server.auditStore.List()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, events)
}

func (server *Server) loadCatalog(ctx context.Context, configPath string, forceRefresh bool) (*config.EffectiveConfig, *catalog.NormalizedCatalog, error) {
	if configPath == "" {
		configPath = server.defaultConfigPath
	}
	if configPath == "" {
		return nil, nil, fmt.Errorf("config query parameter is required")
	}
	if _, err := os.Stat(configPath); err != nil {
		return nil, nil, err
	}

	cfg, err := config.LoadEffective(config.LoadOptions{ProjectPath: configPath})
	if err != nil {
		return nil, nil, err
	}
	ntc, err := catalog.Build(ctx, catalog.BuildOptions{
		Config:       cfg.Config,
		BaseDir:      cfg.BaseDir,
		HTTPClient:   server.client,
		CacheDir:     server.cacheDir,
		ForceRefresh: forceRefresh,
		Observer:     server.observer,
	})
	if err != nil {
		return nil, nil, err
	}
	return cfg, ntc, nil
}

func selectView(cfg config.Config, ntc *catalog.NormalizedCatalog, mode, agentProfile string) *catalog.EffectiveView {
	if agentProfile != "" {
		if view := ntc.EffectiveView(agentProfile); view != nil {
			return view
		}
	}
	if mode == "" {
		mode = cfg.Mode.Default
	}
	if mode == "curated" && cfg.Agents.DefaultProfile != "" {
		if view := ntc.EffectiveView(cfg.Agents.DefaultProfile); view != nil {
			return view
		}
	}
	return ntc.EffectiveView("discover")
}

func (server *Server) recordEvent(tool catalog.Tool, agentProfile string, decision policy.Decision, statusCode, retryCount int, latency time.Duration) {
	_ = server.auditStore.Append(audit.Event{
		Timestamp:     time.Now().UTC(),
		AgentProfile:  agentProfile,
		ToolID:        tool.ID,
		ServiceID:     tool.ServiceID,
		TargetBaseURL: first(tool.Servers),
		Decision:      map[bool]string{true: "allowed", false: "denied"}[decision.Allowed],
		ReasonCode:    decision.ReasonCode,
		Method:        tool.Method,
		Path:          tool.Path,
		RequestSize:   0,
		StatusCode:    statusCode,
		RetryCount:    retryCount,
		LatencyMS:     latency.Milliseconds(),
	})
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}

func first(values []string) string {
	if len(values) == 0 {
		return ""
	}
	return values[0]
}

func requestID(request *http.Request) string {
	if value := request.Header.Get("X-Request-ID"); value != "" {
		return value
	}
	return strconv.FormatInt(time.Now().UTC().UnixNano(), 10)
}

func (server *Server) resolveAuth(cfg config.Config, tool catalog.Tool) []httpexec.AuthScheme {
	var auth []httpexec.AuthScheme
	for _, requirement := range tool.Auth {
		secret, ok := cfg.Secrets[requirement.Name]
		if !ok {
			continue
		}
		value, err := resolveSecret(cfg.Policy, secret, server.keychainResolver)
		if err != nil {
			continue
		}
		auth = append(auth, httpexec.AuthScheme{
			Type:   requirement.Type,
			Scheme: requirement.Scheme,
			In:     requirement.In,
			Name:   requirement.ParamName,
			Value:  value,
		})
	}
	return auth
}

func resolveSecret(policyConfig config.PolicyConfig, secret config.SecretRef, keychainResolver func(string) (string, error)) (string, error) {
	switch secret.Type {
	case "env":
		return os.Getenv(secret.Value), nil
	case "file":
		data, err := os.ReadFile(secret.Value)
		return string(data), err
	case "osKeychain":
		if keychainResolver == nil {
			keychainResolver = defaultKeychainResolver
		}
		return keychainResolver(secret.Value)
	case "exec":
		if !policyConfig.AllowExecSecrets {
			return "", fmt.Errorf("exec secrets are disabled")
		}
		if len(secret.Command) == 0 {
			if secret.Value == "" {
				return "", fmt.Errorf("exec secret requires command or value")
			}
			secret.Command = []string{secret.Value}
		}
		output, err := exec.Command(secret.Command[0], secret.Command[1:]...).Output()
		return string(output), err
	default:
		return "", fmt.Errorf("unsupported secret type %q", secret.Type)
	}
}

func defaultKeychainResolver(reference string) (string, error) {
	service, account, err := splitKeychainReference(reference)
	if err != nil {
		return "", err
	}
	switch stdruntime.GOOS {
	case "darwin":
		output, err := exec.Command("security", "find-generic-password", "-s", service, "-a", account, "-w").Output()
		return strings.TrimSpace(string(output)), err
	case "linux":
		output, err := exec.Command("secret-tool", "lookup", "service", service, "account", account).Output()
		return strings.TrimSpace(string(output)), err
	default:
		return "", fmt.Errorf("osKeychain secrets are unsupported on %s", stdruntime.GOOS)
	}
}

func splitKeychainReference(reference string) (string, string, error) {
	for _, separator := range []string{"/", ":"} {
		if strings.Contains(reference, separator) {
			parts := strings.SplitN(reference, separator, 2)
			if len(parts) == 2 && parts[0] != "" && parts[1] != "" {
				return parts[0], parts[1], nil
			}
		}
	}
	return "", "", fmt.Errorf("osKeychain secret reference must be service/account")
}
