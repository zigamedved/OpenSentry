package notifications

import (
	"database/sql"
	"errors"
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	_ "github.com/lib/pq"
	"github.com/zigamedved/OpenSentry/internal/db"
	"github.com/zigamedved/OpenSentry/internal/models"
	"github.com/zigamedved/OpenSentry/internal/notifications/integrations"
)

func TestMain(m *testing.M) {
	wd, err := os.Getwd()
	if err != nil {
		panic(err)
	}
	root := wd
	for {
		if _, statErr := os.Stat(filepath.Join(root, "go.mod")); statErr == nil {
			break
		}
		parent := filepath.Dir(root)
		if parent == root {
			panic("go.mod not found from " + wd)
		}
		root = parent
	}
	if err := os.Chdir(root); err != nil {
		panic(err)
	}
	os.Exit(m.Run())
}

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

func TestWebhookURLFromData(t *testing.T) {
	if got := webhookURLFromData([]byte(`{"webhook_url":"https://hooks.example/a"}`)); got != "https://hooks.example/a" {
		t.Fatalf("url = %q", got)
	}
	if got := webhookURLFromData([]byte(`{}`)); got != "" {
		t.Fatalf("empty data url = %q", got)
	}
}

func TestSendWebhookNotification_MissingURLTriesToRecordFailure(t *testing.T) {
	np := NewNotificationProcessor(unreachableDB(t), &stubEmailSender{}, log.New(io.Discard, "", 0), "")
	err := np.sendWebhookNotification("n1", "slack", "Nightly backup", "missed", time.Now().UTC(), []byte(`{}`))
	if err == nil || !strings.Contains(err.Error(), "marking notification as failed") {
		t.Fatalf("expected recorded failure, got %v", err)
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

func TestNotificationStatusPersists(t *testing.T) {
	database := openNotifyDatabase(t)
	user, err := database.CreateUser("notify-status@example.com", "Notify", "notify-pass")
	if err != nil {
		t.Fatal(err)
	}
	job := &models.Job{
		Name:       "Nightly backup",
		Schedule:   "0 0 * * *",
		GraceTime:  5,
		Status:     models.StatusHealthy,
		LastPing:   time.Now().UTC(),
		NextExpect: time.Now().UTC().Add(time.Hour),
		UserID:     user.ID,
	}
	if err := database.CreateJob(job); err != nil {
		t.Fatal(err)
	}

	dryID := insertPendingEmail(t, database, user.ID, job.ID)
	sender := &stubEmailSender{err: integrations.ErrEmailDryRun}
	np := NewNotificationProcessor(database.GetDB(), sender, log.New(io.Discard, "", 0), "")
	if err := np.processNotifications(); err != nil {
		t.Fatal(err)
	}
	if sender.calls != 1 {
		t.Fatalf("dry-run sends = %d, want 1", sender.calls)
	}
	status, sentAt, data := notificationRow(t, database, dryID)
	if status != "skipped" || sentAt.Valid || data != "{}" {
		t.Fatalf("dry-run row = %s sent=%v data=%s", status, sentAt.Valid, data)
	}
	if err := np.processNotifications(); err != nil {
		t.Fatal(err)
	}
	if sender.calls != 1 {
		t.Fatalf("dry-run retried, sends = %d", sender.calls)
	}

	sentID := insertPendingEmail(t, database, user.ID, job.ID)
	sentSender := &stubEmailSender{}
	sent := NewNotificationProcessor(database.GetDB(), sentSender, log.New(io.Discard, "", 0), "")
	if err := sent.sendEmailNotification(sentID, user.Email, job.Name, "missed", time.Now().UTC()); err != nil {
		t.Fatal(err)
	}
	status, sentAt, data = notificationRow(t, database, sentID)
	if status != "sent" || !sentAt.Valid || data != "{}" {
		t.Fatalf("sent row = %s sent=%v data=%s", status, sentAt.Valid, data)
	}

	failedID := insertPendingEmail(t, database, user.ID, job.ID)
	failedSender := &stubEmailSender{err: errors.New("smtp down")}
	failed := NewNotificationProcessor(database.GetDB(), failedSender, log.New(io.Discard, "", 0), "")
	if err := failed.sendEmailNotification(failedID, user.Email, job.Name, "missed", time.Now().UTC()); err == nil || !strings.Contains(err.Error(), "smtp down") {
		t.Fatalf("failed send = %v", err)
	}
	status, sentAt, data = notificationRow(t, database, failedID)
	if status != "failed" || sentAt.Valid || !strings.Contains(data, "smtp down") {
		t.Fatalf("failed row = %s sent=%v data=%s", status, sentAt.Valid, data)
	}
}

func insertPendingEmail(t *testing.T, database *db.Database, userID, jobID string) string {
	t.Helper()
	id := uuid.New().String()
	_, err := database.GetDB().Exec(`
		INSERT INTO notifications (id, user_id, job_id, message, type, status, data, created_at)
		VALUES ($1, $2, $3, 'Job missed', 'email', 'pending', '{}'::jsonb, NOW())
	`, id, userID, jobID)
	if err != nil {
		t.Fatal(err)
	}
	return id
}

func notificationRow(t *testing.T, database *db.Database, id string) (string, sql.NullTime, string) {
	t.Helper()
	var status string
	var sentAt sql.NullTime
	var data string
	err := database.GetDB().QueryRow(`SELECT status, sent_at, data::text FROM notifications WHERE id = $1`, id).Scan(&status, &sentAt, &data)
	if err != nil {
		t.Fatal(err)
	}
	return status, sentAt, data
}

func openNotifyDatabase(t *testing.T) *db.Database {
	t.Helper()
	t.Setenv("DB_NAME", "cronsentry_notify_test")
	host := envOr("DB_HOST", "localhost")
	port := envOr("DB_PORT", "5432")
	user := envOr("DB_USER", "postgres")
	password := envOr("DB_PASSWORD", "postgres")
	sslmode := envOr("DB_SSLMODE", "disable")
	admin, err := sql.Open("postgres", fmt.Sprintf("host=%s port=%s user=%s password=%s dbname=postgres sslmode=%s", host, port, user, password, sslmode))
	if err != nil {
		t.Fatal(err)
	}
	defer admin.Close()
	if err := admin.Ping(); err != nil {
		t.Fatalf("postgres is required: %v", err)
	}
	if _, err := admin.Exec(`CREATE DATABASE cronsentry_notify_test`); err != nil && !strings.Contains(err.Error(), "already exists") {
		t.Fatal(err)
	}
	database, err := db.NewDatabase()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { database.Close() })
	if err := database.InitDatabase(); err != nil {
		t.Fatal(err)
	}
	if _, err := database.GetDB().Exec(`TRUNCATE TABLE users RESTART IDENTITY CASCADE`); err != nil {
		t.Fatal(err)
	}
	return database
}

func envOr(key, fallback string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return fallback
}
