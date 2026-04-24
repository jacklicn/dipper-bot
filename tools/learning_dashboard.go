package tools

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// ControllerStatePath returns the persisted adaptive controller state file path.
func ControllerStatePath(workspace, target string) string {
	name := "adaptive_controller_state_memory.json"
	if strings.EqualFold(target, "skill") {
		name = "adaptive_controller_state_skill.json"
	}
	return filepath.Join(workspace, "memory", name)
}

// ReadControllerState reads JSON state for the adaptive controller.
func ReadControllerState(path string) (map[string]any, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var st map[string]any
	if err := json.Unmarshal(b, &st); err != nil {
		return nil, err
	}
	return st, nil
}

// WriteControllerState writes JSON state for the adaptive controller.
func WriteControllerState(path string, st map[string]any) error {
	b, err := json.MarshalIndent(st, "", "  ")
	if err != nil {
		return err
	}
	_ = os.MkdirAll(filepath.Dir(path), 0o750)
	return os.WriteFile(path, b, 0o600)
}

// MergePatch merges shallow keys into dst.
func MergePatch(dst map[string]any, patch map[string]any) {
	for k, v := range patch {
		dst[k] = v
	}
}

// ComputeCategoryBadRates returns per-category bad-rate (rollback+drop) in a time window.
func ComputeCategoryBadRates(lines []string, windowHours int) map[string]map[string]float64 {
	threshold := time.Now().Add(-time.Duration(windowHours) * time.Hour)
	type acc struct {
		total int
		bad   int
	}
	m := map[string]*acc{}
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
		cat, _ := obj["category"].(string)
		if cat == "" {
			cat = "unknown"
		}
		if _, ok := m[cat]; !ok {
			m[cat] = &acc{}
		}
		m[cat].total++
		out, _ := obj["outcome"].(string)
		if out == "rollback" || out == "drop" {
			m[cat].bad++
		}
	}
	out := map[string]map[string]float64{}
	for cat, a := range m {
		if a.total <= 0 {
			continue
		}
		out[cat] = map[string]float64{
			"bad_rate": float64(a.bad) / float64(a.total),
		}
	}
	return out
}

func recentTelemetryEventMaps(lines []string, limit int) []map[string]any {
	var out []map[string]any
	for i := len(lines) - 1; i >= 0 && len(out) < limit; i-- {
		ln := strings.TrimSpace(lines[i])
		if ln == "" {
			continue
		}
		var obj map[string]any
		if err := json.Unmarshal([]byte(ln), &obj); err != nil {
			continue
		}
		out = append(out, obj)
	}
	for i, j := 0, len(out)-1; i < j; i, j = i+1, j-1 {
		out[i], out[j] = out[j], out[i]
	}
	return out
}

// BuildLearningDashboardMap aggregates dashboard-friendly telemetry + controller state.
func BuildLearningDashboardMap(workspace string, lines []string, windowHours int) (map[string]any, error) {
	kpiRaw, _ := os.ReadFile(filepath.Join(workspace, "memory", "learning_kpi.json"))
	memState, _ := ReadControllerState(ControllerStatePath(workspace, "memory"))
	skillState, _ := ReadControllerState(ControllerStatePath(workspace, "skill"))
	kpiStr, err := computeToolKPI(lines, windowHours)
	if err != nil {
		return nil, err
	}
	var kpiObj map[string]any
	_ = json.Unmarshal([]byte(kpiStr), &kpiObj)
	byCat := ComputeCategoryBadRates(lines, windowHours)
	recent := recentTelemetryEventMaps(lines, 120)
	return map[string]any{
		"success":            true,
		"window_hours":       windowHours,
		"kpi":                kpiObj,
		"rates_by_category":  byCat,
		"recent_events":      recent,
		"controller": map[string]any{
			"memory": memState,
			"skill":  skillState,
		},
		"kpi_snapshot_raw": strings.TrimSpace(string(kpiRaw)),
	}, nil
}
