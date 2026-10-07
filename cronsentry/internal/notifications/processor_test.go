package notifications

import (
	"database/sql"
	"errors"
	"io"
	"log"
	"strings"
	"testing"
	"time"

	_ "github.com/lib/pq"
	"github.com/zigamedved/OpenSentry/internal/notifications/integrations"
)

type stubEmailSender struct {
	err     error
	calls   int
	lastTo  string
	lastSub string
}

func (s *stubEmailSender) SendEmail(email, subject, body string) error {
	s.calls++
	s.lastTo = email
	s.lastSub = subject
	return s.err
}

// unreachableDB returns a non-nil *sql.DB that fails on Exec (no live Postgres).
func unreachableDB(t *testing.T) *sql.DB {
	t.Helper()
	db, err := sql.Open("postgres", "host=127.0.0.1 port=1 user=x password=x dbname=x sslmode=disable")
	if err != nil {
		t.Fatalf("sql.Open: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return db
}

func TestSendEmailNotification_PropagatesSenderError(t *testing.T) {
	sender := &stubEmailSender{err: errors.New("sendgrid 401")}
	np := NewNotificationProcessor(unreachableDB(t), sender, log.New(io.Discard, "", 0), "https://monitor.example")

	err := np.sendEmailNotification("n1", "a@b.c", "job", "missed", time.Now().UTC())
	if err == nil {
		t.Fatal("expected error when email send fails")
	}
	if !strings.Contains(err.Error(), "sendgrid 401") {
		t.Fatalf("error should wrap sender failure, got: %v", err)
	}
	if sender.calls != 1 {
		t.Fatalf("SendEmail calls = %d, want 1", sender.calls)
	}
}

func TestSendEmailNotification_SuccessPathStillErrorsWithoutWorkingDB(t *testing.T) {
	sender := &stubEmailSender{err: nil}
	np := NewNotificationProcessor(unreachableDB(t), sender, log.New(io.Discard, "", 0), "")

	err := np.sendEmailNotification("n1", "a@b.c", "job", "missed", time.Now().UTC())
	if err == nil {
		t.Fatal("expected error marking sent against unreachable DB")
	}
	if !strings.Contains(err.Error(), "marking notification as sent") {
		t.Fatalf("expected mark-sent error, got: %v", err)
	}
	if sender.calls != 1 {
		t.Fatalf("SendEmail calls = %d, want 1", sender.calls)
	}
}

func TestSendEmailNotification_DryRunMarksSkipped(t *testing.T) {
	sender := &stubEmailSender{err: integrations.ErrEmailDryRun}
	np := NewNotificationProcessor(unreachableDB(t), sender, log.New(io.Discard, "", 0), "")

	err := np.sendEmailNotification("n1", "a@b.c", "job", "missed", time.Now().UTC())
	if err == nil || !strings.Contains(err.Error(), "skipped") {
		t.Fatalf("expected skipped-status error against unreachable DB, got %v", err)
	}
}

func TestAlertEmailIncludesJobMissAndDashboard(t *testing.T) {
	missed := time.Date(2026, 10, 6, 9, 0, 0, 0, time.UTC)
	subject, body := alertEmail("Nightly backup", "Job missed its schedule", missed, "https://monitor.example/dashboard")
	if !strings.Contains(subject, "Nightly backup") {
		t.Fatalf("subject = %q", subject)
	}
	if !strings.Contains(body, "Nightly backup") || !strings.Contains(body, "Job missed its schedule") {
		t.Fatalf("body missing job details: %s", body)
	}
	if !strings.Contains(body, missed.Format(time.RFC1123)) {
		t.Fatalf("body missing miss time: %s", body)
	}
	if !strings.Contains(body, "https://monitor.example/dashboard") {
		t.Fatalf("body missing dashboard link: %s", body)
	}

	_, plain := alertEmail("Backup", "missed", missed, "")
	if strings.Contains(plain, "href=") {
		t.Fatal("empty dashboard URL should not emit a link")
	}
	if !strings.Contains(plain, "Open your OpenSentry dashboard") {
		t.Fatalf("plain body = %s", plain)
	}
}
