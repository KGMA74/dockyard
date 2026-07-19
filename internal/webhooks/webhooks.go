// Package webhooks delivers registry events (push, delete, retention, gc,
// scan) to
// HTTP endpoints with at-least-once semantics: events are enqueued into a
// SQLite outbox and a dispatcher retries with exponential backoff. Payloads
// are signed with HMAC-SHA256 when the hook has a secret.
package webhooks

import (
	"bytes"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"slices"
	"time"

	"dockyard/internal/events"
	"dockyard/internal/store"
)

const (
	maxAttempts = 8
	baseBackoff = 30 * time.Second
)

type Dispatcher struct {
	store    *store.Store
	client   *http.Client
	wake     chan struct{}
	interval time.Duration // poll fallback, also lets backoff retries fire
}

func NewDispatcher(st *store.Store) *Dispatcher {
	d := &Dispatcher{
		store:    st,
		client:   &http.Client{Timeout: 10 * time.Second},
		wake:     make(chan struct{}, 1),
		interval: 15 * time.Second,
	}
	go d.loop()
	return d
}

// Subscribe wires the dispatcher to the in-process event hub.
func (d *Dispatcher) Subscribe(hub *events.Hub) {
	ch := hub.Subscribe()
	go func() {
		for ev := range ch {
			d.Enqueue(ev)
		}
	}()
}

// Enqueue fans an event out to every enabled webhook subscribed to its type.
func (d *Dispatcher) Enqueue(ev events.Event) {
	hooks, err := d.store.ListWebhooks()
	if err != nil {
		slog.Error("webhooks: list failed", "err", err)
		return
	}
	payload, err := json.Marshal(map[string]any{
		"type":  ev.Type,
		"name":  ev.Name,
		"tag":   ev.Tag,
		"actor": ev.Actor,
		"at":    time.Now().UTC().Format(time.RFC3339),
	})
	if err != nil {
		return
	}
	queued := false
	for _, h := range hooks {
		if !h.Enabled || !slices.Contains(h.Events, ev.Type) {
			continue
		}
		if err := d.store.EnqueueDelivery(h.ID, string(payload)); err == nil {
			queued = true
		}
	}
	if queued {
		select {
		case d.wake <- struct{}{}:
		default:
		}
	}
}

// DeliverNow sends a synchronous test event to one webhook, bypassing the
// outbox — used by the admin "test" button.
func (d *Dispatcher) DeliverNow(hook *store.Webhook) error {
	payload, _ := json.Marshal(map[string]any{
		"type": "test",
		"name": "dockyard/test",
		"tag":  "hello",
		"at":   time.Now().UTC().Format(time.RFC3339),
	})
	return d.send(hook, string(payload))
}

func (d *Dispatcher) loop() {
	ticker := time.NewTicker(d.interval)
	defer ticker.Stop()
	for {
		select {
		case <-d.wake:
		case <-ticker.C:
		}
		d.drain()
	}
}

func (d *Dispatcher) drain() {
	due, err := d.store.DueDeliveries(maxAttempts, 64)
	if err != nil {
		slog.Error("webhooks: outbox query failed", "err", err)
		return
	}
	for _, delivery := range due {
		hook, err := d.store.WebhookByID(delivery.WebhookID)
		if err != nil || !hook.Enabled {
			// Hook gone or disabled — retire the delivery.
			_ = d.store.MarkDelivered(delivery.ID)
			continue
		}
		if err := d.send(hook, delivery.Payload); err != nil {
			backoff := baseBackoff * (1 << min(delivery.Attempts, 6)) // 30s → 32m cap
			_ = d.store.MarkFailed(delivery.ID, time.Now().Add(backoff), err.Error())
			slog.Warn("webhooks: delivery failed", "url", hook.URL, "attempt", delivery.Attempts+1, "err", err)
			continue
		}
		_ = d.store.MarkDelivered(delivery.ID)
	}
}

// send posts the payload in the hook's format, signing generic payloads.
func (d *Dispatcher) send(hook *store.Webhook, payload string) error {
	body := payload
	switch hook.Format {
	case "slack":
		body = wrapText(payload, "text")
	case "discord":
		body = wrapText(payload, "content")
	}

	req, err := http.NewRequest(http.MethodPost, hook.URL, bytes.NewReader([]byte(body)))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	if hook.Secret != "" {
		mac := hmac.New(sha256.New, []byte(hook.Secret))
		mac.Write([]byte(body))
		req.Header.Set("X-Dockyard-Signature", "sha256="+hex.EncodeToString(mac.Sum(nil)))
	}

	resp, err := d.client.Do(req)
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode >= 300 {
		return fmt.Errorf("endpoint answered %d", resp.StatusCode)
	}
	return nil
}

// wrapText turns the raw event JSON into a human message for chat services.
func wrapText(payload, key string) string {
	var ev struct {
		Type  string `json:"type"`
		Name  string `json:"name"`
		Tag   string `json:"tag"`
		Actor string `json:"actor"`
	}
	_ = json.Unmarshal([]byte(payload), &ev)
	ref := ev.Name
	if ev.Tag != "" {
		ref += ":" + ev.Tag
	}
	msg := fmt.Sprintf("Dockyard: %s %s", ev.Type, ref)
	if ev.Actor != "" {
		msg += " by " + ev.Actor
	}
	out, _ := json.Marshal(map[string]string{key: msg})
	return string(out)
}
