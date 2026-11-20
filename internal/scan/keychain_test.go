package scan

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"sync"
	"testing"

	// The version trivy 0.56.2 builds with (its go.mod): this exercises the
	// very keychain trivy resolves registry credentials through when no
	// TRIVY_USERNAME/TRIVY_PASSWORD/TRIVY_REGISTRY_TOKEN is set.
	"github.com/google/go-containerregistry/pkg/authn"
	"github.com/google/go-containerregistry/pkg/name"
	"github.com/google/go-containerregistry/pkg/v1/remote"
)

// fakeRegistry serves one manifest and records the Authorization header of
// every request. With token set it behaves like Dockyard with registry auth
// on: a Bearer challenge until the token is presented. /v2/token is not
// served — a registrytoken must never need it.
type fakeRegistry struct {
	*httptest.Server
	token string

	mu   sync.Mutex
	auth []string
}

func newFakeRegistry(t *testing.T, token string) *fakeRegistry {
	t.Helper()
	r := &fakeRegistry{token: token}
	r.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		r.mu.Lock()
		r.auth = append(r.auth, req.Header.Get("Authorization"))
		r.mu.Unlock()
		if r.token != "" && req.Header.Get("Authorization") != "Bearer "+r.token {
			w.Header().Set("WWW-Authenticate", `Bearer realm="http://`+req.Host+`/v2/token",service="dockyard-registry"`)
			http.Error(w, `{"errors":[{"code":"UNAUTHORIZED"}]}`, http.StatusUnauthorized)
			return
		}
		switch req.URL.Path {
		case "/v2/":
			w.WriteHeader(http.StatusOK)
		case "/v2/team/app/manifests/1.0":
			w.Header().Set("Content-Type", "application/vnd.oci.image.manifest.v1+json")
			_, _ = w.Write([]byte(`{"schemaVersion":2,"mediaType":"application/vnd.oci.image.manifest.v1+json","config":{"mediaType":"application/vnd.oci.image.config.v1+json","digest":"sha256:44136fa355b3678a1146ad16f7e8649e94fb4fc21fe77e8310c060f61caaff8a","size":2},"layers":[]}`))
		default:
			http.NotFound(w, req)
		}
	}))
	t.Cleanup(r.Close)
	return r
}

func (r *fakeRegistry) host(t *testing.T, as string) string {
	u, err := url.Parse(r.URL)
	if err != nil {
		t.Fatal(err)
	}
	return as + ":" + u.Port()
}

func (r *fakeRegistry) authHeaders() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]string(nil), r.auth...)
}

// The per-scan DOCKER_CONFIG gives Dockyard's host — and only it — the
// token: go-containerregistry presents it there as a Bearer header without a
// /v2/token round trip, and stays anonymous everywhere else, which is what
// lets trivy fetch its Java DB from ghcr.io in the same process.
func TestGoContainerRegistryPresentsTheTokenToDockyardOnly(t *testing.T) {
	token := fakeJWT("scan-token-for-the-keychain-test")
	dockyard := newFakeRegistry(t, token)
	elsewhere := newFakeRegistry(t, "") // ghcr.io stand-in: anonymous pulls

	dockyardHost := dockyard.host(t, "localhost") // as RegistryURL names it
	dir, err := writeRegistryAuth(t.TempDir(), dockyardHost, token)
	if err != nil {
		t.Fatal(err)
	}
	// Only the per-scan config may be found: no ~/.docker, no podman file.
	t.Setenv("HOME", t.TempDir())
	t.Setenv("USERPROFILE", t.TempDir())
	t.Setenv("DOCKER_CONFIG", dir)
	t.Setenv("REGISTRY_AUTH_FILE", "")
	t.Setenv("XDG_RUNTIME_DIR", "")

	pull := func(host string) error {
		ref, err := name.ParseReference(host+"/team/app:1.0", name.Insecure)
		if err != nil {
			t.Fatal(err)
		}
		_, err = remote.Get(ref, remote.WithAuthFromKeychain(authn.DefaultKeychain))
		return err
	}

	if err := pull(dockyardHost); err != nil {
		t.Fatalf("pull from Dockyard with the scan's docker config: %v", err)
	}
	sawToken := false
	for _, h := range dockyard.authHeaders() {
		if h == "Bearer "+token {
			sawToken = true
		}
	}
	if !sawToken {
		t.Fatalf("Dockyard never got the token: %q", dockyard.authHeaders())
	}

	for _, host := range []string{elsewhere.host(t, "127.0.0.1"), elsewhere.host(t, "localhost")} {
		if err := pull(host); err != nil {
			t.Fatalf("anonymous pull from %s: %v", host, err)
		}
	}
	for _, h := range elsewhere.authHeaders() {
		if h != "" {
			t.Fatalf("another registry got an Authorization header: %q", h)
		}
	}

	for _, other := range []string{"ghcr.io/aquasecurity/trivy-java-db", "ghcr.io/aquasecurity/trivy-db", "index.docker.io/library/alpine"} {
		repo, err := name.NewRepository(other)
		if err != nil {
			t.Fatal(err)
		}
		auth, err := authn.DefaultKeychain.Resolve(repo)
		if err != nil {
			t.Fatal(err)
		}
		if auth != authn.Anonymous {
			cfg, _ := auth.Authorization()
			t.Fatalf("%s resolves to credentials %+v, want anonymous", other, cfg)
		}
	}
}
