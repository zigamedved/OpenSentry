package api

import (
	"errors"
	"net/http"
	"testing"
	"time"

	"github.com/zigamedved/cronsentry/internal/db"
	"github.com/zigamedved/cronsentry/internal/models"
)

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
