package tools

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// LearningTelemetryTool provides observability over learning events and KPI snapshots.
type LearningTelemetryTool struct {
	Workspace string
}

func (t *LearningTelemetryTool) Name() string { return "learning_telemetry" }

func (t *LearningTelemetryTool) Description() string {
	return `Query learning telemetry, KPI snapshots, adaptive controller state, and dashboard bundles.
Actions:
- kpi (window_hours optional)
- recent (limit optional)
- session (session_key required)
- dashboard_json (window_hours optional)
- controller_get (target: memory|skill)
- controller_patch (target + patch object with optional fields: targetBadRatio,kp,ki,kd,minFloor,maxFloor,onlineTuning,qualityFloor,confidenceFloor)`
}

func (t *LearningTelemetryTool) Parameters() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"action": map[string]any{"type": "string", "enum": []any{"kpi", "recent", "session", "dashboard_json", "controller_get", "controller_patch"}},
			"window_hours": map[string]any{"type": "integer"},
			"limit": map[string]any{"type": "integer"},
			"session_key": map[string]any{"type": "string"},
			"target": map[string]any{"type": "string", "enum": []any{"memory", "skill"}},
			"patch": map[string]any{"type": "object"},
		},
	}
}

func (t *LearningTelemetryTool) Execute(ctx context.Context, params map[string]any) (string, error) {
	action, _ := params["action"].(string)
	if strings.TrimSpace(action) == "" {
		action = "kpi"
	}
	p := filepath.Join(t.Workspace, "memory", "learning_telemetry.jsonl")
	raw, _ := os.ReadFile(p)
	lines := strings.Split(strings.TrimSpace(string(raw)), "\n")
	switch action {
	case "recent":
		limit := 20
		if v, ok := params["limit"].(float64); ok && int(v) > 0 {
			limit = int(v)
		}
		if limit > 200 {
			limit = 200
		}
		start := len(lines) - limit
		if start < 0 {
			start = 0
		}
		return marshalTelemetryRows(lines[start:])
	case "session":
		sessionKey, _ := params["session_key"].(string)
		sessionKey = strings.TrimSpace(sessionKey)
		if sessionKey == "" {
			return `{"success":false,"error":"session_key required for session action"}`, nil
		}
		filtered := make([]string, 0, 16)
		for _, ln := range lines {
			if strings.Contains(ln, `"sessionKey":"`+sessionKey+`"`) {
				filtered = append(filtered, ln)
			}
		}
		return marshalTelemetryRows(filtered)
	case "dashboard_json":
		window := 168
		if v, ok := params["window_hours"].(float64); ok && int(v) > 0 {
			window = int(v)
		}
		m, err := BuildLearningDashboardMap(t.Workspace, lines, window)
		if err != nil {
			return "", err
		}
		b, err := json.Marshal(m)
		return string(b), err
	case "controller_get":
		target, _ := params["target"].(string)
		target = strings.TrimSpace(strings.ToLower(target))
		if target != "memory" && target != "skill" {
			return `{"success":false,"error":"target must be memory|skill"}`, nil
		}
		st, err := ReadControllerState(ControllerStatePath(t.Workspace, target))
		if err != nil {
			return `{"success":false,"error":"` + strings.ReplaceAll(err.Error(), `"`, `'`) + `"}`, nil
		}
		b, _ := json.Marshal(map[string]any{"success": true, "target": target, "state": st})
		return string(b), nil
	case "controller_patch":
		target, _ := params["target"].(string)
		target = strings.TrimSpace(strings.ToLower(target))
		if target != "memory" && target != "skill" {
			return `{"success":false,"error":"target must be memory|skill"}`, nil
		}
		patch, _ := params["patch"].(map[string]any)
		if patch == nil {
			return `{"success":false,"error":"patch object required"}`, nil
		}
		p := ControllerStatePath(t.Workspace, target)
		st, err := ReadControllerState(p)
		if err != nil {
			st = map[string]any{}
		}
		MergePatch(st, patch)
		if err := WriteControllerState(p, st); err != nil {
			return `{"success":false,"error":"` + strings.ReplaceAll(err.Error(), `"`, `'`) + `"}`, nil
		}
		b, _ := json.Marshal(map[string]any{"success": true, "target": target, "state": st})
		return string(b), nil
	default: // kpi
		window := 168
		if v, ok := params["window_hours"].(float64); ok && int(v) > 0 {
			window = int(v)
		}
		return computeToolKPI(lines, window)
	}
}

func marshalTelemetryRows(lines []string) (string, error) {
	rows := make([]map[string]any, 0, len(lines))
	for _, ln := range lines {
		ln = strings.TrimSpace(ln)
		if ln == "" {
			continue
		}
		var obj map[string]any
		if err := json.Unmarshal([]byte(ln), &obj); err != nil {
			continue
		}
		rows = append(rows, obj)
	}
	b, _ := json.Marshal(map[string]any{"success": true, "count": len(rows), "rows": rows})
	return string(b), nil
}

func computeToolKPI(lines []string, windowHours int) (string, error) {
	threshold := time.Now().Add(-time.Duration(windowHours) * time.Hour)
	total := 0
	success := 0
	rollback := 0
	drop := 0
	for _, ln := range lines {
		ln = strings.TrimSpace(ln)
		if ln == "" {
			continue
		}
		var obj map[string]any
		if err := json.Unmarshal([]byte(ln), &obj); err != nil {
			continue
		}
		ts, _ := obj["time"].(string)
		t, err := time.Parse(time.RFC3339, ts)
		if err != nil || t.Before(threshold) {
			continue
		}
		total++
		out, _ := obj["outcome"].(string)
		switch out {
		case "success":
			success++
		case "rollback":
			rollback++
		case "drop":
			drop++
		}
	}
	resp := map[string]any{
		"success":      true,
		"window_hours": windowHours,
		"total_events": total,
	}
	if total > 0 {
		resp["success_rate"] = float64(success) / float64(total)
		resp["rollback_rate"] = float64(rollback) / float64(total)
		resp["drop_rate"] = float64(drop) / float64(total)
	}
	b, _ := json.Marshal(resp)
	return string(b), nil
}

