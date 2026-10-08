package api

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/zigamedved/OpenSentry/internal/db"
	"github.com/zigamedved/OpenSentry/internal/models"
)

const (
	sessionCookie = "opensentry_session"
	sessionMaxAge = 14 * 24 * 60 * 60
	minPassword   = 8
)

type userContextKey struct{}

func (s *Server) handleRegister(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Email    string `json:"email"`
		Password string `json:"password"`
		Name     string `json:"name"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	email := strings.ToLower(strings.TrimSpace(body.Email))
	name := strings.TrimSpace(body.Name)
	if !validEmail(email) {
		http.Error(w, "Invalid email", http.StatusBadRequest)
		return
	}
	if len(body.Password) < minPassword {
		http.Error(w, "Password must be at least 8 characters", http.StatusBadRequest)
		return
	}
	if name == "" {
		http.Error(w, "Name is required", http.StatusBadRequest)
		return
	}

	user, err := s.db.CreateUser(email, name, body.Password)
	if errors.Is(err, db.ErrEmailTaken) {
		http.Error(w, "Email already registered", http.StatusConflict)
		return
	}
	if err != nil {
		s.logger.Printf("register: %v", err)
		http.Error(w, "Failed to register", http.StatusInternalServerError)
		return
	}
	s.issueSession(w, r, user, http.StatusCreated)
}

func (s *Server) handleLogin(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Email    string `json:"email"`
		Password string `json:"password"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	email := strings.ToLower(strings.TrimSpace(body.Email))
	user, err := s.db.UserByEmail(email)
	if err != nil {
		s.logger.Printf("login: %v", err)
		http.Error(w, "Failed to sign in", http.StatusInternalServerError)
		return
	}
	hash := ""
	if user != nil {
		hash = user.Password
	}
	if user == nil || !db.CheckPassword(hash, body.Password) {
		http.Error(w, "Invalid email or password", http.StatusUnauthorized)
		return
	}
	user.Password = ""
	s.issueSession(w, r, user, http.StatusOK)
}

func (s *Server) handleLogout(w http.ResponseWriter, r *http.Request) {
	token := presentedSessionToken(r)
	if err := s.db.DeleteSession(token); err != nil {
		s.logger.Printf("logout: %v", err)
		http.Error(w, "Failed to sign out", http.StatusInternalServerError)
		return
	}
	http.SetCookie(w, &http.Cookie{
		Name:     sessionCookie,
		Value:    "",
		Path:     "/",
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
		Secure:   requestIsHTTPS(r),
		MaxAge:   -1,
		Expires:  time.Unix(0, 0),
	})
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleMe(w http.ResponseWriter, r *http.Request) {
	user := currentUser(r)
	if user == nil {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}
	writeJSON(w, http.StatusOK, publicUser(user))
}

func (s *Server) issueSession(w http.ResponseWriter, r *http.Request, user *models.User, status int) {
	token, expires, err := s.db.CreateSession(user.ID)
	if err != nil {
		s.logger.Printf("session: %v", err)
		http.Error(w, "Failed to start session", http.StatusInternalServerError)
		return
	}
	http.SetCookie(w, &http.Cookie{
		Name:     sessionCookie,
		Value:    token,
		Path:     "/",
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
		Secure:   requestIsHTTPS(r),
		Expires:  expires,
		MaxAge:   sessionMaxAge,
	})
	writeJSON(w, status, map[string]any{
		"token": token,
		"user":  publicUser(user),
	})
}

func publicUser(user *models.User) map[string]string {
	return map[string]string{
		"id":    user.ID,
		"email": user.Email,
		"name":  user.Name,
	}
}

func writeJSON(w http.ResponseWriter, status int, payload any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(payload)
}

func validEmail(email string) bool {
	if strings.ContainsAny(email, " \t") || strings.Count(email, "@") != 1 {
		return false
	}
	local, domain, _ := strings.Cut(email, "@")
	return local != "" && domain != "" && strings.Contains(domain, ".")
}

func requestIsHTTPS(r *http.Request) bool {
	if r.TLS != nil {
		return true
	}
	return strings.EqualFold(strings.TrimSpace(r.Header.Get("X-Forwarded-Proto")), "https")
}

// presentedSessionToken prefers the cookie, then a bearer token.
func presentedSessionToken(r *http.Request) string {
	if cookie, err := r.Cookie(sessionCookie); err == nil {
		if token := strings.TrimSpace(cookie.Value); token != "" {
			return token
		}
	}
	header := r.Header.Get("Authorization")
	if strings.HasPrefix(header, "Bearer ") {
		return strings.TrimSpace(strings.TrimPrefix(header, "Bearer "))
	}
	return ""
}

func (s *Server) userFromRequest(r *http.Request) (*models.User, error) {
	token := presentedSessionToken(r)
	if token == "" || s.db == nil {
		return nil, nil
	}
	user, err := s.db.UserBySessionToken(token)
	if user != nil {
		user.Password = ""
	}
	return user, err
}

func currentUser(r *http.Request) *models.User {
	user, _ := r.Context().Value(userContextKey{}).(*models.User)
	return user
}

func currentUserID(r *http.Request) string {
	if user := currentUser(r); user != nil {
		return user.ID
	}
	return ""
}

func withUser(r *http.Request, user *models.User) *http.Request {
	return r.WithContext(context.WithValue(r.Context(), userContextKey{}, user))
}
