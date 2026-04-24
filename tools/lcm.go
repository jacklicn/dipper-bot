package tools

import (
	"context"
	"encoding/json"
	"regexp"
	"sync"

	"github.com/jacklicn/dipper-bot/lcm"
)

// LcmGrepTool searches compacted history via regex (LCM).
type LcmGrepTool struct {
	Engine *lcm.Engine
	mu     sync.RWMutex
	key    string
}

// NewLcmGrepTool creates an lcm_grep tool.
func NewLcmGrepTool(engine *lcm.Engine) *LcmGrepTool {
	return &LcmGrepTool{Engine: engine}
}

// SetSessionKey sets the current session for grep (called from agent loop).
func (t *LcmGrepTool) SetSessionKey(key string) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.key = key
}

// Name returns the tool name.
func (t *LcmGrepTool) Name() string { return "lcm_grep" }

// Description returns the tool description.
func (t *LcmGrepTool) Description() string {
	return "Search compacted conversation history by regex. Use when you need to recall details from earlier in the session. Returns matching messages and summaries with context."
}

// Parameters returns the JSON schema.
func (t *LcmGrepTool) Parameters() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"pattern": map[string]any{
				"type":        "string",
				"description": "Regex pattern to search for (e.g. 'error|failed', 'config')",
			},
			"limit": map[string]any{
				"type":        "number",
				"description": "Max results to return (default 10)",
			},
		},
		"required": []any{"pattern"},
	}
}

// Execute runs the grep.
func (t *LcmGrepTool) Execute(ctx context.Context, params map[string]any) (string, error) {
	if t.Engine == nil {
		return "LCM is not enabled.", nil
	}
	t.mu.RLock()
	key := t.key
	t.mu.RUnlock()
	if key == "" {
		return "No active session.", nil
	}
	pattern, _ := params["pattern"].(string)
	if pattern == "" {
		return "Pattern is required.", nil
	}
	limit := 10
	if n, ok := params["limit"].(float64); ok && n > 0 {
		limit = int(n)
	}
	re, err := regexp.Compile(pattern)
	if err != nil {
		return "Invalid regex: " + err.Error(), nil
	}
	convID, err := t.Engine.GetConversationID(key)
	if err != nil {
		return "Error: " + err.Error(), nil
	}
	items, err := t.Engine.Search(convID, re, limit)
	if err != nil {
		return "Error: " + err.Error(), nil
	}
	if len(items) == 0 {
		return "No matches found.", nil
	}
	out, _ := json.MarshalIndent(items, "", "  ")
	return string(out), nil
}

// LcmDescribeTool returns a high-level description of compacted history (LCM).
type LcmDescribeTool struct {
	Engine *lcm.Engine
	mu     sync.RWMutex
	key    string
}

// NewLcmDescribeTool creates an lcm_describe tool.
func NewLcmDescribeTool(engine *lcm.Engine) *LcmDescribeTool {
	return &LcmDescribeTool{Engine: engine}
}

// SetSessionKey sets the current session.
func (t *LcmDescribeTool) SetSessionKey(key string) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.key = key
}

// Name returns the tool name.
func (t *LcmDescribeTool) Name() string { return "lcm_describe" }

// Description returns the tool description.
func (t *LcmDescribeTool) Description() string {
	return "Get a high-level description of the compacted conversation so far. Use when you need an overview of what has been discussed, decided, or built."
}

// Parameters returns the JSON schema.
func (t *LcmDescribeTool) Parameters() map[string]any {
	return map[string]any{
		"type":       "object",
		"properties": map[string]any{},
	}
}

// Execute runs the describe.
func (t *LcmDescribeTool) Execute(ctx context.Context, params map[string]any) (string, error) {
	if t.Engine == nil {
		return "LCM is not enabled.", nil
	}
	t.mu.RLock()
	key := t.key
	t.mu.RUnlock()
	if key == "" {
		return "No active session.", nil
	}
	convID, err := t.Engine.GetConversationID(key)
	if err != nil {
		return "Error: " + err.Error(), nil
	}
	desc, err := t.Engine.Describe(convID)
	if err != nil {
		return "Error: " + err.Error(), nil
	}
	return desc, nil
}
