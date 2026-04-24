package channels

import (
	"context"
	"log/slog"
	"sync"

	"github.com/jacklicn/dipper-bot/bus"
	"github.com/jacklicn/dipper-bot/config"
	"github.com/jacklicn/dipper-bot/providers"
)

// Manager starts channels and dispatches outbound messages to them.
type Manager struct {
	cfg      *config.Config
	bus      *bus.MessageBus
	channels []Channel
	cancel   context.CancelFunc
	wg       sync.WaitGroup
}

// NewManager creates a channel manager.
func NewManager(cfg *config.Config, messageBus *bus.MessageBus) *Manager {
	return &Manager{cfg: cfg, bus: messageBus}
}

// EnabledChannels returns the list of enabled channel names.
func (m *Manager) EnabledChannels() []string {
	var out []string
	if m.cfg.Channels.Telegram.Enabled && m.cfg.Channels.Telegram.Token != "" {
		out = append(out, "telegram")
	}
	if m.cfg.Channels.WhatsApp.Enabled {
		out = append(out, "whatsapp")
	}
	if m.cfg.Channels.Discord.Enabled && m.cfg.Channels.Discord.Token != "" {
		out = append(out, "discord")
	}
	if m.cfg.Channels.Email.Enabled && m.cfg.Channels.Email.ConsentGranted {
		out = append(out, "email")
	}
	if m.cfg.Channels.Feishu.Enabled && m.cfg.Channels.Feishu.AppID != "" && m.cfg.Channels.Feishu.AppSecret != "" {
		out = append(out, "feishu")
	}
	if m.cfg.Channels.DingTalk.Enabled && m.cfg.Channels.DingTalk.ClientID != "" && m.cfg.Channels.DingTalk.ClientSecret != "" {
		out = append(out, "dingtalk")
	}
	if m.cfg.Channels.Slack.Enabled && m.cfg.Channels.Slack.BotToken != "" && m.cfg.Channels.Slack.AppToken != "" {
		out = append(out, "slack")
	}
	if m.cfg.Channels.QQ.Enabled && m.cfg.Channels.QQ.AppID != "" && m.cfg.Channels.QQ.Secret != "" {
		out = append(out, "qq")
	}
	if m.cfg.Channels.Wecom.Enabled && m.cfg.Channels.Wecom.BotID != "" && m.cfg.Channels.Wecom.Secret != "" {
		out = append(out, "wecom")
	}
	if m.cfg.Channels.Webhook.Enabled {
		out = append(out, "webhook")
	}
	return out
}

// Start starts all enabled channels and subscribes to outbound.
func (m *Manager) Start(ctx context.Context) {
	ctx, cancel := context.WithCancel(ctx)
	m.cancel = cancel

	// Telegram
	if m.cfg.Channels.Telegram.Enabled && m.cfg.Channels.Telegram.Token != "" {
		transcriber := providers.NewTranscriptionProviderFromConfig(m.cfg)
		tg := NewTelegramChannel(m.cfg.Channels.Telegram, m.bus, transcriber)
		m.channels = append(m.channels, tg)
		m.bus.SubscribeOutbound("telegram", tg.Send)
		m.wg.Add(1)
		go func() {
			defer m.wg.Done()
			_ = tg.Start(ctx)
		}()
	}

	// WhatsApp
	if m.cfg.Channels.WhatsApp.Enabled {
		wa := NewWhatsAppChannel(m.cfg.Channels.WhatsApp, m.bus)
		m.channels = append(m.channels, wa)
		m.bus.SubscribeOutbound("whatsapp", wa.Send)
		m.wg.Add(1)
		go func() {
			defer m.wg.Done()
			_ = wa.Start(ctx)
		}()
	}

	// Discord
	if m.cfg.Channels.Discord.Enabled && m.cfg.Channels.Discord.Token != "" {
		dc := NewDiscordChannel(m.cfg.Channels.Discord, m.bus)
		m.channels = append(m.channels, dc)
		m.bus.SubscribeOutbound("discord", dc.Send)
		m.wg.Add(1)
		go func() {
			defer m.wg.Done()
			if err := dc.Start(ctx); err != nil {
				slog.Error("Discord start", "error", err)
			}
		}()
	}

	// Email
	if m.cfg.Channels.Email.Enabled && m.cfg.Channels.Email.ConsentGranted {
		em := NewEmailChannel(m.cfg.Channels.Email, m.bus)
		m.channels = append(m.channels, em)
		m.bus.SubscribeOutbound("email", em.Send)
		m.wg.Add(1)
		go func() {
			defer m.wg.Done()
			_ = em.Start(ctx)
		}()
	}

	// Feishu
	if m.cfg.Channels.Feishu.Enabled && m.cfg.Channels.Feishu.AppID != "" && m.cfg.Channels.Feishu.AppSecret != "" {
		fs := NewFeishuChannel(m.cfg.Channels.Feishu, m.bus)
		m.channels = append(m.channels, fs)
		m.bus.SubscribeOutbound("feishu", fs.Send)
		m.wg.Add(1)
		go func() {
			defer m.wg.Done()
			_ = fs.Start(ctx)
		}()
	}

	// DingTalk
	if m.cfg.Channels.DingTalk.Enabled && m.cfg.Channels.DingTalk.ClientID != "" && m.cfg.Channels.DingTalk.ClientSecret != "" {
		dt := NewDingTalkChannel(m.cfg.Channels.DingTalk, m.bus)
		m.channels = append(m.channels, dt)
		m.bus.SubscribeOutbound("dingtalk", dt.Send)
		m.wg.Add(1)
		go func() {
			defer m.wg.Done()
			_ = dt.Start(ctx)
		}()
	}

	// Slack
	if m.cfg.Channels.Slack.Enabled && m.cfg.Channels.Slack.BotToken != "" && m.cfg.Channels.Slack.AppToken != "" {
		sl := NewSlackChannel(m.cfg.Channels.Slack, m.bus)
		m.channels = append(m.channels, sl)
		m.bus.SubscribeOutbound("slack", sl.Send)
		m.wg.Add(1)
		go func() {
			defer m.wg.Done()
			_ = sl.Start(ctx)
		}()
	}

	// QQ
	if m.cfg.Channels.QQ.Enabled && m.cfg.Channels.QQ.AppID != "" && m.cfg.Channels.QQ.Secret != "" {
		qq := NewQQChannel(m.cfg.Channels.QQ, m.bus)
		m.channels = append(m.channels, qq)
		m.bus.SubscribeOutbound("qq", qq.Send)
		m.wg.Add(1)
		go func() {
			defer m.wg.Done()
			_ = qq.Start(ctx)
		}()
	}

	// Wecom (Enterprise WeChat)
	if m.cfg.Channels.Wecom.Enabled && m.cfg.Channels.Wecom.BotID != "" && m.cfg.Channels.Wecom.Secret != "" {
		wc := NewWecomChannel(m.cfg.Channels.Wecom, m.bus)
		m.channels = append(m.channels, wc)
		m.bus.SubscribeOutbound("wecom", wc.Send)
		m.wg.Add(1)
		go func() {
			defer m.wg.Done()
			_ = wc.Start(ctx)
		}()
	}

	// Webhook (plugin-style: HTTP POST to inject messages)
	if m.cfg.Channels.Webhook.Enabled {
		wh := NewWebhookChannel(m.cfg.Channels.Webhook, m.bus)
		m.channels = append(m.channels, wh)
		m.bus.SubscribeOutbound("webhook", wh.Send)
		m.wg.Add(1)
		go func() {
			defer m.wg.Done()
			_ = wh.Start(ctx)
		}()
	}
}

// Stop stops all channels.
func (m *Manager) Stop() {
	if m.cancel != nil {
		m.cancel()
	}
	for _, ch := range m.channels {
		ch.Stop()
	}
	m.wg.Wait()
	m.channels = nil
}
