package notifications

import (
	"database/sql"
	"errors"
	"io"
	"log"
	"strings"
	"testing"

	_ "github.com/lib/pq"
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
	np := NewNotificationProcessor(unreachableDB(t), sender, log.New(io.Discard, "", 0))

	err := np.sendEmailNotification("n1", "a@b.c", "job", "missed")
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
	np := NewNotificationProcessor(unreachableDB(t), sender, log.New(io.Discard, "", 0))

	err := np.sendEmailNotification("n1", "a@b.c", "job", "missed")
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
