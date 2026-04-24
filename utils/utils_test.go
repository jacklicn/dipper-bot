package utils_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/jacklicn/dipper-bot/utils"
)

func TestEnsureDir(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "a", "b", "c")
	got, err := utils.EnsureDir(dir)
	if err != nil {
		t.Fatalf("EnsureDir: %v", err)
	}
	if got != dir {
		t.Errorf("EnsureDir returned %q, want %q", got, dir)
	}
	if _, err := os.Stat(dir); os.IsNotExist(err) {
		t.Error("directory was not created")
	}
}

func TestSafeFilename(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{"empty", "", "upload"},
		{"safe", "hello", "hello"},
		{"colon", "a:b", "a_b"},
		{"path", `a/b\c`, "a_b_c"},
		{"quotes", `a"b<c>d`, "a_b_c_d"},
		{"trim", "  x  ", "x"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := utils.SafeFilename(tt.input)
			if got != tt.want {
				t.Errorf("SafeFilename(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

func TestGetDataPath(t *testing.T) {
	home := t.TempDir()
	restore := setEnv("HOME", home)
	defer restore()
	path, err := utils.GetDataPath()
	if err != nil {
		t.Fatalf("GetDataPath: %v", err)
	}
	if path == "" {
		t.Error("GetDataPath returned empty")
	}
	if _, err := os.Stat(path); os.IsNotExist(err) {
		t.Errorf("GetDataPath %q should exist after call", path)
	}
}

func TestGetWorkspacePath_Empty(t *testing.T) {
	home := t.TempDir()
	restore := setEnv("HOME", home)
	defer restore()
	path, err := utils.GetWorkspacePath("")
	if err != nil {
		t.Fatalf("GetWorkspacePath: %v", err)
	}
	if path == "" {
		t.Error("GetWorkspacePath returned empty")
	}
}

func setEnv(key, value string) func() {
	old := os.Getenv(key)
	os.Setenv(key, value)
	return func() {
		if old == "" {
			os.Unsetenv(key)
		} else {
			os.Setenv(key, old)
		}
	}
}

func TestGetWorkspacePath_Custom(t *testing.T) {
	dir := t.TempDir()
	path, err := utils.GetWorkspacePath(dir)
	if err != nil {
		t.Fatalf("GetWorkspacePath: %v", err)
	}
	if path != dir {
		t.Errorf("GetWorkspacePath = %q, want %q", path, dir)
	}
}
