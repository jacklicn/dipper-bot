package providers

import (
	"bufio"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
)

const (
	codexURL    = "https://chatgpt.com/backend-api/codex/responses"
	codexOrigin = "dipper-bot"
)

// CodexProvider calls OpenAI Codex Responses API (OAuth).
type CodexProvider struct {
	AccessToken  string
	AccountID    string
	DefaultModel string
	Client       *http.Client
}

// NewCodexProvider creates a Codex provider. AccessToken and AccountID come from OAuth login.
func NewCodexProvider(accessToken, accountID, defaultModel string) *CodexProvider {
	if defaultModel == "" {
		defaultModel = "openai-codex/gpt-5.1-codex"
	}
	return &CodexProvider{
		AccessToken:  accessToken,
		AccountID:    accountID,
		DefaultModel: defaultModel,
		Client:       http.DefaultClient,
	}
}

// GetDefaultModel implements LLMProvider.
func (p *CodexProvider) GetDefaultModel() string {
	return p.DefaultModel
}

func stripModelPrefix(model string) string {
	if strings.HasPrefix(model, "openai-codex/") {
		return strings.TrimPrefix(model, "openai-codex/")
	}
	if strings.HasPrefix(model, "openai_codex/") {
		return strings.TrimPrefix(model, "openai_codex/")
	}
	return model
}

func promptCacheKey(messages []Message) string {
	raw, _ := json.Marshal(messages)
	h := sha256.Sum256(raw)
	return hex.EncodeToString(h[:])
}

func convertMessages(msgs []Message) (systemPrompt string, inputItems []map[string]any) {
	for idx, m := range msgs {
		switch m.Role {
		case "system":
			systemPrompt = m.Content
		case "user":
			inputItems = append(inputItems, map[string]any{
				"role":    "user",
				"content": []map[string]any{{"type": "input_text", "text": m.Content}},
			})
		case "assistant":
			if m.Content != "" {
				inputItems = append(inputItems, map[string]any{
					"type":    "message",
					"role":    "assistant",
					"content": []map[string]any{{"type": "output_text", "text": m.Content}},
					"status":  "completed",
					"id":      "msg_" + fmt.Sprint(idx),
				})
			}
			for i, tc := range m.ToolCalls {
				callID, itemID := splitToolCallID(tc.ID)
				if callID == "" {
					callID = "call_" + fmt.Sprint(idx)
				}
				if itemID == "" {
					itemID = "fc_" + fmt.Sprint(i)
				}
				args := tc.Function.Arguments
				if args == "" {
					args = "{}"
				}
				inputItems = append(inputItems, map[string]any{
					"type":      "function_call",
					"id":        itemID,
					"call_id":   callID,
					"name":      tc.Function.Name,
					"arguments": args,
				})
			}
		case "tool":
			callID, _ := splitToolCallID(m.ToolCallID)
			if callID == "" {
				callID = "call_0"
			}
			output := m.Content
			if output == "" {
				output = "{}"
			}
			inputItems = append(inputItems, map[string]any{
				"type":    "function_call_output",
				"call_id": callID,
				"output":  output,
			})
		}
	}
	return systemPrompt, inputItems
}

func splitToolCallID(id string) (callID, itemID string) {
	if id == "" {
		return "call_0", ""
	}
	if idx := strings.Index(id, "|"); idx >= 0 {
		return id[:idx], id[idx+1:]
	}
	return id, ""
}

func convertTools(tools []ToolDef) []map[string]any {
	out := make([]map[string]any, 0, len(tools))
	for _, t := range tools {
		fn := t.Function
		params := fn.Parameters
		if params == nil {
			params = make(map[string]any)
		}
		out = append(out, map[string]any{
			"type":        "function",
			"name":        fn.Name,
			"description": fn.Description,
			"parameters":  params,
		})
	}
	return out
}

// Chat implements LLMProvider.
func (p *CodexProvider) Chat(ctx context.Context, req *ChatRequest) (*LLMResponse, error) {
	systemPrompt, inputItems := convertMessages(req.Messages)

	body := map[string]any{
		"model":               stripModelPrefix(req.Model),
		"store":               false,
		"stream":              true,
		"instructions":        systemPrompt,
		"input":               inputItems,
		"text":                map[string]string{"verbosity": "medium"},
		"include":             []string{"reasoning.encrypted_content"},
		"prompt_cache_key":    promptCacheKey(req.Messages),
		"tool_choice":         "auto",
		"parallel_tool_calls": true,
	}
	if len(req.Tools) > 0 {
		body["tools"] = convertTools(req.Tools)
	}
	if req.ReasoningEffort != "" {
		body["reasoning"] = map[string]string{"effort": req.ReasoningEffort}
	}

	bodyBytes, err := json.Marshal(body)
	if err != nil {
		return nil, err
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, codexURL, bytes.NewReader(bodyBytes))
	if err != nil {
		return nil, err
	}
	httpReq.Header.Set("Authorization", "Bearer "+p.AccessToken)
	httpReq.Header.Set("chatgpt-account-id", p.AccountID)
	httpReq.Header.Set("OpenAI-Beta", "responses=experimental")
	httpReq.Header.Set("originator", codexOrigin)
	httpReq.Header.Set("User-Agent", "dipper-bot")
	httpReq.Header.Set("Accept", "text/event-stream")
	httpReq.Header.Set("Content-Type", "application/json")

	resp, err := p.Client.Do(httpReq)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		data, _ := io.ReadAll(resp.Body)
		msg := string(data)
		if resp.StatusCode == 429 {
			msg = "ChatGPT usage quota exceeded or rate limit triggered. Please try again later."
		}
		return nil, fmt.Errorf("codex api %s: %s", resp.Status, msg)
	}

	content, toolCalls, finishReason := consumeSSE(resp.Body)
	return &LLMResponse{
		Content:      content,
		ToolCalls:    toolCalls,
		FinishReason: finishReason,
	}, nil
}

func consumeSSE(r io.Reader) (content string, toolCalls []ToolCallRequest, finishReason string) {
	finishReason = "stop"
	toolCallBuffers := make(map[string]map[string]any)

	scanner := bufio.NewScanner(r)
	scanner.Buffer(nil, 1024*1024)
	var buf []string

	for scanner.Scan() {
		line := scanner.Text()
		if line == "" {
			if len(buf) > 0 {
				var dataLines []string
				for _, l := range buf {
					if strings.HasPrefix(l, "data:") {
						dataLines = append(dataLines, strings.TrimSpace(strings.TrimPrefix(l, "data:")))
					}
				}
				buf = nil
				if len(dataLines) == 0 {
					continue
				}
				data := strings.Join(dataLines, "\n")
				if data == "" || data == "[DONE]" {
					continue
				}
				var event map[string]any
				if err := json.Unmarshal([]byte(data), &event); err != nil {
					continue
				}
				eventType, _ := event["type"].(string)
				switch eventType {
				case "response.output_item.added":
					if item, ok := event["item"].(map[string]any); ok {
						if typ, _ := item["type"].(string); typ == "function_call" {
							callID, _ := item["call_id"].(string)
							if callID != "" {
								toolCallBuffers[callID] = map[string]any{
									"id":        item["id"],
									"name":      item["name"],
									"arguments": "",
								}
							}
						}
					}
				case "response.output_text.delta":
					if d, ok := event["delta"].(string); ok {
						content += d
					}
				case "response.function_call_arguments.delta":
					if callID, ok := event["call_id"].(string); ok && toolCallBuffers[callID] != nil {
						if d, ok := event["delta"].(string); ok {
							toolCallBuffers[callID]["arguments"] = toolCallBuffers[callID]["arguments"].(string) + d
						}
					}
				case "response.function_call_arguments.done":
					if callID, ok := event["call_id"].(string); ok && toolCallBuffers[callID] != nil {
						if args, ok := event["arguments"].(string); ok {
							toolCallBuffers[callID]["arguments"] = args
						}
					}
				case "response.output_item.done":
					if item, ok := event["item"].(map[string]any); ok {
						if typ, _ := item["type"].(string); typ == "function_call" {
							callID, _ := item["call_id"].(string)
							if callID != "" {
								buf := toolCallBuffers[callID]
								if buf == nil {
									buf = make(map[string]any)
								}
								argsRaw, _ := buf["arguments"].(string)
								if argsRaw == "" {
									argsRaw, _ = item["arguments"].(string)
								}
								if argsRaw == "" {
									argsRaw = "{}"
								}
								var args map[string]any
								_ = json.Unmarshal([]byte(argsRaw), &args)
								if args == nil {
									args = map[string]any{"raw": argsRaw}
								}
								id, _ := buf["id"].(string)
								if id == "" {
									id, _ = item["id"].(string)
								}
								if id == "" {
									id = "fc_0"
								}
								name, _ := buf["name"].(string)
								if name == "" {
									name, _ = item["name"].(string)
								}
								toolCalls = append(toolCalls, ToolCallRequest{
									ID:        callID + "|" + id,
									Name:      name,
									Arguments: args,
								})
							}
						}
					}
				case "response.completed":
					if resp, ok := event["response"].(map[string]any); ok {
						if status, ok := resp["status"].(string); ok {
							switch status {
							case "completed":
								finishReason = "stop"
							case "incomplete":
								finishReason = "length"
							case "failed", "cancelled":
								finishReason = "error"
							}
						}
					}
				case "error", "response.failed":
					finishReason = "error"
				}
			}
			continue
		}
		buf = append(buf, line)
	}
	return content, toolCalls, finishReason
}
