package store

import (
	"database/sql"
	"errors"
)

var ErrSigningPolicyNotFound = errors.New("signing policy not found")

type SigningPolicy struct {
	ID          int64  `json:"id"`
	RepoPattern string `json:"repo_pattern"`
	Required    bool   `json:"required"`
	CreatedAt   string `json:"created_at"`
}

func (s *Store) CreateSigningPolicy(repoPattern string, required bool) (*SigningPolicy, error) {
	if repoPattern == "" {
		repoPattern = "*"
	}
	res, err := s.db.Exec(
		`INSERT INTO signing_policies (repo_pattern, required) VALUES (?, ?)`,
		repoPattern, required,
	)
	if err != nil {
		return nil, err
	}
	id, _ := res.LastInsertId()
	return s.signingPolicyByID(id)
}

func (s *Store) ListSigningPolicies() ([]*SigningPolicy, error) {
	rows, err := s.db.Query(`SELECT id, repo_pattern, required, created_at FROM signing_policies ORDER BY id`)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	var out []*SigningPolicy
	for rows.Next() {
		p, err := scanSigningPolicy(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

func (s *Store) DeleteSigningPolicy(id int64) error {
	res, err := s.db.Exec(`DELETE FROM signing_policies WHERE id = ?`, id)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrSigningPolicyNotFound
	}
	return nil
}

func (s *Store) signingPolicyByID(id int64) (*SigningPolicy, error) {
	p, err := scanSigningPolicy(s.db.QueryRow(
		`SELECT id, repo_pattern, required, created_at FROM signing_policies WHERE id = ?`, id,
	))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrSigningPolicyNotFound
	}
	return p, err
}

func scanSigningPolicy(row rowScanner) (*SigningPolicy, error) {
	var p SigningPolicy
	if err := row.Scan(&p.ID, &p.RepoPattern, &p.Required, &p.CreatedAt); err != nil {
		return nil, err
	}
	return &p, nil
}
