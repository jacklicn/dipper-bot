package agent

import (
	"testing"

	"github.com/jacklicn/dipper-bot/providers"
)

func TestBuildUsageEvent(t *testing.T) {
	resp := &providers.LLMResponse{
		ToolCalls: []providers.ToolCallRequest{
			{Name: "read_file", ID: "1", Arguments: map[string]any{}},
		},
		Usage: map[string]int{"prompt_tokens": 10, "completion_tokens": 2, "total_tokens": 12},
	}
	ev := BuildUsageEvent("s1", "primary", "", "custom", "gpt-4o", 0, resp)
	if ev.Source != "primary" {
		t.Fatalf("source %q", ev.Source)
	}
	if len(ev.ToolNames) != 1 || ev.ToolNames[0] != "read_file" {
		t.Fatalf("toolNames %+v", ev.ToolNames)
	}
	if ev.PromptTokens != 10 || ev.CompletionTokens != 2 {
		t.Fatalf("tokens %+v", ev)
	}
}
