package api

import (
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestClientIPUsesLeftmostForwardedAddress(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/api/ping/token", nil)
	req.RemoteAddr = "10.0.0.8:4000"
	req.Header.Set("X-Forwarded-For", " 203.0.113.9, 10.0.0.8 ")
	if got := clientIP(req); got != "203.0.113.9" {
		t.Fatalf("client IP = %q", got)
	}

	direct := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	direct.RemoteAddr = "192.0.2.4:1234"
	if got := clientIP(direct); got != "192.0.2.4" {
		t.Fatalf("remote IP = %q", got)
	}
}

func TestHealthzAndOptionsAreNotLimited(t *testing.T) {
	restore := setLimits(t, 1, 1, 1, 1)
	defer restore()

	handler := NewServer(nil, log.New(io.Discard, "", 0)).Router()
	for i := 0; i < 5; i++ {
		health := httptest.NewRecorder()
		handler.ServeHTTP(health, httptest.NewRequest(http.MethodGet, "/healthz", nil))
		if health.Code == http.StatusTooManyRequests {
			t.Fatal("healthz was rate limited")
		}
		if health.Code != http.StatusServiceUnavailable {
			t.Fatalf("healthz = %d", health.Code)
		}

		options := httptest.NewRecorder()
		handler.ServeHTTP(options, httptest.NewRequest(http.MethodOptions, "/api/jobs", nil))
		if options.Code != http.StatusNoContent {
			t.Fatalf("options = %d", options.Code)
		}
	}
}

func TestPingIPLimitDoesNotConsumeJobBudget(t *testing.T) {
	restore := setLimits(t, 1, 2, 120, 20)
	defer restore()

	handler := NewServer(nil, log.New(io.Discard, "", 0)).Router()
	post := func(ip string) int {
		req := httptest.NewRequest(http.MethodPost, "/api/ping/token-a", nil)
		req.Header.Set("X-Forwarded-For", ip)
		rr := httptest.NewRecorder()
		handler.ServeHTTP(rr, req)
		return rr.Code
	}

	if post("203.0.113.1") == http.StatusTooManyRequests {
		t.Fatal("first ping was limited")
	}
	blocked := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/ping/token-a", nil)
	req.Header.Set("X-Forwarded-For", "203.0.113.1")
	handler.ServeHTTP(blocked, req)
	if blocked.Code != http.StatusTooManyRequests {
		t.Fatalf("second ping from same IP = %d", blocked.Code)
	}
	if strings.TrimSpace(blocked.Body.String()) != "Too Many Requests" {
		t.Fatalf("body = %q", blocked.Body.String())
	}
	if blocked.Header().Get("Retry-After") != "60" {
		t.Fatalf("Retry-After = %q", blocked.Header().Get("Retry-After"))
	}
	if post("203.0.113.2") == http.StatusTooManyRequests {
		t.Fatal("IP rejection also consumed the per-token budget")
	}
	if post("203.0.113.3") != http.StatusTooManyRequests {
		t.Fatal("third distinct IP should hit the per-token limit")
	}
}

func TestSlidingWindowExpires(t *testing.T) {
	limiter := newSlidingLimiter(1, time.Minute)
	start := time.Date(2026, 10, 9, 12, 0, 0, 0, time.UTC)
	if !limiter.allow("k", start) {
		t.Fatal("first attempt rejected")
	}
	if limiter.allow("k", start.Add(30*time.Second)) {
		t.Fatal("second attempt inside the window was allowed")
	}
	if !limiter.allow("k", start.Add(time.Minute+time.Second)) {
		t.Fatal("attempt after the window was rejected")
	}
}

func setLimits(t *testing.T, pingIP, pingJob, management, auth int) func() {
	t.Helper()
	oldIP, oldJob := pingPerIPLimit, pingPerJobLimit
	oldMgmt, oldAuth := managementPerIPLimit, authPerIPLimit
	pingPerIPLimit = pingIP
	pingPerJobLimit = pingJob
	managementPerIPLimit = management
	authPerIPLimit = auth
	return func() {
		pingPerIPLimit = oldIP
		pingPerJobLimit = oldJob
		managementPerIPLimit = oldMgmt
		authPerIPLimit = oldAuth
	}
}
