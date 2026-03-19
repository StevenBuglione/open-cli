package runtime

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"

	embeddedruntime "github.com/StevenBuglione/open-cli/internal/runtime"
	"github.com/StevenBuglione/open-cli/pkg/catalog"
	"github.com/StevenBuglione/open-cli/pkg/instance"
)

// CatalogResponse wraps the runtime catalog response.
type CatalogResponse struct {
	Catalog catalog.NormalizedCatalog `json:"catalog"`
	View    catalog.EffectiveView     `json:"view"`
}

// ExecuteRequest is the payload sent to the tools/execute endpoint.
type ExecuteRequest struct {
	ConfigPath   string            `json:"configPath"`
	Mode         string            `json:"mode,omitempty"`
	AgentProfile string            `json:"agentProfile,omitempty"`
	ToolID       string            `json:"toolId"`
	PathArgs     []string          `json:"pathArgs,omitempty"`
	Flags        map[string]string `json:"flags,omitempty"`
	Body         []byte            `json:"body,omitempty"`
	Approval     bool              `json:"approval,omitempty"`
}

// ExecuteResponse is the response from the tools/execute endpoint.
type ExecuteResponse struct {
	StatusCode  int             `json:"statusCode"`
	Body        json.RawMessage `json:"body,omitempty"`
	Text        string          `json:"text,omitempty"`
	ContentType string          `json:"contentType,omitempty"`
}

// CatalogFetchOptions contains the fields needed for catalog fetching.
type CatalogFetchOptions struct {
	ConfigPath   string
	Mode         string
	AgentProfile string
	RuntimeToken string
}

// Client is the interface for interacting with the runtime.
type Client interface {
	FetchCatalog(CatalogFetchOptions) (CatalogResponse, error)
	Execute(ExecuteRequest) (ExecuteResponse, error)
	RunWorkflow(map[string]any) (map[string]any, error)
	RuntimeInfo() (map[string]any, error)
	Heartbeat(string) (map[string]any, error)
	Stop() (map[string]any, error)
	SessionClose() (map[string]any, error)
}

// HTTPClient communicates with a remote runtime over HTTP.
type HTTPClient struct {
	BaseURL           string
	Session           *TokenSession
	SessionID         string
	ConfigFingerprint string
}

func (client HTTPClient) FetchCatalog(options CatalogFetchOptions) (CatalogResponse, error) {
	endpoint, err := url.Parse(client.BaseURL + "/v1/catalog/effective")
	if err != nil {
		return CatalogResponse{}, err
	}
	query := endpoint.Query()
	if options.ConfigPath != "" {
		query.Set("config", options.ConfigPath)
	}
	if options.Mode != "" {
		query.Set("mode", options.Mode)
	}
	if options.AgentProfile != "" {
		query.Set("agentProfile", options.AgentProfile)
	}
	endpoint.RawQuery = query.Encode()
	var response CatalogResponse
	if err := client.do(http.MethodGet, endpoint.String(), nil, &response); err != nil {
		return CatalogResponse{}, err
	}
	return response, nil
}

func (client HTTPClient) Execute(request ExecuteRequest) (ExecuteResponse, error) {
	var response ExecuteResponse
	err := client.do(http.MethodPost, client.BaseURL+"/v1/tools/execute", request, &response)
	return response, err
}

func (client HTTPClient) RunWorkflow(request map[string]any) (map[string]any, error) {
	var response map[string]any
	err := client.do(http.MethodPost, client.BaseURL+"/v1/workflows/run", request, &response)
	return response, err
}

func (client HTTPClient) RuntimeInfo() (map[string]any, error) {
	var response map[string]any
	err := client.do(http.MethodGet, client.BaseURL+"/v1/runtime/info", nil, &response)
	return response, err
}

func (client HTTPClient) Heartbeat(sessionID string) (map[string]any, error) {
	payload := map[string]any{"sessionId": sessionID}
	if client.ConfigFingerprint != "" {
		payload["configFingerprint"] = client.ConfigFingerprint
	}
	var response map[string]any
	err := client.do(http.MethodPost, client.BaseURL+"/v1/runtime/heartbeat", payload, &response)
	return response, err
}

func (client HTTPClient) Stop() (map[string]any, error) {
	var response map[string]any
	err := client.do(http.MethodPost, client.BaseURL+"/v1/runtime/stop", map[string]any{}, &response)
	return response, err
}

func (client HTTPClient) SessionClose() (map[string]any, error) {
	var response map[string]any
	err := client.do(http.MethodPost, client.BaseURL+"/v1/runtime/session-close", map[string]any{"sessionId": client.SessionID}, &response)
	return response, err
}

func (client HTTPClient) do(method, endpoint string, payload any, output any) error {
	var body io.Reader
	if payload != nil {
		data, err := json.Marshal(payload)
		if err != nil {
			return err
		}
		body = bytes.NewReader(data)
	}
	req, err := http.NewRequest(method, endpoint, body)
	if err != nil {
		return err
	}
	if payload != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	token, err := client.Session.TokenForPreflight(req.Context(), TokenRefreshGrace)
	if err != nil {
		return err
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		data, _ := io.ReadAll(resp.Body)
		httpErr := &HTTPError{StatusCode: resp.StatusCode, Body: strings.TrimSpace(string(data))}
		if resp.StatusCode == http.StatusUnauthorized && httpErr.Body == "authn_failed" {
			_ = client.Session.HandleAuthnFailed()
		}
		return httpErr
	}
	return json.NewDecoder(resp.Body).Decode(output)
}

// EmbeddedClient communicates with the embedded runtime via in-process HTTP handler.
type EmbeddedClient struct {
	Handler           http.Handler
	SessionID         string
	ConfigFingerprint string
}

func (client EmbeddedClient) FetchCatalog(options CatalogFetchOptions) (CatalogResponse, error) {
	query := url.Values{}
	if options.ConfigPath != "" {
		query.Set("config", options.ConfigPath)
	}
	if options.Mode != "" {
		query.Set("mode", options.Mode)
	}
	if options.AgentProfile != "" {
		query.Set("agentProfile", options.AgentProfile)
	}
	var response CatalogResponse
	if err := client.do(http.MethodGet, "/v1/catalog/effective?"+query.Encode(), nil, &response); err != nil {
		return CatalogResponse{}, err
	}
	return response, nil
}

func (client EmbeddedClient) Execute(request ExecuteRequest) (ExecuteResponse, error) {
	var response ExecuteResponse
	err := client.do(http.MethodPost, "/v1/tools/execute", request, &response)
	return response, err
}

func (client EmbeddedClient) RunWorkflow(request map[string]any) (map[string]any, error) {
	var response map[string]any
	err := client.do(http.MethodPost, "/v1/workflows/run", request, &response)
	return response, err
}

func (client EmbeddedClient) RuntimeInfo() (map[string]any, error) {
	var response map[string]any
	err := client.do(http.MethodGet, "/v1/runtime/info", nil, &response)
	return response, err
}

func (client EmbeddedClient) Heartbeat(sessionID string) (map[string]any, error) {
	var response map[string]any
	payload := map[string]any{"sessionId": sessionID}
	if client.ConfigFingerprint != "" {
		payload["configFingerprint"] = client.ConfigFingerprint
	}
	err := client.do(http.MethodPost, "/v1/runtime/heartbeat", payload, &response)
	return response, err
}

func (client EmbeddedClient) Stop() (map[string]any, error) {
	var response map[string]any
	err := client.do(http.MethodPost, "/v1/runtime/stop", map[string]any{}, &response)
	return response, err
}

func (client EmbeddedClient) SessionClose() (map[string]any, error) {
	var response map[string]any
	err := client.do(http.MethodPost, "/v1/runtime/session-close", map[string]any{"sessionId": client.SessionID}, &response)
	return response, err
}

func (client EmbeddedClient) do(method, endpoint string, payload any, output any) error {
	var body io.Reader
	if payload != nil {
		data, err := json.Marshal(payload)
		if err != nil {
			return err
		}
		body = bytes.NewReader(data)
	}
	request := httptest.NewRequest(method, endpoint, body)
	if payload != nil {
		request.Header.Set("Content-Type", "application/json")
	}
	recorder := httptest.NewRecorder()
	client.Handler.ServeHTTP(recorder, request)
	response := recorder.Result()
	defer response.Body.Close()
	if response.StatusCode >= 400 {
		data, _ := io.ReadAll(response.Body)
		return fmt.Errorf("%s", strings.TrimSpace(string(data)))
	}
	return json.NewDecoder(response.Body).Decode(output)
}

// NewClient creates a runtime Client based on the given options.
func NewClient(opts NewClientOptions) (Client, error) {
	if opts.Embedded {
		paths, err := instance.Resolve(instance.Options{
			InstanceID: opts.InstanceID,
			ConfigPath: opts.ConfigPath,
			StateRoot:  opts.StateDir,
			CacheRoot:  CacheRootForState(opts.StateDir),
		})
		if err != nil {
			return nil, err
		}
		server := embeddedruntime.NewServer(embeddedruntime.Options{
			AuditPath:         paths.AuditPath,
			CacheDir:          paths.CacheDir,
			DefaultConfigPath: opts.ConfigPath,
			RuntimeMode:       "embedded",
		})
		return EmbeddedClient{Handler: server.Handler(), SessionID: opts.SessionID, ConfigFingerprint: opts.ConfigFingerprint}, nil
	}
	return HTTPClient{BaseURL: opts.RuntimeURL, Session: opts.RuntimeAuth, SessionID: opts.SessionID, ConfigFingerprint: opts.ConfigFingerprint}, nil
}

// NewClientOptions contains the parameters for NewClient.
type NewClientOptions struct {
	Embedded          bool
	RuntimeURL        string
	ConfigPath        string
	InstanceID        string
	StateDir          string
	SessionID         string
	ConfigFingerprint string
	RuntimeAuth       *TokenSession
}
