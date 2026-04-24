package agent

import (
	"os"
	"path/filepath"
	"strings"
)

// MemoryStore provides long-term memory (MEMORY.md) and history log (HISTORY.md).
type MemoryStore struct {
	memoryDir   string
	memoryPath  string
	historyPath string
}

// NewMemoryStore creates a memory store under workspace/memory.
func NewMemoryStore(workspace string) (*MemoryStore, error) {
	memoryDir := filepath.Join(workspace, "memory")
	if err := os.MkdirAll(memoryDir, 0o750); err != nil {
		return nil, err
	}
	return &MemoryStore{
		memoryDir:   memoryDir,
		memoryPath:  filepath.Join(memoryDir, "MEMORY.md"),
		historyPath: filepath.Join(memoryDir, "HISTORY.md"),
	}, nil
}

// ReadLongTerm returns the contents of MEMORY.md.
func (m *MemoryStore) ReadLongTerm() (string, error) {
	data, err := os.ReadFile(m.memoryPath)
	if err != nil {
		if os.IsNotExist(err) {
			return "", nil
		}
		return "", err
	}
	return string(data), nil
}

// WriteLongTerm writes MEMORY.md.
func (m *MemoryStore) WriteLongTerm(content string) error {
	return os.WriteFile(m.memoryPath, []byte(content), 0o600)
}

// AppendHistory appends a line to HISTORY.md.
func (m *MemoryStore) AppendHistory(entry string) error {
	f, err := os.OpenFile(m.historyPath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	defer f.Close()
	if entry != "" && entry[len(entry)-1] != '\n' {
		entry += "\n"
	}
	_, err = f.WriteString(entry + "\n")
	return err
}

// GetMemoryContext returns a string suitable for inclusion in the system prompt.
// Includes MEMORY.md (MemoryConsolidator), USER.md and NOTE.md (memory tool under memory/).
func (m *MemoryStore) GetMemoryContext() (string, error) {
	readTrim := func(p string) string {
		b, err := os.ReadFile(p)
		if err != nil || len(b) == 0 {
			return ""
		}
		return strings.TrimSpace(string(b))
	}
	var parts []string
	if s := readTrim(m.memoryPath); s != "" {
		parts = append(parts, "## Long-term Memory\n\n"+s)
	}
	userPath := filepath.Join(m.memoryDir, "USER.md")
	if s := readTrim(userPath); s != "" {
		parts = append(parts, "## User profile (memory tool)\n\n"+s)
	}
	curatedPath := filepath.Join(m.memoryDir, "NOTE.md")
	if s := readTrim(curatedPath); s != "" {
		parts = append(parts, "## Curated agent notes (memory tool)\n\n"+s)
	}
	if len(parts) == 0 {
		return "", nil
	}
	return strings.Join(parts, "\n\n---\n\n"), nil
}
