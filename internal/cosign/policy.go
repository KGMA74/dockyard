package cosign

import (
	"crypto"
	"encoding/json"
	"fmt"
	"regexp"
	"strings"

	"dockyard/internal/auth"
	"dockyard/internal/store"
)

// sigManifest is the minimal shape of a cosign signature manifest: one
// layer per signature, each carrying the base64 signature and (for keyless
// signing, unused here) the sigstore bundle as annotations.
type sigManifest struct {
	Layers []struct {
		Digest      string            `json:"digest"`
		Annotations map[string]string `json:"annotations"`
	} `json:"layers"`
}

const signatureAnnotation = "dev.cosignproject.cosign/signature"

var reArtifactTag = regexp.MustCompile(`^sha256-[a-f0-9]{64}\.(sig|att|sbom)$`)

// IsArtifactTag reports whether ref is a cosign-convention tag (signature,
// attestation, or SBOM) rather than a "real" human tag — these must never be
// gated by the signed-push policy, or cosign could never attach them.
func IsArtifactTag(ref string) bool { return reArtifactTag.MatchString(ref) }

// SigTag is the tag cosign publishes a signature under for a given digest.
func SigTag(digest string) string {
	return "sha256-" + strings.TrimPrefix(digest, "sha256:") + ".sig"
}

// Policy decides whether a repository requires a valid cosign signature
// before a tag push is accepted, and verifies signatures against statically
// configured public keys. A nil *Policy is valid and always permissive.
type Policy struct {
	defaultRequired bool
	keys            []crypto.PublicKey
	store           *store.Store // per-repo overrides; nil = defaultRequired always applies
}

func NewPolicy(defaultRequired bool, keys []crypto.PublicKey, st *store.Store) *Policy {
	return &Policy{defaultRequired: defaultRequired, keys: keys, store: st}
}

func (p *Policy) HasKeys() bool { return p != nil && len(p.keys) > 0 }

// Required reports whether repo needs a valid signature before a tag push is
// accepted — the first matching per-repo override wins, else the global
// default.
func (p *Policy) Required(repo string) bool {
	if p == nil {
		return false
	}
	if p.store != nil {
		if policies, err := p.store.ListSigningPolicies(); err == nil {
			for _, sp := range policies {
				if auth.MatchesRepo([]string{sp.RepoPattern}, repo) {
					return sp.Required
				}
			}
		}
	}
	return p.defaultRequired
}

// Signed reports whether a valid cosign signature exists for digest in repo
// name, verified against the configured public keys.
func (p *Policy) Signed(f Fetcher, name, digest string) bool {
	if !p.HasKeys() {
		return false
	}
	raw, _, err := f.GetManifest(name, SigTag(digest))
	if err != nil {
		return false
	}
	var m sigManifest
	if json.Unmarshal(raw, &m) != nil {
		return false
	}
	for _, l := range m.Layers {
		sigB64 := l.Annotations[signatureAnnotation]
		if sigB64 == "" {
			continue
		}
		payload, err := f.GetBlob(name, l.Digest)
		if err != nil {
			continue
		}
		var sp simpleSigningPayload
		if json.Unmarshal(payload, &sp) != nil {
			continue
		}
		if sp.Critical.Image.DockerManifestDigest != digest {
			continue
		}
		if verifySignature(payload, sigB64, p.keys) {
			return true
		}
	}
	return false
}

// Enforce returns an error if repo requires a signature and none verifies
// for digest. A nil Policy never blocks anything.
func (p *Policy) Enforce(f Fetcher, name, digest string) error {
	if !p.Required(name) {
		return nil
	}
	if !p.HasKeys() {
		return fmt.Errorf("signed push required for %q but no cosign public keys are configured", name)
	}
	if !p.Signed(f, name, digest) {
		return fmt.Errorf("no valid cosign signature found for %s (expected tag %s)", digest, SigTag(digest))
	}
	return nil
}
