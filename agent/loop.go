package agent

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/jacklicn/dipper-bot/bus"
	"github.com/jacklicn/dipper-bot/config"
	"github.com/jacklicn/dipper-bot/cron"
	"github.com/jacklicn/dipper-bot/lcm"
	"github.com/jacklicn/dipper-bot/providers"
	"github.com/jacklicn/dipper-bot/session"
	"github.com/jacklicn/dipper-bot/tools"
)

// AgentLoop is the core processing engine: consumes inbound messages, calls LLM, runs tools, publishes outbound.
type AgentLoop struct {
	bus                        *bus.MessageBus
	provider                   providers.LLMProvider
	providerName               string
	workspace                  string
	model                      string
	maxIter                    int
	temp                       float64
	maxTok                     int
	memWindow                  int
	contextWindow              int
	reasoningEffort            string
	runTimeoutSec              int
	context                    *ContextBuilder
	sessions                   *session.SessionManager
	registry                   *tools.Registry
	cron                       *CronServiceRef
	restrict                   bool
	running                    bool
	mu                         sync.Mutex
	lcm                        *lcm.Engine
	consolidator               *tools.MemoryConsolidator
	maintainer                 *MemoryMaintainer
	skillMaintainer            *SkillMaintainer
	telemetry                  *LearningTelemetry
	governance                 *LearningGovernance
	lastAudit                  time.Time
	exp                        config.AgentExperienceConfig
	maxEmbeddedAttachmentBytes int
	// sessionCancels: new message for same session cancels previous run
	sessionCancels   map[string]*sessionRun
	sessionCancelsMu sync.Mutex
	// pendingLearnLines: async learner writes queued for prepend on next assistant reply (all channels).
	pendingLearnMu    sync.Mutex
	pendingLearnLines map[string][]string
}

type sessionRun struct {
	cancel context.CancelFunc
}

// CronServiceRef holds optional cron service for the agent.
type CronServiceRef struct {
	Service *cron.Service
}

// NewAgentLoop creates an agent loop. providerName is used for model resolution (e.g. "moonshot", "openrouter").
func NewAgentLoop(
	messageBus *bus.MessageBus,
	provider providers.LLMProvider,
	providerName string,
	cfg *config.Config,
	workspace string,
	sessionManager *session.SessionManager,
	cronSvc *CronServiceRef,
) (*AgentLoop, error) {
	ctx, err := NewContextBuilder(workspace)
	if err != nil {
		return nil, err
	}
	ctx.SetExperience(cfg.Agents.Defaults.Experience)
	def := cfg.Agents.Defaults
	if def.Model == "" {
		def.Model = "anthropic/claude-opus-4-5"
	}
	if def.MaxToolIterations <= 0 {
		def.MaxToolIterations = 20
	}
	if def.MemoryWindow == 0 {
		def.MemoryWindow = 50
	}

	reg := tools.NewRegistry()
	allowedDir := ""
	if cfg.Tools.RestrictToWorkspaceEnabled() {
		allowedDir = workspace
	}
	reg.Register(&tools.ReadFileTool{WorkspaceDir: workspace, AllowedDir: allowedDir})
	reg.Register(&tools.WriteFileTool{WorkspaceDir: workspace, AllowedDir: allowedDir})
	reg.Register(&tools.EditFileTool{WorkspaceDir: workspace, AllowedDir: allowedDir})
	reg.Register(&tools.ListDirTool{WorkspaceDir: workspace, AllowedDir: allowedDir})

	execTool := tools.NewExecTool(workspace, cfg.Tools.Exec.Timeout, cfg.Tools.RestrictToWorkspaceEnabled())
	reg.Register(execTool)

	reg.Register(tools.NewWebSearchTool(cfg.Tools.Web.Search.Provider, cfg.Tools.Web.Search.APIKey, cfg.Tools.Web.Search.BaseURL, cfg.Tools.Web.Search.MaxResults, cfg.Tools.Web.Proxy))
	reg.Register(tools.NewWebFetchTool(50000, cfg.Tools.Web.Proxy))

	sendFn := func(ctx context.Context, msg *bus.OutboundMessage) error {
		return messageBus.PublishOutbound(ctx, msg)
	}
	msgTool := tools.NewMessageTool(sendFn)
	reg.Register(msgTool)

	reg.Register(tools.NewSessionsListTool(sessionManager))
	reg.Register(tools.NewSessionsHistoryTool(sessionManager))
	reg.Register(tools.NewSessionsSendTool(messageBus.PublishInbound))

	// Curated memory + FTS session index + skills + session_search
	memDir := filepath.Join(workspace, "memory")
	if err := os.MkdirAll(memDir, 0o750); err != nil {
		slog.Warn("memory dir", "error", err)
	}
	var ftsIndexer *session.FTSIndexer
	if fts, err := session.NewFTSIndexer(filepath.Join(memDir, "sessions_fts.db")); err != nil {
		slog.Warn("session fts index", "error", err)
	} else {
		ftsIndexer = fts
		sessionManager.SetFTSIndexer(fts)
		go func(ft *session.FTSIndexer) {
			infos, err := sessionManager.ListSessions(0)
			if err != nil {
				return
			}
			for _, info := range infos {
				sess, err := sessionManager.GetOrCreate(info.Key)
				if err != nil || sess == nil {
					continue
				}
				msgs := sess.GetMessagesFrom(0, 1<<20)
				_ = ft.ReindexSession(info.Key, msgs)
			}
		}(fts)
	}
	curatedMem := &tools.MemoryNoteStore{Workspace: workspace}
	reg.Register(&tools.MemoryTool{Store: curatedMem})
	reg.Register(&tools.SkillsListTool{Workspace: workspace})
	reg.Register(&tools.SkillViewTool{Workspace: workspace})
	reg.Register(&tools.SkillManageTool{Workspace: workspace})
	reg.Register(&tools.SessionSearchTool{
		Workspace: workspace,
		Sessions:  sessionManager,
		FTS:       ftsIndexer,
		Provider:  provider,
		Model:     def.Model,
	})
	reg.Register(&tools.UsageInsightsTool{
		Workspace:         workspace,
		PricingOverrides:  cfg.Agents.Defaults.Experience.UsagePricingOverrides,
		UsageCostCurrency: cfg.Agents.Defaults.Experience.UsageCostCurrency,
		DefaultUsdToCny:   cfg.Agents.Defaults.Experience.DefaultUsdToCny,
	})

	if cronSvc != nil && cronSvc.Service != nil {
		reg.Register(tools.NewCronTool(cronSvc.Service))
	}

	maxSubIters := def.MaxToolIterations
	if maxSubIters <= 0 {
		maxSubIters = 20
	}
	subagentMgr := NewSubagentManager(provider, messageBus, workspace, cfg, providerName, def.Model, def.Temperature, def.MaxTokens, maxSubIters, def.Experience)
	reg.Register(tools.NewSpawnTool(subagentMgr))

	// MCP servers (stdio or HTTP); tools register as mcp_<server>_<toolName>
	if len(cfg.Tools.MCPServers) > 0 {
		tools.ConnectMCPServers(context.Background(), reg, cfg.Tools.MCPServers)
	}

	// LCM (Lossless Context Management)
	var lcmEngine *lcm.Engine
	if lc := def.LCM; lc.LCMEnabled() {
		lcCfg := lcm.Config{
			Enabled:               true,
			DatabasePath:          lc.DatabasePath,
			ContextThreshold:      lc.ContextThreshold,
			FreshTailCount:        lc.FreshTailCount,
			LeafMinFanout:         lc.LeafMinFanout,
			CondensedMinFanout:    lc.CondensedMinFanout,
			LeafChunkTokens:       lc.LeafChunkTokens,
			LeafTargetTokens:      lc.LeafTargetTokens,
			CondensedTargetTokens: lc.CondensedTargetTokens,
			IncrementalMaxDepth:   lc.IncrementalMaxDepth,
		}
		if lcCfg.ContextThreshold == 0 {
			lcCfg.ContextThreshold = 0.60
		}
		if lcCfg.FreshTailCount == 0 {
			lcCfg.FreshTailCount = 16
		}
		if lcCfg.LeafMinFanout == 0 {
			lcCfg.LeafMinFanout = 10
		}
		if lcCfg.CondensedMinFanout == 0 {
			lcCfg.CondensedMinFanout = 8
		}
		if lcCfg.LeafChunkTokens == 0 {
			lcCfg.LeafChunkTokens = 12000
		}
		if lcCfg.LeafTargetTokens == 0 {
			lcCfg.LeafTargetTokens = 600
		}
		if lcCfg.CondensedTargetTokens == 0 {
			lcCfg.CondensedTargetTokens = 900
		}
		if lcCfg.IncrementalMaxDepth == 0 {
			lcCfg.IncrementalMaxDepth = 1
		}
		lcmEngine, _ = lcm.NewEngine(lcCfg, workspace, provider, def.Model)
		if lcmEngine != nil {
			ctx.SetLCM(lcmEngine, def.MaxTokens)
			reg.Register(tools.NewLcmGrepTool(lcmEngine))
			reg.Register(tools.NewLcmDescribeTool(lcmEngine))
		}
	}

	var consolidator *tools.MemoryConsolidator
	if def.ContextWindowTokens > 0 {
		consolidator = tools.NewMemoryConsolidator(
			ctx.MemoryStore(),
			provider,
			def.Model,
			sessionManager,
			def.ContextWindowTokens,
			ctx.BuildMessages,
			func() []providers.ToolDef { return toolDefsFromRegistry(reg) },
		)
		if consolidator != nil && !def.Experience.DisableCompactionPressureLogs && def.ContextWindowTokens > 0 {
			consolidator.SetLogContextPressure(true)
		}
	}

	telemetry := NewLearningTelemetry(workspace)
	maintainer := NewMemoryMaintainer(provider, def.Model, curatedMem, def.MemoryMaintenance, telemetry)
	skillMaintainer := NewSkillMaintainer(provider, def.Model, workspace, def.SkillsEvolution, telemetry)
	governance := NewLearningGovernance(workspace, sessionManager, telemetry, provider, def.Model, def.MemoryMaintenance)

	loop := &AgentLoop{
		bus:                        messageBus,
		provider:                   provider,
		providerName:               providerName,
		workspace:                  workspace,
		model:                      def.Model,
		maxIter:                    def.MaxToolIterations,
		temp:                       def.Temperature,
		maxTok:                     def.MaxTokens,
		memWindow:                  def.MemoryWindow,
		contextWindow:              def.ContextWindowTokens,
		reasoningEffort:            def.ReasoningEffort,
		runTimeoutSec:              def.RunTimeoutSec,
		context:                    ctx,
		sessions:                   sessionManager,
		registry:                   reg,
		cron:                       cronSvc,
		restrict:                   cfg.Tools.RestrictToWorkspaceEnabled(),
		lcm:                        lcmEngine,
		consolidator:               consolidator,
		maintainer:                 maintainer,
		skillMaintainer:            skillMaintainer,
		telemetry:                  telemetry,
		governance:                 governance,
		exp:                        def.Experience,
		maxEmbeddedAttachmentBytes: def.Attachments.MaxEmbeddedBytes,
		sessionCancels:             make(map[string]*sessionRun),
		pendingLearnLines:          make(map[string][]string),
	}
	if maintainer != nil {
		maintainer.onMemoryApplied = func(sk, tgt, act string) {
			line := formatMemoryLearnerLine(tgt, act)
			loop.recordLearnerFeedback(sk, line)
		}
	}
	if skillMaintainer != nil {
		skillMaintainer.onSkillApplied = func(n SkillApplyNotice, sk string) {
			line := formatSkillApplyNoticeLine(n)
			loop.recordLearnerFeedback(sk, line)
		}
	}
	reg.Register(&tools.LearningTelemetryTool{Workspace: workspace})
	reg.Register(&tools.SkillsEcosystemTool{Workspace: workspace})
	return loop, nil
}

// ProcessDirect processes a single message (CLI or cron). Returns response text.
// onProgress is optional; when set, called with (toolName, detail) before each tool execution.
// When ctx is cancelled (e.g. browser closed) or a new message arrives for the same session, the run is aborted.
func (l *AgentLoop) ProcessDirect(ctx context.Context, content, sessionKey, channel, chatID string, onProgress func(toolName, detail string)) (string, error) {
	sessionKey, channel, chatID = normalizeDirectRoute(sessionKey, channel, chatID)
	key := sessionKey

	// Cancel any existing run for this session (e.g. new message or page refresh)
	l.cancelSessionRun(key)

	var execCtx context.Context
	var cancel context.CancelFunc
	if l.runTimeoutSec > 0 {
		execCtx, cancel = context.WithTimeout(ctx, time.Duration(l.runTimeoutSec)*time.Second)
	} else {
		execCtx, cancel = context.WithCancel(ctx)
	}
	run := &sessionRun{cancel: cancel}
	l.sessionCancelsMu.Lock()
	l.sessionCancels[key] = run
	l.sessionCancelsMu.Unlock()
	defer func() {
		l.sessionCancelsMu.Lock()
		if l.sessionCancels[key] == run {
			delete(l.sessionCancels, key)
		}
		l.sessionCancelsMu.Unlock()
		l.flushSessionMemory(key)
		l.flushSessionSkillByKey(key)
		cancel()
	}()

	msg := &bus.InboundMessage{
		Channel:   channel,
		SenderID:  "user",
		ChatID:    chatID,
		Content:   content,
		Timestamp: time.Now(),
	}
	resp, err := l.processMessage(execCtx, msg, sessionKey, onProgress)
	if err != nil {
		if execCtx.Err() == context.Canceled {
			return "", nil // aborted by client disconnect or new message
		}
		return "", err
	}
	if resp == nil {
		return "", nil
	}
	return resp.Content, nil
}

// processMessage handles one inbound message and returns the outbound reply.
func (l *AgentLoop) processMessage(ctx context.Context, msg *bus.InboundMessage, sessionKey string, onProgress func(toolName, detail string)) (*bus.OutboundMessage, error) {
	key := sessionKey
	if key == "" {
		key = msg.SessionKey()
	}

	sess, err := l.sessions.GetOrCreate(key)
	if err != nil {
		return nil, err
	}

	// Slash commands
	cmd := trimLower(msg.Content)
	if cmd == "/new" {
		l.clearPendingLearnerFeedback(key)
		if l.maintainer != nil {
			l.maintainer.FlushFromSession(key, sess)
		}
		l.flushSessionSkill(key, sess)
		sess.Clear()
		_ = l.sessions.Save(sess)
		l.sessions.Invalidate(key)
		if l.lcm != nil {
			_ = l.lcm.ClearSession(key)
		}
		return &bus.OutboundMessage{Channel: msg.Channel, ChatID: msg.ChatID, Content: "New session started."}, nil
	}
	if cmd == "/help" {
		return &bus.OutboundMessage{Channel: msg.Channel, ChatID: msg.ChatID, Content: "🐕 dipper-bot commands:\n/new — Start new conversation\n/help — Show commands"}, nil
	}

	l.setRuntimeToolContext(msg.Channel, msg.ChatID, key)

	if l.consolidator != nil {
		if l.maintainer != nil {
			l.maintainer.FlushFromSession(key, sess)
		}
		l.consolidator.MaybeConsolidateByTokens(ctx, sess, msg.Channel, msg.ChatID, key)
	}

	useConsolidated := l.contextWindow > 0
	maxHist := l.memWindow
	if useConsolidated {
		maxHist = 0 // token-based: get all unconsolidated (consolidation keeps context small)
	}
	history := sess.GetHistory(maxHist, useConsolidated)
	if l.lcm != nil {
		fullHistory := sess.GetHistory(999999, false) // bootstrap with full session for LCM
		_ = l.lcm.Bootstrap(key, fullHistory)
	}
	// Inline PDF/Office attachments (e.g. web "[附件] uploads/x.pdf") as Markdown so the model sees content without a read_file round-trip.
	userContent := tools.ExpandDocumentsInUserMessage(ctx, l.workspace, msg.Content, l.maxEmbeddedAttachmentBytes)
	msgs, err := l.context.BuildMessages(ctx, history, userContent, msg.Channel, msg.ChatID, key)
	if err != nil {
		return nil, err
	}
	msgs = injectMemoryNudgeAfterSystem(msgs, l.exp, sess)

	// Convert to ChatRequest format
	req := &providers.ChatRequest{
		Model:           l.model,
		MaxTokens:       l.maxTok,
		Temperature:     l.temp,
		ReasoningEffort: l.reasoningEffort,
		Messages:        msgs,
		Tools:           toolDefsFromRegistry(l.registry),
	}

	finalContent, toolNames, err := l.runAgentLoop(ctx, req, key, userContent, onProgress)
	if err != nil {
		return nil, err
	}
	if finalContent == "" {
		finalContent = "I've completed but have no response to give."
	}
	if digest := l.takePendingLearnerBlock(key); digest != "" {
		finalContent = digest + finalContent
	}

	sess.AddMessage("user", msg.Content, nil)
	sess.AddMessage("assistant", finalContent, toolNames)
	_ = l.sessions.Save(sess)

	if l.lcm != nil {
		_ = l.lcm.IngestTurn(ctx, key, []map[string]string{
			{"role": "user", "content": userContent},
			{"role": "assistant", "content": finalContent},
		})
	}

	if l.maintainer != nil {
		l.maintainer.Enqueue(key, msg.Content, finalContent)
	}
	if l.skillMaintainer != nil {
		l.skillMaintainer.Enqueue(key, msg.Content, finalContent, toolNames)
	}

	return &bus.OutboundMessage{Channel: msg.Channel, ChatID: msg.ChatID, Content: finalContent}, nil
}

func toolCallDetail(name string, args map[string]any) string {
	if name != "exec" || args == nil {
		return ""
	}
	if cmd, ok := args["command"].(string); ok && cmd != "" {
		return cmd
	}
	return ""
}

func toolDefsFromRegistry(reg *tools.Registry) []providers.ToolDef {
	defs := reg.Definitions()
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

func (l *AgentLoop) runAgentLoop(ctx context.Context, req *providers.ChatRequest, sessionKey, turnUserContent string, onProgress func(toolName, detail string)) (string, []string, error) {
	messages := req.Messages
	usedTools := make([]string, 0, 8)
	resolvedModel := config.ResolveModelForAPI(l.providerName, req.Model)
	resolvedTemp := config.GetModelTemperature(l.providerName, req.Model, req.Temperature)
	skillItersWithoutManage := 0
	skillNotices := make([]*SkillApplyNotice, 0, 2)
	for iter := 0; iter < l.maxIter; iter++ {
		resp, err := l.provider.Chat(ctx, &providers.ChatRequest{
			Model:           resolvedModel,
			MaxTokens:       req.MaxTokens,
			Temperature:     resolvedTemp,
			ReasoningEffort: req.ReasoningEffort,
			Messages:        messages,
			Tools:           req.Tools,
		})
		if err != nil {
			return "", nil, err
		}

		MaybeRecordUsage(l.workspace, l.exp, BuildUsageEvent(sessionKey, "primary", "", l.providerName, resolvedModel, iter, resp))

		if !resp.HasToolCalls() {
			out := resp.Content
			if len(skillNotices) > 0 {
				out += formatSkillFeedbackBlock(skillNotices)
			}
			return out, usedTools, nil
		}

		// Append assistant message with tool calls
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
			usedTools = append(usedTools, tc.Name)
			detail := toolCallDetail(tc.Name, tc.Arguments)
			if onProgress != nil {
				onProgress(tc.Name, detail)
			}
			var result string
			if key, isMCP := mcpToolDedupKey(tc.Name, tc.Arguments); isMCP {
				if prev, dup := mcpDedup[key]; dup {
					slog.Info("tool call skipped (mcp duplicate same turn)", "name", tc.Name)
					result = mcpDupNote + prev
				} else {
					var err error
					result, err = l.registry.Execute(ctx, tc.Name, tc.Arguments)
					if err != nil {
						result = "Error: " + err.Error()
					}
					mcpDedup[key] = result
					slog.Info("tool call", "name", tc.Name)
				}
			} else {
				var err error
				result, err = l.registry.Execute(ctx, tc.Name, tc.Arguments)
				if err != nil {
					result = "Error: " + err.Error()
				}
				slog.Info("tool call", "name", tc.Name)
			}
			messages = AddToolResult(messages, tc.ID, tc.Name, result)
		}
		roundTools := make([]string, 0, len(resp.ToolCalls))
		for _, tc := range resp.ToolCalls {
			roundTools = append(roundTools, tc.Name)
		}
		if l.skillMaintainer != nil {
			if n := l.skillMaintainer.MaybeReflectMidRun(ctx, sessionKey, iter, turnUserContent, resp.Content, roundTools); n != nil {
				skillNotices = append(skillNotices, n)
			}
		}
		skillTh := effectiveSkillNudgeEvery(l.exp)
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
		reflect := "Continue this round with tool calls if more work remains, or one final answer if done—do not reply with only a plan or idle text when tools would help. (2) No mid-flight strategy polls; reasonable defaults + tools until done or exhausted. (3) Do not use web_search or web_fetch unless the user explicitly asked to search the web, look something up online, or fetch a URL they gave—never by default. (4) Real browser/UI work: if mcp_* tools are in your tool list and the user asked to drive Chrome (open site, click, screenshot, logged-in pages), call those mcp tools now—web_fetch is not a substitute. (5) Cost order otherwise: local/memory → narrow shell/edits; avoid redundant large reads and unnecessary spawn. (6) If you must stop without success: what you tried, why blocked, then 2–4 labeled options (A/B/…) + tradeoffs. (7) If success: one clear final answer. (8) Apply every round; a skill_manage reminder may prepend when configured. (1) Only if still genuinely ambiguous: one brief clarifying question before more heavy tools—not every round."
		if skillTh > 0 && skillItersWithoutManage >= skillTh {
			reflect = "[Reminder] Consider updating workspace skills via skill_manage when patterns repeat.\n\n" + reflect
			skillItersWithoutManage = 0
		}
		messages = append(messages, providers.Message{Role: "user", Content: reflect})
	}
	out := "Max iterations reached."
	if len(skillNotices) > 0 {
		out += formatSkillFeedbackBlock(skillNotices)
	}
	return out, usedTools, nil
}

// cancelSessionRun cancels any running process for the session. New message aborts previous.
func (l *AgentLoop) cancelSessionRun(key string) {
	l.sessionCancelsMu.Lock()
	run, ok := l.sessionCancels[key]
	if ok {
		l.flushSessionMemory(key)
		l.flushSessionSkillByKey(key)
		delete(l.sessionCancels, key)
		run.cancel()
	}
	l.sessionCancelsMu.Unlock()
}

func (l *AgentLoop) flushSessionMemory(key string) {
	if l.maintainer == nil || key == "" {
		return
	}
	sess, err := l.sessions.GetOrCreate(key)
	if err != nil || sess == nil {
		return
	}
	l.maintainer.FlushFromSession(key, sess)
}

func (l *AgentLoop) flushAllSessionMemories() {
	if l.maintainer == nil {
		return
	}
	infos, err := l.sessions.ListSessions(0)
	if err != nil {
		return
	}
	for _, info := range infos {
		l.flushSessionMemory(info.Key)
		l.flushSessionSkillByKey(info.Key)
	}
}

func (l *AgentLoop) flushSessionSkillByKey(key string) {
	if l.skillMaintainer == nil || key == "" {
		return
	}
	sess, err := l.sessions.GetOrCreate(key)
	if err != nil || sess == nil {
		return
	}
	l.flushSessionSkill(key, sess)
}

func (l *AgentLoop) flushSessionSkill(key string, sess *session.Session) {
	if l.skillMaintainer == nil || sess == nil {
		return
	}
	userMsg, asstMsg, toolNames, ok := latestTurnWithTools(sess)
	if !ok {
		return
	}
	l.skillMaintainer.FlushFromTurn(key, userMsg, asstMsg, toolNames)
}

func latestTurnWithTools(sess *session.Session) (userMsg, asstMsg string, toolNames []string, ok bool) {
	msgs := sess.GetMessagesFrom(0, 1<<20)
	for i := len(msgs) - 1; i >= 0; i-- {
		if msgs[i].Role != "assistant" {
			continue
		}
		asst := strings.TrimSpace(msgs[i].Content)
		if asst == "" {
			continue
		}
		for j := i - 1; j >= 0; j-- {
			if msgs[j].Role == "user" {
				user := strings.TrimSpace(msgs[j].Content)
				if user != "" {
					return user, asst, msgs[i].ToolsUsed, true
				}
				break
			}
		}
	}
	return "", "", nil, false
}

func (l *AgentLoop) setupSessionRun(parent context.Context, key string) (context.Context, *sessionRun) {
	var execCtx context.Context
	var cancel context.CancelFunc
	if l.runTimeoutSec > 0 {
		execCtx, cancel = context.WithTimeout(parent, time.Duration(l.runTimeoutSec)*time.Second)
	} else {
		execCtx, cancel = context.WithCancel(parent)
	}
	run := &sessionRun{cancel: cancel}
	l.sessionCancelsMu.Lock()
	l.sessionCancels[key] = run
	l.sessionCancelsMu.Unlock()
	return execCtx, run
}

func (l *AgentLoop) finishSessionRun(key string, run *sessionRun) {
	l.sessionCancelsMu.Lock()
	if l.sessionCancels[key] == run {
		delete(l.sessionCancels, key)
	}
	l.sessionCancelsMu.Unlock()
}

func (l *AgentLoop) handleRunFailure(ctx context.Context, m *bus.InboundMessage, sk string, err error) {
	l.flushSessionMemory(sk)
	l.flushSessionSkillByKey(sk)
	if errors.Is(err, context.Canceled) {
		slog.Info("run aborted by new message", "session", sk)
		return
	}
	if errors.Is(err, context.DeadlineExceeded) {
		slog.Info("run timed out", "session", sk)
		_ = l.bus.PublishOutbound(ctx, &bus.OutboundMessage{Channel: m.Channel, ChatID: m.ChatID, Content: "Request timed out."})
		return
	}
	slog.Error("process message", "error", err)
	_ = l.bus.PublishOutbound(ctx, &bus.OutboundMessage{Channel: m.Channel, ChatID: m.ChatID, Content: "Sorry, an error occurred."})
}

func (l *AgentLoop) handleInboundMessage(ctx context.Context, m *bus.InboundMessage) {
	key := m.SessionKey()
	l.cancelSessionRun(key)
	execCtx, run := l.setupSessionRun(ctx, key)
	go func(msg *bus.InboundMessage, sk string, runCtx context.Context, myRun *sessionRun) {
		defer myRun.cancel()
		l.finishSessionRun(sk, myRun)
		defer l.flushSessionMemory(sk)
		defer l.flushSessionSkillByKey(sk)
		resp, err := l.processMessage(runCtx, msg, "", nil)
		if err != nil {
			l.handleRunFailure(ctx, msg, sk, err)
			return
		}
		if resp != nil {
			_ = l.bus.PublishOutbound(ctx, resp)
		}
	}(m, key, execCtx, run)
}

// Run consumes from the bus and processes messages. Call in a goroutine.
// When a new message arrives for the same session, the previous run is cancelled.
func (l *AgentLoop) Run(ctx context.Context) {
	l.mu.Lock()
	l.running = true
	l.mu.Unlock()
	defer func() {
		l.mu.Lock()
		l.running = false
		l.mu.Unlock()
	}()

	for {
		l.maybeRunGovernanceAudit()
		msg, err := l.bus.ConsumeInboundWithTimeout(ctx, time.Second)
		if err != nil {
			l.flushAllSessionMemories()
			return
		}
		if msg == nil {
			continue
		}
		l.handleInboundMessage(ctx, msg)
	}
}

func (l *AgentLoop) maybeRunGovernanceAudit() {
	if l.governance == nil {
		return
	}
	if !l.lastAudit.IsZero() && time.Since(l.lastAudit) < 30*time.Minute {
		return
	}
	l.governance.AuditNow()
	l.lastAudit = time.Now()
}

func trimLower(s string) string {
	return strings.TrimSpace(strings.ToLower(s))
}

func normalizeDirectRoute(sessionKey, channel, chatID string) (string, string, string) {
	if sessionKey == "" {
		sessionKey = "cli:direct"
	}
	if channel == "" {
		channel = "cli"
	}
	if chatID == "" {
		chatID = "direct"
	}
	return sessionKey, channel, chatID
}

func (l *AgentLoop) setRuntimeToolContext(channel, chatID, sessionKey string) {
	if mt := l.registry.Get("message"); mt != nil {
		if m, ok := mt.(*tools.MessageTool); ok {
			m.SetContext(channel, chatID)
		}
	}
	if ct := l.registry.Get("cron"); ct != nil {
		if c, ok := ct.(*tools.CronTool); ok {
			c.SetContext(channel, chatID)
		}
	}
	if st := l.registry.Get("spawn"); st != nil {
		if s, ok := st.(*tools.SpawnTool); ok {
			s.SetContext(channel, chatID)
		}
	}
	if sl := l.registry.Get("sessions_list"); sl != nil {
		if s, ok := sl.(*tools.SessionsListTool); ok {
			s.SetContext(channel, chatID)
		}
	}
	if ss := l.registry.Get("sessions_send"); ss != nil {
		if s, ok := ss.(*tools.SessionsSendTool); ok {
			s.SetContext(channel, chatID)
		}
	}
	if l.lcm != nil {
		if gt := l.registry.Get("lcm_grep"); gt != nil {
			if g, ok := gt.(*tools.LcmGrepTool); ok {
				g.SetSessionKey(sessionKey)
			}
		}
		if dt := l.registry.Get("lcm_describe"); dt != nil {
			if d, ok := dt.(*tools.LcmDescribeTool); ok {
				d.SetSessionKey(sessionKey)
			}
		}
	}
	if st := l.registry.Get("session_search"); st != nil {
		if s, ok := st.(*tools.SessionSearchTool); ok {
			s.SetSessionKey(sessionKey)
		}
	}
}
