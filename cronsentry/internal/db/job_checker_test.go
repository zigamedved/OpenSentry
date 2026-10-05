package db

import (
	"testing"
	"time"
)

func TestPastGraceDeadline(t *testing.T) {
	nextExpect := time.Date(2026, 10, 5, 12, 0, 0, 0, time.UTC)
	graceMinutes := 5

	tests := []struct {
		name string
		now  time.Time
		want bool
	}{
		{
			name: "before next_expect is not past grace",
			now:  nextExpect.Add(-time.Minute),
			want: false,
		},
		{
			name: "at next_expect is not past grace",
			now:  nextExpect,
			want: false,
		},
		{
			name: "just before grace deadline is not missing",
			now:  nextExpect.Add(5*time.Minute - time.Nanosecond),
			want: false,
		},
		{
			name: "exactly at grace deadline is not missing",
			now:  nextExpect.Add(5 * time.Minute),
			want: false,
		},
		{
			name: "just after grace deadline is missing",
			now:  nextExpect.Add(5*time.Minute + time.Nanosecond),
			want: true,
		},
		{
			name: "well after grace deadline is missing",
			now:  nextExpect.Add(15 * time.Minute),
			want: true,
		},
		{
			name: "zero grace: just after next_expect is missing",
			now:  nextExpect.Add(time.Nanosecond),
			want: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			grace := graceMinutes
			if tt.name == "zero grace: just after next_expect is missing" {
				grace = 0
			}
			got := PastGraceDeadline(nextExpect, grace, tt.now)
			if got != tt.want {
				t.Fatalf("PastGraceDeadline(...) = %v, want %v (now=%s)", got, tt.want, tt.now)
			}
		})
	}
}

func TestPastGraceDeadline_UsesMinutesNotNanoseconds(t *testing.T) {
	// Regression for the bug that treated grace_time as nanoseconds via
	// time.Duration(grace).Minutes(), which made grace effectively zero.
	nextExpect := time.Date(2026, 10, 5, 12, 0, 0, 0, time.UTC)
	graceMinutes := 15

	if PastGraceDeadline(nextExpect, graceMinutes, nextExpect.Add(14*time.Minute)) {
		t.Fatal("14 minutes after expect with 15m grace should not be past deadline")
	}
	if !PastGraceDeadline(nextExpect, graceMinutes, nextExpect.Add(15*time.Minute+time.Second)) {
		t.Fatal("15m+1s after expect with 15m grace should be past deadline")
	}
}
