package commands

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"sort"
	"strings"

	"github.com/StevenBuglione/open-cli/pkg/catalog"
	"gopkg.in/yaml.v3"
)

// WriteOutput serialises value in the requested format and writes it to out.
func WriteOutput(out io.Writer, format string, value any) error {
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
	case "table":
		return WriteTable(out, value)
	default:
		return NewUserError(
			fmt.Sprintf("unsupported format %q", format),
			"The --format flag only accepts: json, yaml, pretty, table",
			"Use --format json or --format table")
	}
}

// FindTool returns a pointer to the tool with the given ID, or nil.
func FindTool(tools []catalog.Tool, id string) *catalog.Tool {
	for idx := range tools {
		if tools[idx].ID == id {
			return &tools[idx]
		}
	}
	return nil
}

// ResolveToolReference accepts either a canonical tool ID (demo:listItems) or
// a command-form reference (demo items list-items / demo list-items).
func ResolveToolReference(services []catalog.Service, tools []catalog.Tool, ref []string) (*catalog.Tool, error) {
	if len(ref) == 0 {
		return nil, nil
	}
	if len(ref) == 1 {
		if tool := FindTool(tools, ref[0]); tool != nil {
			return tool, nil
		}
	}
	normalizedRef := normalizeCommandReference(ref)

	serviceNames := map[string][]string{}
	for _, service := range services {
		names := []string{service.ID}
		if strings.TrimSpace(service.Alias) != "" && service.Alias != service.ID {
			names = append(names, service.Alias)
		}
		serviceNames[service.ID] = names
	}

	var matches []*catalog.Tool
	for idx := range tools {
		tool := &tools[idx]
		names := serviceNames[tool.ServiceID]
		if len(names) == 0 {
			names = []string{tool.ServiceID}
		}
		for _, serviceName := range names {
			for _, candidate := range toolCommandForms(serviceName, *tool) {
				if stringSlicesEqual(normalizeCommandReference(candidate), normalizedRef) {
					matches = append(matches, tool)
					goto nextTool
				}
			}
		}
	nextTool:
	}

	switch len(matches) {
	case 0:
		return nil, nil
	case 1:
		return matches[0], nil
	default:
		ids := make([]string, 0, len(matches))
		for _, match := range matches {
			ids = append(ids, match.ID)
		}
		sort.Strings(ids)
		return nil, fmt.Errorf("command reference %q is ambiguous; matches: %s", strings.Join(ref, " "), strings.Join(ids, ", "))
	}
}

func toolCommandForms(serviceName string, tool catalog.Tool) [][]string {
	forms := [][]string{}
	if tool.Command == "" || serviceName == "" {
		return forms
	}
	forms = append(forms, []string{serviceName, tool.Command})
	if tool.Group != "" {
		forms = append(forms, []string{serviceName, tool.Group, tool.Command})
	}
	return forms
}

func stringSlicesEqual(left, right []string) bool {
	if len(left) != len(right) {
		return false
	}
	for idx := range left {
		if left[idx] != right[idx] {
			return false
		}
	}
	return true
}

func normalizeCommandReference(parts []string) []string {
	normalized := make([]string, len(parts))
	for idx, part := range parts {
		normalized[idx] = normalizeCommandToken(part)
	}
	return normalized
}

func normalizeCommandToken(value string) string {
	normalized := strings.TrimSpace(strings.ToLower(value))
	normalized = strings.ReplaceAll(normalized, "_", "-")
	normalized = strings.ReplaceAll(normalized, " ", "-")
	return normalized
}

// CommandSummary returns a short description suitable for cobra.Command.Short.
func CommandSummary(tool catalog.Tool) string {
	if tool.Description != "" {
		return tool.Description
	}
	return tool.Summary
}

// LoadBody resolves a body reference: empty string → nil, "-" → stdin,
// "@path" → file, anything else → literal bytes.
func LoadBody(bodyRef string, stdin io.Reader) ([]byte, error) {
	var body []byte
	var err error
	switch {
	case bodyRef == "":
		return nil, nil
	case bodyRef == "-":
		body, err = io.ReadAll(stdin)
	case strings.HasPrefix(bodyRef, "@"):
		body, err = os.ReadFile(strings.TrimPrefix(bodyRef, "@"))
	default:
		body = []byte(bodyRef)
	}
	if err != nil {
		return nil, err
	}
	if len(body) > 0 && !json.Valid(body) {
		return nil, NewBodyError("The provided body is not valid JSON")
	}
	return body, nil
}

// SortedServiceAliases returns the service aliases in alphabetical order.
func SortedServiceAliases(services []catalog.Service) []string {
	aliases := make([]string, 0, len(services))
	for _, service := range services {
		aliases = append(aliases, service.Alias)
	}
	sort.Strings(aliases)
	return aliases
}

// FilterTools returns tools matching the given criteria. Empty filter values match all.
func FilterTools(tools []catalog.Tool, service, group, safety string) []catalog.Tool {
	var result []catalog.Tool
	for _, tool := range tools {
		if service != "" && tool.ServiceID != service {
			continue
		}
		if group != "" && tool.Group != group {
			continue
		}
		if safety != "" {
			switch safety {
			case "read-only":
				if !tool.Safety.ReadOnly {
					continue
				}
			case "destructive":
				if !tool.Safety.Destructive {
					continue
				}
			case "requires-approval":
				if !tool.Safety.RequiresApproval {
					continue
				}
			case "idempotent":
				if !tool.Safety.Idempotent {
					continue
				}
			}
		}
		result = append(result, tool)
	}
	return result
}

// SearchTools returns tools where pattern appears in ID, Command, Summary, or Description (case-insensitive).
func SearchTools(tools []catalog.Tool, pattern string) []catalog.Tool {
	lower := strings.ToLower(pattern)
	var result []catalog.Tool
	for _, tool := range tools {
		if strings.Contains(strings.ToLower(tool.ID), lower) ||
			strings.Contains(strings.ToLower(tool.Command), lower) ||
			strings.Contains(strings.ToLower(tool.Summary), lower) ||
			strings.Contains(strings.ToLower(tool.Description), lower) {
			result = append(result, tool)
		}
	}
	return result
}

// readConfigFile reads and parses a .cli.json file into a generic map.
func readConfigFile(path string) (map[string]any, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var raw map[string]any
	if err := json.Unmarshal(data, &raw); err != nil {
		return nil, err
	}
	return raw, nil
}
