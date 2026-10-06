package notifications

import (
	"database/sql"
	"errors"
	"fmt"
	"html"
	"log"
	"time"

	"github.com/zigamedved/OpenSentry/internal/notifications/integrations"
)

type NotificationProcessor struct {
	db           *sql.DB
	emailSender  EmailSender
	logger       *log.Logger
	dashboardURL string
	done         chan struct{}
}

func NewNotificationProcessor(db *sql.DB, emailSender EmailSender, logger *log.Logger, dashboardURL string) *NotificationProcessor {
	return &NotificationProcessor{
		db:           db,
		emailSender:  emailSender,
		logger:       logger,
		dashboardURL: dashboardURL,
		done:         make(chan struct{}),
	}
}

func (np *NotificationProcessor) Start() {
	go func() {
		ticker := time.NewTicker(30 * time.Second)
		defer ticker.Stop()

		for {
			select {
			case <-ticker.C:
				if err := np.processNotifications(); err != nil {
					np.logger.Printf("Error processing notifications: %v", err)
				}
			case <-np.done:
				return
			}
		}
	}()
}

func (np *NotificationProcessor) Stop() {
	close(np.done)
}

func (np *NotificationProcessor) processNotifications() error {
	query := `
		SELECT n.id, n.message, n.type, n.created_at, u.email, j.name
		FROM notifications n
		JOIN users u ON n.user_id = u.id
		JOIN jobs j ON n.job_id = j.id
		WHERE n.status = 'pending'
		LIMIT 10
	`

	rows, err := np.db.Query(query)
	if err != nil {
		return fmt.Errorf("error querying notifications: %w", err)
	}
	defer rows.Close()

	for rows.Next() {
		var notification struct {
			ID        string
			Message   string
			Type      string
			CreatedAt time.Time
			Email     string
			JobName   string
		}

		if err := rows.Scan(
			&notification.ID,
			&notification.Message,
			&notification.Type,
			&notification.CreatedAt,
			&notification.Email,
			&notification.JobName,
		); err != nil {
			return fmt.Errorf("error scanning notification: %w", err)
		}

		var processErr error
		if notification.Type == "email" {
			processErr = np.sendEmailNotification(notification.ID, notification.Email, notification.JobName, notification.Message, notification.CreatedAt)
		} else {
			np.logger.Printf("Unsupported notification type: %s", notification.Type)
			processErr = np.markNotificationFailed(notification.ID, fmt.Sprintf("Unsupported type: %s", notification.Type))
		}

		if processErr != nil {
			np.logger.Printf("Error processing notification %s: %v", notification.ID, processErr)
		}
	}

	if err := rows.Err(); err != nil {
		return fmt.Errorf("error iterating notifications: %w", err)
	}

	return nil
}

func alertEmail(jobName, message string, missedAt time.Time, dashboardURL string) (subject, body string) {
	subject = fmt.Sprintf("OpenSentry Alert: Job '%s'", jobName)
	when := missedAt.UTC().Format(time.RFC1123)
	link := "Open your OpenSentry dashboard."
	if dashboardURL != "" {
		link = fmt.Sprintf(`<a href="%s">Open the dashboard</a>`, html.EscapeString(dashboardURL))
	}
	body = fmt.Sprintf(`
		<html>
			<body>
				<h2>OpenSentry Alert</h2>
				<p>%s</p>
				<p>Job: <strong>%s</strong></p>
				<p>Missed at: <strong>%s</strong></p>
				<hr>
				<p>%s</p>
			</body>
		</html>
	`, html.EscapeString(message), html.EscapeString(jobName), html.EscapeString(when), link)
	return subject, body
}

func (np *NotificationProcessor) sendEmailNotification(id, email, jobName, message string, missedAt time.Time) error {
	subject, body := alertEmail(jobName, message, missedAt, np.dashboardURL)

	if err := np.emailSender.SendEmail(email, subject, body); err != nil {
		if errors.Is(err, integrations.ErrEmailDryRun) {
			if markErr := np.markNotificationStatus(id, "skipped", ""); markErr != nil {
				np.logger.Printf("Error marking notification as skipped: %v", markErr)
				return fmt.Errorf("error marking notification as skipped: %w", markErr)
			}
			return nil
		}
		if markErr := np.markNotificationFailed(id, err.Error()); markErr != nil {
			np.logger.Printf("Error marking notification as failed: %v", markErr)
		}
		return fmt.Errorf("error sending email: %w", err)
	}

	if err := np.markNotificationSent(id); err != nil {
		return fmt.Errorf("error marking notification as sent: %w", err)
	}

	return nil
}

func (np *NotificationProcessor) markNotificationSent(id string) error {
	return np.markNotificationStatus(id, "sent", "")
}

func (np *NotificationProcessor) markNotificationFailed(id, reason string) error {
	return np.markNotificationStatus(id, "failed", reason)
}

func (np *NotificationProcessor) markNotificationStatus(id, status, reason string) error {
	query := `
		UPDATE notifications
		SET status = $1,
		    sent_at = CASE WHEN $1 = 'sent' THEN $2 ELSE sent_at END,
		    data = CASE
		        WHEN $3 = '' THEN data
		        ELSE jsonb_build_object('error', $3::text)
		    END
		WHERE id = $4
	`

	_, err := np.db.Exec(query, status, time.Now().UTC(), reason, id)
	if err != nil {
		return fmt.Errorf("error updating notification: %w", err)
	}

	return nil
}
