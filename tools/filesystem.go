package tools

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"unicode/utf8"

	"github.com/jacklicn/dipper-bot/utils"
)

const outputsDirName = "outputs"

// resolvePath joins relative paths with workspaceDir when set, then validates against allowedDir.
func resolvePath(path, workspaceDir, allowedDir string) (string, error) {
	path = filepath.Clean(path)
	if workspaceDir != "" && !filepath.IsAbs(path) {
		path = filepath.Join(workspaceDir, path)
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		return "", err
	}
	if allowedDir != "" {
		allowed, _ := filepath.Abs(allowedDir)
		if abs != allowed && !strings.HasPrefix(abs, allowed+string(filepath.Separator)) {
			return "", os.ErrPermission
		}
	}
	return abs, nil
}

func normalizePythonWritePath(path, workspaceDir string) string {
	if strings.TrimSpace(path) == "" || strings.TrimSpace(workspaceDir) == "" {
		return path
	}
	if strings.ToLower(filepath.Ext(path)) != ".py" {
		return path
	}
	cleanPath := filepath.Clean(path)
	// Keep Python files already targeting outputs/.
	if !filepath.IsAbs(cleanPath) {
		if cleanPath == outputsDirName || strings.HasPrefix(cleanPath, outputsDirName+string(filepath.Separator)) {
			return cleanPath
		}
		return filepath.Join(outputsDirName, filepath.Base(cleanPath))
	}
	absWs, err := filepath.Abs(workspaceDir)
	if err != nil {
		return path
	}
	rel, err := filepath.Rel(absWs, cleanPath)
	if err != nil {
		return path
	}
	// Keep absolute paths outside workspace untouched for safety guard to handle.
	if rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return path
	}
	if rel == outputsDirName || strings.HasPrefix(rel, outputsDirName+string(filepath.Separator)) {
		return path
	}
	return filepath.Join(absWs, outputsDirName, filepath.Base(cleanPath))
}

// ReadFileTool reads a file.
type ReadFileTool struct {
	WorkspaceDir string // Base for resolving relative paths
	AllowedDir   string // When set, path must be under this dir
}

func (r *ReadFileTool) Name() string { return "read_file" }

func (r *ReadFileTool) Description() string {
	return "Read the contents of a file. Use path relative to workspace or absolute if allowed. " +
		"PDF and Office files (.pdf, .doc/.docx, .xls/.xlsx, .ppt/.pptx) are converted to Markdown via MarkItDown in workspace .venv (install: pip install 'markitdown[all]')."
}

func (r *ReadFileTool) Parameters() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"path": map[string]any{"type": "string", "description": "File path to read"},
		},
		"required": []any{"path"},
	}
}

func (r *ReadFileTool) Execute(ctx context.Context, params map[string]any) (string, error) {
	path, _ := params["path"].(string)
	if path == "" {
		return "Error: path is required", nil
	}
	resolved, err := resolvePath(path, r.WorkspaceDir, r.AllowedDir)
	if err != nil {
		if err == os.ErrPermission {
			return "Error: path outside allowed directory", nil
		}
		return "Error: " + err.Error(), nil
	}

	finalPath, statErr := r.resolveExistingFilePath(resolved)
	if statErr != nil {
		return "Error: " + statErr.Error(), nil
	}

	if needsMarkitdownConversion(finalPath) && strings.TrimSpace(r.WorkspaceDir) != "" {
		out, convErr := readFileViaMarkitdown(ctx, finalPath, r.WorkspaceDir)
		if convErr != nil {
			return "Error: " + convErr.Error(), nil
		}
		return out, nil
	}

	data, err := os.ReadFile(finalPath)
	if err != nil {
		return "Error: " + err.Error(), nil
	}
	return string(data), nil
}

// resolveExistingFilePath returns a path to an existing regular file, retrying with a sanitized basename (SafeFilename) if needed.
func (r *ReadFileTool) resolveExistingFilePath(resolved string) (string, error) {
	if fi, err := os.Stat(resolved); err == nil {
		if fi.IsDir() {
			return "", fmt.Errorf("path is a directory")
		}
		return resolved, nil
	} else if !errors.Is(err, os.ErrNotExist) {
		return "", err
	}
	dir, base := filepath.Dir(resolved), filepath.Base(resolved)
	if safeBase := utils.SafeFilename(base); safeBase != base {
		alt := filepath.Join(dir, safeBase)
		if fi, err := os.Stat(alt); err == nil {
			if fi.IsDir() {
				return "", fmt.Errorf("path is a directory")
			}
			return alt, nil
		}
	}
	return "", os.ErrNotExist
}

// WriteFileTool writes a file.
type WriteFileTool struct {
	WorkspaceDir string
	AllowedDir   string
}

func (w *WriteFileTool) Name() string { return "write_file" }

func (w *WriteFileTool) Description() string {
	return "Write content to a file (creates parent dirs). For binary files (e.g. .docx, .xlsx, .pptx, PDF, images), set content_encoding to \"base64\" and pass standard Base64 in content; UTF-8 text in content corrupts Office ZIP packages. Prefer exec + Python libraries writing with open(..., \"wb\") for Office files."
}

func (w *WriteFileTool) Parameters() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"path": map[string]any{"type": "string", "description": "File path"},
			"content": map[string]any{
				"type":        "string",
				"description": "File body: UTF-8 text by default, or standard Base64 when content_encoding is base64",
			},
			"content_encoding": map[string]any{
				"type":        "string",
				"description": "Optional. Omit or \"text\" for UTF-8 text; use \"base64\" for binary (Office, PDF, images)",
				"enum":        []any{"text", "base64"},
			},
		},
		"required": []any{"path", "content"},
	}
}

func decodeWriteFilePayload(params map[string]any) ([]byte, string) {
	content, _ := params["content"].(string)
	enc, _ := params["content_encoding"].(string)
	switch strings.ToLower(strings.TrimSpace(enc)) {
	case "base64", "b64":
		s := strings.TrimSpace(content)
		s = strings.ReplaceAll(s, "\n", "")
		s = strings.ReplaceAll(s, "\r", "")
		s = strings.ReplaceAll(s, " ", "")
		var raw []byte
		var err error
		if raw, err = base64.StdEncoding.DecodeString(s); err == nil {
			return raw, ""
		}
		if raw, err = base64.RawStdEncoding.DecodeString(s); err == nil {
			return raw, ""
		}
		if raw, err = base64.URLEncoding.DecodeString(s); err == nil {
			return raw, ""
		}
		if raw, err = base64.RawURLEncoding.DecodeString(s); err == nil {
			return raw, ""
		}
		return nil, "Error: content is not valid Base64 (" + err.Error() + ")"
	default:
		if !utf8.ValidString(content) {
			return nil, "Error: content is not valid UTF-8; use content_encoding \"base64\" for binary files"
		}
		return []byte(content), ""
	}
}

func (w *WriteFileTool) Execute(ctx context.Context, params map[string]any) (string, error) {
	path, _ := params["path"].(string)
	if path == "" {
		return "Error: path is required", nil
	}
	path = normalizePythonWritePath(path, w.WorkspaceDir)
	resolved, err := resolvePath(path, w.WorkspaceDir, w.AllowedDir)
	if err != nil {
		if err == os.ErrPermission {
			return "Error: path outside allowed directory", nil
		}
		return "Error: " + err.Error(), nil
	}
	dir := filepath.Dir(resolved)
	if err := os.MkdirAll(dir, 0o750); err != nil {
		return "Error: " + err.Error(), nil
	}
	payload, errMsg := decodeWriteFilePayload(params)
	if errMsg != "" {
		return errMsg, nil
	}
	if err := os.WriteFile(resolved, payload, 0o600); err != nil {
		return "Error: " + err.Error(), nil
	}
	return "OK", nil
}

// EditFileTool edits a file by replacing old_text with new_text.
type EditFileTool struct {
	WorkspaceDir string
	AllowedDir   string
}

func (e *EditFileTool) Name() string { return "edit_file" }

func (e *EditFileTool) Description() string {
	return "Edit a file by replacing old_text with new_text. The old_text must exist exactly in the file."
}

func (e *EditFileTool) Parameters() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"path":     map[string]any{"type": "string", "description": "File path to edit"},
			"old_text": map[string]any{"type": "string", "description": "Exact text to find and replace"},
			"new_text": map[string]any{"type": "string", "description": "Text to replace with"},
		},
		"required": []any{"path", "old_text", "new_text"},
	}
}

func (e *EditFileTool) Execute(ctx context.Context, params map[string]any) (string, error) {
	path, _ := params["path"].(string)
	oldText, _ := params["old_text"].(string)
	newText, _ := params["new_text"].(string)
	if path == "" {
		return "Error: path is required", nil
	}
	resolved, err := resolvePath(path, e.WorkspaceDir, e.AllowedDir)
	if err != nil {
		if err == os.ErrPermission {
			return "Error: path outside allowed directory", nil
		}
		return "Error: " + err.Error(), nil
	}
	data, err := os.ReadFile(resolved)
	if err != nil && errors.Is(err, os.ErrNotExist) {
		dir, base := filepath.Dir(resolved), filepath.Base(resolved)
		if safeBase := utils.SafeFilename(base); safeBase != base {
			alt := filepath.Join(dir, safeBase)
			if data2, err2 := os.ReadFile(alt); err2 == nil {
				resolved = alt
				data = data2
				err = nil
			}
		}
	}
	if err != nil {
		return "Error: " + err.Error(), nil
	}
	content := string(data)
	if !strings.Contains(content, oldText) {
		return "Error: old_text not found in file. Make sure it matches exactly.", nil
	}
	count := strings.Count(content, oldText)
	if count > 1 {
		return "Warning: old_text appears multiple times. Please provide more context to make it unique.", nil
	}
	newContent := strings.Replace(content, oldText, newText, 1)
	if err := os.WriteFile(resolved, []byte(newContent), 0o600); err != nil {
		return "Error: " + err.Error(), nil
	}
	return "Successfully edited " + path, nil
}

// ListDirTool lists directory contents.
type ListDirTool struct {
	WorkspaceDir string
	AllowedDir   string
}

func (l *ListDirTool) Name() string { return "list_dir" }

func (l *ListDirTool) Description() string {
	return "List files and directories in a path."
}

func (l *ListDirTool) Parameters() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"path": map[string]any{"type": "string", "description": "Directory path"},
		},
		"required": []any{"path"},
	}
}

func (l *ListDirTool) Execute(ctx context.Context, params map[string]any) (string, error) {
	path, _ := params["path"].(string)
	if path == "" {
		path = "."
	}
	resolved, err := resolvePath(path, l.WorkspaceDir, l.AllowedDir)
	if err != nil {
		if err == os.ErrPermission {
			return "Error: path outside allowed directory", nil
		}
		return "Error: " + err.Error(), nil
	}
	entries, err := os.ReadDir(resolved)
	if err != nil {
		return "Error: " + err.Error(), nil
	}
	names := make([]string, 0, len(entries))
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() {
			name += "/"
		}
		names = append(names, name)
	}
	return strings.Join(names, "\n"), nil
}
