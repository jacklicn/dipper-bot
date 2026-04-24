package agent

import (
	"os"
	"path/filepath"
	"strings"
)

// SkillsLoader loads SKILL.md files from workspace/skills and optional builtin directory.
type SkillsLoader struct {
	workspaceSkills string
	builtinSkills   string
}

// NewSkillsLoader creates a skills loader. builtinDir can be empty to only use workspace.
func NewSkillsLoader(workspace, builtinDir string) *SkillsLoader {
	return &SkillsLoader{
		workspaceSkills: filepath.Join(workspace, "skills"),
		builtinSkills:   builtinDir,
	}
}

// listSkillsFromDir returns skills found in dir (name + path).
func listSkillsFromDir(dir string) ([]struct{ Name, Path string }, error) {
	var out []struct{ Name, Path string }
	f, err := os.Open(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	defer f.Close()
	entries, err := f.ReadDir(-1)
	if err != nil {
		return nil, err
	}
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		skillPath := filepath.Join(dir, e.Name(), "SKILL.md")
		if _, err := os.Stat(skillPath); err != nil {
			continue
		}
		out = append(out, struct{ Name, Path string }{Name: e.Name(), Path: skillPath})
	}
	return out, nil
}

// ListSkills returns skill names and paths (workspace first, then builtin; workspace overrides).
func (s *SkillsLoader) ListSkills() ([]struct{ Name, Path string }, error) {
	seen := make(map[string]bool)
	var out []struct{ Name, Path string }
	ws, err := listSkillsFromDir(s.workspaceSkills)
	if err != nil {
		return nil, err
	}
	for _, sk := range ws {
		seen[sk.Name] = true
		out = append(out, sk)
	}
	if s.builtinSkills != "" {
		builtin, err := listSkillsFromDir(s.builtinSkills)
		if err == nil {
			for _, sk := range builtin {
				if !seen[sk.Name] {
					out = append(out, sk)
				}
			}
		}
	}
	return out, nil
}

// LoadSkill returns the content of a skill's SKILL.md (workspace first, then builtin).
func (s *SkillsLoader) LoadSkill(name string) (string, error) {
	p := filepath.Join(s.workspaceSkills, name, "SKILL.md")
	data, err := os.ReadFile(p)
	if err == nil {
		return string(data), nil
	}
	if s.builtinSkills != "" {
		p = filepath.Join(s.builtinSkills, name, "SKILL.md")
		data, err = os.ReadFile(p)
		if err == nil {
			return string(data), nil
		}
	}
	return "", err
}

// BuildSummary returns a short summary of available skills for the system prompt.
// Matches Python-style guidance: use read_file to load SKILL.md when needed.
func (s *SkillsLoader) BuildSummary() string {
	list, err := s.ListSkills()
	if err != nil || len(list) == 0 {
		return ""
	}
	var b strings.Builder
	b.WriteString("The following skills extend your capabilities. To use a skill, read its SKILL.md using the read_file tool.\n\n")
	for _, sk := range list {
		b.WriteString("- ")
		b.WriteString(sk.Name)
		b.WriteString(": ")
		b.WriteString(sk.Path)
		b.WriteString("\n")
	}
	return b.String()
}
