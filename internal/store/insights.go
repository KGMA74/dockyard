package store

import "time"

type StatsSample struct {
	At        string `json:"at"`
	TotalSize int64  `json:"total_size"`
	BlobCount int    `json:"blob_count"`
	RepoCount int    `json:"repo_count"`
}

// AddStatsSample records one storage snapshot and prunes entries older than
// the retention window.
func (s *Store) AddStatsSample(totalSize int64, blobCount, repoCount int) error {
	if _, err := s.db.Exec(
		`INSERT OR REPLACE INTO stats_history (at, total_size, blob_count, repo_count) VALUES (?, ?, ?, ?)`,
		nowRFC3339(), totalSize, blobCount, repoCount,
	); err != nil {
		return err
	}
	cutoff := time.Now().AddDate(0, 0, -90).UTC().Format(time.RFC3339)
	_, err := s.db.Exec(`DELETE FROM stats_history WHERE at < ?`, cutoff)
	return err
}

// ListStatsSamples returns snapshots oldest-first, capped to limit.
func (s *Store) ListStatsSamples(limit int) ([]*StatsSample, error) {
	if limit <= 0 || limit > 1000 {
		limit = 360
	}
	rows, err := s.db.Query(
		`SELECT at, total_size, blob_count, repo_count FROM
		   (SELECT * FROM stats_history ORDER BY at DESC LIMIT ?)
		 ORDER BY at ASC`, limit,
	)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	var samples []*StatsSample
	for rows.Next() {
		var sm StatsSample
		if err := rows.Scan(&sm.At, &sm.TotalSize, &sm.BlobCount, &sm.RepoCount); err != nil {
			return nil, err
		}
		samples = append(samples, &sm)
	}
	return samples, rows.Err()
}
