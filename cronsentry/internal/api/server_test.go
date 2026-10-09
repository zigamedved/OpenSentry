package api

import (
	"errors"
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/zigamedved/OpenSentry/internal/db"
	"github.com/zigamedved/OpenSentry/internal/models"
)

func TestManagementAuth(t *testing.T) {
	s := &Server{logger: log.New(io.Discard, "", 0)}
	ok := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	})
	handler := s.authMiddleware(ok)

	t.Run("missing token is 401", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/jobs", nil)
		rr := httptest.NewRecorder()
		handler.ServeHTTP(rr, req)
		if rr.Code != http.StatusUnauthorized {
			t.Fatalf("status = %d, want 401", rr.Code)
		}
	})

	t.Run("unknown bearer token is 401", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, "/api/jobs", nil)
		req.Header.Set("Authorization", "Bearer other")
		rr := httptest.NewRecorder()
		handler.ServeHTTP(rr, req)
		if rr.Code != http.StatusUnauthorized {
			t.Fatalf("status = %d, want 401", rr.Code)
		}
	})

	t.Run("shared api token header is not a session", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/jobs", nil)
		req.Header.Set("X-API-Token", "secret-token")
		rr := httptest.NewRecorder()
		handler.ServeHTTP(rr, req)
		if rr.Code != http.StatusUnauthorized {
			t.Fatalf("status = %d, want 401", rr.Code)
		}
	})

	t.Run("ping stays public", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, "/api/ping/job-id", nil)
		rr := httptest.NewRecorder()
		handler.ServeHTTP(rr, req)
		if rr.Code != http.StatusNoContent {
			t.Fatalf("status = %d, want 204", rr.Code)
		}
	})

	t.Run("register login and logout stay public", func(t *testing.T) {
		for _, path := range []string{"/api/register", "/api/login", "/api/logout"} {
			req := httptest.NewRequest(http.MethodPost, path, nil)
			rr := httptest.NewRecorder()
			handler.ServeHTTP(rr, req)
			if rr.Code != http.StatusNoContent {
				t.Fatalf("%s status = %d, want 204", path, rr.Code)
			}
		}
	})

	t.Run("whoami requires a session", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/me", nil)
		rr := httptest.NewRecorder()
		handler.ServeHTTP(rr, req)
		if rr.Code != http.StatusUnauthorized {
			t.Fatalf("status = %d, want 401", rr.Code)
		}
	})

	t.Run("channels require auth", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPut, "/api/channels/slack", nil)
		rr := httptest.NewRecorder()
		handler.ServeHTTP(rr, req)
		if rr.Code != http.StatusUnauthorized {
			t.Fatalf("status = %d, want 401", rr.Code)
		}
	})

	t.Run("healthz stays public", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/healthz", nil)
		rr := httptest.NewRecorder()
		handler.ServeHTTP(rr, req)
		if rr.Code != http.StatusNoContent {
			t.Fatalf("status = %d, want 204", rr.Code)
		}
	})
}

func TestHealthzDatabaseDown(t *testing.T) {
	s := NewServer(nil, log.New(io.Discard, "", 0))
	req := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	rr := httptest.NewRecorder()
	s.handleHealth(rr, req)
	if rr.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want 503", rr.Code)
	}
	if strings.TrimSpace(rr.Body.String()) != "database unavailable" {
		t.Fatalf("body = %q", rr.Body.String())
	}
}

func TestPingHTTPStatus(t *testing.T) {
	if got := pingHTTPStatus(db.ErrJobNotFound); got != http.StatusNotFound {
		t.Fatalf("ErrJobNotFound -> %d, want 404", got)
	}
	if got := pingHTTPStatus(errors.New("db down")); got != http.StatusInternalServerError {
		t.Fatalf("other err -> %d, want 500", got)
	}
	if got := pingHTTPStatus(nil); got != http.StatusOK {
		t.Fatalf("nil -> %d, want 200", got)
	}
}

func TestApplyScheduleChange(t *testing.T) {
	job := &models.Job{
		Schedule:   "0 * * * *",
		NextExpect: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
		LastPing:   time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
		GraceTime:  10,
	}
	oldExpect := job.NextExpect
	oldPing := job.LastPing

	t.Run("invalid cron rejected", func(t *testing.T) {
		err := applyScheduleChange(job, "not a cron")
		if err == nil {
			t.Fatal("expected error for invalid cron")
		}
		if job.Schedule != "0 * * * *" {
			t.Fatalf("schedule should be unchanged on error, got %q", job.Schedule)
		}
	})

	t.Run("valid schedule recomputes next_expect", func(t *testing.T) {
		err := applyScheduleChange(job, "*/5 * * * *")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if job.Schedule != "*/5 * * * *" {
			t.Fatalf("schedule = %q", job.Schedule)
		}
		if job.NextExpect.Equal(oldExpect) {
			t.Fatal("next_expect should change when schedule changes")
		}
		if !job.LastPing.Equal(oldPing) {
			t.Fatal("last_ping must not reset when schedule changes")
		}
	})

	t.Run("same schedule leaves next_expect", func(t *testing.T) {
		before := job.NextExpect
		err := applyScheduleChange(job, job.Schedule)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !job.NextExpect.Equal(before) {
			t.Fatal("identical schedule should not recompute next_expect")
		}
	})
}

func TestApplyStatusChange(t *testing.T) {
	t.Run("resume from paused recomputes next_expect", func(t *testing.T) {
		job := &models.Job{
			Status:     models.StatusPaused,
			Schedule:   "0 0 * * *",
			NextExpect: time.Now().UTC().Add(-48 * time.Hour),
			LastPing:   time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
		}
		lastPing := job.LastPing
		if err := applyStatusChange(job, "healthy"); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if job.Status != models.StatusHealthy {
			t.Fatalf("status = %s", job.Status)
		}
		if job.NextExpect.Before(time.Now().UTC().Add(-2 * time.Second)) {
			t.Fatalf("resume left next_expect in the past: %s", job.NextExpect)
		}
		if !job.LastPing.Equal(lastPing) {
			t.Fatal("resume reset last_ping")
		}
	})

	t.Run("pause keeps next_expect", func(t *testing.T) {
		next := time.Now().UTC().Add(time.Hour)
		job := &models.Job{Status: models.StatusHealthy, Schedule: "0 0 * * *", NextExpect: next}
		if err := applyStatusChange(job, "paused"); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if job.Status != models.StatusPaused {
			t.Fatalf("status = %s", job.Status)
		}
		if !job.NextExpect.Equal(next) {
			t.Fatal("pause changed next_expect")
		}
	})

	t.Run("invalid status rejected", func(t *testing.T) {
		job := &models.Job{Status: models.StatusHealthy, Schedule: "0 0 * * *"}
		if err := applyStatusChange(job, "asleep"); err == nil {
			t.Fatal("expected error for invalid status")
		}
		if job.Status != models.StatusHealthy {
			t.Fatalf("status changed to %s", job.Status)
		}
	})

	t.Run("client cannot force missing", func(t *testing.T) {
		job := &models.Job{Status: models.StatusHealthy, Schedule: "0 0 * * *"}
		if err := applyStatusChange(job, string(models.StatusMissing)); err == nil {
			t.Fatal("expected error for missing status")
		}
		if job.Status != models.StatusHealthy {
			t.Fatalf("status = %s", job.Status)
		}
	})
}
