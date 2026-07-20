package admin

import (
	"bytes"
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
	"dockyard/internal/storage"
	"dockyard/internal/storage/storagetest"

	"github.com/labstack/echo/v4"
)

func genTestKey(t *testing.T) *ecdsa.PrivateKey {
	t.Helper()
	priv, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	return priv
}

func writeTestPublicKeyPEM(t *testing.T, dir string, pub *ecdsa.PublicKey) {
	t.Helper()
	der, err := x509.MarshalPKIXPublicKey(pub)
	if err != nil {
		t.Fatal(err)
	}
	block := pem.EncodeToMemory(&pem.Block{Type: "PUBLIC KEY", Bytes: der})
	if err := os.WriteFile(filepath.Join(dir, "key.pem"), block, 0o600); err != nil {
		t.Fatal(err)
	}
}

// pushSignatureManifest writes a cosign-convention signature manifest for
// digest directly into the backend (bypassing HTTP — this package tests the
// admin handler, not the v2 push path already covered elsewhere).
func pushSignatureManifest(t *testing.T, backend *storage.LocalBackend, name, digest string, priv *ecdsa.PrivateKey) {
	t.Helper()
	payload := fmt.Appendf(nil, `{"critical":{"image":{"docker-manifest-digest":%q},"type":"cosign container image signature"}}`, digest)
	hash := sha256.Sum256(payload)
	sig, err := ecdsa.SignASN1(rand.Reader, priv, hash[:])
	if err != nil {
		t.Fatal(err)
	}
	payloadDgst := storagetest.Digest(payload)
	if err := backend.PutBlob(payloadDgst, bytes.NewReader(payload), int64(len(payload))); err != nil {
		t.Fatal(err)
	}
	manifest := map[string]any{
		"layers": []map[string]any{
			{
				"digest": payloadDgst,
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
	if err := backend.PutManifest(name, cosign.SigTag(digest), storagetest.Digest(raw), raw); err != nil {
		t.Fatal(err)
	}
}

func TestGetManifestDetailsSignedStatus(t *testing.T) {
	const name = "team/app"
	config := []byte(`{"architecture":"amd64"}`)
	layer := []byte("layer-bytes")
	configDgst := storagetest.Digest(config)
	layerDgst := storagetest.Digest(layer)
	manifest := storagetest.ManifestFor(configDgst, layerDgst)
	manifestDgst := storagetest.Digest(manifest)

	newBackendWithManifest := func(t *testing.T) *storage.LocalBackend {
		t.Helper()
		backend, err := storage.NewLocal(t.TempDir())
		if err != nil {
			t.Fatal(err)
		}
		if err := backend.PutBlob(configDgst, bytes.NewReader(config), int64(len(config))); err != nil {
			t.Fatal(err)
		}
		if err := backend.PutBlob(layerDgst, bytes.NewReader(layer), int64(len(layer))); err != nil {
			t.Fatal(err)
		}
		if err := backend.PutManifest(name, "v1", manifestDgst, manifest); err != nil {
			t.Fatal(err)
		}
		return backend
	}

	call := func(t *testing.T, h *Handler) map[string]any {
		t.Helper()
		e := echo.New()
		req := httptest.NewRequest(http.MethodGet, "/api/admin/repositories/manifest?name="+name+"&reference=v1", nil)
		rec := httptest.NewRecorder()
		if err := h.GetManifestDetails(e.NewContext(req, rec)); err != nil {
			t.Fatal(err)
		}
		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d: %s", rec.Code, rec.Body.String())
		}
		var result map[string]any
		if err := json.Unmarshal(rec.Body.Bytes(), &result); err != nil {
			t.Fatal(err)
		}
		return result
	}

	t.Run("no keys configured: signed field absent", func(t *testing.T) {
		backend := newBackendWithManifest(t)
		h := New(backend, cosign.NewPolicy(false, nil, nil))
		result := call(t, h)
		if _, ok := result["signed"]; ok {
			t.Fatalf("signed field should be absent when no cosign keys are configured, got %v", result["signed"])
		}
	})

	t.Run("keys configured, no signature: signed=false", func(t *testing.T) {
		backend := newBackendWithManifest(t)
		dir := t.TempDir()
		writeTestPublicKeyPEM(t, dir, &genTestKey(t).PublicKey)
		keys, err := cosign.LoadPublicKeys(dir)
		if err != nil {
			t.Fatal(err)
		}
		h := New(backend, cosign.NewPolicy(false, keys, nil))
		result := call(t, h)
		if signed, _ := result["signed"].(bool); signed {
			t.Fatal("expected signed=false for an unsigned image")
		}
	})

	t.Run("valid signature: signed=true", func(t *testing.T) {
		backend := newBackendWithManifest(t)
		priv := genTestKey(t)
		dir := t.TempDir()
		writeTestPublicKeyPEM(t, dir, &priv.PublicKey)
		keys, err := cosign.LoadPublicKeys(dir)
		if err != nil {
			t.Fatal(err)
		}
		pushSignatureManifest(t, backend, name, manifestDgst, priv)
		h := New(backend, cosign.NewPolicy(false, keys, nil))
		result := call(t, h)
		if signed, _ := result["signed"].(bool); !signed {
			t.Fatal("expected signed=true for a validly signed image")
		}
	})
}
