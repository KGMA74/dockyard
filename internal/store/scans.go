package store

import (
	"bytes"
	"compress/gzip"
	"database/sql"
	"errors"
	"io"
)

var ErrScanNotFound = errors.New("scan not found")

type ScanResult struct {
	ID            int64   `json:"id"`
	Name          string  `json:"name"`
	Reference     string  `json:"reference"`
	Digest        string  `json:"digest"`
	Status        string  `json:"status"`
	RequestedBy   string  `json:"requested_by"`
	TrivyVersion  string  `json:"trivy_version"`
	CriticalCount int     `json:"critical_count"`
	HighCount     int     `json:"high_count"`
	MediumCount   int     `json:"medium_count"`
	LowCount      int     `json:"low_count"`
	UnknownCount  int     `json:"unknown_count"`
	Error         string  `json:"error"`
	StartedAt     *string `json:"started_at,omitempty"`
	FinishedAt    *string `json:"finished_at,omitempty"`
	CreatedAt     string  `json:"created_at"`
}

const scanColumns = `id, name, reference, digest, status, requested_by, trivy_version,
	critical_count, high_count, medium_count, low_count, unknown_count,
	error, started_at, finished_at, created_at`

func (s *Store) EnqueueScan(name, reference, digest, requestedBy string) (*ScanResult, error) {
	res, err := s.db.Exec(
		`INSERT INTO scans (name, reference, digest, status, requested_by) VALUES (?, ?, ?, 'queued', ?)`,
		name, reference, digest, requestedBy,
	)
	if err != nil {
		return nil, err
	}
	id, _ := res.LastInsertId()
	return s.ScanByID(id)
}

// NextQueuedScan returns the oldest queued scan, or nil if the queue is empty.
func (s *Store) NextQueuedScan() (*ScanResult, error) {
	sc, err := scanScanRow(s.db.QueryRow(
		`SELECT ` + scanColumns + ` FROM scans WHERE status = 'queued' ORDER BY id LIMIT 1`,
	))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	return sc, err
}

func (s *Store) MarkScanRunning(id int64) error {
	_, err := s.db.Exec(
		`UPDATE scans SET status = 'running', started_at = ? WHERE id = ?`,
		nowRFC3339(), id,
	)
	return err
}

func (s *Store) MarkScanSucceeded(id int64, critical, high, medium, low, unknown int, reportGz []byte, trivyVersion string) error {
	_, err := s.db.Exec(
		`UPDATE scans SET status = 'succeeded', finished_at = ?, trivy_version = ?,
		 critical_count = ?, high_count = ?, medium_count = ?, low_count = ?, unknown_count = ?,
		 report_json = ? WHERE id = ?`,
		nowRFC3339(), trivyVersion, critical, high, medium, low, unknown, reportGz, id,
	)
	return err
}

func (s *Store) MarkScanFailed(id int64, errMsg string) error {
	const maxErrLen = 4096
	if len(errMsg) > maxErrLen {
		errMsg = errMsg[:maxErrLen]
	}
	_, err := s.db.Exec(
		`UPDATE scans SET status = 'failed', finished_at = ?, error = ? WHERE id = ?`,
		nowRFC3339(), errMsg, id,
	)
	return err
}

// ListScans returns scans newest-first, optionally filtered by repository
// name and/or digest.
func (s *Store) ListScans(name, digest string, limit, offset int) ([]*ScanResult, int, error) {
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	where := "WHERE 1=1"
	args := []any{}
	if name != "" {
		where += " AND name = ?"
		args = append(args, name)
	}
	if digest != "" {
		where += " AND digest = ?"
		args = append(args, digest)
	}

	var total int
	if err := s.db.QueryRow(`SELECT COUNT(*) FROM scans `+where, args...).Scan(&total); err != nil {
		return nil, 0, err
	}

	rows, err := s.db.Query(
		`SELECT `+scanColumns+` FROM scans `+where+` ORDER BY id DESC LIMIT ? OFFSET ?`,
		append(args, limit, offset)...,
	)
	if err != nil {
		return nil, 0, err
	}
	defer func() { _ = rows.Close() }()
	var out []*ScanResult
	for rows.Next() {
		sc, err := scanScanRow(rows)
		if err != nil {
			return nil, 0, err
		}
		out = append(out, sc)
	}
	return out, total, rows.Err()
}

func (s *Store) ScanByID(id int64) (*ScanResult, error) {
	sc, err := scanScanRow(s.db.QueryRow(`SELECT `+scanColumns+` FROM scans WHERE id = ?`, id))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrScanNotFound
	}
	return sc, err
}

// LatestScanForDigest returns the most recent successful scan for a digest,
// used both by the UI and by the dispatcher's dedup guard.
func (s *Store) LatestScanForDigest(digest string) (*ScanResult, error) {
	sc, err := scanScanRow(s.db.QueryRow(
		`SELECT `+scanColumns+` FROM scans WHERE digest = ? AND status = 'succeeded' ORDER BY id DESC LIMIT 1`,
		digest,
	))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrScanNotFound
	}
	return sc, err
}

// ScanReport returns the decompressed trivy report JSON for a scan. Kept
// separate from ScanByID so list/get queries never pull the (potentially
// multi-MB) blob.
func (s *Store) ScanReport(id int64) ([]byte, error) {
	var gz []byte
	err := s.db.QueryRow(`SELECT report_json FROM scans WHERE id = ?`, id).Scan(&gz)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrScanNotFound
	}
	if err != nil {
		return nil, err
	}
	if len(gz) == 0 {
		return nil, ErrScanNotFound
	}
	r, err := gzip.NewReader(bytes.NewReader(gz))
	if err != nil {
		return nil, err
	}
	defer func() { _ = r.Close() }()
	return io.ReadAll(r)
}

func scanScanRow(row rowScanner) (*ScanResult, error) {
	var sc ScanResult
	if err := row.Scan(
		&sc.ID, &sc.Name, &sc.Reference, &sc.Digest, &sc.Status, &sc.RequestedBy, &sc.TrivyVersion,
		&sc.CriticalCount, &sc.HighCount, &sc.MediumCount, &sc.LowCount, &sc.UnknownCount,
		&sc.Error, &sc.StartedAt, &sc.FinishedAt, &sc.CreatedAt,
	); err != nil {
		return nil, err
	}
	return &sc, nil
}
