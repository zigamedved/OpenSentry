package api

import (
	"errors"
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/zigamedved/OpenSentry/internal/db"
	"github.com/zigamedved/OpenSentry/internal/models"
)

func TestManagementAuth(t *testing.T) {
	s := &Server{apiToken: "secret-token", logger: log.New(io.Discard, "", 0)}
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

	t.Run("wrong token is 401", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, "/api/jobs", nil)
		req.Header.Set("Authorization", "Bearer other")
		rr := httptest.NewRecorder()
		handler.ServeHTTP(rr, req)
		if rr.Code != http.StatusUnauthorized {
			t.Fatalf("status = %d, want 401", rr.Code)
		}
	})

	t.Run("bearer token allows management", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodDelete, "/api/jobs/abc", nil)
		req.Header.Set("Authorization", "Bearer secret-token")
		rr := httptest.NewRecorder()
		handler.ServeHTTP(rr, req)
		if rr.Code != http.StatusNoContent {
			t.Fatalf("status = %d, want 204", rr.Code)
		}
	})

	t.Run("x-api-token header allows management", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/jobs/abc", nil)
		req.Header.Set("X-API-Token", "secret-token")
		rr := httptest.NewRecorder()
		handler.ServeHTTP(rr, req)
		if rr.Code != http.StatusNoContent {
			t.Fatalf("status = %d, want 204", rr.Code)
		}
	})

	t.Run("unset server token rejects even a presented token", func(t *testing.T) {
		open := &Server{logger: log.New(io.Discard, "", 0)}
		req := httptest.NewRequest(http.MethodGet, "/api/jobs", nil)
		req.Header.Set("Authorization", "Bearer secret-token")
		rr := httptest.NewRecorder()
		open.authMiddleware(ok).ServeHTTP(rr, req)
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
