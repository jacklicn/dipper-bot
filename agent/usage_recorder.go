package agent

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/jacklicn/dipper-bot/config"
	"github.com/jacklicn/dipper-bot/providers"
)

// UsageEvent is one LLM completion row (Hermes-style usage trail).
type UsageEvent struct {
	Time             string         `json:"time"`
	Source           string         `json:"source,omitempty"` // primary | subagent
	SessionKey       string         `json:"sessionKey"`
	TaskID           string         `json:"taskId,omitempty"` // subagent spawn id
	Model            string         `json:"model"`
	ProviderName     string         `json:"providerName,omitempty"`
	LoopIteration    int            `json:"loopIteration"`
	PromptTokens     int            `json:"promptTokens"`
	CompletionTokens int            `json:"completionTokens"`
	TotalTokens      int            `json:"totalTokens"`
	ToolCalls        int            `json:"toolCalls,omitempty"`
	ToolNames        []string       `json:"toolNames,omitempty"` // tools requested in this completion
	Usage            map[string]int `json:"usageRaw,omitempty"`
}

var usageMu sync.Mutex

// BuildUsageEvent maps one Chat response into a UsageEvent.
func BuildUsageEvent(sessionKey, source, taskID, providerName, resolvedModel string, iter int, resp *providers.LLMResponse) UsageEvent {
	ev := UsageEvent{
		SessionKey:    sessionKey,
		Source:        source,
		TaskID:        taskID,
		Model:         resolvedModel,
		ProviderName:  providerName,
		LoopIteration: iter,
	}
	if resp == nil {
		return ev
	}
	if len(resp.ToolCalls) > 0 {
		ev.ToolNames = make([]string, 0, len(resp.ToolCalls))
		for _, tc := range resp.ToolCalls {
			ev.ToolNames = append(ev.ToolNames, tc.Name)
		}
	}
	ev.ToolCalls = len(resp.ToolCalls)
	if resp.Usage != nil {
		ev.PromptTokens = resp.Usage["prompt_tokens"]
		ev.CompletionTokens = resp.Usage["completion_tokens"]
		ev.TotalTokens = resp.Usage["total_tokens"]
		ev.Usage = make(map[string]int, len(resp.Usage))
		for k, v := range resp.Usage {
			ev.Usage[k] = v
		}
	}
	return ev
}

// MaybeRecordUsage appends when experience config allows recording.
func MaybeRecordUsage(workspace string, exp config.AgentExperienceConfig, ev UsageEvent) {
	if exp.DisableUsageRecording {
		return
	}
	RecordUsageEvent(workspace, ev)
}

// RecordUsageEvent appends one JSON line to workspace/memory/usage_events.jsonl.
func RecordUsageEvent(workspace string, ev UsageEvent) {
	if workspace == "" {
		return
	}
	if ev.Time == "" {
		ev.Time = time.Now().UTC().Format(time.RFC3339)
	}
	if ev.Source == "" {
		ev.Source = "primary"
	}
	usageMu.Lock()
	defer usageMu.Unlock()
	dir := filepath.Join(workspace, "memory")
	_ = os.MkdirAll(dir, 0o750)
	p := filepath.Join(dir, "usage_events.jsonl")
	b, err := json.Marshal(ev)
	if err != nil {
		return
	}
	f, err := os.OpenFile(p, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		return
	}
	defer f.Close()
	_, _ = f.Write(append(b, '\n'))
}
