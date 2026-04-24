package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/jacklicn/dipper-bot/providers"
	"github.com/jacklicn/dipper-bot/session"
)

const sessionSearchMaxChars = 80_000

// SessionSearchTool searches past sessions via FTS5 and summarizes hits.
type SessionSearchTool struct {
	Workspace      string
	Sessions       *session.SessionManager
	FTS            *session.FTSIndexer
	Provider       providers.LLMProvider
	Model          string
	currentSession string
	mu             sync.Mutex
}

// SetSessionKey sets the active session key (excluded from search results).
func (t *SessionSearchTool) SetSessionKey(key string) {
	t.mu.Lock()
	t.currentSession = key
	t.mu.Unlock()
}

func (t *SessionSearchTool) Name() string { return "session_search" }

func (t *SessionSearchTool) Description() string {
	return `Cross-session recall: FTS5 keyword search over past conversations, then short summaries per session.
Use when the user references prior work across sessions. Omit query to list recent sessions (no LLM cost).`
}

func (t *SessionSearchTool) Parameters() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"query": map[string]any{
				"type":        "string",
				"description": "Keywords; use broad OR-style terms. Empty = list recent sessions.",
			},
			"limit": map[string]any{
				"type":        "integer",
				"description": "Max sessions to summarize (default 3, max 5)",
			},
		},
	}
}

func (t *SessionSearchTool) Execute(ctx context.Context, params map[string]any) (string, error) {
	if t.Sessions == nil {
		return `{"success":false,"error":"sessions not available"}`, nil
	}

	query, _ := params["query"].(string)
	query = strings.TrimSpace(query)
	limit := 3
	if v, ok := params["limit"].(float64); ok && int(v) > 0 {
		limit = int(v)
	}
	if limit > 5 {
		limit = 5
	}

	t.mu.Lock()
	cur := t.currentSession
	t.mu.Unlock()

	if query == "" {
		list, err := t.Sessions.ListSessions(limit + 5)
		if err != nil {
			return "", err
		}
		var rows []map[string]any
		for _, s := range list {
			if s.Key == cur {
				continue
			}
			rows = append(rows, map[string]any{
				"session_id":    s.Key,
				"updated_at":    s.UpdatedAt.Format(time.RFC3339),
				"message_count": s.MsgCount,
			})
			if len(rows) >= limit {
				break
			}
		}
		b, _ := json.Marshal(map[string]any{"success": true, "mode": "recent", "results": rows})
		return string(b), nil
	}

	if t.FTS == nil {
		return `{"success":false,"error":"session FTS index not available; check workspace/memory/sessions_fts.db"}`, nil
	}

	hits, err := t.FTS.Search(query, cur, 80)
	if err != nil {
		return "", err
	}
	seen := map[string]struct{}{}
	var sessionOrder []string
	for _, h := range hits {
		if h.SessionKey == "" {
			continue
		}
		if _, ok := seen[h.SessionKey]; ok {
			continue
		}
		seen[h.SessionKey] = struct{}{}
		sessionOrder = append(sessionOrder, h.SessionKey)
		if len(sessionOrder) >= limit {
			break
		}
	}
	if len(sessionOrder) == 0 {
		b, _ := json.Marshal(map[string]any{"success": true, "query": query, "results": []any{}, "count": 0})
		return string(b), nil
	}

	type sumRow struct {
		SessionKey string `json:"session_id"`
		Summary    string `json:"summary,omitempty"`
		Error      string `json:"error,omitempty"`
	}
	var out []sumRow

	for _, sk := range sessionOrder {
		sess, err := t.Sessions.GetOrCreate(sk)
		if err != nil || sess == nil {
			out = append(out, sumRow{SessionKey: sk, Error: "load session failed"})
			continue
		}
		hist := sess.GetHistory(0, false)
		transcript := formatSessionTranscript(hist)
		if len(transcript) > sessionSearchMaxChars {
			transcript = transcript[:sessionSearchMaxChars] + "\n…[truncated]"
		}
		sum := ""
		if t.Provider != nil && t.Model != "" {
			sum, _ = summarizeSession(ctx, t.Provider, t.Model, query, sk, transcript)
		}
		if sum == "" {
			sum = "[No summary — paste preview]\n" + preview(transcript, 400)
		}
		out = append(out, sumRow{SessionKey: sk, Summary: sum})
	}

	b, _ := json.Marshal(map[string]any{"success": true, "query": query, "results": out, "count": len(out)})
	return string(b), nil
}

func preview(s string, n int) string {
	s = strings.TrimSpace(s)
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}

func formatSessionTranscript(hist []map[string]string) string {
	var b strings.Builder
	for _, m := range hist {
		role := strings.ToUpper(strings.TrimSpace(m["role"]))
		content := m["content"]
		fmt.Fprintf(&b, "[%s]: %s\n\n", role, content)
	}
	return b.String()
}

func summarizeSession(ctx context.Context, p providers.LLMProvider, model, query, sessionKey, transcript string) (string, error) {
	sys := "Summarize the past conversation transcript in past tense. Focus on: user goals, actions taken, outcomes, key commands/paths/errors. Be concise."
	user := fmt.Sprintf("Search topic: %s\nSession: %s\n\nTRANSCRIPT:\n%s", query, sessionKey, transcript)
	resp, err := p.Chat(ctx, &providers.ChatRequest{
		Model:       model,
		Messages:    []providers.Message{{Role: "system", Content: sys}, {Role: "user", Content: user}},
		MaxTokens:   900,
		Temperature: 0.1,
	})
	if err != nil || strings.TrimSpace(resp.Content) == "" {
		return "", err
	}
	return strings.TrimSpace(resp.Content), nil
}
