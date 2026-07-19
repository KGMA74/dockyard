package cosign

import (
	"crypto"
	"crypto/ecdsa"
	"crypto/rsa"
	"crypto/sha256"
	"encoding/base64"
)

// simpleSigningPayload is the "simple signing" envelope cosign signs: the
// bytes of this JSON document (not the container image itself) are what the
// signature covers. See sigstore/cosign's SimpleContainerImage type.
type simpleSigningPayload struct {
	Critical struct {
		Image struct {
			DockerManifestDigest string `json:"docker-manifest-digest"`
		} `json:"image"`
	} `json:"critical"`
}

// verifySignature reports whether sigB64 (base64, as stored in the
// dev.cosignproject.cosign/signature annotation) is a valid signature over
// payload by any of keys. cosign signs the raw payload bytes with ECDSA
// (P-256/SHA-256 by default) or RSA — both are supported here since either
// can be produced by `cosign generate-key-pair`.
func verifySignature(payload []byte, sigB64 string, keys []crypto.PublicKey) bool {
	sig, err := base64.StdEncoding.DecodeString(sigB64)
	if err != nil {
		return false
	}
	hash := sha256.Sum256(payload)
	for _, key := range keys {
		switch k := key.(type) {
		case *ecdsa.PublicKey:
			if ecdsa.VerifyASN1(k, hash[:], sig) {
				return true
			}
		case *rsa.PublicKey:
			if rsa.VerifyPKCS1v15(k, crypto.SHA256, hash[:], sig) == nil {
				return true
			}
			if rsa.VerifyPSS(k, crypto.SHA256, hash[:], sig, nil) == nil {
				return true
			}
		}
	}
	return false
}
