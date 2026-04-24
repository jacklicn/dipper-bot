# dipper-bot Skills

Built-in skills that extend dipper-bot's capabilities.

## Skill Format

Each skill is a directory containing a `SKILL.md` file with:
- YAML frontmatter (name, description, metadata)
- Markdown instructions for the agent

## Available Skills

| Skill | Description |
|-------|-------------|
| `cron` | Schedule reminders and recurring tasks |
| `draw` | Generate charts and diagrams via HTML + Canvas/SVG (Chart.js animation, rAF, SVG/CSS) |
| `github` | Interact with GitHub using the `gh` CLI |
| `weather` | Get weather info using wttr.in and Open-Meteo |
| `summarize` | Summarize URLs, files, and YouTube videos |
| `tmux` | Remote-control tmux sessions |
| `clawhub` | Search and install skills from ClawHub registry |
| `skill-creator` | Create new skills |
| `memory` | Two-layer memory system with grep-based recall |

## Loading

During `dipper-bot onboard`, the built-in `skills/` tree from the binary is copied into `workspace/skills/` (existing files are left unchanged). On first agent use, `ExtractTo` may still add any embedded skill that is missing a directory. Workspace skills override built-in ones.
