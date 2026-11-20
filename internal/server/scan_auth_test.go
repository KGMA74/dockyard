package server

import (
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"dockyard/internal/auth"
	"dockyard/internal/store"
)

// TestMain lets this test binary stand in for trivy: the scan dispatcher
// runs it with trivy's own arguments ("image ...", "--version"), which no
// `go test` invocation starts with. It gets only the environment Dockyard
// gives trivy, so it is driven by those arguments alone.
func TestMain(m *testing.M) {
	if len(os.Args) > 1 && (os.Args[1] == "image" || os.Args[1] == "--version" || os.Args[1] == "--cache-dir") {
		os.Exit(fakeTrivy(os.Args[1:]))
	}
	os.Exit(m.Run())
}

// fakeTrivyCall is what the fake trivy saw, left in its cache dir.
type fakeTrivyCall struct {
	Args []string `json:"args"`
	Env  []string `json:"env"`
}

const fakeTrivyRecord = "fake-trivy-call.json"

// fakeTrivy pulls the image the way trivy (go-containerregistry) does with
// TRIVY_REGISTRY_TOKEN: an anonymous ping of /v2/, then the manifest and
// every blob with the token as a Bearer header, never asking /v2/token.
func fakeTrivy(args []string) int {
	switch {
	case args[0] == "--version":
		fmt.Println("Version: 0.56.2-fake")
		return 0
	case slices.Contains(args, "--download-db-only"):
		return 0
	}
	cacheDir := args[slices.Index(args, "--cache-dir")+1]
	raw, _ := json.Marshal(fakeTrivyCall{Args: os.Args, Env: os.Environ()})
	_ = os.WriteFile(filepath.Join(cacheDir, fakeTrivyRecord), raw, 0o600)

	ref := args[len(args)-1] // localhost:<port>/<name>@<digest>
	host, rest, _ := strings.Cut(ref, "/")
	name, digest, _ := strings.Cut(rest, "@")
	base := "http://" + host + "/v2/"
	token := os.Getenv("TRIVY_REGISTRY_TOKEN")

	get := func(url string, bearer bool) ([]byte, error) {
		req, _ := http.NewRequest(http.MethodGet, url, nil)
		req.Header.Set("Accept", "application/vnd.oci.image.manifest.v1+json, application/vnd.docker.distribution.manifest.v2+json")
		if bearer && token != "" {
			req.Header.Set("Authorization", "Bearer "+token)
		}
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			return nil, err
		}
		defer func() { _ = resp.Body.Close() }()
		body, _ := io.ReadAll(resp.Body)
		if resp.StatusCode != http.StatusOK {
			return nil, fmt.Errorf("GET %s: unexpected status code %d %s: %s", url, resp.StatusCode, http.StatusText(resp.StatusCode), body)
		}
		return body, nil
	}
	fail := func(err error) int {
		fmt.Fprintf(os.Stderr, "FATAL\tFatal error\trun error: image scan error: unable to find the specified image %q in [\"docker\" \"containerd\" \"podman\" \"remote\"]:\n  * remote error: %v\n", ref, err)
		return 1
	}

	if _, err := get(base, false); err == nil || !strings.Contains(err.Error(), "401") {
		return fail(fmt.Errorf("ping: expected a 401 challenge, got %v", err))
	}
	raw, err := get(base+name+"/manifests/"+digest, true)
	if err != nil {
		return fail(err)
	}
	var manifest struct {
		Config struct{ Digest string }   `json:"config"`
		Layers []struct{ Digest string } `json:"layers"`
	}
	if err := json.Unmarshal(raw, &manifest); err != nil {
		return fail(err)
	}
	blobs := []string{manifest.Config.Digest}
	for _, l := range manifest.Layers {
		blobs = append(blobs, l.Digest)
	}
	for _, b := range blobs {
		if _, err := get(base+name+"/blobs/"+b, true); err != nil {
			return fail(err)
		}
	}
	fmt.Print(`{"Results":[{"Target":"` + name + `","Vulnerabilities":[{"VulnerabilityID":"CVE-2024-1111","Severity":"HIGH"}]}]}`)
	return 0
}

type lockedBuffer struct {
	mu sync.Mutex
	b  strings.Builder
}

func (l *lockedBuffer) Write(p []byte) (int, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.b.Write(p)
}

func (l *lockedBuffer) String() string {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.b.String()
}

// The regression from 0.6.1: with V2_AUTH_ENABLED=true and anonymous pull
// off, trivy was handed credentials it does not read, pulled anonymously
// and got a 401 on every scan.
func TestScanWithRegistryAuthUsesAPerScanToken(t *testing.T) {
	// Dockyard's own environment must not reach trivy.
	t.Setenv("JWT_SECRET", "jwt-secret-from-dockyard-env")
	t.Setenv("ADMIN_PASSWORD", "admin-password-from-dockyard-env")
	logs := &lockedBuffer{}
	prevLogger := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(logs, &slog.HandlerOptions{Level: slog.LevelDebug})))
	t.Cleanup(func() { slog.SetDefault(prevLogger) })

	self, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	cacheDir := t.TempDir()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	var st *store.Store
	h := newTestServer(t, func(s *Server) {
		s.v2AuthEnabled = true
		s.v2AnonymousPull = false
		s.scanEnabled = true
		s.trivyOffline = true // no DB warm-up
		s.trivyBinPath = self
		s.trivyCacheDir = cacheDir
		s.port = listener.Addr().(*net.TCPAddr).Port // trivy pulls from localhost:<port>
		s.scanTimeout = 30 * time.Second
		st = s.store
	})
	srv := httptest.NewUnstartedServer(h)
	_ = srv.Listener.Close()
	srv.Listener = listener
	srv.Start()
	t.Cleanup(srv.Close)

	adminJWT := loginAs(t, srv.URL, "admin", "test-password")
	pushToken := fetchToken(t, srv.URL, "admin", "test-password")
	pushImage(t, srv.URL, pushToken, "team/app", "1.0")
	pushImage(t, srv.URL, pushToken, "team/other", "1.0")

	body, _ := json.Marshal(map[string]string{"name": "team/app", "reference": "1.0"})
	status, raw := apiCall(t, srv.URL, http.MethodPost, "/api/admin/scans", adminJWT, body)
	if status != http.StatusAccepted {
		t.Fatalf("trigger scan = %d: %s", status, raw)
	}
	var queued struct {
		Scan store.ScanResult `json:"scan"`
	}
	if err := json.Unmarshal(raw, &queued); err != nil {
		t.Fatal(err)
	}

	var sc store.ScanResult
	deadline := time.Now().Add(45 * time.Second)
	for {
		_, raw = apiCall(t, srv.URL, http.MethodGet, fmt.Sprintf("/api/admin/scans/%d", queued.Scan.ID), adminJWT, nil)
		if err := json.Unmarshal(raw, &sc); err != nil {
			t.Fatal(err)
		}
		if sc.Status == "succeeded" || sc.Status == "failed" {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("scan still %q", sc.Status)
		}
		time.Sleep(50 * time.Millisecond)
	}
	if sc.Status != "succeeded" || sc.HighCount != 1 {
		t.Fatalf("scan = %s (high=%d), error: %s", sc.Status, sc.HighCount, sc.Error)
	}

	rawCall, err := os.ReadFile(filepath.Join(cacheDir, fakeTrivyRecord))
	if err != nil {
		t.Fatal(err)
	}
	var call fakeTrivyCall
	if err := json.Unmarshal(rawCall, &call); err != nil {
		t.Fatal(err)
	}
	var token string
	for _, kv := range call.Env {
		if v, ok := strings.CutPrefix(kv, "TRIVY_REGISTRY_TOKEN="); ok {
			token = v
		}
		if strings.Contains(kv, "from-dockyard-env") {
			t.Errorf("trivy inherited %q", kv)
		}
	}
	if token == "" {
		t.Fatalf("trivy got no TRIVY_REGISTRY_TOKEN: %v", call.Env)
	}
	if strings.Contains(strings.Join(call.Args, " "), token) {
		t.Fatalf("token on trivy's command line: %v", call.Args)
	}

	t.Run("the token is the scanner's, for this repository, read-only", func(t *testing.T) {
		digest := imageDigest("team/app", "1.0")
		checks := []struct {
			name, method, path string
			want               int
		}{
			{"pull the scanned image", http.MethodGet, "/v2/team/app/manifests/" + digest, http.StatusOK},
			{"pull another repository", http.MethodGet, "/v2/team/other/manifests/1.0", http.StatusForbidden},
			{"push to the scanned repository", http.MethodPut, "/v2/team/app/manifests/2.0", http.StatusForbidden},
			{"delete from the scanned repository", http.MethodDelete, "/v2/team/app/manifests/" + digest, http.StatusForbidden},
		}
		for _, c := range checks {
			if got := authedStatus(t, srv.URL, c.method, c.path, token, nil); got != c.want {
				t.Errorf("%s = %d, want %d", c.name, got, c.want)
			}
		}
		status, raw := apiCall(t, srv.URL, http.MethodGet, "/v2/_catalog", token, nil)
		if status != http.StatusOK || strings.Contains(string(raw), "team/other") {
			t.Errorf("catalog = %d %s, want team/app alone", status, raw)
		}
		if status, _ := apiCall(t, srv.URL, http.MethodGet, "/api/admin/repositories", token, nil); status != http.StatusForbidden {
			t.Errorf("admin API with the scanner token = %d, want 403", status)
		}
	})

	t.Run("the token is in neither the stored scan nor the logs", func(t *testing.T) {
		signature := token[strings.LastIndexByte(token, '.')+1:]
		_, report := apiCall(t, srv.URL, http.MethodGet, fmt.Sprintf("/api/admin/scans/%d/report", sc.ID), adminJWT, nil)
		stored, _ := st.ScanByID(sc.ID)
		for where, text := range map[string]string{"report": string(report), "scan error": stored.Error, "logs": logs.String()} {
			if strings.Contains(text, signature) {
				t.Errorf("%s carries the scanner token", where)
			}
		}
	})
}

func TestScannerTokenOnlyInEmbeddedModeWithRegistryAuth(t *testing.T) {
	st, err := store.Open(filepath.Join(t.TempDir(), "dockyard.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	mgr, err := auth.New("admin", "test-password", "test-secret", "", t.TempDir(), st)
	if err != nil {
		t.Fatal(err)
	}
	cases := []struct {
		name      string
		mode      mode
		v2Auth    bool
		wantToken bool
	}{
		{"embedded, auth off", modeEmbedded, false, false},
		{"embedded, auth on", modeEmbedded, true, true},
		{"proxy, auth off", modeProxy, false, false},
		{"proxy, auth on: upstream credentials apply", modeProxy, true, false},
	}
	for _, tc := range cases {
		s := &Server{mode: tc.mode, v2AuthEnabled: tc.v2Auth, auth: mgr}
		if got := s.scannerTokenSource() != nil; got != tc.wantToken {
			t.Errorf("%s: token source = %v, want %v", tc.name, got, tc.wantToken)
		}
	}
}
