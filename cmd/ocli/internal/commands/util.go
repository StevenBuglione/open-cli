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
	default:
		return fmt.Errorf("unsupported format %q", format)
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

// SortedServiceAliases returns the service aliases in alphabetical order.
func SortedServiceAliases(services []catalog.Service) []string {
	aliases := make([]string, 0, len(services))
	for _, service := range services {
		aliases = append(aliases, service.Alias)
	}
	sort.Strings(aliases)
	return aliases
}
