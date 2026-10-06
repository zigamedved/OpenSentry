package db

import (
	"errors"
	"testing"

	"github.com/zigamedved/cronsentry/internal/models"
)

func TestRequireJob(t *testing.T) {
	t.Run("nil job becomes ErrJobNotFound", func(t *testing.T) {
		job, err := requireJob(nil, nil)
		if job != nil {
			t.Fatal("expected nil job")
		}
		if !errors.Is(err, ErrJobNotFound) {
			t.Fatalf("got %v, want ErrJobNotFound", err)
		}
	})

	t.Run("propagates query errors", func(t *testing.T) {
		want := errors.New("connection refused")
		_, err := requireJob(nil, want)
		if !errors.Is(err, want) {
			t.Fatalf("got %v, want %v", err, want)
		}
	})

	t.Run("returns existing job", func(t *testing.T) {
		in := &models.Job{ID: "abc", Schedule: "* * * * *"}
		got, err := requireJob(in, nil)
		if err != nil {
			t.Fatalf("unexpected err: %v", err)
		}
		if got != in {
			t.Fatal("expected same job pointer")
		}
	})
}
