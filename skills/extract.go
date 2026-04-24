package skills

import (
	"os"
	"path/filepath"
)

var builtinSkillNames = []string{
	"cron",
	"github",
	"weather",
	"memory",
	"tmux",
	"clawhub",
	"summarize",
	"skill-creator",
	"product-manager",
	"fullstack-engineer",
	"qa-engineer",
	"markitdown",
}

// ExtractTo copies builtin skills to destDir (e.g. workspace/skills).
// Skips skills that already exist (workspace overrides builtin).
func ExtractTo(destDir string) error {
	for _, skillName := range builtinSkillNames {
		skillDest := filepath.Join(destDir, skillName)
		if _, err := os.Stat(skillDest); err == nil {
			continue // already exists, workspace override
		}
		skillPath := skillName + "/SKILL.md"
		data, err := Builtin.ReadFile(skillPath)
		if err != nil {
			continue
		}
		_ = os.MkdirAll(skillDest, 0o750)
		destFile := filepath.Join(skillDest, "SKILL.md")
		if err := os.WriteFile(destFile, data, 0o644); err != nil {
			return err
		}
	}
	return nil
}
