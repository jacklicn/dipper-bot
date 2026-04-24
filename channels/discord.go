package channels

import (
	"context"
	"log/slog"
	"sync"

	"github.com/bwmarrin/discordgo"
	"github.com/jacklicn/dipper-bot/bus"
	"github.com/jacklicn/dipper-bot/config"
)

const discordAPIBase = "https://discord.com/api/v10"

// DiscordChannel connects to Discord Gateway and sends via REST.
type DiscordChannel struct {
	cfg     config.DiscordConfig
	bus     *bus.MessageBus
	allowed func(string) bool
	session *discordgo.Session
	stop    chan struct{}
	mu      sync.Mutex
}

// NewDiscordChannel creates a Discord channel.
func NewDiscordChannel(cfg config.DiscordConfig, messageBus *bus.MessageBus) *DiscordChannel {
	return &DiscordChannel{
		cfg:     cfg,
		bus:     messageBus,
		allowed: AllowChecker(cfg.AllowFrom),
		stop:    make(chan struct{}),
	}
}

// Name implements Channel.
func (c *DiscordChannel) Name() string { return "discord" }

// Start implements Channel.
func (c *DiscordChannel) Start(ctx context.Context) error {
	if c.cfg.Token == "" {
		slog.Error("Discord token not configured")
		return nil
	}
	s, err := discordgo.New("Bot " + c.cfg.Token)
	if err != nil {
		return err
	}
	var intents discordgo.Intent
	if c.cfg.Intents != 0 {
		intents = discordgo.Intent(c.cfg.Intents)
	} else {
		intents = discordgo.IntentsGuildMessages | discordgo.IntentsDirectMessages | discordgo.IntentsMessageContent
	}
	s.Identify.Intents = intents
	s.AddHandler(c.onMessageCreate)

	c.mu.Lock()
	c.session = s
	c.mu.Unlock()

	if err := s.Open(); err != nil {
		return err
	}
	slog.Info("Discord channel started")
	return nil
}

// Stop implements Channel.
func (c *DiscordChannel) Stop() {
	close(c.stop)
	c.mu.Lock()
	if c.session != nil {
		c.session.Close()
		c.session = nil
	}
	c.mu.Unlock()
}

func (c *DiscordChannel) onMessageCreate(s *discordgo.Session, m *discordgo.MessageCreate) {
	if m.Author == nil || m.Author.Bot {
		return
	}
	senderID := m.Author.ID
	channelID := m.ChannelID
	content := m.Content
	if content == "" {
		content = "[empty message]"
	}
	if !c.allowed(senderID) {
		return
	}
	in := &bus.InboundMessage{
		Channel:  "discord",
		SenderID: senderID,
		ChatID:   channelID,
		Content:  content,
		Metadata: map[string]any{
			"message_id": m.ID,
			"guild_id":   m.GuildID,
		},
	}
	ctx := context.Background()
	_ = c.bus.PublishInbound(ctx, in)
}

// Send implements Channel.
func (c *DiscordChannel) Send(ctx context.Context, msg *bus.OutboundMessage) error {
	c.mu.Lock()
	s := c.session
	c.mu.Unlock()
	if s == nil {
		return nil
	}
	_, err := s.ChannelMessageSend(msg.ChatID, msg.Content)
	return err
}
