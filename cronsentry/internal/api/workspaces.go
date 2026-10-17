package api

import (
	"encoding/csv"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/zigamedved/OpenSentry/internal/db"
)

func (s *Server) writeWorkspaceError(w http.ResponseWriter, err error) bool {
	switch {
	case errors.Is(err, db.ErrNotMember):
		http.Error(w, "Workspace not found", http.StatusNotFound)
	case errors.Is(err, db.ErrNotAdmin):
		http.Error(w, "Admin role required", http.StatusForbidden)
	case errors.Is(err, db.ErrLastAdmin):
		http.Error(w, "A workspace needs an admin", http.StatusBadRequest)
	case errors.Is(err, db.ErrAlreadyMember):
		http.Error(w, "Already a member", http.StatusConflict)
	case errors.Is(err, db.ErrInviteNotFound):
		http.Error(w, "Invite not found", http.StatusNotFound)
	case errors.Is(err, db.ErrInviteEmail):
		http.Error(w, "This invite is for a different email", http.StatusForbidden)
	case errors.Is(err, db.ErrBadRole):
		http.Error(w, "role must be admin or member", http.StatusBadRequest)
	default:
		return false
	}
	return true
}

func (s *Server) handleListWorkspaces(w http.ResponseWriter, r *http.Request) {
	list, err := s.db.ListWorkspaces(currentUserID(r))
	if err != nil {
		s.logger.Printf("list workspaces: %v", err)
		http.Error(w, "Failed to list workspaces", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(list)
}

func (s *Server) handleSwitchWorkspace(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if err := s.db.SetActiveWorkspace(currentUserID(r), id); err != nil {
		if s.writeWorkspaceError(w, err) {
			return
		}
		s.logger.Printf("switch workspace: %v", err)
		http.Error(w, "Failed to switch workspace", http.StatusInternalServerError)
		return
	}
	list, err := s.db.ListWorkspaces(currentUserID(r))
	if err != nil {
		s.logger.Printf("list workspaces after switch: %v", err)
		http.Error(w, "Failed to switch workspace", http.StatusInternalServerError)
		return
	}
	for i := range list {
		if list[i].ID == id {
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(list[i])
			return
		}
	}
	http.Error(w, "Workspace not found", http.StatusNotFound)
}

func (s *Server) handleListMembers(w http.ResponseWriter, r *http.Request) {
	members, err := s.db.ListMembers(r.PathValue("id"), currentUserID(r))
	if err != nil {
		if s.writeWorkspaceError(w, err) {
			return
		}
		s.logger.Printf("list members: %v", err)
		http.Error(w, "Failed to list members", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(members)
}

func (s *Server) handleCreateInvite(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Email string `json:"email"`
		Role  string `json:"role"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}
	if strings.TrimSpace(body.Email) == "" {
		http.Error(w, "Email is required", http.StatusBadRequest)
		return
	}
	role := strings.TrimSpace(body.Role)
	if role == "" {
		role = "member"
	}
	invite, err := s.db.CreateInvite(r.PathValue("id"), currentUserID(r), body.Email, role)
	if err != nil {
		if s.writeWorkspaceError(w, err) {
			return
		}
		s.logger.Printf("create invite: %v", err)
		http.Error(w, "Failed to create invite", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(invite)
}

func (s *Server) handleListWorkspaceInvites(w http.ResponseWriter, r *http.Request) {
	invites, err := s.db.ListWorkspaceInvites(r.PathValue("id"), currentUserID(r))
	if err != nil {
		if s.writeWorkspaceError(w, err) {
			return
		}
		s.logger.Printf("list workspace invites: %v", err)
		http.Error(w, "Failed to list invites", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(invites)
}

func (s *Server) handleListMyInvites(w http.ResponseWriter, r *http.Request) {
	user := currentUser(r)
	if user == nil {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}
	invites, err := s.db.ListPendingInvites(user.Email)
	if err != nil {
		s.logger.Printf("list my invites: %v", err)
		http.Error(w, "Failed to list invites", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(invites)
}

func (s *Server) handleAcceptInvite(w http.ResponseWriter, r *http.Request) {
	user := currentUser(r)
	if user == nil {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}
	invite, err := s.db.AcceptInvite(r.PathValue("token"), user)
	if err != nil {
		if s.writeWorkspaceError(w, err) {
			return
		}
		s.logger.Printf("accept invite: %v", err)
		http.Error(w, "Failed to accept invite", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(invite)
}

func (s *Server) handleSetMemberRole(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Role string `json:"role"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}
	err := s.db.SetMemberRole(r.PathValue("id"), currentUserID(r), r.PathValue("userID"), strings.TrimSpace(body.Role))
	if err != nil {
		if s.writeWorkspaceError(w, err) {
			return
		}
		s.logger.Printf("set member role: %v", err)
		http.Error(w, "Failed to update member", http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleRemoveMember(w http.ResponseWriter, r *http.Request) {
	err := s.db.RemoveMember(r.PathValue("id"), currentUserID(r), r.PathValue("userID"))
	if err != nil {
		if s.writeWorkspaceError(w, err) {
			return
		}
		s.logger.Printf("remove member: %v", err)
		http.Error(w, "Failed to remove member", http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleExportWorkspace(w http.ResponseWriter, r *http.Request) {
	fromRaw := strings.TrimSpace(r.URL.Query().Get("from"))
	toRaw := strings.TrimSpace(r.URL.Query().Get("to"))
	if fromRaw == "" || toRaw == "" {
		http.Error(w, "from and to are required", http.StatusBadRequest)
		return
	}
	from, fromErr := time.Parse(time.RFC3339, fromRaw)
	to, toErr := time.Parse(time.RFC3339, toRaw)
	if fromErr != nil || toErr != nil {
		http.Error(w, "from and to must be RFC3339", http.StatusBadRequest)
		return
	}
	if !from.Before(to) {
		http.Error(w, "from must be before to", http.StatusBadRequest)
		return
	}
	if to.Sub(from) > 366*24*time.Hour {
		http.Error(w, "Date range is too long", http.StatusBadRequest)
		return
	}
	format := strings.TrimSpace(r.URL.Query().Get("format"))
	if format == "" {
		format = "json"
	}
	if format != "json" && format != "csv" {
		http.Error(w, "format must be json or csv", http.StatusBadRequest)
		return
	}
	exported, err := s.db.ExportWorkspace(r.PathValue("id"), currentUserID(r), from, to)
	if err != nil {
		if s.writeWorkspaceError(w, err) {
			return
		}
		s.logger.Printf("export workspace: %v", err)
		http.Error(w, "Failed to export workspace", http.StatusInternalServerError)
		return
	}
	if format == "csv" {
		w.Header().Set("Content-Type", "text/csv; charset=utf-8")
		w.Header().Set("Content-Disposition", `attachment; filename="opensentry-export.csv"`)
		writeExportCSV(w, exported)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Content-Disposition", `attachment; filename="opensentry-export.json"`)
	json.NewEncoder(w).Encode(exported)
}

func writeExportCSV(w http.ResponseWriter, exported *db.WorkspaceExport) {
	cw := csv.NewWriter(w)
	_ = cw.Write([]string{"record", "id", "job_id", "job_name", "type", "status", "message", "data", "created_at"})
	for _, event := range exported.Events {
		_ = cw.Write([]string{
			"event", event.ID, event.JobID, event.JobName, event.Type, "", "", event.Data, event.CreatedAt.UTC().Format(time.RFC3339Nano),
		})
	}
	for _, note := range exported.Notifications {
		_ = cw.Write([]string{
			"notification", note.ID, note.JobID, "", note.Type, note.Status, note.Message, "", note.CreatedAt.UTC().Format(time.RFC3339Nano),
		})
	}
	cw.Flush()
}
