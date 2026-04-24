package agent

import "testing"

func TestMcpToolDedupKey_stableForSameArgs(t *testing.T) {
	k1, ok1 := mcpToolDedupKey("mcp_chrome-devtools_new_page", map[string]any{"url": "https://example.com/"})
	k2, ok2 := mcpToolDedupKey("mcp_chrome-devtools_new_page", map[string]any{"url": "https://example.com/"})
	if !ok1 || !ok2 || k1 != k2 {
		t.Fatalf("expected same key, got %q vs %q ok %v %v", k1, k2, ok1, ok2)
	}
}

func TestMcpToolDedupKey_mapKeyOrder(t *testing.T) {
	k1, _ := mcpToolDedupKey("mcp_chrome-devtools_new_page", map[string]any{"url": "https://a.test", "background": false})
	k2, _ := mcpToolDedupKey("mcp_chrome-devtools_new_page", map[string]any{"background": false, "url": "https://a.test"})
	if k1 != k2 {
		t.Fatalf("json.Marshal should sort keys so dedup matches; got\n%q\nvs\n%q", k1, k2)
	}
}

func TestMcpToolDedupKey_nonMCP(t *testing.T) {
	_, ok := mcpToolDedupKey("web_fetch", map[string]any{"url": "x"})
	if ok {
		t.Fatal("expected non-mcp tool to not get dedup key")
	}
}
