package store

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
)

var ErrPolicyNotFound = errors.New("retention policy not found")

type RetentionPolicy struct {
	ID            int64    `json:"id"`
	RepoPattern   string   `json:"repo_pattern"`
	KeepN         int      `json:"keep_n"`
	UnpulledDays  int      `json:"unpulled_days"`
	KeepPatterns  []string `json:"keep_patterns"`
	ProtectedTags []string `json:"protected_tags"`
	Enabled       bool     `json:"enabled"`
	CreatedAt     string   `json:"created_at"`
}

func (s *Store) CreateRetentionPolicy(p RetentionPolicy) (*RetentionPolicy, error) {
	if p.RepoPattern == "" {
		p.RepoPattern = "*"
	}
	if p.KeepN <= 0 && p.UnpulledDays <= 0 {
		return nil, errors.New("policy needs keep_n and/or unpulled_days")
	}
	keep, err := marshalPatterns(p.KeepPatterns)
	if err != nil {
		return nil, err
	}
	protected, err := marshalPatterns(p.ProtectedTags)
	if err != nil {
		return nil, err
	}
	res, err := s.db.Exec(
		`INSERT INTO retention_policies (repo_pattern, keep_n, unpulled_days, keep_patterns, protected_tags, enabled)
		 VALUES (?, ?, ?, ?, ?, ?)`,
		p.RepoPattern, p.KeepN, p.UnpulledDays, keep, protected, p.Enabled,
	)
	if err != nil {
		return nil, err
	}
	id, _ := res.LastInsertId()
	return s.retentionPolicyByID(id)
}

func (s *Store) ListRetentionPolicies() ([]*RetentionPolicy, error) {
	rows, err := s.db.Query(
		`SELECT id, repo_pattern, keep_n, unpulled_days, keep_patterns, protected_tags, enabled, created_at
		 FROM retention_policies ORDER BY id`,
	)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	var policies []*RetentionPolicy
	for rows.Next() {
		p, err := scanRetentionPolicy(rows)
		if err != nil {
			return nil, err
		}
		policies = append(policies, p)
	}
	return policies, rows.Err()
}

func (s *Store) DeleteRetentionPolicy(id int64) error {
	res, err := s.db.Exec(`DELETE FROM retention_policies WHERE id = ?`, id)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrPolicyNotFound
	}
	return nil
}

func (s *Store) retentionPolicyByID(id int64) (*RetentionPolicy, error) {
	row := s.db.QueryRow(
		`SELECT id, repo_pattern, keep_n, unpulled_days, keep_patterns, protected_tags, enabled, created_at
		 FROM retention_policies WHERE id = ?`, id,
	)
	return scanRetentionPolicy(row)
}

func scanRetentionPolicy(row rowScanner) (*RetentionPolicy, error) {
	var p RetentionPolicy
	var keep, protected string
	err := row.Scan(&p.ID, &p.RepoPattern, &p.KeepN, &p.UnpulledDays, &keep, &protected, &p.Enabled, &p.CreatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrPolicyNotFound
	}
	if err != nil {
		return nil, err
	}
	if err := json.Unmarshal([]byte(keep), &p.KeepPatterns); err != nil {
		return nil, fmt.Errorf("policy %d: bad keep_patterns: %w", p.ID, err)
	}
	if err := json.Unmarshal([]byte(protected), &p.ProtectedTags); err != nil {
		return nil, fmt.Errorf("policy %d: bad protected_tags: %w", p.ID, err)
	}
	if p.KeepPatterns == nil {
		p.KeepPatterns = []string{}
	}
	if p.ProtectedTags == nil {
		p.ProtectedTags = []string{}
	}
	return &p, nil
}
