package commands

import (
	"bufio"
	"fmt"
	"io"
	"strings"

	"github.com/StevenBuglione/open-cli/pkg/catalog"
)

// PromptForMissingArgs prompts the user for missing path parameters on a TTY.
func PromptForMissingArgs(stdin io.Reader, stderr io.Writer, params []catalog.Parameter, args []string) ([]string, error) {
	result := make([]string, len(args))
	copy(result, args)

	scanner := bufio.NewScanner(stdin)
	for i := len(result); i < len(params); i++ {
		fmt.Fprintf(stderr, "Enter %s: ", params[i].Name)
		if !scanner.Scan() {
			return nil, fmt.Errorf("unexpected end of input while prompting for %s", params[i].Name)
		}
		value := strings.TrimSpace(scanner.Text())
		if value == "" {
			return nil, fmt.Errorf("%s is required", params[i].Name)
		}
		result = append(result, value)
	}
	return result, nil
}
