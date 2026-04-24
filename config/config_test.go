package config_test

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/jacklicn/dipper-bot/config"
)

func TestDefaultConfig(t *testing.T) {
	cfg := config.DefaultConfig()
	if cfg == nil {
		t.Fatal("DefaultConfig() returned nil")
	}
	if cfg.Gateway.Port != 8090 {
		t.Errorf("Gateway.Port = %d, want 8090", cfg.Gateway.Port)
	}
	if cfg.Gateway.RateLimitPerMinute != 120 {
		t.Errorf("Gateway.RateLimitPerMinute = %d, want 120", cfg.Gateway.RateLimitPerMinute)
	}
	if cfg.Gateway.RateLimitIPv4Prefix != 32 || cfg.Gateway.RateLimitIPv6Prefix != 128 {
		t.Errorf("Gateway prefixes = /%d /%d, want /32 /128", cfg.Gateway.RateLimitIPv4Prefix, cfg.Gateway.RateLimitIPv6Prefix)
	}
	if cfg.Channels.Webhook.RateLimitPerMinute != 120 {
		t.Errorf("Webhook.RateLimitPerMinute = %d, want 120", cfg.Channels.Webhook.RateLimitPerMinute)
	}
	if cfg.Channels.Webhook.RateLimitIPv4Prefix != 32 || cfg.Channels.Webhook.RateLimitIPv6Prefix != 128 {
		t.Errorf("Webhook prefixes = /%d /%d, want /32 /128", cfg.Channels.Webhook.RateLimitIPv4Prefix, cfg.Channels.Webhook.RateLimitIPv6Prefix)
	}
	if cfg.Agents.Defaults.Model == "" {
		t.Error("Agents.Defaults.Model should be non-empty")
	}
	if !cfg.Logging.FileLoggingEnabled() {
		t.Error("default Logging.FileLoggingEnabled should be true")
	}
	if cfg.Logging.MaxAgeDays != 7 || cfg.Logging.MaxSizeMB != 128 {
		t.Errorf("Logging defaults: maxAge=%d maxSize=%d, want 7 and 128", cfg.Logging.MaxAgeDays, cfg.Logging.MaxSizeMB)
	}
	if cfg.Logging.Level != "info" {
		t.Errorf("Logging.Level = %q, want info", cfg.Logging.Level)
	}
	if !cfg.Agents.Defaults.LCM.LCMEnabled() {
		t.Error("default LCM should be enabled")
	}
	if cfg.Agents.Defaults.SkillsEvolution.MidRunReflectEvery() != 4 {
		t.Errorf("SkillsEvolution.MidRunReflectEvery = %d, want 4", cfg.Agents.Defaults.SkillsEvolution.MidRunReflectEvery())
	}
	if cfg.Agents.Defaults.SkillsEvolution.MidRunReflectCooldownSeconds() != 120 {
		t.Errorf("MidRunReflectCooldownSeconds = %d, want 120", cfg.Agents.Defaults.SkillsEvolution.MidRunReflectCooldownSeconds())
	}
	if cfg.Agents.Defaults.Experience.LearnerFeedbackInstantPush == nil || !*cfg.Agents.Defaults.Experience.LearnerFeedbackInstantPush {
		t.Error("default learnerFeedbackInstantPush should be true")
	}
	if !cfg.Tools.RestrictToWorkspaceEnabled() {
		t.Error("default restrictToWorkspace should be true")
	}
}

func TestDefaultConfig_MarshalLoggingOnboardShape(t *testing.T) {
	cfg := config.DefaultConfig()
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(cfg); err != nil {
		t.Fatal(err)
	}
	s := buf.String()
	for _, key := range []string{
		`"logging"`,
		`"enabled":true`,
		`"level":"info"`,
		`"maxAgeDays":7`,
		`"maxSizeMB":128`,
		`"maxBackups":0`,
		`"fileOnly":false`,
		`"dir":"logs"`,
		`"filename":"dipper-bot.log"`,
		`"compress":false`,
	} {
		if !strings.Contains(s, key) {
			t.Errorf("marshaled config missing %q\n%s", key, s)
		}
	}
}

func TestLoadConfig_MinimalAgentsDefaultsSkillAndMemoryLoops(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.json")
	body := `{"agents":{"defaults":{"workspace":"/tmp/ws","model":"m"}}}`
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, err := config.LoadConfig(path)
	if err != nil {
		t.Fatalf("LoadConfig: %v", err)
	}
	if !cfg.Agents.Defaults.SkillsEvolution.EvolutionEnabled() {
		t.Fatal("skills evolution should default enabled when skillsEvolution omitted")
	}
	if !cfg.Agents.Defaults.MemoryMaintenance.MaintenanceEnabled() {
		t.Fatal("memory maintenance should default enabled when memoryMaintenance omitted")
	}
	if cfg.Agents.Defaults.Experience.SkillPromptNudgeEvery == nil || *cfg.Agents.Defaults.Experience.SkillPromptNudgeEvery != 10 {
		t.Fatalf("SkillPromptNudgeEvery = %v, want pointer to 10", cfg.Agents.Defaults.Experience.SkillPromptNudgeEvery)
	}
	if cfg.Agents.Defaults.Experience.LearnerFeedbackInstantPush == nil || !*cfg.Agents.Defaults.Experience.LearnerFeedbackInstantPush {
		t.Fatalf("LearnerFeedbackInstantPush = %v, want pointer to true", cfg.Agents.Defaults.Experience.LearnerFeedbackInstantPush)
	}
	if !cfg.Agents.Defaults.LCM.LCMEnabled() {
		t.Fatal("LCM should default enabled when lcm omitted")
	}
	if !cfg.Tools.RestrictToWorkspaceEnabled() {
		t.Fatal("restrictToWorkspace should default true when tools.restrictToWorkspace omitted")
	}
}

func TestLoadConfig_AppliesRateLimitDefaults(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.json")
	body := `{"gateway":{"port":8700},"channels":{"webhook":{"enabled":true}}}`
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, err := config.LoadConfig(path)
	if err != nil {
		t.Fatalf("LoadConfig: %v", err)
	}
	if cfg.Gateway.RateLimitPerMinute != 120 {
		t.Fatalf("Gateway.RateLimitPerMinute = %d, want 120", cfg.Gateway.RateLimitPerMinute)
	}
	if cfg.Gateway.RateLimitIPv4Prefix != 32 || cfg.Gateway.RateLimitIPv6Prefix != 128 {
		t.Fatalf("Gateway prefixes = /%d /%d, want /32 /128", cfg.Gateway.RateLimitIPv4Prefix, cfg.Gateway.RateLimitIPv6Prefix)
	}
	if cfg.Channels.Webhook.RateLimitPerMinute != 120 {
		t.Fatalf("Webhook.RateLimitPerMinute = %d, want 120", cfg.Channels.Webhook.RateLimitPerMinute)
	}
	if cfg.Channels.Webhook.RateLimitIPv4Prefix != 32 || cfg.Channels.Webhook.RateLimitIPv6Prefix != 128 {
		t.Fatalf("Webhook prefixes = /%d /%d, want /32 /128", cfg.Channels.Webhook.RateLimitIPv4Prefix, cfg.Channels.Webhook.RateLimitIPv6Prefix)
	}
}

func TestLoadConfig_MissingFile(t *testing.T) {
	cfg, err := config.LoadConfig(filepath.Join(t.TempDir(), "nonexistent.json"))
	if err != nil {
		t.Fatalf("LoadConfig: %v", err)
	}
	if cfg == nil {
		t.Fatal("LoadConfig should return default config for missing file")
	}
	if cfg.Gateway.Port != 8090 {
		t.Errorf("default Gateway.Port = %d, want 8090", cfg.Gateway.Port)
	}
}

func TestLoadConfig_ValidFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.json")
	body := `{"gateway":{"port":8700},"agents":{"defaults":{"model":"test-model"}}}`
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}

	cfg, err := config.LoadConfig(path)
	if err != nil {
		t.Fatalf("LoadConfig: %v", err)
	}
	if cfg.Gateway.Port != 8700 {
		t.Errorf("Gateway.Port = %d, want 8700", cfg.Gateway.Port)
	}
	if cfg.Agents.Defaults.Model != "test-model" {
		t.Errorf("Model = %q, want test-model", cfg.Agents.Defaults.Model)
	}
}

func TestGetProviderAPIKey(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.Providers.Custom.APIKey = "custom-key"
	if got := config.GetProviderAPIKey(cfg, ""); got != "custom-key" {
		t.Errorf("GetProviderAPIKey = %q, want custom-key", got)
	}
}

func TestResolveModelForAPI(t *testing.T) {
	// custom provider passes model through
	got := config.ResolveModelForAPI("custom", "gpt-4")
	if got != "gpt-4" {
		t.Errorf("ResolveModelForAPI(custom, gpt-4) = %q, want gpt-4", got)
	}
}

func TestMatchProvider(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.Providers.Custom.APIKey = "key"

	_, name := config.MatchProvider(cfg, "openai-codex/gpt-5.1-codex")
	if name != "openai_codex" {
		t.Errorf("MatchProvider(openai-codex/...) = %q, want openai_codex", name)
	}

	_, name = config.MatchProvider(cfg, "github-copilot/gpt-4o")
	if name != "github_copilot" {
		t.Errorf("MatchProvider(github-copilot/...) = %q, want github_copilot", name)
	}

	_, name = config.MatchProvider(cfg, "gpt-4")
	if name != "custom" {
		t.Errorf("MatchProvider(gpt-4) = %q, want custom", name)
	}
}

func TestGetModelTemperature(t *testing.T) {
	if got := config.GetModelTemperature("custom", "gpt-4", 0.7); got != 0.7 {
		t.Errorf("custom default temp = %v, want 0.7", got)
	}
}

func TestLoadConfig_MigrateRestrictToWorkspace(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.json")
	// Old format: restrictToWorkspace under tools.exec
	body := `{"tools":{"exec":{"timeout":60,"restrictToWorkspace":true}}}`
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, err := config.LoadConfig(path)
	if err != nil {
		t.Fatalf("LoadConfig: %v", err)
	}
	if !cfg.Tools.RestrictToWorkspaceEnabled() {
		t.Error("Migration should move restrictToWorkspace to tools level")
	}
}

func TestLoadConfig_ExplicitRestrictToWorkspaceFalse(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.json")
	body := `{"tools":{"restrictToWorkspace":false}}`
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, err := config.LoadConfig(path)
	if err != nil {
		t.Fatalf("LoadConfig: %v", err)
	}
	if cfg.Tools.RestrictToWorkspaceEnabled() {
		t.Fatal("restrictToWorkspace false should be honored")
	}
}

func TestSaveConfig(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	cfg := config.DefaultConfig()
	cfg.Gateway.Port = 3000
	if err := config.SaveConfig(cfg, path); err != nil {
		t.Fatalf("SaveConfig: %v", err)
	}
	loaded, err := config.LoadConfig(path)
	if err != nil {
		t.Fatalf("LoadConfig after Save: %v", err)
	}
	if loaded.Gateway.Port != 3000 {
		t.Errorf("after save/load Port = %d, want 3000", loaded.Gateway.Port)
	}
}

func TestSaveConfig_ProviderFields(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	cfg := config.DefaultConfig()
	if err := config.SaveConfig(cfg, path); err != nil {
		t.Fatalf("SaveConfig: %v", err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(data, []byte(`"custom"`)) {
		t.Error("SaveConfig should include custom provider")
	}
}

func TestSaveConfig_NoHTMLEscape(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	cfg := config.DefaultConfig()
	cfg.Channels.Discord.Enabled = true
	cfg.Channels.Discord.GatewayURL = "wss://gateway.discord.gg/?v=10&encoding=json"
	if err := config.SaveConfig(cfg, path); err != nil {
		t.Fatalf("SaveConfig: %v", err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(data, []byte(`\u0026`)) {
		t.Error("SaveConfig should not escape & to \\u0026 in URLs")
	}
	if !bytes.Contains(data, []byte("&encoding=json")) {
		t.Error("SaveConfig should preserve & in gatewayUrl")
	}
}
