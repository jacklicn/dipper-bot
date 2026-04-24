package agent

import (
	"testing"

	"github.com/jacklicn/dipper-bot/config"
)

func boolPtr(v bool) *bool { return &v }

func TestPendingLearnerQueueAppendTake(t *testing.T) {
	l := &AgentLoop{pendingLearnLines: make(map[string][]string)}
	l.appendPendingLearnerLine("cli:1", "line-a")
	l.appendPendingLearnerLine("cli:1", "line-b")
	got := l.takePendingLearnerBlock("cli:1")
	if got == "" || len(got) < 20 {
		t.Fatalf("expected digest, got %q", got)
	}
	if l.takePendingLearnerBlock("cli:1") != "" {
		t.Fatal("second take should be empty")
	}
}

func TestPendingLearnerDedupeConsecutive(t *testing.T) {
	l := &AgentLoop{pendingLearnLines: make(map[string][]string)}
	l.appendPendingLearnerLine("k", "same")
	l.appendPendingLearnerLine("k", "same")
	if n := len(l.pendingLearnLines["k"]); n != 1 {
		t.Fatalf("want 1 line after dedupe, got %d", n)
	}
}

func TestRecordLearnerFeedback_DefaultInstantSkipsQueue(t *testing.T) {
	l := &AgentLoop{pendingLearnLines: make(map[string][]string)}
	l.recordLearnerFeedback("cli:1", "line-a")
	if n := len(l.pendingLearnLines["cli:1"]); n != 0 {
		t.Fatalf("want no queued line in default instant mode, got %d", n)
	}
}

func TestRecordLearnerFeedback_DigestModeQueues(t *testing.T) {
	l := &AgentLoop{
		exp:               config.AgentExperienceConfig{LearnerFeedbackInstantPush: boolPtr(false)},
		pendingLearnLines: make(map[string][]string),
	}
	l.recordLearnerFeedback("cli:1", "line-a")
	if n := len(l.pendingLearnLines["cli:1"]); n != 1 {
		t.Fatalf("want queued line in digest mode, got %d", n)
	}
}
