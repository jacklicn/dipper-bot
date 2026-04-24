package lcm

import (
	"context"
	"log/slog"
	"regexp"
	"time"

	"github.com/jacklicn/dipper-bot/providers"
)

// Engine is the main LCM entry point: assemble context, ingest, compact.
type Engine struct {
	store    *Store
	cfg      Config
	comp     *CompactionEngine
	provider providers.LLMProvider
	model    string
}

// NewEngine creates an LCM engine.
func NewEngine(cfg Config, workspace string, provider providers.LLMProvider, model string) (*Engine, error) {
	if !cfg.Enabled {
		return nil, nil
	}
	store, err := NewStore(cfg, workspace)
	if err != nil {
		return nil, err
	}
	summarize := func(ctx context.Context, messages []struct{ Role, Content string }, maxTokens int) (string, error) {
		msgs := make([]providers.Message, 0, len(messages))
		for _, m := range messages {
			msgs = append(msgs, providers.Message{Role: m.Role, Content: m.Content})
		}
		resp, err := provider.Chat(ctx, &providers.ChatRequest{
			Model:       model,
			Messages:    msgs,
			MaxTokens:   maxTokens,
			Temperature: 0.2,
		})
		if err != nil {
			return "", err
		}
		return resp.Content, nil
	}
	comp := NewCompactionEngine(store, cfg, summarize)
	return &Engine{store: store, cfg: cfg, comp: comp, provider: provider, model: model}, nil
}

// ClearSession removes all LCM data for a session (used by /new).
func (e *Engine) ClearSession(sessionID string) error {
	if e == nil {
		return nil
	}
	return e.store.DeleteConversation(sessionID)
}

// Search finds messages/summaries matching the regex (for lcm_grep).
func (e *Engine) Search(conversationID int64, re *regexp.Regexp, limit int) ([]map[string]string, error) {
	if e == nil {
		return nil, nil
	}
	return e.store.SearchContent(conversationID, re, limit)
}

// Describe returns a high-level overview of the conversation (for lcm_describe).
func (e *Engine) Describe(conversationID int64) (string, error) {
	if e == nil {
		return "", nil
	}
	return e.store.DescribeConversation(conversationID)
}

// GetConversationID returns the conversation ID for a session.
func (e *Engine) GetConversationID(sessionID string) (int64, error) {
	if e == nil {
		return 0, nil
	}
	return e.store.GetOrCreateConversation(sessionID)
}

// Close closes the store.
func (e *Engine) Close() error {
	if e == nil || e.store == nil {
		return nil
	}
	return e.store.Close()
}

// AssembleContext returns role+content pairs for the model, using LCM assembly.
func (e *Engine) AssembleContext(ctx context.Context, conversationID int64, maxContextTokens int) ([]struct{ Role, Content string }, error) {
	if e == nil {
		return nil, nil
	}
	items, err := e.store.GetContextItems(conversationID)
	if err != nil {
		return nil, err
	}
	assembled := Assemble(items, e.cfg, maxContextTokens)
	return ItemsToRoleContent(assembled), nil
}

// Bootstrap reconciles session messages into LCM (import from JSONL if needed).
func (e *Engine) Bootstrap(sessionID string, history []map[string]string) error {
	if e == nil {
		return nil
	}
	convID, err := e.store.GetOrCreateConversation(sessionID)
	if err != nil {
		return err
	}
	maxSeq, err := e.store.GetMaxSeq(convID)
	if err != nil {
		return err
	}
	if maxSeq >= len(history) {
		return nil
	}
	var toIngest []MessageRow
	for i := maxSeq; i < len(history); i++ {
		h := history[i]
		role, _ := h["role"]
		content, _ := h["content"]
		tok := EstimateTokens(content)
		toIngest = append(toIngest, MessageRow{
			ConversationID: convID,
			Seq:            i + 1,
			Role:           role,
			Content:        content,
			TokenCount:     tok,
			CreatedAt:      time.Now(),
		})
	}
	return e.store.IngestMessages(convID, toIngest)
}

// IngestTurn persists new messages from a turn and runs compaction if needed.
func (e *Engine) IngestTurn(ctx context.Context, sessionID string, newMessages []map[string]string) error {
	if e == nil {
		return nil
	}
	convID, err := e.store.GetOrCreateConversation(sessionID)
	if err != nil {
		return err
	}
	maxSeq, err := e.store.GetMaxSeq(convID)
	if err != nil {
		return err
	}
	var toIngest []MessageRow
	for i, m := range newMessages {
		role, _ := m["role"]
		content, _ := m["content"]
		tok := EstimateTokens(content)
		toIngest = append(toIngest, MessageRow{
			ConversationID: convID,
			Seq:            maxSeq + i + 1,
			Role:           role,
			Content:        content,
			TokenCount:     tok,
			CreatedAt:      time.Now(),
		})
	}
	if len(toIngest) == 0 {
		return nil
	}
	if err := e.store.IngestMessages(convID, toIngest); err != nil {
		return err
	}
	total, _ := e.store.GetTotalTokenCount(convID)
	threshold := int(float64(128000) * e.cfg.ContextThreshold) // default 128k model
	if total > threshold {
		if err := e.comp.RunIncremental(ctx, convID); err != nil {
			slog.Warn("lcm compaction", "err", err)
		}
	}
	return nil
}
