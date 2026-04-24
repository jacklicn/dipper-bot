package config

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
)

// migrateConfig migrates old config formats. tools.exec.restrictToWorkspace → tools.restrictToWorkspace.
func migrateConfig(raw map[string]any) []byte {
	tools, _ := raw["tools"].(map[string]any)
	if tools != nil {
		exec, _ := tools["exec"].(map[string]any)
		if exec != nil {
			if v, ok := exec["restrictToWorkspace"]; ok {
				if _, has := tools["restrictToWorkspace"]; !has {
					tools["restrictToWorkspace"] = v
				}
				delete(exec, "restrictToWorkspace")
			}
		}
	}
	out, _ := json.Marshal(raw)
	return out
}

// GetConfigPath returns the default config file path (~/.dipper-bot/config.json).
func GetConfigPath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".dipper-bot", "config.json"), nil
}

// WorkspaceConfigPath returns the config file path inside a workspace (workspace/config.json).
// workspace is expanded (e.g. ~/path).
func WorkspaceConfigPath(workspace string) (string, error) {
	expanded, err := expandWorkspace(workspace)
	if err != nil {
		return "", err
	}
	return filepath.Join(expanded, "config.json"), nil
}

// GetWorkspaceFromDefaultConfig returns the workspace path from ~/.dipper-bot/config.json.
// Used to resolve which config to load: workspace path is stored in default config.
func GetWorkspaceFromDefaultConfig() (string, error) {
	p, err := GetConfigPath()
	if err != nil {
		return "", err
	}
	cfg, err := LoadConfig(p)
	if err != nil || cfg == nil {
		return "", nil
	}
	return cfg.Agents.Defaults.Workspace, nil
}

// ResolveConfigPath returns the config path with priority: workspace config > default.
// workspaceOverride: from --workspace flag. If empty, workspace is read from ~/.dipper-bot/config.json.
// If workspace is set and workspace/config.json exists, return it; else return ~/.dipper-bot/config.json.
func ResolveConfigPath(workspaceOverride string) (string, error) {
	workspace := workspaceOverride
	if workspace == "" {
		workspace, _ = GetWorkspaceFromDefaultConfig()
	}
	if workspace != "" {
		p, err := WorkspaceConfigPath(workspace)
		if err != nil {
			return GetConfigPath()
		}
		if _, err := os.Stat(p); err == nil {
			return p, nil
		}
	}
	return GetConfigPath()
}

// LoadConfig loads config from path or returns default.
func LoadConfig(configPath string) (*Config, error) {
	if configPath == "" {
		p, err := GetConfigPath()
		if err != nil {
			return DefaultConfig(), nil
		}
		configPath = p
	}
	data, err := os.ReadFile(configPath)
	if err != nil {
		if os.IsNotExist(err) {
			return DefaultConfig(), nil
		}
		return nil, err
	}
	var raw map[string]any
	if err := json.Unmarshal(data, &raw); err != nil {
		return DefaultConfig(), nil
	}
	migrated := migrateConfig(raw)
	var c Config
	if err := json.Unmarshal(migrated, &c); err != nil {
		return DefaultConfig(), nil
	}
	// Apply defaults for zero values
	def := DefaultConfig()
	if c.Agents.Defaults.Workspace == "" {
		c.Agents.Defaults.Workspace = def.Agents.Defaults.Workspace
	}
	if c.Agents.Defaults.Model == "" {
		c.Agents.Defaults.Model = def.Agents.Defaults.Model
	}
	if c.Agents.Defaults.MaxTokens == 0 {
		c.Agents.Defaults.MaxTokens = def.Agents.Defaults.MaxTokens
	}
	if c.Gateway.Port == 0 {
		c.Gateway.Port = def.Gateway.Port
	}
	if c.Gateway.RateLimitPerMinute == 0 {
		c.Gateway.RateLimitPerMinute = def.Gateway.RateLimitPerMinute
	}
	if c.Gateway.RateLimitIPv4Prefix == 0 {
		c.Gateway.RateLimitIPv4Prefix = def.Gateway.RateLimitIPv4Prefix
	}
	if c.Gateway.RateLimitIPv6Prefix == 0 {
		c.Gateway.RateLimitIPv6Prefix = def.Gateway.RateLimitIPv6Prefix
	}
	if len(c.Gateway.RateLimitCIDRs) == 0 && len(def.Gateway.RateLimitCIDRs) > 0 {
		c.Gateway.RateLimitCIDRs = append([]string{}, def.Gateway.RateLimitCIDRs...)
	}
	if c.Channels.Webhook.RateLimitPerMinute == 0 {
		c.Channels.Webhook.RateLimitPerMinute = def.Channels.Webhook.RateLimitPerMinute
	}
	if c.Channels.Webhook.RateLimitIPv4Prefix == 0 {
		c.Channels.Webhook.RateLimitIPv4Prefix = def.Channels.Webhook.RateLimitIPv4Prefix
	}
	if c.Channels.Webhook.RateLimitIPv6Prefix == 0 {
		c.Channels.Webhook.RateLimitIPv6Prefix = def.Channels.Webhook.RateLimitIPv6Prefix
	}
	if len(c.Channels.Webhook.RateLimitCIDRs) == 0 && len(def.Channels.Webhook.RateLimitCIDRs) > 0 {
		c.Channels.Webhook.RateLimitCIDRs = append([]string{}, def.Channels.Webhook.RateLimitCIDRs...)
	}
	if c.Tools.Exec.Timeout == 0 {
		c.Tools.Exec.Timeout = def.Tools.Exec.Timeout
	}
	if c.Tools.RestrictToWorkspace == nil {
		c.Tools.RestrictToWorkspace = def.Tools.RestrictToWorkspace
	}
	// Apply LCM defaults for zero values (when enabled or for config display)
	if c.Agents.Defaults.LCM.Enabled == nil {
		c.Agents.Defaults.LCM.Enabled = def.Agents.Defaults.LCM.Enabled
	}
	if c.Agents.Defaults.LCM.ContextThreshold == 0 {
		c.Agents.Defaults.LCM.ContextThreshold = def.Agents.Defaults.LCM.ContextThreshold
	}
	if c.Agents.Defaults.LCM.FreshTailCount == 0 {
		c.Agents.Defaults.LCM.FreshTailCount = def.Agents.Defaults.LCM.FreshTailCount
	}
	if c.Agents.Defaults.LCM.LeafMinFanout == 0 {
		c.Agents.Defaults.LCM.LeafMinFanout = def.Agents.Defaults.LCM.LeafMinFanout
	}
	if c.Agents.Defaults.LCM.CondensedMinFanout == 0 {
		c.Agents.Defaults.LCM.CondensedMinFanout = def.Agents.Defaults.LCM.CondensedMinFanout
	}
	if c.Agents.Defaults.LCM.LeafChunkTokens == 0 {
		c.Agents.Defaults.LCM.LeafChunkTokens = def.Agents.Defaults.LCM.LeafChunkTokens
	}
	if c.Agents.Defaults.LCM.LeafTargetTokens == 0 {
		c.Agents.Defaults.LCM.LeafTargetTokens = def.Agents.Defaults.LCM.LeafTargetTokens
	}
	if c.Agents.Defaults.LCM.CondensedTargetTokens == 0 {
		c.Agents.Defaults.LCM.CondensedTargetTokens = def.Agents.Defaults.LCM.CondensedTargetTokens
	}
	if c.Agents.Defaults.MemoryMaintenance.Enabled == nil {
		c.Agents.Defaults.MemoryMaintenance.Enabled = def.Agents.Defaults.MemoryMaintenance.Enabled
	}
	if c.Agents.Defaults.MemoryMaintenance.QueueSize == 0 {
		c.Agents.Defaults.MemoryMaintenance.QueueSize = def.Agents.Defaults.MemoryMaintenance.QueueSize
	}
	if c.Agents.Defaults.MemoryMaintenance.MinUserChars == 0 {
		c.Agents.Defaults.MemoryMaintenance.MinUserChars = def.Agents.Defaults.MemoryMaintenance.MinUserChars
	}
	if c.Agents.Defaults.MemoryMaintenance.MinAssistantChars == 0 {
		c.Agents.Defaults.MemoryMaintenance.MinAssistantChars = def.Agents.Defaults.MemoryMaintenance.MinAssistantChars
	}
	if c.Agents.Defaults.MemoryMaintenance.NudgeInterval == 0 {
		c.Agents.Defaults.MemoryMaintenance.NudgeInterval = def.Agents.Defaults.MemoryMaintenance.NudgeInterval
	}
	if c.Agents.Defaults.MemoryMaintenance.MinIntervalMinutes == 0 {
		c.Agents.Defaults.MemoryMaintenance.MinIntervalMinutes = def.Agents.Defaults.MemoryMaintenance.MinIntervalMinutes
	}
	if c.Agents.Defaults.MemoryMaintenance.FlushMinTurns == 0 {
		c.Agents.Defaults.MemoryMaintenance.FlushMinTurns = def.Agents.Defaults.MemoryMaintenance.FlushMinTurns
	}
	if c.Agents.Defaults.MemoryMaintenance.MinQualityScore == 0 {
		c.Agents.Defaults.MemoryMaintenance.MinQualityScore = def.Agents.Defaults.MemoryMaintenance.MinQualityScore
	}
	if c.Agents.Defaults.MemoryMaintenance.RepeatSuppressionMinutes == 0 {
		c.Agents.Defaults.MemoryMaintenance.RepeatSuppressionMinutes = def.Agents.Defaults.MemoryMaintenance.RepeatSuppressionMinutes
	}
	if c.Agents.Defaults.MemoryMaintenance.FailureBackoffMinutes == 0 {
		c.Agents.Defaults.MemoryMaintenance.FailureBackoffMinutes = def.Agents.Defaults.MemoryMaintenance.FailureBackoffMinutes
	}
	if c.Agents.Defaults.MemoryMaintenance.MinConfidence == 0 {
		c.Agents.Defaults.MemoryMaintenance.MinConfidence = def.Agents.Defaults.MemoryMaintenance.MinConfidence
	}
	if c.Agents.Defaults.MemoryMaintenance.ControllerTargetBadRate == 0 {
		c.Agents.Defaults.MemoryMaintenance.ControllerTargetBadRate = def.Agents.Defaults.MemoryMaintenance.ControllerTargetBadRate
	}
	if c.Agents.Defaults.MemoryMaintenance.ControllerKp == 0 {
		c.Agents.Defaults.MemoryMaintenance.ControllerKp = def.Agents.Defaults.MemoryMaintenance.ControllerKp
	}
	if c.Agents.Defaults.MemoryMaintenance.ControllerKi == 0 {
		c.Agents.Defaults.MemoryMaintenance.ControllerKi = def.Agents.Defaults.MemoryMaintenance.ControllerKi
	}
	if c.Agents.Defaults.MemoryMaintenance.ControllerKd == 0 {
		c.Agents.Defaults.MemoryMaintenance.ControllerKd = def.Agents.Defaults.MemoryMaintenance.ControllerKd
	}
	if c.Agents.Defaults.MemoryMaintenance.ControllerBatchSize == 0 {
		c.Agents.Defaults.MemoryMaintenance.ControllerBatchSize = def.Agents.Defaults.MemoryMaintenance.ControllerBatchSize
	}
	if c.Agents.Defaults.MemoryMaintenance.ControllerMinFloor == 0 {
		c.Agents.Defaults.MemoryMaintenance.ControllerMinFloor = def.Agents.Defaults.MemoryMaintenance.ControllerMinFloor
	}
	if c.Agents.Defaults.MemoryMaintenance.ControllerMaxFloor == 0 {
		c.Agents.Defaults.MemoryMaintenance.ControllerMaxFloor = def.Agents.Defaults.MemoryMaintenance.ControllerMaxFloor
	}
	if c.Agents.Defaults.MemoryMaintenance.ControllerOnlineTuning == nil {
		c.Agents.Defaults.MemoryMaintenance.ControllerOnlineTuning = def.Agents.Defaults.MemoryMaintenance.ControllerOnlineTuning
	}
	if c.Agents.Defaults.MemoryMaintenance.SemanticRegroupMaxEntries == 0 {
		c.Agents.Defaults.MemoryMaintenance.SemanticRegroupMaxEntries = def.Agents.Defaults.MemoryMaintenance.SemanticRegroupMaxEntries
	}
	if c.Agents.Defaults.MemoryMaintenance.SemanticRegroupMaxGroups == 0 {
		c.Agents.Defaults.MemoryMaintenance.SemanticRegroupMaxGroups = def.Agents.Defaults.MemoryMaintenance.SemanticRegroupMaxGroups
	}
	if c.Agents.Defaults.SkillsEvolution.Enabled == nil {
		c.Agents.Defaults.SkillsEvolution.Enabled = def.Agents.Defaults.SkillsEvolution.Enabled
	}
	if c.Agents.Defaults.SkillsEvolution.CreationNudgeInterval == 0 {
		c.Agents.Defaults.SkillsEvolution.CreationNudgeInterval = def.Agents.Defaults.SkillsEvolution.CreationNudgeInterval
	}
	if c.Agents.Defaults.SkillsEvolution.MinToolCalls == 0 {
		c.Agents.Defaults.SkillsEvolution.MinToolCalls = def.Agents.Defaults.SkillsEvolution.MinToolCalls
	}
	if c.Agents.Defaults.SkillsEvolution.MinIntervalMinutes == 0 {
		c.Agents.Defaults.SkillsEvolution.MinIntervalMinutes = def.Agents.Defaults.SkillsEvolution.MinIntervalMinutes
	}
	if c.Agents.Defaults.SkillsEvolution.MinQualityScore == 0 {
		c.Agents.Defaults.SkillsEvolution.MinQualityScore = def.Agents.Defaults.SkillsEvolution.MinQualityScore
	}
	if c.Agents.Defaults.SkillsEvolution.FlushMinToolCalls == 0 {
		c.Agents.Defaults.SkillsEvolution.FlushMinToolCalls = def.Agents.Defaults.SkillsEvolution.FlushMinToolCalls
	}
	if c.Agents.Defaults.SkillsEvolution.RepeatSuppressionMinutes == 0 {
		c.Agents.Defaults.SkillsEvolution.RepeatSuppressionMinutes = def.Agents.Defaults.SkillsEvolution.RepeatSuppressionMinutes
	}
	if c.Agents.Defaults.SkillsEvolution.FailureBackoffMinutes == 0 {
		c.Agents.Defaults.SkillsEvolution.FailureBackoffMinutes = def.Agents.Defaults.SkillsEvolution.FailureBackoffMinutes
	}
	if c.Agents.Defaults.SkillsEvolution.MinConfidence == 0 {
		c.Agents.Defaults.SkillsEvolution.MinConfidence = def.Agents.Defaults.SkillsEvolution.MinConfidence
	}
	if c.Agents.Defaults.SkillsEvolution.ControllerTargetBadRate == 0 {
		c.Agents.Defaults.SkillsEvolution.ControllerTargetBadRate = def.Agents.Defaults.SkillsEvolution.ControllerTargetBadRate
	}
	if c.Agents.Defaults.SkillsEvolution.ControllerKp == 0 {
		c.Agents.Defaults.SkillsEvolution.ControllerKp = def.Agents.Defaults.SkillsEvolution.ControllerKp
	}
	if c.Agents.Defaults.SkillsEvolution.ControllerKi == 0 {
		c.Agents.Defaults.SkillsEvolution.ControllerKi = def.Agents.Defaults.SkillsEvolution.ControllerKi
	}
	if c.Agents.Defaults.SkillsEvolution.ControllerKd == 0 {
		c.Agents.Defaults.SkillsEvolution.ControllerKd = def.Agents.Defaults.SkillsEvolution.ControllerKd
	}
	if c.Agents.Defaults.SkillsEvolution.ControllerBatchSize == 0 {
		c.Agents.Defaults.SkillsEvolution.ControllerBatchSize = def.Agents.Defaults.SkillsEvolution.ControllerBatchSize
	}
	if c.Agents.Defaults.SkillsEvolution.ControllerMinFloor == 0 {
		c.Agents.Defaults.SkillsEvolution.ControllerMinFloor = def.Agents.Defaults.SkillsEvolution.ControllerMinFloor
	}
	if c.Agents.Defaults.SkillsEvolution.ControllerMaxFloor == 0 {
		c.Agents.Defaults.SkillsEvolution.ControllerMaxFloor = def.Agents.Defaults.SkillsEvolution.ControllerMaxFloor
	}
	if c.Agents.Defaults.SkillsEvolution.ControllerOnlineTuning == nil {
		c.Agents.Defaults.SkillsEvolution.ControllerOnlineTuning = def.Agents.Defaults.SkillsEvolution.ControllerOnlineTuning
	}
	if c.Agents.Defaults.SkillsEvolution.MidRunReflectEveryToolIters == nil {
		c.Agents.Defaults.SkillsEvolution.MidRunReflectEveryToolIters = def.Agents.Defaults.SkillsEvolution.MidRunReflectEveryToolIters
	}
	if c.Agents.Defaults.SkillsEvolution.MidRunReflectMinSeconds == 0 {
		c.Agents.Defaults.SkillsEvolution.MidRunReflectMinSeconds = def.Agents.Defaults.SkillsEvolution.MidRunReflectMinSeconds
	}
	if c.Agents.Defaults.Experience.MemoryPromptNudgeEvery == nil {
		c.Agents.Defaults.Experience.MemoryPromptNudgeEvery = def.Agents.Defaults.Experience.MemoryPromptNudgeEvery
	}
	if c.Agents.Defaults.Experience.SkillPromptNudgeEvery == nil {
		c.Agents.Defaults.Experience.SkillPromptNudgeEvery = def.Agents.Defaults.Experience.SkillPromptNudgeEvery
	}
	if c.Agents.Defaults.Experience.LearnerFeedbackInstantPush == nil {
		c.Agents.Defaults.Experience.LearnerFeedbackInstantPush = def.Agents.Defaults.Experience.LearnerFeedbackInstantPush
	}
	if c.Agents.Defaults.Experience.UsageCostCurrency == "" {
		c.Agents.Defaults.Experience.UsageCostCurrency = def.Agents.Defaults.Experience.UsageCostCurrency
	}
	if c.Agents.Defaults.Experience.DefaultUsdToCny == 0 {
		c.Agents.Defaults.Experience.DefaultUsdToCny = def.Agents.Defaults.Experience.DefaultUsdToCny
	}
	if c.Logging.Enabled == nil {
		c.Logging.Enabled = def.Logging.Enabled
	}
	if c.Logging.Level == "" {
		c.Logging.Level = def.Logging.Level
	}
	if c.Logging.MaxAgeDays == 0 {
		c.Logging.MaxAgeDays = def.Logging.MaxAgeDays
	}
	if c.Logging.MaxSizeMB == 0 {
		c.Logging.MaxSizeMB = def.Logging.MaxSizeMB
	}
	if c.Logging.Dir == "" {
		c.Logging.Dir = def.Logging.Dir
	}
	if c.Logging.Filename == "" {
		c.Logging.Filename = def.Logging.Filename
	}
	return &c, nil
}

// SaveWorkspaceToDefaultConfig saves only the workspace path to ~/.dipper-bot/config.json.
// Merges with existing config to preserve other fields if any.
func SaveWorkspaceToDefaultConfig(workspace string) error {
	p, err := GetConfigPath()
	if err != nil {
		return err
	}
	cfg, _ := LoadConfig(p)
	if cfg == nil {
		cfg = DefaultConfig()
	}
	cfg.Agents.Defaults.Workspace = workspace
	return SaveConfig(cfg, p)
}

// SaveConfig writes config to path.
// Uses SetEscapeHTML(false) so URLs with &, <, > are not escaped (e.g. Discord gatewayUrl).
func SaveConfig(c *Config, configPath string) error {
	if configPath == "" {
		var err error
		configPath, err = GetConfigPath()
		if err != nil {
			return err
		}
	}
	dir := filepath.Dir(configPath)
	if err := os.MkdirAll(dir, 0o750); err != nil {
		return err
	}
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	enc.SetIndent("", "  ")
	if err := enc.Encode(c); err != nil {
		return err
	}
	data := buf.Bytes()
	// Encoder.Encode appends newline; trim trailing newline for consistency with MarshalIndent
	if len(data) > 0 && data[len(data)-1] == '\n' {
		data = data[:len(data)-1]
	}
	return os.WriteFile(configPath, data, 0o600)
}

// GetWorkspacePath expands the workspace path from config.
func GetWorkspacePath(c *Config) (string, error) {
	return expandWorkspace(c.Agents.Defaults.Workspace)
}

// GetProviderAPIKey returns API key for custom provider.
func GetProviderAPIKey(c *Config, model string) string {
	return c.Providers.Custom.APIKey
}

// GetProviderAPIBase returns API base for custom provider.
func GetProviderAPIBase(c *Config, model string) string {
	return c.Providers.Custom.GetAPIBase()
}

// GetProviderName returns the matched provider name for the model (e.g. "openai_codex", "gemini").
func GetProviderName(c *Config, model string) string {
	_, name := MatchProvider(c, model)
	return name
}

func expandWorkspace(w string) (string, error) {
	if w == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		return home + "/.dipper-bot/workspace", nil
	}
	if len(w) >= 1 && w[0] == '~' {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		if w == "~" {
			return home, nil
		}
		if len(w) >= 2 && w[1] == '/' {
			return filepath.Join(home, w[2:]), nil
		}
	}
	return w, nil
}
