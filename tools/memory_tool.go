package tools

import (
	"context"
)

// MemoryTool provides curated memory (USER.md + NOTE.md under memory/).
type MemoryTool struct {
	Store *MemoryNoteStore
}

func (m *MemoryTool) Name() string { return "memory" }

func (m *MemoryTool) Description() string {
	return `Save durable facts to persistent curated memory (survives sessions). Two targets:
- "user": user profile (preferences, style) -> memory/USER.md — when the user corrects earlier profile info, use replace/remove so USER.md stays one coherent model (no separate Honcho-style service; this file is the user model).
- "memory": agent memory notes -> memory/NOTE.md (do not confuse with MEMORY.md from MemoryConsolidator)
Actions: add, replace (old_text + content), remove (old_text).
Entries are §-delimited. Keep compact to save tokens.`
}

func (m *MemoryTool) Parameters() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"action": map[string]any{
				"type":        "string",
				"enum":        []any{"add", "replace", "remove"},
				"description": "Operation to perform.",
			},
			"target": map[string]any{
				"type":        "string",
				"enum":        []any{"memory", "user"},
				"description": "memory = NOTE.md agent notes; user = USER.md profile",
			},
			"content": map[string]any{
				"type":        "string",
				"description": "Entry text for add/replace.",
			},
			"old_text": map[string]any{
				"type":        "string",
				"description": "Unique substring identifying entry for replace/remove.",
			},
		},
		"required": []any{"action", "target"},
	}
}

func (m *MemoryTool) Execute(ctx context.Context, params map[string]any) (string, error) {
	if m.Store == nil {
		return `{"success":false,"error":"memory store not configured"}`, nil
	}
	action, _ := params["action"].(string)
	target, _ := params["target"].(string)
	if target == "" {
		target = "memory"
	}
	content, _ := params["content"].(string)
	oldText, _ := params["old_text"].(string)
	return m.Store.ExecuteMemoryTool(action, target, content, oldText), nil
}
