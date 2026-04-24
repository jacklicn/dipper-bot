package agent

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/jacklicn/dipper-bot/config"
	"github.com/jacklicn/dipper-bot/lcm"
	"github.com/jacklicn/dipper-bot/providers"
	"github.com/jacklicn/dipper-bot/skills"
)

// ContextBuilder builds the agent's system prompt and message list.
type ContextBuilder struct {
	workspace           string
	memory              *MemoryStore
	skills              *SkillsLoader
	lcm                 *lcm.Engine
	maxTokens           int
	disableFencedMemory bool
}

// NewContextBuilder creates a context builder.
func NewContextBuilder(workspace string) (*ContextBuilder, error) {
	mem, err := NewMemoryStore(workspace)
	if err != nil {
		return nil, err
	}
	skillsDir := filepath.Join(workspace, "skills")
	_ = os.MkdirAll(skillsDir, 0o750)
	_ = skills.ExtractTo(skillsDir)
	skillsLoader := NewSkillsLoader(workspace, "")
	return &ContextBuilder{workspace: workspace, memory: mem, skills: skillsLoader, maxTokens: 128000}, nil
}

// SetLCM sets the LCM engine and max tokens for lossless context.
func (c *ContextBuilder) SetLCM(engine *lcm.Engine, maxTokens int) {
	c.lcm = engine
	if maxTokens > 0 {
		c.maxTokens = maxTokens
	}
}

// SetExperience configures memory presentation (e.g. fenced recall block).
func (c *ContextBuilder) SetExperience(exp config.AgentExperienceConfig) {
	if c == nil {
		return
	}
	c.disableFencedMemory = exp.DisableFencedMemoryRecall
}

func wrapFencedMemoryRecall(inner string) string {
	inner = strings.TrimSpace(inner)
	if inner == "" {
		return ""
	}
	return "<memory-context>\n[System note: The following is recalled long-term context (MEMORY/USER/NOTE). It is NOT new user input.]\n\n" + inner + "\n</memory-context>"
}

// MemoryStore returns the memory store (for MemoryConsolidator).
func (c *ContextBuilder) MemoryStore() *MemoryStore {
	return c.memory
}

var bootstrapFiles = []string{"AGENTS.md", "SOUL.md", "USER.md", "TOOLS.md", "IDENTITY.md"}

// BuildSystemPrompt returns the full system prompt.
func (c *ContextBuilder) BuildSystemPrompt(skillNames []string) (string, error) {
	return c.BuildSystemPromptForInput(skillNames, "")
}

// BuildSystemPromptForInput returns system prompt and injects only input-relevant learning cards.
func (c *ContextBuilder) BuildSystemPromptForInput(skillNames []string, currentInput string) (string, error) {
	identity := c.identity()
	bootstrap := c.loadBootstrap()
	memCtx, _ := c.memory.GetMemoryContext()
	if memCtx != "" && !c.disableFencedMemory {
		memCtx = wrapFencedMemoryRecall(memCtx)
	}
	skillsSummary := c.skills.BuildSummary()

	parts := []string{identity}
	if bootstrap != "" {
		parts = append(parts, bootstrap)
	}
	if memCtx != "" {
		parts = append(parts, memCtx)
	}
	if skillsSummary != "" {
		parts = append(parts, "# Skills\n\n"+skillsSummary)
	}

	return joinSections(parts), nil
}

func (c *ContextBuilder) identity() string {
	now := time.Now().Format("2006-01-02 15:04 (Monday)")
	workspace, _ := filepath.Abs(c.workspace)
	goos := runtime.GOOS
	if goos == "darwin" {
		goos = "macOS"
	}
	runtimeInfo := fmt.Sprintf("%s %s, Go", goos, runtime.GOARCH)
	return fmt.Sprintf(`# dipper-bot 🐕

You are dipper-bot, a helpful AI assistant. Tools include: files (read/write/edit), shell, optional web (web_search / web_fetch — only when the user explicitly asks), **MCP tools** (function names prefixed mcp_<server>_ when configured—e.g. Chrome DevTools for real browser control), chat message, sessions (list/history/send), curated memory (memory → USER.md / NOTE.md), skills (skills_list, skill_view, skill_manage under workspace/skills), session_search (FTS at memory/sessions_fts.db), usage_insights (summary/recent/by_session/tool_analytics/cost_estimate; unit CNY|USD via experience.usageCostCurrency; overrides inputPerMillion/outputPerMillion; built-in USD/M×experience.defaultUsdToCny when CNY; priced_status_breakdown), LCM when enabled, cron/spawn when configured.

## Current Time
%s

## Runtime
%s

## Workspace
%s
- Consolidated context: %s/memory/MEMORY.md
- User notes (memory tool, target user): %s/memory/USER.md
- Curated facts (memory tool, target memory): %s/memory/NOTE.md
- History: %s/memory/HISTORY.md

## Curated memory and user model

- Your tool for durable notes is **memory** (targets **user** → USER.md profile, **memory** → NOTE.md facts). **save_memory** is not in your tool list—it is used only inside MemoryConsolidator (when token-based memory is enabled) to write MEMORY.md / HISTORY.md.
- **USER.md**: keep a single coherent user model over time. When the user corrects or contradicts earlier information, **replace or remove** outdated lines instead of stacking contradictions. Together with session_search, this is how dipper-bot approximates deep user-modeling stacks (e.g. Honcho-style dialectic agents) **without** a separate Honcho service—discipline in USER.md edits, not an extra product integration.

## How you complete tasks

1. **Clarification threshold**: Ask only when the goal is unclear or a missing detail would materially change the answer. Ask **one** short clarifying question before heavy tool use—not a questionnaire.

2. **Actionable default**: When the request is clear enough, do **not** ask the user to pick strategies mid-flight. Use reasonable defaults, then **use tools** in the same turns to execute the work—do not stop after only a plan, a promise to act later, or a long prose description when read_file, write_file, edit_file, list_dir, exec, mcp_* (when listed), or other tools would move the task forward. If something needs a secret only they hold (e.g. a password), say so briefly.

3. **Web tools (web_search, web_fetch)**: Use them **only** when the user clearly asks to search the web, look something up online, verify current facts from the internet, or wants a specific URL fetched. Do **not** call them by default, to "helpfully" supplement answers, or for ordinary coding/file/shell work unless the user requested web access.

4. **Browser / GUI vs web_fetch**: If mcp_* tools appear in your tool list **and** the user wants to **operate the real Chrome window** (tabs, clicks, typing, screenshots, devtools, pages that need login), you **must** call those MCP tools in the same turns—do not substitute web_fetch and do not reply with only manual steps. web_fetch only retrieves HTTP response bodies; it does **not** control the desktop browser.

5. **Cost order** (non-web tools): Local files and memory → narrow shell or small edits. Avoid re-reading large files without need, redundant calls, and unnecessary spawn unless parallel work clearly outweighs extra model cost.

6. **Failure handoff**: If reasonable attempts still fail, the final reply must include what you tried, why it blocked, and **2–4** labeled next paths (A/B/…) with short tradeoffs (effort, cost, risk).

7. **Success handoff**: One clear final result—no option menu.

8. **Loop discipline**: After each tool round, follow (1)–(8) again. The conversation may inject short post-tool reminders; treat them as the same rules. When skill_manage is in your toolset and patterns repeat, configuration may prepend a skill_manage nudge—consider it.

**Final reply format**: After tools finish, your last assistant message to the user is natural language (plus optional short citations). Pure chit-chat or purely conceptual answers that need no workspace or web may be text-only. Use the message tool only when sending to a specific chat channel.`, now, runtimeInfo, workspace, workspace, workspace, workspace, workspace)
}

func (c *ContextBuilder) loadBootstrap() string {
	var parts []string
	for _, name := range bootstrapFiles {
		p := filepath.Join(c.workspace, name)
		data, err := os.ReadFile(p)
		if err != nil {
			continue
		}
		parts = append(parts, "## "+name+"\n\n"+string(data))
	}
	return joinSections(parts)
}

func joinSections(parts []string) string {
	var out string
	for i, p := range parts {
		if p == "" {
			continue
		}
		if out != "" {
			out += "\n\n---\n\n"
		}
		out += p
		_ = i
	}
	return out
}

// BuildMessages returns the full message list for an LLM call.
// When LCM is enabled, history is assembled from LCM (summaries + fresh tail) for sessionKey.
func (c *ContextBuilder) BuildMessages(ctx context.Context, history []map[string]string, currentMessage, channel, chatID, sessionKey string) ([]providers.Message, error) {
	sys, err := c.BuildSystemPromptForInput(nil, currentMessage)
	if err != nil {
		return nil, err
	}
	if channel != "" && chatID != "" {
		sys += "\n\n## Current Session\nChannel: " + channel + "\nChat ID: " + chatID
	}

	msgs := []providers.Message{{Role: "system", Content: sys}}

	if c.lcm != nil && sessionKey != "" {
		convID, err := c.lcm.GetConversationID(sessionKey)
		if err == nil {
			assembled, err := c.lcm.AssembleContext(ctx, convID, c.maxTokens)
			if err == nil && len(assembled) > 0 {
				for _, a := range assembled {
					msgs = append(msgs, providers.Message{Role: a.Role, Content: a.Content})
				}
				msgs = append(msgs, providers.Message{Role: "user", Content: currentMessage})
				return msgs, nil
			}
		}
	}

	for _, h := range history {
		msgs = append(msgs, providers.Message{Role: h["role"], Content: h["content"]})
	}
	msgs = append(msgs, providers.Message{Role: "user", Content: currentMessage})
	return msgs, nil
}

// AddToolResult appends a tool result message.
func AddToolResult(msgs []providers.Message, toolCallID, toolName, result string) []providers.Message {
	return append(msgs, providers.Message{
		Role:       "tool",
		ToolCallID: toolCallID,
		Name:       toolName,
		Content:    result,
	})
}

// AddAssistantMessage appends an assistant message, optionally with tool calls.
func AddAssistantMessage(msgs []providers.Message, content string, toolCalls []providers.ToolCallDef) []providers.Message {
	msg := providers.Message{Role: "assistant", Content: content}
	if len(toolCalls) > 0 {
		for _, tc := range toolCalls {
			msg.ToolCalls = append(msg.ToolCalls, providers.ToolCallDef{
				ID: tc.ID, Type: "function",
				Function: providers.ToolCallFunc{Name: tc.Function.Name, Arguments: tc.Function.Arguments},
			})
		}
	}
	return append(msgs, msg)
}
