package providers

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"google.golang.org/genai"
)

// GeminiProvider uses the official Google GenAI Go SDK (google.golang.org/genai).
type GeminiProvider struct {
	client       *genai.Client
	DefaultModel string
}

// NewGeminiProvider creates a Gemini provider using the genai SDK.
func NewGeminiProvider(apiKey, defaultModel string) (*GeminiProvider, error) {
	if defaultModel == "" {
		defaultModel = "gemini-2.0-flash"
	}
	client, err := genai.NewClient(context.Background(), &genai.ClientConfig{
		APIKey:  apiKey,
		Backend: genai.BackendGeminiAPI,
	})
	if err != nil {
		return nil, fmt.Errorf("genai client: %w", err)
	}
	return &GeminiProvider{client: client, DefaultModel: defaultModel}, nil
}

// GetDefaultModel implements LLMProvider.
func (p *GeminiProvider) GetDefaultModel() string {
	return p.DefaultModel
}

// Chat implements LLMProvider.
func (p *GeminiProvider) Chat(ctx context.Context, req *ChatRequest) (*LLMResponse, error) {
	contents, systemInstruction := messagesToGenAI(req.Messages)
	config := &genai.GenerateContentConfig{
		MaxOutputTokens:  int32(req.MaxTokens),
		Temperature:      genai.Ptr(float32(req.Temperature)),
		SystemInstruction: systemInstruction,
	}
	if len(req.Tools) > 0 {
		config.Tools = []*genai.Tool{{
			FunctionDeclarations: toolsToFunctionDeclarations(req.Tools),
		}}
	}

	result, err := p.client.Models.GenerateContent(ctx, req.Model, contents, config)
	if err != nil {
		return nil, err
	}

	content := result.Text()
	toolCalls := make([]ToolCallRequest, 0)
	for _, fc := range result.FunctionCalls() {
		args := fc.Args
		if args == nil {
			args = make(map[string]any)
		}
		toolCalls = append(toolCalls, ToolCallRequest{
			ID:        fc.ID,
			Name:      fc.Name,
			Arguments: args,
		})
	}

	finishReason := ""
	if len(result.Candidates) > 0 && result.Candidates[0].FinishReason != "" {
		finishReason = string(result.Candidates[0].FinishReason)
	}

	r := &LLMResponse{
		Content:      content,
		ToolCalls:    toolCalls,
		FinishReason: finishReason,
	}
	if result.UsageMetadata != nil {
		r.Usage = map[string]int{
			"prompt_tokens":     int(result.UsageMetadata.PromptTokenCount),
			"completion_tokens": int(result.UsageMetadata.CandidatesTokenCount),
			"total_tokens":      int(result.UsageMetadata.TotalTokenCount),
		}
	}
	return r, nil
}

func messagesToGenAI(msgs []Message) ([]*genai.Content, *genai.Content) {
	var contents []*genai.Content
	var systemParts []*genai.Part
	for _, m := range msgs {
		role := strings.ToLower(m.Role)
		if role == "system" {
			if m.Content != "" {
				systemParts = append(systemParts, &genai.Part{Text: m.Content})
			}
			continue
		}
		genRole := "user"
		if role == "assistant" {
			genRole = "model"
		}
		parts := make([]*genai.Part, 0)
		if m.ToolCallID != "" && m.Name != "" {
			resp := map[string]any{"output": m.Content}
			parts = append(parts, &genai.Part{
				FunctionResponse: &genai.FunctionResponse{
					ID:       m.ToolCallID,
					Name:     m.Name,
					Response: resp,
				},
			})
		} else if m.Content != "" {
			parts = append(parts, &genai.Part{Text: m.Content})
		}
		for _, tc := range m.ToolCalls {
			var args map[string]any
			if tc.Function.Arguments != "" {
				_ = json.Unmarshal([]byte(tc.Function.Arguments), &args)
			}
			if args == nil {
				args = make(map[string]any)
			}
			parts = append(parts, &genai.Part{
				FunctionCall: &genai.FunctionCall{
					ID:   tc.ID,
					Name: tc.Function.Name,
					Args: args,
				},
			})
		}
		if len(parts) > 0 {
			contents = append(contents, &genai.Content{Role: genRole, Parts: parts})
		}
	}
	var systemInstruction *genai.Content
	if len(systemParts) > 0 {
		systemInstruction = &genai.Content{Parts: systemParts}
	}
	return contents, systemInstruction
}

func toolsToFunctionDeclarations(tools []ToolDef) []*genai.FunctionDeclaration {
	out := make([]*genai.FunctionDeclaration, 0, len(tools))
	for _, t := range tools {
		schema := t.Function.Parameters
		if schema == nil {
			schema = map[string]any{"type": "object", "properties": map[string]any{}}
		}
		if _, has := schema["type"]; !has {
			schema["type"] = "object"
		}
		out = append(out, &genai.FunctionDeclaration{
			Name:        t.Function.Name,
			Description: t.Function.Description,
			ParametersJsonSchema: schema,
		})
	}
	return out
}
