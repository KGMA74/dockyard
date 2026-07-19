// Package cosign verifies cosign "simple signing" signatures against
// statically configured public keys. Verification only — Dockyard never
// signs anything itself, and does not implement keyless (Fulcio/Rekor)
// verification; that is a documented extension point, not implemented here.
package cosign

import (
	"crypto"
	"crypto/x509"
	"encoding/pem"
	"fmt"
	"os"
	"path/filepath"
)

// LoadPublicKeys reads every *.pem file in dir and parses it as a PKIX
// public key (ECDSA or RSA — cosign's own default key types). An empty or
// missing dir yields no keys, not an error: signing enforcement is simply
// off until an operator configures COSIGN_PUBLIC_KEYS_DIR.
func LoadPublicKeys(dir string) ([]crypto.PublicKey, error) {
	if dir == "" {
		return nil, nil
	}
	matches, err := filepath.Glob(filepath.Join(dir, "*.pem"))
	if err != nil {
		return nil, fmt.Errorf("cosign: glob %s: %w", dir, err)
	}
	keys := make([]crypto.PublicKey, 0, len(matches))
	for _, path := range matches {
		raw, err := os.ReadFile(path)
		if err != nil {
			return nil, fmt.Errorf("cosign: read %s: %w", path, err)
		}
		block, _ := pem.Decode(raw)
		if block == nil {
			return nil, fmt.Errorf("cosign: %s: not a PEM file", path)
		}
		key, err := x509.ParsePKIXPublicKey(block.Bytes)
		if err != nil {
			return nil, fmt.Errorf("cosign: %s: %w", path, err)
		}
		keys = append(keys, key)
	}
	return keys, nil
}
