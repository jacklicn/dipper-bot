package channels

import (
	"context"
	"log/slog"
	"regexp"
	"strings"
	"sync"

	"github.com/jacklicn/dipper-bot/bus"
	"github.com/jacklicn/dipper-bot/config"
	"github.com/slack-go/slack"
	"github.com/slack-go/slack/slackevents"
	"github.com/slack-go/slack/socketmode"
)

// SlackChannel connects to Slack via Socket Mode.
type SlackChannel struct {
	cfg       config.SlackConfig
	bus       *bus.MessageBus
	allowed   func(string) bool
	api       *slack.Client
	socket    *socketmode.Client
	botUserID string
	stop      chan struct{}
	mu        sync.Mutex
}

// NewSlackChannel creates a Slack channel.
func NewSlackChannel(cfg config.SlackConfig, messageBus *bus.MessageBus) *SlackChannel {
	return &SlackChannel{
		cfg:     cfg,
		bus:     messageBus,
		allowed: slackAllowChecker(cfg),
		stop:    make(chan struct{}),
	}
}

// Name implements Channel.
func (c *SlackChannel) Name() string { return "slack" }

// Start implements Channel.
func (c *SlackChannel) Start(ctx context.Context) error {
	if c.cfg.BotToken == "" || c.cfg.AppToken == "" {
		slog.Error("Slack bot/app token not configured")
		return nil
	}
	if c.cfg.Mode != "socket" {
		slog.Error("Unsupported Slack mode", "mode", c.cfg.Mode)
		return nil
	}
	c.api = slack.New(c.cfg.BotToken, slack.OptionAppLevelToken(c.cfg.AppToken))
	c.socket = socketmode.New(c.api)

	// Resolve bot user ID
	if auth, err := c.api.AuthTest(); err == nil {
		c.botUserID = auth.UserID
		slog.Info("Slack bot connected", "user_id", c.botUserID)
	}

	socketmodeHandler := socketmode.NewSocketmodeHandler(c.socket)
	socketmodeHandler.Handle(socketmode.EventTypeEventsAPI, c.handleEventsAPI)
	socketmodeHandler.Handle(socketmode.EventTypeInteractive, func(evt *socketmode.Event, client *socketmode.Client) {
		client.Ack(*evt.Request)
	})

	go func() {
		socketmodeHandler.RunEventLoop()
	}()

	slog.Info("Slack channel started (Socket Mode)")
	<-ctx.Done()
	return nil
}

// Stop implements Channel.
func (c *SlackChannel) Stop() {
	close(c.stop)
	c.mu.Lock()
	c.socket = nil
	c.api = nil
	c.mu.Unlock()
}

func (c *SlackChannel) handleEventsAPI(evt *socketmode.Event, client *socketmode.Client) {
	eventsAPIEvent, ok := evt.Data.(slackevents.EventsAPIEvent)
	if !ok {
		return
	}
	client.Ack(*evt.Request)
	if eventsAPIEvent.Type != slackevents.CallbackEvent {
		return
	}
	inner := eventsAPIEvent.InnerEvent
	ctx := context.Background()
	switch ev := inner.Data.(type) {
	case *slackevents.AppMentionEvent:
		c.handleMessage(ctx, ev.User, ev.Channel, ev.Text, ev.TimeStamp, "channel", client)
	case *slackevents.MessageEvent:
		if ev.SubType == "" {
			channelType := ev.ChannelType
			if channelType == "" {
				channelType = "channel"
			}
			c.handleMessage(ctx, ev.User, ev.Channel, ev.Text, ev.TimeStamp, channelType, client)
		}
	}
}

func (c *SlackChannel) handleMessage(ctx context.Context, userID, channelID, text, ts, channelType string, client *socketmode.Client) {
	if userID == "" || channelID == "" {
		return
	}
	if c.botUserID != "" && userID == c.botUserID {
		return
	}
	if channelType == "im" {
		if !c.cfg.DM.Enabled {
			return
		}
		if c.cfg.DM.Policy == "allowlist" && !c.allowed(userID) {
			return
		}
	} else {
		if c.cfg.GroupPolicy == "allowlist" {
			found := false
			for _, id := range c.cfg.GroupAllowFrom {
				if id == channelID {
					found = true
					break
				}
			}
			if !found {
				return
			}
		}
		if c.cfg.GroupPolicy == "mention" {
			if c.botUserID == "" || !strings.Contains(text, "<@"+c.botUserID+">") {
				return
			}
		}
	}
	text = c.stripBotMention(text)
	if text == "" {
		return
	}
	threadTs := ts
	if c.cfg.ReplyInThread && threadTs == "" {
		threadTs = ts
	}
	in := &bus.InboundMessage{
		Channel:  "slack",
		SenderID: userID,
		ChatID:   channelID,
		Content:  text,
		Metadata: map[string]any{
			"slack": map[string]any{
				"thread_ts":       threadTs,
				"channel_type":   channelType,
			},
		},
	}
	if err := c.bus.PublishInbound(ctx, in); err != nil {
		slog.Error("Slack publish inbound", "error", err)
	}
}

func slackAllowChecker(cfg config.SlackConfig) func(string) bool {
	return func(senderID string) bool {
		if len(cfg.AllowFrom) > 0 {
			for _, id := range cfg.AllowFrom {
				if id == senderID {
					return true
				}
			}
			return false
		}
		return true
	}
}

func (c *SlackChannel) stripBotMention(text string) string {
	if text == "" || c.botUserID == "" {
		return text
	}
	re := regexp.MustCompile(`<@` + regexp.QuoteMeta(c.botUserID) + `>\s*`)
	return strings.TrimSpace(re.ReplaceAllString(text, ""))
}

// Send implements Channel.
func (c *SlackChannel) Send(ctx context.Context, msg *bus.OutboundMessage) error {
	c.mu.Lock()
	api := c.api
	c.mu.Unlock()
	if api == nil {
		return nil
	}
	opts := []slack.MsgOption{slack.MsgOptionText(msg.Content, false)}
	if msg.Metadata != nil {
		if slackMeta, ok := msg.Metadata["slack"].(map[string]any); ok {
			if threadTs, ok := slackMeta["thread_ts"].(string); ok && threadTs != "" {
				opts = append(opts, slack.MsgOptionTS(threadTs))
			}
		}
	}
	_, _, err := api.PostMessage(msg.ChatID, opts...)
	return err
}
