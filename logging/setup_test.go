package logging

import (
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"testing"

	"github.com/jacklicn/dipper-bot/config"
)

func TestParseLevel(t *testing.T) {
	tests := []struct {
		in   string
		want slog.Level
	}{
		{"debug", slog.LevelDebug},
		{"info", slog.LevelInfo},
		{"warn", slog.LevelWarn},
		{"error", slog.LevelError},
		{"", slog.LevelInfo},
		{"unknown", slog.LevelInfo},
	}
	for _, tc := range tests {
		if g := parseLevel(tc.in); g != tc.want {
			t.Errorf("parseLevel(%q) = %v, want %v", tc.in, g, tc.want)
		}
	}
}

func TestSafeLogBasename(t *testing.T) {
	if g := safeLogBasename("../../../etc/passwd"); g != "dipper-bot.log" {
		t.Fatalf("got %q", g)
	}
	if g := safeLogBasename("app.log"); g != "app.log" {
		t.Fatalf("got %q", g)
	}
}

func TestSetupDefault_FileCreatesLogDir(t *testing.T) {
	dir := t.TempDir()
	on := true
	cfg := &config.Config{
		Logging: config.LoggingConfig{
			Enabled:    &on,
			Level:      "info",
			MaxAgeDays: 7,
			MaxSizeMB:  1,
			Dir:        "logs",
			Filename:   "test.log",
			FileOnly:   true,
		},
	}
	if err := SetupDefault(cfg, dir, false); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		slog.SetDefault(slog.New(slog.NewTextHandler(io.Discard, &slog.HandlerOptions{Level: slog.LevelError})))
		Close()
	})
	slog.Info("hello from test")
	_, err := os.Stat(filepath.Join(dir, "logs", "test.log"))
	if err != nil {
		t.Fatal(err)
	}
}
