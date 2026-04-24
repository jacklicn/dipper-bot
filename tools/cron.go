package tools

import (
	"context"
	"fmt"
	"strconv"
	"time"

	"github.com/jacklicn/dipper-bot/cron"
)

// CronTool lets the agent add/list cron jobs.
type CronTool struct {
	Service *cron.Service
	Channel string
	ChatID  string
}

// NewCronTool creates a cron tool.
func NewCronTool(svc *cron.Service) *CronTool {
	return &CronTool{Service: svc}
}

// SetContext sets channel/chat for delivery.
func (c *CronTool) SetContext(channel, chatID string) {
	c.Channel = channel
	c.ChatID = chatID
}

func (c *CronTool) Name() string { return "cron" }

func (c *CronTool) Description() string {
	return "Schedule reminders and recurring tasks. Actions: add, list, remove."
}

func (c *CronTool) Parameters() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"action":        map[string]any{"type": "string", "description": "Action to perform", "enum": []any{"add", "list", "remove"}},
			"message":       map[string]any{"type": "string", "description": "Reminder message (for add)"},
			"every_seconds": map[string]any{"type": "integer", "description": "Interval in seconds (for recurring tasks)"},
			"cron_expr":     map[string]any{"type": "string", "description": "Cron expression like '0 9 * * *' (for scheduled tasks)"},
			"tz":            map[string]any{"type": "string", "description": "IANA timezone for cron expressions (e.g. 'America/Vancouver')"},
			"at":            map[string]any{"type": "string", "description": "ISO datetime for one-time execution (e.g. '2026-02-12T10:30:00')"},
			"deliver":       map[string]any{"type": "boolean", "description": "Deliver response to channel (for add)"},
			"job_id":        map[string]any{"type": "string", "description": "Job ID (for remove)"},
		},
		"required": []any{"action"},
	}
}

func (c *CronTool) Execute(ctx context.Context, params map[string]any) (string, error) {
	action, _ := params["action"].(string)
	switch action {
	case "list":
		jobs, err := c.Service.ListJobs(true)
		if err != nil {
			return "Error: " + err.Error(), nil
		}
		if len(jobs) == 0 {
			return "No scheduled jobs.", nil
		}
		var out string
		for _, j := range jobs {
			status := "enabled"
			if !j.Enabled {
				status = "disabled"
			}
			out += j.ID + " " + j.Name + " " + status + "\n"
		}
		return out, nil
	case "add":
		message, _ := params["message"].(string)
		if message == "" {
			return "Error: message is required for add", nil
		}
		if c.Channel == "" || c.ChatID == "" {
			return "Error: no session context (channel/chat_id)", nil
		}
		tz, _ := params["tz"].(string)
		atStr, _ := params["at"].(string)
		deliver, _ := params["deliver"].(bool)
		if tz != "" {
			expr, _ := params["cron_expr"].(string)
			if expr == "" {
				return "Error: tz can only be used with cron_expr", nil
			}
		}
		if tz != "" {
			if _, err := time.LoadLocation(tz); err != nil {
				return "Error: unknown timezone '" + tz + "'", nil
			}
		}
		name := message
		if len(name) > 30 {
			name = name[:30]
		}
		var sch cron.Schedule
		var deleteAfterRun bool
		if every, ok := params["every_seconds"].(float64); ok && every > 0 {
			sch = cron.Schedule{Kind: cron.ScheduleEvery, EveryMs: int64(every * 1000)}
		} else if n, ok := params["every_seconds"].(int); ok && n > 0 {
			sch = cron.Schedule{Kind: cron.ScheduleEvery, EveryMs: int64(n) * 1000}
		} else if expr, ok := params["cron_expr"].(string); ok && expr != "" {
			sch = cron.Schedule{Kind: cron.ScheduleCron, Expr: expr, Tz: tz}
		} else if atStr != "" {
			t, err := parseCronAt(atStr)
			if err != nil {
				return "Error: invalid at format: " + err.Error(), nil
			}
			sch = cron.Schedule{Kind: cron.ScheduleAt, AtMs: t.UnixMilli()}
			deleteAfterRun = true
		} else {
			return "Error: either every_seconds, cron_expr, or at is required", nil
		}
		job, err := c.Service.AddJob(name, sch, message, deliver, c.Channel, c.ChatID, deleteAfterRun)
		if err != nil {
			return "Error: " + err.Error(), nil
		}
		return "Created job '" + job.Name + "' (id: " + job.ID + ")", nil
	case "remove":
		jobID, _ := params["job_id"].(string)
		if jobID == "" {
			return "Error: job_id required for remove", nil
		}
		ok, err := c.Service.RemoveJob(jobID)
		if err != nil {
			return "Error: " + err.Error(), nil
		}
		if !ok {
			return "Job not found", nil
		}
		return "Removed job " + jobID, nil
	default:
		return "Error: unknown action", nil
	}
}

func parseCronAt(s string) (time.Time, error) {
	if s == "" {
		return time.Time{}, fmt.Errorf("empty time")
	}
	if ms, err := strconv.ParseInt(s, 10, 64); err == nil {
		return time.UnixMilli(ms), nil
	}
	for _, layout := range []string{time.RFC3339, "2006-01-02T15:04:05", "2006-01-02 15:04:05"} {
		if t, err := time.Parse(layout, s); err == nil {
			return t, nil
		}
	}
	return time.Time{}, fmt.Errorf("invalid time format")
}
