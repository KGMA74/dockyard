package v2

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"dockyard/internal/cosign"
	"dockyard/internal/events"
	"dockyard/internal/storage"
	"dockyard/internal/storage/storagetest"
)

func genSigningKey(t *testing.T) *ecdsa.PrivateKey {
	t.Helper()
	priv, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	return priv
}

func requireSignedServer(t *testing.T, priv *ecdsa.PrivateKey) *httptest.Server {
	t.Helper()
	backend, err := storage.NewLocal(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	der, err := x509.MarshalPKIXPublicKey(&priv.PublicKey)
	if err != nil {
		t.Fatal(err)
	}
	pemBytes := pem.EncodeToMemory(&pem.Block{Type: "PUBLIC KEY", Bytes: der})
	if err := os.WriteFile(filepath.Join(dir, "key.pem"), pemBytes, 0o600); err != nil {
		t.Fatal(err)
	}
	keys, err := cosign.LoadPublicKeys(dir)
	if err != nil {
		t.Fatal(err)
	}
	policy := cosign.NewPolicy(true, keys, nil) // required for every repo, no store = no overrides
	srv := httptest.NewServer(New(backend, events.NewHub(), policy))
	t.Cleanup(srv.Close)
	return srv
}

// pushCosignSignature pushes a signature manifest (in the cosign tag
// convention) covering digest, signed with priv.
func pushCosignSignature(t *testing.T, base, name, digest string, priv *ecdsa.PrivateKey) {
	t.Helper()
	payload := fmt.Appendf(nil, `{"critical":{"image":{"docker-manifest-digest":%q},"type":"cosign container image signature"}}`, digest)
	hash := sha256.Sum256(payload)
	sig, err := ecdsa.SignASN1(rand.Reader, priv, hash[:])
	if err != nil {
		t.Fatal(err)
	}
	payloadDgst := pushBlobMonolithic(t, base, name, payload)

	manifest := map[string]any{
		"schemaVersion": 2,
		"layers": []map[string]any{
			{
				"mediaType": "application/vnd.dev.cosign.simplesigning.v1+json",
				"digest":    payloadDgst,
				"size":      len(payload),
				"annotations": map[string]string{
					"dev.cosignproject.cosign/signature": base64.StdEncoding.EncodeToString(sig),
				},
			},
		},
	}
	raw, err := json.Marshal(manifest)
	if err != nil {
		t.Fatal(err)
	}
	resp := do(t, http.MethodPut, base+"/v2/"+name+"/manifests/"+cosign.SigTag(digest), raw)
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("signature manifest PUT: status = %d, want 201", resp.StatusCode)
	}
}

func TestSignedPushRejectsUnsignedTag(t *testing.T) {
	priv := genSigningKey(t)
	srv := requireSignedServer(t, priv)
	const name = "team/app"

	configDgst := pushBlobMonolithic(t, srv.URL, name, []byte(`{"architecture":"amd64"}`))
	layerDgst := pushBlobMonolithic(t, srv.URL, name, []byte("layer-bytes"))
	manifest := storagetest.ManifestFor(configDgst, layerDgst)

	resp := do(t, http.MethodPut, srv.URL+"/v2/"+name+"/manifests/v1", manifest)
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("unsigned tag push: status = %d, want 403", resp.StatusCode)
	}
}

func TestSignedPushAllowsDigestPush(t *testing.T) {
	priv := genSigningKey(t)
	srv := requireSignedServer(t, priv)
	const name = "team/app"

	configDgst := pushBlobMonolithic(t, srv.URL, name, []byte(`{"architecture":"amd64"}`))
	layerDgst := pushBlobMonolithic(t, srv.URL, name, []byte("layer-bytes"))
	manifest := storagetest.ManifestFor(configDgst, layerDgst)
	manifestDgst := storagetest.Digest(manifest)

	// Push by digest is exempt — this is how cosign attaches a signature to
	// an image that isn't tagged yet.
	resp := do(t, http.MethodPut, srv.URL+"/v2/"+name+"/manifests/"+manifestDgst, manifest)
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("digest push: status = %d, want 201", resp.StatusCode)
	}
}

func TestSignedPushAllowsSignatureArtifactTag(t *testing.T) {
	priv := genSigningKey(t)
	srv := requireSignedServer(t, priv)
	const name = "team/app"

	configDgst := pushBlobMonolithic(t, srv.URL, name, []byte(`{"architecture":"amd64"}`))
	layerDgst := pushBlobMonolithic(t, srv.URL, name, []byte("layer-bytes"))
	manifest := storagetest.ManifestFor(configDgst, layerDgst)
	manifestDgst := storagetest.Digest(manifest)

	if resp := do(t, http.MethodPut, srv.URL+"/v2/"+name+"/manifests/"+manifestDgst, manifest); resp.StatusCode != http.StatusCreated {
		t.Fatalf("digest push: status = %d, want 201", resp.StatusCode)
	}
	pushCosignSignature(t, srv.URL, name, manifestDgst, priv)
}

func TestSignedPushAllowsTagOnceSigned(t *testing.T) {
	priv := genSigningKey(t)
	srv := requireSignedServer(t, priv)
	const name = "team/app"

	configDgst := pushBlobMonolithic(t, srv.URL, name, []byte(`{"architecture":"amd64"}`))
	layerDgst := pushBlobMonolithic(t, srv.URL, name, []byte("layer-bytes"))
	manifest := storagetest.ManifestFor(configDgst, layerDgst)
	manifestDgst := storagetest.Digest(manifest)

	// Standard cosign flow: push by digest, sign, then tag.
	if resp := do(t, http.MethodPut, srv.URL+"/v2/"+name+"/manifests/"+manifestDgst, manifest); resp.StatusCode != http.StatusCreated {
		t.Fatalf("digest push: status = %d, want 201", resp.StatusCode)
	}
	pushCosignSignature(t, srv.URL, name, manifestDgst, priv)

	resp := do(t, http.MethodPut, srv.URL+"/v2/"+name+"/manifests/v1", manifest)
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("tag push after signing: status = %d, want 201 (body: %s)", resp.StatusCode, readBody(resp))
	}
}

func TestSignedPushRejectsWrongKeySignature(t *testing.T) {
	priv := genSigningKey(t)
	other := genSigningKey(t) // signs with a key not in the trust store
	srv := requireSignedServer(t, priv)
	const name = "team/app"

	configDgst := pushBlobMonolithic(t, srv.URL, name, []byte(`{"architecture":"amd64"}`))
	layerDgst := pushBlobMonolithic(t, srv.URL, name, []byte("layer-bytes"))
	manifest := storagetest.ManifestFor(configDgst, layerDgst)
	manifestDgst := storagetest.Digest(manifest)

	if resp := do(t, http.MethodPut, srv.URL+"/v2/"+name+"/manifests/"+manifestDgst, manifest); resp.StatusCode != http.StatusCreated {
		t.Fatalf("digest push: status = %d, want 201", resp.StatusCode)
	}
	pushCosignSignature(t, srv.URL, name, manifestDgst, other)

	resp := do(t, http.MethodPut, srv.URL+"/v2/"+name+"/manifests/v1", manifest)
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("tag push signed by an untrusted key: status = %d, want 403", resp.StatusCode)
	}
}

func readBody(resp *http.Response) string {
	buf := make([]byte, 512)
	n, _ := resp.Body.Read(buf)
	return string(buf[:n])
}
