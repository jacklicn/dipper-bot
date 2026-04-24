package gateway

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/jacklicn/dipper-bot/bus"
	"github.com/jacklicn/dipper-bot/utils"
)

// Server is the HTTP gateway server.
type Server struct {
	bus     *bus.MessageBus
	host    string
	port    int
	server  *http.Server
	limiter *utils.FixedWindowLimiter
	keyer   *utils.RateLimitKeyer
}

// NewServer creates a gateway HTTP server. host may be empty for 0.0.0.0.
func NewServer(b *bus.MessageBus, host string, port int) *Server {
	return NewServerWithRateLimit(b, host, port, 120)
}

// NewServerWithRateLimit creates a gateway server with configurable per-IP rate limit (requests/minute).
func NewServerWithRateLimit(b *bus.MessageBus, host string, port int, rateLimitPerMinute int) *Server {
	return NewServerWithRateLimitAndKeying(b, host, port, rateLimitPerMinute, 32, 128, nil)
}

// NewServerWithRateLimitAndKeying creates a gateway server with per-IP/network rate limiting.
func NewServerWithRateLimitAndKeying(
	b *bus.MessageBus,
	host string,
	port int,
	rateLimitPerMinute int,
	ipv4Prefix int,
	ipv6Prefix int,
	cidrRules []string,
) *Server {
	if rateLimitPerMinute <= 0 {
		rateLimitPerMinute = 120
	}
	s := &Server{
		bus:     b,
		host:    host,
		port:    port,
		limiter: utils.NewFixedWindowLimiter(rateLimitPerMinute, time.Minute),
		keyer:   utils.NewRateLimitKeyer(ipv4Prefix, ipv6Prefix, cidrRules),
	}
	addr := fmt.Sprintf("%s:%d", host, port)
	if host == "" {
		addr = ":" + fmt.Sprintf("%d", port)
	}
	mux := http.NewServeMux()
	mux.HandleFunc("POST /message", s.handleMessage)
	s.server = &http.Server{
		Addr:         addr,
		Handler:      withSecurityHeaders(mux),
		ReadTimeout:  10 * time.Second,
		WriteTimeout: 30 * time.Second,
	}
	return s
}

// InboundRequest is the JSON body for POST /message.
type InboundRequest struct {
	Channel  string `json:"channel"`
	ChatID   string `json:"chat_id"`
	SenderID string `json:"sender_id"`
	Content  string `json:"content"`
}

const maxInboundRequestBytes = 1 << 20 // 1MB

func (s *Server) handleMessage(w http.ResponseWriter, r *http.Request) {
	clientIP := utils.ClientIP(r)
	payloadSize := requestSize(r)
	if r.Method != http.MethodPost {
		slog.Warn("gateway rejected request", "client_ip", clientIP, "payload_size", payloadSize, "rate_limited", false, "reject_reason", "method_not_allowed")
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	rateKey := clientIP
	if s.keyer != nil {
		rateKey = s.keyer.KeyFromIP(clientIP)
	}
	if s.limiter != nil && !s.limiter.Allow(rateKey) {
		slog.Warn("gateway rejected request", "client_ip", clientIP, "payload_size", payloadSize, "rate_limited", true, "reject_reason", "rate_limited")
		http.Error(w, "too many requests", http.StatusTooManyRequests)
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, maxInboundRequestBytes)
	var req InboundRequest
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(&req); err != nil {
		slog.Warn("gateway rejected request", "client_ip", clientIP, "payload_size", payloadSize, "rate_limited", false, "reject_reason", "invalid_json", "error", err)
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	var tail any
	if err := dec.Decode(&tail); err != io.EOF {
		slog.Warn("gateway rejected request", "client_ip", clientIP, "payload_size", payloadSize, "rate_limited", false, "reject_reason", "trailing_json_data", "error", err)
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	if req.Channel == "" {
		req.Channel = "web"
	}
	if req.SenderID == "" {
		req.SenderID = "user"
	}
	if strings.TrimSpace(req.Content) == "" {
		slog.Warn("gateway rejected request", "client_ip", clientIP, "payload_size", payloadSize, "rate_limited", false, "reject_reason", "empty_content")
		http.Error(w, "content is required", http.StatusBadRequest)
		return
	}
	if strings.TrimSpace(req.ChatID) == "" {
		slog.Warn("gateway rejected request", "client_ip", clientIP, "payload_size", payloadSize, "rate_limited", false, "reject_reason", "missing_chat_id")
		http.Error(w, "chat_id is required", http.StatusBadRequest)
		return
	}
	msg := &bus.InboundMessage{
		Channel:   req.Channel,
		SenderID:  req.SenderID,
		ChatID:    req.ChatID,
		Content:   req.Content,
		Timestamp: time.Now(),
	}
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()
	if err := s.bus.PublishInbound(ctx, msg); err != nil {
		slog.Error("publish inbound", "client_ip", clientIP, "payload_size", payloadSize, "error", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	slog.Info("gateway accepted request", "client_ip", clientIP, "payload_size", payloadSize, "channel", req.Channel, "chat_id", req.ChatID)
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(http.StatusAccepted)
	_, _ = w.Write([]byte(`{"status":"accepted"}`))
}

// Start starts the HTTP server.
func (s *Server) Start() error {
	slog.Info("gateway listening", "addr", s.server.Addr)
	return s.server.ListenAndServe()
}

// Shutdown gracefully shuts down the server.
func (s *Server) Shutdown(ctx context.Context) error {
	return s.server.Shutdown(ctx)
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
