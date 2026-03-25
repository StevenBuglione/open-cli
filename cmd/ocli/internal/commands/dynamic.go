package commands

import (
	"encoding/json"
	"fmt"
	"sort"
	"strconv"
	"strings"

	cfgpkg "github.com/StevenBuglione/open-cli/cmd/ocli/internal/config"
	runtimepkg "github.com/StevenBuglione/open-cli/cmd/ocli/internal/runtime"
	"github.com/StevenBuglione/open-cli/pkg/catalog"
	"github.com/spf13/cobra"
)

// AddDynamicToolCommands registers one cobra sub-command per tool, grouped by
// service alias and tool group.
func AddDynamicToolCommands(root *cobra.Command, options cfgpkg.Options, client runtimepkg.Client, services []catalog.Service, tools []catalog.Tool) {
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
			serviceCommand = &cobra.Command{
				Use:   serviceAlias,
				Short: fmt.Sprintf("Commands for the %s service", serviceAlias),
			}
			root.AddCommand(serviceCommand)
			serviceCommands[serviceAlias] = serviceCommand
		}

		groupKey := serviceAlias + ":" + tool.Group
		groupCommand := groupCommands[groupKey]
		if groupCommand == nil {
			if tool.Group == serviceAlias || tool.Group == "" {
				// Skip group level when it would stutter with service name
				groupCommand = serviceCommand
			} else {
				groupCommand = &cobra.Command{
					Use:   tool.Group,
					Short: fmt.Sprintf("%s operations", tool.Group),
				}
				serviceCommand.AddCommand(groupCommand)
			}
			groupCommands[groupKey] = groupCommand
		}

		toolCopy := tool
		expectedArgs := len(tool.PathParams)
		mcpBodyFields := mcpScalarBodyFields(toolCopy)
		command := &cobra.Command{
			Use:     tool.Command,
			Short:   CommandSummary(toolCopy),
			Long:    toolCopy.Description,
			Hidden:  toolCopy.Hidden,
			Aliases: append([]string(nil), toolCopy.Aliases...),
			Args: func(cmd *cobra.Command, args []string) error {
				if len(args) >= expectedArgs {
					return nil
				}
				if IsTerminalReader(cmd.InOrStdin()) {
					return nil
				}
				return fmt.Errorf("accepts %d arg(s), received %d", expectedArgs, len(args))
			},
			RunE: func(cmd *cobra.Command, args []string) error {
				if len(args) < len(toolCopy.PathParams) {
					if !IsTerminalReader(cmd.InOrStdin()) {
						return fmt.Errorf("accepts %d arg(s), received %d", len(toolCopy.PathParams), len(args))
					}
					prompted, err := PromptForMissingArgs(cmd.InOrStdin(), cmd.ErrOrStderr(), toolCopy.PathParams, args)
					if err != nil {
						return err
					}
					args = prompted
				}

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
				generatedBody, generatedFlagCount, err := mcpBodyFromFlags(cmd, mcpBodyFields)
				if err != nil {
					return err
				}
				if bodyRef != "" && generatedFlagCount > 0 {
					return fmt.Errorf("cannot combine --body with generated MCP scalar flags")
				}
				body, err := LoadBody(bodyRef, cmd.InOrStdin())
				if err != nil {
					return err
				}
				if len(body) == 0 && len(generatedBody) > 0 {
					body = generatedBody
				}
				dryRun, _ := cmd.Flags().GetBool("dry-run")
				if dryRun {
					WriteDryRun(options.Stdout, toolCopy, args, flags, body)
					return nil
				}
				result, err := client.Execute(runtimepkg.ExecuteRequest{
					ConfigPath:   options.RuntimeRequestConfigPath,
					Mode:         options.Mode,
					AgentProfile: options.AgentProfile,
					ToolID:       toolCopy.ID,
					PathArgs:     args,
					Flags:        flags,
					Body:         body,
					Approval:     options.Approval,
				})
				if err != nil {
					return FormatError(err,
						fmt.Sprintf("Failed to execute tool %s", toolCopy.ID),
						"Check that the target API server is running and reachable")
				}
				if len(result.Body) > 0 && options.Format == "json" {
					_, err = options.Stdout.Write(append(result.Body, '\n'))
					return err
				}
				if result.Text != "" {
					_, err = fmt.Fprintln(options.Stdout, result.Text)
					return err
				}
				return WriteOutput(options.Stdout, options.Format, result)
			},
		}
		for _, flag := range tool.Flags {
			command.Flags().String(flag.Name, "", "parameter "+flag.OriginalName)
		}
		for _, field := range mcpBodyFields {
			command.Flags().String(field.FlagName, mcpFieldDefaultValue(field), mcpFieldHelp(field))
		}
		command.Flags().String("body", "", "inline request body")
		command.Flags().Bool("dry-run", false, "Show the request without executing")
		groupCommand.AddCommand(command)
	}
}

type mcpBodyField struct {
	FlagName string
	JSONName string
	Type     string
	Required bool
	Default  *string
	Enum     []string
}

func mcpScalarBodyFields(tool catalog.Tool) []mcpBodyField {
	if tool.Backend == nil || tool.Backend.Kind != "mcp" || tool.RequestBody == nil {
		return nil
	}
	var schema map[string]any
	for _, content := range tool.RequestBody.ContentTypes {
		if content.MediaType == "" || content.MediaType == "application/json" {
			schema = content.Schema
			break
		}
	}
	properties, _ := schema["properties"].(map[string]any)
	if len(properties) == 0 {
		return nil
	}

	reserved := map[string]struct{}{"body": {}, "dry-run": {}, "help": {}, "h": {}}
	for _, flag := range tool.Flags {
		reserved[flag.Name] = struct{}{}
	}
	for _, param := range tool.PathParams {
		reserved[param.Name] = struct{}{}
	}

	names := make([]string, 0, len(properties))
	for name := range properties {
		names = append(names, name)
	}
	sort.Strings(names)

	fields := make([]mcpBodyField, 0, len(names))
	for _, name := range names {
		property, _ := properties[name].(map[string]any)
		fieldType := scalarSchemaType(property)
		switch fieldType {
		case "string", "boolean", "integer", "number":
		default:
			continue
		}
		flagName := normalizeCommandToken(name)
		if _, exists := reserved[flagName]; exists {
			continue
		}
		fields = append(fields, mcpBodyField{
			FlagName: flagName,
			JSONName: name,
			Type:     fieldType,
			Required: stringSetFromAny(schema["required"]).Contains(name),
			Default:  scalarSchemaDefault(property, fieldType),
			Enum:     scalarSchemaEnum(property),
		})
	}
	return fields
}

func mcpBodyFromFlags(cmd *cobra.Command, fields []mcpBodyField) ([]byte, int, error) {
	if len(fields) == 0 {
		return nil, 0, nil
	}
	body := map[string]any{}
	explicit := 0
	var missing []string
	for _, field := range fields {
		raw, err := cmd.Flags().GetString(field.FlagName)
		if err != nil {
			return nil, 0, err
		}
		flag := cmd.Flags().Lookup(field.FlagName)
		changed := flag != nil && flag.Changed
		if !changed && field.Default == nil {
			if field.Required {
				missing = append(missing, "--"+field.FlagName)
			}
			continue
		}
		if !changed && field.Default != nil {
			raw = *field.Default
		}
		if len(field.Enum) > 0 && !containsString(field.Enum, raw) {
			return nil, explicit, fmt.Errorf("invalid value %q for --%s (expected one of: %s)", raw, field.FlagName, strings.Join(field.Enum, ", "))
		}
		value, err := parseMCPScalarValue(field.Type, raw)
		if err != nil {
			return nil, 0, err
		}
		body[field.JSONName] = value
		if changed {
			explicit++
		}
	}
	if len(missing) > 0 {
		sort.Strings(missing)
		return nil, explicit, fmt.Errorf("missing required MCP flags: %s", strings.Join(missing, ", "))
	}
	if len(body) == 0 {
		return nil, explicit, nil
	}
	data, err := json.Marshal(body)
	if err != nil {
		return nil, explicit, err
	}
	return data, explicit, nil
}

func parseMCPScalarValue(fieldType, raw string) (any, error) {
	switch fieldType {
	case "string":
		return raw, nil
	case "boolean":
		value, err := strconv.ParseBool(raw)
		if err != nil {
			return nil, fmt.Errorf("invalid boolean value %q", raw)
		}
		return value, nil
	case "integer":
		value, err := strconv.Atoi(raw)
		if err != nil {
			return nil, fmt.Errorf("invalid integer value %q", raw)
		}
		return value, nil
	case "number":
		value, err := strconv.ParseFloat(raw, 64)
		if err != nil {
			return nil, fmt.Errorf("invalid number value %q", raw)
		}
		return value, nil
	default:
		return nil, fmt.Errorf("unsupported MCP scalar type %q", fieldType)
	}
}

func scalarSchemaType(property map[string]any) string {
	switch typed := property["type"].(type) {
	case string:
		return typed
	case []any:
		for _, candidate := range typed {
			if value, ok := candidate.(string); ok && value != "null" {
				return value
			}
		}
	}
	return ""
}

func scalarSchemaDefault(property map[string]any, fieldType string) *string {
	value, ok := property["default"]
	if !ok {
		return nil
	}
	switch fieldType {
	case "string":
		if typed, ok := value.(string); ok {
			return &typed
		}
	case "boolean":
		if typed, ok := value.(bool); ok {
			rendered := strconv.FormatBool(typed)
			return &rendered
		}
	case "integer":
		switch typed := value.(type) {
		case int:
			rendered := strconv.Itoa(typed)
			return &rendered
		case float64:
			rendered := strconv.Itoa(int(typed))
			return &rendered
		}
	case "number":
		if typed, ok := value.(float64); ok {
			rendered := strconv.FormatFloat(typed, 'f', -1, 64)
			return &rendered
		}
	}
	return nil
}

func scalarSchemaEnum(property map[string]any) []string {
	values, _ := property["enum"].([]any)
	if len(values) == 0 {
		return nil
	}
	result := make([]string, 0, len(values))
	for _, value := range values {
		result = append(result, fmt.Sprint(value))
	}
	return result
}

func containsString(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}

func stringSetFromAny(value any) stringSet {
	items, _ := value.([]any)
	set := make(stringSet, len(items))
	for _, item := range items {
		if text, ok := item.(string); ok {
			set[text] = struct{}{}
		}
	}
	return set
}

type stringSet map[string]struct{}

func (set stringSet) Contains(value string) bool {
	_, ok := set[value]
	return ok
}

func mcpFieldDefaultValue(field mcpBodyField) string {
	if field.Default == nil {
		return ""
	}
	return *field.Default
}

func mcpFieldHelp(field mcpBodyField) string {
	suffixes := []string{}
	if field.Required {
		suffixes = append(suffixes, "required")
	}
	if field.Default != nil {
		suffixes = append(suffixes, "default="+*field.Default)
	}
	if len(field.Enum) > 0 {
		suffixes = append(suffixes, "one of: "+strings.Join(field.Enum, ", "))
	}
	if len(suffixes) == 0 {
		return "MCP input " + field.JSONName
	}
	return "MCP input " + field.JSONName + " (" + strings.Join(suffixes, "; ") + ")"
}
