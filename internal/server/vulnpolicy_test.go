package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"dockyard/internal/storage/storagetest"
	"dockyard/internal/store"
)

// imageDigest is the manifest digest pushImage produces for name:tag.
func imageDigest(name, tag string) string {
	layer := []byte("layer of " + name + ":" + tag)
	config := []byte(`{"architecture":"amd64","os":"linux","ref":"` + name + ":" + tag + `"}`)
	return storagetest.Digest(storagetest.ManifestFor(storagetest.Digest(config), storagetest.Digest(layer)))
}

func recordScan(t *testing.T, st *store.Store, name, tag, digest string, vulns []store.ScanVulnerability) {
	t.Helper()
	sc, err := st.EnqueueScan(name, tag, digest, "test")
	if err != nil {
		t.Fatal(err)
	}
	if err := st.RecordScanVulnerabilities(sc.ID, vulns); err != nil {
		t.Fatal(err)
	}
	if err := st.MarkScanSucceeded(sc.ID, 1, 0, 1, 0, 0, nil, "test"); err != nil {
		t.Fatal(err)
	}
}

func setPolicy(t *testing.T, base, adminJWT string, scanOnPush bool, block string) {
	t.Helper()
	body, _ := json.Marshal(map[string]any{"scan_on_push": scanOnPush, "block_severity": block})
	if status, raw := apiCall(t, base, http.MethodPut, "/api/admin/vulnerability-policy", adminJWT, body); status != http.StatusOK {
		t.Fatalf("set policy = %d: %s", status, raw)
	}
}

func TestVulnerabilityGate(t *testing.T) {
	var st *store.Store
	var scannerUser, scannerPass string
	srv := httptest.NewServer(newTestServer(t, func(s *Server) {
		s.v2AuthEnabled = true
		s.scanEnabled = true
		s.trivyOffline = true // no DB download in tests; no trivy binary either
		st = s.store
		scannerUser, scannerPass = s.auth.ScannerCredentials()
	}))
	t.Cleanup(srv.Close)
	adminJWT := loginAs(t, srv.URL, "admin", "test-password")
	pushToken := fetchToken(t, srv.URL, "admin", "test-password")
	pushImage(t, srv.URL, pushToken, "team/app", "1.0")
	digest := imageDigest("team/app", "1.0")
	recordScan(t, st, "team/app", "1.0", digest, []store.ScanVulnerability{
		{ID: "CVE-2024-0001", Severity: "CRITICAL"},
		{ID: "CVE-2024-0002", Severity: "MEDIUM"},
	})
	createUser(t, srv.URL, adminJWT, "dev", "dev-password", "reader")
	pull := func(ref string) int {
		return authedStatus(t, srv.URL, http.MethodGet, "/v2/team/app/manifests/"+ref, fetchToken(t, srv.URL, "dev", "dev-password"), nil)
	}

	if got := pull("1.0"); got != http.StatusOK {
		t.Fatalf("pull with no blocking policy = %d", got)
	}

	setPolicy(t, srv.URL, adminJWT, false, "HIGH")

	t.Run("blocks pulls by tag and by digest", func(t *testing.T) {
		if got := pull("1.0"); got != http.StatusForbidden {
			t.Fatalf("pull by tag = %d, want 403", got)
		}
		if got := pull(digest); got != http.StatusForbidden {
			t.Fatalf("pull by digest = %d, want 403", got)
		}
	})

	t.Run("explains why in the registry error", func(t *testing.T) {
		req, _ := http.NewRequest(http.MethodGet, srv.URL+"/v2/team/app/manifests/1.0", nil)
		req.Header.Set("Authorization", "Bearer "+fetchToken(t, srv.URL, "dev", "dev-password"))
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer func() { _ = resp.Body.Close() }()
		var body struct {
			Errors []struct{ Code, Message string } `json:"errors"`
		}
		_ = json.NewDecoder(resp.Body).Decode(&body)
		if len(body.Errors) != 1 || body.Errors[0].Code != "DENIED" ||
			!strings.Contains(body.Errors[0].Message, "CVE-2024-0001") ||
			strings.Contains(body.Errors[0].Message, "CVE-2024-0002") {
			t.Fatalf("unexpected error body: %+v", body)
		}
	})

	t.Run("HEAD still answers, so clients can resolve the digest", func(t *testing.T) {
		if got := authedStatus(t, srv.URL, http.MethodHead, "/v2/team/app/manifests/1.0", pushToken, nil); got != http.StatusOK {
			t.Fatalf("HEAD = %d", got)
		}
	})

	t.Run("the scanner can still pull a blocked image to re-scan it", func(t *testing.T) {
		token := fetchToken(t, srv.URL, scannerUser, scannerPass)
		if got := authedStatus(t, srv.URL, http.MethodGet, "/v2/team/app/manifests/1.0", token, nil); got != http.StatusOK {
			t.Fatalf("scanner pull = %d", got)
		}
		if status, _ := apiCall(t, srv.URL, http.MethodGet, "/api/admin/repositories", token, nil); status != http.StatusForbidden {
			t.Fatalf("scanner token on admin API = %d, want 403", status)
		}
	})

	t.Run("the tag list flags the tag and the audit log records the refusal", func(t *testing.T) {
		_, raw := apiCall(t, srv.URL, http.MethodGet, "/api/admin/repositories/tags?name=team/app", adminJWT, nil)
		if !strings.Contains(string(raw), `"pull_blocked":true`) {
			t.Fatalf("tag list does not flag the blocked tag: %s", raw)
		}
		_, audit := apiCall(t, srv.URL, http.MethodGet, "/api/admin/audit", adminJWT, nil)
		if !strings.Contains(string(audit), "pull-blocked") {
			t.Fatalf("blocked pull not audited: %s", audit)
		}
	})

	t.Run("allowlisting the CVE unblocks the image", func(t *testing.T) {
		body := []byte(`{"cve_id":"cve-2024-0001","reason":"not reachable in our usage"}`)
		if status, raw := apiCall(t, srv.URL, http.MethodPost, "/api/admin/vulnerability-policy/allowlist", adminJWT, body); status != http.StatusCreated {
			t.Fatalf("allow = %d: %s", status, raw)
		}
		if got := pull("1.0"); got != http.StatusOK {
			t.Fatalf("pull after allowlisting = %d", got)
		}
		if status, _ := apiCall(t, srv.URL, http.MethodDelete, "/api/admin/vulnerability-policy/allowlist/CVE-2024-0001", adminJWT, nil); status != http.StatusOK {
			t.Fatalf("disallow = %d", status)
		}
		if got := pull("1.0"); got != http.StatusForbidden {
			t.Fatalf("pull after removing the exception = %d, want 403", got)
		}
	})

	t.Run("a lower threshold also blocks medium", func(t *testing.T) {
		body := []byte(`{"cve_id":"CVE-2024-0001"}`)
		apiCall(t, srv.URL, http.MethodPost, "/api/admin/vulnerability-policy/allowlist", adminJWT, body)
		setPolicy(t, srv.URL, adminJWT, false, "MEDIUM")
		if got := pull("1.0"); got != http.StatusForbidden {
			t.Fatalf("pull with a medium threshold = %d, want 403", got)
		}
		setPolicy(t, srv.URL, adminJWT, false, "")
		if got := pull("1.0"); got != http.StatusOK {
			t.Fatalf("pull with blocking off = %d", got)
		}
	})

	t.Run("unscanned images are not blocked", func(t *testing.T) {
		setPolicy(t, srv.URL, adminJWT, false, "LOW")
		pushImage(t, srv.URL, pushToken, "team/fresh", "1.0")
		token := fetchToken(t, srv.URL, "dev", "dev-password")
		if got := authedStatus(t, srv.URL, http.MethodGet, "/v2/team/fresh/manifests/1.0", token, nil); got != http.StatusOK {
			t.Fatalf("unscanned pull = %d", got)
		}
	})

	t.Run("policy is admin-only", func(t *testing.T) {
		dev := loginAs(t, srv.URL, "dev", "dev-password")
		if status, _ := apiCall(t, srv.URL, http.MethodGet, "/api/admin/vulnerability-policy", dev, nil); status != http.StatusForbidden {
			t.Fatalf("reader reading the policy = %d, want 403", status)
		}
	})
}

func TestScanOnPushQueuesAScan(t *testing.T) {
	var st *store.Store
	srv := httptest.NewServer(newTestServer(t, func(s *Server) {
		s.v2AuthEnabled = true
		s.scanEnabled = true
		s.trivyOffline = true
		st = s.store
	}))
	t.Cleanup(srv.Close)
	adminJWT := loginAs(t, srv.URL, "admin", "test-password")
	token := fetchToken(t, srv.URL, "admin", "test-password")

	pushImage(t, srv.URL, token, "team/off", "1.0")
	setPolicy(t, srv.URL, adminJWT, true, "")
	pushImage(t, srv.URL, token, "team/on", "1.0")

	deadline := time.Now().Add(3 * time.Second)
	for {
		scans, _, err := st.ListScans("", "", 50, 0)
		if err != nil {
			t.Fatal(err)
		}
		var on, off bool
		for _, sc := range scans {
			on = on || (sc.Name == "team/on" && sc.RequestedBy == "on-push" && sc.Digest == imageDigest("team/on", "1.0"))
			off = off || sc.Name == "team/off"
		}
		if off {
			t.Fatal("a push made before scan-on-push was enabled was scanned")
		}
		if on {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("no on-push scan queued; scans: %+v", scans)
		}
		time.Sleep(50 * time.Millisecond)
	}
}
