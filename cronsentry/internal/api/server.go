package api

import (
	"encoding/json"
	"log"
	"net/http"
	"time"

	"github.com/adhocore/gronx"
	"github.com/google/uuid"
	"github.com/zigamedved/cronsentry/internal/db"
	"github.com/zigamedved/cronsentry/internal/models"
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
	mux.HandleFunc("POST /api/jobs", s.handleCreateJob)
	mux.HandleFunc("GET /api/jobs", s.handleListJobs)
	mux.HandleFunc("GET /api/jobs/{id}", s.handleGetJob)
	mux.HandleFunc("PUT /api/jobs/{id}", s.handleUpdateJob)
	mux.HandleFunc("DELETE /api/jobs/{id}", s.handleDeleteJob)
	mux.HandleFunc("POST /api/ping/{id}", s.handlePing)
	return s.corsMiddleware(s.loggingMiddleware(s.recoveryMiddleware(mux)))
}

func (s *Server) corsMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type")

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
		UserID:      "test-user", // hardcoded for now, should come from auth
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
	userID := "test-user" // TODO: get user ID from auth

	jobs, err := s.db.ListJobsByUser(userID)
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

	job, err := s.db.GetJob(id)
	if err != nil {
		s.logger.Printf("Error getting job: %v", err)
		http.Error(w, "Failed to get job", http.StatusInternalServerError)
		return
	}

	if job == nil {
		http.Error(w, "Job not found", http.StatusNotFound)
		return
	}

	if job.UserID != "test-user" {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(job)
}

func (s *Server) handleUpdateJob(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if id == "" {
		http.Error(w, "Job ID is required", http.StatusBadRequest)
		return
	}

	job, err := s.db.GetJob(id)
	if err != nil {
		s.logger.Printf("Error getting job: %v", err)
		http.Error(w, "Failed to get job", http.StatusInternalServerError)
		return
	}

	if job == nil {
		http.Error(w, "Job not found", http.StatusNotFound)
		return
	}

	if job.UserID != "test-user" {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}

	var jobRequest struct {
		Name        string `json:"name"`
		Description string `json:"description"`
		Schedule    string `json:"schedule"`
		GraceTime   int    `json:"grace_time"`
		Status      string `json:"status"`
	}

	if err := json.NewDecoder(r.Body).Decode(&jobRequest); err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	if jobRequest.Name != "" {
		job.Name = jobRequest.Name
	}
	if jobRequest.Description != "" {
		job.Description = jobRequest.Description
	}
	if jobRequest.Schedule != "" {
		job.Schedule = jobRequest.Schedule
	}
	if jobRequest.GraceTime > 0 {
		job.GraceTime = jobRequest.GraceTime
	}
	if jobRequest.Status != "" {
		job.Status = models.JobStatus(jobRequest.Status)
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

	job, err := s.db.GetJob(id)
	if err != nil {
		s.logger.Printf("Error getting job: %v", err)
		http.Error(w, "Failed to get job", http.StatusInternalServerError)
		return
	}

	if job == nil {
		http.Error(w, "Job not found", http.StatusNotFound)
		return
	}

	if job.UserID != "test-user" {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}

	if err := s.db.DeleteJob(id); err != nil {
		s.logger.Printf("Error deleting job: %v", err)
		http.Error(w, "Failed to delete job", http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusOK)
}
