package store

import (
	"bytes"
	"compress/gzip"
	"errors"
	"testing"
)

func gzipBytes(t *testing.T, raw string) []byte {
	t.Helper()
	var buf bytes.Buffer
	w := gzip.NewWriter(&buf)
	if _, err := w.Write([]byte(raw)); err != nil {
		t.Fatal(err)
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func TestScanRoundTrip(t *testing.T) {
	s, _ := openTemp(t)

	sc, err := s.EnqueueScan("library/nginx", "latest", "sha256:abc", "admin")
	if err != nil {
		t.Fatalf("EnqueueScan: %v", err)
	}
	if sc.Status != "queued" {
		t.Fatalf("status = %q, want queued", sc.Status)
	}

	next, err := s.NextQueuedScan()
	if err != nil {
		t.Fatalf("NextQueuedScan: %v", err)
	}
	if next == nil || next.ID != sc.ID {
		t.Fatalf("NextQueuedScan = %+v, want id %d", next, sc.ID)
	}

	if err := s.MarkScanRunning(sc.ID); err != nil {
		t.Fatalf("MarkScanRunning: %v", err)
	}

	report := gzipBytes(t, `{"Results":[]}`)
	if err := s.MarkScanSucceeded(sc.ID, 1, 2, 3, 4, 0, report, "0.56.2"); err != nil {
		t.Fatalf("MarkScanSucceeded: %v", err)
	}

	got, err := s.ScanByID(sc.ID)
	if err != nil {
		t.Fatalf("ScanByID: %v", err)
	}
	if got.Status != "succeeded" || got.CriticalCount != 1 || got.HighCount != 2 || got.TrivyVersion != "0.56.2" {
		t.Fatalf("unexpected scan after success: %+v", got)
	}
	if got.StartedAt == nil || got.FinishedAt == nil {
		t.Fatalf("expected started_at/finished_at to be set: %+v", got)
	}

	rawReport, err := s.ScanReport(sc.ID)
	if err != nil {
		t.Fatalf("ScanReport: %v", err)
	}
	if string(rawReport) != `{"Results":[]}` {
		t.Fatalf("ScanReport = %q", rawReport)
	}

	// Queue is now empty.
	next, err = s.NextQueuedScan()
	if err != nil {
		t.Fatalf("NextQueuedScan (empty): %v", err)
	}
	if next != nil {
		t.Fatalf("NextQueuedScan (empty) = %+v, want nil", next)
	}
}

func TestScanMarkFailed(t *testing.T) {
	s, _ := openTemp(t)

	sc, err := s.EnqueueScan("library/nginx", "latest", "sha256:def", "admin")
	if err != nil {
		t.Fatalf("EnqueueScan: %v", err)
	}
	if err := s.MarkScanFailed(sc.ID, "trivy: timeout"); err != nil {
		t.Fatalf("MarkScanFailed: %v", err)
	}
	got, err := s.ScanByID(sc.ID)
	if err != nil {
		t.Fatalf("ScanByID: %v", err)
	}
	if got.Status != "failed" || got.Error != "trivy: timeout" {
		t.Fatalf("unexpected scan after failure: %+v", got)
	}
}

func TestScanByIDNotFound(t *testing.T) {
	s, _ := openTemp(t)
	if _, err := s.ScanByID(999); !errors.Is(err, ErrScanNotFound) {
		t.Fatalf("ScanByID(999) err = %v, want ErrScanNotFound", err)
	}
	if _, err := s.ScanReport(999); !errors.Is(err, ErrScanNotFound) {
		t.Fatalf("ScanReport(999) err = %v, want ErrScanNotFound", err)
	}
}

func TestListScansFilterAndPagination(t *testing.T) {
	s, _ := openTemp(t)

	for range 3 {
		if _, err := s.EnqueueScan("library/nginx", "latest", "sha256:aaa", "admin"); err != nil {
			t.Fatalf("EnqueueScan: %v", err)
		}
	}
	if _, err := s.EnqueueScan("library/redis", "latest", "sha256:bbb", "admin"); err != nil {
		t.Fatalf("EnqueueScan: %v", err)
	}

	all, total, err := s.ListScans("", "", 0, 0)
	if err != nil {
		t.Fatalf("ListScans: %v", err)
	}
	if total != 4 || len(all) != 4 {
		t.Fatalf("ListScans() total=%d len=%d, want 4/4", total, len(all))
	}
	// Newest first.
	if all[0].Name != "library/redis" {
		t.Fatalf("ListScans()[0].Name = %q, want library/redis", all[0].Name)
	}

	byName, total, err := s.ListScans("library/nginx", "", 0, 0)
	if err != nil {
		t.Fatalf("ListScans(name): %v", err)
	}
	if total != 3 || len(byName) != 3 {
		t.Fatalf("ListScans(name) total=%d len=%d, want 3/3", total, len(byName))
	}

	byDigest, total, err := s.ListScans("", "sha256:bbb", 0, 0)
	if err != nil {
		t.Fatalf("ListScans(digest): %v", err)
	}
	if total != 1 || len(byDigest) != 1 {
		t.Fatalf("ListScans(digest) total=%d len=%d, want 1/1", total, len(byDigest))
	}

	page, total, err := s.ListScans("", "", 2, 0)
	if err != nil {
		t.Fatalf("ListScans(limit): %v", err)
	}
	if total != 4 || len(page) != 2 {
		t.Fatalf("ListScans(limit=2) total=%d len=%d, want 4/2", total, len(page))
	}
}

func TestLatestScanForDigestIgnoresFailed(t *testing.T) {
	s, _ := openTemp(t)

	sc1, err := s.EnqueueScan("library/nginx", "latest", "sha256:ccc", "admin")
	if err != nil {
		t.Fatalf("EnqueueScan: %v", err)
	}
	if err := s.MarkScanFailed(sc1.ID, "boom"); err != nil {
		t.Fatalf("MarkScanFailed: %v", err)
	}

	// Only a failed scan exists — should not be returned as "latest success".
	if _, err := s.LatestScanForDigest("sha256:ccc"); !errors.Is(err, ErrScanNotFound) {
		t.Fatalf("LatestScanForDigest err = %v, want ErrScanNotFound", err)
	}

	sc2, err := s.EnqueueScan("library/nginx", "latest", "sha256:ccc", "admin")
	if err != nil {
		t.Fatalf("EnqueueScan: %v", err)
	}
	if err := s.MarkScanSucceeded(sc2.ID, 0, 0, 0, 0, 0, gzipBytes(t, "{}"), "0.56.2"); err != nil {
		t.Fatalf("MarkScanSucceeded: %v", err)
	}

	latest, err := s.LatestScanForDigest("sha256:ccc")
	if err != nil {
		t.Fatalf("LatestScanForDigest: %v", err)
	}
	if latest.ID != sc2.ID {
		t.Fatalf("LatestScanForDigest = %+v, want id %d", latest, sc2.ID)
	}
}
