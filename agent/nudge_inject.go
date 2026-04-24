package agent

import (
	"github.com/jacklicn/dipper-bot/config"
	"github.com/jacklicn/dipper-bot/providers"
	"github.com/jacklicn/dipper-bot/session"
)

// injectMemoryNudgeAfterSystem inserts a one-shot user reminder after the system message
// when enough completed user turns passed without the memory tool (Hermes-style nudge).
func injectMemoryNudgeAfterSystem(msgs []providers.Message, exp config.AgentExperienceConfig, sess *session.Session) []providers.Message {
	if sess == nil || len(msgs) == 0 || msgs[0].Role != "system" {
		return msgs
	}
	n := effectiveMemoryNudgeEvery(exp)
	if n <= 0 {
		return msgs
	}
	if sess.UserTurnsSinceMemoryTools() < n {
		return msgs
	}
	text := "[Reminder] Several user turns passed without the memory tool. Persist with memory: target user → USER.md (profile; replace outdated lines when the user corrects earlier info), target memory → NOTE.md (facts). MEMORY.md from token consolidation (if enabled) is separate—not a substitute for USER/NOTE."
	out := make([]providers.Message, 0, len(msgs)+1)
	out = append(out, msgs[0])
	out = append(out, providers.Message{Role: "user", Content: text})
	out = append(out, msgs[1:]...)
	return out
}
