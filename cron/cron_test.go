package cron_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/jacklicn/dipper-bot/cron"
)

func TestScheduleKinds(t *testing.T) {
	if cron.ScheduleAt != "at" || cron.ScheduleEvery != "every" || cron.ScheduleCron != "cron" {
		t.Errorf("schedule kind constants changed")
	}
}

func TestCronService_AddListRemove(t *testing.T) {
	dir := t.TempDir()
	storePath := filepath.Join(dir, "jobs.json")
	svc := cron.NewService(storePath, nil)

	ctx := context.Background()
	if err := svc.Start(ctx); err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer svc.Stop()

	sch := cron.Schedule{Kind: cron.ScheduleEvery, EveryMs: 5000}
	job, err := svc.AddJob("test-job", sch, "hello", false, "", "", false)
	if err != nil {
		t.Fatalf("AddJob: %v", err)
	}
	if job.ID == "" || job.Name != "test-job" {
		t.Errorf("AddJob returned %+v", job)
	}

	jobs, err := svc.ListJobs(true)
	if err != nil {
		t.Fatalf("ListJobs: %v", err)
	}
	if len(jobs) != 1 {
		t.Fatalf("ListJobs len = %d, want 1", len(jobs))
	}
	if jobs[0].Name != "test-job" {
		t.Errorf("ListJobs[0].Name = %q", jobs[0].Name)
	}

	ok, err := svc.RemoveJob(job.ID)
	if err != nil {
		t.Fatalf("RemoveJob: %v", err)
	}
	if !ok {
		t.Error("RemoveJob returned false")
	}
	jobs2, _ := svc.ListJobs(true)
	if len(jobs2) != 0 {
		t.Errorf("after Remove, ListJobs len = %d", len(jobs2))
	}
}

func TestCronService_RunJob(t *testing.T) {
	dir := t.TempDir()
	storePath := filepath.Join(dir, "jobs.json")
	var runCount int
	svc := cron.NewService(storePath, func(ctx context.Context, job *cron.Job) (string, error) {
		runCount++
		return "ok", nil
	})

	ctx := context.Background()
	if err := svc.Start(ctx); err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer svc.Stop()

	sch := cron.Schedule{Kind: cron.ScheduleEvery, EveryMs: 3600000}
	job, err := svc.AddJob("run-me", sch, "msg", false, "", "", false)
	if err != nil {
		t.Fatalf("AddJob: %v", err)
	}

	ok, err := svc.RunJob(ctx, job.ID, true)
	if err != nil {
		t.Fatalf("RunJob: %v", err)
	}
	if !ok {
		t.Error("RunJob returned false")
	}
	time.Sleep(50 * time.Millisecond)
	if runCount != 1 {
		t.Errorf("OnJob called %d times, want 1", runCount)
	}
}

func TestCronService_LoadSave(t *testing.T) {
	dir := t.TempDir()
	storePath := filepath.Join(dir, "jobs.json")
	svc := cron.NewService(storePath, nil)
	ctx := context.Background()
	if err := svc.Start(ctx); err != nil {
		t.Fatalf("Start: %v", err)
	}
	svc.Stop()

	_, err := svc.AddJob("persist", cron.Schedule{Kind: cron.ScheduleEvery, EveryMs: 1000}, "m", false, "", "", false)
	if err != nil {
		t.Fatalf("AddJob: %v", err)
	}

	if _, err := os.Stat(storePath); os.IsNotExist(err) {
		t.Error("jobs.json was not created")
	}
}
