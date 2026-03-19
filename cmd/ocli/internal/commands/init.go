package commands

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/getkin/kin-openapi/openapi3"
	"github.com/spf13/cobra"
)

// NewInitCommand returns the "init" subcommand that creates a minimal .cli.json.
func NewInitCommand() *cobra.Command {
	var global bool
	var sourceType string
	var transport string
	var mcpCommand string
	var mcpArgs string
	var mcpURL string

	cmd := &cobra.Command{
		Use:   "init <url-or-file-or-name>",
		Short: "Create a .cli.json configuration from an API spec",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			source := args[0]
			w := cmd.OutOrStdout()

			outPath := ".cli.json"
			if global {
				home, err := os.UserHomeDir()
				if err != nil {
					return err
				}
				dir := filepath.Join(home, ".config", "oas-cli")
				if err := os.MkdirAll(dir, 0o755); err != nil {
					return err
				}
				outPath = filepath.Join(dir, ".cli.json")
			}
			if _, err := os.Stat(outPath); err == nil {
				return NewUserError(
					fmt.Sprintf("%s already exists", outPath),
					"A configuration file is already present",
					"Remove or rename the existing file, then try again")
			}

			var cfg map[string]any
			var authHints []string
			var name string
			var err error
			switch sourceType {
			case "mcp":
				if err := validateMCPFlags(transport, mcpCommand, mcpURL); err != nil {
					return err
				}
				cfg = buildMCPConfig(source, transport, mcpCommand, mcpArgs, mcpURL)
				name = source
			default:
				cfg, authHints, name, err = buildOpenAPIConfig(source, w)
				if err != nil {
					return err
				}
			}

			data, err := json.MarshalIndent(cfg, "", "  ")
			if err != nil {
				return err
			}
			if err := os.WriteFile(outPath, append(data, '\n'), 0o644); err != nil {
				return err
			}

			fmt.Fprintf(w, "\nCreated %s\n", outPath)
			fmt.Fprintln(w)
			fmt.Fprintln(w, "Next steps:")
			fmt.Fprintln(w, "  ocli catalog list              List available tools")
			fmt.Fprintf(w, "  ocli %s --help                See %s commands\n", name, name)
			if len(authHints) > 0 {
				fmt.Fprintln(w)
				fmt.Fprintln(w, "This API requires authentication. Configure secrets:")
				for _, hint := range authHints {
					fmt.Fprintf(w, "  %s\n", hint)
				}
			}
			return nil
		},
	}
	cmd.Flags().BoolVar(&global, "global", false, "Write config to ~/.config/oas-cli/ instead of current directory")
	cmd.Flags().StringVar(&sourceType, "type", "openapi", "Source type: openapi or mcp")
	cmd.Flags().StringVar(&transport, "transport", "", "MCP transport: stdio, sse, streamable-http")
	cmd.Flags().StringVar(&mcpCommand, "command", "", "MCP stdio command")
	cmd.Flags().StringVar(&mcpArgs, "args", "", "Comma-separated MCP stdio args")
	cmd.Flags().StringVar(&mcpURL, "url", "", "MCP sse/streamable-http URL")
	return cmd
}

func isRemoteURL(s string) bool {
	u, err := url.Parse(s)
	if err != nil {
		return false
	}
	return u.Scheme == "http" || u.Scheme == "https"
}

func buildOpenAPIConfig(source string, w io.Writer) (map[string]any, []string, string, error) {
	isURL := isRemoteURL(source)

	var specData []byte
	var specHost string
	if isURL {
		fmt.Fprint(w, "Parsing spec... ")
		client := &http.Client{Timeout: 15 * time.Second}
		resp, err := client.Get(source)
		if err != nil {
			return nil, nil, "", FormatError(err, fmt.Sprintf("Cannot fetch spec from %s", source), "Check the URL and ensure the spec is publicly reachable")
		}
		defer resp.Body.Close()
		if resp.StatusCode >= 400 {
			return nil, nil, "", FormatError(fmt.Errorf("server returned HTTP %d", resp.StatusCode), fmt.Sprintf("Cannot fetch spec from %s", source), "Check the URL and ensure the spec is publicly reachable")
		}
		specData, err = io.ReadAll(resp.Body)
		if err != nil {
			return nil, nil, "", err
		}
		if parsed, err := url.Parse(source); err == nil {
			specHost = parsed.Scheme + "://" + parsed.Host
		}
	} else {
		abs, err := filepath.Abs(source)
		if err != nil {
			return nil, nil, "", err
		}
		if _, err := os.Stat(abs); err != nil {
			return nil, nil, "", FormatError(err, fmt.Sprintf("File not found: %s", source), "Check the path and try again")
		}
		specData, err = os.ReadFile(abs)
		if err != nil {
			return nil, nil, "", err
		}
		fmt.Fprint(w, "Parsing spec... ")
	}

	loader := openapi3.NewLoader()
	loader.IsExternalRefsAllowed = true
	doc, err := loader.LoadFromData(specData)
	if err != nil {
		fmt.Fprintln(w, "x")
		return nil, nil, "", FormatError(err, "Failed to parse OpenAPI spec", "Ensure the file is a valid OpenAPI 3.x document")
	}
	if err := doc.Validate(context.Background(), openapi3.DisableExamplesValidation()); err != nil {
		fmt.Fprintln(w, "x")
		return nil, nil, "", FormatError(err, "OpenAPI spec validation failed", "Check the spec for structural issues")
	}
	fmt.Fprintf(w, "ok OpenAPI %s\n", doc.OpenAPI)

	name := deriveServiceName(source, doc)

	toolCount := 0
	groupSet := map[string]bool{}
	if doc.Paths != nil {
		for _, pathItem := range doc.Paths.Map() {
			for _, operation := range []*openapi3.Operation{
				pathItem.Get, pathItem.Post, pathItem.Put, pathItem.Delete,
				pathItem.Patch, pathItem.Head, pathItem.Options,
			} {
				if operation == nil {
					continue
				}
				toolCount++
				if len(operation.Tags) > 0 {
					groupSet[operation.Tags[0]] = true
				}
			}
		}
	}
	var groups []string
	for group := range groupSet {
		groups = append(groups, group)
	}
	sort.Strings(groups)
	if len(groups) > 0 {
		fmt.Fprintf(w, "Found %d tools across %d groups (%s)\n", toolCount, len(groups), strings.Join(groups, ", "))
	} else {
		fmt.Fprintf(w, "Found %d tools\n", toolCount)
	}

	var authHints []string
	envNamePrefix := serviceNameEnvPrefix(name)
	if doc.Components != nil && doc.Components.SecuritySchemes != nil {
		for schemeName, schemeRef := range doc.Components.SecuritySchemes {
			if schemeRef.Value == nil {
				continue
			}
			scheme := schemeRef.Value
			switch scheme.Type {
			case "oauth2":
				authHints = append(authHints, fmt.Sprintf("ocli config add-secret %s --type oauth2 --token-url <url> --client-id-env <var> --client-secret-env <var>", schemeName))
			case "apiKey":
				authHints = append(authHints, fmt.Sprintf("ocli config add-secret %s --type env --env-value %s_API_KEY", schemeName, envNamePrefix))
			case "http":
				authHints = append(authHints, fmt.Sprintf("ocli config add-secret %s --type env --env-value %s_TOKEN", schemeName, envNamePrefix))
			}
		}
	}

	if doc.Servers != nil {
		for _, server := range doc.Servers {
			if server.URL != "" && !strings.HasPrefix(server.URL, "http") {
				fmt.Fprintf(w, "Warning: relative server URL %s", server.URL)
				if specHost != "" {
					fmt.Fprintf(w, " (resolved against %s)", specHost)
				}
				fmt.Fprintln(w)
			}
		}
	}

	cfg := map[string]any{
		"cli":  "1.0.0",
		"mode": map[string]any{"default": "discover"},
		"sources": map[string]any{
			name + "Source": map[string]any{
				"type":    "openapi",
				"uri":     source,
				"enabled": true,
			},
		},
		"services": map[string]any{
			name: map[string]any{
				"source": name + "Source",
				"alias":  name,
			},
		},
	}
	return cfg, authHints, name, nil
}

func buildMCPConfig(name, transport, command, args, mcpURL string) map[string]any {
	transportMap := map[string]any{"type": transport}
	if transport == "stdio" {
		transportMap["command"] = command
		if args != "" {
			transportMap["args"] = strings.Split(args, ",")
		}
	} else {
		transportMap["url"] = mcpURL
	}
	return map[string]any{
		"cli":  "1.0.0",
		"mode": map[string]any{"default": "discover"},
		"sources": map[string]any{
			name: map[string]any{
				"type":      "mcp",
				"enabled":   true,
				"transport": transportMap,
			},
		},
		"services": map[string]any{
			name: map[string]any{
				"source": name,
				"alias":  name,
			},
		},
	}
}

func validateMCPFlags(transport, command, mcpURL string) error {
	switch transport {
	case "stdio":
		if command == "" {
			return NewUserError("Missing --command", "MCP stdio transport requires a command to execute", "Add --command <executable> (e.g., --command npx)")
		}
	case "sse", "streamable-http":
		if mcpURL == "" {
			return NewUserError("Missing --url", "MCP transport requires a URL", "Add --url <endpoint>")
		}
	case "":
		return NewUserError("Missing --transport", "MCP sources require a transport type", "Add --transport stdio, --transport sse, or --transport streamable-http")
	default:
		return NewUserError(fmt.Sprintf("Unknown transport %q", transport), "Supported MCP transports: stdio, sse, streamable-http", "Use --transport stdio for local MCP servers")
	}
	return nil
}

var versionTokenPattern = regexp.MustCompile(`^(?:v)?\d+(?:\.\d+)*$`)

func deriveServiceName(source string, doc *openapi3.T) string {
	if name := deriveServiceNameFromBasename(source); name != "" {
		return name
	}

	if doc != nil && doc.Info != nil {
		if name := deriveServiceNameFromTitle(doc.Info.Title); name != "" {
			return name
		}
	}

	if isRemoteURL(source) {
		if name := deriveServiceNameFromHost(source); name != "" {
			return name
		}
	}

	return "service"
}

func deriveServiceNameFromBasename(source string) string {
	base := source
	if isRemoteURL(source) {
		parsed, err := url.Parse(source)
		if err != nil {
			return ""
		}
		segment := strings.TrimRight(parsed.Path, "/")
		if segment == "" {
			return ""
		}
		base = path.Base(segment)
	} else {
		base = filepath.Base(source)
	}
	base = trimSpecSuffix(base)
	name := sanitizeServiceName(base)
	if isInvalidServiceName(name) {
		return ""
	}
	return name
}

func deriveServiceNameFromTitle(title string) string {
	tokens := titleTokens(title)
	if len(tokens) == 0 {
		return ""
	}

	for {
		trimmed := false
		for len(tokens) > 0 && isBoilerplateToken(tokens[0]) {
			tokens = tokens[1:]
			trimmed = true
		}
		for len(tokens) > 0 && isBoilerplateToken(tokens[len(tokens)-1]) {
			tokens = tokens[:len(tokens)-1]
			trimmed = true
		}
		if !trimmed {
			break
		}
	}

	name := sanitizeServiceName(strings.Join(tokens, "-"))
	if isInvalidServiceName(name) {
		return ""
	}
	return name
}

func deriveServiceNameFromHost(source string) string {
	parsed, err := url.Parse(source)
	if err != nil {
		return ""
	}

	host := parsed.Hostname()
	if host == "" {
		return ""
	}
	host = strings.TrimPrefix(strings.ToLower(host), "www.")
	parts := strings.Split(host, ".")
	if len(parts) == 0 {
		return ""
	}

	name := sanitizeServiceName(parts[0])
	if isInvalidServiceName(name) {
		return ""
	}
	return name
}

func trimSpecSuffix(base string) string {
	lower := strings.ToLower(base)
	for _, ext := range []string{
		".openapi.yaml",
		".openapi.yml",
		".openapi.json",
		".swagger.json",
		".swagger.yaml",
		".yaml",
		".yml",
		".json",
	} {
		if strings.HasSuffix(lower, ext) {
			return base[:len(base)-len(ext)]
		}
	}
	return base
}

func sanitizeServiceName(raw string) string {
	raw = strings.ToLower(raw)
	var clean []byte
	for i := 0; i < len(raw); i++ {
		c := raw[i]
		if (c >= 'a' && c <= 'z') || (c >= '0' && c <= '9') {
			clean = append(clean, c)
			continue
		}
		if len(clean) > 0 && clean[len(clean)-1] != '-' {
			clean = append(clean, '-')
		}
	}
	return strings.Trim(string(clean), "-")
}

func serviceNameEnvPrefix(name string) string {
	prefix := strings.ToUpper(strings.ReplaceAll(name, "-", "_"))
	if prefix == "" {
		return "OCLI"
	}
	first := prefix[0]
	if first >= 'A' && first <= 'Z' {
		return prefix
	}
	return "OCLI_" + prefix
}

func titleTokens(title string) []string {
	matches := regexp.MustCompile(`[A-Za-z0-9]+(?:\.[A-Za-z0-9]+)*`).FindAllString(title, -1)
	if len(matches) == 0 {
		return nil
	}
	return matches
}

func isBoilerplateToken(token string) bool {
	lower := strings.ToLower(token)
	switch lower {
	case "swagger", "openapi", "api":
		return true
	}
	return versionTokenPattern.MatchString(lower)
}

func isGenericServiceName(name string) bool {
	tokens := strings.Split(name, "-")
	for len(tokens) > 0 && versionTokenPattern.MatchString(tokens[0]) {
		tokens = tokens[1:]
	}
	for len(tokens) > 0 && versionTokenPattern.MatchString(tokens[len(tokens)-1]) {
		tokens = tokens[:len(tokens)-1]
	}
	if len(tokens) != 1 {
		return false
	}
	switch tokens[0] {
	case "openapi", "swagger", "api", "spec", "index":
		return true
	default:
		return false
	}
}

func isInvalidServiceName(name string) bool {
	return name == "" || isGenericServiceName(name) || isVersionOnlyServiceName(name)
}

func isVersionOnlyServiceName(name string) bool {
	tokens := strings.Split(name, "-")
	if len(tokens) == 0 {
		return false
	}
	for _, token := range tokens {
		if !versionTokenPattern.MatchString(token) {
			return false
		}
	}
	return true
}
