package heartbeat

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// Runner is called to execute a heartbeat tick (e.g. run agent with HEARTBEAT.md prompt).
type Runner func(ctx context.Context, prompt string) (string, error)

// Service runs periodic heartbeat checks.
type Service struct {
	Workspace string
	OnTick    Runner
	Interval  time.Duration
	Enabled   bool
	mu        sync.Mutex
	stop      chan struct{}
}

// DefaultInterval is 30 minutes.
const DefaultInterval = 30 * time.Minute

// NewService creates a heartbeat service.
func NewService(workspace string, onTick Runner, interval time.Duration, enabled bool) *Service {
	if interval == 0 {
		interval = DefaultInterval
	}
	return &Service{Workspace: workspace, OnTick: onTick, Interval: interval, Enabled: enabled, stop: make(chan struct{})}
}

// Start starts the heartbeat loop.
func (s *Service) Start(ctx context.Context) {
	if !s.Enabled || s.OnTick == nil {
		return
	}
	go func() {
		ticker := time.NewTicker(s.Interval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-s.stop:
				return
			case <-ticker.C:
				s.tick(ctx)
			}
		}
	}()
}

// Stop stops the service.
func (s *Service) Stop() {
	s.mu.Lock()
	defer s.mu.Unlock()
	select {
	case <-s.stop:
	default:
		close(s.stop)
	}
}

const heartbeatOK = "HEARTBEAT_OK"

func (s *Service) tick(ctx context.Context) {
	path := filepath.Join(s.Workspace, "HEARTBEAT.md")
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return
		}
		return
	}
	content := strings.TrimSpace(string(data))
	if isEmptyHeartbeat(content) {
		return
	}
	prompt := "Read HEARTBEAT.md in your workspace (if it exists). Follow any instructions there. If nothing needs attention, reply with just: HEARTBEAT_OK"
	_, _ = s.OnTick(ctx, prompt)
}

// Skip patterns for empty heartbeat.
var heartbeatSkipPatterns = map[string]bool{
	"- [ ]": true, "* [ ]": true, "- [x]": true, "* [x]": true,
}

func isEmptyHeartbeat(content string) bool {
	for _, line := range strings.Split(content, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") || strings.HasPrefix(line, "<!--") {
			continue
		}
		if heartbeatSkipPatterns[line] {
			continue
		}
		return false
	}
	return true
}
