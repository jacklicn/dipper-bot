package bus

import (
	"context"
	"log/slog"
	"sync"
	"time"
)

// OutboundCallback is called when an outbound message is dispatched to a channel.
type OutboundCallback func(ctx context.Context, msg *OutboundMessage) error

// MessageBus decouples chat channels from the agent core.
// Channels push to inbound; agent consumes and pushes to outbound; dispatcher sends to channels.
type MessageBus struct {
	inbound   chan *InboundMessage
	outbound  chan *OutboundMessage
	subs      map[string][]OutboundCallback
	subsMu    sync.RWMutex
	running   bool
	runningMu sync.Mutex
	done      chan struct{}
}

// NewMessageBus creates a new message bus with default buffer sizes.
func NewMessageBus() *MessageBus {
	return &MessageBus{
		inbound:  make(chan *InboundMessage, 256),
		outbound: make(chan *OutboundMessage, 256),
		subs:     make(map[string][]OutboundCallback),
		done:     make(chan struct{}),
	}
}

// PublishInbound publishes a message from a channel to the agent.
func (b *MessageBus) PublishInbound(ctx context.Context, msg *InboundMessage) error {
	select {
	case b.inbound <- msg:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// ConsumeInbound blocks until an inbound message is available or ctx is done.
func (b *MessageBus) ConsumeInbound(ctx context.Context) (*InboundMessage, error) {
	select {
	case msg := <-b.inbound:
		return msg, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

// ConsumeInboundWithTimeout returns the next message, or (nil, nil) after timeout so the caller can check ctx.
func (b *MessageBus) ConsumeInboundWithTimeout(ctx context.Context, timeout time.Duration) (*InboundMessage, error) {
	timer := time.NewTimer(timeout)
	defer timer.Stop()
	select {
	case msg := <-b.inbound:
		return msg, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-timer.C:
		return nil, nil
	}
}

// PublishOutbound publishes a response from the agent to channels.
func (b *MessageBus) PublishOutbound(ctx context.Context, msg *OutboundMessage) error {
	select {
	case b.outbound <- msg:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// SubscribeOutbound registers a callback for outbound messages for a channel.
func (b *MessageBus) SubscribeOutbound(channel string, cb OutboundCallback) {
	b.subsMu.Lock()
	defer b.subsMu.Unlock()
	b.subs[channel] = append(b.subs[channel], cb)
}

// DispatchOutbound runs the outbound dispatcher loop. Call in a goroutine.
func (b *MessageBus) DispatchOutbound(ctx context.Context) {
	b.runningMu.Lock()
	if b.running {
		b.runningMu.Unlock()
		return
	}
	b.running = true
	b.runningMu.Unlock()
	defer func() {
		b.runningMu.Lock()
		b.running = false
		b.runningMu.Unlock()
		close(b.done)
	}()

	for {
		select {
		case <-ctx.Done():
			return
		case msg, ok := <-b.outbound:
			if !ok {
				return
			}
			b.subsMu.RLock()
			cbs := b.subs[msg.Channel]
			b.subsMu.RUnlock()
			for _, cb := range cbs {
				if err := cb(ctx, msg); err != nil {
					slog.Error("dispatch outbound", "channel", msg.Channel, "error", err)
				}
			}
		}
	}
}

// Stop signals the dispatcher to stop (cancel the context passed to DispatchOutbound).
func (b *MessageBus) Stop() { close(b.done) }

// InboundSize returns the number of pending inbound messages (approximate).
func (b *MessageBus) InboundSize() int { return len(b.inbound) }

// OutboundSize returns the number of pending outbound messages (approximate).
func (b *MessageBus) OutboundSize() int { return len(b.outbound) }
