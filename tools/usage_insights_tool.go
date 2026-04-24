package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/jacklicn/dipper-bot/config"
)

// UsageInsightsTool reads memory/usage_events.jsonl for cross-session analytics.
type UsageInsightsTool struct {
	Workspace         string
	PricingOverrides  []config.UsagePricingOverrideEntry // from agents.defaults.experience (optional)
	UsageCostCurrency string                             // CNY | USD from agents.defaults.experience.usageCostCurrency
	DefaultUsdToCny   float64                            // from agents.defaults.experience.defaultUsdToCny (when currency is CNY)
}

func (t *UsageInsightsTool) Name() string { return "usage_insights" }

func (t *UsageInsightsTool) Description() string {
	return "Analyze LLM usage rows in memory/usage_events.jsonl (cross-session tokens, tool names, bigrams; cost unit from agents.defaults.experience.usageCostCurrency: CNY or USD).\n" +
		"Actions:\n" +
		"- summary (window_hours optional, default 168): totals + by_model + by_source\n" +
		"- recent (limit optional, default 30): last N raw rows\n" +
		"- by_session (window_hours optional): per-sessionKey aggregates (tokens, rows, last_seen)\n" +
		"- tool_analytics (window_hours optional): tool frequency + adjacent bigrams across ordered events per session\n" +
		"- cost_estimate (window_hours optional): totals in configured currency; overrides use inputPerMillion/outputPerMillion; " +
		"built-in table is USD/M (× defaultUsdToCny when unit is CNY); response includes priced_status_breakdown.\n"
}

func (t *UsageInsightsTool) Parameters() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"action":       map[string]any{"type": "string", "enum": []any{"summary", "recent", "by_session", "tool_analytics", "cost_estimate"}},
			"window_hours": map[string]any{"type": "integer"},
			"limit":        map[string]any{"type": "integer"},
		},
		"required": []any{"action"},
	}
}

func (t *UsageInsightsTool) Execute(ctx context.Context, params map[string]any) (string, error) {
	if t == nil || strings.TrimSpace(t.Workspace) == "" {
		return "", fmt.Errorf("workspace not set")
	}
	action, _ := params["action"].(string)
	h := windowHoursParam(params, 168)
	switch action {
	case "summary":
		return usageSummary(t.Workspace, h)
	case "recent":
		limit := 30
		if v, ok := params["limit"].(float64); ok && int(v) > 0 {
			limit = int(v)
		}
		return usageRecent(t.Workspace, limit)
	case "by_session":
		return usageBySession(t.Workspace, h)
	case "tool_analytics":
		return usageToolAnalytics(t.Workspace, h)
	case "cost_estimate":
		return usageCostEstimate(t.Workspace, h, t.PricingOverrides, t.UsageCostCurrency, t.DefaultUsdToCny)
	default:
		return "", fmt.Errorf("unknown action")
	}
}

func windowHoursParam(params map[string]any, def int) int {
	if v, ok := params["window_hours"].(float64); ok && int(v) > 0 {
		return int(v)
	}
	return def
}

func usageEventsPath(workspace string) string {
	return filepath.Join(workspace, "memory", "usage_events.jsonl")
}

func readUsageEventsInWindow(workspace string, windowHours int) ([]map[string]any, error) {
	raw, err := os.ReadFile(usageEventsPath(workspace))
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	cut := time.Now().Add(-time.Duration(windowHours) * time.Hour)
	lines := strings.Split(string(raw), "\n")
	var out []map[string]any
	for _, ln := range lines {
		ln = strings.TrimSpace(ln)
		if ln == "" {
			continue
		}
		var ev map[string]any
		if json.Unmarshal([]byte(ln), &ev) != nil {
			continue
		}
		ts, _ := ev["time"].(string)
		t, err := time.Parse(time.RFC3339, ts)
		if err != nil || t.Before(cut) {
			continue
		}
		out = append(out, ev)
	}
	return out, nil
}

func usageSummary(workspace string, windowHours int) (string, error) {
	events, err := readUsageEventsInWindow(workspace, windowHours)
	if err != nil {
		return "", err
	}
	totPrompt, totComp, totAll := 0, 0, 0
	byModel := map[string]map[string]int{}
	bySource := map[string]int{}
	rows := len(events)
	for _, ev := range events {
		pt := int(num(ev["promptTokens"]))
		ct := int(num(ev["completionTokens"]))
		tt := int(num(ev["totalTokens"]))
		totPrompt += pt
		totComp += ct
		if tt > 0 {
			totAll += tt
		} else {
			totAll += pt + ct
		}
		m, _ := ev["model"].(string)
		if m == "" {
			m = "unknown"
		}
		if byModel[m] == nil {
			byModel[m] = map[string]int{"prompt": 0, "completion": 0, "total": 0, "calls": 0}
		}
		byModel[m]["prompt"] += pt
		byModel[m]["completion"] += ct
		if tt > 0 {
			byModel[m]["total"] += tt
		} else {
			byModel[m]["total"] += pt + ct
		}
		byModel[m]["calls"]++
		src, _ := ev["source"].(string)
		if src == "" {
			src = "primary"
		}
		bySource[src] = bySource[src] + 1
	}
	out := map[string]any{
		"success":             true,
		"window_hours":        windowHours,
		"rows":                rows,
		"prompt_tokens":       totPrompt,
		"completion_tokens":   totComp,
		"total_tokens":        totAll,
		"by_model":            byModel,
		"rows_by_source":      bySource,
	}
	b, _ := json.Marshal(out)
	return string(b), nil
}

func usageRecent(workspace string, limit int) (string, error) {
	raw, err := os.ReadFile(usageEventsPath(workspace))
	if err != nil {
		if os.IsNotExist(err) {
			return `{"success":true,"count":0,"rows":[]}`, nil
		}
		return "", err
	}
	lines := strings.Split(string(raw), "\n")
	var picked []map[string]any
	for i := len(lines) - 1; i >= 0 && len(picked) < limit; i-- {
		ln := strings.TrimSpace(lines[i])
		if ln == "" {
			continue
		}
		var ev map[string]any
		if json.Unmarshal([]byte(ln), &ev) != nil {
			continue
		}
		picked = append(picked, ev)
	}
	for i, j := 0, len(picked)-1; i < j; i, j = i+1, j-1 {
		picked[i], picked[j] = picked[j], picked[i]
	}
	b, _ := json.Marshal(map[string]any{"success": true, "count": len(picked), "rows": picked})
	return string(b), nil
}

type sessionAgg struct {
	Rows             int    `json:"rows"`
	PromptTokens     int    `json:"prompt_tokens"`
	CompletionTokens int    `json:"completion_tokens"`
	TotalTokens      int    `json:"total_tokens"`
	LastTime         string `json:"last_time"`
}

func usageBySession(workspace string, windowHours int) (string, error) {
	events, err := readUsageEventsInWindow(workspace, windowHours)
	if err != nil {
		return "", err
	}
	m := map[string]*sessionAgg{}
	for _, ev := range events {
		sk, _ := ev["sessionKey"].(string)
		if sk == "" {
			sk = "unknown"
		}
		a := m[sk]
		if a == nil {
			a = &sessionAgg{}
			m[sk] = a
		}
		a.Rows++
		pt := int(num(ev["promptTokens"]))
		ct := int(num(ev["completionTokens"]))
		tt := int(num(ev["totalTokens"]))
		a.PromptTokens += pt
		a.CompletionTokens += ct
		if tt > 0 {
			a.TotalTokens += tt
		} else {
			a.TotalTokens += pt + ct
		}
		ts, _ := ev["time"].(string)
		if ts > a.LastTime {
			a.LastTime = ts
		}
	}
	// map to serializable
	outMap := make(map[string]sessionAgg, len(m))
	for k, v := range m {
		outMap[k] = *v
	}
	b, _ := json.Marshal(map[string]any{"success": true, "window_hours": windowHours, "sessions": outMap})
	return string(b), nil
}

func usageToolAnalytics(workspace string, windowHours int) (string, error) {
	events, err := readUsageEventsInWindow(workspace, windowHours)
	if err != nil {
		return "", err
	}
	bySession := map[string][]map[string]any{}
	for _, ev := range events {
		sk, _ := ev["sessionKey"].(string)
		if sk == "" {
			sk = "unknown"
		}
		bySession[sk] = append(bySession[sk], ev)
	}
	toolFreq := map[string]int{}
	bigram := map[string]int{}
	for _, list := range bySession {
		sort.Slice(list, func(i, j int) bool {
			ti, _ := list[i]["time"].(string)
			tj, _ := list[j]["time"].(string)
			return ti < tj
		})
		var chain []string
		for _, ev := range list {
			names := toolNamesFromEvent(ev)
			chain = append(chain, names...)
		}
		for _, n := range chain {
			toolFreq[n]++
		}
		for i := 0; i+1 < len(chain); i++ {
			k := chain[i] + " -> " + chain[i+1]
			bigram[k]++
		}
	}
	b, _ := json.Marshal(map[string]any{
		"success":        true,
		"window_hours":   windowHours,
		"tool_frequency": toolFreq,
		"tool_bigrams":   bigram,
	})
	return string(b), nil
}

func toolNamesFromEvent(ev map[string]any) []string {
	raw, ok := ev["toolNames"].([]any)
	if !ok || len(raw) == 0 {
		return nil
	}
	out := make([]string, 0, len(raw))
	for _, x := range raw {
		if s, ok := x.(string); ok && s != "" {
			out = append(out, s)
		}
	}
	return out
}

type modelCostAgg struct {
	Total float64 `json:"total"`
	Rows  int     `json:"rows"`
}

func usageCostEstimate(workspace string, windowHours int, cfgOverrides []config.UsagePricingOverrideEntry, usageCostCurrency string, defaultUsdToCny float64) (string, error) {
	events, err := readUsageEventsInWindow(workspace, windowHours)
	if err != nil {
		return "", err
	}
	fileOverrides, _ := ReadWorkspacePricingOverrides(workspace)
	cur := NormalizeUsageCostCurrency(usageCostCurrency)
	fx := EffectiveUsdToCny(defaultUsdToCny)
	var pricedTotal float64
	var unknownRows int
	totPrompt, totComp := 0, 0
	byModel := map[string]*modelCostAgg{}
	pricedStatus := map[string]int{}
	for _, ev := range events {
		m, _ := ev["model"].(string)
		if m == "" {
			m = "unknown"
		}
		pt := int(num(ev["promptTokens"]))
		ct := int(num(ev["completionTokens"]))
		totPrompt += pt
		totComp += ct
		u, st := EstimateUsageCostLayered(m, pt, ct, cur, defaultUsdToCny, cfgOverrides, fileOverrides)
		pricedStatus[st]++
		if byModel[m] == nil {
			byModel[m] = &modelCostAgg{}
		}
		byModel[m].Rows++
		if st == "unknown_model" {
			unknownRows++
		} else {
			byModel[m].Total += u
			pricedTotal += u
		}
	}
	outModel := make(map[string]modelCostAgg, len(byModel))
	for k, v := range byModel {
		outModel[k] = *v
	}
	out := map[string]any{
		"success":                   true,
		"currency":                  cur,
		"window_hours":              windowHours,
		"rows":                      len(events),
		"prompt_tokens":             totPrompt,
		"completion_tokens":         totComp,
		"estimated_priced_total":    round4(pricedTotal),
		"rows_unknown_model":        unknownRows,
		"priced_status_breakdown":   pricedStatus,
		"pricing_note":              "order: experience.usagePricingOverrides, memory/pricing_overrides.json, then built-in USD/M table; not billing truth",
		"estimated_priced_by_model": outModel,
	}
	if cur == "CNY" {
		out["default_usd_to_cny"] = fx
	}
	b, _ := json.Marshal(out)
	return string(b), nil
}

func round4(x float64) float64 {
	return float64(int(x*10000+0.5)) / 10000
}

func num(v any) float64 {
	switch x := v.(type) {
	case float64:
		return x
	case int:
		return float64(x)
	case int64:
		return float64(x)
	default:
		return 0
	}
}
