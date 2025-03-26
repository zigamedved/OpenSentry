package db

import (
	"database/sql"
	"fmt"
	"os"
	"time"

	"github.com/google/uuid"
	_ "github.com/lib/pq"
	"github.com/zigamedved/cronsentry/internal/models"
)

type Database struct {
	db *sql.DB
}

func NewDatabase() (*Database, error) {
	host := getEnv("DB_HOST", "localhost")
	port := getEnv("DB_PORT", "5432")
	user := getEnv("DB_USER", "postgres")
	password := getEnv("DB_PASSWORD", "postgres")
	dbname := getEnv("DB_NAME", "cronsentry")
	sslmode := getEnv("DB_SSLMODE", "disable")

	connStr := fmt.Sprintf("host=%s port=%s user=%s password=%s dbname=%s sslmode=%s",
		host, port, user, password, dbname, sslmode)

	db, err := sql.Open("postgres", connStr)
	if err != nil {
		return nil, fmt.Errorf("error connecting to database: %w", err)
	}

	err = db.Ping()
	if err != nil {
		return nil, fmt.Errorf("error pinging database: %w", err)
	}

	db.SetMaxOpenConns(25)
	db.SetMaxIdleConns(5)
	db.SetConnMaxLifetime(5 * time.Minute)

	return &Database{db: db}, nil
}

func (d *Database) Close() error {
	return d.db.Close()
}

func (d *Database) GetJob(id string) (*models.Job, error) {
	query := `
		SELECT id, name, description, schedule, grace_time, 
		       last_ping, next_expect, status, user_id, created_at, updated_at
		FROM jobs
		WHERE id = $1
	`

	var job models.Job
	err := d.db.QueryRow(query, id).Scan(
		&job.ID, &job.Name, &job.Description, &job.Schedule,
		&job.GraceTime, &job.LastPing, &job.NextExpect,
		&job.Status, &job.UserID, &job.CreatedAt, &job.UpdatedAt,
	)

	if err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		return nil, fmt.Errorf("error querying job: %w", err)
	}

	return &job, nil
}

func (d *Database) ListJobsByUser(userID string) ([]*models.Job, error) {
	query := `
		SELECT id, name, description, schedule, grace_time, 
		       last_ping, next_expect, status, user_id, created_at, updated_at
		FROM jobs
		WHERE user_id = $1
		ORDER BY created_at DESC
	`

	rows, err := d.db.Query(query, userID)
	if err != nil {
		return nil, fmt.Errorf("error querying jobs: %w", err)
	}
	defer rows.Close()

	var jobs []*models.Job
	for rows.Next() {
		var job models.Job
		err := rows.Scan(
			&job.ID, &job.Name, &job.Description, &job.Schedule,
			&job.GraceTime, &job.LastPing, &job.NextExpect,
			&job.Status, &job.UserID, &job.CreatedAt, &job.UpdatedAt,
		)
		if err != nil {
			return nil, fmt.Errorf("error scanning job row: %w", err)
		}
		jobs = append(jobs, &job)
	}

	if err = rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating job rows: %w", err)
	}

	return jobs, nil
}

func (d *Database) CreateJob(job *models.Job) error {
	if job.ID == "" {
		job.ID = uuid.New().String()
	}

	now := time.Now().UTC()
	job.CreatedAt = now
	job.UpdatedAt = now

	query := `
		INSERT INTO jobs (id, name, description, schedule, grace_time, 
		                 last_ping, next_expect, status, user_id, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12)
	`

	_, err := d.db.Exec(query,
		job.ID, job.Name, job.Description, job.Schedule,
		job.GraceTime, job.LastPing, job.NextExpect,
		job.Status, job.UserID, job.CreatedAt, job.UpdatedAt,
	)
	if err != nil {
		return fmt.Errorf("error creating job: %w", err)
	}

	return nil
}

func (d *Database) UpdateJob(job *models.Job) error {
	job.UpdatedAt = time.Now().UTC()

	query := `
		UPDATE jobs
		SET name = $1, description = $2, schedule = $3,
		grace_time = $4, last_ping = $5, next_expect = $6, status = $7, updated_at = $8
		WHERE id = $9 AND user_id = $10
	`

	result, err := d.db.Exec(query,
		job.Name, job.Description, job.Schedule,
		job.GraceTime, job.LastPing, job.NextExpect, job.Status,
		job.UpdatedAt, job.ID, job.UserID,
	)
	if err != nil {
		return fmt.Errorf("error updating job: %w", err)
	}

	rows, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("error getting rows affected: %w", err)
	}

	if rows == 0 {
		return fmt.Errorf("job not found or not owned by user")
	}

	return nil
}

func (d *Database) RecordPing(jobID string) error {
	tx, err := d.db.Begin()
	if err != nil {
		return fmt.Errorf("error starting transaction: %w", err)
	}

	now := time.Now().UTC()
	query := `
		UPDATE jobs
		SET last_ping = $1, updated_at = $1
		WHERE id = $2
		RETURNING status
	`

	var currentStatus models.JobStatus
	err = tx.QueryRow(query, now, jobID).Scan(&currentStatus)
	if err != nil {
		tx.Rollback()
		if err == sql.ErrNoRows {
			return fmt.Errorf("job not found")
		}
		return fmt.Errorf("error updating job ping: %w", err)
	}

	eventID := uuid.New().String()
	eventType := "ping"

	if currentStatus == models.StatusMissing {
		eventType = "recovery"

		_, err = tx.Exec(`
			UPDATE jobs
			SET status = $1
			WHERE id = $2
		`, models.StatusHealthy, jobID)

		if err != nil {
			tx.Rollback()
			return fmt.Errorf("error updating job status: %w", err)
		}
	}

	_, err = tx.Exec(`
		INSERT INTO job_events (id, job_id, type, data, created_at)
		VALUES ($1, $2, $3, $4, $5)
	`, eventID, jobID, eventType, "{}", now)

	if err != nil {
		tx.Rollback()
		return fmt.Errorf("error creating event record: %w", err)
	}

	err = tx.Commit()
	if err != nil {
		return fmt.Errorf("error committing transaction: %w", err)
	}

	return nil
}

func getEnv(key, defaultValue string) string {
	if value, exists := os.LookupEnv(key); exists {
		return value
	}
	return defaultValue
}

func (d *Database) GetDB() *sql.DB {
	return d.db
}
