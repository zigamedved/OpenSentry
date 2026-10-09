package api

import (
	"net"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"
)

// Defaults are per API process. They reset on restart and are not shared
// across replicas. Tests lower these before NewServer.
var (
	pingPerIPLimit       = 60
	pingPerJobLimit      = 30
	managementPerIPLimit = 120
	authPerIPLimit       = 20
	rateLimitWindow      = time.Minute
)

type slidingLimiter struct {
	mu     sync.Mutex
	hits   map[string][]time.Time
	limit  int
	window time.Duration
}

func newSlidingLimiter(limit int, window time.Duration) *slidingLimiter {
	return &slidingLimiter{
		hits:   make(map[string][]time.Time),
		limit:  limit,
		window: window,
	}
}

// allow records one attempt and returns false when key is already at the limit.
// A rejected attempt is not stored again, so a flood cannot grow the slice.
func (l *slidingLimiter) allow(key string, now time.Time) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.limit <= 0 {
		return false
	}
	cutoff := now.Add(-l.window)
	if len(l.hits) > 8192 {
		for existing, times := range l.hits {
			if len(times) == 0 || !times[len(times)-1].After(cutoff) {
				delete(l.hits, existing)
			}
		}
	}
	prev := l.hits[key]
	kept := make([]time.Time, 0, len(prev)+1)
	for _, hit := range prev {
		if hit.After(cutoff) {
			kept = append(kept, hit)
		}
	}
	if len(kept) >= l.limit {
		l.hits[key] = kept
		return false
	}
	l.hits[key] = append(kept, now)
	return true
}

func clientIP(r *http.Request) string {
	if fwd := r.Header.Get("X-Forwarded-For"); fwd != "" {
		first := strings.TrimSpace(strings.Split(fwd, ",")[0])
		if first != "" {
			return first
		}
	}
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}

func (s *Server) rateLimitMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodOptions || r.URL.Path == "/healthz" {
			next.ServeHTTP(w, r)
			return
		}
		path := r.URL.Path
		ip := clientIP(r)
		switch {
		case strings.HasPrefix(path, "/api/ping/"):
			if !s.pingIP.allow(ip, time.Now()) {
				writeTooMany(w, s.rateWindow)
				return
			}
			token := strings.TrimPrefix(path, "/api/ping/")
			if !s.pingJob.allow(token, time.Now()) {
				writeTooMany(w, s.rateWindow)
				return
			}
		case r.Method == http.MethodPost && (path == "/api/login" || path == "/api/register"):
			if !s.authIP.allow(ip, time.Now()) {
				writeTooMany(w, s.rateWindow)
				return
			}
		case strings.HasPrefix(path, "/api/"):
			if !s.managementIP.allow(ip, time.Now()) {
				writeTooMany(w, s.rateWindow)
				return
			}
		}
		next.ServeHTTP(w, r)
	})
}

func writeTooMany(w http.ResponseWriter, window time.Duration) {
	seconds := int(window / time.Second)
	if seconds < 1 {
		seconds = 1
	}
	w.Header().Set("Retry-After", strconv.Itoa(seconds))
	http.Error(w, "Too Many Requests", http.StatusTooManyRequests)
}
