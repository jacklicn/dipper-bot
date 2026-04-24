package session

import (
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"unicode"

	_ "modernc.org/sqlite"
)

// FTSIndexer maintains a SQLite FTS5 index of session messages for cross-session search (session_search tool).
type FTSIndexer struct {
	db   *sql.DB
	path string
	mu   sync.Mutex
}

// NewFTSIndexer opens or creates the FTS database at dbPath (e.g. workspace/memory/sessions_fts.db).
func NewFTSIndexer(dbPath string) (*FTSIndexer, error) {
	if err := os.MkdirAll(filepath.Dir(dbPath), 0o750); err != nil {
		return nil, err
	}
	dsn := "file:" + filepath.ToSlash(dbPath) + "?_pragma=busy_timeout(5000)&_pragma=journal_mode(WAL)"
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, err
	}
	if _, err := db.Exec(`CREATE VIRTUAL TABLE IF NOT EXISTS sessions_fts USING fts5(
		session_key UNINDEXED,
		role UNINDEXED,
		content,
		tokenize = 'porter unicode61'
	);`); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("fts init: %w", err)
	}
	return &FTSIndexer{db: db, path: dbPath}, nil
}

// Close releases the database handle.
func (x *FTSIndexer) Close() error {
	if x == nil || x.db == nil {
		return nil
	}
	return x.db.Close()
}

// ReindexSession replaces all indexed rows for one session key.
func (x *FTSIndexer) ReindexSession(sessionKey string, msgs []Message) error {
	if x == nil || x.db == nil {
		return nil
	}
	x.mu.Lock()
	defer x.mu.Unlock()

	tx, err := x.db.Begin()
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()

	if _, err := tx.Exec(`DELETE FROM sessions_fts WHERE session_key = ?`, sessionKey); err != nil {
		return err
	}
	stmt, err := tx.Prepare(`INSERT INTO sessions_fts(session_key, role, content) VALUES (?, ?, ?)`)
	if err != nil {
		return err
	}
	defer stmt.Close()

	for _, m := range msgs {
		c := strings.TrimSpace(m.Content)
		if c == "" {
			continue
		}
		if _, err := stmt.Exec(sessionKey, m.Role, c); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// FTSHit is one ranked row from the index.
type FTSHit struct {
	SessionKey string
	Role       string
	Snippet    string
	Rank       float64
}

// Search runs an FTS5 MATCH query and returns up to maxRows hits (excluding excludeSession when set).
func (x *FTSIndexer) Search(matchQuery string, excludeSession string, maxRows int) ([]FTSHit, error) {
	if x == nil || x.db == nil {
		return nil, nil
	}
	if maxRows <= 0 {
		maxRows = 50
	}
	matchQuery = strings.TrimSpace(matchQuery)
	if matchQuery == "" {
		return nil, nil
	}

	x.mu.Lock()
	defer x.mu.Unlock()

	// FTS5 MATCH: build OR of safe tokens (broad recall).
	q := ftsMatchQuery(matchQuery)
	if q == "" {
		return nil, nil
	}

	rows, err := x.db.Query(`
		SELECT session_key, role, snippet(sessions_fts, 2, '<b>', '</b>', '…', 32), bm25(sessions_fts)
		FROM sessions_fts
		WHERE sessions_fts MATCH ?
		  AND (? = '' OR session_key != ?)
		ORDER BY bm25(sessions_fts)
		LIMIT ?`, q, excludeSession, excludeSession, maxRows)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []FTSHit
	for rows.Next() {
		var h FTSHit
		if err := rows.Scan(&h.SessionKey, &h.Role, &h.Snippet, &h.Rank); err != nil {
			continue
		}
		out = append(out, h)
	}
	return out, rows.Err()
}

func ftsMatchQuery(raw string) string {
	var parts []string
	for _, w := range strings.Fields(raw) {
		w = strings.Map(func(r rune) rune {
			if unicode.IsLetter(r) || unicode.IsNumber(r) || r == '_' || r == '-' {
				return r
			}
			return -1
		}, w)
		if len(w) < 2 {
			continue
		}
		// Quote each token for MATCH.
		parts = append(parts, `"`+strings.ReplaceAll(w, `"`, "")+`"`)
	}
	if len(parts) == 0 {
		return ""
	}
	return strings.Join(parts, " OR ")
}
