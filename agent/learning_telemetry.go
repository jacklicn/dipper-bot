package agent

import (
	"compress/gzip"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/jacklicn/dipper-bot/config"
	"github.com/jacklicn/dipper-bot/providers"
	"github.com/jacklicn/dipper-bot/session"
)

type learningEvent struct {
	Time       string         `json:"time"`
	Category   string         `json:"category"` // memory|skill|governance
	SessionKey string         `json:"sessionKey,omitempty"`
	Outcome    string         `json:"outcome"` // success|rollback|drop|noop|audit
	Reason     string         `json:"reason,omitempty"`
	Quality    int            `json:"quality,omitempty"`
	Confidence int            `json:"confidence,omitempty"`
	Meta       map[string]any `json:"meta,omitempty"`
}

type learningKPI struct {
	WindowHours   int                `json:"windowHours"`
	TotalEvents   int                `json:"totalEvents"`
	SuccessRate   float64            `json:"successRate"`
	RollbackRate  float64            `json:"rollbackRate"`
	DropRate      float64            `json:"dropRate"`
	ByCategory    map[string]float64 `json:"byCategory"`
	LastUpdatedAt string             `json:"lastUpdatedAt"`
}

type LearningTelemetry struct {
	workspace string
	mu        sync.Mutex
}

func NewLearningTelemetry(workspace string) *LearningTelemetry {
	if strings.TrimSpace(workspace) == "" {
		return nil
	}
	return &LearningTelemetry{workspace: workspace}
}

func (t *LearningTelemetry) Record(ev learningEvent) {
	if t == nil {
		return
	}
	if ev.Time == "" {
		ev.Time = time.Now().UTC().Format(time.RFC3339)
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	p := filepath.Join(t.workspace, "memory", "learning_telemetry.jsonl")
	_ = os.MkdirAll(filepath.Dir(p), 0o750)
	b, _ := json.Marshal(ev)
	f, err := os.OpenFile(p, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		return
	}
	defer f.Close()
	_, _ = f.Write(append(b, '\n'))
}

func (t *LearningTelemetry) ComputeKPI(windowHours int) learningKPI {
	k := learningKPI{
		WindowHours: windowHours,
		ByCategory:  map[string]float64{},
	}
	if t == nil || windowHours <= 0 {
		return k
	}
	p := filepath.Join(t.workspace, "memory", "learning_telemetry.jsonl")
	b, err := os.ReadFile(p)
	if err != nil || len(b) == 0 {
		return k
	}
	threshold := time.Now().Add(-time.Duration(windowHours) * time.Hour)
	lines := strings.Split(string(b), "\n")
	total := 0
	success := 0
	rollback := 0
	drop := 0
	catCount := map[string]int{}
	for _, ln := range lines {
		ln = strings.TrimSpace(ln)
		if ln == "" {
			continue
		}
		var ev learningEvent
		if err := json.Unmarshal([]byte(ln), &ev); err != nil {
			continue
		}
		ts, err := time.Parse(time.RFC3339, ev.Time)
		if err != nil || ts.Before(threshold) {
			continue
		}
		total++
		catCount[ev.Category]++
		switch ev.Outcome {
		case "success":
			success++
		case "rollback":
			rollback++
		case "drop":
			drop++
		}
	}
	k.TotalEvents = total
	if total > 0 {
		k.SuccessRate = float64(success) / float64(total)
		k.RollbackRate = float64(rollback) / float64(total)
		k.DropRate = float64(drop) / float64(total)
		for cat, n := range catCount {
			k.ByCategory[cat] = float64(n) / float64(total)
		}
	}
	k.LastUpdatedAt = time.Now().UTC().Format(time.RFC3339)
	return k
}

func (t *LearningTelemetry) SaveKPISnapshot(windowHours int) {
	if t == nil {
		return
	}
	k := t.ComputeKPI(windowHours)
	b, _ := json.MarshalIndent(k, "", "  ")
	p := filepath.Join(t.workspace, "memory", "learning_kpi.json")
	_ = os.MkdirAll(filepath.Dir(p), 0o750)
	_ = os.WriteFile(p, b, 0o600)
}

type LearningGovernance struct {
	workspace string
	sessions  *session.SessionManager
	telemetry *LearningTelemetry
	provider  providers.LLMProvider
	model     string
	memCfg    config.MemoryMaintenanceConfig
}

func NewLearningGovernance(workspace string, sessions *session.SessionManager, telemetry *LearningTelemetry, provider providers.LLMProvider, model string, memCfg config.MemoryMaintenanceConfig) *LearningGovernance {
	if strings.TrimSpace(workspace) == "" || sessions == nil {
		return nil
	}
	return &LearningGovernance{workspace: workspace, sessions: sessions, telemetry: telemetry, provider: provider, model: model, memCfg: memCfg}
}

func (g *LearningGovernance) AuditNow() {
	if g == nil {
		return
	}
	changed := g.rewriteDedupMemory("USER.md")
	changed = g.rewriteDedupMemory("NOTE.md") || changed
	g.tierMemoryByTemperature()
	g.regroupMemoryByTopic()
	g.archiveSessionIndex()
	g.compressOldArchives(7 * 24 * time.Hour)
	if g.telemetry != nil {
		g.telemetry.SaveKPISnapshot(24 * 7)
		g.telemetry.Record(learningEvent{
			Category: "governance",
			Outcome:  "audit",
			Reason:   "periodic",
			Meta:     map[string]any{"memoryRewritten": changed},
		})
	}
}

func (g *LearningGovernance) tierMemoryByTemperature() {
	memDir := filepath.Join(g.workspace, "memory")
	for _, name := range []string{"USER.md", "NOTE.md"} {
		p := filepath.Join(memDir, name)
		b, err := os.ReadFile(p)
		if err != nil || len(b) == 0 {
			continue
		}
		entries := strings.Split(string(b), "\n§\n")
		hot := make([]string, 0, len(entries))
		cold := make([]string, 0, len(entries))
		for _, e := range entries {
			ne := normalizeMemoryText(e)
			if ne == "" {
				continue
			}
			if len(strings.Fields(ne)) >= 8 {
				hot = append(hot, strings.TrimSpace(e))
			} else {
				cold = append(cold, strings.TrimSpace(e))
			}
		}
		_ = os.WriteFile(filepath.Join(memDir, strings.TrimSuffix(name, ".md")+"_HOT.md"), []byte(strings.Join(hot, "\n§\n")), 0o600)
		_ = os.WriteFile(filepath.Join(memDir, strings.TrimSuffix(name, ".md")+"_COLD.md"), []byte(strings.Join(cold, "\n§\n")), 0o600)
	}
}

func (g *LearningGovernance) regroupMemoryByTopic() {
	memDir := filepath.Join(g.workspace, "memory")
	for _, name := range []string{"USER.md", "NOTE.md"} {
		p := filepath.Join(memDir, name)
		b, err := os.ReadFile(p)
		if err != nil || len(b) == 0 {
			continue
		}
		entries := strings.Split(string(b), "\n§\n")
		topics := map[string][]string{}
		for _, e := range entries {
			ne := normalizeMemoryText(e)
			if ne == "" {
				continue
			}
			topic := topicForEntry(ne)
			topics[topic] = append(topics[topic], strings.TrimSpace(e))
		}
		out := make(map[string]any, len(topics))
		for k, v := range topics {
			out[k] = v
		}
		basePath := filepath.Join(memDir, strings.TrimSuffix(name, ".md")+"_TOPICS.json")
		bj, _ := json.MarshalIndent(out, "", "  ")
		_ = os.WriteFile(basePath, bj, 0o600)
		g.semanticRefineTopics(name, basePath)
	}
}

func topicForEntry(s string) string {
	l := strings.ToLower(s)
	switch {
	case strings.Contains(l, "prefer"), strings.Contains(l, "style"), strings.Contains(l, "tone"):
		return "preferences"
	case strings.Contains(l, "path"), strings.Contains(l, "file"), strings.Contains(l, "repo"), strings.Contains(l, "tool"):
		return "workflow"
	case strings.Contains(l, "error"), strings.Contains(l, "fix"), strings.Contains(l, "warning"):
		return "troubleshooting"
	default:
		return "general"
	}
}

func (g *LearningGovernance) semanticRefineTopics(sourceName, topicsPath string) {
	if g == nil || g.provider == nil || strings.TrimSpace(g.model) == "" {
		return
	}
	maxEntries := g.memCfg.SemanticRegroupMaxEntries
	if maxEntries <= 0 {
		maxEntries = 40
	}
	maxGroups := g.memCfg.SemanticRegroupMaxGroups
	if maxGroups <= 0 {
		maxGroups = 6
	}
	raw, err := os.ReadFile(topicsPath)
	if err != nil || len(raw) == 0 {
		return
	}
	var coarse map[string][]string
	if err := json.Unmarshal(raw, &coarse); err != nil {
		return
	}
	type item struct {
		Topic string `json:"topic"`
		Text  string `json:"text"`
	}
	var items []item
	for topic, entries := range coarse {
		for _, e := range entries {
			t := strings.TrimSpace(e)
			if t == "" {
				continue
			}
			items = append(items, item{Topic: topic, Text: t})
		}
	}
	if len(items) == 0 {
		return
	}
	sort.Slice(items, func(i, j int) bool { return len(items[i].Text) > len(items[j].Text) })
	if len(items) > maxEntries {
		items = items[:maxEntries]
	}
	payload, _ := json.Marshal(items)
	system := `You are a memory taxonomy editor.
Input is JSON: array of {topic,text} from a coarse heuristic classifier.
Task: merge semantically similar items into stable topic buckets.
Rules:
- Output ONLY valid JSON object: map[string][]string where keys are topic names (lowercase kebab-case, max ` + strconv.Itoa(maxGroups) + ` keys).
- Merge duplicates and near-duplicates.
- Prefer fewer, clearer topics.
- Each value is a list of concise memory lines (deduped).`
	user := "Source: " + sourceName + "\nJSON:\n" + string(payload)
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	resp, err := g.provider.Chat(ctx, &providers.ChatRequest{
		Model:       g.model,
		Messages:    []providers.Message{{Role: "system", Content: system}, {Role: "user", Content: user}},
		MaxTokens:   900,
		Temperature: 0.1,
	})
	if err != nil || strings.TrimSpace(resp.Content) == "" {
		return
	}
	clean := strings.TrimSpace(resp.Content)
	if strings.HasPrefix(clean, "```") {
		clean = stripMarkdownFence(clean)
	}
	var refined map[string][]string
	if err := json.Unmarshal([]byte(clean), &refined); err != nil {
		return
	}
	bj, _ := json.MarshalIndent(refined, "", "  ")
	out := strings.TrimSuffix(topicsPath, ".json") + "_SEMANTIC.json"
	_ = os.WriteFile(out, bj, 0o600)
}

func stripMarkdownFence(s string) string {
	s = strings.TrimSpace(s)
	s = strings.TrimPrefix(s, "```json")
	s = strings.TrimPrefix(s, "```JSON")
	s = strings.TrimPrefix(s, "```")
	if idx := strings.LastIndex(s, "```"); idx >= 0 {
		s = s[:idx]
	}
	return strings.TrimSpace(s)
}

func (g *LearningGovernance) rewriteDedupMemory(name string) bool {
	p := filepath.Join(g.workspace, "memory", name)
	b, err := os.ReadFile(p)
	if err != nil || len(b) == 0 {
		return false
	}
	parts := strings.Split(string(b), "\n§\n")
	seen := map[string]struct{}{}
	out := make([]string, 0, len(parts))
	for _, part := range parts {
		norm := normalizeMemoryText(part)
		if norm == "" {
			continue
		}
		if _, ok := seen[norm]; ok {
			continue
		}
		seen[norm] = struct{}{}
		out = append(out, strings.TrimSpace(part))
	}
	rewritten := strings.Join(out, "\n§\n")
	if normalizeMemoryText(string(b)) == normalizeMemoryText(rewritten) {
		return false
	}
	_ = os.WriteFile(p, []byte(rewritten), 0o600)
	return true
}

func (g *LearningGovernance) archiveSessionIndex() {
	infos, err := g.sessions.ListSessions(0)
	if err != nil || len(infos) == 0 {
		return
	}
	type row struct {
		Key       string `json:"key"`
		UpdatedAt string `json:"updatedAt"`
		MsgCount  int    `json:"msgCount"`
	}
	out := make([]row, 0, len(infos))
	for _, inf := range infos {
		out = append(out, row{
			Key:       inf.Key,
			UpdatedAt: inf.UpdatedAt.Format(time.RFC3339),
			MsgCount:  inf.MsgCount,
		})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].UpdatedAt > out[j].UpdatedAt })
	b, _ := json.MarshalIndent(out, "", "  ")
	name := "sessions-" + time.Now().UTC().Format("20060102") + ".json"
	p := filepath.Join(g.workspace, "memory", "archive", name)
	_ = os.MkdirAll(filepath.Dir(p), 0o750)
	_ = os.WriteFile(p, b, 0o600)
}

func (g *LearningGovernance) compressOldArchives(olderThan time.Duration) {
	dir := filepath.Join(g.workspace, "memory", "archive")
	entries, err := os.ReadDir(dir)
	if err != nil {
		return
	}
	threshold := time.Now().Add(-olderThan)
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(strings.ToLower(e.Name()), ".json") {
			continue
		}
		full := filepath.Join(dir, e.Name())
		st, err := os.Stat(full)
		if err != nil || st.ModTime().After(threshold) {
			continue
		}
		gzipPath := full + ".gz"
		if _, err := os.Stat(gzipPath); err == nil {
			continue
		}
		src, err := os.ReadFile(full)
		if err != nil {
			continue
		}
		f, err := os.Create(gzipPath)
		if err != nil {
			continue
		}
		zw := gzip.NewWriter(f)
		_, _ = zw.Write(src)
		_ = zw.Close()
		_ = f.Close()
		_ = os.Remove(full)
	}
}

