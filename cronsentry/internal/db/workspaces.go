package db

import (
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/zigamedved/OpenSentry/internal/models"
)

const exportRowCap = 5000

var (
	// ErrNotMember means the user has no row in the workspace.
	ErrNotMember = errors.New("workspace not found")
	// ErrNotAdmin means the user is a member but not an admin.
	ErrNotAdmin = errors.New("admin role required")
	// ErrLastAdmin means the change would leave the workspace with no admin.
	ErrLastAdmin = errors.New("a workspace needs an admin")
	// ErrAlreadyMember means that email is already in the workspace.
	ErrAlreadyMember = errors.New("already a member")
	// ErrInviteNotFound means the token is missing or already used.
	ErrInviteNotFound = errors.New("invite not found")
	// ErrInviteEmail means the signed-in email is not the invited email.
	ErrInviteEmail = errors.New("this invite is for a different email")
	// ErrBadRole means the role is not admin or member.
	ErrBadRole = errors.New("role must be admin or member")
)

func validRole(role string) bool {
	return role == string(models.RoleAdmin) || role == string(models.RoleMember)
}

// EnsurePersonalWorkspace gives a user a workspace and an active selection.
// Existing members are left alone, except a missing active workspace is filled.
func (d *Database) EnsurePersonalWorkspace(userID string) error {
	var name string
	var created time.Time
	err := d.db.QueryRow(`SELECT name, created_at FROM users WHERE id = $1`, userID).Scan(&name, &created)
	if err != nil {
		return fmt.Errorf("load user for workspace: %w", err)
	}
	var members int
	if err := d.db.QueryRow(`SELECT COUNT(*) FROM workspace_members WHERE user_id = $1`, userID).Scan(&members); err != nil {
		return fmt.Errorf("count memberships: %w", err)
	}
	if members == 0 {
		now := time.Now().UTC()
		if created.IsZero() {
			created = now
		}
		wsID := uuid.New().String()
		tx, err := d.db.Begin()
		if err != nil {
			return err
		}
		if _, err := tx.Exec(`
			INSERT INTO workspaces (id, name, created_at, updated_at)
			VALUES ($1, $2, $3, $3)
		`, wsID, name+"'s workspace", created); err != nil {
			tx.Rollback()
			return fmt.Errorf("create workspace: %w", err)
		}
		if _, err := tx.Exec(`
			INSERT INTO workspace_members (workspace_id, user_id, role, created_at)
			VALUES ($1, $2, 'admin', $3)
		`, wsID, userID, created); err != nil {
			tx.Rollback()
			return fmt.Errorf("create membership: %w", err)
		}
		if _, err := tx.Exec(`UPDATE users SET active_workspace_id = $1 WHERE id = $2`, wsID, userID); err != nil {
			tx.Rollback()
			return fmt.Errorf("set active workspace: %w", err)
		}
		if err := tx.Commit(); err != nil {
			return err
		}
		return nil
	}
	_, err = d.db.Exec(`
		UPDATE users
		SET active_workspace_id = (
			SELECT workspace_id FROM workspace_members
			WHERE user_id = $1
			ORDER BY created_at, workspace_id
			LIMIT 1
		)
		WHERE id = $1 AND (
			active_workspace_id IS NULL
			OR NOT EXISTS (
				SELECT 1 FROM workspace_members m
				WHERE m.user_id = $1 AND m.workspace_id = users.active_workspace_id
			)
		)
	`, userID)
	if err != nil {
		return fmt.Errorf("repair active workspace: %w", err)
	}
	return nil
}

func (d *Database) ListWorkspaces(userID string) ([]models.Workspace, error) {
	if err := d.EnsurePersonalWorkspace(userID); err != nil {
		return nil, err
	}
	rows, err := d.db.Query(`
		SELECT w.id, w.name, m.role, w.id = u.active_workspace_id
		FROM workspace_members m
		JOIN workspaces w ON w.id = m.workspace_id
		JOIN users u ON u.id = m.user_id
		WHERE m.user_id = $1
		ORDER BY w.created_at, w.id
	`, userID)
	if err != nil {
		return nil, fmt.Errorf("list workspaces: %w", err)
	}
	defer rows.Close()
	out := make([]models.Workspace, 0)
	for rows.Next() {
		var ws models.Workspace
		if err := rows.Scan(&ws.ID, &ws.Name, &ws.Role, &ws.Active); err != nil {
			return nil, fmt.Errorf("scan workspace: %w", err)
		}
		out = append(out, ws)
	}
	return out, rows.Err()
}

func (d *Database) ActiveWorkspace(userID string) (*models.Workspace, error) {
	list, err := d.ListWorkspaces(userID)
	if err != nil {
		return nil, err
	}
	for i := range list {
		if list[i].Active {
			return &list[i], nil
		}
	}
	if len(list) == 0 {
		return nil, ErrNotMember
	}
	return &list[0], nil
}

// SetActiveWorkspace selects a workspace the user already belongs to.
// The UPDATE is skipped unless that membership exists. The returned row is
// the workspace name and role for the response.
func (d *Database) SetActiveWorkspace(userID, workspaceID string) (*models.Workspace, error) {
	var ws models.Workspace
	err := d.db.QueryRow(`
		WITH switched AS (
			UPDATE users SET active_workspace_id = $1
			WHERE id = $2 AND EXISTS (
				SELECT 1 FROM workspace_members m
				WHERE m.user_id = $2 AND m.workspace_id = $1
			)
			RETURNING id
		)
		SELECT w.id, w.name, m.role, true
		FROM switched
		JOIN workspace_members m ON m.user_id = switched.id AND m.workspace_id = $1
		JOIN workspaces w ON w.id = $1
	`, workspaceID, userID).Scan(&ws.ID, &ws.Name, &ws.Role, &ws.Active)
	if err == sql.ErrNoRows {
		return nil, ErrNotMember
	}
	if err != nil {
		return nil, fmt.Errorf("set active workspace: %w", err)
	}
	return &ws, nil
}

func (d *Database) memberRole(workspaceID, userID string) (string, error) {
	var role string
	err := d.db.QueryRow(`
		SELECT role FROM workspace_members
		WHERE workspace_id = $1 AND user_id = $2
	`, workspaceID, userID).Scan(&role)
	if err == sql.ErrNoRows {
		return "", ErrNotMember
	}
	if err != nil {
		return "", fmt.Errorf("member role: %w", err)
	}
	return role, nil
}

func (d *Database) ListMembers(workspaceID, userID string) ([]models.WorkspaceMember, error) {
	if _, err := d.memberRole(workspaceID, userID); err != nil {
		return nil, err
	}
	rows, err := d.db.Query(`
		SELECT u.id, u.email, u.name, m.role
		FROM workspace_members m
		JOIN users u ON u.id = m.user_id
		WHERE m.workspace_id = $1
		ORDER BY m.created_at, u.email
	`, workspaceID)
	if err != nil {
		return nil, fmt.Errorf("list members: %w", err)
	}
	defer rows.Close()
	out := make([]models.WorkspaceMember, 0)
	for rows.Next() {
		var member models.WorkspaceMember
		if err := rows.Scan(&member.UserID, &member.Email, &member.Name, &member.Role); err != nil {
			return nil, fmt.Errorf("scan member: %w", err)
		}
		out = append(out, member)
	}
	return out, rows.Err()
}

func (d *Database) CreateInvite(workspaceID, actorID, email, role string) (*models.WorkspaceInvite, error) {
	if !validRole(role) {
		return nil, ErrBadRole
	}
	actorRole, err := d.memberRole(workspaceID, actorID)
	if err != nil {
		return nil, err
	}
	if actorRole != string(models.RoleAdmin) {
		return nil, ErrNotAdmin
	}
	email = normalizeEmail(email)
	var existing int
	if err := d.db.QueryRow(`
		SELECT COUNT(*) FROM workspace_members m
		JOIN users u ON u.id = m.user_id
		WHERE m.workspace_id = $1 AND u.email = $2
	`, workspaceID, email).Scan(&existing); err != nil {
		return nil, err
	}
	if existing > 0 {
		return nil, ErrAlreadyMember
	}
	var pending models.WorkspaceInvite
	err = d.db.QueryRow(`
		SELECT i.id, i.workspace_id, w.name, i.email, i.role, i.token, i.created_at
		FROM workspace_invites i
		JOIN workspaces w ON w.id = i.workspace_id
		WHERE i.workspace_id = $1 AND i.email = $2 AND i.accepted_at IS NULL
	`, workspaceID, email).Scan(&pending.ID, &pending.WorkspaceID, &pending.WorkspaceName, &pending.Email, &pending.Role, &pending.Token, &pending.CreatedAt)
	if err == nil {
		return &pending, nil
	}
	if err != sql.ErrNoRows {
		return nil, fmt.Errorf("lookup invite: %w", err)
	}
	invite := &models.WorkspaceInvite{
		ID:          uuid.New().String(),
		WorkspaceID: workspaceID,
		Email:       email,
		Role:        models.WorkspaceRole(role),
		Token:       uuid.New().String(),
		CreatedAt:   time.Now().UTC(),
	}
	err = d.db.QueryRow(`
		INSERT INTO workspace_invites (id, workspace_id, email, role, token, invited_by, created_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7)
		RETURNING (SELECT name FROM workspaces WHERE id = $2)
	`, invite.ID, invite.WorkspaceID, invite.Email, invite.Role, invite.Token, actorID, invite.CreatedAt).Scan(&invite.WorkspaceName)
	if err != nil {
		return nil, fmt.Errorf("create invite: %w", err)
	}
	return invite, nil
}

func (d *Database) ListPendingInvites(email string) ([]models.WorkspaceInvite, error) {
	rows, err := d.db.Query(`
		SELECT i.id, i.workspace_id, w.name, i.email, i.role, i.token, i.created_at
		FROM workspace_invites i
		JOIN workspaces w ON w.id = i.workspace_id
		WHERE i.email = $1 AND i.accepted_at IS NULL
		ORDER BY i.created_at
	`, normalizeEmail(email))
	if err != nil {
		return nil, fmt.Errorf("list invites: %w", err)
	}
	defer rows.Close()
	return scanInvites(rows)
}

func (d *Database) ListWorkspaceInvites(workspaceID, actorID string) ([]models.WorkspaceInvite, error) {
	role, err := d.memberRole(workspaceID, actorID)
	if err != nil {
		return nil, err
	}
	if role != string(models.RoleAdmin) {
		return nil, ErrNotAdmin
	}
	rows, err := d.db.Query(`
		SELECT i.id, i.workspace_id, w.name, i.email, i.role, i.token, i.created_at
		FROM workspace_invites i
		JOIN workspaces w ON w.id = i.workspace_id
		WHERE i.workspace_id = $1 AND i.accepted_at IS NULL
		ORDER BY i.created_at
	`, workspaceID)
	if err != nil {
		return nil, fmt.Errorf("list workspace invites: %w", err)
	}
	defer rows.Close()
	return scanInvites(rows)
}

func scanInvites(rows *sql.Rows) ([]models.WorkspaceInvite, error) {
	out := make([]models.WorkspaceInvite, 0)
	for rows.Next() {
		var invite models.WorkspaceInvite
		if err := rows.Scan(&invite.ID, &invite.WorkspaceID, &invite.WorkspaceName, &invite.Email, &invite.Role, &invite.Token, &invite.CreatedAt); err != nil {
			return nil, fmt.Errorf("scan invite: %w", err)
		}
		out = append(out, invite)
	}
	return out, rows.Err()
}

// AcceptInvite joins the signed-in user to the workspace and makes it active.
func (d *Database) AcceptInvite(token string, user *models.User) (*models.WorkspaceInvite, error) {
	var invite models.WorkspaceInvite
	var accepted sql.NullTime
	err := d.db.QueryRow(`
		SELECT i.id, i.workspace_id, w.name, i.email, i.role, i.token, i.created_at, i.accepted_at
		FROM workspace_invites i
		JOIN workspaces w ON w.id = i.workspace_id
		WHERE i.token = $1
	`, token).Scan(&invite.ID, &invite.WorkspaceID, &invite.WorkspaceName, &invite.Email, &invite.Role, &invite.Token, &invite.CreatedAt, &accepted)
	if err == sql.ErrNoRows {
		return nil, ErrInviteNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("load invite: %w", err)
	}
	if normalizeEmail(invite.Email) != normalizeEmail(user.Email) {
		return nil, ErrInviteEmail
	}
	if _, err := d.db.Exec(`
		INSERT INTO workspace_members (workspace_id, user_id, role, created_at)
		VALUES ($1, $2, $3, $4)
		ON CONFLICT (workspace_id, user_id) DO NOTHING
	`, invite.WorkspaceID, user.ID, invite.Role, time.Now().UTC()); err != nil {
		return nil, fmt.Errorf("join workspace: %w", err)
	}
	if !accepted.Valid {
		if _, err := d.db.Exec(`UPDATE workspace_invites SET accepted_at = $1 WHERE id = $2`, time.Now().UTC(), invite.ID); err != nil {
			return nil, fmt.Errorf("accept invite: %w", err)
		}
	}
	if _, err := d.SetActiveWorkspace(user.ID, invite.WorkspaceID); err != nil {
		return nil, err
	}
	return &invite, nil
}

// AcceptPendingInvites joins every open invite for this email.
// When one is accepted, that workspace becomes active.
func (d *Database) AcceptPendingInvites(user *models.User) error {
	invites, err := d.ListPendingInvites(user.Email)
	if err != nil {
		return err
	}
	for i := range invites {
		if _, err := d.AcceptInvite(invites[i].Token, user); err != nil {
			return err
		}
	}
	return nil
}

func (d *Database) SetMemberRole(workspaceID, actorID, memberID, role string) error {
	if !validRole(role) {
		return ErrBadRole
	}
	actorRole, err := d.memberRole(workspaceID, actorID)
	if err != nil {
		return err
	}
	if actorRole != string(models.RoleAdmin) {
		return ErrNotAdmin
	}
	current, err := d.memberRole(workspaceID, memberID)
	if err != nil {
		return err
	}
	if current == string(models.RoleAdmin) && role != string(models.RoleAdmin) {
		if err := d.requireAnotherAdmin(workspaceID, memberID); err != nil {
			return err
		}
	}
	_, err = d.db.Exec(`
		UPDATE workspace_members SET role = $1
		WHERE workspace_id = $2 AND user_id = $3
	`, role, workspaceID, memberID)
	return err
}

func (d *Database) RemoveMember(workspaceID, actorID, memberID string) error {
	actorRole, err := d.memberRole(workspaceID, actorID)
	if err != nil {
		return err
	}
	if actorRole != string(models.RoleAdmin) {
		return ErrNotAdmin
	}
	current, err := d.memberRole(workspaceID, memberID)
	if err != nil {
		return err
	}
	if current == string(models.RoleAdmin) {
		if err := d.requireAnotherAdmin(workspaceID, memberID); err != nil {
			return err
		}
	}
	_, err = d.db.Exec(`
		DELETE FROM workspace_members WHERE workspace_id = $1 AND user_id = $2
	`, workspaceID, memberID)
	if err != nil {
		return fmt.Errorf("remove member: %w", err)
	}
	_, err = d.db.Exec(`
		UPDATE users
		SET active_workspace_id = (
			SELECT workspace_id FROM workspace_members
			WHERE user_id = $1
			ORDER BY created_at, workspace_id
			LIMIT 1
		)
		WHERE id = $1 AND active_workspace_id = $2
	`, memberID, workspaceID)
	return err
}

func (d *Database) requireAnotherAdmin(workspaceID, exceptUserID string) error {
	var n int
	err := d.db.QueryRow(`
		SELECT COUNT(*) FROM workspace_members
		WHERE workspace_id = $1 AND role = 'admin' AND user_id <> $2
	`, workspaceID, exceptUserID).Scan(&n)
	if err != nil {
		return err
	}
	if n == 0 {
		return ErrLastAdmin
	}
	return nil
}

type ExportEvent struct {
	ID        string    `json:"id"`
	JobID     string    `json:"job_id"`
	JobName   string    `json:"job_name"`
	Type      string    `json:"type"`
	Data      string    `json:"data"`
	CreatedAt time.Time `json:"created_at"`
}

type ExportNotification struct {
	ID        string    `json:"id"`
	JobID     string    `json:"job_id"`
	Message   string    `json:"message"`
	Type      string    `json:"type"`
	Status    string    `json:"status"`
	CreatedAt time.Time `json:"created_at"`
}

type WorkspaceExport struct {
	Events        []ExportEvent        `json:"events"`
	Notifications []ExportNotification `json:"notifications"`
	Truncated     bool                 `json:"truncated"`
}

// ExportWorkspace returns events and notifications in [from, to).
func (d *Database) ExportWorkspace(workspaceID, userID string, from, to time.Time) (*WorkspaceExport, error) {
	if _, err := d.memberRole(workspaceID, userID); err != nil {
		return nil, err
	}
	out := &WorkspaceExport{
		Events:        make([]ExportEvent, 0),
		Notifications: make([]ExportNotification, 0),
	}
	eventRows, err := d.db.Query(`
		SELECT e.id, e.job_id, j.name, e.type, e.data::text, e.created_at
		FROM job_events e
		JOIN jobs j ON j.id = e.job_id
		WHERE j.workspace_id = $1 AND e.created_at >= $2 AND e.created_at < $3
		ORDER BY e.created_at, e.id
		LIMIT $4
	`, workspaceID, from, to, exportRowCap+1)
	if err != nil {
		return nil, fmt.Errorf("export events: %w", err)
	}
	defer eventRows.Close()
	for eventRows.Next() {
		var event ExportEvent
		if err := eventRows.Scan(&event.ID, &event.JobID, &event.JobName, &event.Type, &event.Data, &event.CreatedAt); err != nil {
			return nil, err
		}
		out.Events = append(out.Events, event)
	}
	if err := eventRows.Err(); err != nil {
		return nil, err
	}
	if len(out.Events) > exportRowCap {
		out.Truncated = true
		out.Events = out.Events[:exportRowCap]
	}

	noteRows, err := d.db.Query(`
		SELECT n.id, COALESCE(n.job_id, ''), n.message, n.type, n.status, n.created_at
		FROM notifications n
		JOIN jobs j ON j.id = n.job_id
		WHERE j.workspace_id = $1 AND n.created_at >= $2 AND n.created_at < $3
		ORDER BY n.created_at, n.id
		LIMIT $4
	`, workspaceID, from, to, exportRowCap+1)
	if err != nil {
		return nil, fmt.Errorf("export notifications: %w", err)
	}
	defer noteRows.Close()
	for noteRows.Next() {
		var note ExportNotification
		if err := noteRows.Scan(&note.ID, &note.JobID, &note.Message, &note.Type, &note.Status, &note.CreatedAt); err != nil {
			return nil, err
		}
		out.Notifications = append(out.Notifications, note)
	}
	if err := noteRows.Err(); err != nil {
		return nil, err
	}
	if len(out.Notifications) > exportRowCap {
		out.Truncated = true
		out.Notifications = out.Notifications[:exportRowCap]
	}
	return out, nil
}
