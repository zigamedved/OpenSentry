package integrations

import (
	"errors"
	"testing"
)

func TestParseEmailFrom(t *testing.T) {
	name, email := ParseEmailFrom("")
	if name != "OpenSentry" || email != "noreply@localhost" {
		t.Fatalf("empty = %q %q", name, email)
	}
	name, email = ParseEmailFrom("Alerts <ops@example.com>")
	if name != "Alerts" || email != "ops@example.com" {
		t.Fatalf("named = %q %q", name, email)
	}
	name, email = ParseEmailFrom("ops@example.com")
	if name != "OpenSentry" || email != "ops@example.com" {
		t.Fatalf("bare = %q %q", name, email)
	}
}

func TestSendResultError(t *testing.T) {
	tests := []struct {
		name       string
		err        error
		statusCode int
		wantErr    bool
	}{
		{name: "nil err and 202 accepted", err: nil, statusCode: 202, wantErr: false},
		{name: "nil err and 200 ok", err: nil, statusCode: 200, wantErr: false},
		{name: "network error is returned", err: errors.New("dial tcp: timeout"), statusCode: 0, wantErr: true},
		{name: "401 is failure", err: nil, statusCode: 401, wantErr: true},
		{name: "500 is failure", err: nil, statusCode: 500, wantErr: true},
		{name: "0 status without err is failure", err: nil, statusCode: 0, wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := SendResultError(tt.err, tt.statusCode)
			if (got != nil) != tt.wantErr {
				t.Fatalf("SendResultError(...) = %v, wantErr=%v", got, tt.wantErr)
			}
		})
	}
}
