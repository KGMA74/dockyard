package store

import (
	"database/sql"
	"errors"
	"time"
)

var ErrSessionNotFound = errors.New("session not found")

// Session is a refresh-token-backed login. The refresh token itself is never
// stored — only its SHA-256 hash.
type Session struct {
	ID         int64  `json:"id"`
	UserID     int64  `json:"-"`
	Username   string `json:"username"`
	UserAgent  string `json:"user_agent"`
	IP         string `json:"ip"`
	CreatedAt  string `json:"created_at"`
	LastSeenAt string `json:"last_seen_at"`
	ExpiresAt  string `json:"expires_at"`
}

func nowRFC3339() string { return time.Now().UTC().Format(time.RFC3339) }

func (s *Store) CreateSession(userID int64, refreshHash, userAgent, ip string, expiresAt time.Time) (int64, error) {
	res, err := s.db.Exec(
		`INSERT INTO sessions (user_id, refresh_token_hash, user_agent, ip, expires_at) VALUES (?, ?, ?, ?, ?)`,
		userID, refreshHash, userAgent, ip, expiresAt.UTC().Format(time.RFC3339),
	)
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

// SessionByRefreshHash resolves a non-expired session from a refresh token
// hash, together with its user.
func (s *Store) SessionByRefreshHash(refreshHash string) (*Session, *User, error) {
	row := s.db.QueryRow(
		`SELECT s.id, s.user_id, u.username, s.user_agent, s.ip, s.created_at, s.last_seen_at, s.expires_at
		 FROM sessions s JOIN users u ON u.id = s.user_id
		 WHERE s.refresh_token_hash = ? AND s.expires_at > ?`,
		refreshHash, nowRFC3339(),
	)
	var sess Session
	err := row.Scan(&sess.ID, &sess.UserID, &sess.Username, &sess.UserAgent, &sess.IP,
		&sess.CreatedAt, &sess.LastSeenAt, &sess.ExpiresAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil, ErrSessionNotFound
	}
	if err != nil {
		return nil, nil, err
	}
	u, err := s.userByID(sess.UserID)
	if err != nil {
		return nil, nil, err
	}
	return &sess, u, nil
}

// RotateSessionRefresh swaps the refresh token hash (single-use refresh
// tokens), extends the session and bumps last_seen.
func (s *Store) RotateSessionRefresh(sessionID int64, newRefreshHash string, expiresAt time.Time) error {
	res, err := s.db.Exec(
		`UPDATE sessions SET refresh_token_hash = ?, expires_at = ?, last_seen_at = ? WHERE id = ?`,
		newRefreshHash, expiresAt.UTC().Format(time.RFC3339), nowRFC3339(), sessionID,
	)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrSessionNotFound
	}
	return nil
}

func (s *Store) ListSessions() ([]*Session, error) {
	rows, err := s.db.Query(
		`SELECT s.id, s.user_id, u.username, s.user_agent, s.ip, s.created_at, s.last_seen_at, s.expires_at
		 FROM sessions s JOIN users u ON u.id = s.user_id
		 WHERE s.expires_at > ?
		 ORDER BY s.last_seen_at DESC`, nowRFC3339(),
	)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	var sessions []*Session
	for rows.Next() {
		var sess Session
		if err := rows.Scan(&sess.ID, &sess.UserID, &sess.Username, &sess.UserAgent, &sess.IP,
			&sess.CreatedAt, &sess.LastSeenAt, &sess.ExpiresAt); err != nil {
			return nil, err
		}
		sessions = append(sessions, &sess)
	}
	return sessions, rows.Err()
}

func (s *Store) DeleteSession(sessionID int64) error {
	res, err := s.db.Exec(`DELETE FROM sessions WHERE id = ?`, sessionID)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrSessionNotFound
	}
	return nil
}

// ── Revoked access tokens (persisted logout blacklist) ───────────────────────

// RevokeToken records an access token hash until its natural expiry.
func (s *Store) RevokeToken(tokenHash string, expiresAt time.Time) error {
	_, err := s.db.Exec(
		`INSERT OR REPLACE INTO revoked_tokens (token_hash, expires_at) VALUES (?, ?)`,
		tokenHash, expiresAt.UTC().Format(time.RFC3339),
	)
	return err
}

func (s *Store) IsTokenRevoked(tokenHash string) (bool, error) {
	var n int
	err := s.db.QueryRow(
		`SELECT COUNT(*) FROM revoked_tokens WHERE token_hash = ?`, tokenHash,
	).Scan(&n)
	return n > 0, err
}

// PruneExpired drops expired sessions and spent revocation entries. Called
// periodically by the auth manager.
func (s *Store) PruneExpired() error {
	now := nowRFC3339()
	if _, err := s.db.Exec(`DELETE FROM sessions WHERE expires_at <= ?`, now); err != nil {
		return err
	}
	_, err := s.db.Exec(`DELETE FROM revoked_tokens WHERE expires_at <= ?`, now)
	return err
}
