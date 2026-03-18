package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	runtimepkg "github.com/StevenBuglione/open-cli/cmd/ocli/internal/runtime"
	embeddedruntime "github.com/StevenBuglione/open-cli/internal/runtime"
	"github.com/StevenBuglione/open-cli/internal/version"
	oauthruntime "github.com/StevenBuglione/open-cli/pkg/auth"
	"github.com/StevenBuglione/open-cli/pkg/catalog"
	configpkg "github.com/StevenBuglione/open-cli/pkg/config"
	toolsexec "github.com/StevenBuglione/open-cli/pkg/exec"
	"github.com/StevenBuglione/open-cli/pkg/instance"
	"github.com/spf13/cobra"
	"gopkg.in/yaml.v3"
)

type CommandOptions struct {
	RuntimeURL        string
	RuntimeDeployment string
	RuntimeToken      string
	RuntimeAuth       *runtimeTokenSession
	ConfigPath        string
	Mode              string
	AgentProfile      string
	Format            string
	Approval          bool
	InstanceID        string
	SessionID         string
	HeartbeatEnabled  bool
	ConfigFingerprint string
	StateDir          string
	Embedded          bool
	Stdin             io.Reader
	Stdout            io.Writer
	Stderr            io.Writer
}

type runtimeCatalogResponse struct {
	Catalog catalog.NormalizedCatalog `json:"catalog"`
	View    catalog.EffectiveView     `json:"view"`
}

type runtimeBrowserLoginMetadata struct {
	AuthorizationURL string `json:"authorizationURL"`
	TokenURL         string `json:"tokenURL"`
	ClientID         string `json:"clientId"`
	Audience         string `json:"audience,omitempty"`
}

type runtimeBrowserLoginRequest struct {
	Metadata     runtimeBrowserLoginMetadata
	Scopes       []string
	Audience     string
	CallbackPort int
	StateDir     string
}

type executeRequest struct {
	ConfigPath   string            `json:"configPath"`
	Mode         string            `json:"mode,omitempty"`
	AgentProfile string            `json:"agentProfile,omitempty"`
	ToolID       string            `json:"toolId"`
	PathArgs     []string          `json:"pathArgs,omitempty"`
	Flags        map[string]string `json:"flags,omitempty"`
	Body         []byte            `json:"body,omitempty"`
	Approval     bool              `json:"approval,omitempty"`
}

type executeResponse struct {
	StatusCode  int             `json:"statusCode"`
	Body        json.RawMessage `json:"body,omitempty"`
	Text        string          `json:"text,omitempty"`
	ContentType string          `json:"contentType,omitempty"`
}

type runtimeClient interface {
	FetchCatalog(CommandOptions) (runtimeCatalogResponse, error)
	Execute(executeRequest) (executeResponse, error)
	RunWorkflow(map[string]any) (map[string]any, error)
	RuntimeInfo() (map[string]any, error)
	Heartbeat(string) (map[string]any, error)
	Stop() (map[string]any, error)
	SessionClose() (map[string]any, error)
}

const tokenRefreshGrace = 30 * time.Second

type runtimeSessionToken struct {
	AccessToken string
	ExpiresAt   time.Time
}

type runtimeTokenSession struct {
	mu        sync.Mutex
	token     runtimeSessionToken
	refresh   func(context.Context) (runtimeSessionToken, error)
	refreshed bool
}

type runtimeHTTPError = runtimepkg.HTTPError

func newRuntimeTokenSession(token runtimeSessionToken, refresh func(context.Context) (runtimeSessionToken, error)) *runtimeTokenSession {
	return &runtimeTokenSession{token: token, refresh: refresh}
}

func (session *runtimeTokenSession) tokenForPreflight(ctx context.Context, grace time.Duration) (string, error) {
	if session == nil {
		return "", nil
	}
	session.mu.Lock()
	defer session.mu.Unlock()

	if !session.token.isExpiring(grace) {
		return session.token.AccessToken, nil
	}
	if session.refresh == nil || session.refreshed {
		return "", &runtimeHTTPError{StatusCode: http.StatusUnauthorized, Body: "authn_failed"}
	}
	refreshedToken, err := session.refresh(ctx)
	if err != nil {
		return "", &runtimeHTTPError{StatusCode: http.StatusUnauthorized, Body: "authn_failed"}
	}
	session.refreshed = true
	session.token = refreshedToken
	return session.token.AccessToken, nil
}

func (session *runtimeTokenSession) handleAuthnFailed() error {
	if session == nil {
		return &runtimeHTTPError{StatusCode: http.StatusUnauthorized, Body: "authn_failed"}
	}
	session.mu.Lock()
	defer session.mu.Unlock()
	if session.refresh == nil || session.refreshed {
		return &runtimeHTTPError{StatusCode: http.StatusUnauthorized, Body: "authn_failed"}
	}
	session.token.ExpiresAt = time.Unix(0, 0)
	return nil
}

func (token runtimeSessionToken) isExpiring(grace time.Duration) bool {
	if token.ExpiresAt.IsZero() {
		return false
	}
	return time.Now().After(token.ExpiresAt.Add(-grace))
}

type httpRuntimeClient struct {
	baseURL           string
	session           *runtimeTokenSession
	sessionID         string
	configFingerprint string
}

type embeddedRuntimeClient struct {
	handler           http.Handler
	sessionID         string
	configFingerprint string
}

const defaultRuntimeURL = "http://127.0.0.1:8765"

var managedRuntimeStarter = startManagedRuntime
var runtimeBrowserLoginTokenAcquirer = acquireRuntimeBrowserLoginToken
var terminalSessionIdentityProvider = detectTerminalSessionIdentity
var agentSessionIdentityProvider = detectAgentSessionIdentity
var localSessionHandshake = performLocalSessionHandshake
var cleanupManagedProcesses = toolsexec.CleanupManagedProcesses
var runtimeProcessAlive = toolsexec.ProcessAlive

func main() {
	for _, arg := range os.Args[1:] {
		if arg == "--version" || arg == "-v" {
			fmt.Println(version.String())
			return
		}
		if arg == "--" {
			break
		}
	}
	options := bootstrapFromArgs(os.Args[1:])
	command, err := NewRootCommand(options, os.Args[1:])
	if err != nil {
		_, _ = fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	if err := command.Execute(); err != nil {
		_, _ = fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func NewRootCommand(options CommandOptions, args []string) (*cobra.Command, error) {
	var err error
	options, err = resolveCommandOptions(options)
	if err != nil {
		return nil, err
	}
	if options.Format == "" {
		options.Format = "json"
	}
	if options.Stdout == nil {
		options.Stdout = os.Stdout
	}
	if options.Stderr == nil {
		options.Stderr = os.Stderr
	}
	if options.Stdin == nil {
		options.Stdin = os.Stdin
	}

	client, err := newRuntimeClient(options)
	if err != nil {
		return nil, err
	}
	response, err := client.FetchCatalog(options)
	if err != nil {
		return nil, err
	}

	root := &cobra.Command{
		Use:           "ocli",
		Version:       version.Version,
		SilenceUsage:  true,
		SilenceErrors: true,
	}
	root.SetVersionTemplate(version.String() + "\n")
	if options.RuntimeDeployment == "local" && options.HeartbeatEnabled && options.SessionID != "" {
		root.PersistentPreRunE = func(cmd *cobra.Command, _ []string) error {
			if !shouldSendLocalHeartbeat(cmd) {
				return nil
			}
			_, err := client.Heartbeat(options.SessionID)
			return err
		}
		root.PersistentPostRunE = func(cmd *cobra.Command, _ []string) error {
			if !shouldSendLocalHeartbeat(cmd) {
				return nil
			}
			_, err := client.Heartbeat(options.SessionID)
			return err
		}
	}
	root.SetOut(options.Stdout)
	root.SetErr(options.Stderr)
	root.SetIn(options.Stdin)
	root.PersistentFlags().StringVar(&options.RuntimeURL, "runtime", options.RuntimeURL, "Runtime base URL")
	root.PersistentFlags().StringVar(&options.ConfigPath, "config", options.ConfigPath, "Path to .cli.json")
	root.PersistentFlags().StringVar(&options.Mode, "mode", options.Mode, "Execution mode")
	root.PersistentFlags().StringVar(&options.AgentProfile, "agent-profile", options.AgentProfile, "Agent profile")
	root.PersistentFlags().StringVar(&options.Format, "format", options.Format, "Output format")
	root.PersistentFlags().BoolVar(&options.Approval, "approval", options.Approval, "Grant approval for protected tools")
	root.PersistentFlags().StringVar(&options.InstanceID, "instance-id", options.InstanceID, "Instance id for isolated runtime resolution")
	root.PersistentFlags().StringVar(&options.StateDir, "state-dir", options.StateDir, "State directory root for runtime metadata")
	root.PersistentFlags().BoolVar(&options.Embedded, "embedded", options.Embedded, "Use the embedded runtime instead of an external daemon")

	root.AddCommand(newCatalogCommand(options, response))
	root.AddCommand(newToolCommand(options, response))
	root.AddCommand(newExplainCommand(options, response))
	root.AddCommand(newWorkflowCommand(options, client))
	root.AddCommand(newRuntimeCommand(options, client))
	addDynamicToolCommands(root, options, client, response.Catalog.Services, response.View.Tools)
	root.SetArgs(args)
	return root, nil
}

func newCatalogCommand(options CommandOptions, response runtimeCatalogResponse) *cobra.Command {
	command := &cobra.Command{
		Use: "catalog",
	}
	command.AddCommand(&cobra.Command{
		Use:   "list",
		Short: "List the effective catalog",
		RunE: func(cmd *cobra.Command, _ []string) error {
			return writeOutput(options.Stdout, options.Format, response)
		},
	})
	return command
}

func newToolCommand(options CommandOptions, response runtimeCatalogResponse) *cobra.Command {
	command := &cobra.Command{Use: "tool"}
	command.AddCommand(&cobra.Command{
		Use:   "schema <tool-id>",
		Args:  cobra.ExactArgs(1),
		Short: "Render machine-readable tool schema",
		RunE: func(cmd *cobra.Command, args []string) error {
			tool := findTool(response.Catalog.Tools, args[0])
			if tool == nil {
				return fmt.Errorf("tool %s not found", args[0])
			}
			return writeOutput(options.Stdout, options.Format, tool)
		},
	})
	return command
}

func newExplainCommand(options CommandOptions, response runtimeCatalogResponse) *cobra.Command {
	return &cobra.Command{
		Use:   "explain <tool-id>",
		Args:  cobra.ExactArgs(1),
		Short: "Show guidance for a tool",
		RunE: func(cmd *cobra.Command, args []string) error {
			tool := findTool(response.Catalog.Tools, args[0])
			if tool == nil {
				return fmt.Errorf("tool %s not found", args[0])
			}
			if tool.Guidance == nil {
				return writeOutput(options.Stdout, options.Format, map[string]any{"toolId": tool.ID})
			}
			return writeOutput(options.Stdout, options.Format, map[string]any{
				"toolId":    tool.ID,
				"guidance":  tool.Guidance,
				"summary":   tool.Summary,
				"operation": tool.OperationID,
			})
		},
	}
}

func newWorkflowCommand(options CommandOptions, client runtimeClient) *cobra.Command {
	command := &cobra.Command{Use: "workflow"}
	command.AddCommand(&cobra.Command{
		Use:   "run <workflow-id>",
		Args:  cobra.ExactArgs(1),
		Short: "Run a workflow through the runtime",
		RunE: func(cmd *cobra.Command, args []string) error {
			result, err := client.RunWorkflow(map[string]any{
				"configPath":   options.ConfigPath,
				"mode":         options.Mode,
				"agentProfile": options.AgentProfile,
				"workflowId":   args[0],
				"approval":     options.Approval,
			})
			if err != nil {
				return err
			}
			return writeOutput(options.Stdout, options.Format, result)
		},
	})
	return command
}

func newRuntimeCommand(options CommandOptions, client runtimeClient) *cobra.Command {
	command := &cobra.Command{Use: "runtime"}
	command.AddCommand(&cobra.Command{
		Use:   "info",
		Short: "Show runtime metadata",
		RunE: func(cmd *cobra.Command, args []string) error {
			info, err := client.RuntimeInfo()
			if err != nil {
				return err
			}
			return writeOutput(options.Stdout, options.Format, info)
		},
	})
	command.AddCommand(&cobra.Command{
		Use:   "stop",
		Short: "Stop the runtime",
		RunE: func(cmd *cobra.Command, args []string) error {
			result, err := client.Stop()
			if err != nil {
				return err
			}
			return writeOutput(options.Stdout, options.Format, result)
		},
	})
	command.AddCommand(&cobra.Command{
		Use:   "session-close",
		Short: "Close the runtime session and clear session auth state",
		RunE: func(cmd *cobra.Command, args []string) error {
			result, err := client.SessionClose()
			if err != nil {
				return err
			}
			return writeOutput(options.Stdout, options.Format, result)
		},
	})
	return command
}

func addDynamicToolCommands(root *cobra.Command, options CommandOptions, client runtimeClient, services []catalog.Service, tools []catalog.Tool) {
	serviceCommands := map[string]*cobra.Command{}
	groupCommands := map[string]*cobra.Command{}
	serviceAliases := map[string]string{}
	for _, service := range services {
		serviceAliases[service.ID] = service.Alias
	}

	for _, tool := range tools {
		serviceAlias := serviceAliases[tool.ServiceID]
		if serviceAlias == "" {
			serviceAlias = tool.ServiceID
		}
		serviceCommand := serviceCommands[serviceAlias]
		if serviceCommand == nil {
			serviceCommand = &cobra.Command{Use: serviceAlias}
			root.AddCommand(serviceCommand)
			serviceCommands[serviceAlias] = serviceCommand
		}

		groupKey := serviceAlias + ":" + tool.Group
		groupCommand := groupCommands[groupKey]
		if groupCommand == nil {
			groupCommand = &cobra.Command{Use: tool.Group}
			serviceCommand.AddCommand(groupCommand)
			groupCommands[groupKey] = groupCommand
		}

		toolCopy := tool
		command := &cobra.Command{
			Use:     tool.Command,
			Args:    cobra.ExactArgs(len(tool.PathParams)),
			Short:   commandSummary(toolCopy),
			Long:    toolCopy.Description,
			Hidden:  toolCopy.Hidden,
			Aliases: append([]string(nil), toolCopy.Aliases...),
			RunE: func(cmd *cobra.Command, args []string) error {
				flags := map[string]string{}
				for _, flag := range toolCopy.Flags {
					value, err := cmd.Flags().GetString(flag.Name)
					if err != nil {
						return err
					}
					if value != "" {
						flags[flag.Name] = value
					}
				}
				bodyRef, _ := cmd.Flags().GetString("body")
				body, err := loadBody(bodyRef, cmd.InOrStdin())
				if err != nil {
					return err
				}
				result, err := client.Execute(executeRequest{
					ConfigPath:   options.ConfigPath,
					Mode:         options.Mode,
					AgentProfile: options.AgentProfile,
					ToolID:       toolCopy.ID,
					PathArgs:     args,
					Flags:        flags,
					Body:         body,
					Approval:     options.Approval,
				})
				if err != nil {
					return err
				}
				if len(result.Body) > 0 && options.Format == "json" {
					_, err = options.Stdout.Write(append(result.Body, '\n'))
					return err
				}
				if result.Text != "" {
					_, err = fmt.Fprintln(options.Stdout, result.Text)
					return err
				}
				return writeOutput(options.Stdout, options.Format, result)
			},
		}
		for _, flag := range tool.Flags {
			command.Flags().String(flag.Name, "", "parameter "+flag.OriginalName)
		}
		command.Flags().String("body", "", "inline request body")
		groupCommand.AddCommand(command)
	}
}

func commandSummary(tool catalog.Tool) string {
	if tool.Description != "" {
		return tool.Description
	}
	return tool.Summary
}

func loadBody(bodyRef string, stdin io.Reader) ([]byte, error) {
	switch {
	case bodyRef == "":
		return nil, nil
	case bodyRef == "-":
		return io.ReadAll(stdin)
	case strings.HasPrefix(bodyRef, "@"):
		return os.ReadFile(strings.TrimPrefix(bodyRef, "@"))
	default:
		return []byte(bodyRef), nil
	}
}

func fetchCatalogHTTP(baseURL string, options CommandOptions) (runtimeCatalogResponse, error) {
	endpoint, err := url.Parse(baseURL + "/v1/catalog/effective")
	if err != nil {
		return runtimeCatalogResponse{}, err
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
	req, err := http.NewRequest(http.MethodGet, endpoint.String(), nil)
	if err != nil {
		return runtimeCatalogResponse{}, err
	}
	if options.RuntimeToken != "" {
		req.Header.Set("Authorization", "Bearer "+options.RuntimeToken)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return runtimeCatalogResponse{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		body, _ := io.ReadAll(resp.Body)
		return runtimeCatalogResponse{}, fmt.Errorf("%s", strings.TrimSpace(string(body)))
	}
	var response runtimeCatalogResponse
	err = json.NewDecoder(resp.Body).Decode(&response)
	return response, err
}

func (client httpRuntimeClient) FetchCatalog(options CommandOptions) (runtimeCatalogResponse, error) {
	endpoint, err := url.Parse(client.baseURL + "/v1/catalog/effective")
	if err != nil {
		return runtimeCatalogResponse{}, err
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
	var response runtimeCatalogResponse
	if err := client.do(http.MethodGet, endpoint.String(), nil, &response); err != nil {
		return runtimeCatalogResponse{}, err
	}
	return response, nil
}

func (client httpRuntimeClient) Execute(request executeRequest) (executeResponse, error) {
	var response executeResponse
	err := client.do(http.MethodPost, client.baseURL+"/v1/tools/execute", request, &response)
	return response, err
}

func (client httpRuntimeClient) RunWorkflow(request map[string]any) (map[string]any, error) {
	var response map[string]any
	err := client.do(http.MethodPost, client.baseURL+"/v1/workflows/run", request, &response)
	return response, err
}

func (client httpRuntimeClient) RuntimeInfo() (map[string]any, error) {
	var response map[string]any
	err := client.do(http.MethodGet, client.baseURL+"/v1/runtime/info", nil, &response)
	return response, err
}

func (client httpRuntimeClient) Heartbeat(sessionID string) (map[string]any, error) {
	payload := map[string]any{"sessionId": sessionID}
	if client.configFingerprint != "" {
		payload["configFingerprint"] = client.configFingerprint
	}
	var response map[string]any
	err := client.do(http.MethodPost, client.baseURL+"/v1/runtime/heartbeat", payload, &response)
	return response, err
}

func (client httpRuntimeClient) Stop() (map[string]any, error) {
	var response map[string]any
	err := client.do(http.MethodPost, client.baseURL+"/v1/runtime/stop", map[string]any{}, &response)
	return response, err
}

func (client httpRuntimeClient) SessionClose() (map[string]any, error) {
	var response map[string]any
	err := client.do(http.MethodPost, client.baseURL+"/v1/runtime/session-close", map[string]any{"sessionId": client.sessionID}, &response)
	return response, err
}

func (client httpRuntimeClient) do(method, endpoint string, payload any, output any) error {
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
	token, err := client.session.tokenForPreflight(req.Context(), tokenRefreshGrace)
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
		httpErr := &runtimeHTTPError{StatusCode: resp.StatusCode, Body: strings.TrimSpace(string(data))}
		if resp.StatusCode == http.StatusUnauthorized && httpErr.Body == "authn_failed" {
			_ = client.session.handleAuthnFailed()
		}
		return httpErr
	}
	return json.NewDecoder(resp.Body).Decode(output)
}

func (client embeddedRuntimeClient) FetchCatalog(options CommandOptions) (runtimeCatalogResponse, error) {
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
	var response runtimeCatalogResponse
	if err := client.do(http.MethodGet, "/v1/catalog/effective?"+query.Encode(), nil, &response); err != nil {
		return runtimeCatalogResponse{}, err
	}
	return response, nil
}

func (client embeddedRuntimeClient) Execute(request executeRequest) (executeResponse, error) {
	var response executeResponse
	err := client.do(http.MethodPost, "/v1/tools/execute", request, &response)
	return response, err
}

func (client embeddedRuntimeClient) RunWorkflow(request map[string]any) (map[string]any, error) {
	var response map[string]any
	err := client.do(http.MethodPost, "/v1/workflows/run", request, &response)
	return response, err
}

func (client embeddedRuntimeClient) RuntimeInfo() (map[string]any, error) {
	var response map[string]any
	err := client.do(http.MethodGet, "/v1/runtime/info", nil, &response)
	return response, err
}

func (client embeddedRuntimeClient) Heartbeat(sessionID string) (map[string]any, error) {
	var response map[string]any
	payload := map[string]any{"sessionId": sessionID}
	if client.configFingerprint != "" {
		payload["configFingerprint"] = client.configFingerprint
	}
	err := client.do(http.MethodPost, "/v1/runtime/heartbeat", payload, &response)
	return response, err
}

func (client embeddedRuntimeClient) Stop() (map[string]any, error) {
	var response map[string]any
	err := client.do(http.MethodPost, "/v1/runtime/stop", map[string]any{}, &response)
	return response, err
}

func (client embeddedRuntimeClient) SessionClose() (map[string]any, error) {
	var response map[string]any
	err := client.do(http.MethodPost, "/v1/runtime/session-close", map[string]any{"sessionId": client.sessionID}, &response)
	return response, err
}

func (client embeddedRuntimeClient) do(method, endpoint string, payload any, output any) error {
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
	client.handler.ServeHTTP(recorder, request)
	response := recorder.Result()
	defer response.Body.Close()
	if response.StatusCode >= 400 {
		data, _ := io.ReadAll(response.Body)
		return fmt.Errorf("%s", strings.TrimSpace(string(data)))
	}
	return json.NewDecoder(response.Body).Decode(output)
}

func postJSON[T any](endpoint string, payload any, token string) (T, error) {
	return runtimepkg.PostJSON[T](endpoint, payload, token)
}

func getJSON[T any](endpoint, token string) (T, error) {
	return runtimepkg.GetJSON[T](endpoint, token)
}

func writeOutput(out io.Writer, format string, value any) error {
	switch format {
	case "", "json":
		data, err := json.Marshal(value)
		if err != nil {
			return err
		}
		_, err = out.Write(append(data, '\n'))
		return err
	case "yaml":
		data, err := yaml.Marshal(value)
		if err != nil {
			return err
		}
		_, err = out.Write(data)
		return err
	case "pretty":
		data, err := json.MarshalIndent(value, "", "  ")
		if err != nil {
			return err
		}
		_, err = out.Write(append(data, '\n'))
		return err
	default:
		return fmt.Errorf("unsupported format %q", format)
	}
}

func bootstrapFromArgs(args []string) CommandOptions {
	options := CommandOptions{
		Stdout: os.Stdout,
		Stderr: os.Stderr,
	}
	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "--runtime":
			if i+1 < len(args) {
				options.RuntimeURL = args[i+1]
				i++
			}
		case "--config":
			if i+1 < len(args) {
				options.ConfigPath = args[i+1]
				i++
			}
		case "--mode":
			if i+1 < len(args) {
				options.Mode = args[i+1]
				i++
			}
		case "--agent-profile":
			if i+1 < len(args) {
				options.AgentProfile = args[i+1]
				i++
			}
		case "--format":
			if i+1 < len(args) {
				options.Format = args[i+1]
				i++
			}
		case "--approval":
			options.Approval = true
		case "--instance-id":
			if i+1 < len(args) {
				options.InstanceID = args[i+1]
				i++
			}
		case "--state-dir":
			if i+1 < len(args) {
				options.StateDir = args[i+1]
				i++
			}
		case "--embedded":
			options.Embedded = true
		}
	}
	return options
}

func findTool(tools []catalog.Tool, id string) *catalog.Tool {
	for idx := range tools {
		if tools[idx].ID == id {
			return &tools[idx]
		}
	}
	return nil
}

func sortedServiceAliases(services []catalog.Service) []string {
	aliases := make([]string, 0, len(services))
	for _, service := range services {
		aliases = append(aliases, service.Alias)
	}
	sort.Strings(aliases)
	return aliases
}

func resolveCommandOptions(options CommandOptions) (CommandOptions, error) {
	var cachedRuntimeCfg *configpkg.RuntimeConfig
	cachedRuntimeCfgLoaded := false
	loadCachedRuntimeConfig := func() (*configpkg.RuntimeConfig, bool) {
		if cachedRuntimeCfgLoaded {
			return cachedRuntimeCfg, cachedRuntimeCfg != nil
		}
		cachedRuntimeCfg, cachedRuntimeCfgLoaded = loadRuntimeConfig(options)
		return cachedRuntimeCfg, cachedRuntimeCfg != nil
	}
	if options.InstanceID == "" {
		options.InstanceID = os.Getenv("OCLI_INSTANCE_ID")
	}
	if options.StateDir == "" {
		options.StateDir = os.Getenv("OCLI_STATE_DIR")
	}
	if !options.Embedded {
		options.Embedded = envBool("OCLI_EMBEDDED")
	}
	if options.Embedded {
		options.RuntimeDeployment = "embedded"
		return options, nil
	}
	if options.RuntimeDeployment == "" {
		options.RuntimeDeployment = resolveRuntimeDeployment(options)
	}
	if options.RuntimeDeployment == "embedded" {
		options.Embedded = true
		return options, nil
	}
	if options.RuntimeDeployment == "local" && options.InstanceID == "" {
		if runtimeCfg, ok := loadCachedRuntimeConfig(); ok && runtimeCfg.Local != nil {
			options.InstanceID = resolveLocalRuntimeInstanceID(options, *runtimeCfg.Local)
		}
	}
	if options.RuntimeDeployment == "local" && options.SessionID == "" {
		if runtimeCfg, ok := loadCachedRuntimeConfig(); ok && runtimeCfg.Local != nil {
			options.SessionID = resolveLocalSessionID(options, *runtimeCfg.Local)
		}
		if options.SessionID == "" {
			options.SessionID = options.InstanceID
		}
	}
	if options.Embedded {
		return options, nil
	}
	if options.RuntimeURL == "" {
		options.RuntimeURL = os.Getenv("OCLI_RUNTIME_URL")
	}
	if options.RuntimeURL == "" && options.RuntimeDeployment == "remote" {
		if runtimeCfg, ok := loadCachedRuntimeConfig(); ok && runtimeCfg.Remote != nil && runtimeCfg.Remote.URL != "" {
			options.RuntimeURL = runtimeCfg.Remote.URL
		}
	}
	if options.RuntimeToken == "" && options.RuntimeDeployment == "remote" {
		if runtimeCfg, ok := loadCachedRuntimeConfig(); ok && runtimeCfg.Remote != nil && runtimeCfg.Remote.OAuth != nil {
			token, session, err := resolveRuntimeToken(options, *runtimeCfg.Remote.OAuth)
			if err != nil {
				return options, err
			}
			options.RuntimeToken = token
			options.RuntimeAuth = session
		}
	}
	if options.RuntimeURL == "" {
		if runtimeURL, ok, err := resolveRuntimeURLFromInstance(options); err != nil {
			return options, err
		} else if ok {
			options.RuntimeURL = runtimeURL
		}
	}
	if options.RuntimeURL == "" && options.RuntimeDeployment == "local" {
		runtimeURL, err := managedRuntimeStarter(options)
		if err != nil {
			return options, err
		}
		options.RuntimeURL = runtimeURL
	}
	if options.RuntimeURL == "" {
		options.RuntimeURL = defaultRuntimeURL
	}
	if options.RuntimeDeployment == "local" && options.RuntimeURL != "" {
		handshaken, err := localSessionHandshake(options)
		if err != nil {
			return options, err
		}
		options = handshaken
	}
	return options, nil
}

func resolveRuntimeDeployment(options CommandOptions) string {
	runtimeCfg, ok := loadRuntimeConfig(options)
	if !ok {
		return ""
	}
	mode := "auto"
	if runtimeCfg != nil && runtimeCfg.Mode != "" {
		mode = runtimeCfg.Mode
	}
	switch mode {
	case "embedded", "local", "remote":
		return mode
	case "auto":
		effective, err := configpkg.LoadEffective(configpkg.LoadOptions{
			ProjectPath: options.ConfigPath,
			WorkingDir:  filepath.Dir(options.ConfigPath),
		})
		if err != nil {
			return ""
		}
		if hasLocalMCPSource(effective.Config) {
			return "local"
		}
		return "embedded"
	default:
		return ""
	}
}

func loadRuntimeConfig(options CommandOptions) (*configpkg.RuntimeConfig, bool) {
	if options.ConfigPath == "" {
		return nil, false
	}
	effective, err := configpkg.LoadEffective(configpkg.LoadOptions{
		ProjectPath: options.ConfigPath,
		WorkingDir:  filepath.Dir(options.ConfigPath),
	})
	if err != nil {
		return nil, false
	}
	return effective.Config.Runtime, true
}

func hasLocalMCPSource(cfg configpkg.Config) bool {
	for _, source := range cfg.Sources {
		if source.Type != "mcp" || source.Transport == nil {
			continue
		}
		if source.Transport.Type == "stdio" {
			return true
		}
	}
	return false
}

func resolveRuntimeToken(options CommandOptions, oauth configpkg.RemoteOAuthConfig) (string, *runtimeTokenSession, error) {
	var runtimeHandshake embeddedruntime.HandshakeInfo
	if options.RuntimeURL != "" && (oauth.Mode == "oauthClient" || oauth.Mode == "browserLogin") {
		info, err := fetchRuntimeHandshake(options.RuntimeURL)
		if err != nil {
			return "", nil, err
		}
		runtimeHandshake = info
	}
	switch oauth.Mode {
	case "", "providedToken":
		token := ""
		if strings.HasPrefix(oauth.TokenRef, "env:") {
			token = os.Getenv(strings.TrimPrefix(oauth.TokenRef, "env:"))
		}
		return token, newRuntimeTokenSession(runtimeSessionToken{AccessToken: token}, nil), nil
	case "oauthClient":
		if oauth.Client == nil {
			return "", nil, fmt.Errorf("runtime.remote.oauth.client is required for oauthClient mode")
		}
		effectiveOAuth := oauth
		if effectiveOAuth.Audience == "" && runtimeHandshake.Auth != nil {
			effectiveOAuth.Audience = runtimeHandshake.Auth.Audience
		}
		acquire := func(ctx context.Context) (runtimeSessionToken, error) {
			return resolveRuntimeOAuthClientToken(ctx, effectiveOAuth)
		}
		token, err := acquire(context.Background())
		if err != nil {
			return "", nil, err
		}
		return token.AccessToken, newRuntimeTokenSession(token, acquire), nil
	case "browserLogin":
		if options.RuntimeURL == "" {
			return "", nil, fmt.Errorf("runtime URL is required for browserLogin mode")
		}
		browserConfigEndpoint := "/v1/auth/browser-config"
		if runtimeHandshake.Auth != nil && runtimeHandshake.Auth.BrowserLogin != nil {
			if !runtimeHandshake.Auth.BrowserLogin.Configured {
				return "", nil, fmt.Errorf("runtime browser login is not configured")
			}
			if endpoint := strings.TrimSpace(runtimeHandshake.Auth.BrowserLogin.ConfigEndpoint); endpoint != "" {
				browserConfigEndpoint = endpoint
			}
		}
		metadata, err := fetchRuntimeBrowserLoginMetadata(options.RuntimeURL, browserConfigEndpoint)
		if err != nil {
			return "", nil, err
		}
		paths, err := resolveInstancePaths(options)
		if err != nil {
			return "", nil, err
		}
		request := runtimeBrowserLoginRequest{
			Metadata: metadata,
			Scopes:   append([]string(nil), oauth.Scopes...),
			Audience: metadata.Audience,
			StateDir: paths.StateDir,
		}
		if request.Audience == "" && runtimeHandshake.Auth != nil {
			request.Audience = runtimeHandshake.Auth.Audience
		}
		if oauth.Audience != "" {
			request.Audience = oauth.Audience
		}
		if strings.TrimSpace(request.Audience) == "" {
			return "", nil, fmt.Errorf("runtime browser login metadata missing audience")
		}
		if oauth.BrowserLogin != nil {
			request.CallbackPort = oauth.BrowserLogin.CallbackPort
		}
		token, err := runtimeBrowserLoginTokenAcquirer(request)
		if err != nil {
			return "", nil, err
		}
		return token, newRuntimeTokenSession(runtimeSessionToken{AccessToken: token}, nil), nil
	default:
		return "", nil, fmt.Errorf("runtime.remote.oauth.mode %q is not supported yet", oauth.Mode)
	}
}

func resolveRuntimeOAuthClientToken(ctx context.Context, oauth configpkg.RemoteOAuthConfig) (runtimeSessionToken, error) {
	if oauth.Client == nil {
		return runtimeSessionToken{}, fmt.Errorf("runtime.remote.oauth.client is required for oauthClient mode")
	}
	clientID, err := resolveRuntimeOAuthSecret(oauth.Client.ClientID)
	if err != nil {
		return runtimeSessionToken{}, fmt.Errorf("resolve runtime oauth clientId: %w", err)
	}
	clientSecret, err := resolveRuntimeOAuthSecret(oauth.Client.ClientSecret)
	if err != nil {
		return runtimeSessionToken{}, fmt.Errorf("resolve runtime oauth clientSecret: %w", err)
	}
	form := url.Values{}
	form.Set("grant_type", "client_credentials")
	form.Set("client_id", clientID)
	form.Set("client_secret", clientSecret)
	if len(oauth.Scopes) > 0 {
		form.Set("scope", strings.Join(oauth.Scopes, " "))
	}
	if oauth.Audience != "" {
		form.Set("audience", oauth.Audience)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, oauth.Client.TokenURL, strings.NewReader(form.Encode()))
	if err != nil {
		return runtimeSessionToken{}, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return runtimeSessionToken{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		body, _ := io.ReadAll(resp.Body)
		return runtimeSessionToken{}, fmt.Errorf("runtime oauth token request failed with status %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}
	var token struct {
		AccessToken string `json:"access_token"`
		ExpiresIn   int    `json:"expires_in"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&token); err != nil {
		return runtimeSessionToken{}, err
	}
	if token.AccessToken == "" {
		return runtimeSessionToken{}, fmt.Errorf("runtime oauth token response missing access_token")
	}
	expiresAt := time.Time{}
	if token.ExpiresIn > 0 {
		expiresAt = time.Now().Add(time.Duration(token.ExpiresIn) * time.Second)
	}
	return runtimeSessionToken{AccessToken: token.AccessToken, ExpiresAt: expiresAt}, nil
}

func resolveRuntimeOAuthSecret(secret *configpkg.SecretRef) (string, error) {
	if secret == nil {
		return "", fmt.Errorf("missing secret reference")
	}
	return oauthruntime.ResolveStaticSecret(configpkg.PolicyConfig{}, configpkg.Secret{
		Type:    secret.Type,
		Value:   secret.Value,
		Command: append([]string(nil), secret.Command...),
	}, nil)
}

func resolveLocalRuntimeInstanceID(options CommandOptions, local configpkg.LocalRuntimeConfig) string {
	baseID := instance.DeriveID("", options.ConfigPath)
	switch local.SessionScope {
	case "shared-group":
		if local.ShareKey == "" {
			return baseID
		}
		return instance.DeriveID(baseID+"-"+local.ShareKey, "")
	case "terminal":
		identity := terminalSessionIdentityProvider()
		if identity == "" {
			identity = "terminal"
		}
		return instance.DeriveID(baseID+"-"+identity, "")
	case "agent":
		identity := agentSessionIdentityProvider()
		if identity == "" {
			identity = terminalSessionIdentityProvider()
		}
		if identity == "" {
			identity = "agent"
		}
		return instance.DeriveID(baseID+"-"+identity, "")
	default:
		return baseID
	}
}

func resolveLocalSessionID(options CommandOptions, local configpkg.LocalRuntimeConfig) string {
	switch local.SessionScope {
	case "agent":
		if identity := agentSessionIdentityProvider(); identity != "" {
			return instance.DeriveID(identity, "")
		}
	case "terminal":
		if identity := terminalSessionIdentityProvider(); identity != "" {
			return instance.DeriveID(identity, "")
		}
	case "shared-group":
		if identity := agentSessionIdentityProvider(); identity != "" {
			return instance.DeriveID(identity, "")
		}
		if identity := terminalSessionIdentityProvider(); identity != "" {
			return instance.DeriveID(identity, "")
		}
	}
	if options.InstanceID != "" {
		return options.InstanceID
	}
	return instance.DeriveID("local-session-"+options.ConfigPath, "")
}

func performLocalSessionHandshake(options CommandOptions) (CommandOptions, error) {
	if options.ConfigFingerprint == "" {
		options.ConfigFingerprint = localRuntimeConfigFingerprint(options)
	}
	client, err := newRuntimeClient(options)
	if err != nil {
		return options, err
	}
	info, err := client.RuntimeInfo()
	if err != nil {
		return options, err
	}
	if err := validateRuntimeContract(info, []string{"catalog"}); err != nil {
		return options, err
	}
	lifecycle, _ := info["lifecycle"].(map[string]any)
	if !lifecycleCapabilityEnabled(lifecycle, "heartbeat") {
		options.HeartbeatEnabled = false
		return options, nil
	}
	if fingerprint, _ := lifecycle["configFingerprint"].(string); fingerprint != "" && options.ConfigFingerprint != "" && fingerprint != options.ConfigFingerprint {
		return options, fmt.Errorf("runtime_attach_mismatch")
	}
	if options.SessionID == "" {
		options.SessionID = options.InstanceID
	}
	if _, err := client.Heartbeat(options.SessionID); err != nil {
		return options, err
	}
	options.HeartbeatEnabled = true
	return options, nil
}

func validateRuntimeContract(info map[string]any, requiredCapabilities []string) error {
	contractVersion, _ := info["contractVersion"].(string)
	if strings.TrimSpace(contractVersion) == "" {
		return nil
	}
	server := embeddedruntime.HandshakeInfo{
		ContractVersion: contractVersion,
		Capabilities:    stringSlice(info["capabilities"]),
	}
	client := embeddedruntime.HandshakeInfo{
		ContractVersion:      embeddedruntime.CurrentContractVersion,
		RequiredCapabilities: append([]string(nil), requiredCapabilities...),
	}
	return embeddedruntime.CheckCompatibility(client, server)
}

func stringSlice(value any) []string {
	switch items := value.(type) {
	case []string:
		return append([]string(nil), items...)
	case []any:
		result := make([]string, 0, len(items))
		for _, item := range items {
			if text, ok := item.(string); ok && strings.TrimSpace(text) != "" {
				result = append(result, text)
			}
		}
		return result
	default:
		return nil
	}
}

func lifecycleCapabilityEnabled(lifecycle map[string]any, capability string) bool {
	if lifecycle == nil {
		return false
	}
	switch capabilities := lifecycle["capabilities"].(type) {
	case []any:
		for _, item := range capabilities {
			if text, ok := item.(string); ok && text == capability {
				return true
			}
		}
	case []string:
		for _, item := range capabilities {
			if item == capability {
				return true
			}
		}
	}
	return false
}

func localRuntimeConfigFingerprint(options CommandOptions) string {
	if options.ConfigPath == "" {
		return ""
	}
	effective, err := configpkg.LoadEffective(configpkg.LoadOptions{
		ProjectPath: options.ConfigPath,
		WorkingDir:  filepath.Dir(options.ConfigPath),
	})
	if err != nil || effective.Config.Runtime == nil || effective.Config.Runtime.Local == nil {
		return ""
	}
	localSources := map[string]configpkg.Source{}
	for sourceID, source := range effective.Config.Sources {
		if source.Type == "mcp" && source.Transport != nil && source.Transport.Type == "stdio" {
			localSources[sourceID] = source
		}
	}
	localServices := map[string]configpkg.Service{}
	for serviceID, service := range effective.Config.Services {
		if _, ok := localSources[service.Source]; ok {
			localServices[serviceID] = service
		}
	}
	data, err := json.Marshal(struct {
		RuntimeMode string                        `json:"runtimeMode"`
		Local       *configpkg.LocalRuntimeConfig `json:"local"`
		Sources     map[string]configpkg.Source   `json:"sources,omitempty"`
		Services    map[string]configpkg.Service  `json:"services,omitempty"`
		Policy      configpkg.PolicyConfig        `json:"policy,omitempty"`
	}{
		RuntimeMode: effective.Config.Runtime.Mode,
		Local:       effective.Config.Runtime.Local,
		Sources:     localSources,
		Services:    localServices,
		Policy:      effective.Config.Policy,
	})
	if err != nil {
		return ""
	}
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

func fetchRuntimeHandshake(baseURL string) (embeddedruntime.HandshakeInfo, error) {
	return getJSON[embeddedruntime.HandshakeInfo](strings.TrimRight(baseURL, "/")+"/v1/runtime/info", "")
}

func fetchRuntimeBrowserLoginMetadata(baseURL, endpoint string) (runtimeBrowserLoginMetadata, error) {
	endpointURL, err := resolveRuntimeEndpointURL(baseURL, endpoint)
	if err != nil {
		return runtimeBrowserLoginMetadata{}, err
	}
	req, err := http.NewRequest(http.MethodGet, endpointURL, nil)
	if err != nil {
		return runtimeBrowserLoginMetadata{}, err
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return runtimeBrowserLoginMetadata{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		body, _ := io.ReadAll(resp.Body)
		return runtimeBrowserLoginMetadata{}, fmt.Errorf("%s", strings.TrimSpace(string(body)))
	}
	var metadata runtimeBrowserLoginMetadata
	if err := json.NewDecoder(resp.Body).Decode(&metadata); err != nil {
		return runtimeBrowserLoginMetadata{}, err
	}
	if err := validateRuntimeBrowserLoginMetadata(metadata); err != nil {
		return runtimeBrowserLoginMetadata{}, err
	}
	return metadata, nil
}

func resolveRuntimeEndpointURL(baseURL, endpoint string) (string, error) {
	if strings.TrimSpace(baseURL) == "" {
		return "", fmt.Errorf("runtime URL is required")
	}
	if strings.HasPrefix(endpoint, "http://") || strings.HasPrefix(endpoint, "https://") {
		return endpoint, nil
	}
	if endpoint == "" {
		endpoint = "/"
	}
	if !strings.HasPrefix(endpoint, "/") {
		endpoint = "/" + endpoint
	}
	return strings.TrimRight(baseURL, "/") + endpoint, nil
}

func validateRuntimeBrowserLoginMetadata(metadata runtimeBrowserLoginMetadata) error {
	switch {
	case strings.TrimSpace(metadata.AuthorizationURL) == "":
		return fmt.Errorf("runtime browser login metadata missing authorizationURL")
	case strings.TrimSpace(metadata.TokenURL) == "":
		return fmt.Errorf("runtime browser login metadata missing tokenURL")
	case strings.TrimSpace(metadata.ClientID) == "":
		return fmt.Errorf("runtime browser login metadata missing clientId")
	default:
		return nil
	}
}

func acquireRuntimeBrowserLoginToken(request runtimeBrowserLoginRequest) (string, error) {
	secret := configpkg.Secret{
		Type: "oauth2",
		OAuthConfig: configpkg.OAuthConfig{
			Mode:             "authorizationCode",
			AuthorizationURL: request.Metadata.AuthorizationURL,
			TokenURL:         request.Metadata.TokenURL,
			ClientID: &configpkg.SecretRef{
				Type:  "literal",
				Value: request.Metadata.ClientID,
			},
			Scopes:       append([]string(nil), request.Scopes...),
			Audience:     request.Audience,
			TokenStorage: "instance",
		},
	}
	if request.CallbackPort > 0 {
		callbackPort := request.CallbackPort
		secret.CallbackPort = &callbackPort
	}
	requirement := catalog.AuthRequirement{
		Type:   "oauth2",
		Scopes: append([]string(nil), request.Scopes...),
		OAuthFlows: []catalog.OAuthFlow{{
			Mode:             "authorizationCode",
			AuthorizationURL: request.Metadata.AuthorizationURL,
			TokenURL:         request.Metadata.TokenURL,
		}},
	}
	return oauthruntime.ResolveOAuthAccessToken(
		context.Background(),
		http.DefaultClient,
		configpkg.PolicyConfig{},
		secret,
		requirement,
		"runtime.browser."+request.Metadata.AuthorizationURL,
		request.StateDir,
		nil,
	)
}

func startManagedRuntime(options CommandOptions) (string, error) {
	paths, err := resolveInstancePaths(options)
	if err != nil {
		return "", err
	}
	binary, err := resolveDaemonBinary()
	if err != nil {
		return "", err
	}
	var runtimeCfg *configpkg.RuntimeConfig
	if loaded, ok := loadRuntimeConfig(options); ok {
		runtimeCfg = loaded
	}
	args := managedRuntimeArgs(options, runtimeCfg)
	cmd := exec.Command(binary, args...)
	cmd.Stdout = io.Discard
	cmd.Stderr = io.Discard
	configureManagedRuntimeCommand(cmd)
	if err := cmd.Start(); err != nil {
		return "", err
	}
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		info, err := instance.ReadRuntimeInfo(paths.RuntimePath)
		if err == nil && runtimeInfoReachable(info) {
			return info.URL, nil
		}
		time.Sleep(50 * time.Millisecond)
	}
	if cmd.Process != nil {
		_ = cmd.Process.Kill()
	}
	return "", fmt.Errorf("managed runtime did not become ready")
}

func managedRuntimeArgs(options CommandOptions, runtimeCfg *configpkg.RuntimeConfig) []string {
	args := []string{"--config", options.ConfigPath}
	if options.InstanceID != "" {
		args = append(args, "--instance-id", options.InstanceID)
	}
	if options.StateDir != "" {
		args = append(args, "--state-dir", options.StateDir)
	}
	if runtimeCfg == nil || runtimeCfg.Local == nil {
		return args
	}
	if runtimeCfg.Local.HeartbeatSeconds > 0 {
		args = append(args, "--heartbeat-seconds", strconv.Itoa(runtimeCfg.Local.HeartbeatSeconds))
	}
	if runtimeCfg.Local.MissedHeartbeatLimit > 0 {
		args = append(args, "--missed-heartbeat-limit", strconv.Itoa(runtimeCfg.Local.MissedHeartbeatLimit))
	}
	if runtimeCfg.Local.Shutdown != "" {
		args = append(args, "--shutdown", runtimeCfg.Local.Shutdown)
	}
	if runtimeCfg.Local.SessionScope != "" {
		args = append(args, "--session-scope", runtimeCfg.Local.SessionScope)
	}
	if runtimeCfg.Local.Share != "" {
		args = append(args, "--share", runtimeCfg.Local.Share)
	}
	if runtimeCfg.Local.ShareKey != "" {
		args = append(args, "--share-key-present", "true")
	}
	if options.ConfigFingerprint != "" {
		args = append(args, "--config-fingerprint", options.ConfigFingerprint)
	}
	return args
}

func resolveDaemonBinary() (string, error) {
	executable, err := os.Executable()
	if err == nil {
		sibling := filepath.Join(filepath.Dir(executable), "oclird")
		if _, statErr := os.Stat(sibling); statErr == nil {
			return sibling, nil
		}
	}
	path, err := exec.LookPath("oclird")
	if err != nil {
		return "", fmt.Errorf("resolve oclird binary: %w", err)
	}
	return path, nil
}

func newRuntimeClient(options CommandOptions) (runtimeClient, error) {
	if options.Embedded {
		paths, err := resolveInstancePaths(options)
		if err != nil {
			return nil, err
		}
		server := embeddedruntime.NewServer(embeddedruntime.Options{
			AuditPath:         paths.AuditPath,
			CacheDir:          paths.CacheDir,
			DefaultConfigPath: options.ConfigPath,
			RuntimeMode:       "embedded",
		})
		return embeddedRuntimeClient{handler: server.Handler(), sessionID: options.SessionID, configFingerprint: options.ConfigFingerprint}, nil
	}
	return httpRuntimeClient{baseURL: options.RuntimeURL, session: options.RuntimeAuth, sessionID: options.SessionID, configFingerprint: options.ConfigFingerprint}, nil
}

func resolveRuntimeURLFromInstance(options CommandOptions) (string, bool, error) {
	paths, err := resolveInstancePaths(options)
	if err != nil {
		return "", false, nil
	}
	info, err := instance.ReadRuntimeInfo(paths.RuntimePath)
	if err != nil || info.URL == "" {
		return "", false, nil
	}
	if !runtimeInfoReachable(info) {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		if err := cleanupManagedProcesses(ctx, paths.StateDir); err != nil {
			return "", false, err
		}
		_ = os.Remove(paths.RuntimePath)
		return "", false, nil
	}
	return info.URL, true, nil
}

func runtimeInfoReachable(info instance.RuntimeInfo) bool {
	if info.URL == "" {
		return false
	}
	if info.PID > 0 && !runtimeProcessAlive(info.PID) {
		return false
	}
	return runtimeURLReachable(info.URL)
}

func runtimeURLReachable(runtimeURL string) bool {
	parsed, err := url.Parse(runtimeURL)
	if err != nil || parsed.Host == "" {
		return false
	}
	conn, err := net.DialTimeout("tcp", parsed.Host, time.Second)
	if err != nil {
		return false
	}
	_ = conn.Close()
	return true
}

func resolveInstancePaths(options CommandOptions) (instance.Paths, error) {
	return instance.Resolve(instance.Options{
		InstanceID: options.InstanceID,
		ConfigPath: options.ConfigPath,
		StateRoot:  options.StateDir,
		CacheRoot:  cacheRootForState(options.StateDir),
	})
}

func cacheRootForState(stateDir string) string {
	if stateDir == "" {
		return ""
	}
	return filepath.Join(stateDir, "cache")
}

func detectTerminalSessionIdentity() string {
	if value := os.Getenv("OCLI_TERMINAL_SESSION_ID"); value != "" {
		return value
	}
	for _, fdPath := range []string{"/proc/self/fd/0", "/proc/self/fd/1", "/proc/self/fd/2"} {
		target, err := os.Readlink(fdPath)
		if err == nil && target != "" {
			return target
		}
	}
	return ""
}

func detectAgentSessionIdentity() string {
	for _, name := range []string{"OCLI_AGENT_SESSION_ID", "COPILOT_SESSION_ID"} {
		if value := os.Getenv(name); value != "" {
			return value
		}
	}
	return ""
}

func configureManagedRuntimeCommand(cmd *exec.Cmd) {
	configureManagedRuntimePlatform(cmd)
}

func shouldSendLocalHeartbeat(cmd *cobra.Command) bool {
	if cmd == nil {
		return false
	}
	switch cmd.CommandPath() {
	case "ocli runtime stop", "ocli runtime session-close":
		return false
	default:
		return true
	}
}

func envBool(name string) bool {
	value := strings.TrimSpace(strings.ToLower(os.Getenv(name)))
	return value == "1" || value == "true" || value == "yes" || value == "on"
}
