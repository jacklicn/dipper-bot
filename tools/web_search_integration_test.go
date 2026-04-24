package tools

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"
)

// TestWebSearchTool_DuckDuckGo_live hits the real DuckDuckGo path (ddgsearch).
// Run only when DIPPER_ENABLE_LIVE_WEB_TESTS=1 to avoid flaky default CI/local runs.
func TestWebSearchTool_DuckDuckGo_live(t *testing.T) {
	if testing.Short() {
		t.Skip("skip live DuckDuckGo in -short")
	}
	if os.Getenv("DIPPER_ENABLE_LIVE_WEB_TESTS") != "1" {
		t.Skip("set DIPPER_ENABLE_LIVE_WEB_TESTS=1 to run live web integration tests")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()

	w := NewWebSearchTool("duckduckgo", "", "", 5, "")
	// Short topical query: api.duckduckgo.com returns Abstract + RelatedTopics; ddgsearch may still fail if DDG changes d.js.
	out, err := w.Execute(ctx, map[string]any{"query": "golang", "count": 3})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if strings.HasPrefix(strings.TrimSpace(out), "Error:") {
		t.Fatal(out)
	}
	if !strings.Contains(out, "Results for:") {
		t.Fatalf("expected result header, got: %s", truncateStr(out, 600))
	}
	if strings.Contains(out, "No results for:") {
		t.Fatalf("unexpected empty results: %s", truncateStr(out, 600))
	}
	// At least one numbered line (title / URL block)
	if !strings.Contains(out, "1.") {
		t.Fatalf("expected at least one numbered hit: %s", truncateStr(out, 600))
	}
}

func truncateStr(s string, max int) string {
	if len(s) <= max {
		return s
	}
	return s[:max] + "..."
}
