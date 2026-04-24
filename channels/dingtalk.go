package channels

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/jacklicn/dipper-bot/bus"
	"github.com/jacklicn/dipper-bot/config"
	"github.com/open-dingtalk/dingtalk-stream-sdk-go/chatbot"
	dtclient "github.com/open-dingtalk/dingtalk-stream-sdk-go/client"
)

// DingTalkChannel connects to DingTalk via Stream Mode.
type DingTalkChannel struct {
	cfg          config.DingTalkConfig
	bus          *bus.MessageBus
	allowed      func(string) bool
	streamClient *dtclient.StreamClient
	httpClient   *http.Client
	accessToken  string
	tokenExpiry  time.Time
	stop         chan struct{}
	mu           sync.Mutex
}

// NewDingTalkChannel creates a DingTalk channel.
func NewDingTalkChannel(cfg config.DingTalkConfig, messageBus *bus.MessageBus) *DingTalkChannel {
	return &DingTalkChannel{
		cfg:        cfg,
		bus:        messageBus,
		allowed:    AllowChecker(cfg.AllowFrom),
		stop:       make(chan struct{}),
		httpClient: &http.Client{Timeout: 10 * time.Second},
	}
}

// Name implements Channel.
func (c *DingTalkChannel) Name() string { return "dingtalk" }

// Start implements Channel.
func (c *DingTalkChannel) Start(ctx context.Context) error {
	if c.cfg.ClientID == "" || c.cfg.ClientSecret == "" {
		slog.Error("DingTalk client_id and client_secret not configured")
		return nil
	}
	cred := dtclient.NewAppCredentialConfig(c.cfg.ClientID, c.cfg.ClientSecret)
	c.streamClient = dtclient.NewStreamClient(dtclient.WithAppCredential(cred))
	c.streamClient.RegisterChatBotCallbackRouter(c.onChatbotMessage)
	slog.Info("DingTalk channel started (Stream Mode)")
	go func() {
		for {
			select {
			case <-ctx.Done():
				return
			case <-c.stop:
				return
			default:
				if err := c.streamClient.Start(ctx); err != nil {
					slog.Warn("DingTalk stream error", "error", err)
				}
				time.Sleep(5 * time.Second)
			}
		}
	}()
	<-ctx.Done()
	return nil
}

// Stop implements Channel.
func (c *DingTalkChannel) Stop() {
	close(c.stop)
	c.mu.Lock()
	if c.streamClient != nil {
		c.streamClient.Close()
		c.streamClient = nil
	}
	c.mu.Unlock()
}

func (c *DingTalkChannel) onChatbotMessage(ctx context.Context, data *chatbot.BotCallbackDataModel) ([]byte, error) {
	content := data.Text.Content
	if content == "" {
		return []byte(""), nil
	}
	senderID := data.SenderStaffId
	if senderID == "" {
		senderID = data.SenderId
	}
	if !c.allowed(senderID) {
		return []byte(""), nil
	}
	conversationType := data.ConversationType
	conversationID := data.ConversationId
	chatID := senderID
	if conversationType == "2" && conversationID != "" {
		chatID = "group:" + conversationID
	}
	in := &bus.InboundMessage{
		Channel:  "dingtalk",
		SenderID: senderID,
		ChatID:   chatID,
		Content:  content,
		Metadata: map[string]any{"platform": "dingtalk"},
	}
	_ = c.bus.PublishInbound(ctx, in)
	return []byte(""), nil
}

// Send implements Channel.
func (c *DingTalkChannel) Send(ctx context.Context, msg *bus.OutboundMessage) error {
	token, err := c.getAccessToken()
	if err != nil {
		return err
	}
	chatID := msg.ChatID
	msgParam := `{"text":"` + escapeJSONString(msg.Content) + `","title":"Reply"}`
	if strings.HasPrefix(chatID, "group:") {
		chatID = chatID[6:]
		url := "https://api.dingtalk.com/v1.0/robot/groupMessages/send"
		payload := map[string]any{
			"robotCode":           c.cfg.ClientID,
			"openConversationId":  chatID,
			"msgKey":              "sampleMarkdown",
			"msgParam":            msgParam,
		}
		return c.postJSON(url, token, payload)
	}
	url := "https://api.dingtalk.com/v1.0/robot/oToMessages/batchSend"
	payload := map[string]any{
		"robotCode": c.cfg.ClientID,
		"userIds":   []string{chatID},
		"msgKey":    "sampleMarkdown",
		"msgParam":  msgParam,
	}
	return c.postJSON(url, token, payload)
}

func escapeJSONString(s string) string {
	return strings.ReplaceAll(strings.ReplaceAll(s, "\\", "\\\\"), "\"", "\\\"")
}

func (c *DingTalkChannel) getAccessToken() (string, error) {
	c.mu.Lock()
	if c.accessToken != "" && time.Now().Before(c.tokenExpiry) {
		t := c.accessToken
		c.mu.Unlock()
		return t, nil
	}
	c.mu.Unlock()
	body := map[string]string{
		"appKey":    c.cfg.ClientID,
		"appSecret": c.cfg.ClientSecret,
	}
	b, _ := json.Marshal(body)
	resp, err := c.httpClient.Post("https://api.dingtalk.com/v1.0/oauth2/accessToken", "application/json", bytes.NewReader(b))
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	var result struct {
		AccessToken string `json:"accessToken"`
		ExpireIn    int    `json:"expireIn"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return "", err
	}
	c.mu.Lock()
	c.accessToken = result.AccessToken
	c.tokenExpiry = time.Now().Add(time.Duration(result.ExpireIn-60) * time.Second)
	c.mu.Unlock()
	return c.accessToken, nil
}

func (c *DingTalkChannel) postJSON(url, token string, payload any) error {
	b, _ := json.Marshal(payload)
	req, _ := http.NewRequest("POST", url, bytes.NewReader(b))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("x-acs-dingtalk-access-token", token)
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		return fmt.Errorf("dingtalk api error: %d", resp.StatusCode)
	}
	return nil
}
