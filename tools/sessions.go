package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/jacklicn/dipper-bot/bus"
	"github.com/jacklicn/dipper-bot/session"
)

// SessionsListTool lists all sessions (OpenClaw-style).
type SessionsListTool struct {
	Sessions *session.SessionManager
	channel  string
	chatID   string
}

// NewSessionsListTool creates a SessionsListTool.
func NewSessionsListTool(sessions *session.SessionManager) *SessionsListTool {
	return &SessionsListTool{Sessions: sessions}
}

// SetContext sets the current session for filtering (optional).
func (t *SessionsListTool) SetContext(channel, chatID string) {
	t.channel = channel
	t.chatID = chatID
}

func (t *SessionsListTool) Name() string        { return "sessions_list" }
func (t *SessionsListTool) Description() string { return "List all chat sessions. Returns session keys (channel:chatId), channel, chatId, updatedAt, msgCount. Use to discover sessions before sessions_history or sessions_send." }

func (t *SessionsListTool) Parameters() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"limit": map[string]any{"type": "integer", "description": "Max sessions to return (default 50, 0 = all)"},
		},
	}
}

func (t *SessionsListTool) Execute(ctx context.Context, params map[string]any) (string, error) {
	if t.Sessions == nil {
		return `{"error":"sessions not configured"}`, nil
	}
	limit := 50
	if n, ok := params["limit"].(float64); ok && n >= 0 {
		limit = int(n)
		if limit == 0 {
			limit = 500
		}
	}
	infos, err := t.Sessions.ListSessions(limit)
	if err != nil {
		return fmt.Sprintf(`{"error":"%s"}`, err.Error()), nil
	}
	b, _ := json.Marshal(infos)
	return string(b), nil
}

// SessionsHistoryTool gets message history for a session.
type SessionsHistoryTool struct {
	Sessions *session.SessionManager
}

// NewSessionsHistoryTool creates a SessionsHistoryTool.
func NewSessionsHistoryTool(sessions *session.SessionManager) *SessionsHistoryTool {
	return &SessionsHistoryTool{Sessions: sessions}
}

func (t *SessionsHistoryTool) Name() string        { return "sessions_history" }
func (t *SessionsHistoryTool) Description() string { return "Get message history for a session. Use sessionKey from sessions_list (e.g. telegram:123, web:default)." }

func (t *SessionsHistoryTool) Parameters() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"sessionKey": map[string]any{"type": "string", "description": "Session key (channel:chatId)"},
			"limit":      map[string]any{"type": "integer", "description": "Max messages to return (default 20)"},
		},
		"required": []any{"sessionKey"},
	}
}

func (t *SessionsHistoryTool) Execute(ctx context.Context, params map[string]any) (string, error) {
	if t.Sessions == nil {
		return `{"error":"sessions not configured"}`, nil
	}
	key, _ := params["sessionKey"].(string)
	if key == "" {
		return `{"error":"sessionKey is required"}`, nil
	}
	limit := 20
	if n, ok := params["limit"].(float64); ok && n > 0 {
		limit = int(n)
	}
	sess, err := t.Sessions.GetOrCreate(key)
	if err != nil {
		return fmt.Sprintf(`{"error":"%s"}`, err.Error()), nil
	}
	history := sess.GetHistory(limit, false)
	out := make([]map[string]string, 0, len(history))
	for _, h := range history {
		out = append(out, h)
	}
	b, _ := json.Marshal(out)
	return string(b), nil
}

// SessionsSendTool sends a message to another session (triggers agent to process it).
type SessionsSendTool struct {
	PublishInbound func(ctx context.Context, msg *bus.InboundMessage) error
	channel        string
	chatID         string
}

// NewSessionsSendTool creates a SessionsSendTool.
func NewSessionsSendTool(publishFn func(ctx context.Context, msg *bus.InboundMessage) error) *SessionsSendTool {
	return &SessionsSendTool{PublishInbound: publishFn}
}

// SetContext sets the origin session (for logging).
func (t *SessionsSendTool) SetContext(channel, chatID string) {
	t.channel = channel
	t.chatID = chatID
}

func (t *SessionsSendTool) Name() string        { return "sessions_send" }
func (t *SessionsSendTool) Description() string { return "Send a message to another session. The agent will process it and reply to that session. Use sessionKey from sessions_list (e.g. telegram:123)." }

func (t *SessionsSendTool) Parameters() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"sessionKey": map[string]any{"type": "string", "description": "Target session key (channel:chatId)"},
			"message":    map[string]any{"type": "string", "description": "Message content to send"},
		},
		"required": []any{"sessionKey", "message"},
	}
}

func (t *SessionsSendTool) Execute(ctx context.Context, params map[string]any) (string, error) {
	if t.PublishInbound == nil {
		return `{"error":"sessions_send not configured"}`, nil
	}
	key, _ := params["sessionKey"].(string)
	message, _ := params["message"].(string)
	if key == "" || message == "" {
		return `{"error":"sessionKey and message are required"}`, nil
	}
	channel, chatID := parseSessionKeyForSend(key)
	if channel == "" || chatID == "" {
		return `{"error":"invalid sessionKey, use channel:chatId format"}`, nil
	}
	msg := &bus.InboundMessage{
		Channel:   channel,
		ChatID:    chatID,
		SenderID:  "sessions_send",
		Content:   message,
		Timestamp: time.Now(),
	}
	if err := t.PublishInbound(ctx, msg); err != nil {
		return fmt.Sprintf(`{"error":"%s"}`, err.Error()), nil
	}
	return fmt.Sprintf(`{"ok":true,"message":"Message sent to %s"}`, key), nil
}

func parseSessionKeyForSend(key string) (channel, chatID string) {
	key = strings.TrimSpace(key)
	if i := strings.Index(key, ":"); i >= 0 {
		return key[:i], key[i+1:]
	}
	return "", ""
}
