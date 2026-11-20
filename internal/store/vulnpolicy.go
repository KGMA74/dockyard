package store

import (
	"errors"
	"strings"
	"time"
)

var ErrAllowedCVENotFound = errors.New("CVE is not on the allowlist")

// Severities from most to least severe. A policy's BlockSeverity blocks that
// level and everything above it.
var Severities = []string{"CRITICAL", "HIGH", "MEDIUM", "LOW"}

// SeveritiesAtOrAbove returns the severities a threshold blocks ("HIGH" →
// CRITICAL, HIGH). An empty or unknown threshold blocks nothing.
func SeveritiesAtOrAbove(threshold string) []string {
	for i, s := range Severities {
		if s == threshold {
			return Severities[:i+1]
		}
	}
	return nil
}

func ValidBlockSeverity(s string) bool {
	return s == "" || SeveritiesAtOrAbove(s) != nil
}

type VulnerabilityPolicy struct {
	ScanOnPush    bool   `json:"scan_on_push"`
	BlockSeverity string `json:"block_severity"`
	UpdatedBy     string `json:"updated_by"`
	UpdatedAt     string `json:"updated_at"`
}

func (s *Store) GetVulnerabilityPolicy() (*VulnerabilityPolicy, error) {
	var p VulnerabilityPolicy
	err := s.db.QueryRow(
		`SELECT scan_on_push, block_severity, updated_by, updated_at FROM vulnerability_policy WHERE id = 1`,
	).Scan(&p.ScanOnPush, &p.BlockSeverity, &p.UpdatedBy, &p.UpdatedAt)
	return &p, err
}

func (s *Store) UpdateVulnerabilityPolicy(scanOnPush bool, blockSeverity, updatedBy string) (*VulnerabilityPolicy, error) {
	if !ValidBlockSeverity(blockSeverity) {
		return nil, errors.New("block_severity must be one of CRITICAL, HIGH, MEDIUM, LOW or empty")
	}
	if _, err := s.db.Exec(
		`UPDATE vulnerability_policy SET scan_on_push = ?, block_severity = ?, updated_by = ?, updated_at = ? WHERE id = 1`,
		scanOnPush, blockSeverity, updatedBy, nowRFC3339(),
	); err != nil {
		return nil, err
	}
	return s.GetVulnerabilityPolicy()
}

// AllowedCVE is a vulnerability accepted by the operator: it never blocks a
// pull, until it expires.
type AllowedCVE struct {
	CVEID     string  `json:"cve_id"`
	Reason    string  `json:"reason"`
	ExpiresAt *string `json:"expires_at"`
	CreatedBy string  `json:"created_by"`
	CreatedAt string  `json:"created_at"`
}

func (s *Store) ListAllowedCVEs() ([]*AllowedCVE, error) {
	rows, err := s.db.Query(`SELECT cve_id, reason, expires_at, created_by, created_at FROM cve_allowlist ORDER BY cve_id`)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	out := []*AllowedCVE{}
	for rows.Next() {
		var a AllowedCVE
		if err := rows.Scan(&a.CVEID, &a.Reason, &a.ExpiresAt, &a.CreatedBy, &a.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, &a)
	}
	return out, rows.Err()
}

// AllowCVE adds a CVE to the allowlist, or updates its reason and expiry.
func (s *Store) AllowCVE(cveID, reason, createdBy string, expiresAt *time.Time) error {
	_, err := s.db.Exec(
		`INSERT INTO cve_allowlist (cve_id, reason, expires_at, created_by) VALUES (?, ?, ?, ?)
		 ON CONFLICT (cve_id) DO UPDATE SET reason = excluded.reason, expires_at = excluded.expires_at,
		   created_by = excluded.created_by, created_at = strftime('%Y-%m-%dT%H:%M:%SZ', 'now')`,
		strings.ToUpper(cveID), reason, formatExpiry(expiresAt), createdBy,
	)
	return err
}

func (s *Store) DisallowCVE(cveID string) error {
	res, err := s.db.Exec(`DELETE FROM cve_allowlist WHERE cve_id = ?`, strings.ToUpper(cveID))
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrAllowedCVENotFound
	}
	return nil
}

// ScanVulnerability is one distinct vulnerability found by a scan.
type ScanVulnerability struct {
	ID       string `json:"id"`
	Severity string `json:"severity"`
}

// RecordScanVulnerabilities stores the distinct vulnerabilities of a scan.
// Called before the scan is marked succeeded, so a succeeded scan always has
// them (scans from before this table existed are backfilled on demand).
func (s *Store) RecordScanVulnerabilities(scanID int64, vulns []ScanVulnerability) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	if _, err := tx.Exec(`DELETE FROM scan_vulnerabilities WHERE scan_id = ?`, scanID); err != nil {
		return err
	}
	stmt, err := tx.Prepare(`INSERT OR IGNORE INTO scan_vulnerabilities (scan_id, vuln_id, severity) VALUES (?, ?, ?)`)
	if err != nil {
		return err
	}
	defer func() { _ = stmt.Close() }()
	for _, v := range vulns {
		if _, err := stmt.Exec(scanID, v.ID, v.Severity); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// ScansMissingVulnerabilities lists succeeded scans that found something but
// have no vulnerability rows — scans finished before those rows existed.
func (s *Store) ScansMissingVulnerabilities(limit int) ([]int64, error) {
	rows, err := s.db.Query(
		`SELECT id FROM scans s
		 WHERE status = 'succeeded'
		   AND critical_count + high_count + medium_count + low_count + unknown_count > 0
		   AND NOT EXISTS (SELECT 1 FROM scan_vulnerabilities v WHERE v.scan_id = s.id)
		 ORDER BY id DESC LIMIT ?`, limit,
	)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	var ids []int64
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}

// HasScanVulnerabilities reports whether rows were recorded for a scan.
func (s *Store) HasScanVulnerabilities(scanID int64) (bool, error) {
	var n int
	err := s.db.QueryRow(`SELECT COUNT(*) FROM scan_vulnerabilities WHERE scan_id = ?`, scanID).Scan(&n)
	return n > 0, err
}

// BlockingVulnerabilities returns the scan's vulnerabilities at one of the
// given severities that no unexpired allowlist entry covers, most severe first.
func (s *Store) BlockingVulnerabilities(scanID int64, severities []string, now time.Time) ([]ScanVulnerability, error) {
	if len(severities) == 0 {
		return nil, nil
	}
	args := []any{scanID}
	marks := make([]string, len(severities))
	for i, sev := range severities {
		marks[i] = "?"
		args = append(args, sev)
	}
	args = append(args, now.UTC().Format(time.RFC3339))
	rows, err := s.db.Query(
		`SELECT v.vuln_id, v.severity FROM scan_vulnerabilities v
		 WHERE v.scan_id = ? AND v.severity IN (`+strings.Join(marks, ",")+`)
		   AND NOT EXISTS (SELECT 1 FROM cve_allowlist a
		                   WHERE a.cve_id = UPPER(v.vuln_id) AND (a.expires_at IS NULL OR a.expires_at > ?))
		 ORDER BY CASE v.severity WHEN 'CRITICAL' THEN 0 WHEN 'HIGH' THEN 1 WHEN 'MEDIUM' THEN 2 ELSE 3 END, v.vuln_id`,
		args...,
	)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	var out []ScanVulnerability
	for rows.Next() {
		var v ScanVulnerability
		if err := rows.Scan(&v.ID, &v.Severity); err != nil {
			return nil, err
		}
		out = append(out, v)
	}
	return out, rows.Err()
}
