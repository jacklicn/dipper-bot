package lcm

import (
	"database/sql"
	"fmt"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"

	_ "modernc.org/sqlite"
)

// Store is the LCM SQLite-backed store.
type Store struct {
	db   *sql.DB
	cfg  Config
	mu   sync.Mutex
	path string
}

// NewStore creates an LCM store and runs migrations.
func NewStore(cfg Config, workspace string) (*Store, error) {
	path := cfg.DatabasePath
	if path == "" {
		path = filepath.Join(workspace, "lcm.db")
	}
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, fmt.Errorf("open lcm db: %w", err)
	}
	if _, err := db.Exec("PRAGMA foreign_keys = ON"); err != nil {
		db.Close()
		return nil, fmt.Errorf("enable foreign keys: %w", err)
	}
	if err := migrate(db); err != nil {
		db.Close()
		return nil, fmt.Errorf("migrate lcm db: %w", err)
	}
	return &Store{db: db, cfg: cfg, path: path}, nil
}

// Close closes the database.
func (s *Store) Close() error {
	return s.db.Close()
}

func migrate(db *sql.DB) error {
	_, err := db.Exec(`
		CREATE TABLE IF NOT EXISTS conversations (
			conversation_id INTEGER PRIMARY KEY AUTOINCREMENT,
			session_id TEXT NOT NULL UNIQUE,
			title TEXT,
			created_at TEXT NOT NULL DEFAULT (datetime('now')),
			updated_at TEXT NOT NULL DEFAULT (datetime('now'))
		);

		CREATE TABLE IF NOT EXISTS messages (
			message_id INTEGER PRIMARY KEY AUTOINCREMENT,
			conversation_id INTEGER NOT NULL REFERENCES conversations(conversation_id) ON DELETE CASCADE,
			seq INTEGER NOT NULL,
			role TEXT NOT NULL CHECK (role IN ('system', 'user', 'assistant', 'tool')),
			content TEXT NOT NULL,
			token_count INTEGER NOT NULL,
			created_at TEXT NOT NULL DEFAULT (datetime('now')),
			UNIQUE (conversation_id, seq)
		);

		CREATE TABLE IF NOT EXISTS summaries (
			summary_id TEXT PRIMARY KEY,
			conversation_id INTEGER NOT NULL REFERENCES conversations(conversation_id) ON DELETE CASCADE,
			kind TEXT NOT NULL CHECK (kind IN ('leaf', 'condensed')),
			depth INTEGER NOT NULL DEFAULT 0,
			content TEXT NOT NULL,
			token_count INTEGER NOT NULL,
			earliest_at TEXT,
			latest_at TEXT,
			descendant_count INTEGER NOT NULL DEFAULT 0,
			created_at TEXT NOT NULL DEFAULT (datetime('now'))
		);

		CREATE TABLE IF NOT EXISTS summary_messages (
			summary_id TEXT NOT NULL REFERENCES summaries(summary_id) ON DELETE CASCADE,
			message_id INTEGER NOT NULL REFERENCES messages(message_id) ON DELETE RESTRICT,
			ordinal INTEGER NOT NULL,
			PRIMARY KEY (summary_id, message_id)
		);

		CREATE TABLE IF NOT EXISTS summary_parents (
			summary_id TEXT NOT NULL REFERENCES summaries(summary_id) ON DELETE CASCADE,
			parent_summary_id TEXT NOT NULL REFERENCES summaries(summary_id) ON DELETE RESTRICT,
			ordinal INTEGER NOT NULL,
			PRIMARY KEY (summary_id, parent_summary_id)
		);

		CREATE TABLE IF NOT EXISTS context_items (
			conversation_id INTEGER NOT NULL REFERENCES conversations(conversation_id) ON DELETE CASCADE,
			ordinal INTEGER NOT NULL,
			item_type TEXT NOT NULL CHECK (item_type IN ('message', 'summary')),
			message_id INTEGER REFERENCES messages(message_id) ON DELETE RESTRICT,
			summary_id TEXT REFERENCES summaries(summary_id) ON DELETE RESTRICT,
			created_at TEXT NOT NULL DEFAULT (datetime('now')),
			PRIMARY KEY (conversation_id, ordinal),
			CHECK (
				(item_type = 'message' AND message_id IS NOT NULL AND summary_id IS NULL) OR
				(item_type = 'summary' AND summary_id IS NOT NULL AND message_id IS NULL)
			)
		);

		CREATE INDEX IF NOT EXISTS messages_conv_seq_idx ON messages (conversation_id, seq);
		CREATE INDEX IF NOT EXISTS summaries_conv_created_idx ON summaries (conversation_id, created_at);
		CREATE INDEX IF NOT EXISTS context_items_conv_idx ON context_items (conversation_id, ordinal);
	`)
	return err
}

// DeleteConversation removes a conversation and all its data (for /new).
func (s *Store) DeleteConversation(sessionID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	_, err := s.db.Exec("DELETE FROM conversations WHERE session_id = ?", sessionID)
	return err
}

// GetMaxSeq returns the max seq for a conversation.
func (s *Store) GetMaxSeq(conversationID int64) (int, error) {
	var maxSeq int
	err := s.db.QueryRow("SELECT COALESCE(MAX(seq), 0) FROM messages WHERE conversation_id = ?", conversationID).Scan(&maxSeq)
	return maxSeq, err
}

// GetOrCreateConversation returns conversation ID for sessionKey.
func (s *Store) GetOrCreateConversation(sessionID string) (int64, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	var id int64
	err := s.db.QueryRow("SELECT conversation_id FROM conversations WHERE session_id = ?", sessionID).Scan(&id)
	if err == nil {
		return id, nil
	}
	if err != sql.ErrNoRows {
		return 0, err
	}
	res, err := s.db.Exec(
		"INSERT INTO conversations (session_id, created_at, updated_at) VALUES (?, datetime('now'), datetime('now'))",
		sessionID,
	)
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

// IngestMessages persists messages and appends to context_items.
func (s *Store) IngestMessages(conversationID int64, msgs []MessageRow) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	for _, m := range msgs {
		res, err := tx.Exec(
			`INSERT INTO messages (conversation_id, seq, role, content, token_count, created_at)
			 VALUES (?, ?, ?, ?, ?, ?)`,
			conversationID, m.Seq, m.Role, m.Content, m.TokenCount, m.CreatedAt.Format(time.RFC3339),
		)
		if err != nil {
			return err
		}
		msgID, _ := res.LastInsertId()
		var maxOrd int
		_ = tx.QueryRow("SELECT COALESCE(MAX(ordinal), -1) FROM context_items WHERE conversation_id = ?", conversationID).Scan(&maxOrd)
		_, err = tx.Exec(
			`INSERT INTO context_items (conversation_id, ordinal, item_type, message_id, summary_id, created_at)
			 VALUES (?, ?, 'message', ?, NULL, datetime('now'))`,
			conversationID, maxOrd+1, msgID,
		)
		if err != nil {
			return err
		}
	}
	return tx.Commit()
}

// GetContextItems returns ordered context items for assembly.
func (s *Store) GetContextItems(conversationID int64) ([]ContextItem, error) {
	rows, err := s.db.Query(`
		SELECT ci.ordinal, ci.item_type, ci.message_id, ci.summary_id,
		       m.role, m.content,
		       s.content as summary_content, s.kind, s.depth, s.descendant_count, s.earliest_at, s.latest_at
		FROM context_items ci
		LEFT JOIN messages m ON ci.message_id = m.message_id
		LEFT JOIN summaries s ON ci.summary_id = s.summary_id
		WHERE ci.conversation_id = ?
		ORDER BY ci.ordinal`,
		conversationID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var items []ContextItem
	for rows.Next() {
		var item ContextItem
		var msgID sql.NullInt64
		var summaryID sql.NullString
		var mRole, mContent, sContent, sKind sql.NullString
		var sDepth, sDescCount sql.NullInt64
		var sEarliest, sLatest sql.NullString
		if err := rows.Scan(&item.Ordinal, &item.ItemType, &msgID, &summaryID,
			&mRole, &mContent, &sContent, &sKind, &sDepth, &sDescCount, &sEarliest, &sLatest); err != nil {
			return nil, err
		}
		if msgID.Valid {
			item.MessageID = msgID.Int64
		}
		if summaryID.Valid {
			item.SummaryID = summaryID.String
		}
		if item.ItemType == "message" {
			item.Role = mRole.String
			item.Content = mContent.String
		} else {
			item.Role = "user"
			item.Content = sContent.String
			item.SummaryKind = sKind.String
			item.SummaryDepth = int(sDepth.Int64)
			item.SummaryDescCount = int(sDescCount.Int64)
			if sEarliest.Valid {
				item.SummaryEarliest, _ = time.Parse(time.RFC3339, sEarliest.String)
			}
			if sLatest.Valid {
				item.SummaryLatest, _ = time.Parse(time.RFC3339, sLatest.String)
			}
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

// GetContextItemDetails returns item_type, message_id, summary_id for each ordinal (for compaction).
func (s *Store) GetContextItemDetails(conversationID int64) ([]struct {
	Ordinal   int
	ItemType  string
	MessageID sql.NullInt64
	SummaryID sql.NullString
}, error) {
	rows, err := s.db.Query(`
		SELECT ordinal, item_type, message_id, summary_id FROM context_items
		WHERE conversation_id = ? ORDER BY ordinal`, conversationID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []struct {
		Ordinal   int
		ItemType  string
		MessageID sql.NullInt64
		SummaryID sql.NullString
	}
	for rows.Next() {
		var r struct {
			Ordinal   int
			ItemType  string
			MessageID sql.NullInt64
			SummaryID sql.NullString
		}
		if err := rows.Scan(&r.Ordinal, &r.ItemType, &r.MessageID, &r.SummaryID); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// GetTotalTokenCount returns sum of token_count for all context items.
func (s *Store) GetTotalTokenCount(conversationID int64) (int, error) {
	var total int
	err := s.db.QueryRow(`
		SELECT COALESCE(SUM(CASE WHEN ci.item_type = 'message' THEN m.token_count ELSE s.token_count END), 0)
		FROM context_items ci
		LEFT JOIN messages m ON ci.message_id = m.message_id
		LEFT JOIN summaries s ON ci.summary_id = s.summary_id
		WHERE ci.conversation_id = ?`, conversationID).Scan(&total)
	return total, err
}

// SearchContent finds messages and summaries matching the regex.
func (s *Store) SearchContent(conversationID int64, re *regexp.Regexp, limit int) ([]map[string]string, error) {
	items, err := s.GetContextItems(conversationID)
	if err != nil {
		return nil, err
	}
	var out []map[string]string
	for _, it := range items {
		if re.FindStringIndex(it.Content) == nil {
			continue
		}
		out = append(out, map[string]string{
			"type":    it.ItemType,
			"role":    it.Role,
			"content": it.Content,
			"id":      it.SummaryID,
		})
		if len(out) >= limit {
			break
		}
	}
	return out, nil
}

// DescribeConversation returns a brief overview from summaries and recent messages.
func (s *Store) DescribeConversation(conversationID int64) (string, error) {
	items, err := s.GetContextItems(conversationID)
	if err != nil {
		return "", err
	}
	var parts []string
	n := len(items)
	take := 20
	if n > take {
		parts = append(parts, "Earlier summaries (condensed):")
		for i := 0; i < n-take && i < 5; i++ {
			if items[i].ItemType == "summary" {
				c := items[i].Content
				if len(c) > 200 {
					c = c[:200] + "..."
				}
				parts = append(parts, "- "+c)
			}
		}
		parts = append(parts, "\nRecent messages:")
	}
	for i := max(0, n-take); i < n; i++ {
		it := items[i]
		preview := it.Content
		if len(preview) > 300 {
			preview = preview[:300] + "..."
		}
		parts = append(parts, "["+it.Role+"] "+preview)
	}
	return strings.Join(parts, "\n"), nil
}

// GetMessagesByIDs returns messages for given IDs.
func (s *Store) GetMessagesByIDs(ids []int64) ([]MessageRow, error) {
	if len(ids) == 0 {
		return nil, nil
	}
	placeholders := "?"
	for i := 1; i < len(ids); i++ {
		placeholders += ",?"
	}
	args := make([]any, len(ids))
	for i, id := range ids {
		args[i] = id
	}
	rows, err := s.db.Query(
		fmt.Sprintf(`SELECT message_id, conversation_id, seq, role, content, token_count, created_at
			FROM messages WHERE message_id IN (%s) ORDER BY seq`, placeholders),
		args...,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []MessageRow
	for rows.Next() {
		var m MessageRow
		var createdAt string
		if err := rows.Scan(&m.MessageID, &m.ConversationID, &m.Seq, &m.Role, &m.Content, &m.TokenCount, &createdAt); err != nil {
			return nil, err
		}
		m.CreatedAt, _ = time.Parse(time.RFC3339, createdAt)
		out = append(out, m)
	}
	return out, rows.Err()
}

// ReplaceContextRange replaces a range of context items with a summary.
func (s *Store) ReplaceContextRange(conversationID int64, startOrd, endOrd int, summary *Summary) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	_, err = tx.Exec(
		`INSERT INTO summaries (summary_id, conversation_id, kind, depth, content, token_count, earliest_at, latest_at, descendant_count, created_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, datetime('now'))`,
		summary.SummaryID, summary.ConversationID, summary.Kind, summary.Depth,
		summary.Content, summary.TokenCount,
		summary.EarliestAt.Format(time.RFC3339), summary.LatestAt.Format(time.RFC3339),
		summary.DescendantCount,
	)
	if err != nil {
		return err
	}

	if summary.Kind == "leaf" {
		for i, msgID := range summary.SourceMessageIDs {
			_, err = tx.Exec(
				"INSERT INTO summary_messages (summary_id, message_id, ordinal) VALUES (?, ?, ?)",
				summary.SummaryID, msgID, i,
			)
			if err != nil {
				return err
			}
		}
	} else {
		for i, pid := range summary.ParentSummaryIDs {
			_, err = tx.Exec(
				"INSERT INTO summary_parents (summary_id, parent_summary_id, ordinal) VALUES (?, ?, ?)",
				summary.SummaryID, pid, i,
			)
			if err != nil {
				return err
			}
		}
	}

	// Load all items, replace range in memory, re-insert (ordinal is PK so we rebuild)
	rows, err := tx.Query(`
		SELECT ordinal, item_type, message_id, summary_id FROM context_items
		WHERE conversation_id = ? ORDER BY ordinal`, conversationID)
	if err != nil {
		return err
	}
	type row struct {
		ordinal   int
		itemType  string
		messageID sql.NullInt64
		summaryID sql.NullString
	}
	var all []row
	for rows.Next() {
		var r row
		if err := rows.Scan(&r.ordinal, &r.itemType, &r.messageID, &r.summaryID); err != nil {
			rows.Close()
			return err
		}
		all = append(all, r)
	}
	rows.Close()
	if err != nil {
		return err
	}

	_, err = tx.Exec("DELETE FROM context_items WHERE conversation_id = ?", conversationID)
	if err != nil {
		return err
	}

	newOrd := 0
	for _, r := range all {
		if r.ordinal >= startOrd && r.ordinal < endOrd {
			if r.ordinal == startOrd {
				_, err = tx.Exec(
					`INSERT INTO context_items (conversation_id, ordinal, item_type, message_id, summary_id, created_at)
					 VALUES (?, ?, 'summary', NULL, ?, datetime('now'))`,
					conversationID, newOrd, summary.SummaryID,
				)
				if err != nil {
					return err
				}
				newOrd++
			}
			continue
		}
		var msgID, summaryID any
		if r.messageID.Valid {
			msgID, summaryID = r.messageID.Int64, nil
		} else {
			msgID, summaryID = nil, r.summaryID.String
		}
		_, err = tx.Exec(
			`INSERT INTO context_items (conversation_id, ordinal, item_type, message_id, summary_id, created_at)
			 VALUES (?, ?, ?, ?, ?, datetime('now'))`,
			conversationID, newOrd, r.itemType, msgID, summaryID,
		)
		if err != nil {
			return err
		}
		newOrd++
	}
	return tx.Commit()
}
