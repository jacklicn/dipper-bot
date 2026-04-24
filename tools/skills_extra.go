package tools

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

var skillNameRe = regexp.MustCompile(`^[a-z0-9][a-z0-9._-]*$`)

var skillSubdirsAllowed = map[string]struct{}{
	"references": {}, "templates": {}, "scripts": {}, "assets": {},
}

// SkillsListTool lists skill names under workspace/skills.
type SkillsListTool struct {
	Workspace string
}

func (t *SkillsListTool) Name() string { return "skills_list" }

func (t *SkillsListTool) Description() string {
	return "List available skills (directories under workspace/skills containing SKILL.md)."
}

func (t *SkillsListTool) Parameters() map[string]any {
	return map[string]any{
		"type":       "object",
		"properties": map[string]any{},
	}
}

func (t *SkillsListTool) Execute(ctx context.Context, params map[string]any) (string, error) {
	dir := filepath.Join(t.Workspace, "skills")
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return `{"success":true,"skills":[]}`, nil
		}
		return "", err
	}
	var names []string
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		if _, err := os.Stat(filepath.Join(dir, e.Name(), "SKILL.md")); err == nil {
			names = append(names, e.Name())
		}
	}
	b, _ := json.Marshal(map[string]any{"success": true, "skills": names})
	return string(b), nil
}

// SkillViewTool reads SKILL.md or a supporting file under a skill directory.
type SkillViewTool struct {
	Workspace string
}

func (t *SkillViewTool) Name() string { return "skill_view" }

func (t *SkillViewTool) Description() string {
	return `Read a skill's SKILL.md or a supporting file (references/, templates/, scripts/, assets/ only).
Parameters: name (skill directory name), optional file_path relative to skill dir.`
}

func (t *SkillViewTool) Parameters() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"name": map[string]any{
				"type":        "string",
				"description": "Skill directory name under workspace/skills",
			},
			"file_path": map[string]any{
				"type":        "string",
				"description": "Optional path like references/foo.md (omit for SKILL.md)",
			},
		},
		"required": []any{"name"},
	}
}

func (t *SkillViewTool) Execute(ctx context.Context, params map[string]any) (string, error) {
	name, _ := params["name"].(string)
	name = strings.TrimSpace(name)
	if name == "" || !skillNameRe.MatchString(name) {
		return `{"success":false,"error":"invalid skill name"}`, nil
	}
	base := filepath.Join(t.Workspace, "skills", name)
	if st, err := os.Stat(base); err != nil || !st.IsDir() {
		return `{"success":false,"error":"skill not found"}`, nil
	}
	fp, _ := params["file_path"].(string)
	fp = strings.TrimSpace(strings.ReplaceAll(fp, "\\", "/"))
	var rel string
	if fp == "" {
		rel = "SKILL.md"
	} else {
		parts := strings.Split(fp, "/")
		if len(parts) < 2 || parts[0] == "" || parts[1] == "" {
			return `{"success":false,"error":"file_path must be like references/file.md"}`, nil
		}
		if _, ok := skillSubdirsAllowed[parts[0]]; !ok {
			return `{"success":false,"error":"file_path must start with references/, templates/, scripts/, or assets/"}`, nil
		}
		rel = filepath.FromSlash(fp)
	}
	full := filepath.Join(base, rel)
	full = filepath.Clean(full)
	if !strings.HasPrefix(full, filepath.Clean(base)+string(filepath.Separator)) && full != filepath.Join(base, "SKILL.md") {
		return `{"success":false,"error":"path escapes skill directory"}`, nil
	}
	b, err := os.ReadFile(full)
	if err != nil {
		return `{"success":false,"error":"file not found"}`, nil
	}
	out, _ := json.Marshal(map[string]any{
		"success": true,
		"path":    rel,
		"content": string(b),
	})
	return string(out), nil
}

// SkillManageTool creates/patches/edits/deletes skills under workspace/skills.
type SkillManageTool struct {
	Workspace string
}

func (t *SkillManageTool) Name() string { return "skill_manage" }

func (t *SkillManageTool) Description() string {
	return `Manage procedural skills. Actions: create, patch, edit, delete.
create: name + content (full SKILL.md with YAML frontmatter).
patch: name + old_string + new_string (unique match in SKILL.md).
edit: name + content (full replace SKILL.md).
delete: remove skill directory.`
}

func (t *SkillManageTool) Parameters() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"action": map[string]any{
				"type": "string",
				"enum": []any{"create", "patch", "edit", "delete"},
			},
			"name": map[string]any{"type": "string"},
			"content": map[string]any{
				"type": "string",
			},
			"old_string": map[string]any{"type": "string"},
			"new_string": map[string]any{"type": "string"},
		},
		"required": []any{"action", "name"},
	}
}

func (t *SkillManageTool) Execute(ctx context.Context, params map[string]any) (string, error) {
	action, _ := params["action"].(string)
	name, _ := params["name"].(string)
	name = strings.TrimSpace(name)
	if !skillNameRe.MatchString(name) {
		return `{"success":false,"error":"invalid skill name"}`, nil
	}
	base := filepath.Join(t.Workspace, "skills", name)
	switch action {
	case "create":
		content, _ := params["content"].(string)
		if strings.TrimSpace(content) == "" {
			return `{"success":false,"error":"content required"}`, nil
		}
		if err := os.MkdirAll(base, 0o750); err != nil {
			return "", err
		}
		p := filepath.Join(base, "SKILL.md")
		if _, err := os.Stat(p); err == nil {
			return `{"success":false,"error":"skill already exists"}`, nil
		}
		if err := os.WriteFile(p, []byte(content), 0o600); err != nil {
			return "", err
		}
		b, _ := json.Marshal(map[string]any{"success": true, "message": "skill created", "path": "skills/" + name})
		return string(b), nil
	case "patch":
		oldS, _ := params["old_string"].(string)
		newS, _ := params["new_string"].(string)
		if oldS == "" {
			return `{"success":false,"error":"old_string required"}`, nil
		}
		p := filepath.Join(base, "SKILL.md")
		raw, err := os.ReadFile(p)
		if err != nil {
			return `{"success":false,"error":"SKILL.md not found"}`, nil
		}
		s := string(raw)
		if c := strings.Count(s, oldS); c != 1 {
			return `{"success":false,"error":"old_string must match exactly once"}`, nil
		}
		ns := strings.Replace(s, oldS, newS, 1)
		if err := os.WriteFile(p, []byte(ns), 0o600); err != nil {
			return "", err
		}
		b, _ := json.Marshal(map[string]any{"success": true, "message": "patched"})
		return string(b), nil
	case "edit":
		content, _ := params["content"].(string)
		if strings.TrimSpace(content) == "" {
			return `{"success":false,"error":"content required"}`, nil
		}
		p := filepath.Join(base, "SKILL.md")
		if err := os.WriteFile(p, []byte(content), 0o600); err != nil {
			return "", err
		}
		b, _ := json.Marshal(map[string]any{"success": true, "message": "updated"})
		return string(b), nil
	case "delete":
		if err := os.RemoveAll(base); err != nil {
			return "", err
		}
		b, _ := json.Marshal(map[string]any{"success": true, "message": "deleted"})
		return string(b), nil
	default:
		return `{"success":false,"error":"unknown action"}`, nil
	}
}
