package agent

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestTopicForEntry(t *testing.T) {
	if got := topicForEntry("prefer concise tone in replies"); got != "preferences" {
		t.Fatalf("unexpected topic: %s", got)
	}
	if got := topicForEntry("fix error when reading file path"); got != "workflow" && got != "troubleshooting" {
		t.Fatalf("unexpected topic for mixed sentence: %s", got)
	}
}

func TestCompressOldArchives(t *testing.T) {
	workspace := t.TempDir()
	dir := filepath.Join(workspace, "memory", "archive")
	if err := os.MkdirAll(dir, 0o750); err != nil {
		t.Fatal(err)
	}
	old := filepath.Join(dir, "sessions-old.json")
	if err := os.WriteFile(old, []byte(`{"ok":true}`), 0o600); err != nil {
		t.Fatal(err)
	}
	oldTime := time.Now().Add(-10 * 24 * time.Hour)
	if err := os.Chtimes(old, oldTime, oldTime); err != nil {
		t.Fatal(err)
	}
	g := &LearningGovernance{workspace: workspace}
	g.compressOldArchives(7 * 24 * time.Hour)
	if _, err := os.Stat(old + ".gz"); err != nil {
		t.Fatalf("expected gz archive: %v", err)
	}
}

