package providers

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
)

// OpenAIProvider calls an OpenAI-compatible API (OpenAI, Anthropic via gateway, OpenRouter, etc.).
type OpenAIProvider struct {
	APIKey       string
	APIBase      string
	DefaultModel string
	Client       *http.Client
}

// NewOpenAIProvider creates a provider. APIBase defaults to https://api.openai.com/v1.
func NewOpenAIProvider(apiKey, apiBase, defaultModel string) *OpenAIProvider {
	if apiBase == "" {
		apiBase = "https://api.openai.com/v1"
	}
	if defaultModel == "" {
		defaultModel = "gpt-4o-mini"
	}
	return &OpenAIProvider{
		APIKey:       apiKey,
		APIBase:      apiBase,
		DefaultModel: defaultModel,
		Client:       http.DefaultClient,
	}
}

// GetDefaultModel implements LLMProvider.
func (p *OpenAIProvider) GetDefaultModel() string {
	return p.DefaultModel
}

// openAIChatRequest is the request body for /chat/completions.
type openAIChatRequest struct {
	Model           string        `json:"model"`
	Messages        []openAIMsg   `json:"messages"`
	MaxTokens       int           `json:"max_tokens"`
	Temperature     float64       `json:"temperature"`
	Tools           []openAITool  `json:"tools,omitempty"`
	ToolChoice      any           `json:"tool_choice,omitempty"`
	ReasoningEffort string        `json:"reasoning_effort,omitempty"`
}

type openAIMsg struct {
	Role       string            `json:"role"`
	Content    string            `json:"content,omitempty"`
	ToolCalls  []openAIToolCall  `json:"tool_calls,omitempty"`
	ToolCallID string            `json:"tool_call_id,omitempty"`
	Name       string            `json:"name,omitempty"`
}

type openAITool struct {
	Type     string     `json:"type"`
	Function openAIFunc  `json:"function"`
}

type openAIFunc struct {
	Name        string         `json:"name"`
	Description string         `json:"description"`
	Parameters  map[string]any `json:"parameters"`
}

type openAIToolCall struct {
	ID       string         `json:"id"`
	Type     string         `json:"type"`
	Function openAICallFunc `json:"function"`
}

type openAICallFunc struct {
	Name      string `json:"name"`
	Arguments string `json:"arguments"`
}

type openAIChatResponse struct {
	Choices []struct {
		Message struct {
			Content   string           `json:"content"`
			ToolCalls []openAIToolCall `json:"tool_calls"`
		} `json:"message"`
		FinishReason string `json:"finish_reason"`
	} `json:"choices"`
	Usage *struct {
		PromptTokens     int `json:"prompt_tokens"`
		CompletionTokens int `json:"completion_tokens"`
		TotalTokens      int `json:"total_tokens"`
	} `json:"usage"`
}

// Chat implements LLMProvider.
func (p *OpenAIProvider) Chat(ctx context.Context, req *ChatRequest) (*LLMResponse, error) {
	msgs := make([]openAIMsg, 0, len(req.Messages))
	for _, m := range req.Messages {
		msg := openAIMsg{Role: m.Role, Content: m.Content}
		if m.ToolCallID != "" {
			msg.ToolCallID = m.ToolCallID
			msg.Name = m.Name
		}
		if len(m.ToolCalls) > 0 {
			for _, tc := range m.ToolCalls {
				msg.ToolCalls = append(msg.ToolCalls, openAIToolCall{
					ID: tc.ID, Type: "function",
					Function: openAICallFunc{Name: tc.Function.Name, Arguments: tc.Function.Arguments},
				})
			}
		}
		msgs = append(msgs, msg)
	}

	body := openAIChatRequest{
		Model:           req.Model,
		Messages:        msgs,
		MaxTokens:       req.MaxTokens,
		Temperature:     req.Temperature,
		ReasoningEffort: req.ReasoningEffort,
	}
	if len(req.Tools) > 0 {
		body.Tools = make([]openAITool, 0, len(req.Tools))
		for _, t := range req.Tools {
			body.Tools = append(body.Tools, openAITool{
				Type:     "function",
				Function: openAIFunc{Name: t.Function.Name, Description: t.Function.Description, Parameters: t.Function.Parameters},
			})
		}
		if req.ToolChoice != nil {
			body.ToolChoice = req.ToolChoice
		} else {
			body.ToolChoice = "auto"
		}
	}

	var buf bytes.Buffer
	if err := json.NewEncoder(&buf).Encode(body); err != nil {
		return nil, err
	}

	base := strings.TrimSuffix(p.APIBase, "/")
	url := base + "/chat/completions"
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, url, &buf)
	if err != nil {
		return nil, err
	}
	httpReq.Header.Set("Content-Type", "application/json")
	if p.APIKey != "" {
		httpReq.Header.Set("Authorization", "Bearer "+p.APIKey)
	}

	resp, err := p.Client.Do(httpReq)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("api %s: %s", resp.Status, string(data))
	}

	var out openAIChatResponse
	if err := json.Unmarshal(data, &out); err != nil {
		return nil, err
	}
	if len(out.Choices) == 0 {
		return nil, fmt.Errorf("no choices in response")
	}

	choice := out.Choices[0]
	toolCalls := make([]ToolCallRequest, 0, len(choice.Message.ToolCalls))
	for _, tc := range choice.Message.ToolCalls {
		var args map[string]any
		if tc.Function.Arguments != "" {
			_ = json.Unmarshal([]byte(tc.Function.Arguments), &args)
		}
		if args == nil {
			args = make(map[string]any)
		}
		toolCalls = append(toolCalls, ToolCallRequest{
			ID: tc.ID, Name: tc.Function.Name, Arguments: args,
		})
	}

	r := &LLMResponse{
		Content:      choice.Message.Content,
		ToolCalls:   toolCalls,
		FinishReason: choice.FinishReason,
	}
	if out.Usage != nil {
		r.Usage = map[string]int{
			"prompt_tokens":     out.Usage.PromptTokens,
			"completion_tokens": out.Usage.CompletionTokens,
			"total_tokens":      out.Usage.TotalTokens,
		}
	}
	return r, nil
}
