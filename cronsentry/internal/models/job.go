package models

import (
	"time"
)

// JobStatus represents the current status of a monitored job
type JobStatus string

const (
	// StatusHealthy indicates the job is running as expected
	StatusHealthy JobStatus = "healthy"
	// StatusLate indicates the job is running but outside the expected window
	StatusLate JobStatus = "late"
	// StatusMissing indicates the job has missed its expected run time
	StatusMissing JobStatus = "missing"
	// StatusPaused indicates the job monitoring is paused
	StatusPaused JobStatus = "paused"
)

// Job represents a monitored cron job
type Job struct {
	ID          string    `json:"id" db:"id"`
	Name        string    `json:"name" db:"name"`
	Description string    `json:"description" db:"description"`
	Schedule    string    `json:"schedule" db:"schedule"`
	Timezone    string    `json:"timezone" db:"timezone"`
	GraceTime   int       `json:"grace_time" db:"grace_time"` // In minutes
	LastPing    time.Time `json:"last_ping" db:"last_ping"`
	NextExpect  time.Time `json:"next_expect" db:"next_expect"`
	Status      JobStatus `json:"status" db:"status"`
	UserID      string    `json:"user_id" db:"user_id"`
	CreatedAt   time.Time `json:"created_at" db:"created_at"`
	UpdatedAt   time.Time `json:"updated_at" db:"updated_at"`
}

// JobEvent represents a ping or an incident for a job
type JobEvent struct {
	ID        string    `json:"id" db:"id"`
	JobID     string    `json:"job_id" db:"job_id"`
	Type      string    `json:"type" db:"type"` // "ping", "miss", "recovery"
	Data      string    `json:"data" db:"data"` // Additional data as JSON string
	CreatedAt time.Time `json:"created_at" db:"created_at"`
}

// User represents a registered user of the system
type User struct {
	ID        string    `json:"id" db:"id"`
	Email     string    `json:"email" db:"email"`
	Name      string    `json:"name" db:"name"`
	Password  string    `json:"-" db:"password_hash"` // Hashed, never returned in JSON
	CreatedAt time.Time `json:"created_at" db:"created_at"`
	UpdatedAt time.Time `json:"updated_at" db:"updated_at"`
}
