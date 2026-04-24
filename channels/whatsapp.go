package channels

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"sync"
	"time"

	"github.com/jacklicn/dipper-bot/bus"
	"github.com/jacklicn/dipper-bot/config"
	"github.com/gorilla/websocket"
)

// WhatsAppChannel connects to the Node.js bridge via WebSocket.
type WhatsAppChannel struct {
	cfg     config.WhatsAppConfig
	bus     *bus.MessageBus
	allowed func(string) bool
	conn    *websocket.Conn
	connMu  sync.Mutex
	stop    chan struct{}
	client  *http.Client
}

// NewWhatsAppChannel creates a WhatsApp channel (bridge client).
func NewWhatsAppChannel(cfg config.WhatsAppConfig, messageBus *bus.MessageBus) *WhatsAppChannel {
	return &WhatsAppChannel{
		cfg:     cfg,
		bus:     messageBus,
		allowed: AllowChecker(cfg.AllowFrom),
		stop:    make(chan struct{}),
		client:  &http.Client{Timeout: 10 * time.Second},
	}
}

// Name implements Channel.
func (c *WhatsAppChannel) Name() string { return "whatsapp" }

// Start implements Channel. Connects to bridge and processes messages.
func (c *WhatsAppChannel) Start(ctx context.Context) error {
	if c.cfg.BridgeURL == "" {
		c.cfg.BridgeURL = "ws://127.0.0.1:3001"
	}
	go c.run(ctx)
	return nil
}

func (c *WhatsAppChannel) run(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		case <-c.stop:
			return
		default:
		}
		dialer := websocket.Dialer{HandshakeTimeout: 10 * time.Second}
		conn, _, err := dialer.DialContext(ctx, c.cfg.BridgeURL, nil)
		if err != nil {
			slog.Warn("WhatsApp bridge connect failed", "error", err, "url", c.cfg.BridgeURL)
			select {
			case <-time.After(5 * time.Second):
				continue
			case <-ctx.Done():
				return
			}
		}
		c.connMu.Lock()
		c.conn = conn
		c.connMu.Unlock()

		// Auth if token set
		if c.cfg.BridgeToken != "" {
			_ = conn.WriteJSON(map[string]string{"type": "auth", "token": c.cfg.BridgeToken})
		}

		slog.Info("Connected to WhatsApp bridge")
		for {
			_, data, err := conn.ReadMessage()
			if err != nil {
				break
			}
			c.handleBridgeMessage(ctx, data)
		}
		conn.Close()
		c.connMu.Lock()
		c.conn = nil
		c.connMu.Unlock()
		slog.Warn("WhatsApp bridge disconnected, reconnecting in 5s...")
		select {
		case <-time.After(5 * time.Second):
		case <-ctx.Done():
			return
		}
	}
}

// Stop implements Channel.
func (c *WhatsAppChannel) Stop() {
	close(c.stop)
	c.connMu.Lock()
	if c.conn != nil {
		c.conn.Close()
		c.conn = nil
	}
	c.connMu.Unlock()
}

func (c *WhatsAppChannel) handleBridgeMessage(ctx context.Context, data []byte) {
	var raw map[string]any
	if err := json.Unmarshal(data, &raw); err != nil {
		return
	}
	msgType, _ := raw["type"].(string)
	switch msgType {
	case "message":
		sender, _ := raw["sender"].(string)
		pn, _ := raw["pn"].(string)
		content, _ := raw["content"].(string)
		if sender == "" && pn != "" {
			sender = pn
		}
		if sender == "" {
			return
		}
		senderID := sender
		for i, r := range sender {
			if r == '@' {
				senderID = sender[:i]
				break
			}
		}
		chatID := sender
		if !c.allowed(senderID) {
			slog.Warn("WhatsApp access denied", "sender", senderID)
			return
		}
		in := &bus.InboundMessage{
			Channel:  "whatsapp",
			SenderID: senderID,
			ChatID:   chatID,
			Content:  content,
			Metadata: map[string]any{
				"message_id": raw["id"],
				"timestamp":  raw["timestamp"],
				"is_group":   raw["isGroup"],
			},
		}
		_ = c.bus.PublishInbound(ctx, in)
	case "status":
		// connected / disconnected
	case "qr":
		slog.Info("Scan QR code in the bridge terminal to connect WhatsApp")
	case "error":
		slog.Error("WhatsApp bridge error", "error", raw["error"])
	}
}

// Send implements Channel.
func (c *WhatsAppChannel) Send(ctx context.Context, msg *bus.OutboundMessage) error {
	c.connMu.Lock()
	conn := c.conn
	c.connMu.Unlock()
	if conn == nil {
		return nil
	}
	payload := map[string]string{"type": "send", "to": msg.ChatID, "text": msg.Content}
	return conn.WriteJSON(payload)
}
