package store

import (
	"os"
	"path/filepath"
	"slices"
	"testing"
)

func openTemp(t *testing.T) (*Store, string) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "data", "dockyard.db")
	s, err := Open(path)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { _ = s.Close() })
	return s, path
}

func TestOpenCreatesSchema(t *testing.T) {
	s, path := openTemp(t)

	if _, err := os.Stat(path); err != nil {
		t.Fatalf("database file not created: %v", err)
	}

	rows, err := s.DB().Query(`SELECT name FROM sqlite_master WHERE type = 'table' ORDER BY name`)
	if err != nil {
		t.Fatalf("query sqlite_master: %v", err)
	}
	defer func() { _ = rows.Close() }()
	var tables []string
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			t.Fatal(err)
		}
		tables = append(tables, name)
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"users", "sessions", "revoked_tokens", "audit_log", "schema_migrations"} {
		if !slices.Contains(tables, want) {
			t.Errorf("missing table %q, got %v", want, tables)
		}
	}

	v, err := s.Version()
	if err != nil {
		t.Fatalf("Version: %v", err)
	}
	if v != 1 {
		t.Errorf("schema version = %d, want 1", v)
	}
}

func TestReopenIsIdempotent(t *testing.T) {
	path := filepath.Join(t.TempDir(), "dockyard.db")
	s1, err := Open(path)
	if err != nil {
		t.Fatalf("first Open: %v", err)
	}
	if _, err := s1.DB().Exec(
		`INSERT INTO users (username, password_hash, role) VALUES ('admin', 'x', 'admin')`,
	); err != nil {
		t.Fatalf("insert user: %v", err)
	}
	if err := s1.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}

	s2, err := Open(path)
	if err != nil {
		t.Fatalf("second Open: %v", err)
	}
	defer func() { _ = s2.Close() }()
	var count int
	if err := s2.DB().QueryRow(`SELECT COUNT(*) FROM users`).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Errorf("user count after reopen = %d, want 1 (data lost or migration re-ran)", count)
	}
	if v, _ := s2.Version(); v != 1 {
		t.Errorf("schema version after reopen = %d, want 1", v)
	}
}

func TestForeignKeysEnforced(t *testing.T) {
	s, _ := openTemp(t)
	_, err := s.DB().Exec(
		`INSERT INTO sessions (user_id, refresh_token_hash, expires_at) VALUES (999, 'h', '2030-01-01T00:00:00Z')`,
	)
	if err == nil {
		t.Fatal("session insert with unknown user_id succeeded, want FK violation")
	}
}

func TestSessionCascadeOnUserDelete(t *testing.T) {
	s, _ := openTemp(t)
	res, err := s.DB().Exec(`INSERT INTO users (username, password_hash) VALUES ('bob', 'x')`)
	if err != nil {
		t.Fatal(err)
	}
	uid, _ := res.LastInsertId()
	if _, err := s.DB().Exec(
		`INSERT INTO sessions (user_id, refresh_token_hash, expires_at) VALUES (?, 'h', '2030-01-01T00:00:00Z')`, uid,
	); err != nil {
		t.Fatal(err)
	}
	if _, err := s.DB().Exec(`DELETE FROM users WHERE id = ?`, uid); err != nil {
		t.Fatal(err)
	}
	var count int
	if err := s.DB().QueryRow(`SELECT COUNT(*) FROM sessions`).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 0 {
		t.Errorf("sessions not cascaded on user delete: %d remaining", count)
	}
}

func TestRoleConstraint(t *testing.T) {
	s, _ := openTemp(t)
	if _, err := s.DB().Exec(
		`INSERT INTO users (username, password_hash, role) VALUES ('eve', 'x', 'superadmin')`,
	); err == nil {
		t.Fatal("insert with invalid role succeeded, want CHECK violation")
	}
}
