package api

import (
	"log"
	"os"
	"strings"
)

// allowedOriginsFromEnv reads CORS_ORIGINS. An empty list means the API does
// not answer cross-origin browser reads. Same-origin dashboards do not need
// an entry. Ping clients that send no Origin header are unaffected.
func allowedOriginsFromEnv(logger *log.Logger) map[string]struct{} {
	raw := os.Getenv("CORS_ORIGINS")
	allowed := make(map[string]struct{})
	for _, part := range strings.Split(raw, ",") {
		origin := strings.TrimSpace(part)
		origin = strings.TrimRight(origin, "/")
		if origin == "" {
			continue
		}
		if strings.Contains(origin, "*") || !strings.Contains(origin, "://") {
			if logger != nil {
				logger.Printf("CORS_ORIGINS entry %q ignored; list exact origins such as https://monitor.example.com", origin)
			}
			continue
		}
		allowed[origin] = struct{}{}
	}
	return allowed
}

func (s *Server) originAllowed(origin string) bool {
	if origin == "" || s.allowedOrigins == nil {
		return false
	}
	_, ok := s.allowedOrigins[origin]
	return ok
}
