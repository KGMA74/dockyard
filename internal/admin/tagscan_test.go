package admin

import (
	"path/filepath"
	"testing"

	"dockyard/internal/store"
)

// The tag list carries each tag's latest scan with its ID, so the console
// can open that scan's report straight from the list.
func TestTagScanInfoCarriesTheScanToOpen(t *testing.T) {
	db, err := store.Open(filepath.Join(t.TempDir(), "dockyard.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	const digest = "sha256:1111111111111111111111111111111111111111111111111111111111111111"

	if got := tagScanInfo(db, digest); got != nil {
		t.Fatalf("never scanned: %+v, want nil", got)
	}

	sc, err := db.EnqueueScan("team/app", "1.0", digest, "admin")
	if err != nil {
		t.Fatal(err)
	}
	if err := db.MarkScanSucceeded(sc.ID, 1, 2, 3, 4, 0, nil, "0.56.2"); err != nil {
		t.Fatal(err)
	}
	got := tagScanInfo(db, digest)
	if got == nil || got.ID != sc.ID || got.TrivyVersion != "0.56.2" || got.Status != "succeeded" || got.HighCount != 2 {
		t.Fatalf("tagScanInfo = %+v, want scan %d, succeeded, Trivy 0.56.2", got, sc.ID)
	}
}
