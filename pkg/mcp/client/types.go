package client

import "context"

type ToolDescriptor struct {
	Name        string         `json:"name"`
	Description string         `json:"description,omitempty"`
	InputSchema map[string]any `json:"inputSchema,omitempty"`
	Annotations map[string]any `json:"annotations,omitempty"`
}

type ToolResult struct {
	StructuredContent any              `json:"structuredContent,omitempty"`
	Content           []map[string]any `json:"content,omitempty"`
	IsError           bool             `json:"isError,omitempty"`
}

type Client interface {
	ListTools(ctx context.Context) ([]ToolDescriptor, error)
	CallTool(ctx context.Context, name string, args any) (ToolResult, error)
	Close() error
}
