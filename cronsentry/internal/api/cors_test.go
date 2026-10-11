package api

import (
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestCORSRejectsWildcardAndUnknownOrigins(t *testing.T) {
	t.Setenv("CORS_ORIGINS", "*")
	handler := NewServer(nil, log.New(io.Discard, "", 0)).Router()

	for _, origin := range []string{"https://evil.example", "https://monitor.example.com"} {
		rr := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodOptions, "/api/jobs", nil)
		req.Header.Set("Origin", origin)
		handler.ServeHTTP(rr, req)
		if rr.Code != http.StatusNoContent {
			t.Fatalf("preflight %s = %d", origin, rr.Code)
		}
		if got := rr.Header().Get("Access-Control-Allow-Origin"); got != "" {
			t.Fatalf("ACAO for %s = %q", origin, got)
		}
		if rr.Header().Get("Access-Control-Allow-Credentials") != "" {
			t.Fatal("credentials allowed without an origin match")
		}
	}
}

func TestCORSEchoesAllowlistedOrigin(t *testing.T) {
	t.Setenv("CORS_ORIGINS", "https://monitor.example.com, http://localhost:3000/")
	handler := NewServer(nil, log.New(io.Discard, "", 0)).Router()

	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/me", nil)
	req.Header.Set("Origin", "https://monitor.example.com")
	handler.ServeHTTP(rr, req)
	if rr.Code != http.StatusUnauthorized {
		t.Fatalf("me = %d", rr.Code)
	}
	if got := rr.Header().Get("Access-Control-Allow-Origin"); got != "https://monitor.example.com" {
		t.Fatalf("ACAO = %q", got)
	}
	if rr.Header().Get("Access-Control-Allow-Credentials") != "true" {
		t.Fatal("allowlisted origin cannot send cookies")
	}
	if rr.Header().Get("Vary") != "Origin" {
		t.Fatalf("Vary = %q", rr.Header().Get("Vary"))
	}

	local := httptest.NewRecorder()
	localReq := httptest.NewRequest(http.MethodOptions, "/api/jobs", nil)
	localReq.Header.Set("Origin", "http://localhost:3000")
	handler.ServeHTTP(local, localReq)
	if local.Header().Get("Access-Control-Allow-Origin") != "http://localhost:3000" {
		t.Fatalf("localhost ACAO = %q", local.Header().Get("Access-Control-Allow-Origin"))
	}
	if local.Header().Get("Access-Control-Allow-Headers") != "Content-Type, Authorization" {
		t.Fatalf("headers = %q", local.Header().Get("Access-Control-Allow-Headers"))
	}

	other := httptest.NewRecorder()
	otherReq := httptest.NewRequest(http.MethodPost, "/api/ping/not-a-job", nil)
	otherReq.Header.Set("Origin", "https://evil.example")
	handler.ServeHTTP(other, otherReq)
	if other.Code == http.StatusForbidden {
		t.Fatal("ping rejected for a browser origin")
	}
	if got := other.Header().Get("Access-Control-Allow-Origin"); got != "" {
		t.Fatalf("ping ACAO = %q", got)
	}
}

func TestPingHasNoWildcardCORS(t *testing.T) {
	t.Setenv("CORS_ORIGINS", "")
	handler := NewServer(nil, log.New(io.Discard, "", 0)).Router()
	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/ping/token", nil)
	handler.ServeHTTP(rr, req)
	if got := rr.Header().Get("Access-Control-Allow-Origin"); got != "" {
		t.Fatalf("ACAO = %q", got)
	}
}
