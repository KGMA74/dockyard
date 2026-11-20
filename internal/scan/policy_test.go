package scan

import (
	"sort"
	"strings"
	"testing"

	"dockyard/internal/store"
)

func TestDistinctVulnerabilitiesKeepsTheWorstSeverity(t *testing.T) {
	report := []byte(`{"Results":[
		{"Vulnerabilities":[
			{"VulnerabilityID":"CVE-1","Severity":"MEDIUM"},
			{"VulnerabilityID":"CVE-2","Severity":"low"}
		]},
		{"Vulnerabilities":[
			{"VulnerabilityID":"CVE-1","Severity":"CRITICAL"},
			{"VulnerabilityID":"GHSA-abcd-efgh-ijkl","Severity":"weird"},
			{"VulnerabilityID":"","Severity":"HIGH"}
		]}
	]}`)
	got, err := distinctVulnerabilities(report)
	if err != nil {
		t.Fatal(err)
	}
	sort.Slice(got, func(i, j int) bool { return got[i].ID < got[j].ID })
	want := []store.ScanVulnerability{
		{ID: "CVE-1", Severity: "CRITICAL"},
		{ID: "CVE-2", Severity: "LOW"},
		{ID: "GHSA-abcd-efgh-ijkl", Severity: "UNKNOWN"},
	}
	if len(got) != len(want) {
		t.Fatalf("got %+v, want %+v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("got %+v, want %+v", got, want)
		}
	}
}

func TestBlockedMessageNamesTheImageAndCapsTheList(t *testing.T) {
	var blockers []store.ScanVulnerability
	for _, id := range []string{"CVE-1", "CVE-2", "CVE-3", "CVE-4", "CVE-5", "CVE-6", "CVE-7"} {
		blockers = append(blockers, store.ScanVulnerability{ID: id, Severity: "HIGH"})
	}
	blockers[0].Severity = "CRITICAL"
	msg := BlockedMessage("team/app", "1.0", "HIGH", blockers)
	for _, want := range []string{"team/app:1.0", "high and above", "1 critical, 6 high", "CVE-5", "+2 more"} {
		if !strings.Contains(msg, want) {
			t.Errorf("message %q lacks %q", msg, want)
		}
	}
	if strings.Contains(msg, "CVE-6") {
		t.Errorf("message lists more than five IDs: %q", msg)
	}
	digestRef := "sha256:" + strings.Repeat("a", 64)
	if msg := BlockedMessage("team/app", digestRef, "HIGH", blockers[:1]); !strings.Contains(msg, "team/app@sha256:") {
		t.Errorf("digest reference not shown with @: %q", msg)
	}
}
