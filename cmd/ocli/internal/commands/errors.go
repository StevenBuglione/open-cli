package commands

import "fmt"

// UserError holds a structured error with cause and suggestion for display.
type UserError struct {
	Err        string
	Cause      string
	Suggestion string
}

func (e *UserError) Error() string {
	return fmt.Sprintf("Error: %s\n\nCause: %s\n\nSuggestion: %s", e.Err, e.Cause, e.Suggestion)
}

// FormatError wraps an error with structured cause and suggestion text.
func FormatError(err error, cause, suggestion string) *UserError {
	return &UserError{Err: err.Error(), Cause: cause, Suggestion: suggestion}
}

// NewUserError creates a structured error from string components.
func NewUserError(msg, cause, suggestion string) *UserError {
	return &UserError{Err: msg, Cause: cause, Suggestion: suggestion}
}
