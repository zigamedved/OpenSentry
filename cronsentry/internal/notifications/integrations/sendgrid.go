package integrations

import (
	"fmt"
	"log"

	"github.com/sendgrid/sendgrid-go"
	"github.com/sendgrid/sendgrid-go/helpers/mail"
)

type SendgridClient struct { // add logger, testMode (enabled bool, so that we return early if test etc...)
	*sendgrid.Client
}

func NewSendgridSendClient(apiKey string) SendgridClient {
	return SendgridClient{sendgrid.NewSendClient(apiKey)}
}

func (sc SendgridClient) SendEmail(to, subject, body string) error {
	// if !sc.enabled {
	// 	e.logger.Printf("Email would be sent to %s: %s", to, subject)
	// 	return nil
	// }

	from := mail.NewEmail("CronSentry", "cronsentry@example.com")
	message := mail.NewSingleEmail(from, subject, &mail.Email{Name: to, Address: to}, body, "") // fix last arg
	response, err := sc.Send(message)
	if err != nil {
		log.Println(err)
	} else {
		fmt.Println(response.StatusCode)
		fmt.Println(response.Headers)
	}

	return nil
}
