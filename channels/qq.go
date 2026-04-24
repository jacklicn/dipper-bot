package channels

import (
	"context"
	"log/slog"
	"sync"

	"github.com/jacklicn/dipper-bot/bus"
	"github.com/jacklicn/dipper-bot/config"
	"github.com/tencent-connect/botgo"
	"github.com/tencent-connect/botgo/dto"
	"github.com/tencent-connect/botgo/openapi"
	"github.com/tencent-connect/botgo/token"
)

// QQChannel connects to QQ Open Platform via botgo SDK.
type QQChannel struct {
	cfg           config.QQConfig
	bus           *bus.MessageBus
	allowed       func(string) bool
	api           openapi.OpenAPI
	chatTypeCache map[string]string
	msgSeq        uint32
	stop          chan struct{}
	mu            sync.Mutex
}

// NewQQChannel creates a QQ channel.
func NewQQChannel(cfg config.QQConfig, messageBus *bus.MessageBus) *QQChannel {
	return &QQChannel{
		cfg:           cfg,
		bus:           messageBus,
		allowed:       AllowChecker(cfg.AllowFrom),
		chatTypeCache: make(map[string]string),
		stop:          make(chan struct{}),
	}
}

// Name implements Channel.
func (c *QQChannel) Name() string { return "qq" }

// Start implements Channel.
func (c *QQChannel) Start(ctx context.Context) error {
	if c.cfg.AppID == "" || c.cfg.Secret == "" {
		slog.Error("QQ app_id and secret not configured")
		return nil
	}
	credentials := &token.QQBotCredentials{
		AppID:     c.cfg.AppID,
		AppSecret: c.cfg.Secret,
	}
	tokenSource := token.NewQQBotTokenSource(credentials)
	if err := token.StartRefreshAccessToken(ctx, tokenSource); err != nil {
		slog.Error("QQ token refresh failed", "error", err)
		return nil
	}
	c.mu.Lock()
	c.api = botgo.NewOpenAPI(c.cfg.AppID, tokenSource)
	c.mu.Unlock()
	slog.Info("QQ channel started (send-only; configure webhook for inbound)")
	<-ctx.Done()
	return nil
}

// Stop implements Channel.
func (c *QQChannel) Stop() {
	close(c.stop)
	c.mu.Lock()
	c.api = nil
	c.mu.Unlock()
}

// Send implements Channel.
func (c *QQChannel) Send(ctx context.Context, msg *bus.OutboundMessage) error {
	c.mu.Lock()
	api := c.api
	c.msgSeq++
	seq := c.msgSeq
	msgType := c.chatTypeCache[msg.ChatID]
	if msgType == "" {
		msgType = "c2c"
	}
	c.mu.Unlock()
	if api == nil {
		return nil
	}
	apiMsg := &dto.MessageToCreate{
		Content: msg.Content,
		MsgType: dto.TextMsg,
		MsgID:   getMsgID(msg),
		MsgSeq:  seq,
	}
	if msgType == "group" {
		_, err := api.PostGroupMessage(ctx, msg.ChatID, apiMsg)
		return err
	}
	_, err := api.PostC2CMessage(ctx, msg.ChatID, apiMsg)
	return err
}

func getMsgID(msg *bus.OutboundMessage) string {
	if msg.Metadata != nil {
		if id, ok := msg.Metadata["message_id"].(string); ok {
			return id
		}
	}
	return ""
}
