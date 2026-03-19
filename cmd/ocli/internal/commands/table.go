package commands

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"
	"text/tabwriter"

	runtimepkg "github.com/StevenBuglione/open-cli/cmd/ocli/internal/runtime"
	"github.com/StevenBuglione/open-cli/pkg/catalog"
)

// IsTerminal reports whether w is connected to a terminal device.
func IsTerminal(w io.Writer) bool {
	f, ok := w.(*os.File)
	if !ok {
		return false
	}
	fi, err := f.Stat()
	if err != nil {
		return false
	}
	return fi.Mode()&os.ModeCharDevice != 0
}

// IsTerminalReader checks if an io.Reader is connected to a terminal.
func IsTerminalReader(r io.Reader) bool {
	f, ok := r.(*os.File)
	if !ok {
		return false
	}
	fi, err := f.Stat()
	if err != nil {
		return false
	}
	return fi.Mode()&os.ModeCharDevice != 0
}

// WriteTable renders value as a human-readable table.
func WriteTable(out io.Writer, value any) error {
	w := tabwriter.NewWriter(out, 0, 0, 2, ' ', 0)
	switch v := value.(type) {
	case runtimepkg.CatalogResponse:
		return writeCatalogTable(w, v)
	case explainReport:
		return writeExplainTable(w, v)
	case *explainReport:
		return writeExplainTable(w, *v)
	case *catalog.Tool:
		return writeToolTable(w, v)
	case catalog.Tool:
		return writeToolTable(w, &v)
	case map[string]any:
		return writeMapTable(w, v)
	default:
		data, err := json.MarshalIndent(value, "", "  ")
		if err != nil {
			return err
		}
		_, err = out.Write(append(data, '\n'))
		return err
	}
}

func writeCatalogTable(w *tabwriter.Writer, resp runtimepkg.CatalogResponse) error {
	serviceAliases := map[string]string{}
	for _, svc := range resp.Catalog.Services {
		serviceAliases[svc.ID] = svc.Alias
	}
	fmt.Fprintf(w, "SERVICE\tGROUP\tCOMMAND\tMETHOD\tSUMMARY\n")
	for _, tool := range resp.View.Tools {
		alias := serviceAliases[tool.ServiceID]
		if alias == "" {
			alias = tool.ServiceID
		}
		summary := tool.Summary
		if summary == "" {
			summary = tool.Description
		}
		fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\n", alias, tool.Group, tool.Command, tool.Method, summary)
	}
	return w.Flush()
}

func writeToolTable(w *tabwriter.Writer, tool *catalog.Tool) error {
	fmt.Fprintf(w, "FIELD\tVALUE\n")
	fmt.Fprintf(w, "ID\t%s\n", tool.ID)
	fmt.Fprintf(w, "Service\t%s\n", tool.ServiceID)
	fmt.Fprintf(w, "Method\t%s\n", tool.Method)
	fmt.Fprintf(w, "Path\t%s\n", tool.Path)
	fmt.Fprintf(w, "Group\t%s\n", tool.Group)
	fmt.Fprintf(w, "Command\t%s\n", tool.Command)
	if tool.Summary != "" {
		fmt.Fprintf(w, "Summary\t%s\n", tool.Summary)
	}
	if tool.Description != "" {
		fmt.Fprintf(w, "Description\t%s\n", tool.Description)
	}
	return w.Flush()
}

func writeMapTable(w *tabwriter.Writer, m map[string]any) error {
	fmt.Fprintf(w, "KEY\tVALUE\n")
	for k, v := range m {
		fmt.Fprintf(w, "%s\t%v\n", k, v)
	}
	return w.Flush()
}

func writeExplainTable(w *tabwriter.Writer, report explainReport) error {
	fmt.Fprintf(w, "FIELD\tVALUE\n")
	fmt.Fprintf(w, "ID\t%s\n", report.ToolID)
	fmt.Fprintf(w, "Service\t%s\n", report.Service)
	fmt.Fprintf(w, "Method\t%s\n", report.Method)
	fmt.Fprintf(w, "Path\t%s\n", report.Path)
	fmt.Fprintf(w, "Group\t%s\n", report.Group)
	fmt.Fprintf(w, "Command\t%s\n", report.Command)
	if report.Summary != "" {
		fmt.Fprintf(w, "Summary\t%s\n", report.Summary)
	}
	if report.Description != "" {
		fmt.Fprintf(w, "Description\t%s\n", report.Description)
	}
	if len(report.Auth) == 0 {
		if len(report.AuthAlternatives) == 0 {
			fmt.Fprintln(w, "Auth:\tnone")
		} else {
			for idx, alternative := range report.AuthAlternatives {
				label := fmt.Sprintf("Auth option %d:", idx+1)
				if len(alternative.Requirements) == 0 {
					fmt.Fprintf(w, "%s\tnone\n", label)
					continue
				}
				for reqIdx, requirement := range alternative.Requirements {
					reqLabel := label
					if reqIdx > 0 {
						reqLabel = ""
					}
					fmt.Fprintf(w, "%s\t%s\n", reqLabel, formatAuthRequirement(requirement))
				}
			}
		}
	} else {
		for idx, requirement := range report.Auth {
			label := "Auth:"
			if idx > 0 {
				label = "Auth:"
			}
			fmt.Fprintf(w, "%s\t%s\n", label, formatAuthRequirement(requirement))
		}
	}
	fmt.Fprintf(w, "Approval:\t%s\n", yesNo(report.ApprovalRequired))
	fmt.Fprintf(w, "Approval status:\t%s\n", report.ApprovalStatus)
	fmt.Fprintf(w, "Runtime:\t%s\n", report.Runtime.Mode)
	fmt.Fprintf(w, "Runtime available:\t%s\n", yesNo(report.RuntimeAvailable))
	return w.Flush()
}

func formatAuthRequirement(requirement catalog.AuthRequirement) string {
	parts := []string{requirement.Name}
	if requirement.Type != "" {
		parts = append(parts, requirement.Type)
	}
	if requirement.Scheme != "" {
		parts = append(parts, requirement.Scheme)
	}
	if len(requirement.Scopes) > 0 {
		parts = append(parts, "scopes="+strings.Join(requirement.Scopes, " "))
	}
	return strings.Join(parts, " ")
}

func yesNo(value bool) string {
	if value {
		return "yes"
	}
	return "no"
}
