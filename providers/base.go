package providers

import "context"

// ToolCallRequest is a tool call from the LLM.
type ToolCallRequest struct {
	ID        string         `json:"id"`
	Name      string         `json:"name"`
	Arguments map[string]any  `json:"arguments"`
}

// LLMResponse is the response from an LLM provider.
type LLMResponse struct {
	Content         string            `json:"content"`
	ToolCalls        []ToolCallRequest `json:"tool_calls,omitempty"`
	FinishReason     string            `json:"finish_reason"`
	Usage            map[string]int    `json:"usage,omitempty"`
	ReasoningContent string            `json:"reasoning_content,omitempty"`
}

// HasToolCalls returns true if the response contains tool calls.
func (r *LLMResponse) HasToolCalls() bool {
	return len(r.ToolCalls) > 0
}

// LLMProvider is the interface for LLM backends.
type LLMProvider interface {
	Chat(ctx context.Context, req *ChatRequest) (*LLMResponse, error)
	GetDefaultModel() string
}

// ChatRequest is the input to Chat.
type ChatRequest struct {
	Messages        []Message `json:"messages"`
	Tools           []ToolDef `json:"tools,omitempty"`
	ToolChoice      any       `json:"tool_choice,omitempty"` // "auto", "none", or map for {"type":"function","function":{"name":"save_memory"}}
	Model           string    `json:"model"`
	MaxTokens       int       `json:"max_tokens"`
	Temperature     float64   `json:"temperature"`
	ReasoningEffort string    `json:"reasoning_effort,omitempty"` // low/medium/high for thinking models
}

// Message is a single chat message.
type Message struct {
	Role         string         `json:"role"`
	Content      string         `json:"content,omitempty"`
	ToolCalls    []ToolCallDef  `json:"tool_calls,omitempty"`
	ToolCallID   string         `json:"tool_call_id,omitempty"`
	Name         string         `json:"name,omitempty"`
}

// ToolDef is OpenAI-style tool definition.
type ToolDef struct {
	Type     string      `json:"type"`
	Function ToolFunction `json:"function"`
}

// ToolFunction describes a tool's name, description, parameters.
type ToolFunction struct {
	Name        string     `json:"name"`
	Description string     `json:"description"`
	Parameters  map[string]any `json:"parameters"`
}

// ToolCallDef is assistant tool call in a message.
type ToolCallDef struct {
	ID       string         `json:"id"`
	Type     string         `json:"type"`
	Function ToolCallFunc   `json:"function"`
}

// ToolCallFunc is the function part of a tool call.
type ToolCallFunc struct {
	Name      string         `json:"name"`
	Arguments string         `json:"arguments"`
}
