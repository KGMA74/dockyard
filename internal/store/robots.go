package store

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"
)

var ErrRobotNotFound = errors.New("robot account not found")

// Robot is a non-human registry credential (CI pipeline, cluster pull secret).
type Robot struct {
	ID           int64    `json:"id"`
	Name         string   `json:"name"`
	Description  string   `json:"description"`
	Role         string   `json:"role"`
	RepoPatterns []string `json:"repo_patterns"`
	SecretHash   string   `json:"-"`
	CreatedBy    string   `json:"created_by"`
	CreatedAt    string   `json:"created_at"`
	ExpiresAt    *string  `json:"expires_at"`
	LastUsedAt   *string  `json:"last_used_at"`
}

// Expired reports whether the robot's expiry date has passed.
func (r *Robot) Expired(now time.Time) bool {
	if r.ExpiresAt == nil {
		return false
	}
	t, err := time.Parse(time.RFC3339, *r.ExpiresAt)
	return err != nil || !now.Before(t)
}

const robotColumns = `id, name, description, role, repo_patterns, secret_hash, created_by, created_at, expires_at, last_used_at`

func formatExpiry(expiresAt *time.Time) any {
	if expiresAt == nil {
		return nil
	}
	return expiresAt.UTC().Format(time.RFC3339)
}

func (s *Store) CreateRobot(name, description, role string, repoPatterns []string, secretHash, createdBy string, expiresAt *time.Time) (*Robot, error) {
	if role != RolePusher && role != RoleReader {
		return nil, fmt.Errorf("invalid robot role %q", role)
	}
	patterns, err := marshalPatterns(repoPatterns)
	if err != nil {
		return nil, err
	}
	res, err := s.db.Exec(
		`INSERT INTO robots (name, description, role, repo_patterns, secret_hash, created_by, expires_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?)`,
		name, description, role, patterns, secretHash, createdBy, formatExpiry(expiresAt),
	)
	if err != nil {
		return nil, fmt.Errorf("create robot %q: %w", name, err)
	}
	id, _ := res.LastInsertId()
	return s.RobotByID(id)
}

func (s *Store) ListRobots() ([]*Robot, error) {
	rows, err := s.db.Query(`SELECT ` + robotColumns + ` FROM robots ORDER BY name`)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	robots := []*Robot{}
	for rows.Next() {
		r, err := scanRobot(rows)
		if err != nil {
			return nil, err
		}
		robots = append(robots, r)
	}
	return robots, rows.Err()
}

func (s *Store) RobotByID(id int64) (*Robot, error) {
	return scanRobot(s.db.QueryRow(`SELECT `+robotColumns+` FROM robots WHERE id = ?`, id))
}

func (s *Store) RobotByName(name string) (*Robot, error) {
	return scanRobot(s.db.QueryRow(`SELECT `+robotColumns+` FROM robots WHERE name = ?`, name))
}

// RotateRobotSecret replaces the secret (the old one stops working at once)
// and sets a new expiry.
func (s *Store) RotateRobotSecret(id int64, secretHash string, expiresAt *time.Time) error {
	return s.execOnRobot(`UPDATE robots SET secret_hash = ?, expires_at = ? WHERE id = ?`,
		secretHash, formatExpiry(expiresAt), id)
}

func (s *Store) DeleteRobot(id int64) error {
	return s.execOnRobot(`DELETE FROM robots WHERE id = ?`, id)
}

// TouchRobot records a use. At most one write a minute per robot: Basic-auth
// clients authenticate every request, and a docker push is hundreds of them.
func (s *Store) TouchRobot(id int64) error {
	now := time.Now().UTC()
	_, err := s.db.Exec(
		`UPDATE robots SET last_used_at = ? WHERE id = ? AND (last_used_at IS NULL OR last_used_at < ?)`,
		now.Format(time.RFC3339), id, now.Add(-time.Minute).Format(time.RFC3339),
	)
	return err
}

func (s *Store) execOnRobot(query string, args ...any) error {
	res, err := s.db.Exec(query, args...)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrRobotNotFound
	}
	return nil
}

func scanRobot(row rowScanner) (*Robot, error) {
	var r Robot
	var patterns string
	err := row.Scan(&r.ID, &r.Name, &r.Description, &r.Role, &patterns, &r.SecretHash,
		&r.CreatedBy, &r.CreatedAt, &r.ExpiresAt, &r.LastUsedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrRobotNotFound
	}
	if err != nil {
		return nil, err
	}
	if err := json.Unmarshal([]byte(patterns), &r.RepoPatterns); err != nil {
		return nil, fmt.Errorf("robot %q: bad repo_patterns: %w", r.Name, err)
	}
	if r.RepoPatterns == nil {
		r.RepoPatterns = []string{}
	}
	return &r, nil
}
