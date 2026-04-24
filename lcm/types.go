package lcm

import "time"

// ContextItem represents a single item in the assembled context (message or summary).
type ContextItem struct {
	ItemType         string // "message" or "summary"
	MessageID        int64
	SummaryID        string
	Role             string
	Content          string
	Ordinal          int
	SummaryKind      string    // for summaries: "leaf" or "condensed"
	SummaryDepth     int       // for summaries
	SummaryDescCount int       // for summaries
	SummaryEarliest  time.Time // for summaries
	SummaryLatest    time.Time // for summaries
}

// Summary represents a DAG node (leaf or condensed).
type Summary struct {
	SummaryID        string
	ConversationID   int64
	Kind             string // "leaf" or "condensed"
	Depth            int
	Content          string
	TokenCount       int
	EarliestAt       time.Time
	LatestAt         time.Time
	DescendantCount  int
	CreatedAt        time.Time
	SourceMessageIDs []int64  // for leaf
	ParentSummaryIDs []string // for condensed
}

// MessageRow is a persisted message.
type MessageRow struct {
	MessageID     int64
	ConversationID int64
	Seq           int
	Role          string
	Content       string
	TokenCount    int
	CreatedAt     time.Time
}
