package store

import (
	"path/filepath"
	"testing"
	"time"
)

func TestSeveritiesAtOrAbove(t *testing.T) {
	if got := SeveritiesAtOrAbove("HIGH"); len(got) != 2 || got[0] != "CRITICAL" || got[1] != "HIGH" {
		t.Fatalf("HIGH → %v", got)
	}
	if got := SeveritiesAtOrAbove(""); got != nil {
		t.Fatalf("empty threshold blocks %v", got)
	}
	if ValidBlockSeverity("UNKNOWN") || !ValidBlockSeverity("") || !ValidBlockSeverity("LOW") {
		t.Fatal("ValidBlockSeverity disagrees with the schema")
	}
}

func TestBlockingVulnerabilitiesHonoursTheAllowlistAndItsExpiry(t *testing.T) {
	st, err := Open(filepath.Join(t.TempDir(), "dockyard.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })

	sc, err := st.EnqueueScan("team/app", "1.0", "sha256:x", "test")
	if err != nil {
		t.Fatal(err)
	}
	if err := st.RecordScanVulnerabilities(sc.ID, []ScanVulnerability{
		{ID: "CVE-A", Severity: "CRITICAL"},
		{ID: "ghsa-lower-case-id", Severity: "HIGH"},
		{ID: "CVE-C", Severity: "MEDIUM"},
	}); err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	ids := func() []string {
		got, err := st.BlockingVulnerabilities(sc.ID, SeveritiesAtOrAbove("HIGH"), now)
		if err != nil {
			t.Fatal(err)
		}
		out := []string{}
		for _, v := range got {
			out = append(out, v.ID)
		}
		return out
	}

	if got := ids(); len(got) != 2 || got[0] != "CVE-A" {
		t.Fatalf("blocking = %v, want CVE-A first then the GHSA", got)
	}

	// An allowlist entry matches regardless of case.
	if err := st.AllowCVE("GHSA-LOWER-CASE-ID", "", "admin", nil); err != nil {
		t.Fatal(err)
	}
	if got := ids(); len(got) != 1 || got[0] != "CVE-A" {
		t.Fatalf("after allowlisting the GHSA = %v", got)
	}

	// An expired exception no longer applies.
	past := now.Add(-time.Hour)
	if err := st.AllowCVE("CVE-A", "temporary", "admin", &past); err != nil {
		t.Fatal(err)
	}
	if got := ids(); len(got) != 1 || got[0] != "CVE-A" {
		t.Fatalf("expired exception still applies: %v", got)
	}
	future := now.Add(time.Hour)
	if err := st.AllowCVE("CVE-A", "temporary", "admin", &future); err != nil {
		t.Fatal(err)
	}
	if got := ids(); len(got) != 0 {
		t.Fatalf("live exception ignored: %v", got)
	}

	if err := st.DisallowCVE("cve-nope"); err != ErrAllowedCVENotFound {
		t.Fatalf("removing an unknown CVE = %v", err)
	}
}
