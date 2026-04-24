package session

import (
	"bufio"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/jacklicn/dipper-bot/utils"
)

// Message is a single chat message in a session.
type Message struct {
	Role      string    `json:"role"`
	Content   string    `json:"content"`
	Timestamp time.Time `json:"timestamp"`
	ToolsUsed []string  `json:"tools_used,omitempty"`
}

// Session holds conversation state.
type Session struct {
	Key              string
	Messages         []Message
	CreatedAt        time.Time
	UpdatedAt        time.Time
	LastConsolidated int
	mu               sync.RWMutex
}

// AddMessage appends a message.
func (s *Session) AddMessage(role, content string, toolsUsed []string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.Messages = append(s.Messages, Message{
		Role:      role,
		Content:   content,
		Timestamp: time.Now(),
		ToolsUsed: toolsUsed,
	})
	s.UpdatedAt = time.Now()
}

// GetHistory returns recent messages in LLM format (role + content), up to maxMessages.
// When fromConsolidated is true, returns only messages from LastConsolidated onward (for token-based memory).
func (s *Session) GetHistory(maxMessages int, fromConsolidated bool) []map[string]string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	start := 0
	if fromConsolidated && s.LastConsolidated > 0 {
		start = s.LastConsolidated
	}
	unconsolidated := s.Messages[start:]
	n := len(unconsolidated)
	if maxMessages > 0 && n > maxMessages {
		unconsolidated = unconsolidated[n-maxMessages:]
	}
	out := make([]map[string]string, 0, len(unconsolidated))
	for _, m := range unconsolidated {
		out = append(out, map[string]string{"role": m.Role, "content": m.Content})
	}
	return out
}

// Clear clears all messages.
func (s *Session) Clear() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.Messages = nil
	s.LastConsolidated = 0
	s.UpdatedAt = time.Now()
}

// LastConsolidatedIndex returns the index of the last consolidated message.
func (s *Session) LastConsolidatedIndex() int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.LastConsolidated
}

// SetLastConsolidated sets the last consolidated index (caller must hold consolidation lock).
func (s *Session) SetLastConsolidated(n int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.LastConsolidated = n
	s.UpdatedAt = time.Now()
}

// UserTurnsSinceMemoryTools counts completed user turns after the last assistant message
// that recorded the memory tool or save_memory in ToolsUsed (curated USER/NOTE vs internal consolidator).
func (s *Session) UserTurnsSinceMemoryTools() int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	last := -1
	for i := len(s.Messages) - 1; i >= 0; i-- {
		if s.Messages[i].Role != "assistant" {
			continue
		}
		for _, t := range s.Messages[i].ToolsUsed {
			if t == "memory" || t == "save_memory" {
				last = i
				break
			}
		}
		if last >= 0 {
			break
		}
	}
	n := 0
	if last < 0 {
		for i := 0; i < len(s.Messages); i++ {
			if s.Messages[i].Role == "user" {
				n++
			}
		}
		return n
	}
	for i := last + 1; i < len(s.Messages); i++ {
		if s.Messages[i].Role == "user" {
			n++
		}
	}
	return n
}

// GetMessagesFrom returns a copy of messages from index start to end (for memory consolidation).
func (s *Session) GetMessagesFrom(start, end int) []Message {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if start < 0 {
		start = 0
	}
	if end > len(s.Messages) {
		end = len(s.Messages)
	}
	if start >= end {
		return nil
	}
	out := make([]Message, end-start)
	copy(out, s.Messages[start:end])
	return out
}

// SessionManager manages persistent sessions (JSONL on disk).
type SessionManager struct {
	workspace   string
	sessionsDir string
	cache       map[string]*Session
	mu          sync.RWMutex
	fts         *FTSIndexer // optional cross-session FTS index
}

// NewSessionManager creates a session manager. Sessions are stored in workspace/sessions.
func NewSessionManager(workspace string) (*SessionManager, error) {
	expanded, err := expandWorkspace(workspace)
	if err != nil {
		return nil, err
	}
	dir := filepath.Join(expanded, "sessions")
	if err := os.MkdirAll(dir, 0o750); err != nil {
		return nil, err
	}
	return &SessionManager{workspace: workspace, sessionsDir: dir, cache: make(map[string]*Session)}, nil
}

// SetFTSIndexer attaches an FTS5 indexer updated on each Save (optional).
func (m *SessionManager) SetFTSIndexer(ft *FTSIndexer) {
	m.mu.Lock()
	m.fts = ft
	m.mu.Unlock()
}

func expandWorkspace(w string) (string, error) {
	if w == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		return filepath.Join(home, ".dipper-bot", "workspace"), nil
	}
	if len(w) >= 1 && w[0] == '~' {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		if w == "~" {
			return home, nil
		}
		if len(w) >= 2 && w[1] == '/' {
			return filepath.Join(home, w[2:]), nil
		}
	}
	return w, nil
}

func (m *SessionManager) sessionPath(key string) string {
	safe := utils.SafeFilename(key)
	safe = replaceColon(safe)
	return filepath.Join(m.sessionsDir, safe+".jsonl")
}

func replaceColon(s string) string {
	b := []byte(s)
	for i := range b {
		if b[i] == ':' {
			b[i] = '_'
		}
	}
	return string(b)
}

// GetOrCreate returns an existing session or creates a new one.
func (m *SessionManager) GetOrCreate(key string) (*Session, error) {
	m.mu.Lock()
	if s, ok := m.cache[key]; ok {
		m.mu.Unlock()
		return s, nil
	}
	m.mu.Unlock()

	s, err := m.load(key)
	if err != nil || s == nil {
		s = &Session{Key: key, CreatedAt: time.Now(), UpdatedAt: time.Now()}
	}

	m.mu.Lock()
	m.cache[key] = s
	m.mu.Unlock()
	return s, nil
}

func (m *SessionManager) load(key string) (*Session, error) {
	path := m.sessionPath(key)
	f, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	defer f.Close()

	s := &Session{Key: key}
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := sc.Bytes()
		if len(line) == 0 {
			continue
		}
		var raw map[string]any
		if err := json.Unmarshal(line, &raw); err != nil {
			continue
		}
		if raw["_type"] == "metadata" {
			if v, ok := raw["key"].(string); ok {
				s.Key = v
			}
			if v, ok := raw["created_at"].(string); ok {
				s.CreatedAt, _ = time.Parse(time.RFC3339, v)
			}
			if v, ok := raw["updated_at"].(string); ok {
				s.UpdatedAt, _ = time.Parse(time.RFC3339, v)
			}
			if v, ok := raw["last_consolidated"].(float64); ok {
				s.LastConsolidated = int(v)
			}
			continue
		}
		role, _ := raw["role"].(string)
		content, _ := raw["content"].(string)
		var ts time.Time
		if v, ok := raw["timestamp"].(string); ok {
			ts, _ = time.Parse(time.RFC3339, v)
		}
		var tools []string
		if t, ok := raw["tools_used"].([]interface{}); ok {
			for _, x := range t {
				if str, ok := x.(string); ok {
					tools = append(tools, str)
				}
			}
		}
		s.Messages = append(s.Messages, Message{Role: role, Content: content, Timestamp: ts, ToolsUsed: tools})
	}
	return s, sc.Err()
}

// Save persists a session to disk.
func (m *SessionManager) Save(s *Session) error {
	path := m.sessionPath(s.Key)
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o750); err != nil {
		return err
	}
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()

	meta := map[string]any{
		"_type":             "metadata",
		"key":               s.Key,
		"created_at":        s.CreatedAt.Format(time.RFC3339),
		"updated_at":        s.UpdatedAt.Format(time.RFC3339),
		"last_consolidated": s.LastConsolidated,
	}
	enc := json.NewEncoder(f)
	if err := enc.Encode(meta); err != nil {
		return err
	}
	s.mu.RLock()
	for _, msg := range s.Messages {
		_ = enc.Encode(map[string]any{
			"role": msg.Role, "content": msg.Content,
			"timestamp":  msg.Timestamp.Format(time.RFC3339),
			"tools_used": msg.ToolsUsed,
		})
	}
	s.mu.RUnlock()

	m.mu.Lock()
	m.cache[s.Key] = s
	ft := m.fts
	m.mu.Unlock()

	if ft != nil {
		s.mu.RLock()
		msgs := make([]Message, len(s.Messages))
		copy(msgs, s.Messages)
		s.mu.RUnlock()
		_ = ft.ReindexSession(s.Key, msgs)
	}
	return nil
}

// Invalidate removes a session from cache.
func (m *SessionManager) Invalidate(key string) {
	m.mu.Lock()
	delete(m.cache, key)
	m.mu.Unlock()
}

// SessionInfo holds summary info for a session (for sessions_list).
type SessionInfo struct {
	Key       string    `json:"key"`
	Channel   string    `json:"channel"`
	ChatID    string    `json:"chatId"`
	UpdatedAt time.Time `json:"updatedAt"`
	MsgCount  int       `json:"msgCount"`
}

// ListSessions returns all sessions from disk. Keys are parsed from metadata or filename.
func (m *SessionManager) ListSessions(limit int) ([]SessionInfo, error) {
	entries, err := os.ReadDir(m.sessionsDir)
	if err != nil {
		return nil, err
	}
	var out []SessionInfo
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".jsonl") {
			continue
		}
		base := strings.TrimSuffix(e.Name(), ".jsonl")
		path := filepath.Join(m.sessionsDir, e.Name())
		info, err := m.sessionInfoFromFile(path, base)
		if err != nil {
			continue
		}
		out = append(out, info)
		if limit > 0 && len(out) >= limit {
			break
		}
	}
	return out, nil
}

func (m *SessionManager) sessionInfoFromFile(path, fallbackKey string) (SessionInfo, error) {
	f, err := os.Open(path)
	if err != nil {
		return SessionInfo{}, err
	}
	defer f.Close()
	dec := json.NewDecoder(f)
	var raw map[string]any
	if err := dec.Decode(&raw); err != nil {
		return SessionInfo{}, err
	}
	key := fallbackKey
	if raw["_type"] == "metadata" {
		if v, ok := raw["key"].(string); ok && v != "" {
			key = v
		} else {
			key = strings.Replace(fallbackKey, "_", ":", 1)
		}
	} else {
		key = strings.Replace(fallbackKey, "_", ":", 1)
	}
	channel, chatID := parseSessionKey(key)
	updatedAt := time.Time{}
	if v, ok := raw["updated_at"].(string); ok {
		updatedAt, _ = time.Parse(time.RFC3339, v)
	}
	msgCount := 0
	for dec.More() {
		if dec.Decode(&raw) == nil {
			msgCount++
		}
	}
	return SessionInfo{Key: key, Channel: channel, ChatID: chatID, UpdatedAt: updatedAt, MsgCount: msgCount}, nil
}

func parseSessionKey(key string) (channel, chatID string) {
	if i := strings.Index(key, ":"); i >= 0 {
		return key[:i], key[i+1:]
	}
	return key, ""
}
