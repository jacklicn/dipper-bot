package bus_test

import (
	"context"
	"testing"
	"time"

	"github.com/jacklicn/dipper-bot/bus"
)

func TestInboundMessage_SessionKey(t *testing.T) {
	msg := &bus.InboundMessage{Channel: "telegram", ChatID: "123"}
	if got := msg.SessionKey(); got != "telegram:123" {
		t.Errorf("SessionKey() = %q, want telegram:123", got)
	}
}

func TestNewMessageBus(t *testing.T) {
	b := bus.NewMessageBus()
	if b == nil {
		t.Fatal("NewMessageBus() returned nil")
	}
	if b.InboundSize() != 0 || b.OutboundSize() != 0 {
		t.Errorf("new bus should have 0 size, got inbound=%d outbound=%d", b.InboundSize(), b.OutboundSize())
	}
}

func TestMessageBus_PublishConsumeInbound(t *testing.T) {
	b := bus.NewMessageBus()
	ctx := context.Background()
	msg := &bus.InboundMessage{Channel: "web", ChatID: "c1", Content: "hello"}

	if err := b.PublishInbound(ctx, msg); err != nil {
		t.Fatalf("PublishInbound: %v", err)
	}
	if b.InboundSize() != 1 {
		t.Errorf("InboundSize() = %d, want 1", b.InboundSize())
	}

	got, err := b.ConsumeInbound(ctx)
	if err != nil {
		t.Fatalf("ConsumeInbound: %v", err)
	}
	if got != msg || got.Content != "hello" {
		t.Errorf("ConsumeInbound got %+v, want Content=hello", got)
	}
}

func TestMessageBus_ConsumeInboundWithTimeout(t *testing.T) {
	b := bus.NewMessageBus()
	ctx := context.Background()

	got, err := b.ConsumeInboundWithTimeout(ctx, 10*time.Millisecond)
	if err != nil {
		t.Fatalf("ConsumeInboundWithTimeout: %v", err)
	}
	if got != nil {
		t.Errorf("expected nil message after timeout, got %+v", got)
	}
}

func TestMessageBus_SubscribeOutbound(t *testing.T) {
	b := bus.NewMessageBus()
	ctx := context.Background()
	var received *bus.OutboundMessage
	b.SubscribeOutbound("web", func(ctx context.Context, msg *bus.OutboundMessage) error {
		received = msg
		return nil
	})

	go b.DispatchOutbound(ctx)
	defer b.Stop()

	out := &bus.OutboundMessage{Channel: "web", ChatID: "c1", Content: "hi"}
	if err := b.PublishOutbound(ctx, out); err != nil {
		t.Fatalf("PublishOutbound: %v", err)
	}

	time.Sleep(50 * time.Millisecond)
	if received == nil || received.Content != "hi" {
		t.Errorf("subscriber did not receive message: got %+v", received)
	}
}
