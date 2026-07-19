package webhooks

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"dockyard/internal/events"
	"dockyard/internal/store"
)

func newDispatcherFixture(t *testing.T) (*Dispatcher, *store.Store) {
	t.Helper()
	st, err := store.Open(filepath.Join(t.TempDir(), "dockyard.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	// Build without the background loop — tests drive drain() directly.
	d := &Dispatcher{
		store:  st,
		client: &http.Client{Timeout: 5 * time.Second},
		wake:   make(chan struct{}, 1),
	}
	return d, st
}

func TestDeliveryWithSignature(t *testing.T) {
	d, st := newDispatcherFixture(t)

	var gotBody []byte
	var gotSig string
	sink := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotBody, _ = io.ReadAll(r.Body)
		gotSig = r.Header.Get("X-Dockyard-Signature")
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(sink.Close)

	if _, err := st.CreateWebhook(store.Webhook{
		URL: sink.URL, Secret: "hunter2", Events: []string{"push"}, Enabled: true,
	}); err != nil {
		t.Fatal(err)
	}

	d.Enqueue(events.Event{Type: "push", Name: "team/app", Tag: "v1", Actor: "alice"})
	d.drain()

	if len(gotBody) == 0 {
		t.Fatal("nothing delivered")
	}
	var payload struct {
		Type, Name, Tag, Actor string
	}
	if err := json.Unmarshal(gotBody, &payload); err != nil {
		t.Fatal(err)
	}
	if payload.Type != "push" || payload.Name != "team/app" || payload.Actor != "alice" {
		t.Errorf("payload = %+v", payload)
	}
	mac := hmac.New(sha256.New, []byte("hunter2"))
	mac.Write(gotBody)
	if want := "sha256=" + hex.EncodeToString(mac.Sum(nil)); gotSig != want {
		t.Errorf("signature = %q, want %q", gotSig, want)
	}

	// Delivered → outbox empty.
	if due, _ := st.DueDeliveries(maxAttempts, 10); len(due) != 0 {
		t.Errorf("outbox not drained: %d rows", len(due))
	}
}

func TestEventTypeFiltering(t *testing.T) {
	d, st := newDispatcherFixture(t)
	var hits atomic.Int64
	sink := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
	}))
	t.Cleanup(sink.Close)

	if _, err := st.CreateWebhook(store.Webhook{URL: sink.URL, Events: []string{"delete"}, Enabled: true}); err != nil {
		t.Fatal(err)
	}
	d.Enqueue(events.Event{Type: "push", Name: "x"})
	d.drain()
	if hits.Load() != 0 {
		t.Error("push delivered to a delete-only hook")
	}
	d.Enqueue(events.Event{Type: "delete", Name: "x"})
	d.drain()
	if hits.Load() != 1 {
		t.Errorf("delete deliveries = %d, want 1", hits.Load())
	}
}

func TestRetryWithBackoff(t *testing.T) {
	d, st := newDispatcherFixture(t)
	var calls atomic.Int64
	failing := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		w.WriteHeader(http.StatusInternalServerError)
	}))
	t.Cleanup(failing.Close)

	if _, err := st.CreateWebhook(store.Webhook{URL: failing.URL, Events: []string{"push"}, Enabled: true}); err != nil {
		t.Fatal(err)
	}
	d.Enqueue(events.Event{Type: "push", Name: "x"})

	d.drain()
	if calls.Load() != 1 {
		t.Fatalf("first attempt calls = %d", calls.Load())
	}
	// Backoff: not due yet → drain does nothing.
	d.drain()
	if calls.Load() != 1 {
		t.Errorf("retried before backoff elapsed: %d calls", calls.Load())
	}
	// Force the retry time and verify attempts accumulate toward the cap.
	if err := st.MarkFailed(1, time.Now().Add(-time.Second), "forced"); err != nil {
		t.Fatal(err)
	}
	d.drain()
	if calls.Load() != 2 {
		t.Errorf("after forcing due time: %d calls, want 2", calls.Load())
	}
}

func TestChatFormats(t *testing.T) {
	d, st := newDispatcherFixture(t)
	var gotBody []byte
	sink := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotBody, _ = io.ReadAll(r.Body)
	}))
	t.Cleanup(sink.Close)

	hook, err := st.CreateWebhook(store.Webhook{URL: sink.URL, Events: []string{"push"}, Format: "slack", Enabled: true})
	if err != nil {
		t.Fatal(err)
	}
	if err := d.DeliverNow(hook); err != nil {
		t.Fatal(err)
	}
	var slack struct {
		Text string `json:"text"`
	}
	if err := json.Unmarshal(gotBody, &slack); err != nil || slack.Text == "" {
		t.Errorf("slack payload = %s", gotBody)
	}
}
