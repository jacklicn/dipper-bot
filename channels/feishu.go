package channels

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"strings"
	"sync"

	"github.com/jacklicn/dipper-bot/bus"
	"github.com/jacklicn/dipper-bot/config"
	lark "github.com/larksuite/oapi-sdk-go/v3"
	larkim "github.com/larksuite/oapi-sdk-go/v3/service/im/v1"
)

// FeishuChannel connects to Feishu/Lark via WebSocket long connection.
type FeishuChannel struct {
	cfg     config.FeishuConfig
	bus     *bus.MessageBus
	allowed func(string) bool
	client  *lark.Client
	stop    chan struct{}
	mu      sync.Mutex
}

// NewFeishuChannel creates a Feishu channel.
func NewFeishuChannel(cfg config.FeishuConfig, messageBus *bus.MessageBus) *FeishuChannel {
	return &FeishuChannel{
		cfg:     cfg,
		bus:     messageBus,
		allowed: AllowChecker(cfg.AllowFrom),
		stop:    make(chan struct{}),
	}
}

// Name implements Channel.
func (c *FeishuChannel) Name() string { return "feishu" }

// Start implements Channel. Uses event subscription - for WebSocket long connection
// the lark-oapi Go SDK requires running the HTTP server. We use a simplified
// approach: start HTTP server for event webhook, or use polling if available.
// The Python version uses lark.ws.Client - Go SDK may use different approach.
func (c *FeishuChannel) Start(ctx context.Context) error {
	if c.cfg.AppID == "" || c.cfg.AppSecret == "" {
		slog.Error("Feishu app_id and app_secret not configured")
		return nil
	}
	c.client = lark.NewClient(c.cfg.AppID, c.cfg.AppSecret)
	slog.Info("Feishu channel started (send-only; configure event subscription for inbound)")
	<-ctx.Done()
	return nil
}

// Stop implements Channel.
func (c *FeishuChannel) Stop() {
	close(c.stop)
	c.mu.Lock()
	c.client = nil
	c.mu.Unlock()
}

// Send implements Channel.
func (c *FeishuChannel) Send(ctx context.Context, msg *bus.OutboundMessage) error {
	c.mu.Lock()
	client := c.client
	c.mu.Unlock()
	if client == nil {
		return nil
	}
	receiveIDType := "open_id"
	if strings.HasPrefix(msg.ChatID, "oc_") {
		receiveIDType = "chat_id"
	}
	content := json.RawMessage(`{"text":"` + escapeJSON(msg.Content) + `"}`)
	req := larkim.NewCreateMessageReqBuilder().
		ReceiveIdType(receiveIDType).
		Body(larkim.NewCreateMessageReqBodyBuilder().
			ReceiveId(msg.ChatID).
			MsgType("text").
			Content(string(content)).
			Build()).
		Build()
	_, err := client.Im.V1.Message.Create(ctx, req)
	return err
}

func escapeJSON(s string) string {
	return strings.ReplaceAll(strings.ReplaceAll(s, "\\", "\\\\"), "\"", "\\\"")
}

// HandleEvent handles Feishu event webhook. Call this from your HTTP server
// when using webhook mode instead of WebSocket.
func (c *FeishuChannel) HandleEvent(w http.ResponseWriter, r *http.Request) {
	// Placeholder for webhook verification and event handling
	w.WriteHeader(http.StatusOK)
}
