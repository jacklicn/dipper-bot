package tools

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"time"
)

// ExecTool runs shell commands.
type ExecTool struct {
	Timeout             time.Duration
	WorkingDir          string
	RestrictToWorkspace bool
	denyPatterns        []*regexp.Regexp
}

// NewExecTool creates an exec tool.
func NewExecTool(workingDir string, timeoutSec int, restrictToWorkspace bool) *ExecTool {
	patterns := []string{
		`\brm\s+-[rf]{1,2}\b`,
		`\bdel\s+/[fq]\b`,
		`\brmdir\s+/s\b`,
		`\b(format|mkfs|diskpart)\b`,
		`\bdd\s+if=`,
		`>\s*/dev/sd`,
		`\b(shutdown|reboot|poweroff)\b`,
		`:\(\)\s*\{.*\};\s*:`,
		// Block common one-line remote code execution patterns.
		`(curl|wget)[^|;\n\r]*\|\s*(sh|bash|zsh|ksh)\b`,
		`(curl|wget)[^|;\n\r]*\|\s*(python|python3|node|perl|ruby)\b`,
		`(iwr|invoke-webrequest)\b[^|;\n\r]*\|\s*(iex|invoke-expression)\b`,
		`\biex\b[^;\n\r]*\(\s*new-object\s+net\.webclient\)\.downloadstring\(`,
	}
	var res []*regexp.Regexp
	for _, p := range patterns {
		if re, err := regexp.Compile(p); err == nil {
			res = append(res, re)
		}
	}
	to := time.Duration(timeoutSec) * time.Second
	if to == 0 {
		to = 60 * time.Second
	}
	return &ExecTool{Timeout: to, WorkingDir: workingDir, RestrictToWorkspace: restrictToWorkspace, denyPatterns: res}
}

func (e *ExecTool) Name() string { return "exec" }

func (e *ExecTool) Description() string {
	return "Execute a shell command and return its output. Python commands (python/python3) run in workspace .venv when in workspace. Use with caution."
}

func (e *ExecTool) Parameters() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"command":     map[string]any{"type": "string", "description": "The shell command to execute"},
			"working_dir": map[string]any{"type": "string", "description": "Optional working directory"},
		},
		"required": []any{"command"},
	}
}

func (e *ExecTool) Execute(ctx context.Context, params map[string]any) (string, error) {
	cmd, _ := params["command"].(string)
	if cmd == "" {
		return "Error: command is required", nil
	}
	workDir, _ := params["working_dir"].(string)
	if workDir == "" {
		workDir = e.WorkingDir
	}
	if workDir == "" {
		workDir = "."
	}
	workDir = e.defaultPythonWorkingDir(cmd, workDir)

	if err := e.guardCommand(cmd, workDir); err != "" {
		return err, nil
	}

	// When running Python in workspace, use workspace .venv
	cmd = e.maybeUseVenv(cmd, workDir)

	var c *exec.Cmd
	if runtime.GOOS == "windows" {
		c = exec.CommandContext(ctx, "cmd", "/c", cmd)
	} else {
		c = exec.CommandContext(ctx, "sh", "-c", cmd)
	}
	c.Dir = workDir
	out, err := runWithTimeout(ctx, c, e.Timeout)
	if err != nil {
		return "Error: " + err.Error(), nil
	}
	const maxLen = 10000
	if len(out) > maxLen {
		out = out[:maxLen] + "\n... (truncated)"
	}
	return out, nil
}

func (e *ExecTool) defaultPythonWorkingDir(cmd, baseDir string) string {
	trimmed := strings.TrimSpace(cmd)
	lower := strings.ToLower(trimmed)
	if !(strings.HasPrefix(lower, "python ") || strings.HasPrefix(lower, "python3 ")) {
		return baseDir
	}
	if strings.TrimSpace(baseDir) == "" {
		baseDir = "."
	}
	// Run Python from the workspace root so paths like `python scripts/foo.py` and imports resolve
	// correctly. Scripts saved under outputs/ should be invoked as `python outputs/foo.py`.
	outputsDir := filepath.Join(baseDir, outputsDirName)
	_ = os.MkdirAll(outputsDir, 0o750)
	return baseDir
}

func (e *ExecTool) guardCommand(cmd, cwd string) string {
	lower := strings.ToLower(strings.TrimSpace(cmd))
	for _, re := range e.denyPatterns {
		if re.MatchString(lower) {
			return "Error: Command blocked by safety guard"
		}
	}
	if e.RestrictToWorkspace && e.WorkingDir != "" {
		absCwd, _ := filepath.Abs(cwd)
		absWs, _ := filepath.Abs(e.WorkingDir)
		sep := string(filepath.Separator)
		if absCwd != absWs && !strings.HasPrefix(absCwd, absWs+sep) {
			return "Error: Command blocked (working_dir outside workspace)"
		}
	}
	return ""
}

// maybeUseVenv rewrites Python commands to use workspace/.venv when applicable.
// Scripts created in workspace are executed in the venv.
func (e *ExecTool) maybeUseVenv(cmd, workDir string) string {
	trimmed := strings.TrimSpace(cmd)
	lower := strings.ToLower(trimmed)
	var pyPrefix string
	if strings.HasPrefix(lower, "python3 ") {
		pyPrefix = "python3 "
	} else if strings.HasPrefix(lower, "python ") {
		pyPrefix = "python "
	} else {
		return cmd
	}
	if e.WorkingDir == "" {
		return cmd
	}
	absWork, _ := filepath.Abs(workDir)
	absWs, _ := filepath.Abs(e.WorkingDir)
	sep := string(filepath.Separator)
	inWorkspace := absWork == absWs || strings.HasPrefix(absWork, absWs+sep)
	if !inWorkspace {
		return cmd
	}
	venvDir := filepath.Join(absWs, ".venv")
	pyPath := filepath.Join(venvDir, "bin", "python3")
	if runtime.GOOS == "windows" {
		pyPath = filepath.Join(venvDir, "Scripts", "python.exe")
	}
	if _, err := os.Stat(pyPath); err != nil {
		if err := ensureWorkspaceVenv(absWs); err != nil {
			return cmd
		}
	}
	rest := trimmed[len(pyPrefix):]
	return pyPath + " " + rest
}

func ensureWorkspaceVenv(workspace string) error {
	venvDir := filepath.Join(workspace, ".venv")
	if _, err := os.Stat(filepath.Join(venvDir, "pyvenv.cfg")); err == nil {
		return nil
	}
	if err := os.MkdirAll(workspace, 0o750); err != nil {
		return err
	}
	py, err := exec.LookPath("python3")
	if err != nil {
		py, err = exec.LookPath("python")
		if err != nil {
			return err
		}
	}
	c := exec.Command(py, "-m", "venv", venvDir)
	c.Dir = workspace
	if _, err := c.CombinedOutput(); err != nil {
		return err
	}
	return nil
}

// workspaceVenvPython returns the python executable in workspace/.venv, creating the venv if missing.
func workspaceVenvPython(workspace string) (string, error) {
	if strings.TrimSpace(workspace) == "" {
		return "", errors.New("workspace is empty")
	}
	absWs, err := filepath.Abs(workspace)
	if err != nil {
		return "", err
	}
	venvDir := filepath.Join(absWs, ".venv")
	pyPath := filepath.Join(venvDir, "bin", "python3")
	if runtime.GOOS == "windows" {
		pyPath = filepath.Join(venvDir, "Scripts", "python.exe")
	}
	if _, err := os.Stat(pyPath); err != nil {
		if err := ensureWorkspaceVenv(absWs); err != nil {
			return "", fmt.Errorf("create venv: %w", err)
		}
	}
	if _, err := os.Stat(pyPath); err != nil {
		return "", fmt.Errorf("python not found in workspace .venv: %w", err)
	}
	return pyPath, nil
}

func runWithTimeout(ctx context.Context, c *exec.Cmd, timeout time.Duration) (string, error) {
	var stdout, stderr bytes.Buffer
	c.Stdout = &stdout
	c.Stderr = &stderr
	if err := c.Start(); err != nil {
		return "", err
	}
	done := make(chan error, 1)
	go func() { done <- c.Wait() }()
	timer := time.NewTimer(timeout)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		_ = c.Process.Kill()
		return stdout.String() + stderr.String(), ctx.Err()
	case err := <-done:
		out := stdout.String()
		if stderr.Len() > 0 {
			out += "\nSTDERR:\n" + stderr.String()
		}
		if err != nil {
			out += "\nExit code: " + err.Error()
		}
		return out, nil
	case <-timer.C:
		_ = c.Process.Kill()
		return stdout.String() + stderr.String() + "\nError: command timed out", context.DeadlineExceeded
	}
}
