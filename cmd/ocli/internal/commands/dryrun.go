package commands

import (
	"fmt"
	"io"
	"net/url"
	"strings"

	"github.com/StevenBuglione/open-cli/pkg/catalog"
)

// WriteDryRun prints the HTTP request that would be sent, without executing.
func WriteDryRun(w io.Writer, tool catalog.Tool, pathArgs []string, flags map[string]string, body []byte) {
	baseURL := "http://localhost"
	if len(tool.Servers) > 0 {
		baseURL = tool.Servers[0]
	}

	path := tool.Path
	for i, param := range tool.PathParams {
		if i < len(pathArgs) {
			path = strings.ReplaceAll(path, "{"+param.OriginalName+"}", pathArgs[i])
		}
	}

	query := url.Values{}
	for _, param := range tool.Flags {
		if param.Location == "query" {
			if value := flags[param.Name]; value != "" {
				query.Set(param.OriginalName, value)
			}
		}
	}

	fullURL := baseURL + path
	if len(query) > 0 {
		fullURL += "?" + query.Encode()
	}

	fmt.Fprintf(w, "%s %s\n", strings.ToUpper(tool.Method), fullURL)
	if len(body) > 0 {
		fmt.Fprintln(w, "Content-Type: application/json")
	}
	fmt.Fprintln(w)
	if len(body) > 0 {
		fmt.Fprintln(w, string(body))
	}
}
