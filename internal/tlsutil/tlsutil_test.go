package tlsutil

import (
	"crypto/x509"
	"testing"
)

func TestOffMode(t *testing.T) {
	for _, mode := range []string{"", ModeOff} {
		cfg, err := Config(Options{Mode: mode})
		if err != nil || cfg != nil {
			t.Errorf("mode %q: (%v, %v), want (nil, nil)", mode, cfg, err)
		}
	}
}

func TestUnknownModeRejected(t *testing.T) {
	if _, err := Config(Options{Mode: "wat"}); err == nil {
		t.Fatal("unknown mode accepted")
	}
}

func TestStaticModeRequiresFiles(t *testing.T) {
	if _, err := Config(Options{Mode: ModeStatic}); err == nil {
		t.Fatal("static mode without files accepted")
	}
}

func TestSelfSignedGenerateAndReuse(t *testing.T) {
	dir := t.TempDir()
	cfg, err := Config(Options{Mode: ModeSelfSigned, Dir: dir, Domain: "registry.example.com"})
	if err != nil {
		t.Fatalf("self-signed: %v", err)
	}
	if len(cfg.Certificates) != 1 {
		t.Fatal("no certificate generated")
	}
	leaf, err := x509.ParseCertificate(cfg.Certificates[0].Certificate[0])
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, name := range leaf.DNSNames {
		if name == "registry.example.com" {
			found = true
		}
	}
	if !found {
		t.Errorf("domain missing from SANs: %v", leaf.DNSNames)
	}

	// Second call must reuse the persisted cert, not regenerate.
	cfg2, err := Config(Options{Mode: ModeSelfSigned, Dir: dir, Domain: "registry.example.com"})
	if err != nil {
		t.Fatal(err)
	}
	leaf2, _ := x509.ParseCertificate(cfg2.Certificates[0].Certificate[0])
	if leaf.SerialNumber.Cmp(leaf2.SerialNumber) != 0 {
		t.Error("certificate was regenerated instead of reused")
	}

	// A different domain not covered by the SANs must trigger regeneration.
	cfg3, err := Config(Options{Mode: ModeSelfSigned, Dir: dir, Domain: "other.example.com"})
	if err != nil {
		t.Fatal(err)
	}
	leaf3, _ := x509.ParseCertificate(cfg3.Certificates[0].Certificate[0])
	if leaf.SerialNumber.Cmp(leaf3.SerialNumber) == 0 {
		t.Error("cert not regenerated for uncovered domain")
	}
}

func TestStaticModeLoadsGeneratedPair(t *testing.T) {
	dir := t.TempDir()
	if _, err := Config(Options{Mode: ModeSelfSigned, Dir: dir}); err != nil {
		t.Fatal(err)
	}
	cfg, err := Config(Options{Mode: ModeStatic, CertFile: dir + "/cert.pem", KeyFile: dir + "/key.pem"})
	if err != nil || len(cfg.Certificates) != 1 {
		t.Fatalf("static load: %v", err)
	}
}

func TestACMERequiresDomain(t *testing.T) {
	if _, err := Config(Options{Mode: ModeACME, Dir: t.TempDir()}); err == nil {
		t.Fatal("acme without domain accepted")
	}
	cfg, err := Config(Options{Mode: ModeACME, Dir: t.TempDir(), Domain: "registry.example.com"})
	if err != nil || cfg == nil || cfg.GetCertificate == nil {
		t.Fatalf("acme config: (%v, %v)", cfg, err)
	}
}
