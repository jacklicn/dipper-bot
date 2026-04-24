package utils

import (
	"os"
	"path/filepath"
	"strings"
)

// EnsureDir creates the directory and parents if needed.
func EnsureDir(path string) (string, error) {
	return path, os.MkdirAll(path, 0o750)
}

// GetDataPath returns ~/.dipper-bot and ensures it exists.
func GetDataPath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return EnsureDir(filepath.Join(home, ".dipper-bot"))
}

// GetWorkspacePath returns the workspace path (default ~/.dipper-bot/workspace).
func GetWorkspacePath(workspace string) (string, error) {
	if workspace != "" {
		return EnsureDir(expandHome(workspace))
	}
	data, err := GetDataPath()
	if err != nil {
		return "", err
	}
	return EnsureDir(filepath.Join(data, "workspace"))
}

// GetSessionsPath returns ~/.dipper-bot/sessions.
func GetSessionsPath() (string, error) {
	data, err := GetDataPath()
	if err != nil {
		return "", err
	}
	return EnsureDir(filepath.Join(data, "sessions"))
}

// SafeFilename replaces unsafe characters for use in filenames.
// Handles Windows/Linux disallowed chars, Unicode quotes, and Chinese quotation marks.
func SafeFilename(name string) string {
	unsafe := `<>:"/\|?*` + `""''„‟‚‛‹›«»` + `「」『』＂＇` // ASCII + Unicode + Chinese quotes
	for _, c := range unsafe {
		name = strings.ReplaceAll(name, string(c), "_")
	}
	// Replace control chars
	var b strings.Builder
	for _, r := range name {
		if r < 32 || r == 127 {
			b.WriteRune('_')
		} else {
			b.WriteRune(r)
		}
	}
	name = strings.TrimSpace(strings.Trim(b.String(), "."))
	if name == "" {
		return "upload"
	}
	return name
}

func expandHome(p string) string {
	if p == "~" || strings.HasPrefix(p, "~/") {
		home, _ := os.UserHomeDir()
		if home == "" {
			return p
		}
		if p == "~" {
			return home
		}
		return filepath.Join(home, p[2:])
	}
	return p
}
