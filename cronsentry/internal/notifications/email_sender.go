package notifications

type EmailSender interface {
	SendEmail(email, subject, body string) error
}
