package api

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestReadPingBodyCapsAndReplacesInvalidUTF8(t *testing.T) {
	raw := append(bytes.Repeat([]byte("a"), maxPingBody+10), 0xff)
	req := httptest.NewRequest(http.MethodPost, "/api/ping/token", bytes.NewReader(raw))
	body, truncated, err := readPingBody(req)
	if err != nil {
		t.Fatal(err)
	}
	if !truncated {
		t.Fatal("expected truncation")
	}
	if body != strings.Repeat("a", maxPingBody) {
		t.Fatalf("body length = %d", len(body))
	}

	invalid := httptest.NewRequest(http.MethodPost, "/api/ping/token", bytes.NewReader([]byte{'h', 0xff, 'i'}))
	body, truncated, err = readPingBody(invalid)
	if err != nil {
		t.Fatal(err)
	}
	if truncated {
		t.Fatal("short body was truncated")
	}
	if body != "h\uFFFDi" {
		t.Fatalf("body = %q", body)
	}
}
