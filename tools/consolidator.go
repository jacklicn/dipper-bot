package tools

import (
	"context"
	"encoding/json"
	"log/slog"
	"math"
	"strings"
	"sync"
	"time"

	"github.com/jacklicn/dipper-bot/providers"
	"github.com/jacklicn/dipper-bot/session"
)

const maxConsolidationRounds = 5
const maxFailuresBeforeRawArchive = 3

// MemoryStoreForConsolidator is the interface for memory store used by MemoryConsolidator.
type MemoryStoreForConsolidator interface {
	ReadLongTerm() (string, error)
	WriteLongTerm(content string) error
	AppendHistory(entry string) error
}

// saveMemoryTool is the tool definition for memory consolidation.
var saveMemoryTool = providers.ToolDef{
	Type: "function",
	Function: providers.ToolFunction{
		Name:        "save_memory",
		Description: "Save the memory consolidation result to persistent storage.",
		Parameters: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"history_entry": map[string]any{
					"type":        "string",
					"description": "A paragraph summarizing key events/decisions/topics. Start with [YYYY-MM-DD HH:MM]. Include detail useful for grep search.",
				},
				"memory_update": map[string]any{
					"type":        "string",
					"description": "Full updated long-term memory as markdown. Include all existing facts plus new ones. Return unchanged if nothing new.",
				},
			},
			"required": []any{"history_entry", "memory_update"},
		},
	},
}

// MemoryConsolidator archives old session messages into MEMORY.md + HISTORY.md when prompt exceeds context window.
type MemoryConsolidator struct {
	store       MemoryStoreForConsolidator
	provider    providers.LLMProvider
	model       string
	sessions    *session.SessionManager
	contextTok  int
	buildMsgs   func(ctx context.Context, history []map[string]string, current string, channel, chatID, sessionKey string) ([]providers.Message, error)
	getToolDefs func() []providers.ToolDef
	logPressure bool // slog estimated prompt vs context window (Hermes-style compaction signal)
	mu          sync.Mutex
	locks       map[string]*sync.Mutex
	consecFail  int
}

// SetLogContextPressure enables info logs when estimated prompt size approaches contextWindowTokens.
func (c *MemoryConsolidator) SetLogContextPressure(v bool) {
	if c == nil {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	c.logPressure = v
}

// NewMemoryConsolidator creates a memory consolidator.
func NewMemoryConsolidator(
	store MemoryStoreForConsolidator,
	provider providers.LLMProvider,
	model string,
	sessions *session.SessionManager,
	contextWindowTokens int,
	buildMsgs func(ctx context.Context, history []map[string]string, current string, channel, chatID, sessionKey string) ([]providers.Message, error),
	getToolDefs func() []providers.ToolDef,
) *MemoryConsolidator {
	return &MemoryConsolidator{
		store:       store,
		provider:    provider,
		model:       model,
		sessions:    sessions,
		contextTok:  contextWindowTokens,
		buildMsgs:   buildMsgs,
		getToolDefs: getToolDefs,
		locks:       make(map[string]*sync.Mutex),
	}
}

func (c *MemoryConsolidator) getLock(key string) *sync.Mutex {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.locks[key] == nil {
		c.locks[key] = &sync.Mutex{}
	}
	return c.locks[key]
}

// EstimateMessageTokens returns approximate token count for a session message (heuristic: ~4 chars/token).
func EstimateMessageTokens(role, content string) int {
	n := len(role) + len(content) + 10 // overhead for structure
	if n == 0 {
		return 1
	}
	tok := n / 4
	if tok < 1 {
		return 1
	}
	return tok
}

// EstimateSessionPromptTokens estimates total prompt tokens for the session (builds probe, uses heuristic).
func (c *MemoryConsolidator) EstimateSessionPromptTokens(ctx context.Context, sess *session.Session, channel, chatID, sessionKey string) int {
	history := sess.GetHistory(0, true) // all unconsolidated
	msgs, err := c.buildMsgs(ctx, history, "[token-probe]", channel, chatID, sessionKey)
	if err != nil {
		return 0
	}
	var n int
	for _, m := range msgs {
		n += len(m.Role) + len(m.Content) + 20
	}
	tools := c.getToolDefs()
	if len(tools) > 0 {
		b, _ := json.Marshal(tools)
		n += len(b)
	}
	tok := n / 4
	if tok < 1 {
		return 1
	}
	return tok
}

// PickConsolidationBoundary finds a user-turn boundary that removes at least tokensToRemove tokens.
func (c *MemoryConsolidator) PickConsolidationBoundary(sess *session.Session, tokensToRemove int) (endIdx int, removedTokens int, ok bool) {
	start := sess.LastConsolidatedIndex()
	all := sess.GetMessagesFrom(0, 999999)
	if start >= len(all) || tokensToRemove <= 0 {
		return 0, 0, false
	}
	removed := 0
	lastBoundary := -1
	for i := start; i < len(all); i++ {
		m := all[i]
		if i > start && m.Role == "user" {
			lastBoundary = i
			if removed >= tokensToRemove {
				return i, removed, true
			}
		}
		removed += EstimateMessageTokens(m.Role, m.Content)
	}
	if lastBoundary >= 0 {
		return lastBoundary, removed, true
	}
	return 0, 0, false
}

// formatMessagesForConsolidation formats session messages for the consolidation prompt.
func formatMessagesForConsolidation(msgs []session.Message) string {
	var lines []string
	for _, m := range msgs {
		ts := m.Timestamp.Format("2006-01-02 15:04")
		tools := ""
		if len(m.ToolsUsed) > 0 {
			tools = " [tools: " + strings.Join(m.ToolsUsed, ", ") + "]"
		}
		lines = append(lines, "["+ts+"] "+m.Role+" "+tools+": "+m.Content)
	}
	return strings.Join(lines, "\n")
}

// ConsolidateMessages runs LLM consolidation on the given chunk and writes to MEMORY.md + HISTORY.md.
func (c *MemoryConsolidator) ConsolidateMessages(ctx context.Context, chunk []session.Message) bool {
	if len(chunk) == 0 {
		return true
	}
	currentMem, _ := c.store.ReadLongTerm()
	prompt := `Process this conversation and call the save_memory tool with your consolidation.

## Current Long-term Memory
` + currentMem + `

## Conversation to Process
` + formatMessagesForConsolidation(chunk)

	chatMsgs := []providers.Message{
		{Role: "system", Content: "You are a memory consolidation agent. Call the save_memory tool with your consolidation of the conversation."},
		{Role: "user", Content: prompt},
	}

	toolChoice := map[string]any{
		"type":     "function",
		"function": map[string]any{"name": "save_memory"},
	}

	resp, err := c.provider.Chat(ctx, &providers.ChatRequest{
		Messages:    chatMsgs,
		Tools:       []providers.ToolDef{saveMemoryTool},
		ToolChoice:  toolChoice,
		Model:       c.model,
		MaxTokens:   4096,
		Temperature: 0.1,
	})
	if err != nil {
		slog.Warn("memory consolidation Chat failed", "error", err)
		return c.failOrRawArchive(chunk)
	}
	if !resp.HasToolCalls() || len(resp.ToolCalls) == 0 {
		slog.Warn("memory consolidation: LLM did not call save_memory")
		return c.failOrRawArchive(chunk)
	}

	args := resp.ToolCalls[0].Arguments
	if args == nil {
		return c.failOrRawArchive(chunk)
	}

	entry, _ := args["history_entry"].(string)
	update, _ := args["memory_update"].(string)
	if entry == "" || update == "" {
		slog.Warn("memory consolidation: save_memory missing required fields")
		return c.failOrRawArchive(chunk)
	}

	_ = c.store.AppendHistory(entry)
	if update != currentMem {
		_ = c.store.WriteLongTerm(update)
	}
	c.consecFail = 0
	slog.Info("memory consolidation done", "messages", len(chunk))
	return true
}

func (c *MemoryConsolidator) failOrRawArchive(chunk []session.Message) bool {
	c.consecFail++
	if c.consecFail < maxFailuresBeforeRawArchive {
		return false
	}
	c.rawArchive(chunk)
	c.consecFail = 0
	return true
}

func (c *MemoryConsolidator) rawArchive(chunk []session.Message) {
	ts := time.Now().Format("2006-01-02 15:04")
	entry := "[" + ts + "] [RAW] " + itoa(len(chunk)) + " messages\n" + formatMessagesForConsolidation(chunk)
	_ = c.store.AppendHistory(entry)
	slog.Warn("memory consolidation degraded: raw-archived", "count", len(chunk))
}

func itoa(n int) string {
	if n <= 0 {
		return "0"
	}
	var b [20]byte
	i := len(b) - 1
	for n > 0 {
		b[i] = byte('0' + n%10)
		n /= 10
		i--
	}
	return string(b[i+1:])
}

// MaybeConsolidateByTokens archives old messages until prompt fits within half the context window.
func (c *MemoryConsolidator) MaybeConsolidateByTokens(ctx context.Context, sess *session.Session, channel, chatID, sessionKey string) {
	if c.contextTok <= 0 {
		return
	}
	all := sess.GetMessagesFrom(0, 999999)
	if len(all) == 0 {
		return
	}

	lock := c.getLock(sessionKey)
	lock.Lock()
	defer lock.Unlock()

	target := c.contextTok / 2
	estimated := c.EstimateSessionPromptTokens(ctx, sess, channel, chatID, sessionKey)
	if estimated <= 0 {
		return
	}
	ratio := float64(estimated) / float64(c.contextTok)
	if c.logPressure && c.contextTok > 0 && ratio >= 0.85 && estimated < c.contextTok {
		pct := int(math.Min(100, math.Round(ratio*100)))
		slog.Info("context compaction pressure", "session", sessionKey, "estimated_tokens", estimated, "context_window", c.contextTok, "pct_of_window", pct, "note", "below hard consolidate threshold")
	}
	if estimated < c.contextTok {
		return
	}
	if c.logPressure {
		slog.Info("context compaction trigger", "session", sessionKey, "estimated_tokens", estimated, "context_window", c.contextTok, "target_after", target)
	}

	for round := 0; round < maxConsolidationRounds; round++ {
		if estimated <= target {
			return
		}
		toRemove := estimated - target
		if toRemove < 1 {
			toRemove = 1
		}
		endIdx, _, ok := c.PickConsolidationBoundary(sess, toRemove)
		if !ok {
			return
		}
		start := sess.LastConsolidatedIndex()
		if start >= endIdx {
			return
		}
		chunk := sess.GetMessagesFrom(start, endIdx)
		if len(chunk) == 0 {
			return
		}

		if !c.ConsolidateMessages(ctx, chunk) {
			return
		}
		sess.SetLastConsolidated(endIdx)
		_ = c.sessions.Save(sess)

		estimated = c.EstimateSessionPromptTokens(ctx, sess, channel, chatID, sessionKey)
		if estimated <= 0 {
			return
		}
	}
}
