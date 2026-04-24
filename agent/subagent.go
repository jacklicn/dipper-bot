package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/jacklicn/dipper-bot/bus"
	"github.com/jacklicn/dipper-bot/config"
	"github.com/jacklicn/dipper-bot/providers"
	"github.com/jacklicn/dipper-bot/tools"
)

// SubagentManager runs background subagent tasks and announces results to the bus.
type SubagentManager struct {
	provider     providers.LLMProvider
	providerName string
	bus          *bus.MessageBus
	workspace    string
	model        string
	temp         float64
	maxTok       int
	maxIters     int
	cfg          *config.Config
	exp          config.AgentExperienceConfig
	running      map[string]struct{}
	mu           sync.Mutex
}

// NewSubagentManager creates a SubagentManager aligned with the primary agent loop (usage + nudges).
func NewSubagentManager(
	provider providers.LLMProvider,
	messageBus *bus.MessageBus,
	workspace string,
	cfg *config.Config,
	providerName string,
	model string,
	temp float64,
	maxTok int,
	maxToolIterations int,
	exp config.AgentExperienceConfig,
) *SubagentManager {
	if maxToolIterations <= 0 {
		maxToolIterations = 20
	}
	return &SubagentManager{
		provider:     provider,
		providerName: providerName,
		bus:          messageBus,
		workspace:    workspace,
		model:        model,
		temp:         temp,
		maxTok:       maxTok,
		maxIters:     maxToolIterations,
		cfg:          cfg,
		exp:          exp,
		running:      make(map[string]struct{}),
	}
}

// Spawn starts a subagent in the background. Returns immediately with a status message.
func (m *SubagentManager) Spawn(ctx context.Context, task, label, originChannel, originChatID string) (string, error) {
	taskID := fmt.Sprintf("%x", time.Now().UnixNano()%0xFFFFFFFF)
	if label == "" {
		if len(task) > 30 {
			label = task[:30] + "..."
		} else {
			label = task
		}
	}
	m.mu.Lock()
	m.running[taskID] = struct{}{}
	m.mu.Unlock()

	go m.runSubagent(context.Background(), taskID, task, label, originChannel, originChatID)
	slog.Info("subagent spawned", "id", taskID, "label", label)
	return fmt.Sprintf("Subagent [%s] started (id: %s). I'll notify you when it completes.", label, taskID), nil
}

func (m *SubagentManager) runSubagent(ctx context.Context, taskID, task, label, originChannel, originChatID string) {
	defer func() {
		m.mu.Lock()
		delete(m.running, taskID)
		m.mu.Unlock()
	}()

	allowedDir := ""
	if m.cfg.Tools.RestrictToWorkspaceEnabled() {
		allowedDir = m.workspace
	}
	reg := tools.NewRegistry()
	reg.Register(&tools.ReadFileTool{WorkspaceDir: m.workspace, AllowedDir: allowedDir})
	reg.Register(&tools.WriteFileTool{WorkspaceDir: m.workspace, AllowedDir: allowedDir})
	reg.Register(&tools.EditFileTool{WorkspaceDir: m.workspace, AllowedDir: allowedDir})
	reg.Register(&tools.ListDirTool{WorkspaceDir: m.workspace, AllowedDir: allowedDir})
	reg.Register(tools.NewExecTool(m.workspace, m.cfg.Tools.Exec.Timeout, m.cfg.Tools.RestrictToWorkspaceEnabled()))
	reg.Register(tools.NewWebSearchTool(m.cfg.Tools.Web.Search.Provider, m.cfg.Tools.Web.Search.APIKey, m.cfg.Tools.Web.Search.BaseURL, m.cfg.Tools.Web.Search.MaxResults, m.cfg.Tools.Web.Proxy))
	reg.Register(tools.NewWebFetchTool(50000, m.cfg.Tools.Web.Proxy))
	if len(m.cfg.Tools.MCPServers) > 0 {
		tools.ConnectMCPServers(context.Background(), reg, m.cfg.Tools.MCPServers)
	}

	mcpBrowserNote := ""
	toolListSuffix := ""
	if len(m.cfg.Tools.MCPServers) > 0 {
		toolListSuffix = ", plus mcp_* browser tools"
		mcpBrowserNote = "\n\n**MCP / real browser**: You also have tools whose names start with `mcp_` (Chrome DevTools MCP). When the task requires controlling the real Chrome window—open tabs, navigate, click, type, screenshots, devtools, or pages that need an existing login—**call those `mcp_*` tools in the same turns**. `web_fetch` only downloads HTTP bodies; it does **not** drive the GUI browser."
	}

	systemPrompt := fmt.Sprintf(`You are a background subagent. Complete this task and respond with a concise summary of what you did and the outcome.

Task: %s

Use the tools available (read_file, write_file, edit_file, list_dir, exec, web_search, web_fetch%s) as needed. **Call those tools in the same turns as you work**—do not output only a plan or description when a tool would progress the task. Follow the same task rules as the main agent: (1) Clarification threshold — one short question only if the goal is unclear or a missing detail would change the outcome; ask before heavy work. (2) Actionable default — if clear enough, no mid-flight strategy polls; reasonable defaults + tool loop until done or exhausted. (3) Web tools — use web_search and web_fetch only when the task text explicitly asks for a web search, online lookup, or fetching a URL; do not use them by default. (4) Cost order — local files/memory, then narrow shell/edits; avoid redundant large reads. (5) Failure handoff — what you tried, why blocked, 2–4 labeled options (A/B/…) with tradeoffs. (6) Success — one concise summary. (7) Loop discipline — re-apply after each tool round; post-tool reminders restate these rules.%s`, task, toolListSuffix, mcpBrowserNote)

	messages := []providers.Message{
		{Role: "system", Content: systemPrompt},
		{Role: "user", Content: task},
	}
	toolDefs := reg.Definitions()
	resolvedModel := config.ResolveModelForAPI(m.providerName, m.model)
	resolvedTemp := config.GetModelTemperature(m.providerName, m.model, m.temp)
	req := &providers.ChatRequest{
		Model:       resolvedModel,
		MaxTokens:   m.maxTok,
		Temperature: resolvedTemp,
		Messages:    messages,
		Tools:       toolDefsToProvider(toolDefs),
	}

	sessionKey := "subagent:" + taskID
	skillTh := effectiveSkillNudgeEvery(m.exp)
	if reg.Get("skill_manage") == nil {
		skillTh = 0 // subagent toolset has no skill_manage; avoid misleading nudges
	}
	var finalContent string
	skillItersWithoutManage := 0
	for iter := 0; iter < m.maxIters; iter++ {
		resp, err := m.provider.Chat(ctx, &providers.ChatRequest{
			Model:       resolvedModel,
			MaxTokens:   m.maxTok,
			Temperature: resolvedTemp,
			Messages:    messages,
			Tools:       req.Tools,
		})
		if err != nil {
			m.announce(originChannel, originChatID, taskID, label, "Error: "+err.Error())
			return
		}

		MaybeRecordUsage(m.workspace, m.exp, BuildUsageEvent(sessionKey, "subagent", taskID, m.providerName, resolvedModel, iter, resp))

		if !resp.HasToolCalls() {
			finalContent = resp.Content
			break
		}
		toolCallDefs := make([]providers.ToolCallDef, 0, len(resp.ToolCalls))
		for _, tc := range resp.ToolCalls {
			argBytes, _ := json.Marshal(tc.Arguments)
			toolCallDefs = append(toolCallDefs, providers.ToolCallDef{
				ID: tc.ID, Type: "function",
				Function: providers.ToolCallFunc{Name: tc.Name, Arguments: string(argBytes)},
			})
		}
		messages = AddAssistantMessage(messages, resp.Content, toolCallDefs)
		mcpDedup := make(map[string]string)
		const mcpDupNote = "[duplicate identical MCP tool_call in this assistant message — skipped second execution; result reused]\n\n"
		for _, tc := range resp.ToolCalls {
			var result string
			if key, isMCP := mcpToolDedupKey(tc.Name, tc.Arguments); isMCP {
				if prev, dup := mcpDedup[key]; dup {
					slog.Info("subagent tool call skipped (mcp duplicate same turn)", "name", tc.Name)
					result = mcpDupNote + prev
				} else {
					var err error
					result, err = reg.Execute(ctx, tc.Name, tc.Arguments)
					if err != nil {
						result = "Error: " + err.Error()
					}
					mcpDedup[key] = result
					slog.Info("subagent tool call", "name", tc.Name)
				}
			} else {
				var err error
				result, err = reg.Execute(ctx, tc.Name, tc.Arguments)
				if err != nil {
					result = "Error: " + err.Error()
				}
				slog.Info("subagent tool call", "name", tc.Name)
			}
			messages = AddToolResult(messages, tc.ID, tc.Name, result)
		}
		if skillTh > 0 {
			sawManage := false
			for _, tc := range resp.ToolCalls {
				if tc.Name == "skill_manage" {
					sawManage = true
					break
				}
			}
			if sawManage {
				skillItersWithoutManage = 0
			} else {
				skillItersWithoutManage++
			}
		}
		reflect := "Continue with tool calls if more work remains, or one final answer if done—do not reply with only a plan when tools would help. (2) No strategy polls; defaults + tools until done or exhausted. (3) web_search/web_fetch only if the task explicitly requested search or URL fetch—not by default. (4) Real browser/UI: if you have mcp_* tools and the task needs Chrome control, call them—web_fetch is not a substitute. (5) Cost order: local/memory → narrow shell/edits; avoid redundant large reads. (6) Stop without success? Attempts, why blocked, 2–4 labeled options + tradeoffs. (7) Success? One clear answer. (8) Every round; skill_manage reminder may prepend when configured (ignore if tool absent). (1) Only if still genuinely ambiguous: one brief question before heavy tools—not every round."
		if skillTh > 0 && skillItersWithoutManage >= skillTh {
			reflect = "[Reminder] Consider updating workspace skills via skill_manage when patterns repeat.\n\n" + reflect
			skillItersWithoutManage = 0
		}
		messages = append(messages, providers.Message{Role: "user", Content: reflect})
		req.Messages = messages
	}
	if finalContent == "" {
		finalContent = "Task completed but no final response was generated."
	}
	m.announce(originChannel, originChatID, taskID, label, finalContent)
}

func (m *SubagentManager) announce(channel, chatID, taskID, label, content string) {
	msg := fmt.Sprintf("[Subagent %s] %s\n\n%s", label, taskID, content)
	_ = m.bus.PublishOutbound(context.Background(), &bus.OutboundMessage{
		Channel: channel,
		ChatID:  chatID,
		Content: msg,
	})
}

func toolDefsToProvider(defs []map[string]any) []providers.ToolDef {
	out := make([]providers.ToolDef, 0, len(defs))
	for _, d := range defs {
		fn, _ := d["function"].(map[string]any)
		if fn == nil {
			continue
		}
		name, _ := fn["name"].(string)
		desc, _ := fn["description"].(string)
		params, _ := fn["parameters"].(map[string]any)
		out = append(out, providers.ToolDef{
			Type:     "function",
			Function: providers.ToolFunction{Name: name, Description: desc, Parameters: params},
		})
	}
	return out
}
