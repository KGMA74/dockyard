package v2

import (
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"dockyard/internal/events"
	"dockyard/internal/storage"
	"dockyard/internal/storage/storagetest"
	"dockyard/internal/store"
)

func newTestServerWithDB(t *testing.T) (*httptest.Server, *events.Hub, *store.Store) {
	t.Helper()
	backend, err := storage.NewLocal(t.TempDir())
	if err != nil {
		t.Fatalf("NewLocal: %v", err)
	}
	db, err := store.Open(filepath.Join(t.TempDir(), "dockyard.db"))
	if err != nil {
		t.Fatalf("store.Open: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	hub := events.NewHub()
	srv := httptest.NewServer(New(backend, hub, nil, db))
	t.Cleanup(srv.Close)
	return srv, hub, db
}

func TestMonolithicPushBlockedOverRepoQuota(t *testing.T) {
	srv, _, db := newTestServerWithDB(t)
	const name = "team/api"

	if _, err := db.SetQuota("repo", name, 10, 90); err != nil {
		t.Fatalf("SetQuota: %v", err)
	}

	content := []byte("this content is way more than ten bytes")
	dgst := storagetest.Digest(content)
	resp := do(t, http.MethodPost, srv.URL+"/v2/"+name+"/blobs/uploads/?digest="+dgst, content)
	if resp.StatusCode != http.StatusInsufficientStorage {
		t.Fatalf("push over quota: status = %d, want %d", resp.StatusCode, http.StatusInsufficientStorage)
	}

	usage, err := db.ListQuotaUsage()
	if err != nil {
		t.Fatalf("ListQuotaUsage: %v", err)
	}
	if len(usage) != 0 {
		t.Fatalf("a blocked push must not record usage, got %+v", usage)
	}
}

func TestMonolithicPushAllowedWithinQuotaAndTracksUsage(t *testing.T) {
	srv, _, db := newTestServerWithDB(t)
	const name = "team/api"

	if _, err := db.SetQuota("repo", name, 1_000_000, 90); err != nil {
		t.Fatalf("SetQuota: %v", err)
	}

	content := []byte("small blob")
	pushBlobMonolithic(t, srv.URL, name, content)

	usage, err := db.ListQuotaUsage()
	if err != nil {
		t.Fatalf("ListQuotaUsage: %v", err)
	}
	if len(usage) != 1 || usage[0].BytesUsed != int64(len(content)) {
		t.Fatalf("expected usage to track the pushed blob size, got %+v", usage)
	}
}

func TestMonolithicPushPublishesQuotaWarning(t *testing.T) {
	srv, hub, db := newTestServerWithDB(t)
	const name = "team/api"
	sub := hub.Subscribe()
	defer hub.Unsubscribe(sub)

	// Warn threshold at 50%; a push that lands at/above it should fire once.
	if _, err := db.SetQuota("repo", name, 10, 50); err != nil {
		t.Fatalf("SetQuota: %v", err)
	}

	content := []byte("123456") // 6/10 bytes = 60%
	pushBlobMonolithic(t, srv.URL, name, content)

	select {
	case ev := <-sub:
		if ev.Type != "quota_warning" {
			t.Fatalf("expected a quota_warning event, got %+v", ev)
		}
	default:
		t.Fatal("expected a quota_warning event to be published")
	}
}

func TestChunkedPushBlockedOverRepoQuota(t *testing.T) {
	srv, _, db := newTestServerWithDB(t)
	const name = "team/api"

	if _, err := db.SetQuota("repo", name, 5, 90); err != nil {
		t.Fatalf("SetQuota: %v", err)
	}

	resp := do(t, http.MethodPost, srv.URL+"/v2/"+name+"/blobs/uploads/", nil)
	if resp.StatusCode != http.StatusAccepted {
		t.Fatalf("init upload: status = %d, want 202", resp.StatusCode)
	}
	loc := resp.Header.Get("Location")

	content := []byte("more than five bytes")
	dgst := storagetest.Digest(content)
	putResp := do(t, http.MethodPut, srv.URL+loc+"?digest="+dgst, content)
	if putResp.StatusCode != http.StatusInsufficientStorage {
		t.Fatalf("chunked commit over quota: status = %d, want %d", putResp.StatusCode, http.StatusInsufficientStorage)
	}

	usage, err := db.ListQuotaUsage()
	if err != nil {
		t.Fatalf("ListQuotaUsage: %v", err)
	}
	if len(usage) != 0 {
		t.Fatalf("a blocked chunked commit must not record usage, got %+v", usage)
	}
}
