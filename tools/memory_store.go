package tools

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
)

// Memory notes: USER.md (user profile) and NOTE.md (agent notes), §-delimited entries.
const memoryEntryDelimiter = "\n§\n"

const (
	memoryCharLimit = 2200
	userCharLimit   = 1375
)

var memoryThreatPatterns = []*regexp.Regexp{
	regexp.MustCompile(`(?i)ignore\s+(previous|all|above|prior)\s+instructions`),
	regexp.MustCompile(`(?i)you\s+are\s+now\s+`),
	regexp.MustCompile(`(?i)system\s+prompt\s+override`),
	regexp.MustCompile(`(?i)curl\s+[^\n]*\$\{?\w*(KEY|TOKEN|SECRET|PASSWORD|CREDENTIAL|API)`),
}

func scanMemoryContentSafety(content string) error {
	for _, re := range memoryThreatPatterns {
		if re.MatchString(content) {
			return fmt.Errorf("content blocked by safety scan")
		}
	}
	return nil
}

// MemoryNoteStore implements add/replace/remove for USER.md and NOTE.md under workspace/memory.
type MemoryNoteStore struct {
	Workspace string
	mu        sync.Mutex
}

func (c *MemoryNoteStore) pathFor(target string) (string, int, error) {
	memDir := filepath.Join(c.Workspace, "memory")
	switch target {
	case "user":
		return filepath.Join(memDir, "USER.md"), userCharLimit, nil
	case "memory":
		return filepath.Join(memDir, "NOTE.md"), memoryCharLimit, nil
	default:
		return "", 0, fmt.Errorf("invalid target (use memory or user)")
	}
}

func readEntries(path string) []string {
	b, err := os.ReadFile(path)
	if err != nil || len(strings.TrimSpace(string(b))) == 0 {
		return nil
	}
	raw := strings.Split(string(b), memoryEntryDelimiter)
	var out []string
	for _, e := range raw {
		e = strings.TrimSpace(e)
		if e != "" {
			out = append(out, e)
		}
	}
	return out
}

var memoryWhitespaceRe = regexp.MustCompile(`\s+`)

func normalizeEntryForCompare(s string) string {
	s = strings.TrimSpace(strings.ToLower(s))
	if s == "" {
		return ""
	}
	return memoryWhitespaceRe.ReplaceAllString(s, " ")
}

func writeEntries(path string, entries []string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
		return err
	}
	content := strings.Join(entries, memoryEntryDelimiter)
	if content != "" {
		content += "\n"
	}
	return os.WriteFile(path, []byte(content), 0o600)
}

func (c *MemoryNoteStore) charCount(entries []string) int {
	if len(entries) == 0 {
		return 0
	}
	return len(strings.Join(entries, memoryEntryDelimiter))
}

func (c *MemoryNoteStore) successJSON(target string, message string, entries []string) string {
	cur := c.charCount(entries)
	limit := memoryCharLimit
	if target == "user" {
		limit = userCharLimit
	}
	pct := 0
	if limit > 0 {
		pct = min(100, (cur*100)/limit)
	}
	resp := map[string]any{
		"success":     true,
		"target":      target,
		"entries":     entries,
		"usage":       fmt.Sprintf("%d%% - %d/%d chars", pct, cur, limit),
		"entry_count": len(entries),
		"message":     message,
	}
	b, _ := json.Marshal(resp)
	return string(b)
}

func (c *MemoryNoteStore) errJSON(msg string) string {
	b, _ := json.Marshal(map[string]any{"success": false, "error": msg})
	return string(b)
}

// ExecuteMemoryTool dispatches memory tool actions (JSON responses).
func (c *MemoryNoteStore) ExecuteMemoryTool(action, target, content, oldText string) string {
	if c == nil || c.Workspace == "" {
		return c.errJSON("memory tool not configured")
	}
	c.mu.Lock()
	defer c.mu.Unlock()

	path, limit, err := c.pathFor(target)
	if err != nil {
		return c.errJSON(err.Error())
	}

	switch action {
	case "add":
		content = strings.TrimSpace(content)
		if content == "" {
			return c.errJSON("content is required for add")
		}
		if err := scanMemoryContentSafety(content); err != nil {
			return c.errJSON(err.Error())
		}
		entries := readEntries(path)
		normContent := normalizeEntryForCompare(content)
		for _, e := range entries {
			if normalizeEntryForCompare(e) == normContent {
				return c.successJSON(target, "Entry already exists (no duplicate added).", entries)
			}
		}
		next := append(entries, content)
		if c.charCount(next) > limit {
			return c.errJSON(fmt.Sprintf("Would exceed %d char budget; replace or remove entries first", limit))
		}
		if err := writeEntries(path, next); err != nil {
			return c.errJSON(err.Error())
		}
		return c.successJSON(target, "Entry added.", next)

	case "replace":
		oldText = strings.TrimSpace(oldText)
		content = strings.TrimSpace(content)
		if oldText == "" || content == "" {
			return c.errJSON("old_text and content are required for replace")
		}
		if err := scanMemoryContentSafety(content); err != nil {
			return c.errJSON(err.Error())
		}
		entries := readEntries(path)
		var matches []int
		normOld := normalizeEntryForCompare(oldText)
		for i, e := range entries {
			if strings.Contains(normalizeEntryForCompare(e), normOld) {
				matches = append(matches, i)
			}
		}
		if len(matches) == 0 {
			return c.errJSON("no entry matched old_text")
		}
		if len(matches) > 1 {
			uniq := map[string]struct{}{}
			for _, i := range matches {
				uniq[entries[i]] = struct{}{}
			}
			if len(uniq) > 1 {
				return c.errJSON("multiple entries matched; be more specific")
			}
		}
		idx := matches[0]
		test := append([]string(nil), entries...)
		test[idx] = content
		if c.charCount(test) > limit {
			return c.errJSON("replacement would exceed char budget")
		}
		entries[idx] = content
		for i, e := range entries {
			if i != idx && normalizeEntryForCompare(e) == normalizeEntryForCompare(content) {
				return c.successJSON(target, "Replacement already covered by existing entry (no duplicate written).", entries)
			}
		}
		if err := writeEntries(path, entries); err != nil {
			return c.errJSON(err.Error())
		}
		return c.successJSON(target, "Entry replaced.", entries)

	case "remove":
		oldText = strings.TrimSpace(oldText)
		if oldText == "" {
			return c.errJSON("old_text is required for remove")
		}
		entries := readEntries(path)
		var matches []int
		normOld := normalizeEntryForCompare(oldText)
		for i, e := range entries {
			if strings.Contains(normalizeEntryForCompare(e), normOld) {
				matches = append(matches, i)
			}
		}
		if len(matches) == 0 {
			return c.errJSON("no entry matched old_text")
		}
		if len(matches) > 1 {
			uniq := map[string]struct{}{}
			for _, i := range matches {
				uniq[entries[i]] = struct{}{}
			}
			if len(uniq) > 1 {
				return c.errJSON("multiple entries matched; be more specific")
			}
		}
		idx := matches[0]
		entries = append(entries[:idx], entries[idx+1:]...)
		if err := writeEntries(path, entries); err != nil {
			return c.errJSON(err.Error())
		}
		return c.successJSON(target, "Entry removed.", entries)

	default:
		return c.errJSON("unknown action (use add, replace, remove)")
	}
}
