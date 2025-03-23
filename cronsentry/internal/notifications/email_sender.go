package notifications

import (
	"fmt"
	"log"
	"net/smtp"
	"os"
)

// EmailSender handles sending email notifications
type EmailSender struct {
	smtpHost     string
	smtpPort     string
	smtpUsername string
	smtpPassword string
	from         string
	logger       *log.Logger
	enabled      bool
}

// NewEmailSender creates a new email sender
func NewEmailSender(logger *log.Logger) *EmailSender {
	smtpHost := os.Getenv("SMTP_HOST")
	smtpPort := os.Getenv("SMTP_PORT")
	smtpUsername := os.Getenv("SMTP_USERNAME")
	smtpPassword := os.Getenv("SMTP_PASSWORD")
	from := os.Getenv("SMTP_FROM")

	// Only enable if all required fields are present
	enabled := smtpHost != "" && smtpPort != "" && smtpUsername != "" && smtpPassword != "" && from != ""

	if !enabled {
		logger.Println("Email notifications disabled (missing configuration)")
	} else {
		logger.Println("Email notifications enabled")
	}

	return &EmailSender{
		smtpHost:     smtpHost,
		smtpPort:     smtpPort,
		smtpUsername: smtpUsername,
		smtpPassword: smtpPassword,
		from:         from,
		logger:       logger,
		enabled:      enabled,
	}
}

// Send sends an email notification
func (e *EmailSender) Send(to, subject, body string) error {
	if !e.enabled {
		e.logger.Printf("Email would be sent to %s: %s", to, subject)
		return nil
	}

	// Set up authentication information
	auth := smtp.PlainAuth("", e.smtpUsername, e.smtpPassword, e.smtpHost)

	// Format email
	mime := "MIME-version: 1.0;\nContent-Type: text/html; charset=\"UTF-8\";\n\n"
	msg := []byte(
		fmt.Sprintf("To: %s\r\nFrom: %s\r\nSubject: %s\r\n%s\r\n%s",
			to, e.from, subject, mime, body),
	)

	// Send email
	addr := fmt.Sprintf("%s:%s", e.smtpHost, e.smtpPort)
	if err := smtp.SendMail(addr, auth, e.from, []string{to}, msg); err != nil {
		return fmt.Errorf("failed to send email: %w", err)
	}

	e.logger.Printf("Email sent to %s: %s", to, subject)

	return nil
}
