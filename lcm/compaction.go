package lcm

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"log/slog"
	"strings"
	"time"
)

// SummarizeFunc is called to summarize content via LLM. Returns summary text or error.
type SummarizeFunc func(ctx context.Context, messages []struct{ Role, Content string }, maxTokens int) (string, error)

const leafPrompt = `Summarize the following conversation transcript. Preserve key facts, decisions, and context. Use timestamps when relevant. Keep the summary concise but informative. Output only the summary, no preamble.`

const condensedPrompt = `Condense the following summaries into a single higher-level summary. Preserve the essential narrative, decisions, and outcomes. Be more abstract than the source summaries. Output only the condensed summary, no preamble.`

// CompactionEngine runs leaf and condensed compaction passes.
type CompactionEngine struct {
	store    *Store
	cfg      Config
	summarize SummarizeFunc
}

// NewCompactionEngine creates a compaction engine.
func NewCompactionEngine(store *Store, cfg Config, summarize SummarizeFunc) *CompactionEngine {
	return &CompactionEngine{store: store, cfg: cfg, summarize: summarize}
}

// RunIncremental runs one leaf pass and optionally condensation (best-effort).
func (c *CompactionEngine) RunIncremental(ctx context.Context, conversationID int64) error {
	details, err := c.store.GetContextItemDetails(conversationID)
	if err != nil {
		return err
	}
	freshTail := c.cfg.FreshTailCount
	if freshTail <= 0 {
		freshTail = 32
	}
	n := len(details)
	if n <= freshTail {
		return nil
	}

	// Find oldest contiguous chunk of raw messages outside fresh tail
	evictableEnd := n - freshTail
	var startOrd, endOrd int
	var msgIDs []int64
	var totalTokens int

	for i := 0; i < evictableEnd; i++ {
		if details[i].ItemType != "message" {
			break
		}
		if !details[i].MessageID.Valid {
			continue
		}
		if len(msgIDs) == 0 {
			startOrd = details[i].Ordinal
		}
		msgIDs = append(msgIDs, details[i].MessageID.Int64)
		endOrd = details[i].Ordinal + 1
	}

	if len(msgIDs) < c.cfg.LeafMinFanout {
		return nil
	}

	msgs, err := c.store.GetMessagesByIDs(msgIDs)
	if err != nil || len(msgs) == 0 {
		return err
	}
	for _, m := range msgs {
		totalTokens += m.TokenCount
	}
	// Trim chunk from end if over token limit (keep oldest)
	if totalTokens > c.cfg.LeafChunkTokens {
		for len(msgs) > 1 && totalTokens > c.cfg.LeafChunkTokens {
			totalTokens -= msgs[len(msgs)-1].TokenCount
			msgs = msgs[:len(msgs)-1]
			msgIDs = msgIDs[:len(msgIDs)-1]
		}
		endOrd = details[len(msgIDs)-1].Ordinal + 1
		if len(msgIDs) < c.cfg.LeafMinFanout {
			return nil
		}
	}

	var sb strings.Builder
	for _, m := range msgs {
		sb.WriteString("[" + m.CreatedAt.Format(time.RFC3339) + "] " + m.Role + ": " + m.Content + "\n\n")
	}
	input := sb.String()

	summary, err := c.summarize(ctx, []struct{ Role, Content string }{
		{Role: "user", Content: leafPrompt + "\n\n---\n\n" + input},
	}, c.cfg.LeafTargetTokens)
	if err != nil {
		slog.Warn("lcm leaf summarization failed", "err", err)
		summary = TruncateFallback(input, 512)
	}

	if EstimateTokens(summary) > totalTokens {
		summary = TruncateFallback(input, c.cfg.LeafTargetTokens/2)
	}

	earliest := msgs[0].CreatedAt
	latest := msgs[len(msgs)-1].CreatedAt
	var b [8]byte
	_, _ = rand.Read(b[:])
	summaryID := "sum_" + hex.EncodeToString(b[:])

	s := &Summary{
		SummaryID:        summaryID,
		ConversationID:   conversationID,
		Kind:             "leaf",
		Depth:            0,
		Content:          summary,
		TokenCount:       EstimateTokens(summary),
		EarliestAt:       earliest,
		LatestAt:         latest,
		DescendantCount:  0,
		SourceMessageIDs: msgIDs,
	}

	if err := c.store.ReplaceContextRange(conversationID, startOrd, endOrd, s); err != nil {
		return err
	}
	slog.Info("lcm leaf compaction", "conversation", conversationID, "messages", len(msgIDs))

	if c.cfg.IncrementalMaxDepth != 0 {
		_ = c.runCondensation(ctx, conversationID)
	}
	return nil
}

func (c *CompactionEngine) runCondensation(ctx context.Context, conversationID int64) error {
	items, err := c.store.GetContextItems(conversationID)
	if err != nil || len(items) == 0 {
		return err
	}
	freshTail := c.cfg.FreshTailCount
	if freshTail <= 0 {
		freshTail = 32
	}
	freshStart := len(items) - freshTail
	if freshStart <= 0 {
		return nil
	}
	evictable := items[:freshStart]

	// Find first contiguous segment of leaf summaries
	minFanout := c.cfg.CondensedMinFanout
	if minFanout <= 0 {
		minFanout = 4
	}
	var segment []ContextItem
	for i := 0; i < len(evictable); i++ {
		if evictable[i].ItemType != "summary" || evictable[i].SummaryKind != "leaf" {
			continue
		}
		j := i
		for j < len(evictable) && evictable[j].ItemType == "summary" && evictable[j].SummaryKind == "leaf" {
			j++
		}
		seg := evictable[i:j]
		if len(seg) >= minFanout {
			segment = seg
			break
		}
		i = j
	}
	if len(segment) < minFanout {
		return nil
	}

	// Trim from end if total tokens exceeds limit (keep oldest)
	const maxCondensedInputTokens = 8000
	totalTokens := 0
	trimEnd := len(segment)
	for k := 0; k < len(segment); k++ {
		totalTokens += EstimateTokens(segment[k].Content)
		if totalTokens > maxCondensedInputTokens && k >= minFanout {
			trimEnd = k
			break
		}
	}
	segment = segment[:trimEnd]
	if len(segment) < minFanout {
		return nil
	}

	// Build input for LLM
	var sb strings.Builder
	for _, it := range segment {
		earliest := it.SummaryEarliest.Format(time.RFC3339)
		latest := it.SummaryLatest.Format(time.RFC3339)
		sb.WriteString("[")
		sb.WriteString(earliest)
		sb.WriteString(" - ")
		sb.WriteString(latest)
		sb.WriteString("]\n")
		sb.WriteString(it.Content)
		sb.WriteString("\n\n")
	}
	input := sb.String()

	summary, err := c.summarize(ctx, []struct{ Role, Content string }{
		{Role: "user", Content: condensedPrompt + "\n\n---\n\n" + input},
	}, c.cfg.CondensedTargetTokens)
	if err != nil {
		slog.Warn("lcm condensed summarization failed", "err", err)
		summary = TruncateFallback(input, c.cfg.CondensedTargetTokens/2)
	}

	parentIDs := make([]string, len(segment))
	var earliest, latest time.Time
	descendantTotal := 0
	for i, it := range segment {
		parentIDs[i] = it.SummaryID
		if i == 0 || it.SummaryEarliest.Before(earliest) {
			earliest = it.SummaryEarliest
		}
		if i == 0 || it.SummaryLatest.After(latest) {
			latest = it.SummaryLatest
		}
		descendantTotal += 1 + it.SummaryDescCount
	}

	var b [8]byte
	_, _ = rand.Read(b[:])
	summaryID := "sum_" + hex.EncodeToString(b[:])

	s := &Summary{
		SummaryID:        summaryID,
		ConversationID:   conversationID,
		Kind:             "condensed",
		Depth:            1,
		Content:          summary,
		TokenCount:       EstimateTokens(summary),
		EarliestAt:       earliest,
		LatestAt:         latest,
		DescendantCount:  descendantTotal,
		ParentSummaryIDs: parentIDs,
	}

	startOrd := segment[0].Ordinal
	endOrd := segment[len(segment)-1].Ordinal + 1
	if err := c.store.ReplaceContextRange(conversationID, startOrd, endOrd, s); err != nil {
		return err
	}
	slog.Info("lcm condensed compaction", "conversation", conversationID, "summaries", len(segment))
	return nil
}
