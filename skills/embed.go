package skills

import "embed"

// Builtin embeds the builtin skill SKILL.md files.
//
//go:embed cron/SKILL.md draw/SKILL.md github/SKILL.md weather/SKILL.md memory/SKILL.md tmux/SKILL.md clawhub/SKILL.md summarize/SKILL.md skill-creator/SKILL.md product-manager/SKILL.md fullstack-engineer/SKILL.md qa-engineer/SKILL.md markitdown/SKILL.md
var Builtin embed.FS
