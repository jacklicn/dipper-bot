package cron

// ScheduleKind is the type of schedule.
type ScheduleKind string

const (
	ScheduleAt    ScheduleKind = "at"
	ScheduleEvery ScheduleKind = "every"
	ScheduleCron  ScheduleKind = "cron"
)

// Schedule defines when a job runs.
type Schedule struct {
	Kind    ScheduleKind `json:"kind"`
	AtMs    int64        `json:"atMs,omitempty"`
	EveryMs int64        `json:"everyMs,omitempty"`
	Expr    string       `json:"expr,omitempty"`
	Tz      string       `json:"tz,omitempty"`
}

// Payload defines what to do when the job runs.
type Payload struct {
	Kind    string `json:"kind"`
	Message string `json:"message"`
	Deliver bool   `json:"deliver"`
	Channel string `json:"channel,omitempty"`
	To      string `json:"to,omitempty"`
}

// JobState holds runtime state of a job.
type JobState struct {
	NextRunAtMs  int64  `json:"nextRunAtMs,omitempty"`
	LastRunAtMs  int64  `json:"lastRunAtMs,omitempty"`
	LastStatus   string `json:"lastStatus,omitempty"`
	LastError    string `json:"lastError,omitempty"`
}

// Job is a scheduled job.
type Job struct {
	ID             string   `json:"id"`
	Name           string   `json:"name"`
	Enabled        bool     `json:"enabled"`
	Schedule       Schedule `json:"schedule"`
	Payload        Payload  `json:"payload"`
	State          JobState `json:"state"`
	CreatedAtMs    int64    `json:"createdAtMs"`
	UpdatedAtMs    int64    `json:"updatedAtMs"`
	DeleteAfterRun bool     `json:"deleteAfterRun"`
}

// Store is the persistent cron store.
type Store struct {
	Version int   `json:"version"`
	Jobs    []Job `json:"jobs"`
}
