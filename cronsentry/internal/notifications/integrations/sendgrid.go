package integrations

import (
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

func (sc SendgridClient) SendEmail(to, subject, body string) error {
	if !sc.enabled {
		sc.logger.Printf("Email would be sent to %s: %s", to, subject)
		return nil
	}

	from := mail.NewEmail("CronSentry", "cronsentry@example.com")
	message := mail.NewSingleEmail(from, subject, &mail.Email{Name: to, Address: to}, body, "") // fix last arg
	response, err := sc.Send(message)
	if err != nil {
		sc.logger.Println("Error sending email", err)
	} else {
		sc.logger.Printf("Status code %d, headers: %v", response.StatusCode, response.Headers)
	}

	return nil
}
