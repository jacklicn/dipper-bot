package lcm

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"
	"time"
)

// EstimateTokens returns rough token count (~4 chars per token).
func EstimateTokens(s string) int {
	return (len(s) + 3) / 4
}

// Assemble builds the context for the model: summaries (evictable) + fresh tail (protected).
func Assemble(items []ContextItem, cfg Config, maxContextTokens int) []ContextItem {
	freshTail := cfg.FreshTailCount
	if freshTail <= 0 {
		freshTail = 32
	}
	threshold := int(float64(maxContextTokens) * cfg.ContextThreshold)
	if threshold <= 0 {
		threshold = maxContextTokens
	}

	n := len(items)
	if n == 0 {
		return nil
	}

	// Split: evictable prefix (summaries + old messages) vs protected fresh tail
	freshStart := n - freshTail
	if freshStart < 0 {
		freshStart = 0
	}

	// Count raw messages in tail (always include)
	tailTokens := 0
	for i := freshStart; i < n; i++ {
		tailTokens += EstimateTokens(items[i].Content)
		if items[i].Role != "" {
			tailTokens += 10 // role overhead
		}
	}

	// Fill budget from evictable, keeping newest
	budget := threshold - tailTokens
	if budget <= 0 {
		return items[freshStart:]
	}

	var out []ContextItem
	used := 0
	for i := freshStart - 1; i >= 0 && used < budget; i-- {
		tok := EstimateTokens(items[i].Content) + 10
		if used+tok > budget {
			break
		}
		used += tok
		out = append([]ContextItem{items[i]}, out...)
	}
	out = append(out, items[freshStart:]...)
	return out
}

// FormatSummaryAsMessage wraps summary content in XML for the model.
func FormatSummaryAsMessage(summaryID, kind string, depth, descendantCount int, earliest, latest time.Time, content string) string {
	earliestStr := earliest.Format(time.RFC3339)
	latestStr := latest.Format(time.RFC3339)
	return fmt.Sprintf(`<summary id="%s" kind="%s" depth="%d" descendant_count="%d" earliest_at="%s" latest_at="%s">
<content>
%s
</content>
</summary>`, summaryID, kind, depth, descendantCount, earliestStr, latestStr, content)
}

// FormatContextItem formats a context item for the LLM. Summaries get XML wrapper.
func FormatContextItem(it ContextItem) string {
	if it.ItemType == "summary" {
		return FormatSummaryAsMessage(it.SummaryID, it.SummaryKind, it.SummaryDepth, it.SummaryDescCount,
			it.SummaryEarliest, it.SummaryLatest, it.Content)
	}
	return it.Content
}

// ItemsToRoleContent converts assembled items to role+content pairs for the LLM.
func ItemsToRoleContent(items []ContextItem) []struct{ Role, Content string } {
	out := make([]struct{ Role, Content string }, 0, len(items))
	for _, it := range items {
		if it.ItemType == "summary" {
			out = append(out, struct{ Role, Content string }{"user", FormatContextItem(it)})
		} else {
			out = append(out, struct{ Role, Content string }{it.Role, it.Content})
		}
	}
	return out
}

// GenerateSummaryID creates a unique ID for a summary.
func GenerateSummaryID(content string) string {
	h := sha256.Sum256([]byte(content + time.Now().Format(time.RFC3339Nano)))
	return "sum_" + hex.EncodeToString(h[:8])
}

// TruncateFallback truncates content for compaction fallback when LLM fails.
func TruncateFallback(s string, maxTokens int) string {
	maxChars := maxTokens * 4
	if len(s) <= maxChars {
		return s
	}
	truncated := s[:maxChars]
	if !strings.HasSuffix(truncated, "\n") {
		truncated += "\n"
	}
	return truncated + "[Truncated for context management]"
}
