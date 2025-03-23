package notifications

import (
	"database/sql"
	"fmt"
	"log"
	"time"
)

type NotificationProcessor struct {
	db          *sql.DB
	emailSender EmailSender // should be interface that has SendEmail method implemented
	logger      *log.Logger
	done        chan struct{}
}

func NewNotificationProcessor(db *sql.DB, emailSender EmailSender, logger *log.Logger) *NotificationProcessor {
	return &NotificationProcessor{
		db:          db,
		emailSender: emailSender,
		logger:      logger,
		done:        make(chan struct{}),
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
		SELECT n.id, n.message, n.type, u.email, j.name
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
			ID      string
			Message string
			Type    string
			Email   string
			JobName string
		}

		if err := rows.Scan(
			&notification.ID,
			&notification.Message,
			&notification.Type,
			&notification.Email,
			&notification.JobName,
		); err != nil {
			return fmt.Errorf("error scanning notification: %w", err)
		}

		var processErr error
		if notification.Type == "email" {
			processErr = np.sendEmailNotification(notification.ID, notification.Email, notification.JobName, notification.Message)
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

func (np *NotificationProcessor) sendEmailNotification(id, email, jobName, message string) error {
	subject := fmt.Sprintf("CronSentry Alert: Job '%s'", jobName)
	body := fmt.Sprintf(`
		<html>
			<body>
				<h2>CronSentry Alert</h2>
				<p>%s</p>
				<p>Job: <strong>%s</strong></p>
				<p>Time: <strong>%s</strong></p>
				<hr>
				<p>View details in your <a href="https://cronsentry.example.com/dashboard">CronSentry Dashboard</a></p>
			</body>
		</html>
	`, message, jobName, time.Now().Format(time.RFC1123))

	if err := np.emailSender.SendEmail(email, subject, body); err != nil {
		if err := np.markNotificationFailed(id, err.Error()); err != nil {
			np.logger.Printf("Error marking notification as failed: %v", err)
		}
		return fmt.Errorf("error sending email: %w", err)
	}

	if err := np.markNotificationSent(id); err != nil {
		return fmt.Errorf("error marking notification as sent: %w", err)
	}

	return nil
}

func (np *NotificationProcessor) markNotificationSent(id string) error {
	query := `
		UPDATE notifications
		SET status = 'sent', sent_at = $1
		WHERE id = $2
	`

	_, err := np.db.Exec(query, time.Now().UTC(), id)
	if err != nil {
		return fmt.Errorf("error updating notification: %w", err)
	}

	return nil
}

func (np *NotificationProcessor) markNotificationFailed(id, reason string) error {
	query := `
		UPDATE notifications
		SET status = 'failed', data = jsonb_set(COALESCE(data, '{}'::jsonb), '{error}', $1)
		WHERE id = $2
	`

	_, err := np.db.Exec(query, fmt.Sprintf("\"%s\"", reason), id)
	if err != nil {
		return fmt.Errorf("error updating notification: %w", err)
	}

	return nil
}
