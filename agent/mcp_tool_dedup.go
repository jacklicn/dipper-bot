package agent

import (
	"encoding/json"
	"strings"
)

// mcpToolDedupKey returns a stable key for (tool name, JSON arguments) when the tool
// is an MCP-backed function (name prefix mcp_). Used to skip duplicate invocations
// in the same assistant message—models often emit parallel identical new_page calls.
func mcpToolDedupKey(name string, args map[string]any) (key string, ok bool) {
	if name == "" || !strings.HasPrefix(name, "mcp_") {
		return "", false
	}
	a := args
	if a == nil {
		a = map[string]any{}
	}
	b, err := json.Marshal(a)
	if err != nil {
		b = []byte("{}")
	}
	return name + "\x00" + string(b), true
}
