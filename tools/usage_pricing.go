package tools

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/jacklicn/dipper-bot/config"
)

const defaultUsdToCnyFallback = 7.2

// pricePerMillion is always USD/M for the built-in table rows; override entries use the configured usage cost currency.
type pricePerMillion struct {
	in  float64
	out float64
}

// roughModelPricing: internal USD per 1M tokens (not billing truth).
var roughModelPricing = []struct {
	prefix string
	p      pricePerMillion
}{
	{prefix: "gpt-4o-mini", p: pricePerMillion{in: 0.15, out: 0.60}},
	{prefix: "gpt-4o", p: pricePerMillion{in: 2.50, out: 10.0}},
	{prefix: "gpt-4-turbo", p: pricePerMillion{in: 10.0, out: 30.0}},
	{prefix: "gpt-4", p: pricePerMillion{in: 30.0, out: 60.0}},
	{prefix: "gpt-3.5-turbo", p: pricePerMillion{in: 0.50, out: 1.50}},
	{prefix: "claude-3-5-sonnet", p: pricePerMillion{in: 3.0, out: 15.0}},
	{prefix: "claude-3-5-haiku", p: pricePerMillion{in: 0.80, out: 4.0}},
	{prefix: "claude-3-opus", p: pricePerMillion{in: 15.0, out: 75.0}},
	{prefix: "claude-sonnet-4", p: pricePerMillion{in: 3.0, out: 15.0}},
	{prefix: "claude-opus-4", p: pricePerMillion{in: 15.0, out: 75.0}},
	{prefix: "anthropic/claude-3.5-sonnet", p: pricePerMillion{in: 3.0, out: 15.0}},
	{prefix: "anthropic/claude-3-5-sonnet", p: pricePerMillion{in: 3.0, out: 15.0}},
	{prefix: "anthropic/claude-opus", p: pricePerMillion{in: 15.0, out: 75.0}},
	{prefix: "gemini-2.0-flash", p: pricePerMillion{in: 0.10, out: 0.40}},
	{prefix: "gemini-1.5-pro", p: pricePerMillion{in: 1.25, out: 5.0}},
	{prefix: "gemini", p: pricePerMillion{in: 0.35, out: 1.05}},
}

// NormalizeUsageCostCurrency returns CNY or USD for experience.usageCostCurrency.
func NormalizeUsageCostCurrency(s string) string {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "usd", "dollar", "美元":
		return "USD"
	case "cny", "人民币", "":
		return "CNY"
	default:
		return "CNY"
	}
}

// EffectiveUsdToCny returns the FX multiplier for converting the built-in USD/M table to CNY when currency is CNY.
func EffectiveUsdToCny(configured float64) float64 {
	if configured > 0 {
		return configured
	}
	return defaultUsdToCnyFallback
}

// ReadWorkspacePricingOverrides loads workspace/memory/pricing_overrides.json (array or {"overrides":[...]}).
// Per-1M amounts use the same unit as agents.defaults.experience.usageCostCurrency.
func ReadWorkspacePricingOverrides(workspace string) ([]config.UsagePricingOverrideEntry, error) {
	if strings.TrimSpace(workspace) == "" {
		return nil, nil
	}
	p := filepath.Join(workspace, "memory", "pricing_overrides.json")
	b, err := os.ReadFile(p)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	if len(strings.TrimSpace(string(b))) == 0 {
		return nil, nil
	}
	var arr []config.UsagePricingOverrideEntry
	if err := json.Unmarshal(b, &arr); err == nil {
		return arr, nil
	}
	var wrap struct {
		Overrides []config.UsagePricingOverrideEntry `json:"overrides"`
	}
	if err := json.Unmarshal(b, &wrap); err != nil {
		return nil, fmt.Errorf("pricing_overrides.json: %w", err)
	}
	return wrap.Overrides, nil
}

func matchPricingOverride(modelLower string, entries []config.UsagePricingOverrideEntry) (pricePerMillion, bool) {
	for _, e := range entries {
		sub := strings.ToLower(strings.TrimSpace(e.MatchSubstring))
		if sub == "" {
			continue
		}
		if strings.Contains(modelLower, sub) {
			return pricePerMillion{in: e.InputPerMillion, out: e.OutputPerMillion}, true
		}
	}
	return pricePerMillion{}, false
}

func amountFromPM(p pricePerMillion, promptTok, completionTok int) float64 {
	if promptTok < 0 {
		promptTok = 0
	}
	if completionTok < 0 {
		completionTok = 0
	}
	return float64(promptTok)/1e6*p.in + float64(completionTok)/1e6*p.out
}

func estimateBuiltinUSD(model string, promptTok, completionTok int) (usd float64, status string) {
	m := strings.ToLower(strings.TrimSpace(model))
	for _, row := range roughModelPricing {
		if strings.Contains(m, strings.ToLower(row.prefix)) {
			return amountFromPM(row.p, promptTok, completionTok), "priced"
		}
	}
	return 0, "unknown_model"
}

// EstimateUsageCostLayered returns estimated cost in usageCostCurrency (CNY or USD).
// Overrides and file entries are in that same unit. Built-in table is USD/M → multiplied by usdToCny only when currency is CNY.
// Status: priced_config | priced_file | priced | unknown_model
func EstimateUsageCostLayered(model string, promptTok, completionTok int, usageCostCurrency string, usdToCny float64, cfgOverrides, fileOverrides []config.UsagePricingOverrideEntry) (amount float64, status string) {
	cur := NormalizeUsageCostCurrency(usageCostCurrency)
	m := strings.ToLower(strings.TrimSpace(model))
	if p, ok := matchPricingOverride(m, cfgOverrides); ok {
		return amountFromPM(p, promptTok, completionTok), "priced_config"
	}
	if p, ok := matchPricingOverride(m, fileOverrides); ok {
		return amountFromPM(p, promptTok, completionTok), "priced_file"
	}
	usd, st := estimateBuiltinUSD(model, promptTok, completionTok)
	if st == "unknown_model" {
		return 0, "unknown_model"
	}
	if cur == "USD" {
		return usd, "priced"
	}
	return usd * EffectiveUsdToCny(usdToCny), "priced"
}
