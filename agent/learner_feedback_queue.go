package agent

import (
	"context"
	"log/slog"

	"github.com/jacklicn/dipper-bot/bus"
)

const maxPendingLearnerLinesPerSession = 16

func (l *AgentLoop) appendPendingLearnerLine(sessionKey, line string) {
	if l == nil || sessionKey == "" || line == "" {
		return
	}
	l.pendingLearnMu.Lock()
	defer l.pendingLearnMu.Unlock()
	if l.pendingLearnLines == nil {
		l.pendingLearnLines = make(map[string][]string)
	}
	prev := l.pendingLearnLines[sessionKey]
	if len(prev) > 0 && prev[len(prev)-1] == line {
		return
	}
	next := append(prev, line)
	if len(next) > maxPendingLearnerLinesPerSession {
		next = next[len(next)-maxPendingLearnerLinesPerSession:]
	}
	l.pendingLearnLines[sessionKey] = next
}

func (l *AgentLoop) takePendingLearnerBlock(sessionKey string) string {
	if l == nil || sessionKey == "" {
		return ""
	}
	l.pendingLearnMu.Lock()
	defer l.pendingLearnMu.Unlock()
	if l.pendingLearnLines == nil {
		return ""
	}
	lines := l.pendingLearnLines[sessionKey]
	if len(lines) == 0 {
		return ""
	}
	delete(l.pendingLearnLines, sessionKey)
	s := formatPendingLearnerDigest(lines)
	slog.Info("learner feedback prepended", "session", sessionKey, "lines", len(lines))
	return s
}

func (l *AgentLoop) clearPendingLearnerFeedback(sessionKey string) {
	if l == nil || sessionKey == "" {
		return
	}
	l.pendingLearnMu.Lock()
	defer l.pendingLearnMu.Unlock()
	if l.pendingLearnLines != nil {
		delete(l.pendingLearnLines, sessionKey)
	}
}

func (l *AgentLoop) maybeInstantLearnerPush(sessionKey, line string) {
	if l == nil || !effectiveLearnerFeedbackInstantPush(l.exp) || l.bus == nil || sessionKey == "" || line == "" {
		return
	}
	ch, cid := channelChatFromRouteKey(sessionKey)
	if ch == "" {
		return
	}
	busRef := l.bus
	go func(text string) {
		_ = busRef.PublishOutbound(context.Background(), &bus.OutboundMessage{
			Channel: ch, ChatID: cid, Content: text,
		})
	}(line)
}

// recordLearnerFeedback keeps instant push and digest mutually exclusive.
// - default (instant=true): push now, skip digest queue
// - instant=false: queue for next reply digest
func (l *AgentLoop) recordLearnerFeedback(sessionKey, line string) {
	if l == nil || sessionKey == "" || line == "" {
		return
	}
	if effectiveLearnerFeedbackInstantPush(l.exp) {
		l.maybeInstantLearnerPush(sessionKey, line)
		return
	}
	l.appendPendingLearnerLine(sessionKey, line)
}
