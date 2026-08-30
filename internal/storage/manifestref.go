package storage

import "encoding/json"

// maxManifestListDepth bounds nested manifest-list resolution (an index of
// indices). Real images never nest this deep; the cap turns a crafted or cyclic
// reference chain into a bounded walk instead of unbounded recursion. Mirrors
// internal/admin/manifest.go.
const maxManifestListDepth = 8

// manifestDoc is the minimal shape needed to walk blob references. A single
// image manifest carries config+layers; a manifest list / OCI image index
// carries manifests[] pointing at child manifests (by digest).
type manifestDoc struct {
	Config struct {
		Digest string `json:"digest"`
	} `json:"config"`
	Layers []struct {
		Digest string `json:"digest"`
	} `json:"layers"`
	Manifests []struct {
		Digest string `json:"digest"`
	} `json:"manifests"`
}

// collectBlobRefs adds every config and layer digest referenced by raw (and, for
// a manifest list / index, by its child manifests) into the set. fetchChild
// resolves a child manifest by digest within the same repository; it may return
// (nil, err) for a missing child, which is skipped.
func collectBlobRefs(raw []byte, into map[string]struct{}, fetchChild func(digest string) ([]byte, error)) {
	walkManifest(raw, into, fetchChild, 0)
}

func walkManifest(raw []byte, into map[string]struct{}, fetchChild func(digest string) ([]byte, error), depth int) {
	var m manifestDoc
	if json.Unmarshal(raw, &m) != nil {
		return
	}
	if m.Config.Digest != "" {
		into[m.Config.Digest] = struct{}{}
	}
	for _, l := range m.Layers {
		if l.Digest != "" {
			into[l.Digest] = struct{}{}
		}
	}
	if depth >= maxManifestListDepth {
		return
	}
	for _, child := range m.Manifests {
		if child.Digest == "" {
			continue
		}
		childRaw, err := fetchChild(child.Digest)
		if err != nil || childRaw == nil {
			continue
		}
		walkManifest(childRaw, into, fetchChild, depth+1)
	}
}

// childManifestDigests returns the child-manifest digests referenced by an
// index/manifest-list document. Empty for a plain image manifest.
func childManifestDigests(raw []byte) []string {
	var m manifestDoc
	if json.Unmarshal(raw, &m) != nil {
		return nil
	}
	var out []string
	for _, c := range m.Manifests {
		if c.Digest != "" {
			out = append(out, c.Digest)
		}
	}
	return out
}
