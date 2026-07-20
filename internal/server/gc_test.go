package server

import (
	"bytes"
	"testing"
	"time"

	"dockyard/internal/events"
	"dockyard/internal/storage"
	"dockyard/internal/storage/storagetest"
)

func TestRunGCRemovesOnlyOrphanBlobs(t *testing.T) {
	backend, err := storage.NewLocal(t.TempDir())
	if err != nil {
		t.Fatalf("NewLocal: %v", err)
	}

	putBlob := func(content []byte) string {
		t.Helper()
		dgst := storagetest.Digest(content)
		if err := backend.PutBlob(dgst, bytes.NewReader(content), int64(len(content))); err != nil {
			t.Fatalf("PutBlob: %v", err)
		}
		return dgst
	}
	configDgst := putBlob([]byte("config-blob"))
	layerDgst := putBlob([]byte("layer-blob"))
	orphanDgst := putBlob([]byte("orphan-blob"))

	manifest := storagetest.ManifestFor(configDgst, layerDgst)
	if err := backend.PutManifest("gc/app", "v1", storagetest.Digest(manifest), manifest); err != nil {
		t.Fatalf("PutManifest: %v", err)
	}

	runGC(backend, nil)

	for _, keep := range []string{configDgst, layerDgst} {
		if ok, _ := backend.BlobExists(keep); !ok {
			t.Errorf("referenced blob %s was removed by GC", keep)
		}
	}
	if ok, _ := backend.BlobExists(orphanDgst); ok {
		t.Errorf("orphan blob %s survived GC", orphanDgst)
	}
}

func TestRunGCIsIdempotentOnCleanStore(t *testing.T) {
	backend, err := storage.NewLocal(t.TempDir())
	if err != nil {
		t.Fatalf("NewLocal: %v", err)
	}
	runGC(backend, nil) // must not panic or error on an empty store
	runGC(backend, nil)
}

func TestRunGCPublishesEventOnlyWhenSomethingWasRemoved(t *testing.T) {
	backend, err := storage.NewLocal(t.TempDir())
	if err != nil {
		t.Fatalf("NewLocal: %v", err)
	}
	hub := events.NewHub()
	ch := hub.Subscribe()
	defer hub.Unsubscribe(ch)

	// Nothing to collect — must not publish.
	runGC(backend, hub)
	select {
	case ev := <-ch:
		t.Fatalf("unexpected event on a no-op GC: %+v", ev)
	case <-time.After(100 * time.Millisecond):
	}

	// An orphan blob exists — must publish "gc".
	orphan := []byte("orphan-blob")
	dgst := storagetest.Digest(orphan)
	if err := backend.PutBlob(dgst, bytes.NewReader(orphan), int64(len(orphan))); err != nil {
		t.Fatalf("PutBlob: %v", err)
	}
	runGC(backend, hub)
	select {
	case ev := <-ch:
		if ev.Type != "gc" || ev.Actor != "scheduler" {
			t.Fatalf("unexpected event: %+v", ev)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("expected a gc event after removing an orphan blob")
	}
}
