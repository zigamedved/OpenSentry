package db

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"time"

	"github.com/google/uuid"
)

// AlertChannel is a per-user Slack or Discord incoming webhook.
type AlertChannel struct {
	Kind       string
	WebhookURL string
}

func (d *Database) ListAlertChannels(userID string) ([]AlertChannel, error) {
	rows, err := d.db.Query(`
		SELECT kind, webhook_url
		FROM alert_channels
		WHERE user_id = $1
		ORDER BY kind
	`, userID)
	if err != nil {
		return nil, fmt.Errorf("error listing alert channels: %w", err)
	}
	defer rows.Close()

	var channels []AlertChannel
	for rows.Next() {
		var channel AlertChannel
		if err := rows.Scan(&channel.Kind, &channel.WebhookURL); err != nil {
			return nil, fmt.Errorf("error scanning alert channel: %w", err)
		}
		channels = append(channels, channel)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating alert channels: %w", err)
	}
	return channels, nil
}

func (d *Database) UpsertAlertChannel(userID, kind, webhookURL string) error {
	now := time.Now().UTC()
	_, err := d.db.Exec(`
		INSERT INTO alert_channels (id, user_id, kind, webhook_url, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, $5)
		ON CONFLICT (user_id, kind) DO UPDATE
		SET webhook_url = EXCLUDED.webhook_url, updated_at = EXCLUDED.updated_at
	`, uuid.New().String(), userID, kind, webhookURL, now)
	if err != nil {
		return fmt.Errorf("error saving alert channel: %w", err)
	}
	return nil
}

func (d *Database) DeleteAlertChannel(userID, kind string) error {
	_, err := d.db.Exec(`DELETE FROM alert_channels WHERE user_id = $1 AND kind = $2`, userID, kind)
	if err != nil {
		return fmt.Errorf("error deleting alert channel: %w", err)
	}
	return nil
}

// SyncEnvAlertChannels stores non-empty webhook URLs for the self-host user.
// Empty values are left alone so a URL saved in the dashboard is not wiped on boot.
func (d *Database) SyncEnvAlertChannels(userID, slackURL, discordURL string) error {
	if slackURL != "" {
		if err := d.UpsertAlertChannel(userID, "slack", slackURL); err != nil {
			return err
		}
	}
	if discordURL != "" {
		if err := d.UpsertAlertChannel(userID, "discord", discordURL); err != nil {
			return err
		}
	}
	return nil
}

// enqueueAlerts inserts one pending notification per configured channel.
// Email is always queued. Slack and Discord are queued only when that user has a webhook.
// Callers run this inside the same transaction as the job status change.
func enqueueAlerts(tx *sql.Tx, userID, jobID, jobName, event string, when time.Time) error {
	message := fmt.Sprintf("Job '%s' has missed its scheduled run time", jobName)
	if event == "recovery" {
		message = fmt.Sprintf("Job '%s' recovered", jobName)
	}

	if err := insertNotification(tx, userID, jobID, message, "email", "{}", when); err != nil {
		return err
	}

	rows, err := tx.Query(`
		SELECT kind, webhook_url
		FROM alert_channels
		WHERE user_id = $1 AND kind IN ('slack', 'discord')
	`, userID)
	if err != nil {
		return fmt.Errorf("error querying alert channels: %w", err)
	}
	defer rows.Close()

	for rows.Next() {
		var kind, webhookURL string
		if err := rows.Scan(&kind, &webhookURL); err != nil {
			return fmt.Errorf("error scanning alert channel: %w", err)
		}
		payload, err := json.Marshal(map[string]string{"webhook_url": webhookURL})
		if err != nil {
			return fmt.Errorf("error encoding webhook payload: %w", err)
		}
		if err := insertNotification(tx, userID, jobID, message, kind, string(payload), when); err != nil {
			return err
		}
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("error iterating alert channels: %w", err)
	}
	return nil
}

func insertNotification(tx *sql.Tx, userID, jobID, message, kind, data string, when time.Time) error {
	_, err := tx.Exec(`
		INSERT INTO notifications (id, user_id, job_id, message, type, status, data, created_at)
		VALUES ($1, $2, $3, $4, $5, 'pending', $6::jsonb, $7)
	`, uuid.New().String(), userID, jobID, message, kind, data, when)
	if err != nil {
		return fmt.Errorf("error creating %s notification: %w", kind, err)
	}
	return nil
}
