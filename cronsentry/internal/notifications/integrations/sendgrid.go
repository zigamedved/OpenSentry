package integrations

import (
	"errors"
	"fmt"
	"log"
	"strings"

	"github.com/sendgrid/sendgrid-go"
	"github.com/sendgrid/sendgrid-go/helpers/mail"
)

// ErrEmailDryRun is returned when sending is disabled. Callers should record
// the notification as skipped rather than failed or sent.
var ErrEmailDryRun = errors.New("email dry-run")

type SendgridClient struct {
	*sendgrid.Client
	logger    *log.Logger
	enabled   bool
	fromName  string
	fromEmail string
}

func NewSendgridSendClient(apiKey string, logger *log.Logger, enabled bool, fromName, fromEmail string) SendgridClient {
	return SendgridClient{
		Client:    sendgrid.NewSendClient(apiKey),
		logger:    logger,
		enabled:   enabled,
		fromName:  fromName,
		fromEmail: fromEmail,
	}
}

// ParseEmailFrom accepts "Name <addr@host>" or a bare address.
// An empty value uses a local placeholder that is not a real mailbox.
func ParseEmailFrom(value string) (name, email string) {
	value = strings.TrimSpace(value)
	if value == "" {
		return "OpenSentry", "noreply@localhost"
	}
	if start := strings.Index(value, "<"); start >= 0 && strings.HasSuffix(value, ">") {
		name = strings.TrimSpace(value[:start])
		email = strings.TrimSpace(strings.TrimSuffix(value[start+1:], ">"))
		if name == "" {
			name = "OpenSentry"
		}
		return name, email
	}
	return "OpenSentry", value
}

// SendResultError returns a non-nil error when the SendGrid call failed or
// returned a non-2xx status. Extracted for unit testing without a live API.
func SendResultError(err error, statusCode int) error {
	if err != nil {
		return err
	}
	if statusCode < 200 || statusCode >= 300 {
		return fmt.Errorf("sendgrid returned status %d", statusCode)
	}
	return nil
}

func (sc SendgridClient) SendEmail(to, subject, body string) error {
	if !sc.enabled {
		sc.logger.Printf("would send email to %s: %s", to, subject)
		return ErrEmailDryRun
	}

	from := mail.NewEmail(sc.fromName, sc.fromEmail)
	toEmail := mail.NewEmail(to, to)
	message := mail.NewSingleEmail(from, subject, toEmail, "", body)
	response, err := sc.Send(message)
	statusCode := 0
	if response != nil {
		statusCode = response.StatusCode
		sc.logger.Printf("Status code %d, headers: %v", response.StatusCode, response.Headers)
	}
	if sendErr := SendResultError(err, statusCode); sendErr != nil {
		sc.logger.Println("Error sending email", sendErr)
		return sendErr
	}

	return nil
}
