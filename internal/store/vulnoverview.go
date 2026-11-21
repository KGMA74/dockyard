package store

// latestSuccessfulScans selects, for every scanned digest, its most recent
// successful scan — what the pull gate and the tag list consider current.
const latestSuccessfulScans = `SELECT MAX(id) FROM scans WHERE status = 'succeeded' GROUP BY digest`

// LatestSuccessfulScans returns the current scan of every scanned image (one
// per digest), most recent first.
func (s *Store) LatestSuccessfulScans() ([]*ScanResult, error) {
	rows, err := s.db.Query(`SELECT ` + scanColumns + ` FROM scans WHERE id IN (` + latestSuccessfulScans + `) ORDER BY id DESC`)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	var out []*ScanResult
	for rows.Next() {
		sc, err := scanScanRow(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, sc)
	}
	return out, rows.Err()
}

// ScanFinding ties one distinct vulnerability to the scan that found it.
type ScanFinding struct {
	ScanID   int64
	VulnID   string
	Severity string
}

// FindingsOfLatestScans lists the distinct vulnerabilities of every current
// scan (see LatestSuccessfulScans), for registry-wide views.
func (s *Store) FindingsOfLatestScans() ([]ScanFinding, error) {
	rows, err := s.db.Query(`SELECT scan_id, vuln_id, severity FROM scan_vulnerabilities WHERE scan_id IN (` + latestSuccessfulScans + `)`)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	var out []ScanFinding
	for rows.Next() {
		var f ScanFinding
		if err := rows.Scan(&f.ScanID, &f.VulnID, &f.Severity); err != nil {
			return nil, err
		}
		out = append(out, f)
	}
	return out, rows.Err()
}
