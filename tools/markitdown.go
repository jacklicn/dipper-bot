package tools

import (
	"context"
	"errors"
	"fmt"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// Extensions we convert with MarkItDown instead of returning raw bytes to the LLM.
var markitdownExtensions = map[string]struct{}{
	".pdf":  {},
	".docx": {},
	".doc":  {},
	".xlsx": {},
	".xls":  {},
	".pptx": {},
	".ppt":  {},
}

func needsMarkitdownConversion(absPath string) bool {
	ext := strings.ToLower(filepath.Ext(absPath))
	_, ok := markitdownExtensions[ext]
	return ok
}

const (
	markitdownRunTimeout = 3 * time.Minute
	maxMarkitdownChars   = 1_500_000
)

var (
	resolveWorkspacePython = workspaceVenvPython
	runMarkitdownCommand   = defaultRunMarkitdownCommand
)

func defaultRunMarkitdownCommand(ctx context.Context, py, absPath, workspaceDir string) (string, error) {
	cmd := exec.CommandContext(ctx, py, "-m", "markitdown", absPath)
	cmd.Dir = workspaceDir
	out, err := cmd.CombinedOutput()
	return strings.TrimSpace(string(out)), err
}

// runMarkItDownBody runs `python -m markitdown` and returns Markdown body (no title prefix).
func runMarkItDownBody(ctx context.Context, absPath, workspaceDir string) (string, error) {
	py, err := resolveWorkspacePython(workspaceDir)
	if err != nil {
		return "", err
	}
	runCtx, cancel := context.WithTimeout(ctx, markitdownRunTimeout)
	defer cancel()
	s, err := runMarkitdownCommand(runCtx, py, absPath, workspaceDir)
	if err != nil {
		if errors.Is(runCtx.Err(), context.DeadlineExceeded) {
			return "", fmt.Errorf("markitdown timed out after %v", markitdownRunTimeout)
		}
		hint := "install MarkItDown in the workspace venv: exec(command=\"python -m pip install 'markitdown[all]'\")"
		if strings.Contains(s, "No module named 'markitdown'") || strings.Contains(s, "No module named markitdown") {
			return "", fmt.Errorf("markitdown is not installed; %s", hint)
		}
		if s != "" {
			return "", fmt.Errorf("markitdown failed: %w\n%s", err, s)
		}
		return "", fmt.Errorf("markitdown failed: %w; %s", err, hint)
	}
	if s == "" {
		return "", errors.New("markitdown produced empty output (unsupported format or empty document?)")
	}
	if len(s) > maxMarkitdownChars {
		s = s[:maxMarkitdownChars] + "\n\n... (truncated: output exceeded read_file limit for converted documents)"
	}
	return s, nil
}

// readFileViaMarkitdown runs MarkItDown and returns text suitable for the read_file tool result.
func readFileViaMarkitdown(ctx context.Context, absPath, workspaceDir string) (string, error) {
	body, err := runMarkItDownBody(ctx, absPath, workspaceDir)
	if err != nil {
		return "", err
	}
	return "# Converted to Markdown (MarkItDown)\n\n" + body, nil
}
