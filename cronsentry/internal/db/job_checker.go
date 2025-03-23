package db

import (
	"fmt"
	"log"
	"time"

	"github.com/google/uuid"
	"github.com/zigamedved/cronsentry/internal/models"
)

type JobChecker struct {
	db     *Database
	logger *log.Logger
	done   chan struct{}
}

func NewJobChecker(database *Database, logger *log.Logger) *JobChecker {
	return &JobChecker{
		db:     database,
		logger: logger,
		done:   make(chan struct{}),
	}
}

func (jc *JobChecker) Start() {
	go func() {
		ticker := time.NewTicker(1 * time.Minute)
		defer ticker.Stop()

		for {
			select {
			case <-ticker.C:
				if err := jc.checkJobs(); err != nil {
					jc.logger.Printf("Error checking jobs: %v", err)
				}
			case <-jc.done:
				return
			}
		}
	}()
}

func (jc *JobChecker) Stop() {
	close(jc.done)
}

func (jc *JobChecker) checkJobs() error {
	query := `
		SELECT id, name, user_id, last_ping, next_expect
		FROM jobs
		WHERE status != $1
		AND next_expect < $2
	`

	now := time.Now().UTC()
	rows, err := jc.db.db.Query(query, models.StatusPaused, now)
	if err != nil {
		return fmt.Errorf("error querying jobs: %w", err)
	}
	defer rows.Close()

	for rows.Next() {
		var job struct {
			ID         string
			Name       string
			UserID     string
			LastPing   time.Time
			NextExpect time.Time
		}

		if err := rows.Scan(&job.ID, &job.Name, &job.UserID, &job.LastPing, &job.NextExpect); err != nil {
			return fmt.Errorf("error scanning job: %w", err)
		}

		if job.NextExpect.Before(now) {
			if err := jc.markJobMissing(job.ID, job.Name, job.UserID); err != nil {
				jc.logger.Printf("Error marking job as missing: %v", err)
			}
		}
	}

	if err := rows.Err(); err != nil {
		return fmt.Errorf("error iterating jobs: %w", err)
	}

	return nil
}

func (jc *JobChecker) markJobMissing(jobID, jobName, userID string) error {
	tx, err := jc.db.db.Begin()
	if err != nil {
		return fmt.Errorf("error starting transaction: %w", err)
	}

	_, err = tx.Exec(`
		UPDATE jobs
		SET status = $1, updated_at = $2
		WHERE id = $3 AND status != $1
	`, models.StatusMissing, time.Now().UTC(), jobID)

	if err != nil {
		tx.Rollback()
		return fmt.Errorf("error updating job status: %w", err)
	}

	eventID := uuid.New().String()
	now := time.Now().UTC()

	_, err = tx.Exec(`
		INSERT INTO job_events (id, job_id, type, data, created_at)
		VALUES ($1, $2, $3, $4, $5)
	`, eventID, jobID, "miss", "{}", now)

	if err != nil {
		tx.Rollback()
		return fmt.Errorf("error creating event: %w", err)
	}

	notificationID := uuid.New().String()
	message := fmt.Sprintf("Job '%s' has missed its scheduled run time", jobName)

	_, err = tx.Exec(`
		INSERT INTO notifications (id, user_id, job_id, message, type, status, created_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7)
	`, notificationID, userID, jobID, message, "email", "pending", now)

	if err != nil {
		tx.Rollback()
		return fmt.Errorf("error creating notification: %w", err)
	}

	err = tx.Commit()
	if err != nil {
		return fmt.Errorf("error committing transaction: %w", err)
	}

	jc.logger.Printf("Job %s marked as missing", jobID)

	return nil
}
