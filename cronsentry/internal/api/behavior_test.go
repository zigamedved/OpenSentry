package api

import (
	"bytes"
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	_ "github.com/lib/pq"
	"github.com/zigamedved/OpenSentry/internal/db"
	"github.com/zigamedved/OpenSentry/internal/models"
)

// These tests pin the job API as it behaves today. They talk to Postgres and
// fail when a status code, JSON field, or error string changes.

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

func TestJobLifecycleMatchesCurrentBehavior(t *testing.T) {
	srv := newBehaviorServer(t)

	empty := do(t, srv, http.MethodGet, "/api/jobs", "")
	if empty.StatusCode != http.StatusOK {
		t.Fatalf("list status = %d", empty.StatusCode)
	}
	if strings.TrimSpace(empty.Body) != "[]" {
		t.Fatalf("empty list = %q, want []", empty.Body)
	}
	if empty.Header.Get("Access-Control-Allow-Origin") != "*" {
		t.Fatalf("cors = %q", empty.Header.Get("Access-Control-Allow-Origin"))
	}

	created := do(t, srv, http.MethodPost, "/api/jobs", `{
		"name": "Nightly backup",
		"description": "Daily database backup",
		"schedule": "0 0 * * *",
		"grace_time": 15
	}`)
	if created.StatusCode != http.StatusCreated {
		t.Fatalf("create status = %d body %s", created.StatusCode, created.Body)
	}
	job := decodeJob(t, created.Body)
	if job.ID == "" || job.Name != "Nightly backup" || job.Description != "Daily database backup" {
		t.Fatalf("created job = %+v", job)
	}
	if job.Schedule != "0 0 * * *" || job.GraceTime != 15 || job.Status != models.StatusHealthy {
		t.Fatalf("schedule/status = %s %d %s", job.Schedule, job.GraceTime, job.Status)
	}
	if job.UserID != "test-user" {
		t.Fatalf("user_id = %q", job.UserID)
	}
	if job.NextExpect.Before(time.Now().UTC().Add(-2 * time.Second)) {
		t.Fatalf("next_expect = %s", job.NextExpect)
	}
	if job.LastPing.IsZero() {
		t.Fatal("last_ping is zero")
	}

	listed := do(t, srv, http.MethodGet, "/api/jobs", "")
	jobs := decodeJobs(t, listed.Body)
	if len(jobs) != 1 || jobs[0].ID != job.ID {
		t.Fatalf("list = %+v", jobs)
	}

	got := do(t, srv, http.MethodGet, "/api/jobs/"+job.ID, "")
	if got.StatusCode != http.StatusOK {
		t.Fatalf("get status = %d", got.StatusCode)
	}
	if decodeJob(t, got.Body).ID != job.ID {
		t.Fatal("get returned a different job")
	}

	pinged := do(t, srv, http.MethodPost, "/api/ping/"+job.ID, "")
	if pinged.StatusCode != http.StatusOK || pinged.Body != `{"status":"ok"}` {
		t.Fatalf("ping = %d %q", pinged.StatusCode, pinged.Body)
	}
	afterPing := decodeJob(t, do(t, srv, http.MethodGet, "/api/jobs/"+job.ID, "").Body)
	if afterPing.Status != models.StatusHealthy {
		t.Fatalf("status after ping = %s", afterPing.Status)
	}
	if afterPing.LastPing.Before(job.LastPing) {
		t.Fatalf("last_ping moved backwards: %s -> %s", job.LastPing, afterPing.LastPing)
	}
	if afterPing.NextExpect.IsZero() {
		t.Fatal("next_expect cleared by ping")
	}

	updated := do(t, srv, http.MethodPut, "/api/jobs/"+job.ID, `{"schedule":"0 12 * * *"}`)
	if updated.StatusCode != http.StatusOK {
		t.Fatalf("update status = %d body %s", updated.StatusCode, updated.Body)
	}
	afterUpdate := decodeJob(t, updated.Body)
	if afterUpdate.Schedule != "0 12 * * *" {
		t.Fatalf("schedule = %q", afterUpdate.Schedule)
	}
	if afterUpdate.NextExpect.Equal(afterPing.NextExpect) {
		t.Fatal("schedule change left next_expect unchanged")
	}
	if !afterUpdate.LastPing.Equal(afterPing.LastPing) {
		t.Fatalf("schedule change reset last_ping: %s -> %s", afterPing.LastPing, afterUpdate.LastPing)
	}
	if afterUpdate.Name != "Nightly backup" || afterUpdate.GraceTime != 15 {
		t.Fatalf("update changed unrelated fields: %+v", afterUpdate)
	}

	deleted := do(t, srv, http.MethodDelete, "/api/jobs/"+job.ID, "")
	if deleted.StatusCode != http.StatusOK {
		t.Fatalf("delete status = %d", deleted.StatusCode)
	}
	missing := do(t, srv, http.MethodGet, "/api/jobs/"+job.ID, "")
	if missing.StatusCode != http.StatusNotFound || strings.TrimSpace(missing.Body) != "Job not found" {
		t.Fatalf("get after delete = %d %q", missing.StatusCode, missing.Body)
	}
}

func TestJobAPIRejectsInvalidInput(t *testing.T) {
	srv := newBehaviorServer(t)

	cases := []struct {
		name   string
		method string
		path   string
		body   string
		status int
		want   string
	}{
		{"invalid json", http.MethodPost, "/api/jobs", "{", http.StatusBadRequest, "Invalid request body"},
		{"missing name", http.MethodPost, "/api/jobs", `{"schedule":"0 0 * * *"}`, http.StatusBadRequest, "Name and schedule are required"},
		{"invalid cron", http.MethodPost, "/api/jobs", `{"name":"x","schedule":"not a cron"}`, http.StatusBadRequest, "Invalid CRON schedule provided"},
		{"unknown job", http.MethodGet, "/api/jobs/does-not-exist", "", http.StatusNotFound, "Job not found"},
		{"unknown ping", http.MethodPost, "/api/ping/does-not-exist", "", http.StatusNotFound, "Job not found"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			resp := do(t, srv, tc.method, tc.path, tc.body)
			if resp.StatusCode != tc.status || strings.TrimSpace(resp.Body) != tc.want {
				t.Fatalf("got %d %q, want %d %q", resp.StatusCode, resp.Body, tc.status, tc.want)
			}
		})
	}

	created := decodeJob(t, do(t, srv, http.MethodPost, "/api/jobs", `{"name":"x","schedule":"0 0 * * *","grace_time":5}`).Body)
	badUpdate := do(t, srv, http.MethodPut, "/api/jobs/"+created.ID, `{"schedule":"not a cron"}`)
	if badUpdate.StatusCode != http.StatusBadRequest || strings.TrimSpace(badUpdate.Body) != "Invalid CRON schedule provided" {
		t.Fatalf("bad update = %d %q", badUpdate.StatusCode, badUpdate.Body)
	}
	unchanged := decodeJob(t, do(t, srv, http.MethodGet, "/api/jobs/"+created.ID, "").Body)
	if unchanged.Schedule != "0 0 * * *" {
		t.Fatalf("invalid update changed schedule to %q", unchanged.Schedule)
	}
}

func TestPreflightAllowsBrowserCalls(t *testing.T) {
	srv := newBehaviorServer(t)
	resp := do(t, srv, http.MethodOptions, "/api/jobs", "")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("options status = %d", resp.StatusCode)
	}
	if resp.Header.Get("Access-Control-Allow-Origin") != "*" {
		t.Fatalf("origin = %q", resp.Header.Get("Access-Control-Allow-Origin"))
	}
	if !strings.Contains(resp.Header.Get("Access-Control-Allow-Methods"), "POST") {
		t.Fatalf("methods = %q", resp.Header.Get("Access-Control-Allow-Methods"))
	}
}

type recorded struct {
	StatusCode int
	Header     http.Header
	Body       string
}

func newBehaviorServer(t *testing.T) *httptest.Server {
	t.Helper()
	t.Setenv("DB_NAME", "cronsentry_test")
	ensureTestDatabase(t)

	database, err := db.NewDatabase()
	if err != nil {
		t.Fatalf("connect test database: %v", err)
	}
	t.Cleanup(func() { database.Close() })
	if err := database.InitDatabase(); err != nil {
		t.Fatalf("init schema: %v", err)
	}
	if _, err := database.GetDB().Exec(`TRUNCATE TABLE jobs RESTART IDENTITY CASCADE`); err != nil {
		t.Fatalf("truncate jobs: %v", err)
	}

	srv := httptest.NewServer(NewServer(database, log.New(io.Discard, "", 0), behaviorAPIToken).Router())
	t.Cleanup(srv.Close)
	return srv
}

func ensureTestDatabase(t *testing.T) {
	t.Helper()
	host := envOr("DB_HOST", "localhost")
	port := envOr("DB_PORT", "5432")
	user := envOr("DB_USER", "postgres")
	password := envOr("DB_PASSWORD", "postgres")
	sslmode := envOr("DB_SSLMODE", "disable")
	admin, err := sql.Open("postgres", fmt.Sprintf("host=%s port=%s user=%s password=%s dbname=postgres sslmode=%s", host, port, user, password, sslmode))
	if err != nil {
		t.Fatalf("open admin db: %v", err)
	}
	defer admin.Close()
	if err := admin.Ping(); err != nil {
		t.Fatalf("postgres is required for behavior tests: %v", err)
	}
	if _, err := admin.Exec(`CREATE DATABASE cronsentry_test`); err != nil && !strings.Contains(err.Error(), "already exists") {
		t.Fatalf("create test database: %v", err)
	}
}

func do(t *testing.T, srv *httptest.Server, method, path, body string) recorded {
	t.Helper()
	req, err := http.NewRequest(method, srv.URL+path, bytes.NewBufferString(body))
	if err != nil {
		t.Fatal(err)
	}
	if body != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	req.Header.Set("Authorization", "Bearer "+behaviorAPIToken)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	payload, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatal(err)
	}
	return recorded{StatusCode: resp.StatusCode, Header: resp.Header.Clone(), Body: string(payload)}
}

func decodeJob(t *testing.T, body string) models.Job {
	t.Helper()
	var job models.Job
	if err := json.Unmarshal([]byte(body), &job); err != nil {
		t.Fatalf("decode job %q: %v", body, err)
	}
	return job
}

func decodeJobs(t *testing.T, body string) []models.Job {
	t.Helper()
	var jobs []models.Job
	if err := json.Unmarshal([]byte(body), &jobs); err != nil {
		t.Fatalf("decode jobs %q: %v", body, err)
	}
	return jobs
}

const behaviorAPIToken = "behavior-test-token"

func envOr(key, fallback string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return fallback
}
