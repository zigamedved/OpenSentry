package integrations

import (
	"fmt"
	"log"

	"github.com/sendgrid/sendgrid-go"
	"github.com/sendgrid/sendgrid-go/helpers/mail"
)

type SendgridClient struct {
	*sendgrid.Client
	logger  *log.Logger
	enabled bool
}

func NewSendgridSendClient(apiKey string, logger *log.Logger, enabled bool) SendgridClient {
	return SendgridClient{
		sendgrid.NewSendClient(apiKey),
		logger,
		enabled,
	}
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
		sc.logger.Printf("Email would be sent to %s: %s", to, subject)
		return nil
	}

	from := mail.NewEmail("OpenSentry", "noreply@example.com")
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
