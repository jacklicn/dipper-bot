package config

// LCMConfig holds Lossless Context Management settings (lossless-claw style).
type LCMConfig struct {
	// Enabled: nil = true (LCM on by default). Explicit *false disables LCM and lcm_* tools.
	Enabled *bool `json:"enabled,omitempty"`
	DatabasePath          string  `json:"databasePath,omitempty"`
	ContextThreshold      float64 `json:"contextThreshold"`
	FreshTailCount        int     `json:"freshTailCount"`
	LeafMinFanout         int     `json:"leafMinFanout"`
	CondensedMinFanout    int     `json:"condensedMinFanout"`
	LeafChunkTokens       int     `json:"leafChunkTokens"`
	LeafTargetTokens      int     `json:"leafTargetTokens"`
	CondensedTargetTokens int     `json:"condensedTargetTokens"`
	IncrementalMaxDepth   int     `json:"incrementalMaxDepth"`
}

// LCMEnabled reports whether LCM is active (default true when Enabled is nil).
func (l LCMConfig) LCMEnabled() bool {
	if l.Enabled != nil {
		return *l.Enabled
	}
	return true
}

// MemoryMaintenanceConfig controls background memory maintenance.
type MemoryMaintenanceConfig struct {
	// Enabled: nil = true (default on). Explicit *false disables the memory maintainer.
	Enabled *bool `json:"enabled,omitempty"`
	QueueSize                 int     `json:"queueSize,omitempty"`
	MinUserChars              int     `json:"minUserChars,omitempty"`
	MinAssistantChars         int     `json:"minAssistantChars,omitempty"`
	NudgeInterval             int     `json:"nudgeInterval,omitempty"`             // Run at most every N completed turns per session.
	MinIntervalMinutes        int     `json:"minIntervalMinutes,omitempty"`        // Minimum wall-clock interval between runs per session.
	FlushMinTurns             int     `json:"flushMinTurns,omitempty"`             // Flush memory on reset/new after at least N user turns.
	MinQualityScore           int     `json:"minQualityScore,omitempty"`           // Candidate score threshold.
	RepeatSuppressionMinutes  int     `json:"repeatSuppressionMinutes,omitempty"`  // Suppress near-duplicate decisions in short window.
	FailureBackoffMinutes     int     `json:"failureBackoffMinutes,omitempty"`     // Pause after repeated failures.
	MinConfidence             int     `json:"minConfidence,omitempty"`             // LLM self-rated confidence threshold [0,100].
	ControllerTargetBadRate   float64 `json:"controllerTargetBadRate,omitempty"`   // PID target bad-event ratio [0,1].
	ControllerKp              float64 `json:"controllerKp,omitempty"`              // PID proportional gain.
	ControllerKi              float64 `json:"controllerKi,omitempty"`              // PID integral gain.
	ControllerKd              float64 `json:"controllerKd,omitempty"`              // PID derivative gain.
	ControllerBatchSize       int     `json:"controllerBatchSize,omitempty"`       // Adapt after N events.
	ControllerMinFloor        int     `json:"controllerMinFloor,omitempty"`        // Minimum quality/confidence floor for adaptation.
	ControllerMaxFloor        int     `json:"controllerMaxFloor,omitempty"`        // Maximum quality/confidence floor for adaptation.
	ControllerOnlineTuning    *bool   `json:"controllerOnlineTuning,omitempty"`    // When true, gently retunes PID gains at runtime.
	SemanticRegroupMaxEntries int     `json:"semanticRegroupMaxEntries,omitempty"` // Cap items when refining topic clusters from telemetry.
	SemanticRegroupMaxGroups  int     `json:"semanticRegroupMaxGroups,omitempty"`  // Max topic groups for semantic regroup pass.
}

// MaintenanceEnabled reports whether background memory maintenance runs (default true when Enabled is nil).
func (m MemoryMaintenanceConfig) MaintenanceEnabled() bool {
	if m.Enabled != nil {
		return *m.Enabled
	}
	return true
}

// SkillsEvolutionConfig controls autonomous skill evolution loop.
type SkillsEvolutionConfig struct {
	// Enabled: nil = true (default on). Explicit *false disables SkillMaintainer.
	Enabled *bool `json:"enabled,omitempty"`
	CreationNudgeInterval    int     `json:"creationNudgeInterval,omitempty"`    // Consider skill evolution every N completed turns per session.
	MinToolCalls             int     `json:"minToolCalls,omitempty"`             // Require at least N tool calls in the turn.
	MinIntervalMinutes       int     `json:"minIntervalMinutes,omitempty"`       // Minimum wall-clock interval between runs per session.
	MinQualityScore          int     `json:"minQualityScore,omitempty"`          // Candidate score threshold.
	FlushMinToolCalls        int     `json:"flushMinToolCalls,omitempty"`        // Flush on lifecycle events only if at least N tools in latest turn.
	RepeatSuppressionMinutes int     `json:"repeatSuppressionMinutes,omitempty"` // Suppress near-duplicate decisions in short window.
	FailureBackoffMinutes    int     `json:"failureBackoffMinutes,omitempty"`    // Pause after repeated failures.
	MinConfidence            int     `json:"minConfidence,omitempty"`            // LLM self-rated confidence threshold [0,100].
	ControllerTargetBadRate  float64 `json:"controllerTargetBadRate,omitempty"`  // PID target bad-event ratio [0,1].
	ControllerKp             float64 `json:"controllerKp,omitempty"`             // PID proportional gain.
	ControllerKi             float64 `json:"controllerKi,omitempty"`             // PID integral gain.
	ControllerKd             float64 `json:"controllerKd,omitempty"`             // PID derivative gain.
	ControllerBatchSize      int     `json:"controllerBatchSize,omitempty"`      // Adapt after N events.
	ControllerMinFloor       int     `json:"controllerMinFloor,omitempty"`       // Minimum quality/confidence floor for adaptation.
	ControllerMaxFloor       int     `json:"controllerMaxFloor,omitempty"`       // Maximum quality/confidence floor for adaptation.
	ControllerOnlineTuning   *bool   `json:"controllerOnlineTuning,omitempty"`   // When true, gently retunes PID gains at runtime.
	// MidRunReflectEveryToolIters: nil = 4 (after every N completed tool-loop iterations within one user turn, run a skill reflect pass). &0 disables mid-run reflects (post-turn Enqueue/Flush unchanged).
	MidRunReflectEveryToolIters *int `json:"midRunReflectEveryToolIters,omitempty"`
	// MidRunReflectMinSeconds: min wall-clock gap between mid-run reflects per session; 0 = default 120 after LoadConfig.
	MidRunReflectMinSeconds int `json:"midRunReflectMinSeconds,omitempty"`
}

// MidRunReflectEvery returns N tool-loop iterations between mid-run skill reflects; 0 = disabled.
func (s SkillsEvolutionConfig) MidRunReflectEvery() int {
	if s.MidRunReflectEveryToolIters == nil {
		return 4
	}
	v := *s.MidRunReflectEveryToolIters
	if v <= 0 {
		return 0
	}
	return v
}

// MidRunReflectCooldownSeconds returns minimum seconds between mid-run reflects for one session.
func (s SkillsEvolutionConfig) MidRunReflectCooldownSeconds() int {
	if s.MidRunReflectMinSeconds <= 0 {
		return 120
	}
	return s.MidRunReflectMinSeconds
}

// EvolutionEnabled reports whether autonomous skill evolution runs (default true when Enabled is nil).
func (s SkillsEvolutionConfig) EvolutionEnabled() bool {
	if s.Enabled != nil {
		return *s.Enabled
	}
	return true
}

// UsagePricingOverrideEntry overrides per-1M-token rates when model id contains matchSubstring (case-insensitive).
// Amounts use the same unit as agents.defaults.experience.usageCostCurrency (CNY or USD).
type UsagePricingOverrideEntry struct {
	MatchSubstring   string  `json:"matchSubstring"`
	InputPerMillion  float64 `json:"inputPerMillion"`
	OutputPerMillion float64 `json:"outputPerMillion"`
}

// AgentExperienceConfig aligns UX with common “self-improving agent” patterns (Hermes-inspired):
// fenced recall block, in-prompt nudges, compaction pressure logs, per-call usage JSONL.
type AgentExperienceConfig struct {
	DisableFencedMemoryRecall bool `json:"disableFencedMemoryRecall,omitempty"` // When false (default), wrap MEMORY/USER/NOTE in a <memory-context> fence in the system prompt.
	// MemoryPromptNudgeEvery: nil = default 10 user turns; &0 = off; &N = inject a reminder every N user turns without the memory tool (USER.md/NOTE.md). Internal MemoryConsolidator uses a separate save_memory tool name and is not shown to the main agent.
	MemoryPromptNudgeEvery *int `json:"memoryPromptNudgeEvery,omitempty"`
	// SkillPromptNudgeEvery: nil = default 10 (same cadence idea as memory nudge); &0 = off; &N = after N tool iterations without skill_manage, inject a reminder.
	SkillPromptNudgeEvery *int `json:"skillPromptNudgeEvery,omitempty"`
	DisableCompactionPressureLogs bool `json:"disableCompactionPressureLogs,omitempty"` // When false (default) and contextWindowTokens>0, log estimated fill vs window before consolidation.
	DisableUsageRecording         bool `json:"disableUsageRecording,omitempty"`         // When false (default), append provider usage rows to memory/usage_events.jsonl.
	// UsageCostCurrency: CNY (default) or USD — unit for usage_insights cost_estimate and for usagePricingOverrides / memory/pricing_overrides.json per-1M rates.
	UsageCostCurrency string `json:"usageCostCurrency,omitempty"`
	// DefaultUsdToCny: when usageCostCurrency is CNY, multiply the built-in USD/M heuristic table by this. 0 = loader applies default (7.2).
	DefaultUsdToCny float64 `json:"defaultUsdToCny,omitempty"`
	// UsagePricingOverrides: merged with memory/pricing_overrides.json (config first); then built-in USD/M table (× defaultUsdToCny when currency is CNY).
	UsagePricingOverrides []UsagePricingOverrideEntry `json:"usagePricingOverrides,omitempty"`
	// LearnerFeedbackInstantPush: when true (default), async skill/memory maintainer writes also PublishOutbound immediately (second bubble on chat channels). When false, feedback is only prepended to the next assistant reply so CLI and channels see the same text.
	LearnerFeedbackInstantPush *bool `json:"learnerFeedbackInstantPush,omitempty"`
}

// AgentDefaults holds default agent settings.
type AgentDefaults struct {
	Workspace           string                  `json:"workspace"`
	Model               string                  `json:"model"`
	MaxTokens           int                     `json:"maxTokens"`
	Temperature         float64                 `json:"temperature"`
	MaxToolIterations   int                     `json:"maxToolIterations"`
	MemoryWindow        int                     `json:"memoryWindow"`
	ContextWindowTokens int                     `json:"contextWindowTokens,omitempty"` // Token-based memory: when >0, consolidate when prompt exceeds this; 0 = use memoryWindow only
	ReasoningEffort     string                  `json:"reasoningEffort,omitempty"`     // low/medium/high — enables thinking mode for supported models
	RunTimeoutSec       int                     `json:"runTimeoutSec,omitempty"`       // 0 = no timeout; new message for same session cancels previous
	LCM                 LCMConfig               `json:"lcm,omitempty"`
	MemoryMaintenance   MemoryMaintenanceConfig `json:"memoryMaintenance,omitempty"`
	SkillsEvolution     SkillsEvolutionConfig   `json:"skillsEvolution,omitempty"`
	Experience          AgentExperienceConfig   `json:"experience,omitempty"`
	Attachments         AttachmentConfig        `json:"attachments,omitempty"` // inline PDF/Office embed budget for “[附件]” lines
}

// AttachmentConfig controls inline embedding of uploaded documents (e.g. web "[附件]") before the LLM call.
type AttachmentConfig struct {
	// MaxEmbeddedBytes caps total MarkDown bytes appended per user message from PDF/Office conversion. 0 = use default (400000).
	MaxEmbeddedBytes int `json:"maxEmbeddedBytes,omitempty"`
}

// AgentsConfig holds agent configuration.
type AgentsConfig struct {
	Defaults AgentDefaults `json:"defaults"`
}

// ProviderConfig holds a single LLM provider's API settings.
type ProviderConfig struct {
	APIKey       string            `json:"apiKey"`
	APIBase      string            `json:"apiBase"`
	ExtraHeaders map[string]string `json:"extraHeaders"` // null when not set
}

// GetAPIBase returns the API base URL, or empty string if not set.
func (p *ProviderConfig) GetAPIBase() string {
	if p != nil {
		return p.APIBase
	}
	return ""
}

// TranscriptionConfig holds voice transcription settings (STT).
type TranscriptionConfig struct {
	Provider string `json:"provider,omitempty"` // "groq" (cloud) or "vosk" (local)
	VoskURL  string `json:"voskUrl,omitempty"`  // Vosk WebSocket URL, e.g. "ws://localhost:2700"
}

// ProvidersConfig holds provider config.
type ProvidersConfig struct {
	Custom        ProviderConfig      `json:"custom"`
	Groq          ProviderConfig      `json:"groq"` // Groq API key for Whisper voice transcription
	Transcription TranscriptionConfig `json:"transcription,omitempty"`
}

// GatewayConfig holds gateway server settings.
type GatewayConfig struct {
	Host                string   `json:"host"`
	Port                int      `json:"port"`
	RateLimitPerMinute  int      `json:"rateLimitPerMinute,omitempty"`  // 0 = default (120 req/min)
	RateLimitIPv4Prefix int      `json:"rateLimitIPv4Prefix,omitempty"` // 0 = default (/32)
	RateLimitIPv6Prefix int      `json:"rateLimitIPv6Prefix,omitempty"` // 0 = default (/128)
	RateLimitCIDRs      []string `json:"rateLimitCidrs,omitempty"`      // Optional CIDR buckets
}

// WebSearchConfig holds web search tool config.
// Provider: duckduckgo (default), brave, tavily, jina, searxng.
type WebSearchConfig struct {
	Provider   string `json:"provider,omitempty"` // duckduckgo (default), brave, tavily, jina, searxng
	APIKey     string `json:"apiKey,omitempty"`
	BaseURL    string `json:"baseUrl,omitempty"` // SearXNG base URL, e.g. https://searx.example
	MaxResults int    `json:"maxResults"`
}

// WebToolsConfig holds web tools config.
type WebToolsConfig struct {
	Search WebSearchConfig `json:"search"`
	Proxy  string          `json:"proxy,omitempty"` // HTTP/SOCKS5 proxy URL, e.g. "http://127.0.0.1:7890" or "socks5://127.0.0.1:1080"
}

// ExecToolConfig holds shell exec tool config.
type ExecToolConfig struct {
	Timeout int `json:"timeout"`
}

// MCPServerConfig holds MCP server connection (stdio or HTTP).
type MCPServerConfig struct {
	Command      string            `json:"command,omitempty"`
	Args         []string          `json:"args,omitempty"`
	Env          map[string]string `json:"env,omitempty"`
	URL          string            `json:"url,omitempty"`
	Headers      map[string]string `json:"headers,omitempty"`      // Custom auth headers for HTTP/SSE
	ToolTimeout  int               `json:"toolTimeout,omitempty"`  // Seconds before tool call is cancelled (default 30)
	EnabledTools []string          `json:"enabledTools,omitempty"` // Only register these tools; ["*"] = all; [] = none
}

// ToolsConfig holds tools configuration.
type ToolsConfig struct {
	Web                 WebToolsConfig             `json:"web"`
	Exec                ExecToolConfig             `json:"exec"`
	RestrictToWorkspace *bool                      `json:"restrictToWorkspace,omitempty"` // default true when omitted
	MCPServers          map[string]MCPServerConfig `json:"mcpServers,omitempty"`
}

// RestrictToWorkspaceEnabled reports whether file/exec paths are limited to the workspace.
// When the field is omitted from JSON, this returns true.
func (t ToolsConfig) RestrictToWorkspaceEnabled() bool {
	if t.RestrictToWorkspace == nil {
		return true
	}
	return *t.RestrictToWorkspace
}

// TelegramConfig holds Telegram channel config.
type TelegramConfig struct {
	Enabled   bool     `json:"enabled"`
	Token     string   `json:"token,omitempty"`
	Proxy     string   `json:"proxy,omitempty"` // HTTP/SOCKS5 proxy, e.g. "http://127.0.0.1:7890"
	AllowFrom []string `json:"allowFrom,omitempty"`
}

// WhatsAppConfig holds WhatsApp bridge config.
type WhatsAppConfig struct {
	Enabled     bool     `json:"enabled"`
	BridgeURL   string   `json:"bridgeUrl,omitempty"`
	BridgeToken string   `json:"bridgeToken,omitempty"`
	AllowFrom   []string `json:"allowFrom,omitempty"`
}

// DiscordConfig holds Discord channel config.
type DiscordConfig struct {
	Enabled    bool     `json:"enabled"`
	Token      string   `json:"token,omitempty"`
	AllowFrom  []string `json:"allowFrom,omitempty"`
	GatewayURL string   `json:"gatewayUrl,omitempty"`
	Intents    int      `json:"intents,omitempty"`
}

// FeishuConfig holds Feishu/Lark channel config (WebSocket).
type FeishuConfig struct {
	Enabled           bool     `json:"enabled"`
	AppID             string   `json:"appId,omitempty"`
	AppSecret         string   `json:"appSecret,omitempty"`
	EncryptKey        string   `json:"encryptKey,omitempty"`
	VerificationToken string   `json:"verificationToken,omitempty"`
	AllowFrom         []string `json:"allowFrom,omitempty"`
}

// DingTalkConfig holds DingTalk channel config (Stream mode).
type DingTalkConfig struct {
	Enabled      bool     `json:"enabled"`
	ClientID     string   `json:"clientId,omitempty"`
	ClientSecret string   `json:"clientSecret,omitempty"`
	AllowFrom    []string `json:"allowFrom,omitempty"`
}

// EmailConfig holds Email channel config (IMAP + SMTP).
type EmailConfig struct {
	Enabled          bool     `json:"enabled"`
	ConsentGranted   bool     `json:"consentGranted"`
	IMAPHost         string   `json:"imapHost,omitempty"`
	IMAPPort         int      `json:"imapPort,omitempty"`
	IMAPUsername     string   `json:"imapUsername,omitempty"`
	IMAPPassword     string   `json:"imapPassword,omitempty"`
	IMAPMailbox      string   `json:"imapMailbox,omitempty"`
	IMAPUseSSL       bool     `json:"imapUseSsl"`
	SMTPHost         string   `json:"smtpHost,omitempty"`
	SMTPPort         int      `json:"smtpPort,omitempty"`
	SMTPUsername     string   `json:"smtpUsername,omitempty"`
	SMTPPassword     string   `json:"smtpPassword,omitempty"`
	SMTPUseTLS       bool     `json:"smtpUseTls"`
	SMTPUseSSL       bool     `json:"smtpUseSsl"`
	FromAddress      string   `json:"fromAddress,omitempty"`
	AutoReplyEnabled bool     `json:"autoReplyEnabled"`
	PollIntervalSec  int      `json:"pollIntervalSeconds,omitempty"`
	MarkSeen         bool     `json:"markSeen"`
	MaxBodyChars     int      `json:"maxBodyChars,omitempty"`
	SubjectPrefix    string   `json:"subjectPrefix,omitempty"`
	AllowFrom        []string `json:"allowFrom,omitempty"`
}

// MochatMentionConfig holds Mochat mention behavior.
type MochatMentionConfig struct {
	RequireInGroups bool `json:"requireInGroups"`
}

// MochatGroupRule holds per-group mention rule.
type MochatGroupRule struct {
	RequireMention bool `json:"requireMention"`
}

// MochatConfig holds Mochat channel config.
type MochatConfig struct {
	Enabled                   bool                       `json:"enabled"`
	BaseURL                   string                     `json:"baseUrl,omitempty"`
	SocketURL                 string                     `json:"socketUrl,omitempty"`
	SocketPath                string                     `json:"socketPath,omitempty"`
	SocketDisableMsgpack      bool                       `json:"socketDisableMsgpack"`
	SocketReconnectDelayMs    int                        `json:"socketReconnectDelayMs,omitempty"`
	SocketMaxReconnectDelayMs int                        `json:"socketMaxReconnectDelayMs,omitempty"`
	SocketConnectTimeoutMs    int                        `json:"socketConnectTimeoutMs,omitempty"`
	RefreshIntervalMs         int                        `json:"refreshIntervalMs,omitempty"`
	WatchTimeoutMs            int                        `json:"watchTimeoutMs,omitempty"`
	WatchLimit                int                        `json:"watchLimit,omitempty"`
	RetryDelayMs              int                        `json:"retryDelayMs,omitempty"`
	MaxRetryAttempts          int                        `json:"maxRetryAttempts,omitempty"`
	ClawToken                 string                     `json:"clawToken,omitempty"`
	AgentUserID               string                     `json:"agentUserId,omitempty"`
	Sessions                  []string                   `json:"sessions,omitempty"`
	Panels                    []string                   `json:"panels,omitempty"`
	AllowFrom                 []string                   `json:"allowFrom,omitempty"`
	Mention                   MochatMentionConfig        `json:"mention"`
	Groups                    map[string]MochatGroupRule `json:"groups,omitempty"`
	ReplyDelayMode            string                     `json:"replyDelayMode,omitempty"`
	ReplyDelayMs              int                        `json:"replyDelayMs,omitempty"`
}

// SlackDMConfig holds Slack DM policy.
type SlackDMConfig struct {
	Enabled   bool     `json:"enabled"`
	Policy    string   `json:"policy,omitempty"` // "open" or "allowlist"
	AllowFrom []string `json:"allowFrom,omitempty"`
}

// SlackConfig holds Slack channel config.
type SlackConfig struct {
	Enabled           bool          `json:"enabled"`
	Mode              string        `json:"mode,omitempty"`
	WebhookPath       string        `json:"webhookPath,omitempty"`
	BotToken          string        `json:"botToken,omitempty"`
	AppToken          string        `json:"appToken,omitempty"`
	UserTokenReadOnly bool          `json:"userTokenReadOnly"`
	ReplyInThread     bool          `json:"replyInThread"`
	ReactEmoji        string        `json:"reactEmoji,omitempty"`
	AllowFrom         []string      `json:"allowFrom,omitempty"`
	GroupPolicy       string        `json:"groupPolicy,omitempty"`
	GroupAllowFrom    []string      `json:"groupAllowFrom,omitempty"`
	DM                SlackDMConfig `json:"dm"`
}

// QQConfig holds QQ channel config (botpy SDK).
type QQConfig struct {
	Enabled   bool     `json:"enabled"`
	AppID     string   `json:"appId,omitempty"`
	Secret    string   `json:"secret,omitempty"`
	AllowFrom []string `json:"allowFrom,omitempty"`
}

// WecomConfig holds WeCom (Enterprise WeChat) AI Bot channel config.
// Uses WebSocket long connection — no public IP required.
type WecomConfig struct {
	Enabled        bool     `json:"enabled"`
	BotID          string   `json:"botId,omitempty"`
	Secret         string   `json:"secret,omitempty"`
	AllowFrom      []string `json:"allowFrom,omitempty"`
	WelcomeMessage string   `json:"welcomeMessage,omitempty"`
}

// ChannelsConfig holds all channel configs.
type ChannelsConfig struct {
	WhatsApp WhatsAppConfig `json:"whatsapp"`
	Telegram TelegramConfig `json:"telegram"`
	Discord  DiscordConfig  `json:"discord"`
	Feishu   FeishuConfig   `json:"feishu"`
	Mochat   MochatConfig   `json:"mochat"`
	DingTalk DingTalkConfig `json:"dingtalk"`
	Email    EmailConfig    `json:"email"`
	Slack    SlackConfig    `json:"slack"`
	QQ       QQConfig       `json:"qq"`
	Wecom    WecomConfig    `json:"wecom"`
	Webhook  WebhookConfig  `json:"webhook"` // Plugin-style: HTTP POST to inject messages
}

// WebhookConfig holds webhook channel config (plugin-style).
// External services POST to /message to inject messages; enables channel plugins.
type WebhookConfig struct {
	Enabled             bool     `json:"enabled"`
	Port                int      `json:"port,omitempty"` // HTTP server port (default 9000)
	Path                string   `json:"path,omitempty"` // Path for POST (default /message)
	AllowFrom           []string `json:"allowFrom,omitempty"`
	RateLimitPerMinute  int      `json:"rateLimitPerMinute,omitempty"`  // 0 = default (120 req/min)
	RateLimitIPv4Prefix int      `json:"rateLimitIPv4Prefix,omitempty"` // 0 = default (/32)
	RateLimitIPv6Prefix int      `json:"rateLimitIPv6Prefix,omitempty"` // 0 = default (/128)
	RateLimitCIDRs      []string `json:"rateLimitCidrs,omitempty"`      // Optional CIDR buckets
}

// LoggingConfig controls process-wide slog output (rotating file under workspace + optional stderr).
type LoggingConfig struct {
	// Enabled: nil = true (write rotating files under workspace/logs by default). Explicit false = stderr only.
	Enabled *bool `json:"enabled,omitempty"`
	// Level: debug, info, warn, error (default info).
	Level string `json:"level,omitempty"`
	// MaxAgeDays: delete rotated files older than this many days (default 7). Passed to lumberjack MaxAge.
	MaxAgeDays int `json:"maxAgeDays,omitempty"`
	// MaxSizeMB: rotate when the active file exceeds this many MiB (default 32).
	MaxSizeMB int `json:"maxSizeMB,omitempty"`
	// MaxBackups: optional cap on rotated files by count; 0 = prune by MaxAgeDays only.
	MaxBackups int `json:"maxBackups"`
	// FileOnly: when true, logs go only to the file (no stderr copy).
	FileOnly bool `json:"fileOnly"`
	// Dir: directory under workspace for log files (default "logs").
	Dir string `json:"dir,omitempty"`
	// Filename: base log file name inside Dir (default "dipper-bot.log").
	Filename string `json:"filename,omitempty"`
	// Compress: gzip rotated files (lumberjack).
	Compress bool `json:"compress"`
}

// FileLoggingEnabled reports whether rotating log files are written under the workspace (default true).
func (l LoggingConfig) FileLoggingEnabled() bool {
	if l.Enabled != nil {
		return *l.Enabled
	}
	return true
}

// Config is the root configuration.
type Config struct {
	Agents    AgentsConfig    `json:"agents"`
	Channels  ChannelsConfig  `json:"channels"`
	Providers ProvidersConfig `json:"providers"`
	Gateway   GatewayConfig   `json:"gateway"`
	Tools     ToolsConfig     `json:"tools"`
	Logging   LoggingConfig   `json:"logging,omitempty"`
}

// DefaultConfig returns a config with defaults.
func DefaultConfig() *Config {
	ctrlOnline := true
	memPromptNudge := 10
	skillPromptNudge := 10
	memMaintOn := true
	skillEvoOn := true
	lcmOn := true
	loggingOn := true
	restrictWS := true
	midRunSkillEvery := 4
	learnerPushOn := true
	return &Config{
		Logging: LoggingConfig{
			Enabled:    &loggingOn,
			Level:      "info",
			MaxAgeDays: 7,
			MaxSizeMB:  128,
			MaxBackups: 0,
			FileOnly:   false,
			Dir:        "logs",
			Filename:   "dipper-bot.log",
			Compress:   false,
		},
		Agents: AgentsConfig{
			Defaults: AgentDefaults{
				Workspace:         "~/.dipper-bot/workspace",
				Model:             "gpt-4",
				MaxTokens:         8192,
				Temperature:       0.7,
				MaxToolIterations: 20,
				MemoryWindow:      50,
				Experience: AgentExperienceConfig{
					MemoryPromptNudgeEvery: &memPromptNudge,
					SkillPromptNudgeEvery:  &skillPromptNudge,
					LearnerFeedbackInstantPush: &learnerPushOn,
					UsageCostCurrency:      "CNY",
					DefaultUsdToCny:        7.2,
				},
				LCM: LCMConfig{
					Enabled:               &lcmOn,
					ContextThreshold:      0.75,
					FreshTailCount:        32,
					LeafMinFanout:         8,
					CondensedMinFanout:    4,
					LeafChunkTokens:       20000,
					LeafTargetTokens:      1200,
					CondensedTargetTokens: 2000,
					IncrementalMaxDepth:   -1,
				},
				MemoryMaintenance: MemoryMaintenanceConfig{
					Enabled:                   &memMaintOn,
					QueueSize:                 64,
					MinUserChars:              40,
					MinAssistantChars:         40,
					NudgeInterval:             1,
					MinIntervalMinutes:        5,
					FlushMinTurns:             6,
					MinQualityScore:           50,
					RepeatSuppressionMinutes:  30,
					FailureBackoffMinutes:     15,
					MinConfidence:             60,
					ControllerTargetBadRate:   0.08,
					ControllerKp:              16.0,
					ControllerKi:              4.0,
					ControllerKd:              6.0,
					ControllerBatchSize:       8,
					ControllerMinFloor:        40,
					ControllerMaxFloor:        95,
					ControllerOnlineTuning:    &ctrlOnline,
					SemanticRegroupMaxEntries: 40,
					SemanticRegroupMaxGroups:  6,
				},
				SkillsEvolution: SkillsEvolutionConfig{
					Enabled:                  &skillEvoOn,
					CreationNudgeInterval:    15,
					MinToolCalls:             5,
					MinIntervalMinutes:       30,
					MinQualityScore:          60,
					FlushMinToolCalls:        5,
					RepeatSuppressionMinutes: 60,
					FailureBackoffMinutes:    30,
					MinConfidence:            60,
					ControllerTargetBadRate:  0.08,
					ControllerKp:             16.0,
					ControllerKi:             4.0,
					ControllerKd:             6.0,
					ControllerBatchSize:      8,
					ControllerMinFloor:       40,
					ControllerMaxFloor:       95,
					ControllerOnlineTuning:      &ctrlOnline,
					MidRunReflectEveryToolIters: &midRunSkillEvery,
					MidRunReflectMinSeconds:     120,
				},
			},
		},
		Gateway: GatewayConfig{Host: "0.0.0.0", Port: 8090, RateLimitPerMinute: 120, RateLimitIPv4Prefix: 32, RateLimitIPv6Prefix: 128},
		Channels: ChannelsConfig{
			WhatsApp: WhatsAppConfig{BridgeURL: "ws://127.0.0.1:3001"},
			Discord:  DiscordConfig{GatewayURL: "wss://gateway.discord.gg/?v=10&encoding=json", Intents: 37377},
			Webhook:  WebhookConfig{RateLimitPerMinute: 120, RateLimitIPv4Prefix: 32, RateLimitIPv6Prefix: 128},
		},
		Tools: ToolsConfig{
			Exec:                ExecToolConfig{Timeout: 60},
			Web:                 WebToolsConfig{Search: WebSearchConfig{MaxResults: 5}},
			RestrictToWorkspace: &restrictWS,
			MCPServers: map[string]MCPServerConfig{
				"chrome-devtools": {
					Command: "npx",
					Args: []string{
						"-y", "chrome-devtools-mcp@latest",
						"--no-performance-crux", "--no-usage-statistics",
					},
				},
			},
		},
	}
}
