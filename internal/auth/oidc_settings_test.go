package auth

import (
	"path/filepath"
	"strings"
	"testing"

	"dockyard/internal/store"
)

func TestSealRoundTrip(t *testing.T) {
	m := newManager(t)
	sealed, err := m.seal(sealSSOClientSecret, "s3cret")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(sealed, sealPrefix) || strings.Contains(sealed, "s3cret") {
		t.Fatalf("sealed value %q", sealed)
	}
	again, _ := m.seal(sealSSOClientSecret, "s3cret")
	if again == sealed {
		t.Fatal("two seals of the same value are identical: the nonce is not random")
	}
	plain, stale, err := m.unseal(sealSSOClientSecret, sealed)
	if err != nil || plain != "s3cret" || stale {
		t.Fatalf("unseal = %q, %v, %v", plain, stale, err)
	}
	if _, _, err := m.unseal("another-purpose", sealed); err == nil {
		t.Fatal("a value sealed for one purpose opened for another")
	}
	tampered := sealed[:len(sealed)-2] + "AA"
	if _, _, err := m.unseal(sealSSOClientSecret, tampered); err == nil {
		t.Fatal("tampered value accepted")
	}
}

// A console SSO configuration survives a restart and a JWT_SECRET rotation,
// and is never applied with a key that cannot open its secret.
func TestConsoleSSOSurvivesRestartAndRotation(t *testing.T) {
	dir := t.TempDir()
	st, err := store.Open(filepath.Join(dir, "dockyard.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	boot := func(secret, previous string) *Manager {
		m, err := New(testUser, testPassword, secret, previous, dir, st)
		if err != nil {
			t.Fatal(err)
		}
		return m
	}

	m := boot("old-secret", "")
	sealed, err := m.seal(sealSSOClientSecret, "client-s3cret")
	if err != nil {
		t.Fatal(err)
	}
	if err := st.SaveOIDCSettings(&store.OIDCSettings{
		Enabled: true, IssuerURL: "https://idp.example", ClientID: "dockyard", ClientSecretEnc: sealed,
		DisplayName: "Company", DefaultRole: "reader", Scopes: []string{"openid"},
	}); err != nil {
		t.Fatal(err)
	}

	clientSecret := func(m *Manager) string {
		s := m.oidc.Load()
		if s == nil {
			return "<sso off>"
		}
		return s.cfg.ClientSecret
	}
	if got := clientSecret(boot("old-secret", "")); got != "client-s3cret" {
		t.Fatalf("after restart: %q", got)
	}
	// Rotation: the previous secret still opens it, and it is re-sealed.
	if got := clientSecret(boot("new-secret", "old-secret")); got != "client-s3cret" {
		t.Fatalf("during rotation: %q", got)
	}
	if got := clientSecret(boot("new-secret", "")); got != "client-s3cret" {
		t.Fatalf("after rotation (re-sealed?): %q", got)
	}
	// A key that never sealed it: SSO stays off instead of failing sign-ins.
	if got := clientSecret(boot("unrelated", "")); got != "<sso off>" {
		t.Fatalf("with an unrelated key: %q", got)
	}
}
