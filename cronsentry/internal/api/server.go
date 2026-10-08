package api

import (
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/adhocore/gronx"
	"github.com/google/uuid"
	"github.com/zigamedved/OpenSentry/internal/db"
	"github.com/zigamedved/OpenSentry/internal/models"
	"github.com/zigamedved/OpenSentry/internal/notifications/integrations"
)

type Server struct {
	db     *db.Database
	logger *log.Logger
}

func NewServer(database *db.Database, logger *log.Logger) *Server {
	return &Server{
		db:     database,
		logger: logger,
	}
}

func (s *Server) Router() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", s.handleHealth)
	mux.HandleFunc("POST /api/register", s.handleRegister)
	mux.HandleFunc("POST /api/login", s.handleLogin)
	mux.HandleFunc("POST /api/logout", s.handleLogout)
	mux.HandleFunc("GET /api/me", s.handleMe)
	mux.HandleFunc("POST /api/jobs", s.handleCreateJob)
	mux.HandleFunc("GET /api/jobs", s.handleListJobs)
	mux.HandleFunc("GET /api/jobs/{id}", s.handleGetJob)
	mux.HandleFunc("GET /api/jobs/{id}/events", s.handleListEvents)
	mux.HandleFunc("PUT /api/jobs/{id}", s.handleUpdateJob)
	mux.HandleFunc("DELETE /api/jobs/{id}", s.handleDeleteJob)
	mux.HandleFunc("POST /api/ping/{id}", s.handlePing)
	mux.HandleFunc("GET /api/channels", s.handleListChannels)
	mux.HandleFunc("PUT /api/channels/{kind}", s.handlePutChannel)
	return s.corsMiddleware(s.loggingMiddleware(s.recoveryMiddleware(s.authMiddleware(mux))))
}

// publicPath is reachable without a session. Ping URLs, health, account
// creation, and logout stay public. Logout must run even when the session is
// already expired so the HttpOnly cookie can be cleared. Everything else
// under /api/ needs a session.
func publicPath(method, path string) bool {
	if method == http.MethodOptions {
		return true
	}
	if path == "/healthz" || strings.HasPrefix(path, "/api/ping/") {
		return true
	}
	if method == http.MethodPost && (path == "/api/register" || path == "/api/login" || path == "/api/logout") {
		return true
	}
	return false
}

func (s *Server) handleListChannels(w http.ResponseWriter, r *http.Request) {
	channels, err := s.db.ListAlertChannels(currentUserID(r))
	if err != nil {
		s.logger.Printf("Error listing alert channels: %v", err)
		http.Error(w, "Failed to list alert channels", http.StatusInternalServerError)
		return
	}
	response := map[string]string{"slack": "", "discord": ""}
	for _, channel := range channels {
		if channel.Kind == "slack" || channel.Kind == "discord" {
			response[channel.Kind] = channel.WebhookURL
		}
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(response)
}

func (s *Server) handlePutChannel(w http.ResponseWriter, r *http.Request) {
	kind := r.PathValue("kind")
	if kind != "slack" && kind != "discord" {
		http.Error(w, "channel must be slack or discord", http.StatusBadRequest)
		return
	}

	var body struct {
		WebhookURL string `json:"webhook_url"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	webhookURL := strings.TrimSpace(body.WebhookURL)
	if webhookURL == "" {
		if err := s.db.DeleteAlertChannel(currentUserID(r), kind); err != nil {
			s.logger.Printf("Error clearing %s channel: %v", kind, err)
			http.Error(w, "Failed to clear alert channel", http.StatusInternalServerError)
			return
		}
		w.WriteHeader(http.StatusNoContent)
		return
	}
	if err := integrations.ValidateWebhookURL(webhookURL); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if err := s.db.UpsertAlertChannel(currentUserID(r), kind, webhookURL); err != nil {
		s.logger.Printf("Error saving %s channel: %v", kind, err)
		http.Error(w, "Failed to save alert channel", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"kind": kind, "webhook_url": webhookURL})
}

func (s *Server) authMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if publicPath(r.Method, r.URL.Path) || !strings.HasPrefix(r.URL.Path, "/api/") {
			next.ServeHTTP(w, r)
			return
		}
		user, err := s.userFromRequest(r)
		if err != nil {
			s.logger.Printf("session lookup: %v", err)
			http.Error(w, "Failed to authenticate", http.StatusInternalServerError)
			return
		}
		if user == nil {
			w.Header().Set("WWW-Authenticate", `Bearer realm="opensentry"`)
			http.Error(w, "Unauthorized", http.StatusUnauthorized)
			return
		}
		next.ServeHTTP(w, withUser(r, user))
	})
}

func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	if s.db == nil || s.db.GetDB() == nil {
		http.Error(w, "database unavailable", http.StatusServiceUnavailable)
		return
	}
	if err := s.db.GetDB().PingContext(r.Context()); err != nil {
		s.logger.Printf("healthz database ping failed: %v", err)
		http.Error(w, "database unavailable", http.StatusServiceUnavailable)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte(`{"status":"ok"}`))
}

func (s *Server) corsMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization")

		if r.Method == "OPTIONS" {
			w.WriteHeader(http.StatusOK)
			return
		}

		next.ServeHTTP(w, r)
	})
}

func (s *Server) handleCreateJob(w http.ResponseWriter, r *http.Request) {
	var jobRequest struct {
		Name        string `json:"name"`
		Description string `json:"description"`
		Schedule    string `json:"schedule"`
		GraceTime   int    `json:"grace_time"`
	}

	if err := json.NewDecoder(r.Body).Decode(&jobRequest); err != nil {
		s.logger.Println("Invalid request body")
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	if jobRequest.Name == "" || jobRequest.Schedule == "" {
		s.logger.Println("Name and schedule are required")
		http.Error(w, "Name and schedule are required", http.StatusBadRequest)
		return
	}

	if !gronx.IsValid(jobRequest.Schedule) {
		s.logger.Println("Invalid CRON schedule provided")
		http.Error(w, "Invalid CRON schedule provided", http.StatusBadRequest)
		return
	}

	nextTick, err := gronx.NextTick(jobRequest.Schedule, true)
	if err != nil {
		s.logger.Println("Error calculating next tick")
		http.Error(w, "Error calculating next tick", http.StatusBadRequest)
		return
	}

	job := &models.Job{
		ID:          uuid.New().String(),
		Name:        jobRequest.Name,
		Description: jobRequest.Description,
		Schedule:    jobRequest.Schedule,
		GraceTime:   jobRequest.GraceTime,
		Status:      models.StatusHealthy,
		LastPing:    time.Now().UTC(),
		NextExpect:  nextTick,
		UserID:      currentUserID(r),
		CreatedAt:   time.Now().UTC(),
		UpdatedAt:   time.Now().UTC(),
	}

	if err := s.db.CreateJob(job); err != nil {
		s.logger.Printf("Error creating job: %v", err)
		http.Error(w, "Failed to create job", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(job)
}

func (s *Server) handleListJobs(w http.ResponseWriter, r *http.Request) {
	jobs, err := s.db.ListJobsByUser(currentUserID(r))
	if err != nil {
		s.logger.Printf("Error listing jobs: %v", err)
		http.Error(w, "Failed to list jobs", http.StatusInternalServerError)
		return
	}

	// Ensure we always return an array, even if empty
	if jobs == nil {
		jobs = make([]*models.Job, 0)
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(jobs)
}

func (s *Server) handleGetJob(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if id == "" {
		http.Error(w, "Job ID is required", http.StatusBadRequest)
		return
	}

	job, ok := s.ownedJob(w, r, id)
	if !ok {
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(job)
}

func (s *Server) ownedJob(w http.ResponseWriter, r *http.Request, id string) (*models.Job, bool) {
	userID := currentUserID(r)
	if userID == "" {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return nil, false
	}
	job, err := s.db.GetJobForUser(id, userID)
	if err != nil {
		s.logger.Printf("Error getting job: %v", err)
		http.Error(w, "Failed to get job", http.StatusInternalServerError)
		return nil, false
	}
	if job == nil {
		http.Error(w, "Job not found", http.StatusNotFound)
		return nil, false
	}
	return job, true
}

func (s *Server) handleListEvents(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if id == "" {
		http.Error(w, "Job ID is required", http.StatusBadRequest)
		return
	}

	if _, ok := s.ownedJob(w, r, id); !ok {
		return
	}

	limit := 50
	if raw := r.URL.Query().Get("limit"); raw != "" {
		parsed, err := strconv.Atoi(raw)
		if err != nil || parsed < 1 || parsed > 100 {
			http.Error(w, "Invalid limit", http.StatusBadRequest)
			return
		}
		limit = parsed
	}

	events, err := s.db.ListJobEvents(id, currentUserID(r), limit)
	if err != nil {
		s.logger.Printf("Error listing job events: %v", err)
		http.Error(w, "Failed to list job events", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(events)
}

func pingHTTPStatus(err error) int {
	if err == nil {
		return http.StatusOK
	}
	if errors.Is(err, db.ErrJobNotFound) {
		return http.StatusNotFound
	}
	return http.StatusInternalServerError
}

var errInvalidStatus = errors.New("invalid status")

// applyStatusChange sets a job's status. Resuming from paused recomputes
// next_expect from now so the missed maintenance window does not alert.
// last_ping is left untouched.
func applyStatusChange(job *models.Job, status string) error {
	next := models.JobStatus(status)
	switch next {
	case models.StatusHealthy, models.StatusPaused:
	default:
		return errInvalidStatus
	}
	if job.Status == models.StatusPaused && next == models.StatusHealthy {
		nextTick, err := gronx.NextTick(job.Schedule, true)
		if err != nil {
			return err
		}
		job.NextExpect = nextTick
	}
	job.Status = next
	return nil
}

// applyScheduleChange validates a new cron schedule and recomputes next_expect
// when the expression actually changes. last_ping is left untouched.
func applyScheduleChange(job *models.Job, newSchedule string) error {
	if !gronx.IsValid(newSchedule) {
		return errors.New("invalid CRON schedule")
	}
	scheduleChanged := newSchedule != job.Schedule
	job.Schedule = newSchedule
	if scheduleChanged {
		nextTick, err := gronx.NextTick(newSchedule, true)
		if err != nil {
			return err
		}
		job.NextExpect = nextTick
	}
	return nil
}

func (s *Server) handleUpdateJob(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if id == "" {
		http.Error(w, "Job ID is required", http.StatusBadRequest)
		return
	}

	job, ok := s.ownedJob(w, r, id)
	if !ok {
		return
	}

	// Pointers distinguish an omitted field from a present one. Pause can send
	// only {"status":"paused"} without clearing name or description, and a
	// present empty description still clears it.
	var jobRequest struct {
		Name        *string `json:"name"`
		Description *string `json:"description"`
		Schedule    *string `json:"schedule"`
		GraceTime   *int    `json:"grace_time"`
		Status      *string `json:"status"`
	}

	if err := json.NewDecoder(r.Body).Decode(&jobRequest); err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	if jobRequest.Name != nil && strings.TrimSpace(*jobRequest.Name) == "" {
		http.Error(w, "Name is required", http.StatusBadRequest)
		return
	}
	if jobRequest.GraceTime != nil && *jobRequest.GraceTime < 1 {
		http.Error(w, "Grace time must be positive", http.StatusBadRequest)
		return
	}
	if jobRequest.Status != nil && *jobRequest.Status != "" {
		switch models.JobStatus(*jobRequest.Status) {
		case models.StatusHealthy, models.StatusPaused:
		default:
			http.Error(w, "Invalid status", http.StatusBadRequest)
			return
		}
	}
	if jobRequest.Schedule != nil && *jobRequest.Schedule != "" && !gronx.IsValid(*jobRequest.Schedule) {
		s.logger.Println("Invalid CRON schedule provided")
		http.Error(w, "Invalid CRON schedule provided", http.StatusBadRequest)
		return
	}

	if jobRequest.Name != nil {
		job.Name = strings.TrimSpace(*jobRequest.Name)
	}
	if jobRequest.Description != nil {
		job.Description = *jobRequest.Description
	}
	if jobRequest.Schedule != nil && *jobRequest.Schedule != "" {
		if err := applyScheduleChange(job, *jobRequest.Schedule); err != nil {
			s.logger.Println("Invalid CRON schedule provided")
			http.Error(w, "Invalid CRON schedule provided", http.StatusBadRequest)
			return
		}
	}
	if jobRequest.GraceTime != nil {
		job.GraceTime = *jobRequest.GraceTime
	}
	if jobRequest.Status != nil && *jobRequest.Status != "" {
		if err := applyStatusChange(job, *jobRequest.Status); err != nil {
			if errors.Is(err, errInvalidStatus) {
				http.Error(w, "Invalid status", http.StatusBadRequest)
				return
			}
			s.logger.Printf("Error calculating next tick: %v", err)
			http.Error(w, "Error calculating next tick", http.StatusBadRequest)
			return
		}
	}

	if err := s.db.UpdateJob(job); err != nil {
		s.logger.Printf("Error updating job: %v", err)
		http.Error(w, "Failed to update job", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(job)
}

func (s *Server) handlePing(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if id == "" {
		http.Error(w, "Job ID is required", http.StatusBadRequest)
		return
	}

	if err := s.db.RecordPing(id); err != nil {
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

func (s *Server) loggingMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		s.logger.Printf("Started %s %s", r.Method, r.URL.Path)
		next.ServeHTTP(w, r)
		s.logger.Printf("Completed %s %s in %v", r.Method, r.URL.Path, time.Since(start))
	})
}

func (s *Server) recoveryMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if err := recover(); err != nil {
				s.logger.Printf("Panic: %v", err)
				http.Error(w, "Internal server error", http.StatusInternalServerError)
			}
		}()
		next.ServeHTTP(w, r)
	})
}

func (s *Server) handleDeleteJob(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if id == "" {
		http.Error(w, "Job ID is required", http.StatusBadRequest)
		return
	}

	if _, ok := s.ownedJob(w, r, id); !ok {
		return
	}

	if err := s.db.DeleteJob(id, currentUserID(r)); err != nil {
		s.logger.Printf("Error deleting job: %v", err)
		http.Error(w, "Failed to delete job", http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusOK)
}
