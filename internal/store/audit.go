package store

type AuditEntry struct {
	ID       int64  `json:"id"`
	At       string `json:"at"`
	Actor    string `json:"actor"`
	Action   string `json:"action"`
	Repo     string `json:"repo,omitempty"`
	Tag      string `json:"tag,omitempty"`
	SourceIP string `json:"source_ip,omitempty"`
	Result   string `json:"result"`
	Details  string `json:"details,omitempty"`
}

func (s *Store) AddAudit(e AuditEntry) error {
	_, err := s.db.Exec(
		`INSERT INTO audit_log (actor, action, repo, tag, source_ip, result, details)
		 VALUES (?, ?, ?, ?, ?, ?, ?)`,
		e.Actor, e.Action, e.Repo, e.Tag, e.SourceIP, e.Result, e.Details,
	)
	return err
}

// ListAudit returns entries newest-first, optionally filtered by repo and/or
// actor, plus the total count matching the filter.
func (s *Store) ListAudit(repo, actor string, limit, offset int) ([]*AuditEntry, int, error) {
	if limit <= 0 || limit > 500 {
		limit = 50
	}
	where := " WHERE 1=1"
	args := []any{}
	if repo != "" {
		where += " AND repo = ?"
		args = append(args, repo)
	}
	if actor != "" {
		where += " AND actor = ?"
		args = append(args, actor)
	}

	var total int
	if err := s.db.QueryRow(`SELECT COUNT(*) FROM audit_log`+where, args...).Scan(&total); err != nil {
		return nil, 0, err
	}

	rows, err := s.db.Query(
		`SELECT id, at, actor, action, repo, tag, source_ip, result, details
		 FROM audit_log`+where+` ORDER BY id DESC LIMIT ? OFFSET ?`,
		append(args, limit, offset)...,
	)
	if err != nil {
		return nil, 0, err
	}
	defer func() { _ = rows.Close() }()
	var entries []*AuditEntry
	for rows.Next() {
		var e AuditEntry
		if err := rows.Scan(&e.ID, &e.At, &e.Actor, &e.Action, &e.Repo, &e.Tag,
			&e.SourceIP, &e.Result, &e.Details); err != nil {
			return nil, 0, err
		}
		entries = append(entries, &e)
	}
	return entries, total, rows.Err()
}
