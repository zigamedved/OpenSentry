package models

import (
	"time"
)

type JobStatus string

const (
	StatusHealthy JobStatus = "healthy"
	StatusMissing JobStatus = "missing"
	StatusPaused  JobStatus = "paused"
)

type Job struct {
	ID          string    `json:"id" db:"id"`
	Name        string    `json:"name" db:"name"`
	Description string    `json:"description" db:"description"`
	Schedule    string    `json:"schedule" db:"schedule"`
	GraceTime   int       `json:"grace_time" db:"grace_time"` // minutes
	LastPing    time.Time `json:"last_ping" db:"last_ping"`
	NextExpect  time.Time `json:"next_expect" db:"next_expect"`
	Status      JobStatus `json:"status" db:"status"`
	UserID      string    `json:"user_id" db:"user_id"`
	CreatedAt   time.Time `json:"created_at" db:"created_at"`
	UpdatedAt   time.Time `json:"updated_at" db:"updated_at"`
}

type JobEventType string

const (
	TypePing     JobEventType = "ping"
	TypeMiss     JobEventType = "miss"
	TypeRecovery JobEventType = "recovery"
)

type JobEvent struct {
	ID        string       `json:"id" db:"id"`
	JobID     string       `json:"job_id" db:"job_id"`
	Type      JobEventType `json:"type" db:"type"`
	Data      string       `json:"data" db:"data"` // additional data as JSON string
	CreatedAt time.Time    `json:"created_at" db:"created_at"`
}

type User struct {
	ID        string    `json:"id" db:"id"`
	Email     string    `json:"email" db:"email"`
	Name      string    `json:"name" db:"name"`
	Password  string    `json:"-" db:"password_hash"` // never returned in JSON
	CreatedAt time.Time `json:"created_at" db:"created_at"`
	UpdatedAt time.Time `json:"updated_at" db:"updated_at"`
}
