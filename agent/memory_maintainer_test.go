package agent

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/jacklicn/dipper-bot/config"
	"github.com/jacklicn/dipper-bot/providers"
	"github.com/jacklicn/dipper-bot/session"
	"github.com/jacklicn/dipper-bot/tools"
)

func TestLatestTurnPair(t *testing.T) {
	sess := &session.Session{
		Key:       "k",
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
	}
	sess.AddMessage("user", "first", nil)
	sess.AddMessage("assistant", "reply-1", nil)
	sess.AddMessage("user", "second", nil)
	sess.AddMessage("assistant", "reply-2", nil)

	u, a, ok := latestTurnPair(sess)
	if !ok {
		t.Fatal("expected latest pair")
	}
	if u != "second" || a != "reply-2" {
		t.Fatalf("latest pair mismatch: user=%q asst=%q", u, a)
	}
}

func TestCountUserTurns(t *testing.T) {
	sess := &session.Session{
		Key:       "k",
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
	}
	sess.AddMessage("user", "u1", nil)
	sess.AddMessage("assistant", "a1", nil)
	sess.AddMessage("user", "u2", nil)
	sess.AddMessage("assistant", "a2", nil)

	if n := countUserTurns(sess); n != 2 {
		t.Fatalf("countUserTurns=%d want=2", n)
	}
}

type fakeProvider struct {
	content string
}

func (f *fakeProvider) Chat(ctx context.Context, req *providers.ChatRequest) (*providers.LLMResponse, error) {
	return &providers.LLMResponse{Content: f.content}, nil
}

func (f *fakeProvider) GetDefaultModel() string { return "fake" }

func TestFlushFromSession_WritesMemory(t *testing.T) {
	workspace := t.TempDir()
	store := &tools.MemoryNoteStore{Workspace: workspace}
	p := &fakeProvider{content: "NOOP: no\nTARGET: memory\nACTION: add\nCONTENT: always use utf8 encoding\nOLD_TEXT:"}
	memOn := true
	m := NewMemoryMaintainer(p, "fake-model", store, config.MemoryMaintenanceConfig{
		Enabled:           &memOn,
		QueueSize:         8,
		MinUserChars:      1,
		MinAssistantChars: 1,
		NudgeInterval:     1,
		FlushMinTurns:     1,
	}, nil)
	if m == nil {
		t.Fatal("expected maintainer")
	}

	sess := &session.Session{
		Key:       "k",
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
	}
	sess.AddMessage("user", "remember this", nil)
	sess.AddMessage("assistant", "ok", nil)

	m.FlushFromSession("k", sess)

	notePath := filepath.Join(workspace, "memory", "NOTE.md")
	b, err := os.ReadFile(notePath)
	if err != nil {
		t.Fatalf("ReadFile NOTE.md: %v", err)
	}
	if string(b) == "" {
		t.Fatal("expected NOTE.md to be written")
	}
}

func TestScoreMemoryCandidate(t *testing.T) {
	if got := scoreMemoryCandidate("add", "always prefer utf8 output in docs", ""); got < 50 {
		t.Fatalf("expected score >= 50, got %d", got)
	}
	if got := scoreMemoryCandidate("add", "ok", ""); got != 0 {
		t.Fatalf("expected noise score 0, got %d", got)
	}
}

func TestTokenJaccard(t *testing.T) {
	a := "always use utf8 in docs output"
	b := "always use utf8 output in docs"
	if tokenJaccard(a, b) < 0.9 {
		t.Fatalf("expected high similarity")
	}
}

func TestParseMemoryReview_WithConfidence(t *testing.T) {
	raw := "NOOP: no\nCONFIDENCE: 65\nTARGET: user\nACTION: add\nCONTENT: prefers concise answers\nOLD_TEXT:"
	noop, confidence, target, action, content, oldText := parseMemoryReview(raw)
	if noop {
		t.Fatal("expected noop=false")
	}
	if confidence != 65 {
		t.Fatalf("confidence mismatch: %d", confidence)
	}
	if target != "user" || action != "add" || content == "" || oldText != "" {
		t.Fatalf("unexpected parse values: target=%q action=%q content=%q old=%q", target, action, content, oldText)
	}
}

