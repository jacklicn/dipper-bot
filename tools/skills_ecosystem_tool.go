package tools

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// SkillsEcosystemTool handles template import, publish flow, and scoreboards.
type SkillsEcosystemTool struct {
	Workspace string
}

func (t *SkillsEcosystemTool) Name() string { return "skills_ecosystem" }

func (t *SkillsEcosystemTool) Description() string {
	return `Skills ecosystem operations.
Actions:
- list_templates: list templates under workspace/skills-templates
- import_template: import template into workspace/skills
- publish: snapshot a skill to workspace/published-skills
- scoreboard: read top skills from telemetry`
}

func (t *SkillsEcosystemTool) Parameters() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"action":        map[string]any{"type": "string", "enum": []any{"list_templates", "import_template", "publish", "scoreboard"}},
			"name":          map[string]any{"type": "string"},
			"template_name": map[string]any{"type": "string"},
			"limit":         map[string]any{"type": "integer"},
		},
		"required": []any{"action"},
	}
}

func (t *SkillsEcosystemTool) Execute(ctx context.Context, params map[string]any) (string, error) {
	action, _ := params["action"].(string)
	switch action {
	case "list_templates":
		return t.listTemplates()
	case "import_template":
		templateName, _ := params["template_name"].(string)
		name, _ := params["name"].(string)
		return t.importTemplate(templateName, name)
	case "publish":
		name, _ := params["name"].(string)
		return t.publishSkill(name)
	case "scoreboard":
		limit := 10
		if v, ok := params["limit"].(float64); ok && int(v) > 0 {
			limit = int(v)
		}
		return t.scoreboard(limit)
	default:
		return `{"success":false,"error":"unknown action"}`, nil
	}
}

func (t *SkillsEcosystemTool) listTemplates() (string, error) {
	dir := filepath.Join(t.Workspace, "skills-templates")
	entries, err := os.ReadDir(dir)
	if err != nil {
		return `{"success":true,"templates":[]}`, nil
	}
	out := make([]string, 0, len(entries))
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		if _, err := os.Stat(filepath.Join(dir, e.Name(), "SKILL.md")); err == nil {
			out = append(out, e.Name())
		}
	}
	sort.Strings(out)
	b, _ := json.Marshal(map[string]any{"success": true, "templates": out})
	return string(b), nil
}

func (t *SkillsEcosystemTool) importTemplate(templateName, name string) (string, error) {
	templateName = strings.TrimSpace(templateName)
	if name = strings.TrimSpace(name); name == "" {
		name = templateName
	}
	if !skillNameRe.MatchString(templateName) || !skillNameRe.MatchString(name) {
		return `{"success":false,"error":"invalid template/name"}`, nil
	}
	src := filepath.Join(t.Workspace, "skills-templates", templateName, "SKILL.md")
	dstDir := filepath.Join(t.Workspace, "skills", name)
	dst := filepath.Join(dstDir, "SKILL.md")
	b, err := os.ReadFile(src)
	if err != nil {
		return `{"success":false,"error":"template not found"}`, nil
	}
	if _, err := os.Stat(dst); err == nil {
		return `{"success":false,"error":"skill already exists"}`, nil
	}
	_ = os.MkdirAll(dstDir, 0o750)
	if err := os.WriteFile(dst, b, 0o600); err != nil {
		return "", err
	}
	resp, _ := json.Marshal(map[string]any{"success": true, "message": "template imported", "skill": name})
	return string(resp), nil
}

func (t *SkillsEcosystemTool) publishSkill(name string) (string, error) {
	name = strings.TrimSpace(name)
	if !skillNameRe.MatchString(name) {
		return `{"success":false,"error":"invalid skill name"}`, nil
	}
	src := filepath.Join(t.Workspace, "skills", name, "SKILL.md")
	b, err := os.ReadFile(src)
	if err != nil {
		return `{"success":false,"error":"skill not found"}`, nil
	}
	tag := time.Now().UTC().Format("20060102-150405")
	dstDir := filepath.Join(t.Workspace, "published-skills", name+"-"+tag)
	_ = os.MkdirAll(dstDir, 0o750)
	if err := os.WriteFile(filepath.Join(dstDir, "SKILL.md"), b, 0o600); err != nil {
		return "", err
	}
	resp, _ := json.Marshal(map[string]any{"success": true, "published": filepath.ToSlash(strings.TrimPrefix(dstDir, t.Workspace+string(filepath.Separator)))})
	return string(resp), nil
}

func (t *SkillsEcosystemTool) scoreboard(limit int) (string, error) {
	if limit <= 0 {
		limit = 10
	}
	path := filepath.Join(t.Workspace, "memory", "learning_telemetry.jsonl")
	b, err := os.ReadFile(path)
	if err != nil {
		return `{"success":true,"rows":[]}`, nil
	}
	type rec struct {
		Success int `json:"success"`
		Total   int `json:"total"`
	}
	m := map[string]*rec{}
	for _, ln := range strings.Split(string(b), "\n") {
		ln = strings.TrimSpace(ln)
		if ln == "" || !strings.Contains(ln, `"category":"skill"`) {
			continue
		}
		var row map[string]any
		if err := json.Unmarshal([]byte(ln), &row); err != nil {
			continue
		}
		meta, _ := row["meta"].(map[string]any)
		skill, _ := meta["skill"].(string)
		if skill == "" {
			continue
		}
		if _, ok := m[skill]; !ok {
			m[skill] = &rec{}
		}
		m[skill].Total++
		outcome, _ := row["outcome"].(string)
		if outcome == "success" {
			m[skill].Success++
		}
	}
	type row struct {
		Skill       string  `json:"skill"`
		Successes   int     `json:"successes"`
		Total       int     `json:"total"`
		SuccessRate float64 `json:"successRate"`
	}
	rows := make([]row, 0, len(m))
	for skill, r := range m {
		rate := 0.0
		if r.Total > 0 {
			rate = float64(r.Success) / float64(r.Total)
		}
		rows = append(rows, row{Skill: skill, Successes: r.Success, Total: r.Total, SuccessRate: rate})
	}
	sort.Slice(rows, func(i, j int) bool {
		if rows[i].SuccessRate == rows[j].SuccessRate {
			return rows[i].Successes > rows[j].Successes
		}
		return rows[i].SuccessRate > rows[j].SuccessRate
	})
	if len(rows) > limit {
		rows = rows[:limit]
	}
	resp, _ := json.Marshal(map[string]any{"success": true, "count": len(rows), "rows": rows})
	return string(resp), nil
}

