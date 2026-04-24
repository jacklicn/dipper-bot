package channels

import (
	"context"
	"crypto/tls"
	"fmt"
	"log/slog"
	"net"
	"net/smtp"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/emersion/go-imap"
	"github.com/emersion/go-imap/client"
	"github.com/emersion/go-message/mail"
	"github.com/jacklicn/dipper-bot/bus"
	"github.com/jacklicn/dipper-bot/config"
)

func dialSSL(addr string) (net.Conn, error) {
	host := addr
	if idx := strings.Index(addr, ":"); idx >= 0 {
		host = addr[:idx]
	}
	config := &tls.Config{ServerName: host}
	return tls.Dial("tcp", addr, config)
}

// EmailChannel polls IMAP for inbound emails and sends via SMTP.
type EmailChannel struct {
	cfg                 config.EmailConfig
	bus                 *bus.MessageBus
	allowed             func(string) bool
	stop                chan struct{}
	lastSubjectByChat   map[string]string
	lastMessageIDByChat map[string]string
	processedUIDs       map[string]struct{}
	maxProcessedUIDs    int
	mu                  sync.Mutex
}

// NewEmailChannel creates an Email channel.
func NewEmailChannel(cfg config.EmailConfig, messageBus *bus.MessageBus) *EmailChannel {
	return &EmailChannel{
		cfg:                 cfg,
		bus:                 messageBus,
		allowed:             AllowChecker(cfg.AllowFrom),
		stop:                make(chan struct{}),
		lastSubjectByChat:   make(map[string]string),
		lastMessageIDByChat: make(map[string]string),
		processedUIDs:       make(map[string]struct{}),
		maxProcessedUIDs:    100000,
	}
}

// Name implements Channel.
func (c *EmailChannel) Name() string { return "email" }

// Start implements Channel. Polls IMAP for new messages.
func (c *EmailChannel) Start(ctx context.Context) error {
	if !c.cfg.ConsentGranted {
		slog.Warn("Email channel disabled: consentGranted is false")
		return nil
	}
	if !c.validateConfig() {
		return nil
	}
	slog.Info("Starting Email channel (IMAP polling mode)...")
	pollSec := c.cfg.PollIntervalSec
	if pollSec < 5 {
		pollSec = 30
	}
	ticker := time.NewTicker(time.Duration(pollSec) * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return nil
		case <-c.stop:
			return nil
		case <-ticker.C:
			items := c.fetchNewMessages()
			for _, item := range items {
				sender, _ := item["sender"].(string)
				if subject, ok := item["subject"].(string); ok && subject != "" {
					c.mu.Lock()
					c.lastSubjectByChat[sender] = subject
					c.mu.Unlock()
				}
				if mid, ok := item["message_id"].(string); ok && mid != "" {
					c.mu.Lock()
					c.lastMessageIDByChat[sender] = mid
					c.mu.Unlock()
				}
				content, _ := item["content"].(string)
				metadata, _ := item["metadata"].(map[string]any)
				in := &bus.InboundMessage{
					Channel:   "email",
					SenderID:  sender,
					ChatID:    sender,
					Content:   content,
					Timestamp: time.Now(),
					Metadata:  metadata,
				}
				if !c.allowed(sender) {
					continue
				}
				_ = c.bus.PublishInbound(ctx, in)
			}
		}
	}
}

// Stop implements Channel.
func (c *EmailChannel) Stop() {
	close(c.stop)
}

// Send implements Channel.
func (c *EmailChannel) Send(ctx context.Context, msg *bus.OutboundMessage) error {
	if !c.cfg.ConsentGranted {
		return nil
	}
	if c.cfg.SMTPHost == "" {
		return nil
	}
	toAddr := strings.TrimSpace(msg.ChatID)
	if toAddr == "" {
		return nil
	}
	c.mu.Lock()
	baseSubject := c.lastSubjectByChat[toAddr]
	inReplyTo := c.lastMessageIDByChat[toAddr]
	c.mu.Unlock()
	if baseSubject == "" {
		baseSubject = "dipper-bot reply"
	}
	subject := c.replySubject(baseSubject)
	if msg.Metadata != nil {
		if s, ok := msg.Metadata["subject"].(string); ok && strings.TrimSpace(s) != "" {
			subject = strings.TrimSpace(s)
		}
	}
	from := c.cfg.FromAddress
	if from == "" {
		from = c.cfg.SMTPUsername
	}
	if from == "" {
		from = c.cfg.IMAPUsername
	}
	body := msg.Content
	if body == "" {
		body = " "
	}
	header := fmt.Sprintf("From: %s\r\nTo: %s\r\nSubject: %s\r\nMIME-Version: 1.0\r\nContent-Type: text/plain; charset=utf-8\r\n\r\n",
		from, toAddr, subject)
	if inReplyTo != "" {
		header = fmt.Sprintf("In-Reply-To: %s\r\nReferences: %s\r\n%s", inReplyTo, inReplyTo, header)
	}
	auth := smtp.PlainAuth("", c.cfg.SMTPUsername, c.cfg.SMTPPassword, strings.Split(c.cfg.SMTPHost, ":")[0])
	addr := fmt.Sprintf("%s:%d", c.cfg.SMTPHost, c.cfg.SMTPPort)
	if c.cfg.SMTPPort == 0 {
		addr = c.cfg.SMTPHost
	}
	if c.cfg.SMTPUseSSL {
		return c.sendSMTPSSL(addr, auth, from, toAddr, header+body)
	}
	return smtp.SendMail(addr, auth, from, []string{toAddr}, []byte(header+body))
}

func (c *EmailChannel) sendSMTPSSL(addr string, auth smtp.Auth, from, to, body string) error {
	conn, err := dialSSL(addr)
	if err != nil {
		return err
	}
	defer conn.Close()
	client, err := smtp.NewClient(conn, strings.Split(addr, ":")[0])
	if err != nil {
		return err
	}
	defer client.Close()
	if err = client.Auth(auth); err != nil {
		return err
	}
	if err = client.Mail(from); err != nil {
		return err
	}
	if err = client.Rcpt(to); err != nil {
		return err
	}
	w, err := client.Data()
	if err != nil {
		return err
	}
	_, err = w.Write([]byte(body))
	if err != nil {
		return err
	}
	return w.Close()
}

func (c *EmailChannel) validateConfig() bool {
	missing := []string{}
	if c.cfg.IMAPHost == "" {
		missing = append(missing, "imapHost")
	}
	if c.cfg.IMAPUsername == "" {
		missing = append(missing, "imapUsername")
	}
	if c.cfg.IMAPPassword == "" {
		missing = append(missing, "imapPassword")
	}
	if c.cfg.SMTPHost == "" {
		missing = append(missing, "smtpHost")
	}
	if c.cfg.SMTPUsername == "" {
		missing = append(missing, "smtpUsername")
	}
	if c.cfg.SMTPPassword == "" {
		missing = append(missing, "smtpPassword")
	}
	if len(missing) > 0 {
		slog.Error("Email channel not configured", "missing", missing)
		return false
	}
	return true
}

func (c *EmailChannel) replySubject(base string) string {
	base = strings.TrimSpace(base)
	if base == "" {
		base = "dipper-bot reply"
	}
	prefix := c.cfg.SubjectPrefix
	if prefix == "" {
		prefix = "Re: "
	}
	if strings.HasPrefix(strings.ToLower(base), "re:") {
		return base
	}
	return prefix + base
}

func (c *EmailChannel) fetchNewMessages() []map[string]any {
	return c.fetchMessages([]string{"UNSEEN"}, true, true, 0)
}

func (c *EmailChannel) fetchMessages(criteria []string, markSeen, dedupe bool, limit int) []map[string]any {
	var result []map[string]any
	mailbox := c.cfg.IMAPMailbox
	if mailbox == "" {
		mailbox = "INBOX"
	}
	var cl *client.Client
	var err error
	if c.cfg.IMAPUseSSL {
		addr := fmt.Sprintf("%s:%d", c.cfg.IMAPHost, c.cfg.IMAPPort)
		if c.cfg.IMAPPort == 0 {
			addr = c.cfg.IMAPHost
		}
		cl, err = client.DialTLS(addr, nil)
	} else {
		addr := fmt.Sprintf("%s:%d", c.cfg.IMAPHost, c.cfg.IMAPPort)
		if c.cfg.IMAPPort == 0 {
			addr = c.cfg.IMAPHost
		}
		cl, err = client.Dial(addr)
	}
	if err != nil {
		slog.Error("Email IMAP connect", "error", err)
		return result
	}
	defer cl.Logout()
	if err = cl.Login(c.cfg.IMAPUsername, c.cfg.IMAPPassword); err != nil {
		slog.Error("Email IMAP login", "error", err)
		return result
	}
	_, err = cl.Select(mailbox, false)
	if err != nil {
		return result
	}
	searchCrit := imap.NewSearchCriteria()
	switch len(criteria) {
	case 1:
		if criteria[0] == "UNSEEN" {
			searchCrit.WithoutFlags = []string{imap.SeenFlag}
		}
	default:
		// SINCE/BEFORE etc - simplified for UNSEEN only
	}
	ids, err := cl.Search(searchCrit)
	if err != nil || len(ids) == 0 {
		return result
	}
	if limit > 0 && len(ids) > limit {
		ids = ids[len(ids)-limit:]
	}
	for _, id := range ids {
		seq := new(imap.SeqSet)
		seq.AddNum(id)
		items := make(chan *imap.Message, 1)
		go func() {
			_ = cl.Fetch(seq, []imap.FetchItem{imap.FetchEnvelope, imap.FetchBody, imap.FetchUid}, items)
		}()
		msg := <-items
		if msg == nil {
			continue
		}
		uid := ""
		if msg.Uid != 0 {
			uid = fmt.Sprintf("%d", msg.Uid)
		}
		if dedupe && uid != "" {
			c.mu.Lock()
			if _, ok := c.processedUIDs[uid]; ok {
				c.mu.Unlock()
				continue
			}
			c.mu.Unlock()
		}
		body := c.extractBody(msg)
		if body == "" {
			body = "(empty email body)"
		}
		maxChars := c.cfg.MaxBodyChars
		if maxChars <= 0 {
			maxChars = 12000
		}
		if len(body) > maxChars {
			body = body[:maxChars]
		}
		from := ""
		if msg.Envelope != nil && len(msg.Envelope.From) > 0 {
			from = strings.ToLower(strings.TrimSpace(msg.Envelope.From[0].Address()))
		}
		if from == "" {
			continue
		}
		subject := ""
		if msg.Envelope != nil {
			subject = msg.Envelope.Subject
		}
		dateVal := ""
		if msg.Envelope != nil && !msg.Envelope.Date.IsZero() {
			dateVal = msg.Envelope.Date.Format(time.RFC1123Z)
		}
		messageID := ""
		if msg.Envelope != nil {
			messageID = msg.Envelope.MessageId
		}
		content := fmt.Sprintf("Email received.\nFrom: %s\nSubject: %s\nDate: %s\n\n%s", from, subject, dateVal, body)
		metadata := map[string]any{
			"message_id":   messageID,
			"subject":      subject,
			"date":         dateVal,
			"sender_email": from,
			"uid":          uid,
		}
		result = append(result, map[string]any{
			"sender":     from,
			"subject":    subject,
			"message_id": messageID,
			"content":    content,
			"metadata":   metadata,
		})
		if dedupe && uid != "" {
			c.mu.Lock()
			c.processedUIDs[uid] = struct{}{}
			if markSeen {
				seq2 := new(imap.SeqSet)
				seq2.AddNum(id)
				cl.Store(seq2, imap.AddFlags, []interface{}{imap.SeenFlag}, nil)
			}
			if len(c.processedUIDs) > c.maxProcessedUIDs {
				n := len(c.processedUIDs) / 2
				newMap := make(map[string]struct{})
				i := 0
				for k := range c.processedUIDs {
					if i >= n {
						newMap[k] = struct{}{}
					}
					i++
				}
				c.processedUIDs = newMap
			}
			c.mu.Unlock()
		}
	}
	return result
}

func (c *EmailChannel) extractBody(msg *imap.Message) string {
	if msg == nil {
		return ""
	}
	for _, v := range msg.Body {
		if v == nil {
			continue
		}
		mr, err := mail.CreateReader(v)
		if err != nil {
			continue
		}
		for {
			p, err := mr.NextPart()
			if err != nil {
				break
			}
			if p == nil {
				break
			}
			switch h := p.Header.(type) {
			case *mail.InlineHeader:
				ct, _, _ := h.ContentType()
				if ct == "text/plain" {
					b := make([]byte, 64*1024)
					n, _ := p.Body.Read(b)
					return strings.TrimSpace(string(b[:n]))
				}
			case *mail.AttachmentHeader:
				continue
			}
		}
	}
	for _, v := range msg.Body {
		if v == nil {
			continue
		}
		mr, err := mail.CreateReader(v)
		if err != nil {
			continue
		}
		for {
			p, err := mr.NextPart()
			if err != nil {
				break
			}
			if p == nil {
				break
			}
			switch h := p.Header.(type) {
			case *mail.InlineHeader:
				ct, _, _ := h.ContentType()
				if ct == "text/html" {
					b := make([]byte, 64*1024)
					n, _ := p.Body.Read(b)
					return htmlToText(strings.TrimSpace(string(b[:n])))
				}
			}
		}
	}
	return ""
}

var (
	brRe   = regexp.MustCompile(`(?i)<\s*br\s*/?>`)
	closeP = regexp.MustCompile(`(?i)<\s*/\s*p\s*>`)
	tagRe  = regexp.MustCompile(`<[^>]+>`)
)

func htmlToText(raw string) string {
	raw = brRe.ReplaceAllString(raw, "\n")
	raw = closeP.ReplaceAllString(raw, "\n")
	raw = tagRe.ReplaceAllString(raw, "")
	return raw
}
