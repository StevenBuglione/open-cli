package runtime

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	stdruntime "runtime"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/StevenBuglione/open-cli/pkg/audit"
	oauth "github.com/StevenBuglione/open-cli/pkg/auth"
	"github.com/StevenBuglione/open-cli/pkg/catalog"
	"github.com/StevenBuglione/open-cli/pkg/config"
	httpexec "github.com/StevenBuglione/open-cli/pkg/exec"
	"github.com/StevenBuglione/open-cli/pkg/obs"
	"github.com/StevenBuglione/open-cli/pkg/policy"
)

type Options struct {
	AuditPath            string
	CacheDir             string
	StateDir             string
	DefaultConfigPath    string
	InstanceID           string
	RuntimeURL           string
	RuntimeMode          string
	HeartbeatSeconds     int
	MissedHeartbeatLimit int
	ShutdownMode         string
	SessionScope         string
	ShareMode            string
	ShareKeyPresent      bool
	ConfigFingerprint    string
	// GracePeriod is the maximum time to wait for in-flight requests to drain
	// before triggering shutdown when a session lease expires.  Defaults to 5s.
	GracePeriod      time.Duration
	HTTPClient       *http.Client
	Observer         obs.Observer
	KeychainResolver func(string) (string, error)
	Shutdown         func() error
}

type Server struct {
	auditStore           *audit.FileStore
	client               *http.Client
	processSupervisor    *httpexec.ProcessSupervisor
	cacheDir             string
	stateDir             string
	defaultConfigPath    string
	instanceID           string
	runtimeURL           string
	runtimeMode          string
	heartbeatSeconds     int
	missedHeartbeatLimit int
	shutdownMode         string
	sessionScope         string
	shareMode            string
	shareKeyPresent      bool
	configFingerprint    string
	gracePeriod          time.Duration
	observer             obs.Observer
	keychainResolver     func(string) (string, error)
	shutdown             func() error
	leaseMu              sync.Mutex
	leases               map[string]sessionLease
	inflight             atomic.Int64
}

type sessionLease struct {
	ExpiresAt time.Time
	Timer     *time.Timer
}

type authResult struct {
	Enabled      bool
	Principal    string
	Lineage      *audit.DelegationLineage
	Scopes       []string
	AllowedTools map[string]struct{}
}

type runtimeAuthError struct {
	StatusCode int
	Code       string
	Message    string
}

var errRuntimeAttachMismatch = errors.New("runtime_attach_mismatch")

func (e *runtimeAuthError) Error() string {
	if e.Message != "" {
		return e.Message
	}
	return e.Code
}

type introspectionResponse struct {
	Active   bool            `json:"active"`
	Scope    string          `json:"scope,omitempty"`
	Subject  string          `json:"sub,omitempty"`
	ClientID string          `json:"client_id,omitempty"`
	Audience json.RawMessage `json:"aud,omitempty"`
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
	StatusCode  int             `json:"statusCode"`
	Body        json.RawMessage `json:"body,omitempty"`
	Text        string          `json:"text,omitempty"`
	ContentType string          `json:"contentType,omitempty"`
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
	if options.GracePeriod <= 0 {
		options.GracePeriod = 5 * time.Second
	}
	return &Server{
		auditStore:           audit.NewFileStore(options.AuditPath),
		client:               options.HTTPClient,
		processSupervisor:    httpexec.NewProcessSupervisor(firstNonEmpty(options.StateDir, filepath.Dir(options.AuditPath))),
		cacheDir:             options.CacheDir,
		stateDir:             firstNonEmpty(options.StateDir, filepath.Dir(options.AuditPath)),
		defaultConfigPath:    options.DefaultConfigPath,
		instanceID:           options.InstanceID,
		runtimeURL:           options.RuntimeURL,
		runtimeMode:          options.RuntimeMode,
		heartbeatSeconds:     options.HeartbeatSeconds,
		missedHeartbeatLimit: options.MissedHeartbeatLimit,
		shutdownMode:         options.ShutdownMode,
		sessionScope:         options.SessionScope,
		shareMode:            options.ShareMode,
		shareKeyPresent:      options.ShareKeyPresent,
		configFingerprint:    options.ConfigFingerprint,
		gracePeriod:          options.GracePeriod,
		observer:             options.Observer,
		keychainResolver:     options.KeychainResolver,
		shutdown:             options.Shutdown,
		leases:               map[string]sessionLease{},
	}
}

// InflightCount returns the number of non-runtime requests currently in-flight.
// Exposed for testing.
func (server *Server) InflightCount() int64 {
	return server.inflight.Load()
}

func (server *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/v1/catalog/effective", server.handleEffectiveCatalog)
	mux.HandleFunc("/v1/tools/execute", server.handleExecuteTool)
	mux.HandleFunc("/v1/workflows/run", server.handleWorkflowRun)
	mux.HandleFunc("/v1/refresh", server.handleRefresh)
	mux.HandleFunc("/v1/audit/events", server.handleAuditEvents)
	mux.HandleFunc("/v1/auth/browser-config", server.handleBrowserConfig)
	mux.HandleFunc("/v1/runtime/info", server.handleRuntimeInfo)
	mux.HandleFunc("/v1/runtime/heartbeat", server.handleRuntimeHeartbeat)
	mux.HandleFunc("/v1/runtime/stop", server.handleRuntimeStop)
	mux.HandleFunc("/v1/runtime/session-close", server.handleRuntimeSessionClose)
	return server.inflightMiddleware(mux)
}

// inflightMiddleware wraps every request to track the server-level in-flight
// count.  Runtime management endpoints (/v1/runtime/*) are excluded so that
// heartbeat and session-close calls are not counted as business requests.
func (server *Server) inflightMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/v1/runtime/") {
			next.ServeHTTP(w, r)
			return
		}
		server.inflight.Add(1)
		defer server.inflight.Add(-1)
		next.ServeHTTP(w, r)
	})
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
		server.writeConfigSelectionError(w, err)
		return
	}
	authz, err := server.authorizeRequest(ctx, r, cfg.Config, ntc)
	if err != nil {
		finishErr = err
		if authErr, ok := err.(*runtimeAuthError); ok {
			server.recordRuntimeEvent("authn_failure", "", nil, "", "denied", authErr.Code, authErr.StatusCode)
			http.Error(w, authErr.Code, authErr.StatusCode)
			return
		}
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	mode := r.URL.Query().Get("mode")
	agentProfile := r.URL.Query().Get("agentProfile")
	filteredCatalog := ntc
	if authz.Enabled {
		server.recordRuntimeEvent("authenticated_connect", authz.Principal, authz.Lineage, "", "allowed", "authenticated_connect", http.StatusOK)
		filteredCatalog = filterCatalog(ntc, authz.AllowedTools)
		server.recordCatalogEvent(authz.Principal, authz.Lineage, "catalog_filtered")
	}
	view := selectView(cfg.Config, filteredCatalog, mode, agentProfile)
	server.observer.Emit(ctx, obs.Event{Name: "runtime.catalog.effective", Operation: "catalog.effective", StatusCode: http.StatusOK, Duration: time.Since(start), RequestID: requestID})
	writeJSON(w, http.StatusOK, effectiveCatalogResponse{Catalog: filteredCatalog, View: view})
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
		server.writeConfigSelectionError(w, err)
		return
	}
	authz, err := server.authorizeRequest(ctx, r, cfg.Config, ntc)
	if err != nil {
		finishErr = err
		if authErr, ok := err.(*runtimeAuthError); ok {
			http.Error(w, authErr.Code, authErr.StatusCode)
			return
		}
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	tool := ntc.FindTool(request.ToolID)
	if tool == nil {
		finishErr = fmt.Errorf("tool not found")
		server.observer.Emit(ctx, obs.Event{Name: "runtime.tools.execute", Operation: request.ToolID, Duration: time.Since(start), ErrorCategory: "tool_not_found", RequestID: requestID})
		http.Error(w, "tool not found", http.StatusNotFound)
		return
	}
	if authz.Enabled {
		if _, ok := authz.AllowedTools[tool.ID]; !ok {
			server.recordEvent(authz.Principal, authz.Lineage, *tool, request.AgentProfile, policy.Decision{Allowed: false, ReasonCode: "authz_denied"}, 0, 0, 0)
			http.Error(w, "authz_denied", http.StatusForbidden)
			return
		}
	}

	decision := policy.Decide(cfg.Config, *tool, policy.Context{
		Mode:            request.Mode,
		AgentProfile:    request.AgentProfile,
		ApprovalGranted: request.Approval,
	})
	if !decision.Allowed {
		server.recordEvent(authz.Principal, authz.Lineage, *tool, request.AgentProfile, decision, 0, 0, 0)
		server.observer.Emit(ctx, obs.Event{Name: "runtime.tools.execute", Service: tool.ServiceID, Operation: tool.ID, Duration: time.Since(start), ErrorCategory: decision.ReasonCode, RequestID: requestID})
		http.Error(w, decision.ReasonCode, http.StatusForbidden)
		return
	}

	execStart := time.Now()
	result, err := server.executeTool(ctx, cfg.Config, *tool, request)
	if err != nil {
		finishErr = err
		server.recordEvent(authz.Principal, authz.Lineage, *tool, request.AgentProfile, policy.Decision{Allowed: false, ReasonCode: "execution_error"}, 0, 0, 0)
		server.observer.Emit(ctx, obs.Event{Name: "runtime.tools.execute", Service: tool.ServiceID, Operation: tool.ID, Duration: time.Since(execStart), ErrorCategory: "execution_error", RequestID: requestID})
		http.Error(w, err.Error(), http.StatusBadGateway)
		return
	}

	latency := time.Since(execStart)
	server.recordEvent(authz.Principal, authz.Lineage, *tool, request.AgentProfile, decision, result.StatusCode, result.RetryCount, latency)
	server.observer.Emit(ctx, obs.Event{Name: "runtime.tools.execute", Service: tool.ServiceID, Operation: tool.ID, StatusCode: result.StatusCode, Duration: latency, RequestID: requestID})

	response := executeToolResponse{StatusCode: result.StatusCode}
	if json.Valid(result.Body) {
		response.Body = append([]byte(nil), result.Body...)
	} else {
		response.Text = string(result.Body)
	}
	if contentType := result.Headers.Get("Content-Type"); contentType != "" {
		response.ContentType = contentType
	}
	writeJSON(w, http.StatusOK, response)
}

func (server *Server) executeTool(ctx context.Context, cfg config.Config, tool catalog.Tool, request executeToolRequest) (*httpexec.Result, error) {
	if tool.Backend != nil && tool.Backend.Kind == "mcp" {
		if sourceConfig, exists := cfg.Sources[tool.Backend.SourceID]; exists && sourceConfig.Type == "mcp" {
			return httpexec.ExecuteMCP(ctx, httpexec.MCPRequest{
				Tool:              tool,
				Source:            sourceConfig,
				Secrets:           cfg.Secrets,
				Policy:            cfg.Policy,
				StateDir:          server.stateDir,
				HTTPClient:        server.client,
				Body:              request.Body,
				ProcessSupervisor: server.processSupervisor,
			})
		}
	}
	serviceConfig, ok := cfg.Services[tool.ServiceID]
	if ok {
		if sourceConfig, exists := cfg.Sources[serviceConfig.Source]; exists && sourceConfig.Type == "mcp" {
			return httpexec.ExecuteMCP(ctx, httpexec.MCPRequest{
				Tool:              tool,
				Source:            sourceConfig,
				Secrets:           cfg.Secrets,
				Policy:            cfg.Policy,
				StateDir:          server.stateDir,
				HTTPClient:        server.client,
				Body:              request.Body,
				ProcessSupervisor: server.processSupervisor,
			})
		}
	}

	authPlan, err := server.resolveAuthPlan(ctx, cfg, tool)
	if err != nil {
		return nil, err
	}
	return httpexec.Execute(ctx, server.client, httpexec.Request{
		Tool:     tool,
		PathArgs: request.PathArgs,
		Flags:    request.Flags,
		Body:     request.Body,
		AuthPlan: authPlan,
	})
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
		server.writeConfigSelectionError(w, err)
		return
	}
	authz, err := server.authorizeRequest(ctx, r, cfg.Config, ntc)
	if err != nil {
		finishErr = err
		if authErr, ok := err.(*runtimeAuthError); ok {
			http.Error(w, authErr.Code, authErr.StatusCode)
			return
		}
		http.Error(w, err.Error(), http.StatusInternalServerError)
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
			if authz.Enabled {
				if _, ok := authz.AllowedTools[tool.ID]; !ok {
					server.observer.Emit(ctx, obs.Event{Name: "runtime.workflows.run", Operation: request.WorkflowID, Duration: time.Since(start), ErrorCategory: "authz_denied", RequestID: requestID})
					http.Error(w, "authz_denied", http.StatusForbidden)
					return
				}
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

	cfg, ntc, err := server.loadCatalog(ctx, request.ConfigPath, true)
	if err != nil {
		finishErr = err
		server.observer.Emit(ctx, obs.Event{Name: "runtime.refresh", Operation: "refresh", Duration: time.Since(start), ErrorCategory: "refresh_error", RequestID: requestID})
		server.writeConfigSelectionError(w, err)
		return
	}
	authz, err := server.authenticateRequest(ctx, r, cfg.Config)
	if err != nil {
		finishErr = err
		if authErr, ok := err.(*runtimeAuthError); ok {
			server.recordRuntimeEvent("authn_failure", "", nil, "", "denied", authErr.Code, authErr.StatusCode)
			http.Error(w, authErr.Code, authErr.StatusCode)
			return
		}
		http.Error(w, err.Error(), http.StatusInternalServerError)
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

	if authz.Enabled {
		server.recordRuntimeEvent("token_refresh", authz.Principal, authz.Lineage, "", "allowed", "token_refresh", http.StatusOK)
	}
	server.observer.Emit(ctx, obs.Event{Name: "runtime.refresh", Operation: "refresh", StatusCode: http.StatusOK, Duration: time.Since(start), RequestID: requestID})
	writeJSON(w, http.StatusOK, response)
}

func (server *Server) handleAuditEvents(w http.ResponseWriter, r *http.Request) {
	configPath, err := server.boundConfigPath(r.URL.Query().Get("config"))
	if err != nil {
		server.writeConfigSelectionError(w, err)
		return
	}
	if configPath != "" {
		cfg, err := config.LoadEffective(config.LoadOptions{ProjectPath: configPath})
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		if _, err := server.authenticateRequest(r.Context(), r, cfg.Config); err != nil {
			if authErr, ok := err.(*runtimeAuthError); ok {
				http.Error(w, authErr.Code, authErr.StatusCode)
				return
			}
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
	}
	events, err := server.auditStore.List()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, events)
}

func (server *Server) handleBrowserConfig(w http.ResponseWriter, r *http.Request) {
	configPath, err := server.boundConfigPath(r.URL.Query().Get("config"))
	if err != nil {
		server.writeConfigSelectionError(w, err)
		return
	}
	if configPath == "" {
		http.Error(w, "config query parameter is required", http.StatusBadRequest)
		return
	}
	cfg, err := config.LoadEffective(config.LoadOptions{ProjectPath: configPath})
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	authCfg := runtimeServerAuthConfig(cfg.Config)
	if authCfg == nil || authCfg.AuthorizationURL == "" || authCfg.TokenURL == "" || authCfg.BrowserClientID == "" {
		http.Error(w, "browser login is not configured", http.StatusNotFound)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"authorizationURL":        authCfg.AuthorizationURL,
		"tokenURL":                authCfg.TokenURL,
		"clientId":                authCfg.BrowserClientID,
		"audience":                authCfg.Audience,
		"required":                runtimeServerAuthEnabled(authCfg),
		"scopePrefixes":           append([]string(nil), AuthScopePrefixes...),
		"tokenValidationProfiles": configuredTokenValidationProfiles(authCfg),
		"authorizationEnvelope": map[string]any{
			"version":       CurrentAuthorizationEnvelopeVersion,
			"scopePrefixes": append([]string(nil), AuthScopePrefixes...),
		},
	})
}

func (server *Server) handleRuntimeInfo(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	configPath, err := server.boundConfigPath(r.URL.Query().Get("config"))
	if err != nil {
		server.writeConfigSelectionError(w, err)
		return
	}

	var authCfg *config.RuntimeServerAuthConfig
	resolvedAuth := &authResult{}
	if configPath != "" {
		cfg, err := config.LoadEffective(config.LoadOptions{ProjectPath: configPath})
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		authCfg = runtimeServerAuthConfig(cfg.Config)
		if strings.TrimSpace(r.Header.Get("Authorization")) != "" {
			resolvedAuth, err = server.authenticateRequest(r.Context(), r, cfg.Config)
			if err != nil {
				if authErr, ok := err.(*runtimeAuthError); ok {
					http.Error(w, authErr.Code, authErr.StatusCode)
					return
				}
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}
		}
	}
	response := map[string]any{
		"contractVersion": CurrentContractVersion,
		"capabilities":    ServerCapabilities,
		"instanceId":      server.instanceID,
		"url":             server.runtimeURL,
		"runtimeMode":     server.effectiveRuntimeMode(),
		"auditPath":       server.auditStorePath(),
		"stateDir":        server.stateDir,
		"cacheDir":        server.cacheDir,
		"auth":            runtimeAuthHandshakeMetadata(authCfg, resolvedAuth),
	}
	if server.lifecycleEnabled() {
		response["lifecycle"] = map[string]any{
			"capabilities":         []string{"heartbeat", "session-close"},
			"heartbeatSeconds":     server.heartbeatSeconds,
			"missedHeartbeatLimit": server.missedHeartbeatLimit,
			"shutdown":             server.shutdownMode,
			"sessionScope":         server.sessionScope,
			"shareMode":            server.shareMode,
			"shareKeyPresent":      server.shareKeyPresent,
			"configFingerprint":    server.configFingerprint,
			"activeSessions":       server.activeLeaseCount(),
		}
	}
	writeJSON(w, http.StatusOK, response)
}

func (server *Server) effectiveRuntimeMode() string {
	if strings.TrimSpace(server.runtimeMode) != "" {
		return server.runtimeMode
	}
	if server.lifecycleEnabled() {
		return "local"
	}
	return "embedded"
}

func (server *Server) handleRuntimeHeartbeat(w http.ResponseWriter, r *http.Request) {
	if ok := server.requireConfiguredAuth(w, r, false); !ok {
		return
	}
	var request struct {
		SessionID         string `json:"sessionId"`
		ConfigFingerprint string `json:"configFingerprint,omitempty"`
	}
	if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if strings.TrimSpace(request.SessionID) == "" {
		http.Error(w, "sessionId is required", http.StatusBadRequest)
		return
	}
	sessionID := strings.TrimSpace(request.SessionID)
	if server.configFingerprint != "" && request.ConfigFingerprint != "" && request.ConfigFingerprint != server.configFingerprint {
		http.Error(w, "runtime_attach_mismatch", http.StatusConflict)
		return
	}
	if !server.canAttachSession(sessionID) {
		http.Error(w, "runtime_attach_conflict", http.StatusConflict)
		return
	}
	server.renewLease(sessionID)
	writeJSON(w, http.StatusOK, map[string]any{
		"renewed":        true,
		"sessionId":      sessionID,
		"activeSessions": server.activeLeaseCount(),
	})
}

func (server *Server) handleRuntimeStop(w http.ResponseWriter, r *http.Request) {
	if ok := server.requireConfiguredAuth(w, r, false); !ok {
		return
	}
	if server.shutdown == nil {
		http.Error(w, "runtime stop is not configured", http.StatusNotImplemented)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"stopped": true})
	if flusher, ok := w.(http.Flusher); ok {
		flusher.Flush()
	}
	go func() {
		_ = server.shutdown()
	}()
}

func (server *Server) handleRuntimeSessionClose(w http.ResponseWriter, r *http.Request) {
	if ok := server.requireConfiguredAuth(w, r, false); !ok {
		return
	}
	var request struct {
		SessionID string `json:"sessionId"`
	}
	if r.Body != nil {
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil && err != io.EOF {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
	}
	if strings.TrimSpace(request.SessionID) != "" {
		server.removeLease(strings.TrimSpace(request.SessionID))
		server.recordRuntimeEvent("session_close", "", nil, strings.TrimSpace(request.SessionID), "allowed", "session_close", http.StatusOK)
	}
	if err := os.RemoveAll(filepath.Join(server.stateDir, "oauth")); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"closed":         true,
		"activeSessions": server.activeLeaseCount(),
	})
}

func (server *Server) authorizeRequest(ctx context.Context, request *http.Request, cfg config.Config, ntc *catalog.NormalizedCatalog) (*authResult, error) {
	authz, err := server.authenticateRequest(ctx, request, cfg)
	if err != nil {
		return nil, err
	}
	if authz.Enabled {
		authz.AllowedTools = authorizedTools(cfg, ntc, authz.Scopes)
	}
	return authz, nil
}

func (server *Server) authenticateRequest(ctx context.Context, request *http.Request, cfg config.Config) (*authResult, error) {
	authCfg := runtimeServerAuthConfig(cfg)
	if !runtimeServerAuthEnabled(authCfg) {
		return &authResult{}, nil
	}
	token, err := bearerToken(request.Header.Get("Authorization"))
	if err != nil {
		return nil, &runtimeAuthError{StatusCode: http.StatusUnauthorized, Code: "authn_failed", Message: err.Error()}
	}
	principal := ""
	var lineage *audit.DelegationLineage
	var scopes []string
	if runtimeServerAuthUsesOIDCJWKS(authCfg) {
		claims, err := server.validateJWTWithJWKS(ctx, *authCfg, token)
		if err != nil {
			return nil, &runtimeAuthError{StatusCode: http.StatusUnauthorized, Code: "authn_failed", Message: err.Error()}
		}
		principal = firstNonEmpty(claims.Subject, claims.ClientID)
		if principal == "" {
			return nil, &runtimeAuthError{StatusCode: http.StatusUnauthorized, Code: "authn_failed", Message: "oidc_jwks token must contain sub or client_id"}
		}
		scopes = strings.Fields(claims.Scope)
		lineage = claims.delegationLineage()
	} else {
		introspection, err := server.introspectToken(ctx, *authCfg, token)
		if err != nil {
			return nil, &runtimeAuthError{StatusCode: http.StatusUnauthorized, Code: "authn_failed", Message: err.Error()}
		}
		principal = firstNonEmpty(introspection.Subject, introspection.ClientID)
		scopes = strings.Fields(introspection.Scope)
	}
	return &authResult{
		Enabled:   true,
		Principal: principal,
		Lineage:   lineage,
		Scopes:    scopes,
	}, nil
}

func runtimeServerAuthConfig(cfg config.Config) *config.RuntimeServerAuthConfig {
	if cfg.Runtime == nil || cfg.Runtime.Server == nil {
		return nil
	}
	return cfg.Runtime.Server.Auth
}

func runtimeServerAuthEnabled(auth *config.RuntimeServerAuthConfig) bool {
	if auth == nil {
		return false
	}
	if auth.Mode == "oauth2Introspection" {
		return true
	}
	if auth.ValidationProfile == "oauth2_introspection" {
		return true
	}
	return runtimeServerAuthUsesOIDCJWKS(auth)
}

func configuredTokenValidationProfiles(auth *config.RuntimeServerAuthConfig) []string {
	if auth == nil {
		return []string{}
	}
	if auth.ValidationProfile != "" {
		return []string{auth.ValidationProfile}
	}
	if auth.Mode == "oauth2Introspection" {
		return []string{"oauth2_introspection"}
	}
	return []string{}
}

func browserLoginConfigured(auth *config.RuntimeServerAuthConfig) bool {
	return auth != nil && auth.AuthorizationURL != "" && auth.TokenURL != "" && auth.BrowserClientID != ""
}

func runtimeAuthHandshakeMetadata(authCfg *config.RuntimeServerAuthConfig, resolved *authResult) map[string]any {
	metadata := map[string]any{
		"required":                runtimeServerAuthEnabled(authCfg),
		"scopePrefixes":           append([]string(nil), AuthScopePrefixes...),
		"tokenValidationProfiles": configuredTokenValidationProfiles(authCfg),
		"browserLogin": map[string]any{
			"configured":     browserLoginConfigured(authCfg),
			"configEndpoint": "/v1/auth/browser-config",
		},
		"authorizationEnvelope": map[string]any{
			"version":       CurrentAuthorizationEnvelopeVersion,
			"scopePrefixes": append([]string(nil), AuthScopePrefixes...),
		},
	}
	if authCfg != nil && authCfg.Audience != "" {
		metadata["audience"] = authCfg.Audience
	}
	if resolved != nil {
		if resolved.Principal != "" {
			metadata["principal"] = resolved.Principal
		}
	}
	return metadata
}

func bearerToken(header string) (string, error) {
	if header == "" {
		return "", fmt.Errorf("missing bearer token")
	}
	parts := strings.SplitN(header, " ", 2)
	if len(parts) != 2 || !strings.EqualFold(parts[0], "Bearer") || strings.TrimSpace(parts[1]) == "" {
		return "", fmt.Errorf("invalid bearer token")
	}
	return strings.TrimSpace(parts[1]), nil
}

func (server *Server) introspectToken(ctx context.Context, authCfg config.RuntimeServerAuthConfig, token string) (*introspectionResponse, error) {
	form := strings.NewReader("token=" + urlEncode(token))
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, authCfg.IntrospectionURL, form)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	if authCfg.ClientID != nil {
		clientID, err := resolveSecret(config.PolicyConfig{}, config.Secret{Type: authCfg.ClientID.Type, Value: authCfg.ClientID.Value, Command: authCfg.ClientID.Command}, server.keychainResolver)
		if err != nil {
			return nil, err
		}
		req.Form = map[string][]string{"token": {token}, "client_id": {clientID}}
	}
	if authCfg.ClientSecret != nil {
		clientSecret, err := resolveSecret(config.PolicyConfig{}, config.Secret{Type: authCfg.ClientSecret.Type, Value: authCfg.ClientSecret.Value, Command: authCfg.ClientSecret.Command}, server.keychainResolver)
		if err != nil {
			return nil, err
		}
		req.Form.Set("client_secret", clientSecret)
		req.Body = io.NopCloser(strings.NewReader(req.Form.Encode()))
		req.ContentLength = int64(len(req.Form.Encode()))
	}
	resp, err := server.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("token introspection failed with status %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}
	var introspection introspectionResponse
	if err := json.NewDecoder(resp.Body).Decode(&introspection); err != nil {
		return nil, err
	}
	if !introspection.Active {
		return nil, fmt.Errorf("token is inactive")
	}
	if !audienceIncludes(introspection.Audience, authCfg.Audience) {
		return nil, fmt.Errorf("token audience mismatch")
	}
	return &introspection, nil
}

func audienceIncludes(raw json.RawMessage, expected string) bool {
	if expected == "" {
		return true
	}
	var single string
	if err := json.Unmarshal(raw, &single); err == nil {
		return single == expected
	}
	var many []string
	if err := json.Unmarshal(raw, &many); err == nil {
		for _, item := range many {
			if item == expected {
				return true
			}
		}
	}
	return false
}

func authorizedTools(cfg config.Config, ntc *catalog.NormalizedCatalog, scopes []string) map[string]struct{} {
	union := map[string]struct{}{}
	explicit := map[string]struct{}{}
	hasUnion := false
	for _, scope := range scopes {
		switch {
		case strings.HasPrefix(scope, "bundle:"):
			hasUnion = true
			serviceID := strings.TrimPrefix(scope, "bundle:")
			for _, tool := range ntc.Tools {
				if tool.ServiceID == serviceID {
					union[tool.ID] = struct{}{}
				}
			}
		case strings.HasPrefix(scope, "profile:"):
			hasUnion = true
			profileName := strings.TrimPrefix(scope, "profile:")
			for toolID := range authorizedProfileTools(cfg, ntc, profileName) {
				union[toolID] = struct{}{}
			}
		case strings.HasPrefix(scope, "tool:"):
			toolID := strings.TrimPrefix(scope, "tool:")
			if ntc.FindTool(toolID) != nil {
				explicit[toolID] = struct{}{}
			}
		}
	}
	final := map[string]struct{}{}
	switch {
	case len(explicit) > 0 && hasUnion:
		for toolID := range explicit {
			if _, ok := union[toolID]; ok {
				final[toolID] = struct{}{}
			}
		}
	case len(explicit) > 0:
		for toolID := range explicit {
			final[toolID] = struct{}{}
		}
	case hasUnion:
		for toolID := range union {
			final[toolID] = struct{}{}
		}
	default:
		return final
	}
	for toolID := range final {
		if policy.MatchesAny(cfg.Policy.ManagedDeny, toolID) || policy.MatchesAny(cfg.Policy.Deny, toolID) {
			delete(final, toolID)
		}
	}
	return final
}

func authorizedProfileTools(cfg config.Config, ntc *catalog.NormalizedCatalog, profileName string) map[string]struct{} {
	allowed := map[string]struct{}{}
	profile, ok := cfg.Agents.Profiles[profileName]
	if !ok {
		return allowed
	}
	mode := profile.Mode
	if mode == "" {
		mode = cfg.Mode.Default
	}
	if mode != "curated" {
		for _, tool := range ntc.Tools {
			allowed[tool.ID] = struct{}{}
		}
		return allowed
	}
	toolSet := cfg.Curation.ToolSets[profile.ToolSet]
	for _, tool := range ntc.Tools {
		if policy.ToolAllowed(tool.ID, toolSet) {
			allowed[tool.ID] = struct{}{}
		}
	}
	return allowed
}

func filterCatalog(ntc *catalog.NormalizedCatalog, allowed map[string]struct{}) *catalog.NormalizedCatalog {
	filtered := *ntc
	filtered.Tools = make([]catalog.Tool, 0, len(ntc.Tools))
	services := map[string]struct{}{}
	for _, tool := range ntc.Tools {
		if _, ok := allowed[tool.ID]; ok {
			filtered.Tools = append(filtered.Tools, tool)
			services[tool.ServiceID] = struct{}{}
		}
	}
	filtered.Services = make([]catalog.Service, 0, len(ntc.Services))
	for _, service := range ntc.Services {
		if _, ok := services[service.ID]; ok {
			filtered.Services = append(filtered.Services, service)
		}
	}
	filtered.Workflows = make([]catalog.Workflow, 0, len(ntc.Workflows))
	for _, workflow := range ntc.Workflows {
		allowedWorkflow := true
		for _, step := range workflow.Steps {
			if _, ok := allowed[step.ToolID]; !ok {
				allowedWorkflow = false
				break
			}
		}
		if allowedWorkflow {
			filtered.Workflows = append(filtered.Workflows, workflow)
		}
	}
	filtered.EffectiveViews = make([]catalog.EffectiveView, 0, len(ntc.EffectiveViews))
	for _, view := range ntc.EffectiveViews {
		filteredView := catalog.EffectiveView{Name: view.Name, Mode: view.Mode}
		for _, tool := range view.Tools {
			if _, ok := allowed[tool.ID]; ok {
				filteredView.Tools = append(filteredView.Tools, tool)
			}
		}
		filtered.EffectiveViews = append(filtered.EffectiveViews, filteredView)
	}
	return &filtered
}

func (server *Server) recordCatalogEvent(principal string, lineage *audit.DelegationLineage, reason string) {
	_ = server.auditStore.Append(audit.Event{
		Timestamp:  time.Now().UTC(),
		EventType:  "catalog_filtered",
		Principal:  principal,
		Lineage:    cloneDelegationLineage(lineage),
		ToolID:     "catalog.effective",
		Decision:   "allowed",
		ReasonCode: reason,
	})
}

func (server *Server) recordRuntimeEvent(eventType, principal string, lineage *audit.DelegationLineage, sessionID, decision, reason string, statusCode int) {
	toolID := "runtime." + eventType
	if eventType == "session_close" || eventType == "session_expiry" {
		toolID = "runtime.session"
	}
	_ = server.auditStore.Append(audit.Event{
		Timestamp:  time.Now().UTC(),
		EventType:  eventType,
		Principal:  principal,
		Lineage:    cloneDelegationLineage(lineage),
		SessionID:  sessionID,
		ToolID:     toolID,
		Decision:   decision,
		ReasonCode: reason,
		StatusCode: statusCode,
	})
}

func cloneDelegationLineage(lineage *audit.DelegationLineage) *audit.DelegationLineage {
	if lineage == nil {
		return nil
	}
	clone := &audit.DelegationLineage{
		DelegatedBy:  lineage.DelegatedBy,
		DelegationID: lineage.DelegationID,
	}
	if len(lineage.Actor) > 0 {
		clone.Actor = cloneNonEmptyStringMap(lineage.Actor)
	}
	return clone
}

func cloneNonEmptyStringMap(source map[string]string) map[string]string {
	if len(source) == 0 {
		return nil
	}
	cloned := make(map[string]string, len(source))
	for key, value := range source {
		if strings.TrimSpace(key) == "" || value == "" {
			continue
		}
		cloned[key] = value
	}
	if len(cloned) == 0 {
		return nil
	}
	return cloned
}

func urlEncode(value string) string {
	replacer := strings.NewReplacer("%", "%25", "&", "%26", "=", "%3D", "+", "%2B", " ", "%20")
	return replacer.Replace(value)
}

func (server *Server) loadCatalog(ctx context.Context, configPath string, forceRefresh bool) (*config.EffectiveConfig, *catalog.NormalizedCatalog, error) {
	var err error
	configPath, err = server.boundConfigPath(configPath)
	if err != nil {
		return nil, nil, err
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
		StateDir:     server.stateDir,
		ForceRefresh: forceRefresh,
		Observer:     server.observer,
	})
	if err != nil {
		return nil, nil, err
	}
	return cfg, ntc, nil
}

func (server *Server) boundConfigPath(requestConfigPath string) (string, error) {
	if server.effectiveRuntimeMode() != "remote" || strings.TrimSpace(server.defaultConfigPath) == "" {
		return firstNonEmpty(strings.TrimSpace(requestConfigPath), server.defaultConfigPath), nil
	}

	runtimeConfigPath, err := canonicalConfigPath(server.defaultConfigPath)
	if err != nil {
		return "", err
	}
	if strings.TrimSpace(requestConfigPath) == "" {
		return runtimeConfigPath, nil
	}

	requestedConfigPath, err := canonicalConfigPath(requestConfigPath)
	if err != nil {
		return "", err
	}
	if requestedConfigPath != runtimeConfigPath {
		return "", errRuntimeAttachMismatch
	}
	return runtimeConfigPath, nil
}

func canonicalConfigPath(path string) (string, error) {
	absolutePath, err := filepath.Abs(strings.TrimSpace(path))
	if err != nil {
		return "", err
	}
	return filepath.Clean(absolutePath), nil
}

func (server *Server) writeConfigSelectionError(w http.ResponseWriter, err error) {
	if errors.Is(err, errRuntimeAttachMismatch) {
		http.Error(w, errRuntimeAttachMismatch.Error(), http.StatusConflict)
		return
	}
	http.Error(w, err.Error(), http.StatusBadRequest)
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

func (server *Server) recordEvent(principal string, lineage *audit.DelegationLineage, tool catalog.Tool, agentProfile string, decision policy.Decision, statusCode, retryCount int, latency time.Duration) {
	eventType := "tool_execution"
	decisionValue := "allowed"
	if isExecutionErrorReason(decision.ReasonCode) {
		eventType = "execution_error"
		decisionValue = "error"
	} else if !decision.Allowed {
		eventType = "authz_denial"
		decisionValue = "denied"
	}
	_ = server.auditStore.Append(audit.Event{
		Timestamp:     time.Now().UTC(),
		EventType:     eventType,
		Principal:     principal,
		Lineage:       cloneDelegationLineage(lineage),
		AgentProfile:  agentProfile,
		ToolID:        tool.ID,
		ServiceID:     tool.ServiceID,
		TargetBaseURL: first(tool.Servers),
		Decision:      decisionValue,
		ReasonCode:    decision.ReasonCode,
		Method:        tool.Method,
		Path:          tool.Path,
		RequestSize:   0,
		StatusCode:    statusCode,
		RetryCount:    retryCount,
		LatencyMS:     latency.Milliseconds(),
	})
}

func isExecutionErrorReason(reasonCode string) bool {
	if reasonCode == "" {
		return false
	}
	switch reasonCode {
	case "authz_denied", "approval_required", "managed_deny", "curated_deny", "allowed":
		return false
	default:
		return strings.HasSuffix(reasonCode, "_error")
	}
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}

func (server *Server) CloseWithContext(ctx context.Context) error {
	server.leaseMu.Lock()
	for sessionID, lease := range server.leases {
		if lease.Timer != nil {
			lease.Timer.Stop()
		}
		delete(server.leases, sessionID)
	}
	server.leaseMu.Unlock()
	if server.processSupervisor == nil {
		return nil
	}
	return errors.Join(server.processSupervisor.Shutdown(ctx))
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

func (server *Server) resolveAuth(ctx context.Context, cfg config.Config, tool catalog.Tool) ([]httpexec.AuthScheme, error) {
	var authSchemes []httpexec.AuthScheme
	for _, requirement := range tool.Auth {
		secretKey, secret, ok := lookupSecret(cfg.Secrets, tool.ServiceID, requirement.Name)
		if !ok {
			continue
		}
		if requirement.Type == "oauth2" || requirement.Type == "openIdConnect" {
			token, err := oauth.ResolveOAuthAccessToken(ctx, server.client, cfg.Policy, secret, requirement, secretKey, server.stateDir, server.keychainResolver)
			if err != nil {
				return nil, err
			}
			authSchemes = append(authSchemes, httpexec.AuthScheme{
				Type:   "http",
				Scheme: "bearer",
				Value:  token,
			})
			continue
		}

		value, err := resolveSecret(cfg.Policy, secret, server.keychainResolver)
		if err != nil {
			continue
		}
		authSchemes = append(authSchemes, httpexec.AuthScheme{
			Type:   requirement.Type,
			Scheme: requirement.Scheme,
			In:     requirement.In,
			Name:   requirement.ParamName,
			Value:  value,
		})
	}
	return authSchemes, nil
}

func lookupSecret(secrets map[string]config.Secret, serviceID, name string) (string, config.Secret, bool) {
	if serviceID != "" {
		key := serviceID + "." + name
		if secret, ok := secrets[key]; ok {
			return key, secret, true
		}
	}
	secret, ok := secrets[name]
	return name, secret, ok
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ""
}

func (server *Server) auditStorePath() string {
	if server.auditStore == nil {
		return ""
	}
	return server.auditStore.Path()
}

func (server *Server) lifecycleEnabled() bool {
	return server.heartbeatSeconds > 0 && server.missedHeartbeatLimit > 0
}

func (server *Server) leaseTTL() time.Duration {
	return time.Duration(server.heartbeatSeconds*server.missedHeartbeatLimit) * time.Second
}

func (server *Server) activeLeaseCount() int {
	server.leaseMu.Lock()
	defer server.leaseMu.Unlock()
	return len(server.leases)
}

func (server *Server) canAttachSession(sessionID string) bool {
	if server.shareMode == "group" || sessionID == "" {
		return true
	}
	server.leaseMu.Lock()
	defer server.leaseMu.Unlock()
	for existingSessionID := range server.leases {
		if existingSessionID != sessionID {
			return false
		}
	}
	return true
}

func (server *Server) renewLease(sessionID string) {
	if !server.lifecycleEnabled() {
		return
	}
	expiresAt := time.Now().Add(server.leaseTTL())
	server.leaseMu.Lock()
	existing, ok := server.leases[sessionID]
	if ok && existing.Timer != nil {
		existing.Timer.Stop()
	}
	lease := sessionLease{ExpiresAt: expiresAt}
	lease.Timer = time.AfterFunc(server.leaseTTL(), func() {
		server.expireLease(sessionID, expiresAt)
	})
	server.leases[sessionID] = lease
	server.leaseMu.Unlock()
}

func (server *Server) expireLease(sessionID string, expiresAt time.Time) {
	server.leaseMu.Lock()
	lease, ok := server.leases[sessionID]
	if !ok || !lease.ExpiresAt.Equal(expiresAt) {
		server.leaseMu.Unlock()
		return
	}
	delete(server.leases, sessionID)
	remaining := len(server.leases)
	server.leaseMu.Unlock()
	server.recordRuntimeEvent("session_expiry", "", nil, sessionID, "allowed", "session_expiry", 0)
	if remaining == 0 && server.shutdownMode == "when-owner-exits" && server.shutdown != nil {
		server.drainInflightAndShutdown("expiry")
	}
}

func (server *Server) removeLease(sessionID string) {
	server.leaseMu.Lock()
	lease, ok := server.leases[sessionID]
	if ok && lease.Timer != nil {
		lease.Timer.Stop()
		delete(server.leases, sessionID)
	}
	remaining := len(server.leases)
	server.leaseMu.Unlock()
	if ok && remaining == 0 && server.shutdownMode == "when-owner-exits" && server.shutdown != nil {
		server.drainInflightAndShutdown("close")
	}
}

// drainInflightAndShutdown waits up to gracePeriod for in-flight requests to
// complete, then records a lease_expiry_shutdown audit event and calls the
// shutdown hook.  reason is "expiry" or "close".
func (server *Server) drainInflightAndShutdown(reason string) {
	deadline := time.Now().Add(server.gracePeriod)
	for server.inflight.Load() > 0 && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}
	server.recordRuntimeEvent("lease_expiry_shutdown", "", nil, "", "allowed", "lease_expiry_shutdown:"+reason, 0)
	_ = server.shutdown()
}

func (server *Server) requireConfiguredAuth(w http.ResponseWriter, r *http.Request, requireDefaultConfig bool) bool {
	configPath, err := server.boundConfigPath(r.URL.Query().Get("config"))
	if err != nil {
		server.writeConfigSelectionError(w, err)
		return false
	}
	if configPath == "" {
		if requireDefaultConfig {
			http.Error(w, "config query parameter is required", http.StatusBadRequest)
			return false
		}
		return true
	}
	cfg, err := config.LoadEffective(config.LoadOptions{ProjectPath: configPath})
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return false
	}
	if _, err := server.authenticateRequest(r.Context(), r, cfg.Config); err != nil {
		if authErr, ok := err.(*runtimeAuthError); ok {
			http.Error(w, authErr.Code, authErr.StatusCode)
			return false
		}
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return false
	}
	return true
}

func resolveSecret(policyConfig config.PolicyConfig, secret config.Secret, keychainResolver func(string) (string, error)) (string, error) {
	switch secret.Type {
	case "literal":
		return secret.Value, nil
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
		command := append([]string(nil), secret.Command...)
		if len(command) == 0 {
			if secret.Value == "" {
				return "", fmt.Errorf("exec secret requires command or value")
			}
			command = []string{secret.Value}
		}
		output, err := exec.Command(command[0], command[1:]...).Output()
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
