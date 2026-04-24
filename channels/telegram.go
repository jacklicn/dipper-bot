package channels

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"sync"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
	"github.com/jacklicn/dipper-bot/bus"
	"github.com/jacklicn/dipper-bot/config"
	"github.com/jacklicn/dipper-bot/providers"
)

// TelegramChannel connects to Telegram Bot API and forwards messages via the bus.
type TelegramChannel struct {
	cfg         config.TelegramConfig
	bus         *bus.MessageBus
	allowed     func(string) bool
	bot         *tgbotapi.BotAPI
	stop        chan struct{}
	mu          sync.Mutex
	transcriber providers.TranscriptionProvider
}

// NewTelegramChannel creates a Telegram channel. transcriber enables voice transcription (Groq or Vosk).
func NewTelegramChannel(cfg config.TelegramConfig, messageBus *bus.MessageBus, transcriber providers.TranscriptionProvider) *TelegramChannel {
	return &TelegramChannel{
		cfg:         cfg,
		bus:         messageBus,
		allowed:     AllowChecker(cfg.AllowFrom),
		stop:        make(chan struct{}),
		transcriber: transcriber,
	}
}

// Name implements Channel.
func (c *TelegramChannel) Name() string { return "telegram" }

// Start implements Channel. Runs long polling.
func (c *TelegramChannel) Start(ctx context.Context) error {
	if c.cfg.Token == "" {
		slog.Error("Telegram token not configured")
		return nil
	}
	bot, err := tgbotapi.NewBotAPI(c.cfg.Token)
	if err != nil {
		return err
	}
	c.mu.Lock()
	c.bot = bot
	c.mu.Unlock()

	u := tgbotapi.NewUpdate(0)
	u.Timeout = 60
	updates := bot.GetUpdatesChan(u)

	go func() {
		for {
			select {
			case <-ctx.Done():
				return
			case <-c.stop:
				return
			case update := <-updates:
				c.handleUpdate(ctx, update)
			}
		}
	}()
	slog.Info("Telegram channel started")
	return nil
}

// Stop implements Channel.
func (c *TelegramChannel) Stop() {
	close(c.stop)
	c.mu.Lock()
	c.bot = nil
	c.mu.Unlock()
}

func (c *TelegramChannel) handleUpdate(ctx context.Context, update tgbotapi.Update) {
	if update.Message == nil {
		return
	}
	msg := update.Message
	senderID := formatTelegramUserID(msg.From)
	if !c.allowed(senderID) {
		slog.Warn("Telegram access denied", "sender", senderID)
		return
	}
	chatID := formatChatID(msg.Chat.ID)
	content := msg.Text
	if content == "" && msg.Caption != "" {
		content = "[Media] " + msg.Caption
	}
	if content == "" && msg.Voice != nil && c.transcriber != nil {
		if text := c.transcribeVoice(ctx, msg.Voice.FileID); text != "" {
			content = "[transcription: " + text + "]"
		}
	}
	if content == "" {
		content = "[non-text message]"
	}

	in := &bus.InboundMessage{
		Channel:   "telegram",
		SenderID:  senderID,
		ChatID:    chatID,
		Content:   content,
		Metadata:  map[string]any{"message_id": msg.MessageID},
	}
	if err := c.bus.PublishInbound(ctx, in); err != nil {
		slog.Error("Telegram publish inbound", "error", err)
	}
}

func (c *TelegramChannel) transcribeVoice(ctx context.Context, fileID string) string {
	c.mu.Lock()
	bot := c.bot
	c.mu.Unlock()
	if bot == nil || c.transcriber == nil {
		return ""
	}
	file, err := bot.GetFile(tgbotapi.FileConfig{FileID: fileID})
	if err != nil {
		slog.Warn("Telegram get file", "error", err)
		return ""
	}
	url := "https://api.telegram.org/file/bot" + bot.Token + "/" + file.FilePath
	resp, err := http.Get(url)
	if err != nil {
		slog.Warn("Telegram download voice", "error", err)
		return ""
	}
	defer resp.Body.Close()
	tmp, err := os.CreateTemp("", "dipper-bot-voice-*.ogg")
	if err != nil {
		return ""
	}
	defer os.Remove(tmp.Name())
	defer tmp.Close()
	if _, err := io.Copy(tmp, resp.Body); err != nil {
		return ""
	}
	_ = tmp.Sync()
	path, _ := filepath.Abs(tmp.Name())
	text, err := c.transcriber.Transcribe(ctx, path)
	if err != nil {
		slog.Warn("transcribe", "error", err)
		return ""
	}
	return text
}

// Send implements Channel.
func (c *TelegramChannel) Send(ctx context.Context, msg *bus.OutboundMessage) error {
	c.mu.Lock()
	bot := c.bot
	c.mu.Unlock()
	if bot == nil {
		return nil
	}
	chatID, err := parseTelegramChatID(msg.ChatID)
	if err != nil {
		return err
	}
	apiMsg := tgbotapi.NewMessage(chatID, msg.Content)
	apiMsg.ParseMode = "HTML"
	_, err = bot.Send(apiMsg)
	return err
}

func formatTelegramUserID(from *tgbotapi.User) string {
	if from == nil {
		return ""
	}
	return strconv.FormatInt(from.ID, 10)
}

func formatChatID(chatID int64) string {
	return strconv.FormatInt(chatID, 10)
}

func parseTelegramChatID(s string) (int64, error) {
	return strconv.ParseInt(s, 10, 64)
}
