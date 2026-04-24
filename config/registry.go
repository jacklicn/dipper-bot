package config

import "strings"

// ModelOverride holds per-model parameter overrides (e.g. kimi-k2.5 temperature >= 1.0).
type ModelOverride struct {
	ModelPattern string
	Temperature  float64 // 0 = no override
}

// ProviderSpec holds metadata for one LLM provider.
type ProviderSpec struct {
	Name             string
	Keywords         []string
	DefaultAPIBase   string
	IsGateway        bool
	IsOAuth          bool
	IsLocal          bool     // local deployment (vLLM, Ollama) - status shows api_base
	LitellmPrefix    string   // e.g. "moonshot" → model becomes "moonshot/kimi-k2.5"
	StripModelPrefix bool     // AiHubMix: strip provider prefix before re-prefixing
	SkipPrefixes     []string // don't add prefix if model already starts with these
	ModelOverrides   []ModelOverride
}

// providerSpecs defines provider lookup.
var providerSpecs = []ProviderSpec{
	{Name: "custom", IsGateway: false, IsOAuth: false},
	{Name: "openai_codex", Keywords: []string{"openai-codex"}, IsOAuth: true},
	{Name: "github_copilot", Keywords: []string{"github_copilot", "copilot"}, IsOAuth: true},
}

// GetProviderConfig returns the provider config for the given name, or nil.
func GetProviderConfig(c *Config, name string) *ProviderConfig {
	return getProviderConfig(c, name)
}

func getProviderConfig(c *Config, name string) *ProviderConfig {
	switch name {
	case "custom":
		return &c.Providers.Custom
	default:
		return nil
	}
}

// ProviderSpecs returns all provider specs for status display.
func ProviderSpecs() []ProviderSpec {
	return append([]ProviderSpec{}, providerSpecs...)
}

// GetDefaultAPIBase returns the default API base URL for a provider, or empty if none.
func GetDefaultAPIBase(providerName string) string {
	for _, spec := range providerSpecs {
		if spec.Name == providerName && spec.DefaultAPIBase != "" {
			return spec.DefaultAPIBase
		}
	}
	return ""
}

// findSpecByName returns the provider spec by name.
func findSpecByName(name string) *ProviderSpec {
	for i := range providerSpecs {
		if providerSpecs[i].Name == name {
			return &providerSpecs[i]
		}
	}
	return nil
}

// ResolveModelForAPI returns the model string to send to the provider's API.
// ResolveModelForAPI logic.
func ResolveModelForAPI(providerName, model string) string {
	spec := findSpecByName(providerName)
	if spec == nil {
		return model
	}
	modelLower := strings.ToLower(model)

	if spec.IsGateway {
		// Gateway: for direct HTTP, OpenRouter/SiliconFlow expect model as-is (e.g. "anthropic/claude-3").
		// AiHubMix expects "openai/"+bare_model (strip provider prefix).
		if spec.StripModelPrefix {
			if idx := strings.LastIndex(model, "/"); idx >= 0 {
				model = model[idx+1:]
			}
			if spec.LitellmPrefix != "" {
				model = spec.LitellmPrefix + "/" + model
			}
		}
		return model
	}

	// Direct provider: strip provider prefix, API expects bare model name
	if spec.LitellmPrefix != "" && strings.HasPrefix(modelLower, strings.ToLower(spec.LitellmPrefix)+"/") {
		return model[len(spec.LitellmPrefix)+1:]
	}
	for _, skip := range spec.SkipPrefixes {
		if strings.HasPrefix(modelLower, strings.ToLower(skip)) {
			return model[len(skip):]
		}
	}
	return model
}

// GetModelTemperature returns effective temperature for the model.
// Applies model_overrides (e.g. kimi-k2.5 requires temperature >= 1.0).
func GetModelTemperature(providerName, model string, defaultTemp float64) float64 {
	spec := findSpecByName(providerName)
	if spec == nil {
		return defaultTemp
	}
	modelLower := strings.ToLower(model)
	for _, mo := range spec.ModelOverrides {
		if mo.Temperature > 0 && strings.Contains(modelLower, strings.ToLower(mo.ModelPattern)) {
			return mo.Temperature
		}
	}
	return defaultTemp
}

// MatchProvider finds provider config by model. Prefers explicit prefix (openai-codex, github-copilot) over custom.
func MatchProvider(c *Config, model string) (*ProviderConfig, string) {
	modelLower := strings.ToLower(model)
	// Explicit provider prefix wins
	if strings.HasPrefix(modelLower, "openai-codex/") || strings.HasPrefix(modelLower, "openai_codex/") {
		return nil, "openai_codex" // OAuth, no ProviderConfig
	}
	if strings.HasPrefix(modelLower, "github-copilot/") || strings.HasPrefix(modelLower, "github_copilot/") {
		return nil, "github_copilot" // OAuth, no ProviderConfig
	}
	// Fallback to custom
	p := GetProviderConfig(c, "custom")
	if p != nil && p.APIKey != "" {
		return p, "custom"
	}
	return nil, ""
}
