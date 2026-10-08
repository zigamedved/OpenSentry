package db

import (
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/lib/pq"
	"github.com/zigamedved/OpenSentry/internal/models"
	"golang.org/x/crypto/bcrypt"
)

// ErrEmailTaken is returned when a registration email is already in use.
var ErrEmailTaken = errors.New("email already registered")

const (
	sessionTTL   = 14 * 24 * time.Hour
	demoUserID   = "test-user"
	demoEmail    = "test@example.com"
	demoName     = "Test User"
	demoPassword = "opensentry-demo"
)

// A valid hash so a missing account takes about as long as a wrong password.
var dummyPasswordHash = []byte("$2a$10$vI8aWBnW3fID.ZQ4/zo1G.q1lRps.9cGLcZEiGDMVr5yUP1KUOYTa")

func hashPassword(password string) (string, error) {
	hashed, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return "", fmt.Errorf("hash password: %w", err)
	}
	return string(hashed), nil
}

// CheckPassword reports whether password matches a bcrypt hash.
// An empty hash still runs a compare so unknown emails are not faster.
func CheckPassword(hash, password string) bool {
	if hash == "" {
		_ = bcrypt.CompareHashAndPassword(dummyPasswordHash, []byte(password))
		return false
	}
	return bcrypt.CompareHashAndPassword([]byte(hash), []byte(password)) == nil
}

func normalizeEmail(email string) string {
	return strings.ToLower(strings.TrimSpace(email))
}

func isUniqueViolation(err error) bool {
	var pqErr *pq.Error
	return errors.As(err, &pqErr) && pqErr.Code == "23505"
}

func (d *Database) CreateUser(email, name, password string) (*models.User, error) {
	now := time.Now().UTC()
	hashed, err := hashPassword(password)
	if err != nil {
		return nil, err
	}
	user := &models.User{
		ID:        uuid.New().String(),
		Email:     normalizeEmail(email),
		Name:      strings.TrimSpace(name),
		Password:  hashed,
		CreatedAt: now,
		UpdatedAt: now,
	}
	_, err = d.db.Exec(`
		INSERT INTO users (id, email, name, password_hash, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6)
	`, user.ID, user.Email, user.Name, user.Password, user.CreatedAt, user.UpdatedAt)
	if err != nil {
		if isUniqueViolation(err) {
			return nil, ErrEmailTaken
		}
		return nil, fmt.Errorf("create user: %w", err)
	}
	user.Password = ""
	return user, nil
}

func scanUser(row *sql.Row) (*models.User, error) {
	var user models.User
	err := row.Scan(&user.ID, &user.Email, &user.Name, &user.Password, &user.CreatedAt, &user.UpdatedAt)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &user, nil
}

const userSelect = `
	SELECT id, email, name, password_hash, created_at, updated_at
	FROM users
`

func (d *Database) UserByEmail(email string) (*models.User, error) {
	user, err := scanUser(d.db.QueryRow(userSelect+` WHERE email = $1`, normalizeEmail(email)))
	if err != nil {
		return nil, fmt.Errorf("user by email: %w", err)
	}
	return user, nil
}

func (d *Database) UserByID(id string) (*models.User, error) {
	user, err := scanUser(d.db.QueryRow(userSelect+` WHERE id = $1`, id))
	if err != nil {
		return nil, fmt.Errorf("user by id: %w", err)
	}
	return user, nil
}

func newSessionToken() (string, error) {
	buf := make([]byte, 32)
	if _, err := rand.Read(buf); err != nil {
		return "", fmt.Errorf("session token: %w", err)
	}
	return hex.EncodeToString(buf), nil
}

func tokenHash(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}

// CreateSession stores a hashed opaque token and returns the raw token.
func (d *Database) CreateSession(userID string) (string, time.Time, error) {
	token, err := newSessionToken()
	if err != nil {
		return "", time.Time{}, err
	}
	now := time.Now().UTC()
	expires := now.Add(sessionTTL)
	_, err = d.db.Exec(`
		INSERT INTO sessions (id, user_id, token_hash, expires_at, created_at)
		VALUES ($1, $2, $3, $4, $5)
	`, uuid.New().String(), userID, tokenHash(token), expires, now)
	if err != nil {
		return "", time.Time{}, fmt.Errorf("create session: %w", err)
	}
	return token, expires, nil
}

// UserBySessionToken returns the user for an unexpired session token.
// A missing or expired token is (nil, nil).
func (d *Database) UserBySessionToken(token string) (*models.User, error) {
	if strings.TrimSpace(token) == "" {
		return nil, nil
	}
	var user models.User
	err := d.db.QueryRow(`
		SELECT u.id, u.email, u.name, u.password_hash, u.created_at, u.updated_at
		FROM sessions s
		JOIN users u ON u.id = s.user_id
		WHERE s.token_hash = $1 AND s.expires_at > NOW()
	`, tokenHash(token)).Scan(&user.ID, &user.Email, &user.Name, &user.Password, &user.CreatedAt, &user.UpdatedAt)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("session lookup: %w", err)
	}
	return &user, nil
}

func (d *Database) DeleteSession(token string) error {
	if strings.TrimSpace(token) == "" {
		return nil
	}
	_, err := d.db.Exec(`DELETE FROM sessions WHERE token_hash = $1`, tokenHash(token))
	if err != nil {
		return fmt.Errorf("delete session: %w", err)
	}
	return nil
}

// SeedDemoUser creates the local demo account when it is missing.
// The returned id is the account that env webhooks should attach to.
func (d *Database) SeedDemoUser() (string, error) {
	existing, err := d.UserByEmail(demoEmail)
	if err != nil {
		return "", err
	}
	if existing != nil {
		return existing.ID, nil
	}
	hashed, err := hashPassword(demoPassword)
	if err != nil {
		return "", err
	}
	now := time.Now().UTC()
	_, err = d.db.Exec(`
		INSERT INTO users (id, email, name, password_hash, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6)
		ON CONFLICT (id) DO NOTHING
	`, demoUserID, demoEmail, demoName, hashed, now, now)
	if err != nil {
		return "", fmt.Errorf("seed demo user: %w", err)
	}
	existing, err = d.UserByID(demoUserID)
	if err != nil {
		return "", err
	}
	if existing == nil {
		return "", fmt.Errorf("seed demo user: test-user was not created")
	}
	return existing.ID, nil
}
