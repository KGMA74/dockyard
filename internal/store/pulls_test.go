package store

import (
	"testing"
)

func TestRecordPullsUpsert(t *testing.T) {
	s, _ := openTemp(t)

	if err := s.RecordPulls(map[[2]string]int{{"team/app", "v1"}: 3}); err != nil {
		t.Fatal(err)
	}
	at, count, ok := s.LastPull("team/app", "v1")
	if !ok || count != 3 || at.IsZero() {
		t.Fatalf("LastPull = (%v, %d, %v)", at, count, ok)
	}

	if err := s.RecordPulls(map[[2]string]int{{"team/app", "v1"}: 2}); err != nil {
		t.Fatal(err)
	}
	_, count, _ = s.LastPull("team/app", "v1")
	if count != 5 {
		t.Errorf("count after second batch = %d, want 5", count)
	}
	if _, _, ok := s.LastPull("team/app", "never-pulled"); ok {
		t.Error("unknown reference reported as pulled")
	}
}

func TestPullTrackerBatchesAndFlushes(t *testing.T) {
	s, _ := openTemp(t)
	tracker := NewPullTracker(s)

	for range 10 {
		tracker.Record("lib/app", "latest")
	}
	tracker.Record("lib/app", "sha256:abc")
	tracker.Close() // flushes synchronously

	if _, count, ok := s.LastPull("lib/app", "latest"); !ok || count != 10 {
		t.Errorf("latest count = %d ok=%v, want 10", count, ok)
	}
	if _, _, ok := s.LastPull("lib/app", "sha256:abc"); !ok {
		t.Error("digest pull not recorded")
	}
}
