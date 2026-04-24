package cron

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/robfig/cron/v3"
)

// JobRunner is called to execute a job. Returns response text.
type JobRunner func(ctx context.Context, job *Job) (string, error)

// Service manages scheduled jobs. Set OnJob after creation to run jobs through the agent.
type Service struct {
	storePath string
	OnJob     JobRunner
	store     *Store
	mu        sync.Mutex
	stop      chan struct{}
	timer     *time.Timer
}

// NewService creates a cron service.
func NewService(storePath string, onJob JobRunner) *Service {
	return &Service{storePath: storePath, OnJob: onJob, stop: make(chan struct{})}
}

func (s *Service) load() (*Store, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.store != nil {
		return s.store, nil
	}
	s.store = &Store{Version: 1}
	data, err := os.ReadFile(s.storePath)
	if err != nil {
		if os.IsNotExist(err) {
			return s.store, nil
		}
		return nil, err
	}
	_ = json.Unmarshal(data, s.store)
	if s.store.Jobs == nil {
		s.store.Jobs = []Job{}
	}
	return s.store, nil
}

func (s *Service) save() error {
	s.mu.Lock()
	store := s.store
	s.mu.Unlock()
	if store == nil {
		return nil
	}
	dir := filepath.Dir(s.storePath)
	if err := os.MkdirAll(dir, 0o750); err != nil {
		return err
	}
	data, err := json.MarshalIndent(store, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(s.storePath, data, 0o600)
}

// Start loads store and starts the scheduler loop.
func (s *Service) Start(ctx context.Context) error {
	if _, err := s.load(); err != nil {
		return err
	}
	s.recomputeNextRuns()
	_ = s.save()
	go s.loop(ctx)
	return nil
}

// Stop stops the scheduler.
func (s *Service) Stop() {
	close(s.stop)
	if s.timer != nil {
		s.timer.Stop()
	}
}

func (s *Service) loop(ctx context.Context) {
	for {
		next := s.nextWakeMs()
		if next == 0 {
			select {
			case <-ctx.Done():
				return
			case <-time.After(10 * time.Second):
				continue
			}
		}
		delay := time.Duration(next-time.Now().UnixMilli()) * time.Millisecond
		if delay < 0 {
			delay = 0
		}
		s.timer = time.NewTimer(delay)
		select {
		case <-ctx.Done():
			s.timer.Stop()
			return
		case <-s.stop:
			s.timer.Stop()
			return
		case <-s.timer.C:
			s.tick(ctx)
		}
	}
}

func (s *Service) nextWakeMs() int64 {
	s.mu.Lock()
	defer s.mu.Unlock()
	var min int64
	for _, j := range s.store.Jobs {
		if !j.Enabled || j.State.NextRunAtMs <= 0 {
			continue
		}
		if min == 0 || j.State.NextRunAtMs < min {
			min = j.State.NextRunAtMs
		}
	}
	return min
}

func (s *Service) recomputeNextRuns() {
	s.mu.Lock()
	defer s.mu.Unlock()
	now := time.Now().UnixMilli()
	for i := range s.store.Jobs {
		j := &s.store.Jobs[i]
		if j.Enabled {
			j.State.NextRunAtMs = computeNextRun(&j.Schedule, now)
		}
	}
}

// ComputeNextRun returns the next run time in milliseconds (exported for cron list display).
func ComputeNextRun(sch *Schedule, nowMs int64) int64 {
	return computeNextRun(sch, nowMs)
}

func computeNextRun(sch *Schedule, nowMs int64) int64 {
	switch sch.Kind {
	case ScheduleAt:
		if sch.AtMs > nowMs {
			return sch.AtMs
		}
		return 0
	case ScheduleEvery:
		if sch.EveryMs <= 0 {
			return 0
		}
		return nowMs + sch.EveryMs
	case ScheduleCron:
		if sch.Expr == "" {
			return 0
		}
		parser := cron.NewParser(cron.Minute | cron.Hour | cron.Dom | cron.Month | cron.Dow)
		sched, err := parser.Parse(sch.Expr)
		if err != nil {
			return 0
		}
		from := time.UnixMilli(nowMs)
		if sch.Tz != "" {
			if loc, err := time.LoadLocation(sch.Tz); err == nil {
				from = time.UnixMilli(nowMs).In(loc)
			}
		}
		next := sched.Next(from)
		return next.UnixMilli()
	}
	return 0
}

func (s *Service) tick(ctx context.Context) {
	s.mu.Lock()
	now := time.Now().UnixMilli()
	var due []Job
	for _, j := range s.store.Jobs {
		if j.Enabled && j.State.NextRunAtMs > 0 && now >= j.State.NextRunAtMs {
			due = append(due, j)
		}
	}
	s.mu.Unlock()

	for _, j := range due {
		s.runJob(ctx, &j)
	}
	s.recomputeNextRuns()
	_ = s.save()
}

func (s *Service) runJob(ctx context.Context, job *Job) {
	if s.OnJob != nil {
		_, err := s.OnJob(ctx, job)
		if err != nil {
			job.State.LastStatus = "error"
			job.State.LastError = err.Error()
		} else {
			job.State.LastStatus = "ok"
			job.State.LastError = ""
		}
	}
	job.State.LastRunAtMs = time.Now().UnixMilli()
	job.UpdatedAtMs = job.State.LastRunAtMs
	if job.Schedule.Kind == ScheduleAt {
		if job.DeleteAfterRun {
			s.mu.Lock()
			for i := range s.store.Jobs {
				if s.store.Jobs[i].ID == job.ID {
					s.store.Jobs = append(s.store.Jobs[:i], s.store.Jobs[i+1:]...)
					break
				}
			}
			s.mu.Unlock()
			_ = s.save()
			return
		}
		job.Enabled = false
		job.State.NextRunAtMs = 0
	} else if job.Schedule.Kind == ScheduleEvery {
		job.State.NextRunAtMs = time.Now().UnixMilli() + job.Schedule.EveryMs
	} else if job.Schedule.Kind == ScheduleCron {
		job.State.NextRunAtMs = computeNextRun(&job.Schedule, time.Now().UnixMilli())
	}

	s.mu.Lock()
	for i := range s.store.Jobs {
		if s.store.Jobs[i].ID == job.ID {
			s.store.Jobs[i] = *job
			break
		}
	}
	s.mu.Unlock()
}

// ListJobs returns all jobs (optionally including disabled).
func (s *Service) ListJobs(includeDisabled bool) ([]Job, error) {
	store, err := s.load()
	if err != nil {
		return nil, err
	}
	var out []Job
	for _, j := range store.Jobs {
		if includeDisabled || j.Enabled {
			out = append(out, j)
		}
	}
	return out, nil
}

// AddJob adds a new job.
func (s *Service) AddJob(name string, schedule Schedule, message string, deliver bool, channel, to string, deleteAfterRun bool) (*Job, error) {
	store, err := s.load()
	if err != nil {
		return nil, err
	}
	now := time.Now().UnixMilli()
	job := Job{
		ID:             shortID(),
		Name:           name,
		Enabled:        true,
		Schedule:       schedule,
		Payload:        Payload{Kind: "agent_turn", Message: message, Deliver: deliver, Channel: channel, To: to},
		CreatedAtMs:    now,
		UpdatedAtMs:    now,
		DeleteAfterRun: deleteAfterRun,
	}
	job.State.NextRunAtMs = computeNextRun(&job.Schedule, now)
	store.Jobs = append(store.Jobs, job)
	return &job, s.save()
}

// RemoveJob removes a job by ID.
func (s *Service) RemoveJob(jobID string) (bool, error) {
	store, err := s.load()
	if err != nil {
		return false, err
	}
	for i, j := range store.Jobs {
		if j.ID == jobID {
			store.Jobs = append(store.Jobs[:i], store.Jobs[i+1:]...)
			return true, s.save()
		}
	}
	return false, nil
}

// EnableJob enables or disables a job.
func (s *Service) EnableJob(jobID string, enabled bool) (*Job, error) {
	store, err := s.load()
	if err != nil {
		return nil, err
	}
	for i := range store.Jobs {
		if store.Jobs[i].ID == jobID {
			store.Jobs[i].Enabled = enabled
			store.Jobs[i].UpdatedAtMs = time.Now().UnixMilli()
			if enabled {
				store.Jobs[i].State.NextRunAtMs = computeNextRun(&store.Jobs[i].Schedule, time.Now().UnixMilli())
			} else {
				store.Jobs[i].State.NextRunAtMs = 0
			}
			return &store.Jobs[i], s.save()
		}
	}
	return nil, nil
}

// RunJob runs a job once.
func (s *Service) RunJob(ctx context.Context, jobID string, force bool) (bool, error) {
	store, err := s.load()
	if err != nil {
		return false, err
	}
	for i := range store.Jobs {
		if store.Jobs[i].ID == jobID {
			if !force && !store.Jobs[i].Enabled {
				return false, nil
			}
			s.runJob(ctx, &store.Jobs[i])
			_ = s.save()
			return true, nil
		}
	}
	return false, nil
}

// Status returns job count etc.
func (s *Service) Status() (int, error) {
	store, err := s.load()
	if err != nil {
		return 0, err
	}
	return len(store.Jobs), nil
}

func shortID() string {
	b := make([]byte, 4)
	rand.Read(b)
	return hex.EncodeToString(b)[:8]
}
