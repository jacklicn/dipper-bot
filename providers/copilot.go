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

const (
	copilotDefaultBase = "https://api.githubcopilot.com"
	copilotUserAgent   = "GithubCopilot/1.155.0"
)

// GitHubCopilotProvider calls the GitHub Copilot Chat API (OpenAI-compatible, OAuth).
type GitHubCopilotProvider struct {
	APIKey       string
	APIBase      string
	DefaultModel string
	Client       *http.Client
}

// NewGitHubCopilotProvider creates a Copilot provider. APIKey comes from OAuth (api-key.json).
func NewGitHubCopilotProvider(apiKey, apiBase, defaultModel string) *GitHubCopilotProvider {
	if apiBase == "" {
		apiBase = copilotDefaultBase
	}
	if defaultModel == "" {
		defaultModel = "github_copilot/gpt-4o"
	}
	return &GitHubCopilotProvider{
		APIKey:       apiKey,
		APIBase:      strings.TrimSuffix(apiBase, "/"),
		DefaultModel: defaultModel,
		Client:       http.DefaultClient,
	}
}

// GetDefaultModel implements LLMProvider.
func (p *GitHubCopilotProvider) GetDefaultModel() string {
	return p.DefaultModel
}

func stripCopilotModelPrefix(model string) string {
	if strings.HasPrefix(model, "github-copilot/") {
		return strings.TrimPrefix(model, "github-copilot/")
	}
	if strings.HasPrefix(model, "github_copilot/") {
		return strings.TrimPrefix(model, "github_copilot/")
	}
	return model
}

// Chat implements LLMProvider.
func (p *GitHubCopilotProvider) Chat(ctx context.Context, req *ChatRequest) (*LLMResponse, error) {
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

	model := stripCopilotModelPrefix(req.Model)
	if model == "" {
		model = stripCopilotModelPrefix(p.DefaultModel)
	}
	if model == "" {
		model = "gpt-4o"
	}

	body := openAIChatRequest{
		Model:       model,
		Messages:    msgs,
		MaxTokens:   req.MaxTokens,
		Temperature: req.Temperature,
	}
	if len(req.Tools) > 0 {
		body.Tools = make([]openAITool, 0, len(req.Tools))
		for _, t := range req.Tools {
			body.Tools = append(body.Tools, openAITool{
				Type:     "function",
				Function: openAIFunc{Name: t.Function.Name, Description: t.Function.Description, Parameters: t.Function.Parameters},
			})
		}
		body.ToolChoice = "auto"
	}

	var buf bytes.Buffer
	if err := json.NewEncoder(&buf).Encode(body); err != nil {
		return nil, err
	}

	url := p.APIBase + "/chat/completions"
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, url, &buf)
	if err != nil {
		return nil, err
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Authorization", "Bearer "+p.APIKey)
	httpReq.Header.Set("Editor-Version", "vscode/1.95.0")
	httpReq.Header.Set("Editor-Plugin-Version", "copilot-chat/0.26.7")
	httpReq.Header.Set("Copilot-Integration-Id", "vscode-chat")
	httpReq.Header.Set("User-Agent", copilotUserAgent)
	httpReq.Header.Set("Openai-Intent", "conversation-panel")
	httpReq.Header.Set("X-Github-Api-Version", "2025-04-01")

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
		return nil, fmt.Errorf("copilot api %s: %s", resp.Status, string(data))
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
		ToolCalls:    toolCalls,
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
