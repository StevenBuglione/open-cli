package commands

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/spf13/cobra"
)

// NewInitCommand returns the "init" subcommand that creates a minimal .cli.json.
func NewInitCommand() *cobra.Command {
	var global bool

	cmd := &cobra.Command{
		Use:   "init <url-or-file>",
		Short: "Create a .cli.json configuration from an API spec",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			source := args[0]

			isURL := isRemoteURL(source)
			if isURL {
				if err := validateRemoteSpec(source); err != nil {
					return fmt.Errorf("Error: Cannot fetch spec from %s\n\nCause: %v\n\nSuggestion: Check the URL and ensure the spec is publicly reachable", source, err)
				}
			} else {
				abs, err := filepath.Abs(source)
				if err != nil {
					return err
				}
				if _, err := os.Stat(abs); err != nil {
					return fmt.Errorf("Error: File not found: %s\n\nCause: %v\n\nSuggestion: Check the path and try again", source, err)
				}
			}

			name := deriveServiceName(source)

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

			data, err := json.MarshalIndent(cfg, "", "  ")
			if err != nil {
				return err
			}

			outPath := ".cli.json"
			if global {
				home, err := os.UserHomeDir()
				if err != nil {
					return err
				}
				dir := filepath.Join(home, ".config", "ocli")
				if err := os.MkdirAll(dir, 0o755); err != nil {
					return err
				}
				outPath = filepath.Join(dir, ".cli.json")
			}

			if _, err := os.Stat(outPath); err == nil {
				return fmt.Errorf("Error: %s already exists\n\nCause: A configuration file is already present\n\nSuggestion: Remove or rename the existing file, then try again", outPath)
			}

			if err := os.WriteFile(outPath, append(data, '\n'), 0o644); err != nil {
				return err
			}

			w := cmd.OutOrStdout()
			fmt.Fprintf(w, "Created %s\n\n", outPath)
			fmt.Fprintln(w, "Next steps:")
			fmt.Fprintln(w, "  ocli --embedded catalog list   List available tools")
			fmt.Fprintf(w, "  ocli --embedded %s <group> <command>   Run a tool\n", name)
			return nil
		},
	}
	cmd.Flags().BoolVar(&global, "global", false, "Write config to ~/.config/ocli/ instead of current directory")
	return cmd
}

func isRemoteURL(s string) bool {
	u, err := url.Parse(s)
	if err != nil {
		return false
	}
	return u.Scheme == "http" || u.Scheme == "https"
}

func validateRemoteSpec(specURL string) error {
	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Head(specURL)
	if err != nil {
		return err
	}
	resp.Body.Close()
	if resp.StatusCode >= 400 {
		return fmt.Errorf("server returned HTTP %d", resp.StatusCode)
	}
	return nil
}

func deriveServiceName(source string) string {
	base := filepath.Base(source)
	// Strip common extensions
	for _, ext := range []string{".openapi.yaml", ".openapi.json", ".yaml", ".yml", ".json"} {
		if strings.HasSuffix(strings.ToLower(base), ext) {
			base = base[:len(base)-len(ext)]
			break
		}
	}
	// Clean: lowercase, replace non-alphanumeric with hyphens
	name := strings.ToLower(base)
	var clean []byte
	for i := 0; i < len(name); i++ {
		c := name[i]
		if (c >= 'a' && c <= 'z') || (c >= '0' && c <= '9') {
			clean = append(clean, c)
		} else if len(clean) > 0 && clean[len(clean)-1] != '-' {
			clean = append(clean, '-')
		}
	}
	result := strings.Trim(string(clean), "-")
	if result == "" {
		return "api"
	}
	return result
}
