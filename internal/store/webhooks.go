package store

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"
)

var ErrWebhookNotFound = errors.New("webhook not found")

type Webhook struct {
	ID        int64    `json:"id"`
	URL       string   `json:"url"`
	Secret    string   `json:"-"`
	Events    []string `json:"events"`
	Format    string   `json:"format"`
	Enabled   bool     `json:"enabled"`
	CreatedAt string   `json:"created_at"`
}

type WebhookDelivery struct {
	ID        int64
	WebhookID int64
	Payload   string
	Attempts  int
}

func (s *Store) CreateWebhook(w Webhook) (*Webhook, error) {
	if w.URL == "" {
		return nil, errors.New("url is required")
	}
	if w.Format == "" {
		w.Format = "generic"
	}
	if len(w.Events) == 0 {
		w.Events = []string{"push"}
	}
	events, err := marshalPatterns(w.Events)
	if err != nil {
		return nil, err
	}
	res, err := s.db.Exec(
		`INSERT INTO webhooks (url, secret, events, format, enabled) VALUES (?, ?, ?, ?, ?)`,
		w.URL, w.Secret, events, w.Format, w.Enabled,
	)
	if err != nil {
		return nil, err
	}
	id, _ := res.LastInsertId()
	return s.webhookByID(id)
}

func (s *Store) ListWebhooks() ([]*Webhook, error) {
	rows, err := s.db.Query(
		`SELECT id, url, secret, events, format, enabled, created_at FROM webhooks ORDER BY id`,
	)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	var hooks []*Webhook
	for rows.Next() {
		h, err := scanWebhook(rows)
		if err != nil {
			return nil, err
		}
		hooks = append(hooks, h)
	}
	return hooks, rows.Err()
}

func (s *Store) DeleteWebhook(id int64) error {
	res, err := s.db.Exec(`DELETE FROM webhooks WHERE id = ?`, id)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrWebhookNotFound
	}
	return nil
}

func (s *Store) webhookByID(id int64) (*Webhook, error) {
	h, err := scanWebhook(s.db.QueryRow(
		`SELECT id, url, secret, events, format, enabled, created_at FROM webhooks WHERE id = ?`, id,
	))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrWebhookNotFound
	}
	return h, err
}

func (s *Store) WebhookByID(id int64) (*Webhook, error) { return s.webhookByID(id) }

func scanWebhook(row rowScanner) (*Webhook, error) {
	var h Webhook
	var events string
	if err := row.Scan(&h.ID, &h.URL, &h.Secret, &events, &h.Format, &h.Enabled, &h.CreatedAt); err != nil {
		return nil, err
	}
	if err := json.Unmarshal([]byte(events), &h.Events); err != nil {
		return nil, fmt.Errorf("webhook %d: bad events: %w", h.ID, err)
	}
	return &h, nil
}

// ── Delivery outbox ──────────────────────────────────────────────────────────

func (s *Store) EnqueueDelivery(webhookID int64, payload string) error {
	_, err := s.db.Exec(
		`INSERT INTO webhook_deliveries (webhook_id, payload) VALUES (?, ?)`,
		webhookID, payload,
	)
	return err
}

// DueDeliveries returns undelivered rows whose retry time has come, oldest
// first, capped to keep a delivery burst bounded.
func (s *Store) DueDeliveries(maxAttempts, limit int) ([]*WebhookDelivery, error) {
	rows, err := s.db.Query(
		`SELECT id, webhook_id, payload, attempts FROM webhook_deliveries
		 WHERE delivered_at IS NULL AND attempts < ? AND next_attempt_at <= ?
		 ORDER BY id LIMIT ?`,
		maxAttempts, nowRFC3339(), limit,
	)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	var due []*WebhookDelivery
	for rows.Next() {
		var d WebhookDelivery
		if err := rows.Scan(&d.ID, &d.WebhookID, &d.Payload, &d.Attempts); err != nil {
			return nil, err
		}
		due = append(due, &d)
	}
	return due, rows.Err()
}

func (s *Store) MarkDelivered(deliveryID int64) error {
	_, err := s.db.Exec(
		`UPDATE webhook_deliveries SET delivered_at = ?, attempts = attempts + 1 WHERE id = ?`,
		nowRFC3339(), deliveryID,
	)
	return err
}

func (s *Store) MarkFailed(deliveryID int64, retryAt time.Time, lastError string) error {
	_, err := s.db.Exec(
		`UPDATE webhook_deliveries SET attempts = attempts + 1, next_attempt_at = ?, last_error = ? WHERE id = ?`,
		retryAt.UTC().Format(time.RFC3339), lastError, deliveryID,
	)
	return err
}
