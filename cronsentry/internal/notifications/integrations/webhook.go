package integrations

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// ValidateWebhookURL accepts http and https URLs with a host.
func ValidateWebhookURL(raw string) error {
	parsed, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || parsed.Host == "" {
		return errors.New("webhook url must be an http or https URL")
	}
	if parsed.Scheme != "https" && parsed.Scheme != "http" {
		return errors.New("webhook url must be an http or https URL")
	}
	return nil
}

// AlertText is the plain-text body shared by Slack and Discord.
func AlertText(jobName, message string, occurred time.Time, dashboardURL string) string {
	text := fmt.Sprintf("OpenSentry: %s\nJob: %s\nWhen: %s", message, jobName, occurred.UTC().Format(time.RFC1123))
	if dashboardURL != "" {
		text += "\nDashboard: " + dashboardURL
	}
	return text
}

func SlackPayload(text string) ([]byte, error) {
	return json.Marshal(map[string]string{"text": text})
}

func DiscordPayload(text string) ([]byte, error) {
	return json.Marshal(map[string]string{"content": text})
}

// WebhookClient posts a JSON body to an incoming webhook.
type WebhookClient struct {
	HTTP *http.Client
}

func (c WebhookClient) client() *http.Client {
	if c.HTTP != nil {
		return c.HTTP
	}
	return &http.Client{Timeout: 10 * time.Second}
}

func (c WebhookClient) Post(webhookURL string, payload []byte) error {
	if err := ValidateWebhookURL(webhookURL); err != nil {
		return err
	}
	req, err := http.NewRequest(http.MethodPost, webhookURL, bytes.NewReader(payload))
	if err != nil {
		return fmt.Errorf("webhook request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.client().Do(req)
	if err != nil {
		return fmt.Errorf("webhook request failed: %w", err)
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 1024))
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("webhook returned status %d", resp.StatusCode)
	}
	return nil
}
