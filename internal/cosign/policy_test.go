package cosign

import (
	"crypto"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"dockyard/internal/store"
)

func openTestStore(t *testing.T) *store.Store {
	t.Helper()
	s, err := store.Open(filepath.Join(t.TempDir(), "dockyard.db"))
	if err != nil {
		t.Fatalf("store.Open: %v", err)
	}
	t.Cleanup(func() { _ = s.Close() })
	return s
}

// fakeFetcher is an in-memory Fetcher for tests — keyed by "name/reference".
type fakeFetcher struct {
	manifests map[string][]byte
	blobs     map[string][]byte
}

func newFakeFetcher() *fakeFetcher {
	return &fakeFetcher{manifests: map[string][]byte{}, blobs: map[string][]byte{}}
}

func (f *fakeFetcher) GetManifest(name, reference string) ([]byte, string, error) {
	raw, ok := f.manifests[name+"/"+reference]
	if !ok {
		return nil, "", fmt.Errorf("not found: %s/%s", name, reference)
	}
	return raw, "sha256:manifest", nil
}

func (f *fakeFetcher) GetBlob(_, digest string) ([]byte, error) {
	raw, ok := f.blobs[digest]
	if !ok {
		return nil, fmt.Errorf("blob not found: %s", digest)
	}
	return raw, nil
}

// signedFixture builds a fake cosign signature manifest for digest, signed
// with priv, and registers it (plus the payload blob) on the fetcher.
func signedFixture(t *testing.T, f *fakeFetcher, priv *ecdsa.PrivateKey, name, digest string) {
	t.Helper()
	payload := fmt.Appendf(nil, `{"critical":{"image":{"docker-manifest-digest":%q},"type":"cosign container image signature"}}`, digest)
	hash := sha256.Sum256(payload)
	sig, err := ecdsa.SignASN1(rand.Reader, priv, hash[:])
	if err != nil {
		t.Fatal(err)
	}
	blobDigest := "sha256:" + fmt.Sprintf("%x", sha256.Sum256(payload))
	f.blobs[blobDigest] = payload

	manifest := map[string]any{
		"layers": []map[string]any{
			{
				"digest": blobDigest,
				"annotations": map[string]string{
					signatureAnnotation: base64.StdEncoding.EncodeToString(sig),
				},
			},
		},
	}
	raw, err := json.Marshal(manifest)
	if err != nil {
		t.Fatal(err)
	}
	f.manifests[name+"/"+SigTag(digest)] = raw
}

func genKey(t *testing.T) *ecdsa.PrivateKey {
	t.Helper()
	priv, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	return priv
}

func writePublicKeyPEM(t *testing.T, dir, name string, pub crypto.PublicKey) {
	t.Helper()
	der, err := x509.MarshalPKIXPublicKey(pub)
	if err != nil {
		t.Fatal(err)
	}
	block := &pem.Block{Type: "PUBLIC KEY", Bytes: der}
	if err := os.WriteFile(filepath.Join(dir, name), pem.EncodeToMemory(block), 0o600); err != nil {
		t.Fatal(err)
	}
}

func TestLoadPublicKeysRoundTrip(t *testing.T) {
	dir := t.TempDir()
	priv := genKey(t)
	writePublicKeyPEM(t, dir, "key1.pem", &priv.PublicKey)

	keys, err := LoadPublicKeys(dir)
	if err != nil {
		t.Fatalf("LoadPublicKeys: %v", err)
	}
	if len(keys) != 1 {
		t.Fatalf("got %d keys, want 1", len(keys))
	}
}

func TestLoadPublicKeysEmptyDir(t *testing.T) {
	keys, err := LoadPublicKeys("")
	if err != nil || keys != nil {
		t.Fatalf("LoadPublicKeys(\"\") = %v, %v, want nil, nil", keys, err)
	}
}

func TestPolicySignedValid(t *testing.T) {
	dir := t.TempDir()
	priv := genKey(t)
	writePublicKeyPEM(t, dir, "key1.pem", &priv.PublicKey)
	keys, err := LoadPublicKeys(dir)
	if err != nil {
		t.Fatal(err)
	}

	f := newFakeFetcher()
	digest := "sha256:" + fmt.Sprintf("%064x", 1)
	signedFixture(t, f, priv, "team/app", digest)

	p := NewPolicy(false, keys, nil)
	if !p.Signed(f, "team/app", digest) {
		t.Fatal("expected signature to verify")
	}
}

func TestPolicySignedWrongKey(t *testing.T) {
	dir := t.TempDir()
	priv := genKey(t)
	other := genKey(t)
	writePublicKeyPEM(t, dir, "key1.pem", &other.PublicKey) // configured key != signer
	keys, err := LoadPublicKeys(dir)
	if err != nil {
		t.Fatal(err)
	}

	f := newFakeFetcher()
	digest := "sha256:" + fmt.Sprintf("%064x", 2)
	signedFixture(t, f, priv, "team/app", digest)

	p := NewPolicy(false, keys, nil)
	if p.Signed(f, "team/app", digest) {
		t.Fatal("expected signature verification to fail against the wrong key")
	}
}

func TestPolicySignedDigestMismatch(t *testing.T) {
	dir := t.TempDir()
	priv := genKey(t)
	writePublicKeyPEM(t, dir, "key1.pem", &priv.PublicKey)
	keys, err := LoadPublicKeys(dir)
	if err != nil {
		t.Fatal(err)
	}

	f := newFakeFetcher()
	signedDigest := "sha256:" + fmt.Sprintf("%064x", 3)
	requestedDigest := "sha256:" + fmt.Sprintf("%064x", 4)
	signedFixture(t, f, priv, "team/app", signedDigest)

	p := NewPolicy(false, keys, nil)
	if p.Signed(f, "team/app", requestedDigest) {
		t.Fatal("expected verification to fail when the signed digest doesn't match")
	}
}

func TestPolicySignedNoKeys(t *testing.T) {
	p := NewPolicy(true, nil, nil)
	if p.Signed(newFakeFetcher(), "team/app", "sha256:abc") {
		t.Fatal("expected Signed to be false with no configured keys")
	}
}

func TestPolicyEnforce(t *testing.T) {
	dir := t.TempDir()
	priv := genKey(t)
	writePublicKeyPEM(t, dir, "key1.pem", &priv.PublicKey)
	keys, err := LoadPublicKeys(dir)
	if err != nil {
		t.Fatal(err)
	}

	digest := "sha256:" + fmt.Sprintf("%064x", 5)

	t.Run("not required", func(t *testing.T) {
		p := NewPolicy(false, keys, nil)
		if err := p.Enforce(newFakeFetcher(), "team/app", digest); err != nil {
			t.Fatalf("Enforce (not required) = %v, want nil", err)
		}
	})

	t.Run("required, unsigned", func(t *testing.T) {
		p := NewPolicy(true, keys, nil)
		if err := p.Enforce(newFakeFetcher(), "team/app", digest); err == nil {
			t.Fatal("expected Enforce to reject an unsigned push")
		}
	})

	t.Run("required, signed", func(t *testing.T) {
		f := newFakeFetcher()
		signedFixture(t, f, priv, "team/app", digest)
		p := NewPolicy(true, keys, nil)
		if err := p.Enforce(f, "team/app", digest); err != nil {
			t.Fatalf("Enforce (signed) = %v, want nil", err)
		}
	})

	t.Run("required, no keys configured", func(t *testing.T) {
		p := NewPolicy(true, nil, nil)
		if err := p.Enforce(newFakeFetcher(), "team/app", digest); err == nil {
			t.Fatal("expected Enforce to fail closed when no keys are configured")
		}
	})
}

func TestPolicyNilIsPermissive(t *testing.T) {
	var p *Policy
	if p.Required("team/app") {
		t.Fatal("nil Policy should never require signatures")
	}
	if err := p.Enforce(newFakeFetcher(), "team/app", "sha256:abc"); err != nil {
		t.Fatalf("nil Policy Enforce = %v, want nil", err)
	}
}

// TestPolicyRequiredPerRepoOverrideMatrix covers the accept/reject matrix
// for every combination of global default and a per-repo override: the
// first matching override always wins over the global default.
func TestPolicyRequiredPerRepoOverrideMatrix(t *testing.T) {
	cases := []struct {
		name            string
		globalDefault   bool
		overridePattern string
		overrideValue   bool
		repo            string
		want            bool
	}{
		{"no override, default off", false, "", false, "team/app", false},
		{"no override, default on", true, "", false, "team/app", true},
		{"override forces on despite default off", false, "team/*", true, "team/app", true},
		{"override forces off despite default on", true, "team/*", false, "team/app", false},
		{"override doesn't match other repos", true, "team/*", false, "other/app", true},
		{"wildcard override applies to everything", false, "*", true, "anything/goes", true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			st := openTestStore(t)
			if c.overridePattern != "" {
				if _, err := st.CreateSigningPolicy(c.overridePattern, c.overrideValue); err != nil {
					t.Fatal(err)
				}
			}
			p := NewPolicy(c.globalDefault, nil, st)
			if got := p.Required(c.repo); got != c.want {
				t.Errorf("Required(%q) = %v, want %v", c.repo, got, c.want)
			}
		})
	}
}

// TestPolicyRequiredFirstMatchWins verifies overrides are evaluated in
// creation order and the first pattern matching the repo wins, even if a
// later, more specific pattern also matches.
func TestPolicyRequiredFirstMatchWins(t *testing.T) {
	st := openTestStore(t)
	if _, err := st.CreateSigningPolicy("team/*", true); err != nil {
		t.Fatal(err)
	}
	if _, err := st.CreateSigningPolicy("team/app", false); err != nil {
		t.Fatal(err)
	}
	p := NewPolicy(false, nil, st)
	if !p.Required("team/app") {
		t.Fatal("expected the first-created override (team/*, required) to win over the later, more specific one")
	}
}

// TestPolicyEnforceMultipleKeysAnyMatch verifies a signature is accepted if
// it verifies against ANY configured key, not just the first one.
func TestPolicyEnforceMultipleKeysAnyMatch(t *testing.T) {
	dir := t.TempDir()
	unrelated := genKey(t)
	signer := genKey(t)
	writePublicKeyPEM(t, dir, "key1.pem", &unrelated.PublicKey)
	writePublicKeyPEM(t, dir, "key2.pem", &signer.PublicKey)
	keys, err := LoadPublicKeys(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(keys) != 2 {
		t.Fatalf("got %d keys, want 2", len(keys))
	}

	f := newFakeFetcher()
	digest := "sha256:" + fmt.Sprintf("%064x", 7)
	signedFixture(t, f, signer, "team/app", digest)

	p := NewPolicy(true, keys, nil)
	if err := p.Enforce(f, "team/app", digest); err != nil {
		t.Fatalf("Enforce = %v, want nil (signature matches the second configured key)", err)
	}
}

func TestIsArtifactTag(t *testing.T) {
	digest := "sha256:" + fmt.Sprintf("%064x", 6)
	cases := map[string]bool{
		SigTag(digest):         true,
		"sha256-" + fmt.Sprintf("%064x", 6) + ".att":  true,
		"sha256-" + fmt.Sprintf("%064x", 6) + ".sbom": true,
		"latest":                                      false,
		"v1.2.3":                                      false,
		digest:                                        false,
	}
	for tag, want := range cases {
		if got := IsArtifactTag(tag); got != want {
			t.Errorf("IsArtifactTag(%q) = %v, want %v", tag, got, want)
		}
	}
}
