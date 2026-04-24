package agent

import (
	"context"
	"encoding/json"
	"log/slog"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/jacklicn/dipper-bot/config"
	"github.com/jacklicn/dipper-bot/providers"
	"github.com/jacklicn/dipper-bot/tools"
)

type skillTurn struct {
	sessionKey string
	userMsg    string
	asstMsg    string
	toolNames  []string
}

// SkillMaintainer maintains procedural skills in workspace/skills.
type SkillMaintainer struct {
	provider     providers.LLMProvider
	model        string
	tool         *tools.SkillManageTool
	queue        chan skillTurn
	cfg          config.SkillsEvolutionConfig
	mu           sync.Mutex
	turns        map[string]int
	lastRun      map[string]time.Time
	lastDecision map[string]time.Time
	failureUntil map[string]time.Time
	failures     map[string]int
	lowQuality   map[string]int
	qualityFloor int
	confFloor    int
	goodEvents   int
	badEvents    int
	controller   *AdaptiveThresholdController
	telemetry    *LearningTelemetry
	midRunMu     sync.Mutex
	lastMidRun   map[string]time.Time
	// onSkillApplied is optional (e.g. bus follow-up when async worker updates a skill).
	onSkillApplied func(notice SkillApplyNotice, sessionKey string)
}

func NewSkillMaintainer(provider providers.LLMProvider, model, workspace string, cfg config.SkillsEvolutionConfig, telemetry *LearningTelemetry) *SkillMaintainer {
	if provider == nil || model == "" || workspace == "" || !cfg.EvolutionEnabled() {
		return nil
	}
	if cfg.CreationNudgeInterval <= 0 {
		cfg.CreationNudgeInterval = 15
	}
	if cfg.MinToolCalls <= 0 {
		cfg.MinToolCalls = 5
	}
	if cfg.MinIntervalMinutes <= 0 {
		cfg.MinIntervalMinutes = 30
	}
	if cfg.MinQualityScore <= 0 {
		cfg.MinQualityScore = 60
	}
	if cfg.FlushMinToolCalls <= 0 {
		cfg.FlushMinToolCalls = cfg.MinToolCalls
	}
	if cfg.RepeatSuppressionMinutes <= 0 {
		cfg.RepeatSuppressionMinutes = 60
	}
	if cfg.FailureBackoffMinutes <= 0 {
		cfg.FailureBackoffMinutes = 30
	}
	if cfg.MinConfidence <= 0 {
		cfg.MinConfidence = 60
	}
	if cfg.ControllerTargetBadRate <= 0 {
		cfg.ControllerTargetBadRate = 0.08
	}
	if cfg.ControllerKp == 0 {
		cfg.ControllerKp = 16
	}
	if cfg.ControllerKi == 0 {
		cfg.ControllerKi = 4
	}
	if cfg.ControllerKd == 0 {
		cfg.ControllerKd = 6
	}
	if cfg.ControllerBatchSize <= 0 {
		cfg.ControllerBatchSize = 8
	}
	if cfg.ControllerMinFloor <= 0 {
		cfg.ControllerMinFloor = 40
	}
	if cfg.ControllerMaxFloor <= 0 {
		cfg.ControllerMaxFloor = 95
	}
	if cfg.ControllerOnlineTuning == nil {
		v := true
		cfg.ControllerOnlineTuning = &v
	}
	onlineTune := *cfg.ControllerOnlineTuning
	statePath := filepath.Join(workspace, "memory", "adaptive_controller_state_skill.json")
	controller := NewAdaptiveThresholdController(cfg.MinQualityScore, cfg.MinConfidence, AdaptiveControllerParams{
		TargetBadRatio: cfg.ControllerTargetBadRate,
		Kp:             cfg.ControllerKp,
		Ki:             cfg.ControllerKi,
		Kd:             cfg.ControllerKd,
		MinFloor:       cfg.ControllerMinFloor,
		MaxFloor:       cfg.ControllerMaxFloor,
		OnlineTuning:   onlineTune,
		StatePath:      statePath,
	})
	qf, cf := controller.Current()
	m := &SkillMaintainer{
		provider:     provider,
		model:        model,
		tool:         &tools.SkillManageTool{Workspace: workspace},
		queue:        make(chan skillTurn, 32),
		cfg:          cfg,
		turns:        make(map[string]int),
		lastRun:      make(map[string]time.Time),
		lastDecision: make(map[string]time.Time),
		failureUntil: make(map[string]time.Time),
		failures:     make(map[string]int),
		lowQuality:   make(map[string]int),
		qualityFloor: qf,
		confFloor:    cf,
		controller:   controller,
		telemetry:    telemetry,
		lastMidRun:   make(map[string]time.Time),
	}
	go m.worker()
	return m
}

func (m *SkillMaintainer) Enqueue(sessionKey, userMsg, asstMsg string, toolNames []string) {
	if m == nil || len(toolNames) < m.cfg.MinToolCalls {
		return
	}
	if !m.shouldRun(sessionKey) {
		return
	}
	select {
	case m.queue <- skillTurn{
		sessionKey: sessionKey,
		userMsg:    clipRunes(userMsg, 2500),
		asstMsg:    clipRunes(asstMsg, 2500),
		toolNames:  dedupeStrings(toolNames),
	}:
		m.noteQueuedRun(sessionKey)
	default:
	}
}

func (m *SkillMaintainer) shouldRun(sessionKey string) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.turns[sessionKey]++
	if until := m.failureUntil[sessionKey]; !until.IsZero() && time.Now().Before(until) {
		return false
	}
	if m.turns[sessionKey]%m.cfg.CreationNudgeInterval != 0 {
		return false
	}
	last := m.lastRun[sessionKey]
	if !last.IsZero() && time.Since(last) < time.Duration(m.cfg.MinIntervalMinutes)*time.Minute {
		return false
	}
	return true
}

func (m *SkillMaintainer) noteQueuedRun(sessionKey string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.lastRun[sessionKey] = time.Now()
}

// FlushFromTurn forces one skill evolution pass during lifecycle events.
func (m *SkillMaintainer) FlushFromTurn(sessionKey, userMsg, asstMsg string, toolNames []string) {
	if m == nil || len(toolNames) < m.cfg.FlushMinToolCalls {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	notice, _ := m.reflectAndApplyMode(ctx, skillTurn{
		sessionKey: sessionKey,
		userMsg:    clipRunes(userMsg, 2500),
		asstMsg:    clipRunes(asstMsg, 2500),
		toolNames:  dedupeStrings(toolNames),
	}, false)
	if notice != nil && m.onSkillApplied != nil {
		m.onSkillApplied(*notice, sessionKey)
	}
}

func (m *SkillMaintainer) worker() {
	for t := range m.queue {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		notice, _ := m.reflectAndApplyMode(ctx, t, false)
		if notice != nil && m.onSkillApplied != nil {
			m.onSkillApplied(*notice, t.sessionKey)
		}
		cancel()
	}
}

func (m *SkillMaintainer) reflectAndApply(ctx context.Context, t skillTurn) error {
	_, err := m.reflectAndApplyMode(ctx, t, false)
	return err
}

func (m *SkillMaintainer) reflectAndApplyMode(ctx context.Context, t skillTurn, midRun bool) (*SkillApplyNotice, error) {
	system := `You maintain reusable procedural skills.
If the turn contains a reusable workflow, output one operation.
Output ONLY lines:
NOOP: yes|no
CONFIDENCE: <0-100>
ACTION: create|patch
NAME: <skill-name-kebab-case>
CONTENT: <full SKILL.md content for create/edit>
OLD_TEXT: <required for patch>
NEW_TEXT: <required for patch>
Rules:
- Prefer create for new workflows.
- Use patch only when a concrete existing skill should be fixed.
- Keep skill focused and reusable.
`
	if midRun {
		system += `

Mid-run mode: the user message may continue with more tool rounds after this snapshot. Prefer ACTION: patch when improving an existing skill that clearly matches this workflow; use create only for genuinely new reusable procedures. When unsure, answer NOOP: yes.
`
	}
	existing := strings.Join(m.listExistingSkills(), ", ")
	if existing == "" {
		existing = "(none)"
	}
	user := "Session: " + t.sessionKey +
		"\nTools used: " + strings.Join(t.toolNames, ", ") +
		"\nExisting skills: " + existing +
		"\nUser:\n" + t.userMsg +
		"\nAssistant:\n" + t.asstMsg
	resp, err := m.provider.Chat(ctx, &providers.ChatRequest{
		Model:       m.model,
		Messages:    []providers.Message{{Role: "system", Content: system}, {Role: "user", Content: user}},
		MaxTokens:   900,
		Temperature: 0.2,
	})
	if err != nil {
		return nil, err
	}
	noop, confidence, action, name, content, oldText, newText := parseSkillReview(resp.Content)
	if noop {
		return nil, nil
	}
	if confidence < m.currentConfidenceFloor() {
		m.markLowQuality(t.sessionKey)
		m.record(t.sessionKey, "drop", "low_confidence", 0, confidence, name)
		return nil, nil
	}
	name = normalizeSkillName(name)
	action = normalizeSkillAction(action)
	if name == "" || action == "" {
		return nil, nil
	}
	if strings.HasPrefix(name, "temp") || strings.HasPrefix(name, "test") || strings.HasPrefix(name, "new-skill") {
		return nil, nil // denoise
	}
	score := scoreSkillCandidate(action, name, content, t.toolNames)
	if score < m.currentQualityFloor() {
		m.markLowQuality(t.sessionKey)
		m.record(t.sessionKey, "drop", "low_quality", score, confidence, name)
		return nil, nil
	}
	if action == "create" && m.isNearDuplicateSkill(name, content) {
		return nil, nil // drift guard: avoid duplicate skill creation
	}
	fp := skillDecisionKey(t.sessionKey, action, name, content, oldText, newText)
	if m.isRepeatedDecision(fp) {
		return nil, nil
	}
	skillPath := filepath.Join(m.tool.Workspace, "skills", name, "SKILL.md")
	prev, _ := os.ReadFile(skillPath)

	var notice *SkillApplyNotice
	switch action {
	case "create":
		content = strings.TrimSpace(content)
		if content == "" {
			return nil, nil
		}
		result, _ := m.tool.Execute(ctx, map[string]any{"action": "create", "name": name, "content": content})
		if !skillToolSuccess(result) || !validateSkillFile(skillPath) {
			// Fallback depth: if skill already exists, attempt patch by replacing full file body.
			if len(prev) > 0 {
				patchResult, _ := m.tool.Execute(ctx, map[string]any{
					"action":     "patch",
					"name":       name,
					"old_string": string(prev),
					"new_string": content,
				})
				if !skillToolSuccess(patchResult) || !validateSkillFile(skillPath) {
					_ = rollbackSkillFile(skillPath, prev)
					m.markFailure(t.sessionKey)
					m.record(t.sessionKey, "rollback", "create_and_patch_fallback_failed", score, confidence, name)
					return nil, nil
				}
				notice = &SkillApplyNotice{Action: "patch", Name: name, MidRun: midRun}
				break
			}
			_ = rollbackSkillFile(skillPath, prev)
			m.markFailure(t.sessionKey)
			m.record(t.sessionKey, "rollback", "create_failed", score, confidence, name)
			return nil, nil
		}
		notice = &SkillApplyNotice{Action: "create", Name: name, MidRun: midRun}
	case "patch":
		oldText = strings.TrimSpace(oldText)
		newText = strings.TrimSpace(newText)
		if oldText == "" || newText == "" {
			return nil, nil
		}
		result, _ := m.tool.Execute(ctx, map[string]any{"action": "patch", "name": name, "old_string": oldText, "new_string": newText})
		if !skillToolSuccess(result) || !validateSkillFile(skillPath) {
			_ = rollbackSkillFile(skillPath, prev)
			m.markFailure(t.sessionKey)
			m.record(t.sessionKey, "rollback", "patch_failed", score, confidence, name)
			return nil, nil
		}
		notice = &SkillApplyNotice{Action: "patch", Name: name, MidRun: midRun}
	default:
		return nil, nil
	}
	m.markSuccess(t.sessionKey, fp)
	m.record(t.sessionKey, "success", "applied", score, confidence, name)
	return notice, nil
}

// MaybeReflectMidRun runs an extra skill reflect pass during a single user turn (between tool rounds).
// It reuses the same quality gates as post-turn evolution but is throttled by MidRunReflectEvery and cooldown.
// When a skill file is written, returns a notice so the caller can append user-visible feedback to the reply.
func (m *SkillMaintainer) MaybeReflectMidRun(ctx context.Context, sessionKey string, completedToolIter int, userMsg, asstThinking string, toolNames []string) *SkillApplyNotice {
	if m == nil {
		return nil
	}
	n := m.cfg.MidRunReflectEvery()
	if n <= 0 {
		return nil
	}
	if (completedToolIter+1)%n != 0 {
		return nil
	}
	if len(toolNames) < m.cfg.MinToolCalls {
		return nil
	}
	for _, name := range toolNames {
		if name == "skill_manage" {
			return nil
		}
	}
	minGap := time.Duration(m.cfg.MidRunReflectCooldownSeconds()) * time.Second
	m.midRunMu.Lock()
	last := m.lastMidRun[sessionKey]
	if !last.IsZero() && time.Since(last) < minGap {
		m.midRunMu.Unlock()
		return nil
	}
	m.lastMidRun[sessionKey] = time.Now()
	m.midRunMu.Unlock()

	userMsg = strings.TrimSpace(userMsg)
	if userMsg == "" {
		userMsg = "(no user text in context)"
	}
	asst := strings.TrimSpace(asstThinking)
	if asst == "" {
		asst = "(assistant message had no prose before tool calls this round)"
	}
	asst += "\n\n[Mid-run snapshot: user turn may continue; tools this round: " + strings.Join(toolNames, ", ") + "]"

	t := skillTurn{
		sessionKey: sessionKey,
		userMsg:    clipRunes(userMsg, 2500),
		asstMsg:    clipRunes(asst, 2500),
		toolNames:  dedupeStrings(toolNames),
	}
	slog.Info("skill evolution mid-run reflect", "session", sessionKey, "toolIter", completedToolIter, "tools", len(toolNames))
	ctx2, cancel := context.WithTimeout(context.Background(), 28*time.Second)
	defer cancel()
	notice, err := m.reflectAndApplyMode(ctx2, t, true)
	if err != nil {
		slog.Debug("skill mid-run reflect failed", "error", err)
		return nil
	}
	return notice
}

func (m *SkillMaintainer) isRepeatedDecision(fp string) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	last := m.lastDecision[fp]
	if last.IsZero() {
		return false
	}
	return time.Since(last) < time.Duration(m.cfg.RepeatSuppressionMinutes)*time.Minute
}

func (m *SkillMaintainer) markSuccess(sessionKey, fp string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.lastDecision[fp] = time.Now()
	m.failures[sessionKey] = 0
	m.lowQuality[sessionKey] = 0
	m.failureUntil[sessionKey] = time.Time{}
}

func (m *SkillMaintainer) markFailure(sessionKey string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.failures[sessionKey]++
	if m.failures[sessionKey] >= 3 {
		m.failureUntil[sessionKey] = time.Now().Add(time.Duration(m.cfg.FailureBackoffMinutes) * time.Minute)
		m.failures[sessionKey] = 0
	}
}

func (m *SkillMaintainer) markLowQuality(sessionKey string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.lowQuality[sessionKey]++
	if m.lowQuality[sessionKey] >= 3 {
		m.failureUntil[sessionKey] = time.Now().Add(time.Duration(m.cfg.FailureBackoffMinutes*2) * time.Minute)
		m.lowQuality[sessionKey] = 0
	}
}

func (m *SkillMaintainer) currentQualityFloor() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.adaptThresholdsLocked()
	return m.qualityFloor
}

func (m *SkillMaintainer) currentConfidenceFloor() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.adaptThresholdsLocked()
	return m.confFloor
}

func (m *SkillMaintainer) record(sessionKey, outcome, reason string, quality, confidence int, skillName string) {
	m.mu.Lock()
	switch outcome {
	case "success":
		m.goodEvents++
	case "drop", "rollback":
		m.badEvents++
	}
	m.adaptThresholdsLocked()
	qf := m.qualityFloor
	cf := m.confFloor
	m.mu.Unlock()
	if m.telemetry == nil {
		return
	}
	meta := map[string]any{}
	if skillName != "" {
		meta["skill"] = skillName
	}
	meta["qualityFloor"] = qf
	meta["confidenceFloor"] = cf
	m.telemetry.Record(learningEvent{
		Category:   "skill",
		SessionKey: sessionKey,
		Outcome:    outcome,
		Reason:     reason,
		Quality:    quality,
		Confidence: confidence,
		Meta:       meta,
	})
}

func (m *SkillMaintainer) adaptThresholdsLocked() {
	if m.controller == nil {
		return
	}
	if m.goodEvents+m.badEvents < m.cfg.ControllerBatchSize {
		return
	}
	m.controller.Observe(m.goodEvents, m.badEvents)
	m.qualityFloor, m.confFloor = m.controller.Current()
	m.goodEvents = 0
	m.badEvents = 0
}

func (m *SkillMaintainer) listExistingSkills() []string {
	dir := filepath.Join(m.tool.Workspace, "skills")
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}
	out := make([]string, 0, len(entries))
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		if _, err := os.Stat(filepath.Join(dir, e.Name(), "SKILL.md")); err == nil {
			out = append(out, e.Name())
		}
	}
	sort.Strings(out)
	if len(out) > 24 {
		out = out[:24]
	}
	return out
}

func (m *SkillMaintainer) isNearDuplicateSkill(name, content string) bool {
	dir := filepath.Join(m.tool.Workspace, "skills")
	entries, err := os.ReadDir(dir)
	if err != nil {
		return false
	}
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		existingName := e.Name()
		if tokenJaccard(existingName, name) >= 0.9 || strings.HasPrefix(name, existingName) || strings.HasPrefix(existingName, name) {
			return true
		}
		p := filepath.Join(dir, existingName, "SKILL.md")
		b, err := os.ReadFile(p)
		if err != nil || len(b) == 0 {
			continue
		}
		if tokenJaccard(string(b), content) >= 0.9 {
			return true
		}
	}
	return false
}

func parseSkillReview(raw string) (noop bool, confidence int, action, name, content, oldText, newText string) {
	confidence = 100
	for _, ln := range strings.Split(raw, "\n") {
		t := strings.TrimSpace(ln)
		u := strings.ToUpper(t)
		switch {
		case strings.HasPrefix(u, "NOOP:"):
			v := strings.TrimSpace(t[len("NOOP:"):])
			noop = strings.EqualFold(v, "yes") || strings.EqualFold(v, "true")
		case strings.HasPrefix(u, "CONFIDENCE:"):
			v := strings.TrimSpace(t[len("CONFIDENCE:"):])
			confidence = atoiClamp(v, 0, 100, 100)
		case strings.HasPrefix(u, "ACTION:"):
			action = strings.TrimSpace(t[len("ACTION:"):])
		case strings.HasPrefix(u, "NAME:"):
			name = strings.TrimSpace(t[len("NAME:"):])
		case strings.HasPrefix(u, "CONTENT:"):
			content = strings.TrimSpace(t[len("CONTENT:"):])
		case strings.HasPrefix(u, "OLD_TEXT:"):
			oldText = strings.TrimSpace(t[len("OLD_TEXT:"):])
		case strings.HasPrefix(u, "NEW_TEXT:"):
			newText = strings.TrimSpace(t[len("NEW_TEXT:"):])
		}
	}
	return
}

var skillNameAllowed = regexp.MustCompile(`^[a-z0-9][a-z0-9._-]*$`)

func normalizeSkillName(v string) string {
	v = strings.ToLower(strings.TrimSpace(v))
	v = strings.ReplaceAll(v, " ", "-")
	if !skillNameAllowed.MatchString(v) {
		return ""
	}
	return v
}

func normalizeSkillAction(v string) string {
	v = strings.ToLower(strings.TrimSpace(v))
	switch v {
	case "create", "add":
		return "create"
	case "patch", "update":
		return "patch"
	default:
		return ""
	}
}

func dedupeStrings(in []string) []string {
	if len(in) == 0 {
		return nil
	}
	seen := make(map[string]struct{}, len(in))
	out := make([]string, 0, len(in))
	for _, s := range in {
		s = strings.TrimSpace(s)
		if s == "" {
			continue
		}
		if _, ok := seen[s]; ok {
			continue
		}
		seen[s] = struct{}{}
		out = append(out, s)
	}
	return out
}

func scoreSkillCandidate(action, name, content string, toolNames []string) int {
	score := 0
	if action == "create" || action == "patch" {
		score += 20
	}
	if len(name) >= 4 {
		score += 10
	}
	if len(dedupeStrings(toolNames)) >= 2 {
		score += 20
	}
	c := strings.TrimSpace(content)
	if strings.HasPrefix(c, "---") && strings.Contains(c, "description:") {
		score += 35
	}
	if len([]rune(c)) >= 140 {
		score += 15
	}
	return score
}

func skillToolSuccess(result string) bool {
	var payload map[string]any
	if err := json.Unmarshal([]byte(result), &payload); err != nil {
		return false
	}
	v, _ := payload["success"].(bool)
	return v
}

func validateSkillFile(path string) bool {
	b, err := os.ReadFile(path)
	if err != nil {
		return false
	}
	s := strings.TrimSpace(string(b))
	return strings.HasPrefix(s, "---") && strings.Contains(s, "description:")
}

func rollbackSkillFile(path string, prev []byte) error {
	if len(prev) == 0 {
		_ = os.Remove(path)
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
		return err
	}
	return os.WriteFile(path, prev, 0o600)
}

func skillDecisionKey(sessionKey, action, name, content, oldText, newText string) string {
	return strings.TrimSpace(strings.ToLower(sessionKey + "|" + action + "|" + name + "|" + content + "|" + oldText + "|" + newText))
}
