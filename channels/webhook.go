package channels

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/jacklicn/dipper-bot/bus"
	"github.com/jacklicn/dipper-bot/config"
	"github.com/jacklicn/dipper-bot/utils"
)

// WebhookChannel is a plugin-style channel: HTTP server receives POST and publishes to bus.
// Enables external services to inject messages (channel plugin pattern).
type WebhookChannel struct {
	cfg      config.WebhookConfig
	bus      *bus.MessageBus
	allowed  func(string) bool
	server   *http.Server
	stop     chan struct{}
	stopOnce sync.Once
	limiter  *utils.FixedWindowLimiter
	keyer    *utils.RateLimitKeyer
	mu       sync.Mutex
}

// NewWebhookChannel creates a Webhook channel.
func NewWebhookChannel(cfg config.WebhookConfig, messageBus *bus.MessageBus) *WebhookChannel {
	rate := cfg.RateLimitPerMinute
	if rate <= 0 {
		rate = 120
	}
	return &WebhookChannel{
		cfg:     cfg,
		bus:     messageBus,
		allowed: AllowChecker(cfg.AllowFrom),
		stop:    make(chan struct{}),
		limiter: utils.NewFixedWindowLimiter(rate, time.Minute),
		keyer:   utils.NewRateLimitKeyer(cfg.RateLimitIPv4Prefix, cfg.RateLimitIPv6Prefix, cfg.RateLimitCIDRs),
	}
}

// Name implements Channel.
func (c *WebhookChannel) Name() string { return "webhook" }

// Start implements Channel. Runs HTTP server.
func (c *WebhookChannel) Start(ctx context.Context) error {
	port := c.cfg.Port
	if port <= 0 {
		port = 9000
	}
	path := c.cfg.Path
	if path == "" {
		path = "/message"
	}
	mux := http.NewServeMux()
	mux.HandleFunc(path, c.handlePost)
	c.mu.Lock()
	c.server = &http.Server{
		Addr:         ":" + strconv.Itoa(port),
		Handler:      withSecurityHeaders(mux),
		ReadTimeout:  10 * time.Second,
		WriteTimeout: 30 * time.Second,
	}
	c.mu.Unlock()
	go func() {
		select {
		case <-ctx.Done():
		case <-c.stop:
		}
		_ = c.server.Shutdown(context.Background())
	}()
	go func() {
		if err := c.server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			slog.Error("webhook channel", "error", err)
		}
	}()
	slog.Info("Webhook channel started", "port", port, "path", path)
	return nil
}

// Stop implements Channel.
func (c *WebhookChannel) Stop() {
	c.stopOnce.Do(func() { close(c.stop) })
}

// Send implements Channel. Webhook is receive-only; outbound goes to gateway or other channels.
func (c *WebhookChannel) Send(ctx context.Context, msg *bus.OutboundMessage) error {
	// Webhook is inject-only; no native send. Could POST to a callback URL if configured.
	return nil
}

func (c *WebhookChannel) handlePost(w http.ResponseWriter, r *http.Request) {
	clientIP := utils.ClientIP(r)
	payloadSize := requestSize(r)
	if r.Method != http.MethodPost {
		slog.Warn("webhook rejected request", "client_ip", clientIP, "payload_size", payloadSize, "rate_limited", false, "reject_reason", "method_not_allowed")
		http.Error(w, "Method Not Allowed", http.StatusMethodNotAllowed)
		return
	}
	rateKey := clientIP
	if c.keyer != nil {
		rateKey = c.keyer.KeyFromIP(clientIP)
	}
	if c.limiter != nil && !c.limiter.Allow(rateKey) {
		slog.Warn("webhook rejected request", "client_ip", clientIP, "payload_size", payloadSize, "rate_limited", true, "reject_reason", "rate_limited")
		http.Error(w, "Too Many Requests", http.StatusTooManyRequests)
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, 1<<20) // 1MB
	var body struct {
		Sender string   `json:"sender"`
		ChatID string   `json:"chat_id"`
		Text   string   `json:"text"`
		Media  []string `json:"media"`
	}
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(&body); err != nil {
		slog.Warn("webhook rejected request", "client_ip", clientIP, "payload_size", payloadSize, "rate_limited", false, "reject_reason", "invalid_json", "error", err)
		http.Error(w, "Bad Request", 400)
		return
	}
	var tail any
	if err := dec.Decode(&tail); err != io.EOF {
		slog.Warn("webhook rejected request", "client_ip", clientIP, "payload_size", payloadSize, "rate_limited", false, "reject_reason", "trailing_json_data", "error", err)
		http.Error(w, "Bad Request", 400)
		return
	}
	sender := body.Sender
	if sender == "" {
		sender = "unknown"
	}
	chatID := body.ChatID
	if chatID == "" {
		chatID = sender
	}
	if !c.allowed(sender) {
		slog.Warn("webhook rejected request", "client_ip", clientIP, "payload_size", payloadSize, "rate_limited", false, "reject_reason", "sender_not_allowed", "sender", sender)
		w.WriteHeader(http.StatusForbidden)
		return
	}
	if strings.TrimSpace(body.Text) == "" {
		slog.Warn("webhook rejected request", "client_ip", clientIP, "payload_size", payloadSize, "rate_limited", false, "reject_reason", "empty_text", "sender", sender)
		http.Error(w, "Bad Request", 400)
		return
	}
	imb := &bus.InboundMessage{
		Channel:   "webhook",
		ChatID:    chatID,
		SenderID:  sender,
		Content:   body.Text,
		Timestamp: time.Now(),
	}
	if err := c.bus.PublishInbound(r.Context(), imb); err != nil {
		slog.Error("webhook publish", "client_ip", clientIP, "payload_size", payloadSize, "sender", sender, "chat_id", chatID, "error", err)
		http.Error(w, "Internal Server Error", 500)
		return
	}
	slog.Info("webhook accepted request", "client_ip", clientIP, "payload_size", payloadSize, "sender", sender, "chat_id", chatID)
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]bool{"ok": true})
}

func withSecurityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("X-Frame-Options", "DENY")
		w.Header().Set("Referrer-Policy", "no-referrer")
		next.ServeHTTP(w, r)
	})
}

func requestSize(r *http.Request) int64 {
	if r == nil || r.ContentLength < 0 {
		return 0
	}
	return r.ContentLength
}
