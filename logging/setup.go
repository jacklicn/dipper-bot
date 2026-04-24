package logging

import (
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/jacklicn/dipper-bot/config"
	"gopkg.in/natefinch/lumberjack.v2"
)

var rotateMu sync.Mutex
var activeRotate *lumberjack.Logger

func setActiveRotate(l *lumberjack.Logger) {
	rotateMu.Lock()
	defer rotateMu.Unlock()
	if activeRotate != nil && activeRotate != l {
		_ = activeRotate.Close()
	}
	activeRotate = l
}

// Close releases the rotating log file handle (e.g. tests or process re-init).
func Close() {
	setActiveRotate(nil)
}

func parseLevel(s string) slog.Level {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "debug":
		return slog.LevelDebug
	case "warn", "warning":
		return slog.LevelWarn
	case "error":
		return slog.LevelError
	case "info":
		return slog.LevelInfo
	default:
		return slog.LevelInfo
	}
}

// safeLogBasename returns a single filename segment for the log file (no path traversal).
func safeLogBasename(name string) string {
	name = strings.TrimSpace(name)
	if name == "" {
		return "dipper-bot.log"
	}
	name = filepath.ToSlash(name)
	if strings.Contains(name, "..") {
		return "dipper-bot.log"
	}
	base := filepath.Base(name)
	if base == "." || base == "/" || base == "" {
		return "dipper-bot.log"
	}
	return base
}

// SetupDefault configures the process-wide slog default handler from cfg.Logging and workspace.
// workspaceAbs must be an absolute expanded workspace directory. When file logging is disabled
// or workspace is empty, logs go to stderr only. forceDebug lowers the level to debug (e.g. gateway --verbose).
func SetupDefault(cfg *config.Config, workspaceAbs string, forceDebug bool) error {
	setActiveRotate(nil)

	if cfg == nil {
		slog.SetDefault(slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelInfo})))
		return nil
	}
	lc := cfg.Logging
	level := parseLevel(lc.Level)
	if forceDebug {
		level = slog.LevelDebug
	}
	opts := &slog.HandlerOptions{Level: level}

	if strings.TrimSpace(workspaceAbs) == "" || !lc.FileLoggingEnabled() {
		slog.SetDefault(slog.New(slog.NewTextHandler(os.Stderr, opts)))
		return nil
	}

	logDirName := strings.TrimSpace(lc.Dir)
	if logDirName == "" {
		logDirName = "logs"
	}
	logDir := filepath.Join(workspaceAbs, logDirName)
	if err := os.MkdirAll(logDir, 0o750); err != nil {
		slog.SetDefault(slog.New(slog.NewTextHandler(os.Stderr, opts)))
		return fmt.Errorf("logging: mkdir %q: %w", logDir, err)
	}

	maxAge := lc.MaxAgeDays
	if maxAge <= 0 {
		maxAge = 7
	}
	maxSize := lc.MaxSizeMB
	if maxSize <= 0 {
		maxSize = 128
	}

	lj := &lumberjack.Logger{
		Filename:   filepath.Join(logDir, safeLogBasename(lc.Filename)),
		MaxSize:    maxSize,
		MaxBackups: lc.MaxBackups,
		MaxAge:     maxAge,
		Compress:   lc.Compress,
		LocalTime:  true,
	}

	setActiveRotate(lj)

	var out io.Writer = lj
	if !lc.FileOnly {
		out = io.MultiWriter(os.Stderr, lj)
	}
	slog.SetDefault(slog.New(slog.NewTextHandler(out, opts)))
	return nil
}
