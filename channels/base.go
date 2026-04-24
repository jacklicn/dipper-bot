package channels

import (
	"context"

	"github.com/jacklicn/dipper-bot/bus"
)

// Channel is the interface for chat platform integrations.
type Channel interface {
	Name() string
	Start(ctx context.Context) error
	Stop()
	Send(ctx context.Context, msg *bus.OutboundMessage) error
}

// AllowChecker returns true if sender is allowed.
// allowFrom contains "*" = allow all; empty = allow all (backward compat); otherwise allow listed IDs.
func AllowChecker(allowFrom []string) func(senderID string) bool {
	return func(senderID string) bool {
		for _, id := range allowFrom {
			if id == "*" || id == senderID {
				return true
			}
		}
		return len(allowFrom) == 0
	}
}
