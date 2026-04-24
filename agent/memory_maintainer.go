package agent

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/jacklicn/dipper-bot/config"
	"github.com/jacklicn/dipper-bot/providers"
	"github.com/jacklicn/dipper-bot/session"
	"github.com/jacklicn/dipper-bot/tools"
)

type memoryTurn struct {
	sessionKey string
	userMsg    string
	asstMsg    string
}

// MemoryMaintainer runs a constrained background review workflow and updates memory files.
// It only writes through the memory tool store (USER.md / NOTE.md).
type MemoryMaintainer struct {
	provider     providers.LLMProvider
	model        string
	store        *tools.MemoryNoteStore
	queue        chan memoryTurn
	cfg          config.MemoryMaintenanceConfig
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
	// onMemoryApplied is optional (e.g. queue learner feedback for next reply).
	onMemoryApplied func(sessionKey, target, action string)
}

func NewMemoryMaintainer(provider providers.LLMProvider, model string, store *tools.MemoryNoteStore, cfg config.MemoryMaintenanceConfig, telemetry *LearningTelemetry) *MemoryMaintainer {
	if provider == nil || store == nil || model == "" || !cfg.MaintenanceEnabled() {
		return nil
	}
	if cfg.QueueSize <= 0 {
		cfg.QueueSize = 64
	}
	if cfg.MinUserChars <= 0 {
		cfg.MinUserChars = 40
	}
	if cfg.MinAssistantChars <= 0 {
		cfg.MinAssistantChars = 40
	}
	if cfg.NudgeInterval <= 0 {
		cfg.NudgeInterval = 1
	}
	if cfg.MinIntervalMinutes <= 0 {
		cfg.MinIntervalMinutes = 5
	}
	if cfg.MinQualityScore <= 0 {
		cfg.MinQualityScore = 50
	}
	if cfg.RepeatSuppressionMinutes <= 0 {
		cfg.RepeatSuppressionMinutes = 30
	}
	if cfg.FailureBackoffMinutes <= 0 {
		cfg.FailureBackoffMinutes = 15
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
	statePath := filepath.Join(store.Workspace, "memory", "adaptive_controller_state_memory.json")
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
	m := &MemoryMaintainer{
		provider:     provider,
		model:        model,
		store:        store,
		queue:        make(chan memoryTurn, cfg.QueueSize),
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
	}
	go m.worker()
	return m
}

func (m *MemoryMaintainer) Enqueue(sessionKey, userMsg, asstMsg string) {
	if m == nil {
		return
	}
	if len([]rune(strings.TrimSpace(userMsg))) < m.cfg.MinUserChars ||
		len([]rune(strings.TrimSpace(asstMsg))) < m.cfg.MinAssistantChars {
		return
	}
	if !m.shouldRunForSession(sessionKey) {
		return
	}
	t := memoryTurn{
		sessionKey: sessionKey,
		userMsg:    clipRunes(userMsg, 4000),
		asstMsg:    clipRunes(asstMsg, 4000),
	}
	select {
	case m.queue <- t:
	default:
		// Backpressure safety: drop oldest style behavior by ignoring when queue is full.
	}
}

func (m *MemoryMaintainer) shouldRunForSession(sessionKey string) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.turns[sessionKey]++
	if until := m.failureUntil[sessionKey]; !until.IsZero() && time.Now().Before(until) {
		return false
	}
	if m.turns[sessionKey]%m.cfg.NudgeInterval != 0 {
		return false
	}
	last := m.lastRun[sessionKey]
	if !last.IsZero() && time.Since(last) < time.Duration(m.cfg.MinIntervalMinutes)*time.Minute {
		return false
	}
	m.lastRun[sessionKey] = time.Now()
	return true
}

func (m *MemoryMaintainer) worker() {
	for t := range m.queue {
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		_ = m.reviewAndApply(ctx, t)
		cancel()
	}
}

// FlushFromSession forces one maintenance pass using the latest user/assistant pair.
func (m *MemoryMaintainer) FlushFromSession(sessionKey string, sess *session.Session) {
	if m == nil || sess == nil {
		return
	}
	if m.cfg.FlushMinTurns > 0 && countUserTurns(sess) < m.cfg.FlushMinTurns {
		return
	}
	userMsg, asstMsg, ok := latestTurnPair(sess)
	if !ok {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	_ = m.reviewAndApply(ctx, memoryTurn{
		sessionKey: sessionKey,
		userMsg:    clipRunes(userMsg, 4000),
		asstMsg:    clipRunes(asstMsg, 4000),
	})
}

func (m *MemoryMaintainer) reviewAndApply(ctx context.Context, t memoryTurn) error {
	system := `You are a strict memory-maintenance reviewer.
You can propose exactly one operation for persistent memory.
Allowed targets: user, memory
Allowed actions: add, replace, remove
If no durable fact should be stored, output NOOP: yes.
Output ONLY lines:
NOOP: yes|no
CONFIDENCE: <0-100>
TARGET: user|memory
ACTION: add|replace|remove
CONTENT: <text for add/replace>
OLD_TEXT: <text for replace/remove>
`
	user := "Session: " + t.sessionKey + "\nUser:\n" + t.userMsg + "\n\nAssistant:\n" + t.asstMsg
	resp, err := m.provider.Chat(ctx, &providers.ChatRequest{
		Model:       m.model,
		Messages:    []providers.Message{{Role: "system", Content: system}, {Role: "user", Content: user}},
		MaxTokens:   220,
		Temperature: 0.0,
	})
	if err != nil {
		return err
	}
	noop, confidence, target, action, content, oldText := parseMemoryReview(resp.Content)
	target = normalizeTarget(target)
	action = normalizeAction(action)
	content = normalizeMemoryText(content)
	oldText = normalizeMemoryText(oldText)
	if noop || target == "" || action == "" {
		return nil
	}
	if confidence < m.currentConfidenceFloor() {
		m.markLowQuality(t.sessionKey)
		m.record(t.sessionKey, "drop", "low_confidence", 0, confidence, nil)
		return nil
	}
	if (action == "add" || action == "replace") && content == "" {
		return nil
	}
	if (action == "replace" || action == "remove") && oldText == "" {
		return nil
	}
	fp := sessionDecisionKey(t.sessionKey, action, target, content, oldText)
	if m.isRepeatedDecision(fp) {
		return nil
	}
	score := scoreMemoryCandidate(action, content, oldText)
	if score < m.currentQualityFloor() {
		m.markLowQuality(t.sessionKey)
		m.record(t.sessionKey, "drop", "low_quality", score, confidence, nil)
		return nil // denoise: low-quality memory candidates are dropped
	}
	if action != "remove" && m.isNearDuplicateMemory(target, content) {
		return nil // drift guard: suppress near-duplicate memory updates
	}
	backupPath := memoryTargetPath(m.store.Workspace, target)
	prev, _ := os.ReadFile(backupPath)
	result := m.store.ExecuteMemoryTool(action, target, content, oldText)
	if !memoryToolSuccess(result) || !validateMemoryWrite(action, content, backupPath) {
		_ = os.MkdirAll(filepath.Dir(backupPath), 0o750)
		_ = os.WriteFile(backupPath, prev, 0o600) // rollback on failed write/validation
		m.markFailure(t.sessionKey)
		m.record(t.sessionKey, "rollback", "write_or_validation_failed", score, confidence, map[string]any{"target": target, "action": action})
		return nil
	}
	m.markSuccess(t.sessionKey, fp)
	m.record(t.sessionKey, "success", "applied", score, confidence, map[string]any{"target": target, "action": action})
	if m.onMemoryApplied != nil {
		m.onMemoryApplied(t.sessionKey, target, action)
	}
	return nil
}

func (m *MemoryMaintainer) isRepeatedDecision(fp string) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	last := m.lastDecision[fp]
	if last.IsZero() {
		return false
	}
	return time.Since(last) < time.Duration(m.cfg.RepeatSuppressionMinutes)*time.Minute
}

func (m *MemoryMaintainer) markSuccess(sessionKey, fp string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.lastDecision[fp] = time.Now()
	m.failures[sessionKey] = 0
	m.lowQuality[sessionKey] = 0
	m.failureUntil[sessionKey] = time.Time{}
}

func (m *MemoryMaintainer) markFailure(sessionKey string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.failures[sessionKey]++
	if m.failures[sessionKey] >= 3 {
		m.failureUntil[sessionKey] = time.Now().Add(time.Duration(m.cfg.FailureBackoffMinutes) * time.Minute)
		m.failures[sessionKey] = 0
	}
}

func (m *MemoryMaintainer) markLowQuality(sessionKey string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.lowQuality[sessionKey]++
	if m.lowQuality[sessionKey] >= 3 {
		m.failureUntil[sessionKey] = time.Now().Add(time.Duration(m.cfg.FailureBackoffMinutes*2) * time.Minute)
		m.lowQuality[sessionKey] = 0
	}
}

func (m *MemoryMaintainer) currentQualityFloor() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.adaptThresholdsLocked()
	return m.qualityFloor
}

func (m *MemoryMaintainer) currentConfidenceFloor() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.adaptThresholdsLocked()
	return m.confFloor
}

func (m *MemoryMaintainer) record(sessionKey, outcome, reason string, quality, confidence int, meta map[string]any) {
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
	if meta == nil {
		meta = map[string]any{}
	}
	meta["qualityFloor"] = qf
	meta["confidenceFloor"] = cf
	m.telemetry.Record(learningEvent{
		Category:   "memory",
		SessionKey: sessionKey,
		Outcome:    outcome,
		Reason:     reason,
		Quality:    quality,
		Confidence: confidence,
		Meta:       meta,
	})
}

func (m *MemoryMaintainer) adaptThresholdsLocked() {
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

func latestTurnPair(sess *session.Session) (userMsg, asstMsg string, ok bool) {
	msgs := sess.GetMessagesFrom(0, 1<<20)
	for i := len(msgs) - 1; i >= 0; i-- {
		if msgs[i].Role != "assistant" {
			continue
		}
		asst := strings.TrimSpace(msgs[i].Content)
		if asst == "" {
			continue
		}
		for j := i - 1; j >= 0; j-- {
			if msgs[j].Role == "user" {
				user := strings.TrimSpace(msgs[j].Content)
				if user != "" {
					return user, asst, true
				}
				break
			}
		}
	}
	return "", "", false
}

func countUserTurns(sess *session.Session) int {
	msgs := sess.GetMessagesFrom(0, 1<<20)
	n := 0
	for _, m := range msgs {
		if m.Role == "user" && strings.TrimSpace(m.Content) != "" {
			n++
		}
	}
	return n
}

func parseMemoryReview(raw string) (noop bool, confidence int, target, action, content, oldText string) {
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
		case strings.HasPrefix(u, "TARGET:"):
			target = strings.ToLower(strings.TrimSpace(t[len("TARGET:"):]))
		case strings.HasPrefix(u, "ACTION:"):
			action = strings.ToLower(strings.TrimSpace(t[len("ACTION:"):]))
		case strings.HasPrefix(u, "CONTENT:"):
			content = strings.TrimSpace(t[len("CONTENT:"):])
		case strings.HasPrefix(u, "OLD_TEXT:"):
			oldText = strings.TrimSpace(t[len("OLD_TEXT:"):])
		}
	}
	return
}

func atoiClamp(raw string, lo, hi, def int) int {
	n := 0
	hasDigit := false
	if raw == "" {
		return def
	}
	for _, r := range raw {
		if r < '0' || r > '9' {
			continue
		}
		hasDigit = true
		n = n*10 + int(r-'0')
	}
	if !hasDigit {
		return def
	}
	if n < lo {
		return lo
	}
	if n > hi {
		return hi
	}
	return n
}

func clipRunes(s string, max int) string {
	if max <= 0 {
		return ""
	}
	r := []rune(s)
	if len(r) <= max {
		return s
	}
	return string(r[:max])
}

func normalizeTarget(v string) string {
	v = strings.ToLower(strings.TrimSpace(v))
	switch v {
	case "user", "profile":
		return "user"
	case "memory", "note", "notes":
		return "memory"
	default:
		return ""
	}
}

func normalizeAction(v string) string {
	v = strings.ToLower(strings.TrimSpace(v))
	switch v {
	case "add", "append", "create":
		return "add"
	case "replace", "update", "edit":
		return "replace"
	case "remove", "delete":
		return "remove"
	default:
		return ""
	}
}

var memorySpaceRe = regexp.MustCompile(`\s+`)

func normalizeMemoryText(v string) string {
	v = strings.TrimSpace(v)
	if v == "" {
		return ""
	}
	return memorySpaceRe.ReplaceAllString(v, " ")
}

func scoreMemoryCandidate(action, content, oldText string) int {
	score := 0
	if action == "add" || action == "replace" || action == "remove" {
		score += 20
	}
	if len([]rune(content)) >= 18 {
		score += 35
	}
	if len([]rune(oldText)) >= 8 {
		score += 20
	}
	lc := strings.ToLower(content)
	noise := []string{"ok", "thanks", "done", "noted", "great", "nice"}
	for _, n := range noise {
		if lc == n {
			return 0
		}
	}
	if strings.Contains(lc, "always") || strings.Contains(lc, "prefer") {
		score += 25
	}
	return score
}

func memoryTargetPath(workspace, target string) string {
	memDir := filepath.Join(workspace, "memory")
	if target == "user" {
		return filepath.Join(memDir, "USER.md")
	}
	return filepath.Join(memDir, "NOTE.md")
}

func memoryToolSuccess(result string) bool {
	var payload map[string]any
	if err := json.Unmarshal([]byte(result), &payload); err != nil {
		return false
	}
	v, _ := payload["success"].(bool)
	return v
}

func validateMemoryWrite(action, content, path string) bool {
	if action == "remove" {
		return true
	}
	b, err := os.ReadFile(path)
	if err != nil {
		return false
	}
	return strings.Contains(normalizeMemoryText(string(b)), normalizeMemoryText(content))
}

func sessionDecisionKey(sessionKey, action, target, content, oldText string) string {
	return normalizeMemoryText(sessionKey + "|" + action + "|" + target + "|" + content + "|" + oldText)
}

func (m *MemoryMaintainer) isNearDuplicateMemory(target, content string) bool {
	p := memoryTargetPath(m.store.Workspace, target)
	b, err := os.ReadFile(p)
	if err != nil || len(b) == 0 {
		return false
	}
	candidate := normalizeMemoryText(content)
	for _, part := range strings.Split(string(b), "\n§\n") {
		entry := normalizeMemoryText(part)
		if entry == "" {
			continue
		}
		if tokenJaccard(entry, candidate) >= 0.92 {
			return true
		}
	}
	return false
}

func tokenJaccard(a, b string) float64 {
	ta := make(map[string]struct{})
	tb := make(map[string]struct{})
	for _, t := range strings.Fields(strings.ToLower(a)) {
		ta[t] = struct{}{}
	}
	for _, t := range strings.Fields(strings.ToLower(b)) {
		tb[t] = struct{}{}
	}
	if len(ta) == 0 || len(tb) == 0 {
		return 0
	}
	inter := 0
	for t := range ta {
		if _, ok := tb[t]; ok {
			inter++
		}
	}
	union := len(ta)
	for t := range tb {
		if _, ok := ta[t]; !ok {
			union++
		}
	}
	if union == 0 {
		return 0
	}
	return float64(inter) / float64(union)
}

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}
