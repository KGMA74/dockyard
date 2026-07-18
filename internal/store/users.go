package store

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
)

// Roles a user can hold. Admin can do everything; pusher can pull and push;
// reader can only pull/browse.
const (
	RoleAdmin  = "admin"
	RolePusher = "pusher"
	RoleReader = "reader"
)

var ErrUserNotFound = errors.New("user not found")

func ValidRole(role string) bool {
	return role == RoleAdmin || role == RolePusher || role == RoleReader
}

type User struct {
	ID           int64    `json:"id"`
	Username     string   `json:"username"`
	PasswordHash string   `json:"-"`
	Role         string   `json:"role"`
	RepoPatterns []string `json:"repo_patterns"`
	CreatedAt    string   `json:"created_at"`
	UpdatedAt    string   `json:"updated_at"`
}

func (s *Store) CreateUser(username, passwordHash, role string, repoPatterns []string) (*User, error) {
	if !ValidRole(role) {
		return nil, fmt.Errorf("invalid role %q", role)
	}
	patterns, err := marshalPatterns(repoPatterns)
	if err != nil {
		return nil, err
	}
	res, err := s.db.Exec(
		`INSERT INTO users (username, password_hash, role, repo_patterns) VALUES (?, ?, ?, ?)`,
		username, passwordHash, role, patterns,
	)
	if err != nil {
		return nil, fmt.Errorf("create user %q: %w", username, err)
	}
	id, _ := res.LastInsertId()
	return s.userByID(id)
}

func (s *Store) GetUser(username string) (*User, error) {
	return s.scanUser(s.db.QueryRow(
		`SELECT id, username, password_hash, role, repo_patterns, created_at, updated_at
		 FROM users WHERE username = ?`, username,
	))
}

func (s *Store) ListUsers() ([]*User, error) {
	rows, err := s.db.Query(
		`SELECT id, username, password_hash, role, repo_patterns, created_at, updated_at
		 FROM users ORDER BY username`,
	)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	var users []*User
	for rows.Next() {
		u, err := s.scanUser(rows)
		if err != nil {
			return nil, err
		}
		users = append(users, u)
	}
	return users, rows.Err()
}

func (s *Store) CountUsers() (int, error) {
	var n int
	err := s.db.QueryRow(`SELECT COUNT(*) FROM users`).Scan(&n)
	return n, err
}

func (s *Store) countAdmins() (int, error) {
	var n int
	err := s.db.QueryRow(`SELECT COUNT(*) FROM users WHERE role = ?`, RoleAdmin).Scan(&n)
	return n, err
}

func (s *Store) UpdateUserPassword(username, passwordHash string) error {
	return s.execOnUser(
		`UPDATE users SET password_hash = ?, updated_at = strftime('%Y-%m-%dT%H:%M:%fZ', 'now') WHERE username = ?`,
		passwordHash, username,
	)
}

// UpdateUserAccess changes a user's role and repo patterns. Demoting the last
// admin is refused so the instance cannot lock itself out.
func (s *Store) UpdateUserAccess(username, role string, repoPatterns []string) error {
	if !ValidRole(role) {
		return fmt.Errorf("invalid role %q", role)
	}
	if role != RoleAdmin {
		if isLast, err := s.isLastAdmin(username); err != nil {
			return err
		} else if isLast {
			return errors.New("cannot demote the last admin")
		}
	}
	patterns, err := marshalPatterns(repoPatterns)
	if err != nil {
		return err
	}
	return s.execOnUser(
		`UPDATE users SET role = ?, repo_patterns = ?, updated_at = strftime('%Y-%m-%dT%H:%M:%fZ', 'now') WHERE username = ?`,
		role, patterns, username,
	)
}

// DeleteUser removes a user. Deleting the last admin is refused.
func (s *Store) DeleteUser(username string) error {
	if isLast, err := s.isLastAdmin(username); err != nil {
		return err
	} else if isLast {
		return errors.New("cannot delete the last admin")
	}
	return s.execOnUser(`DELETE FROM users WHERE username = ?`, username)
}

// ── internals ────────────────────────────────────────────────────────────────

func (s *Store) isLastAdmin(username string) (bool, error) {
	u, err := s.GetUser(username)
	if err != nil {
		return false, err
	}
	if u.Role != RoleAdmin {
		return false, nil
	}
	admins, err := s.countAdmins()
	if err != nil {
		return false, err
	}
	return admins <= 1, nil
}

func (s *Store) execOnUser(query string, args ...any) error {
	res, err := s.db.Exec(query, args...)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrUserNotFound
	}
	return nil
}

func (s *Store) userByID(id int64) (*User, error) {
	return s.scanUser(s.db.QueryRow(
		`SELECT id, username, password_hash, role, repo_patterns, created_at, updated_at
		 FROM users WHERE id = ?`, id,
	))
}

type rowScanner interface{ Scan(dest ...any) error }

func (s *Store) scanUser(row rowScanner) (*User, error) {
	var u User
	var patterns string
	err := row.Scan(&u.ID, &u.Username, &u.PasswordHash, &u.Role, &patterns, &u.CreatedAt, &u.UpdatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrUserNotFound
	}
	if err != nil {
		return nil, err
	}
	if err := json.Unmarshal([]byte(patterns), &u.RepoPatterns); err != nil {
		return nil, fmt.Errorf("user %q: bad repo_patterns: %w", u.Username, err)
	}
	if u.RepoPatterns == nil {
		u.RepoPatterns = []string{}
	}
	return &u, nil
}

func marshalPatterns(patterns []string) (string, error) {
	if patterns == nil {
		patterns = []string{}
	}
	raw, err := json.Marshal(patterns)
	if err != nil {
		return "", fmt.Errorf("marshal repo_patterns: %w", err)
	}
	return string(raw), nil
}
