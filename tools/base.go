package tools

import "context"

// Tool is the interface for agent tools.
type Tool interface {
	Name() string
	Description() string
	Parameters() map[string]any
	Execute(ctx context.Context, params map[string]any) (string, error)
}

// Schema returns OpenAI function schema for the tool.
func Schema(t Tool) map[string]any {
	return map[string]any{
		"type": "function",
		"function": map[string]any{
			"name":        t.Name(),
			"description": t.Description(),
			"parameters": t.Parameters(),
		},
	}
}
