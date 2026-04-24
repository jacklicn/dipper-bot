package agent

import "testing"

func TestNormalizeSkillName(t *testing.T) {
	if got := normalizeSkillName("My Skill"); got != "my-skill" {
		t.Fatalf("normalizeSkillName mismatch: %q", got)
	}
	if got := normalizeSkillName("../bad"); got != "" {
		t.Fatalf("expected invalid name to be empty, got %q", got)
	}
}

func TestNormalizeSkillAction(t *testing.T) {
	if got := normalizeSkillAction("update"); got != "patch" {
		t.Fatalf("update should map to patch, got %q", got)
	}
	if got := normalizeSkillAction("create"); got != "create" {
		t.Fatalf("create mismatch: %q", got)
	}
}

func TestParseSkillReview(t *testing.T) {
	raw := "NOOP: no\nCONFIDENCE: 77\nACTION: create\nNAME: test-skill\nCONTENT: hello"
	noop, confidence, action, name, content, oldText, newText := parseSkillReview(raw)
	if noop {
		t.Fatal("expected noop=false")
	}
	if confidence != 77 {
		t.Fatalf("confidence mismatch: %d", confidence)
	}
	if action != "create" || name != "test-skill" || content != "hello" {
		t.Fatalf("unexpected parse result: action=%q name=%q content=%q", action, name, content)
	}
	if oldText != "" || newText != "" {
		t.Fatalf("unexpected patch fields: old=%q new=%q", oldText, newText)
	}
}

func TestAtoiClamp(t *testing.T) {
	if got := atoiClamp("x88%", 0, 100, 60); got != 88 {
		t.Fatalf("atoiClamp parse mismatch: %d", got)
	}
	if got := atoiClamp("", 0, 100, 60); got != 60 {
		t.Fatalf("atoiClamp default mismatch: %d", got)
	}
}

func TestScoreSkillCandidate(t *testing.T) {
	score := scoreSkillCandidate("create", "my-skill", "---\ndescription: x\n---\nbody body body body body body", []string{"read_file", "exec"})
	if score < 60 {
		t.Fatalf("expected high score, got %d", score)
	}
}

func TestSkillDecisionKeyStable(t *testing.T) {
	k1 := skillDecisionKey("s1", "create", "a-skill", "x", "", "")
	k2 := skillDecisionKey("s1", "create", "a-skill", "x", "", "")
	if k1 != k2 {
		t.Fatalf("decision key must be stable")
	}
}

