package db

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/adhocore/gronx"
	"github.com/google/uuid"
	"github.com/zigamedved/OpenSentry/internal/models"
)

// pingBodyKeep is how many newest events retain a stored body.
// Older event bodies are removed. The job row still keeps the latest body.
const pingBodyKeep = 20

// RecordPingSignal records a success, start, or fail ping for a ping token.
// body is already size-capped by the API. truncated is recorded on the event.
// A non-empty body replaces the job's last body. An empty body leaves it.
// start stores started_at and does not move the schedule or the status.
// success clears started_at, stores the duration when a start exists, and
// moves next_expect. A missing job becomes healthy and sends a recovery alert.
// A paused job stays paused.
// fail clears started_at. A job that is not paused and not already missing
// becomes missing and sends one failure alert. A second fail does not alert again.
func (d *Database) RecordPingSignal(token, kind, body string, truncated bool) error {
	now := time.Now().UTC()
	job, err := requireJob(d.GetJobByPingToken(token))
	if err != nil {
		if errors.Is(err, ErrJobNotFound) {
			return err
		}
		return fmt.Errorf("error recording ping: %w", err)
	}

	var duration *int64
	if kind != "start" && job.StartedAt != nil {
		ms := now.Sub(*job.StartedAt).Milliseconds()
		if ms < 0 {
			ms = 0
		}
		duration = &ms
	}

	tx, err := d.db.Begin()
	if err != nil {
		return fmt.Errorf("error starting transaction: %w", err)
	}

	eventType := kind
	alert := ""
	switch kind {
	case "start":
		err = execPing(tx, `
			UPDATE jobs
			SET started_at = $1, updated_at = $1,
			    last_body = CASE WHEN $2 <> '' THEN $2 ELSE last_body END
			WHERE id = $3
		`, now, body, job.ID)
		eventType = "start"
	case "fail":
		err = execPing(tx, `
			UPDATE jobs
			SET updated_at = $1,
			    started_at = NULL,
			    last_duration_ms = COALESCE($2, last_duration_ms),
			    last_body = CASE WHEN $3 <> '' THEN $3 ELSE last_body END,
			    status = CASE
			      WHEN status = 'paused' OR status = 'missing' THEN status
			      ELSE 'missing'
			    END
			WHERE id = $4
		`, now, durationValue(duration), body, job.ID)
		eventType = "fail"
		if job.Status != models.StatusPaused && job.Status != models.StatusMissing {
			alert = "fail"
		}
	case "success":
		var nextTick time.Time
		nextTick, err = gronx.NextTickAfter(job.Schedule, now, true)
		if err != nil {
			tx.Rollback()
			return fmt.Errorf("error calculating next tick: %w", err)
		}
		var currentStatus models.JobStatus
		err = tx.QueryRow(`
			UPDATE jobs
			SET last_ping = $1, updated_at = $1, next_expect = $2,
			    started_at = NULL,
			    last_duration_ms = COALESCE($3, last_duration_ms),
			    last_body = CASE WHEN $4 <> '' THEN $4 ELSE last_body END
			WHERE id = $5
			RETURNING status
		`, now, nextTick, durationValue(duration), body, job.ID).Scan(&currentStatus)
		if err == nil && currentStatus == models.StatusMissing {
			eventType = "recovery"
			alert = "recovery"
			_, err = tx.Exec(`UPDATE jobs SET status = $1 WHERE id = $2`, models.StatusHealthy, job.ID)
		} else if err == nil {
			eventType = "ping"
		}
	default:
		tx.Rollback()
		return fmt.Errorf("unknown ping kind %q", kind)
	}
	if err != nil {
		tx.Rollback()
		if errors.Is(err, sql.ErrNoRows) {
			return ErrJobNotFound
		}
		return fmt.Errorf("error updating job ping: %w", err)
	}

	_, err = tx.Exec(`
		INSERT INTO job_events (id, job_id, type, data, created_at)
		VALUES ($1, $2, $3, $4, $5)
	`, uuid.New().String(), job.ID, eventType, pingEventData(body, truncated, duration, kind != "start"), now)
	if err != nil {
		tx.Rollback()
		return fmt.Errorf("error creating event record: %w", err)
	}

	if alert != "" {
		if err = enqueueAlerts(tx, job.UserID, job.ID, job.Name, alert, now); err != nil {
			tx.Rollback()
			return err
		}
	}

	if err = prunePingBodies(tx, job.ID); err != nil {
		tx.Rollback()
		return err
	}

	if err = tx.Commit(); err != nil {
		return fmt.Errorf("error committing transaction: %w", err)
	}
	return nil
}

func execPing(tx *sql.Tx, query string, args ...any) error {
	result, err := tx.Exec(query, args...)
	if err != nil {
		return err
	}
	rows, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if rows == 0 {
		return sql.ErrNoRows
	}
	return nil
}

func durationValue(duration *int64) any {
	if duration == nil {
		return nil
	}
	return *duration
}

func pingEventData(body string, truncated bool, duration *int64, includeDuration bool) string {
	payload := map[string]any{}
	if body != "" {
		payload["body"] = body
		if truncated {
			payload["truncated"] = true
		}
	}
	if includeDuration && duration != nil {
		payload["duration_ms"] = *duration
	}
	if len(payload) == 0 {
		return "{}"
	}
	raw, err := json.Marshal(payload)
	if err != nil {
		return "{}"
	}
	return string(raw)
}

func prunePingBodies(tx *sql.Tx, jobID string) error {
	_, err := tx.Exec(`
		UPDATE job_events
		SET data = (data - 'body') - 'truncated'
		WHERE job_id = $1
		  AND id IN (
		    SELECT id FROM job_events
		    WHERE job_id = $1 AND data ? 'body'
		    ORDER BY created_at DESC, id DESC
		    OFFSET $2
		  )
	`, jobID, pingBodyKeep)
	if err != nil {
		return fmt.Errorf("error pruning ping bodies: %w", err)
	}
	return nil
}
