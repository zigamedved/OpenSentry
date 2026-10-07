package integrations

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestValidateWebhookURL(t *testing.T) {
	if err := ValidateWebhookURL("https://hooks.slack.com/services/T/B/secret"); err != nil {
		t.Fatal(err)
	}
	if err := ValidateWebhookURL("http://127.0.0.1:9/hook"); err != nil {
		t.Fatal(err)
	}
	for _, raw := range []string{"", "not a url", "javascript:alert(1)", "ftp://example.com/hook", "https://"} {
		if err := ValidateWebhookURL(raw); err == nil {
			t.Fatalf("expected error for %q", raw)
		}
	}
}

func TestAlertTextAndPayloads(t *testing.T) {
	when := time.Date(2026, 10, 7, 12, 0, 0, 0, time.UTC)
	text := AlertText("Nightly backup", "Job 'Nightly backup' has missed its scheduled run time", when, "https://monitor.example")
	if !strings.Contains(text, "Nightly backup") || !strings.Contains(text, when.Format(time.RFC1123)) || !strings.Contains(text, "https://monitor.example") {
		t.Fatalf("text = %q", text)
	}

	slack, err := SlackPayload(text)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(slack), `"text"`) {
		t.Fatalf("slack payload = %s", slack)
	}
	discord, err := DiscordPayload(text)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(discord), `"content"`) || strings.Contains(string(discord), `"text"`) {
		t.Fatalf("discord payload = %s", discord)
	}
}

func TestWebhookClientPost(t *testing.T) {
	var gotBody, gotType string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotType = r.Header.Get("Content-Type")
		body, _ := io.ReadAll(r.Body)
		gotBody = string(body)
		w.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()

	client := WebhookClient{HTTP: &http.Client{}}
	payload := []byte(`{"text":"missed"}`)
	if err := client.Post(server.URL, payload); err != nil {
		t.Fatal(err)
	}
	if gotType != "application/json" || gotBody != string(payload) {
		t.Fatalf("type %q body %s", gotType, gotBody)
	}

	failing := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "nope", http.StatusBadGateway)
	}))
	defer failing.Close()
	err := client.Post(failing.URL, payload)
	if err == nil || !strings.Contains(err.Error(), "502") {
		t.Fatalf("expected status error, got %v", err)
	}
}
