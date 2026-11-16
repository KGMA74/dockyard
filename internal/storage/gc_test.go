package storage_test

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"dockyard/internal/storage"
	"dockyard/internal/storage/storagetest"
)

func putBlob(t *testing.T, b storage.Backend, content string) string {
	t.Helper()
	dgst := storagetest.Digest([]byte(content))
	if err := b.PutBlob(dgst, bytes.NewReader([]byte(content)), int64(len(content))); err != nil {
		t.Fatalf("PutBlob: %v", err)
	}
	return dgst
}

// A push uploads layers first and the manifest last. A collection running in
// that window must not delete the layers it has not seen a manifest for yet.
func TestCollectGarbageGracePeriodProtectsInFlightPush(t *testing.T) {
	b, err := storage.NewLocal(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	inFlight := putBlob(t, b, "layer of a push whose manifest has not arrived yet")

	res, err := storage.CollectGarbage(b, storage.GCOptions{GracePeriod: time.Hour})
	if err != nil {
		t.Fatalf("CollectGarbage: %v", err)
	}
	if len(res.Removed) != 0 || res.SkippedRecent != 1 {
		t.Fatalf("result = %+v, want nothing removed and 1 skipped as recent", res)
	}
	if ok, _ := b.BlobExists(inFlight); !ok {
		t.Fatal("grace period did not protect a freshly uploaded blob")
	}

	// Without a grace period the same orphan is collected.
	res, err = storage.CollectGarbage(b, storage.GCOptions{})
	if err != nil {
		t.Fatalf("CollectGarbage: %v", err)
	}
	if len(res.Removed) != 1 || res.Removed[0] != inFlight {
		t.Fatalf("result = %+v, want the orphan removed", res)
	}
}

func TestCollectGarbageDryRunRemovesNothing(t *testing.T) {
	b, err := storage.NewLocal(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	orphan := putBlob(t, b, "orphan")
	res, err := storage.CollectGarbage(b, storage.GCOptions{DryRun: true})
	if err != nil {
		t.Fatalf("CollectGarbage: %v", err)
	}
	if len(res.Removed) != 1 || res.FreedBytes != int64(len("orphan")) || res.ScannedBlobs != 1 {
		t.Fatalf("dry run = %+v", res)
	}
	if ok, _ := b.BlobExists(orphan); !ok {
		t.Fatal("dry run removed a blob")
	}
}

// If a manifest cannot be read, its layers are unknown — not unreferenced.
// The collection must abort rather than delete them.
func TestCollectGarbageAbortsWhenAManifestIsUnreadable(t *testing.T) {
	root := t.TempDir()
	b, err := storage.NewLocal(root)
	if err != nil {
		t.Fatal(err)
	}
	cfg := putBlob(t, b, "config")
	layer := putBlob(t, b, "layer")
	manifest := storagetest.ManifestFor(cfg, layer)
	dgst := storagetest.Digest(manifest)
	if err := b.PutManifest("team/app", "v1", dgst, manifest); err != nil {
		t.Fatal(err)
	}

	// Make the manifest unreadable in a way that works on every OS: replace
	// the file with a directory of the same name.
	manifestFile := filepath.Join(root, "repositories", "team", "app", "manifests", strings.TrimPrefix(dgst, "sha256:"))
	if err := os.Remove(manifestFile); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(manifestFile, 0755); err != nil {
		t.Fatal(err)
	}

	if _, err := storage.CollectGarbage(b, storage.GCOptions{}); err == nil {
		t.Fatal("CollectGarbage succeeded although a manifest could not be read")
	}
	for _, d := range []string{cfg, layer} {
		if ok, _ := b.BlobExists(d); !ok {
			t.Fatalf("blob %s was deleted although its manifest was merely unreadable", d)
		}
	}
}

func TestPurgeStaleUploads(t *testing.T) {
	root := t.TempDir()
	b, err := storage.NewLocal(root)
	if err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{"abandoned", "active"} {
		if err := b.InitUpload(id); err != nil {
			t.Fatal(err)
		}
		if err := b.AppendUpload(id, strings.NewReader("partial layer")); err != nil {
			t.Fatal(err)
		}
	}
	old := time.Now().Add(-48 * time.Hour)
	for _, p := range []string{
		filepath.Join(root, "uploads", "abandoned"),
		filepath.Join(root, "uploads", "abandoned", "data"),
	} {
		if err := os.Chtimes(p, old, old); err != nil {
			t.Fatal(err)
		}
	}

	removed, err := b.PurgeStaleUploads(24 * time.Hour)
	if err != nil {
		t.Fatalf("PurgeStaleUploads: %v", err)
	}
	if removed != 1 {
		t.Fatalf("removed = %d, want 1", removed)
	}
	if _, err := b.GetUploadSize("abandoned"); err == nil {
		t.Error("abandoned upload survived the purge")
	}
	if _, err := b.GetUploadSize("active"); err != nil {
		t.Errorf("active upload was purged: %v", err)
	}
}
