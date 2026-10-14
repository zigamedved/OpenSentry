package api

import (
	"io"
	"net/http"
	"strings"
)

// maxPingBody is the most output stored from one ping. Extra bytes are dropped.
const maxPingBody = 64 * 1024

func (s *Server) recordPing(w http.ResponseWriter, r *http.Request, kind string) {
	token := r.PathValue("token")
	if token == "" {
		http.Error(w, "Ping token is required", http.StatusBadRequest)
		return
	}

	body, truncated, err := readPingBody(r)
	if err != nil {
		http.Error(w, "Failed to read ping body", http.StatusBadRequest)
		return
	}

	if err := s.db.RecordPingSignal(token, kind, body, truncated); err != nil {
		status := pingHTTPStatus(err)
		if status == http.StatusNotFound {
			http.Error(w, "Job not found", http.StatusNotFound)
			return
		}
		s.logger.Printf("Error recording ping: %v", err)
		http.Error(w, "Failed to record ping", http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusOK)
	w.Write([]byte(`{"status":"ok"}`))
}

// readPingBody keeps at most maxPingBody bytes. A longer body is truncated.
// Invalid UTF-8 bytes are replaced so the log can be stored as text.
func readPingBody(r *http.Request) (string, bool, error) {
	raw, err := io.ReadAll(io.LimitReader(r.Body, maxPingBody+1))
	if err != nil {
		return "", false, err
	}
	truncated := false
	if len(raw) > maxPingBody {
		raw = raw[:maxPingBody]
		truncated = true
	}
	return strings.ToValidUTF8(string(raw), "\uFFFD"), truncated, nil
}
