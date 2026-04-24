package agent

import "testing"

func TestChannelChatFromRouteKey(t *testing.T) {
	ch, id := channelChatFromRouteKey("telegram:12345")
	if ch != "telegram" || id != "12345" {
		t.Fatalf("got %q %q", ch, id)
	}
}

func TestFormatSkillApplyNoticeLine(t *testing.T) {
	s := formatSkillApplyNoticeLine(SkillApplyNotice{Action: "patch", Name: "foo-bar"})
	if s == "" || len(s) < 10 {
		t.Fatal("expected non-empty line")
	}
}

func TestFormatSkillFeedbackBlock(t *testing.T) {
	b := formatSkillFeedbackBlock([]*SkillApplyNotice{
		{Action: "create", Name: "a", MidRun: true},
		{Action: "patch", Name: "b", MidRun: false},
	})
	if b == "" || len(b) < 20 {
		t.Fatal("expected block")
	}
}
