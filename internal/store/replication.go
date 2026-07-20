package store

import (
	"database/sql"
	"errors"
	"time"
)

var ErrReplicationTargetNotFound = errors.New("replication target not found")

type ReplicationTarget struct {
	ID          int64  `json:"id"`
	Name        string `json:"name"`
	BaseURL     string `json:"base_url"`
	Username    string `json:"username"`
	Password    string `json:"-"`
	RepoPattern string `json:"repo_pattern"`
	Enabled     bool   `json:"enabled"`
	CreatedAt   string `json:"created_at"`
}

type ReplicationDelivery struct {
	ID       int64
	TargetID int64
	Repo     string
	Tag      string
	Attempts int
}

func (s *Store) CreateReplicationTarget(t ReplicationTarget) (*ReplicationTarget, error) {
	if t.Name == "" {
		return nil, errors.New("name is required")
	}
	if t.BaseURL == "" {
		return nil, errors.New("base_url is required")
	}
	if t.RepoPattern == "" {
		t.RepoPattern = "*"
	}
	res, err := s.db.Exec(
		`INSERT INTO replication_targets (name, base_url, username, password, repo_pattern, enabled) VALUES (?, ?, ?, ?, ?, ?)`,
		t.Name, t.BaseURL, t.Username, t.Password, t.RepoPattern, t.Enabled,
	)
	if err != nil {
		return nil, err
	}
	id, _ := res.LastInsertId()
	return s.replicationTargetByID(id)
}

func (s *Store) ListReplicationTargets() ([]*ReplicationTarget, error) {
	rows, err := s.db.Query(
		`SELECT id, name, base_url, username, password, repo_pattern, enabled, created_at FROM replication_targets ORDER BY id`,
	)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	var out []*ReplicationTarget
	for rows.Next() {
		t, err := scanReplicationTarget(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	return out, rows.Err()
}

func (s *Store) DeleteReplicationTarget(id int64) error {
	res, err := s.db.Exec(`DELETE FROM replication_targets WHERE id = ?`, id)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrReplicationTargetNotFound
	}
	return nil
}

func (s *Store) replicationTargetByID(id int64) (*ReplicationTarget, error) {
	t, err := scanReplicationTarget(s.db.QueryRow(
		`SELECT id, name, base_url, username, password, repo_pattern, enabled, created_at FROM replication_targets WHERE id = ?`, id,
	))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrReplicationTargetNotFound
	}
	return t, err
}

func (s *Store) ReplicationTargetByID(id int64) (*ReplicationTarget, error) {
	return s.replicationTargetByID(id)
}

func scanReplicationTarget(row rowScanner) (*ReplicationTarget, error) {
	var t ReplicationTarget
	if err := row.Scan(&t.ID, &t.Name, &t.BaseURL, &t.Username, &t.Password, &t.RepoPattern, &t.Enabled, &t.CreatedAt); err != nil {
		return nil, err
	}
	return &t, nil
}

// ── Delivery outbox ──────────────────────────────────────────────────────────

func (s *Store) EnqueueReplication(targetID int64, repo, tag string) error {
	_, err := s.db.Exec(
		`INSERT INTO replication_deliveries (target_id, repo, tag) VALUES (?, ?, ?)`,
		targetID, repo, tag,
	)
	return err
}

// DueReplications returns undelivered rows whose retry time has come,
// oldest first, capped to keep a delivery burst bounded.
func (s *Store) DueReplications(maxAttempts, limit int) ([]*ReplicationDelivery, error) {
	rows, err := s.db.Query(
		`SELECT id, target_id, repo, tag, attempts FROM replication_deliveries
		 WHERE delivered_at IS NULL AND attempts < ? AND next_attempt_at <= ?
		 ORDER BY id LIMIT ?`,
		maxAttempts, nowRFC3339(), limit,
	)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	var due []*ReplicationDelivery
	for rows.Next() {
		var d ReplicationDelivery
		if err := rows.Scan(&d.ID, &d.TargetID, &d.Repo, &d.Tag, &d.Attempts); err != nil {
			return nil, err
		}
		due = append(due, &d)
	}
	return due, rows.Err()
}

func (s *Store) MarkReplicationDelivered(deliveryID int64) error {
	_, err := s.db.Exec(
		`UPDATE replication_deliveries SET delivered_at = ?, attempts = attempts + 1 WHERE id = ?`,
		nowRFC3339(), deliveryID,
	)
	return err
}

func (s *Store) MarkReplicationFailed(deliveryID int64, retryAt time.Time, lastError string) error {
	_, err := s.db.Exec(
		`UPDATE replication_deliveries SET attempts = attempts + 1, next_attempt_at = ?, last_error = ? WHERE id = ?`,
		retryAt.UTC().Format(time.RFC3339), lastError, deliveryID,
	)
	return err
}
