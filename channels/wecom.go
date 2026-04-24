package channels

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/gorilla/websocket"
	"github.com/jacklicn/dipper-bot/bus"
	"github.com/jacklicn/dipper-bot/config"
)

const wecomWSURL = "wss://openws.work.weixin.qq.com"

// wecomMsgInfo holds the last message context for replying (req_id required by WeCom).
type wecomMsgInfo struct {
	ReqID string
}

// WecomChannel connects to WeCom (Enterprise WeChat) AI Bot via WebSocket long connection.
// Protocol: https://developer.work.weixin.qq.com/document/path/101463
type WecomChannel struct {
	cfg         config.WecomConfig
	bus         *bus.MessageBus
	allowed     func(string) bool
	stop        chan struct{}
	mu          sync.Mutex
	conn        *websocket.Conn
	chatFrames  map[string]*wecomMsgInfo // chat_id -> last msg info for reply
	processed   map[string]struct{}       // msgid dedup
	processedN  int
}

// NewWecomChannel creates a WeCom channel.
func NewWecomChannel(cfg config.WecomConfig, messageBus *bus.MessageBus) *WecomChannel {
	return &WecomChannel{
		cfg:        cfg,
		bus:        messageBus,
		allowed:    AllowChecker(cfg.AllowFrom),
		stop:       make(chan struct{}),
		chatFrames: make(map[string]*wecomMsgInfo),
		processed:  make(map[string]struct{}),
	}
}

// Name implements Channel.
func (c *WecomChannel) Name() string { return "wecom" }

// Start implements Channel. WebSocket long connection to WeCom.
func (c *WecomChannel) Start(ctx context.Context) error {
	if c.cfg.BotID == "" || c.cfg.Secret == "" {
		slog.Error("Wecom botId and secret not configured")
		return nil
	}
	go c.run(ctx)
	return nil
}

func (c *WecomChannel) run(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		case <-c.stop:
			return
		default:
		}
		if err := c.connectAndServe(ctx); err != nil {
			slog.Warn("Wecom connect error", "error", err)
		}
		select {
		case <-time.After(3 * time.Second):
		case <-ctx.Done():
			return
		case <-c.stop:
			return
		}
	}
}

func (c *WecomChannel) connectAndServe(ctx context.Context) error {
	dialer := websocket.Dialer{HandshakeTimeout: 15 * time.Second}
	conn, _, err := dialer.DialContext(ctx, wecomWSURL, nil)
	if err != nil {
		return err
	}
	c.mu.Lock()
	c.conn = conn
	c.mu.Unlock()
	defer func() {
		conn.Close()
		c.mu.Lock()
		c.conn = nil
		c.mu.Unlock()
	}()

	// aibot_subscribe
	reqID := uuid.New().String()
	sub := map[string]any{
		"cmd": "aibot_subscribe",
		"headers": map[string]string{"req_id": reqID},
		"body": map[string]string{
			"bot_id": c.cfg.BotID,
			"secret": c.cfg.Secret,
		},
	}
	if err := conn.WriteJSON(sub); err != nil {
		return err
	}

	// Read subscribe response
	_, data, err := conn.ReadMessage()
	if err != nil {
		return err
	}
	var resp struct {
		Errcode int    `json:"errcode"`
		Errmsg  string `json:"errmsg"`
	}
	if err := json.Unmarshal(data, &resp); err != nil {
		return err
	}
	if resp.Errcode != 0 {
		return fmt.Errorf("wecom subscribe failed: errcode=%d errmsg=%s", resp.Errcode, resp.Errmsg)
	}
	slog.Info("Wecom authenticated successfully")

	// Read loop
	for {
		select {
		case <-ctx.Done():
			return nil
		case <-c.stop:
			return nil
		default:
		}
		_, data, err := conn.ReadMessage()
		if err != nil {
			return err
		}
		c.handleFrame(ctx, conn, data)
	}
}

func (c *WecomChannel) handleFrame(ctx context.Context, conn *websocket.Conn, data []byte) {
	var frame struct {
		Cmd     string          `json:"cmd"`
		Headers struct {
			ReqID string `json:"req_id"`
		} `json:"headers"`
		Body json.RawMessage `json:"body"`
	}
	if err := json.Unmarshal(data, &frame); err != nil {
		slog.Debug("Wecom parse frame", "error", err)
		return
	}

	switch frame.Cmd {
	case "aibot_msg_callback":
		c.handleMsgCallback(ctx, frame.Headers.ReqID, frame.Body)
	case "aibot_event_callback":
		c.handleEventCallback(ctx, conn, frame.Headers.ReqID, frame.Body)
	default:
		slog.Debug("Wecom unknown cmd", "cmd", frame.Cmd)
	}
}

func (c *WecomChannel) handleMsgCallback(ctx context.Context, reqID string, body json.RawMessage) {
	var b struct {
		MsgID    string `json:"msgid"`
		ChatID   string `json:"chatid"`
		ChatType string `json:"chattype"`
		From     struct {
			UserID string `json:"userid"`
		} `json:"from"`
		MsgType string `json:"msgtype"`
		Text    struct {
			Content string `json:"content"`
		} `json:"text"`
	}
	if err := json.Unmarshal(body, &b); err != nil {
		return
	}

	// Dedup
	c.mu.Lock()
	if _, ok := c.processed[b.MsgID]; ok {
		c.mu.Unlock()
		return
	}
	c.processed[b.MsgID] = struct{}{}
	c.processedN++
	if c.processedN > 1000 {
		c.processed = make(map[string]struct{})
		c.processedN = 0
	}
	chatID := b.ChatID
	if chatID == "" {
		chatID = b.From.UserID
	}
	c.chatFrames[chatID] = &wecomMsgInfo{ReqID: reqID}
	c.mu.Unlock()

	senderID := b.From.UserID
	if !c.allowed(senderID) {
		return
	}

	content := ""
	switch b.MsgType {
	case "text":
		content = b.Text.Content
	default:
		content = "[" + b.MsgType + "]"
	}
	if content == "" {
		return
	}

	imb := &bus.InboundMessage{
		Channel:   "wecom",
		ChatID:    chatID,
		SenderID:  senderID,
		Content:   content,
		Timestamp: time.Now(),
		Metadata:  map[string]any{"message_id": b.MsgID, "msg_type": b.MsgType, "req_id": reqID},
	}
	_ = c.bus.PublishInbound(ctx, imb)
}

func (c *WecomChannel) handleEventCallback(ctx context.Context, conn *websocket.Conn, reqID string, body json.RawMessage) {
	var b struct {
		MsgType string `json:"msgtype"`
		Event   struct {
			EventType string `json:"eventtype"`
		} `json:"event"`
		From   struct {
			UserID string `json:"userid"`
		} `json:"from"`
		ChatID string `json:"chatid"`
	}
	if err := json.Unmarshal(body, &b); err != nil {
		return
	}
	if b.Event.EventType == "enter_chat" && c.cfg.WelcomeMessage != "" {
		chatID := b.ChatID
		if chatID == "" {
			chatID = b.From.UserID
		}
		c.sendWelcome(conn, reqID, chatID)
	}
	if b.Event.EventType == "disconnected_event" {
		slog.Info("Wecom disconnected_event, connection will close")
	}
}

func (c *WecomChannel) sendWelcome(conn *websocket.Conn, reqID, chatID string) {
	msg := map[string]any{
		"cmd":     "aibot_respond_welcome_msg",
		"headers": map[string]string{"req_id": reqID},
		"body": map[string]any{
			"msgtype": "text",
			"text":    map[string]string{"content": c.cfg.WelcomeMessage},
		},
	}
	if err := conn.WriteJSON(msg); err != nil {
		slog.Warn("Wecom welcome send", "error", err)
	}
}

// Stop implements Channel.
func (c *WecomChannel) Stop() {
	close(c.stop)
	c.mu.Lock()
	if c.conn != nil {
		c.conn.Close()
		c.conn = nil
	}
	c.mu.Unlock()
}

// Send implements Channel. Replies via aibot_respond_msg (stream, finish=true).
func (c *WecomChannel) Send(ctx context.Context, msg *bus.OutboundMessage) error {
	c.mu.Lock()
	conn := c.conn
	info := c.chatFrames[msg.ChatID]
	c.mu.Unlock()
	if conn == nil || info == nil {
		slog.Warn("Wecom Send: no conn or no frame for chat", "chat_id", msg.ChatID)
		return nil
	}
	content := msg.Content
	if content == "" {
		return nil
	}

	streamID := "stream_" + uuid.New().String()[:8]
	req := map[string]any{
		"cmd":     "aibot_respond_msg",
		"headers": map[string]string{"req_id": info.ReqID},
		"body": map[string]any{
			"msgtype": "stream",
			"stream": map[string]any{
				"id":      streamID,
				"finish":  true,
				"content": content,
			},
		},
	}
	if err := conn.WriteJSON(req); err != nil {
		slog.Error("Wecom Send", "error", err)
		return err
	}
	slog.Debug("Wecom message sent", "chat_id", msg.ChatID)
	return nil
}
