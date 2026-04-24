package tools

import (
	"context"
	"sync"
)

// Spawner is implemented by agent.SubagentManager to run background tasks.
type Spawner interface {
	Spawn(ctx context.Context, task, label, originChannel, originChatID string) (string, error)
}

// SpawnTool spawns a subagent for background task execution.
type SpawnTool struct {
	Spawner Spawner
	channel string
	chatID  string
	mu      sync.Mutex
}

// NewSpawnTool creates a SpawnTool.
func NewSpawnTool(spawner Spawner) *SpawnTool {
	return &SpawnTool{Spawner: spawner, channel: "cli", chatID: "direct"}
}

// SetContext sets the origin channel and chat ID for subagent announcements.
func (s *SpawnTool) SetContext(channel, chatID string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.channel = channel
	s.chatID = chatID
}

func (s *SpawnTool) Name() string { return "spawn" }

func (s *SpawnTool) Description() string {
	return "Spawn a subagent to handle a task in the background. Use for complex or time-consuming tasks. The subagent will report back when done."
}

func (s *SpawnTool) Parameters() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"task":  map[string]any{"type": "string", "description": "The task for the subagent to complete"},
			"label": map[string]any{"type": "string", "description": "Optional short label for the task (for display)"},
		},
		"required": []any{"task"},
	}
}

func (s *SpawnTool) Execute(ctx context.Context, params map[string]any) (string, error) {
	task, _ := params["task"].(string)
	if task == "" {
		return "Error: task is required", nil
	}
	label, _ := params["label"].(string)
	s.mu.Lock()
	ch, cid := s.channel, s.chatID
	s.mu.Unlock()
	if s.Spawner == nil {
		return "Error: spawn not configured", nil
	}
	return s.Spawner.Spawn(ctx, task, label, ch, cid)
}
