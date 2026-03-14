package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	embeddedruntime "github.com/StevenBuglione/oas-cli-go/internal/runtime"
	"github.com/StevenBuglione/oas-cli-go/pkg/catalog"
	"github.com/StevenBuglione/oas-cli-go/pkg/instance"
	"github.com/spf13/cobra"
	"gopkg.in/yaml.v3"
)

type CommandOptions struct {
	RuntimeURL   string
	ConfigPath   string
	Mode         string
	AgentProfile string
	Format       string
	Approval     bool
	InstanceID   string
	StateDir     string
	Embedded     bool
	Stdin        io.Reader
	Stdout       io.Writer
	Stderr       io.Writer
}

type runtimeCatalogResponse struct {
	Catalog catalog.NormalizedCatalog `json:"catalog"`
	View    catalog.EffectiveView     `json:"view"`
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
	StatusCode int             `json:"statusCode"`
	Body       json.RawMessage `json:"body,omitempty"`
	Text       string          `json:"text,omitempty"`
}

type runtimeClient interface {
	FetchCatalog(CommandOptions) (runtimeCatalogResponse, error)
	Execute(executeRequest) (executeResponse, error)
	RunWorkflow(map[string]any) (map[string]any, error)
}

type httpRuntimeClient struct {
	baseURL string
}

type embeddedRuntimeClient struct {
	handler http.Handler
}

const defaultRuntimeURL = "http://127.0.0.1:8765"

func main() {
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
		Use:           "oascli",
		SilenceUsage:  true,
		SilenceErrors: true,
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
	resp, err := http.Get(endpoint.String())
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
	return fetchCatalogHTTP(client.baseURL, options)
}

func (client httpRuntimeClient) Execute(request executeRequest) (executeResponse, error) {
	return postJSON[executeResponse](client.baseURL+"/v1/tools/execute", request)
}

func (client httpRuntimeClient) RunWorkflow(request map[string]any) (map[string]any, error) {
	return postJSON[map[string]any](client.baseURL+"/v1/workflows/run", request)
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

func postJSON[T any](endpoint string, payload any) (T, error) {
	var zero T
	body, err := json.Marshal(payload)
	if err != nil {
		return zero, err
	}
	resp, err := http.Post(endpoint, "application/json", bytes.NewReader(body))
	if err != nil {
		return zero, err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		data, _ := io.ReadAll(resp.Body)
		return zero, fmt.Errorf("%s", strings.TrimSpace(string(data)))
	}
	var decoded T
	err = json.NewDecoder(resp.Body).Decode(&decoded)
	return decoded, err
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
	if options.InstanceID == "" {
		options.InstanceID = os.Getenv("OASCLI_INSTANCE_ID")
	}
	if options.StateDir == "" {
		options.StateDir = os.Getenv("OASCLI_STATE_DIR")
	}
	if !options.Embedded {
		options.Embedded = envBool("OASCLI_EMBEDDED")
	}
	if options.Embedded {
		return options, nil
	}
	if options.RuntimeURL == "" {
		options.RuntimeURL = os.Getenv("OASCLI_RUNTIME_URL")
	}
	if options.RuntimeURL == "" {
		if runtimeURL, ok := resolveRuntimeURLFromInstance(options); ok {
			options.RuntimeURL = runtimeURL
		}
	}
	if options.RuntimeURL == "" {
		options.RuntimeURL = defaultRuntimeURL
	}
	return options, nil
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
		})
		return embeddedRuntimeClient{handler: server.Handler()}, nil
	}
	return httpRuntimeClient{baseURL: options.RuntimeURL}, nil
}

func resolveRuntimeURLFromInstance(options CommandOptions) (string, bool) {
	paths, err := resolveInstancePaths(options)
	if err != nil {
		return "", false
	}
	info, err := instance.ReadRuntimeInfo(paths.RuntimePath)
	if err != nil || info.URL == "" {
		return "", false
	}
	if !runtimeURLReachable(info.URL) {
		_ = os.Remove(paths.RuntimePath)
		return "", false
	}
	return info.URL, true
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

func envBool(name string) bool {
	value := strings.TrimSpace(strings.ToLower(os.Getenv(name)))
	return value == "1" || value == "true" || value == "yes" || value == "on"
}
