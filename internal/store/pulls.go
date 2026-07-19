package store

import (
	"time"
)

// RecordPulls upserts a batch of pull observations in one transaction.
func (s *Store) RecordPulls(counts map[[2]string]int) error {
	if len(counts) == 0 {
		return nil
	}
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	now := nowRFC3339()
	for key, n := range counts {
		if _, err := tx.Exec(
			`INSERT INTO last_pulls (repo, reference, last_pulled_at, pull_count) VALUES (?, ?, ?, ?)
			 ON CONFLICT(repo, reference) DO UPDATE SET
			   last_pulled_at = excluded.last_pulled_at,
			   pull_count = pull_count + excluded.pull_count`,
			key[0], key[1], now, n,
		); err != nil {
			_ = tx.Rollback()
			return err
		}
	}
	return tx.Commit()
}

// LastPull returns the last pull time and count for repo/reference; ok=false
// when it was never pulled.
func (s *Store) LastPull(repo, reference string) (at time.Time, count int, ok bool) {
	var raw string
	err := s.db.QueryRow(
		`SELECT last_pulled_at, pull_count FROM last_pulls WHERE repo = ? AND reference = ?`,
		repo, reference,
	).Scan(&raw, &count)
	if err != nil {
		return time.Time{}, 0, false
	}
	at, err = time.Parse(time.RFC3339, raw)
	return at, count, err == nil
}

// ── Async pull tracker ───────────────────────────────────────────────────────

// PullTracker batches pull observations off the request hot path. Record never
// blocks: under overload observations are dropped rather than slowing pulls.
type PullTracker struct {
	store *Store
	ch    chan [2]string
	done  chan struct{}
}

func NewPullTracker(s *Store) *PullTracker {
	t := &PullTracker{
		store: s,
		ch:    make(chan [2]string, 1024),
		done:  make(chan struct{}),
	}
	go t.loop()
	return t
}

// Record notes one pull of repo/reference. Safe from any goroutine, never blocks.
func (t *PullTracker) Record(repo, reference string) {
	select {
	case t.ch <- [2]string{repo, reference}:
	default: // full buffer — drop rather than stall the pull
	}
}

// Close flushes pending observations and stops the loop.
func (t *PullTracker) Close() {
	close(t.ch)
	<-t.done
}

func (t *PullTracker) loop() {
	defer close(t.done)
	ticker := time.NewTicker(3 * time.Second)
	defer ticker.Stop()
	pending := make(map[[2]string]int)

	flush := func() {
		if len(pending) > 0 {
			_ = t.store.RecordPulls(pending)
			pending = make(map[[2]string]int)
		}
	}
	for {
		select {
		case key, open := <-t.ch:
			if !open {
				flush()
				return
			}
			pending[key]++
			if len(pending) >= 256 {
				flush()
			}
		case <-ticker.C:
			flush()
		}
	}
}
