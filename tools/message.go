package tools

import (
	"context"

	"github.com/jacklicn/dipper-bot/bus"
)

// MessageTool sends messages via the bus to a channel.
type MessageTool struct {
	SendFn  func(ctx context.Context, msg *bus.OutboundMessage) error
	Channel string
	ChatID  string
}

// NewMessageTool creates a message tool.
func NewMessageTool(sendFn func(context.Context, *bus.OutboundMessage) error) *MessageTool {
	return &MessageTool{SendFn: sendFn}
}

// SetContext sets the current channel/chat for sending.
func (m *MessageTool) SetContext(channel, chatID string) {
	m.Channel = channel
	m.ChatID = chatID
}

func (m *MessageTool) Name() string { return "message" }

func (m *MessageTool) Description() string {
	return "Send a message to the user on a chat channel (e.g. Telegram, WhatsApp). Use when you need to deliver a message to the user on that channel."
}

func (m *MessageTool) Parameters() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"content": map[string]any{"type": "string", "description": "Message content to send"},
			"channel": map[string]any{"type": "string", "description": "Target channel (optional, uses current if not set)"},
			"chat_id": map[string]any{"type": "string", "description": "Target chat ID (optional)"},
		},
		"required": []any{"content"},
	}
}

func (m *MessageTool) Execute(ctx context.Context, params map[string]any) (string, error) {
	content := CoerceStringParam(params["content"])
	channel := CoerceStringParam(params["channel"])
	chatID := CoerceStringParam(params["chat_id"])
	if channel == "" {
		channel = m.Channel
	}
	if chatID == "" {
		chatID = m.ChatID
	}
	if channel == "" || chatID == "" {
		return "Error: no channel/chat_id set", nil
	}
	err := m.SendFn(ctx, &bus.OutboundMessage{Channel: channel, ChatID: chatID, Content: content})
	if err != nil {
		return "Error: " + err.Error(), nil
	}
	return "Message sent.", nil
}
