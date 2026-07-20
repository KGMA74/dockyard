package store

import (
	"database/sql"
	"errors"
	"fmt"
)

var ErrQuotaNotFound = errors.New("quota not found")

type Quota struct {
	ID          int64  `json:"id"`
	ScopeType   string `json:"scope_type"` // "repo" or "user"
	ScopeValue  string `json:"scope_value"`
	MaxBytes    int64  `json:"max_bytes"`
	WarnPercent int    `json:"warn_percent"`
	CreatedAt   string `json:"created_at"`
}

type QuotaUsage struct {
	ScopeType  string `json:"scope_type"`
	ScopeValue string `json:"scope_value"`
	BytesUsed  int64  `json:"bytes_used"`
}

// QuotaScope identifies one side (repo or user) of a push to check/reserve
// quota against.
type QuotaScope struct {
	Type  string
	Value string
}

// QuotaExceeded reports which scope would be pushed over its configured
// limit.
type QuotaExceeded struct {
	ScopeType  string
	ScopeValue string
	UsedBytes  int64
	MaxBytes   int64
}

func (e *QuotaExceeded) Error() string {
	return fmt.Sprintf("quota exceeded for %s %q: %d + incoming bytes would exceed limit of %d",
		e.ScopeType, e.ScopeValue, e.UsedBytes, e.MaxBytes)
}

// QuotaWarning reports a scope that just crossed its warn threshold as part
// of a reservation.
type QuotaWarning struct {
	ScopeType  string
	ScopeValue string
	UsedBytes  int64
	MaxBytes   int64
	Percent    int
}

func (s *Store) SetQuota(scopeType, scopeValue string, maxBytes int64, warnPercent int) (*Quota, error) {
	if scopeType != "repo" && scopeType != "user" {
		return nil, fmt.Errorf("invalid scope_type %q, must be repo or user", scopeType)
	}
	if scopeValue == "" {
		return nil, errors.New("scope_value is required")
	}
	if maxBytes <= 0 {
		return nil, errors.New("max_bytes must be positive")
	}
	if warnPercent <= 0 || warnPercent > 100 {
		warnPercent = 90
	}
	_, err := s.db.Exec(`
		INSERT INTO quotas (scope_type, scope_value, max_bytes, warn_percent)
		VALUES (?, ?, ?, ?)
		ON CONFLICT(scope_type, scope_value) DO UPDATE SET
			max_bytes = excluded.max_bytes,
			warn_percent = excluded.warn_percent`,
		scopeType, scopeValue, maxBytes, warnPercent,
	)
	if err != nil {
		return nil, err
	}
	return s.quotaByScope(scopeType, scopeValue)
}

func (s *Store) ListQuotas() ([]*Quota, error) {
	rows, err := s.db.Query(`SELECT id, scope_type, scope_value, max_bytes, warn_percent, created_at FROM quotas ORDER BY scope_type, scope_value`)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	var out []*Quota
	for rows.Next() {
		q, err := scanQuota(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, q)
	}
	return out, rows.Err()
}

func (s *Store) DeleteQuota(id int64) error {
	res, err := s.db.Exec(`DELETE FROM quotas WHERE id = ?`, id)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrQuotaNotFound
	}
	return nil
}

// ListQuotaUsage returns current usage for every scope that has ever had
// bytes reserved against it (including scopes with no quota configured).
func (s *Store) ListQuotaUsage() ([]*QuotaUsage, error) {
	rows, err := s.db.Query(`SELECT scope_type, scope_value, bytes_used FROM quota_usage ORDER BY scope_type, scope_value`)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	var out []*QuotaUsage
	for rows.Next() {
		var u QuotaUsage
		if err := rows.Scan(&u.ScopeType, &u.ScopeValue, &u.BytesUsed); err != nil {
			return nil, err
		}
		out = append(out, &u)
	}
	return out, rows.Err()
}

// ResetQuotaUsage zeroes the recorded usage for one scope (e.g. after an
// admin manually clears out a repo, or to start a new billing period).
func (s *Store) ResetQuotaUsage(scopeType, scopeValue string) error {
	_, err := s.db.Exec(`DELETE FROM quota_usage WHERE scope_type = ? AND scope_value = ?`, scopeType, scopeValue)
	return err
}

// ReserveQuota checks every given scope against its configured quota (scopes
// with no quota row are unlimited) and, only if all of them have room for
// size more bytes, atomically records the increment against each and
// returns any scopes that just crossed their warn threshold. If any scope
// would be pushed over its limit, nothing is recorded and the first
// offending scope is returned.
func (s *Store) ReserveQuota(size int64, scopes ...QuotaScope) (*QuotaExceeded, []QuotaWarning, error) {
	if size <= 0 || len(scopes) == 0 {
		return nil, nil, nil
	}
	tx, err := s.db.Begin()
	if err != nil {
		return nil, nil, err
	}
	defer func() { _ = tx.Rollback() }()

	type limit struct {
		hasQuota bool
		max      int64
		warnPct  int
		current  int64
	}
	limits := make(map[QuotaScope]limit, len(scopes))

	for _, sc := range scopes {
		if sc.Value == "" {
			continue
		}
		var l limit
		err := tx.QueryRow(`SELECT max_bytes, warn_percent FROM quotas WHERE scope_type = ? AND scope_value = ?`, sc.Type, sc.Value).
			Scan(&l.max, &l.warnPct)
		switch {
		case errors.Is(err, sql.ErrNoRows):
			// unlimited — still track usage below.
		case err != nil:
			return nil, nil, err
		default:
			l.hasQuota = true
		}

		err = tx.QueryRow(`SELECT bytes_used FROM quota_usage WHERE scope_type = ? AND scope_value = ?`, sc.Type, sc.Value).Scan(&l.current)
		if err != nil && !errors.Is(err, sql.ErrNoRows) {
			return nil, nil, err
		}

		if l.hasQuota && l.current+size > l.max {
			return &QuotaExceeded{ScopeType: sc.Type, ScopeValue: sc.Value, UsedBytes: l.current, MaxBytes: l.max}, nil, nil
		}
		limits[sc] = l
	}

	var warnings []QuotaWarning
	for _, sc := range scopes {
		if sc.Value == "" {
			continue
		}
		l := limits[sc]
		newTotal := l.current + size
		_, err := tx.Exec(`
			INSERT INTO quota_usage (scope_type, scope_value, bytes_used) VALUES (?, ?, ?)
			ON CONFLICT(scope_type, scope_value) DO UPDATE SET bytes_used = bytes_used + excluded.bytes_used`,
			sc.Type, sc.Value, size,
		)
		if err != nil {
			return nil, nil, err
		}
		if l.hasQuota {
			oldPct := int(l.current * 100 / l.max)
			newPct := int(newTotal * 100 / l.max)
			if oldPct < l.warnPct && newPct >= l.warnPct {
				warnings = append(warnings, QuotaWarning{ScopeType: sc.Type, ScopeValue: sc.Value, UsedBytes: newTotal, MaxBytes: l.max, Percent: newPct})
			}
		}
	}

	if err := tx.Commit(); err != nil {
		return nil, nil, err
	}
	return nil, warnings, nil
}

func (s *Store) quotaByScope(scopeType, scopeValue string) (*Quota, error) {
	q, err := scanQuota(s.db.QueryRow(
		`SELECT id, scope_type, scope_value, max_bytes, warn_percent, created_at FROM quotas WHERE scope_type = ? AND scope_value = ?`,
		scopeType, scopeValue,
	))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrQuotaNotFound
	}
	return q, err
}

func scanQuota(row rowScanner) (*Quota, error) {
	var q Quota
	if err := row.Scan(&q.ID, &q.ScopeType, &q.ScopeValue, &q.MaxBytes, &q.WarnPercent, &q.CreatedAt); err != nil {
		return nil, err
	}
	return &q, nil
}
