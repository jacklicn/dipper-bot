package session_test

import (
	"testing"

	"github.com/jacklicn/dipper-bot/session"
)

func TestSessionManager_GetOrCreateAndHistory(t *testing.T) {
	workspace := t.TempDir()

	mgr, err := session.NewSessionManager(workspace)
	if err != nil {
		t.Fatalf("NewSessionManager: %v", err)
	}

	sess, err := mgr.GetOrCreate("test:chat1")
	if err != nil {
		t.Fatalf("GetOrCreate: %v", err)
	}
	if sess.Key != "test:chat1" {
		t.Errorf("session Key = %q", sess.Key)
	}

	sess.AddMessage("user", "hello", nil)
	sess.AddMessage("assistant", "hi", nil)
	history := sess.GetHistory(10, false)
	if len(history) != 2 {
		t.Fatalf("GetHistory len = %d, want 2", len(history))
	}
	if history[0]["role"] != "user" || history[0]["content"] != "hello" {
		t.Errorf("history[0] = %v", history[0])
	}
	if history[1]["role"] != "assistant" || history[1]["content"] != "hi" {
		t.Errorf("history[1] = %v", history[1])
	}

	if err := mgr.Save(sess); err != nil {
		t.Fatalf("Save: %v", err)
	}
}

func TestSessionManager_GetOrCreateCached(t *testing.T) {
	workspace := t.TempDir()

	mgr, err := session.NewSessionManager(workspace)
	if err != nil {
		t.Fatalf("NewSessionManager: %v", err)
	}
	s1, _ := mgr.GetOrCreate("key1")
	s2, _ := mgr.GetOrCreate("key1")
	if s1 != s2 {
		t.Error("GetOrCreate should return same session for same key")
	}
}

func TestSession_Clear(t *testing.T) {
	workspace := t.TempDir()

	mgr, err := session.NewSessionManager(workspace)
	if err != nil {
		t.Fatalf("NewSessionManager: %v", err)
	}
	sess, _ := mgr.GetOrCreate("clear-test")
	sess.AddMessage("user", "x", nil)
	sess.Clear()
	history := sess.GetHistory(5, false)
	if len(history) != 0 {
		t.Errorf("after Clear, GetHistory len = %d", len(history))
	}
}

func TestSession_UserTurnsSinceMemoryTools(t *testing.T) {
	workspace := t.TempDir()
	mgr, err := session.NewSessionManager(workspace)
	if err != nil {
		t.Fatal(err)
	}
	s, _ := mgr.GetOrCreate("mem-turns")
	s.AddMessage("user", "a", nil)
	s.AddMessage("assistant", "b", []string{"memory"})
	s.AddMessage("user", "c", nil)
	s.AddMessage("user", "d", nil)
	if n := s.UserTurnsSinceMemoryTools(); n != 2 {
		t.Fatalf("UserTurnsSinceMemoryTools = %d, want 2", n)
	}
	s2, _ := mgr.GetOrCreate("no-memory")
	s2.AddMessage("user", "u1", nil)
	s2.AddMessage("assistant", "a1", nil)
	s2.AddMessage("user", "u2", nil)
	if n := s2.UserTurnsSinceMemoryTools(); n != 2 {
		t.Fatalf("no memory case: got %d want 2", n)
	}
}
