package agent

import (
	"fmt"
	"strings"
)

// SkillApplyNotice describes a successful autonomous skill write (create or patch).
type SkillApplyNotice struct {
	Action string // "create" or "patch"
	Name   string // kebab-case skill id
	MidRun bool   // true when applied during an in-flight tool loop
}

func channelChatFromRouteKey(sessionKey string) (channel, chatID string) {
	if i := strings.Index(sessionKey, ":"); i >= 0 {
		return sessionKey[:i], sessionKey[i+1:]
	}
	return sessionKey, ""
}

// formatSkillApplyNoticeLine is a short Hermes-style ping for a follow-up outbound message (async worker / flush).
func formatSkillApplyNoticeLine(n SkillApplyNotice) string {
	rel := "skills/" + n.Name + "/SKILL.md"
	switch n.Action {
	case "create":
		return fmt.Sprintf("📚 技能已创建 / Skill created: `%s` → `%s`", n.Name, rel)
	case "patch":
		return fmt.Sprintf("📚 技能已更新 / Skill updated: `%s` → `%s`", n.Name, rel)
	default:
		return fmt.Sprintf("📚 技能已写入 / Skill saved: `%s` → `%s`", n.Name, rel)
	}
}

// formatSkillFeedbackBlock appends to the main assistant reply (mid-run updates in the same bubble).
// formatMemoryLearnerLine is a one-line notice after autonomous USER/NOTE maintenance.
func formatMemoryLearnerLine(target, action string) string {
	file := "memory/NOTE.md"
	if target == "user" {
		file = "memory/USER.md"
	}
	return fmt.Sprintf("🧠 记忆已更新 / Memory updated: target=%s action=%s → `%s`", target, action, file)
}

func formatPendingLearnerDigest(lines []string) string {
	if len(lines) == 0 {
		return ""
	}
	var b strings.Builder
	b.WriteString("📚 **学习反馈 / Learner digest** (since your last reply)\n\n")
	for _, ln := range lines {
		b.WriteString("- ")
		b.WriteString(ln)
		b.WriteString("\n")
	}
	b.WriteString("\n—\n\n")
	return b.String()
}

func formatSkillFeedbackBlock(ns []*SkillApplyNotice) string {
	if len(ns) == 0 {
		return ""
	}
	var b strings.Builder
	b.WriteString("\n\n---\n")
	b.WriteString("📚 **技能 / Skills** ")
	for i, n := range ns {
		if i > 0 {
			b.WriteString(" · ")
		}
		switch n.Action {
		case "create":
			fmt.Fprintf(&b, "新建 `%s`", n.Name)
		case "patch":
			fmt.Fprintf(&b, "更新 `%s`", n.Name)
		default:
			fmt.Fprintf(&b, "`%s`", n.Name)
		}
		if n.MidRun {
			b.WriteString(" (执行期)")
		}
	}
	b.WriteString("\n")
	return b.String()
}
