package admin

import (
	"encoding/json"
	"errors"
	"sort"
)

const (
	mediaTypeManifestList = "application/vnd.docker.distribution.manifest.list.v2+json"
	mediaTypeOCIIndex     = "application/vnd.oci.image.index.v1+json"

	// maxManifestListDepth bounds nested manifest-list resolution (index of
	// indices). Real images never nest this deep — this exists to turn a
	// crafted or cyclic reference chain (list A → list B → list A) into an
	// error instead of unbounded recursion.
	maxManifestListDepth = 8
)

type layerDetail struct {
	Digest    string `json:"digest"`
	SizeBytes int64  `json:"size_bytes"`
	SizeHuman string `json:"size_human"`
}

type platformDetail struct {
	Architecture string `json:"architecture"`
	OS           string `json:"os"`
	Digest       string `json:"digest"`
	SizeBytes    int64  `json:"size_bytes"`
	SizeHuman    string `json:"size_human"`
}

type manifestListEntry struct {
	MediaType string `json:"mediaType"`
	Digest    string `json:"digest"`
	Platform  struct {
		Architecture string `json:"architecture"`
		OS           string `json:"os"`
	} `json:"platform"`
}

type ociImageConfig struct {
	Created      string `json:"created"`
	Architecture string `json:"architecture"`
	OS           string `json:"os"`
}

// parseManifestDetails parses a V2 manifest into a flat, UI-friendly shape. A tag
// can point at either a single-platform image manifest or a manifest list / OCI
// index (multi-arch) — the latter carries no layers or config of its own, only
// references to per-platform manifests, so it's resolved recursively via getManifest.
//
// getBlob fetches a blob's raw bytes by digest (used to read the image config for
// created/architecture/os). getManifest fetches another manifest's raw bytes by
// digest, within the same repository — required to resolve manifest lists.
func parseManifestDetails(
	raw []byte,
	digest string,
	getBlob func(digest string) ([]byte, error),
	getManifest func(digest string) ([]byte, error),
) (map[string]any, error) {
	return parseManifestDetailsRec(raw, digest, getBlob, getManifest, map[string]bool{}, 0)
}

func parseManifestDetailsRec(
	raw []byte,
	digest string,
	getBlob func(digest string) ([]byte, error),
	getManifest func(digest string) ([]byte, error),
	seen map[string]bool,
	depth int,
) (map[string]any, error) {
	var probe struct {
		MediaType string              `json:"mediaType"`
		Manifests []manifestListEntry `json:"manifests"`
	}
	if err := json.Unmarshal(raw, &probe); err != nil {
		return nil, err
	}

	if probe.MediaType == mediaTypeManifestList || probe.MediaType == mediaTypeOCIIndex || len(probe.Manifests) > 0 {
		if depth >= maxManifestListDepth {
			return nil, errors.New("manifest list nesting too deep")
		}
		if seen[digest] {
			return nil, errors.New("manifest list reference cycle detected")
		}
		seen[digest] = true
		return parseManifestList(probe.MediaType, digest, probe.Manifests, getBlob, getManifest, seen, depth+1)
	}
	return parseSingleManifest(raw, digest, getBlob)
}

func parseSingleManifest(raw []byte, digest string, getBlob func(string) ([]byte, error)) (map[string]any, error) {
	var manifest struct {
		MediaType string `json:"mediaType"`
		Config    struct {
			MediaType string `json:"mediaType"`
			Size      int64  `json:"size"`
			Digest    string `json:"digest"`
		} `json:"config"`
		Layers []struct {
			MediaType string `json:"mediaType"`
			Size      int64  `json:"size"`
			Digest    string `json:"digest"`
		} `json:"layers"`
	}
	if err := json.Unmarshal(raw, &manifest); err != nil {
		return nil, err
	}

	layers := make([]layerDetail, 0, len(manifest.Layers))
	total := manifest.Config.Size
	for _, l := range manifest.Layers {
		layers = append(layers, layerDetail{Digest: l.Digest, SizeBytes: l.Size, SizeHuman: humanSize(l.Size)})
		total += l.Size
	}

	result := map[string]any{
		"digest":           digest,
		"media_type":       manifest.MediaType,
		"total_size_bytes": total,
		"total_size_human": humanSize(total),
		"layers":           layers,
		"config_digest":    manifest.Config.Digest,
	}

	if manifest.Config.Digest != "" {
		if cfgBytes, err := getBlob(manifest.Config.Digest); err == nil {
			var cfg ociImageConfig
			if json.Unmarshal(cfgBytes, &cfg) == nil {
				result["created"] = cfg.Created
				result["architecture"] = cfg.Architecture
				result["os"] = cfg.OS
			}
		}
	}

	return result, nil
}

// parseManifestList resolves each platform's image manifest so the UI can show the
// real total size — the list itself only references child manifests, it never
// carries layers or a size of its own. Layers are deduplicated by digest across
// platforms for the merged "Layers" list; total size sums each platform's own
// total as-is (shared base layers across architectures are rare enough that a
// small overcount there beats reporting 0).
func parseManifestList(
	mediaType, digest string,
	entries []manifestListEntry,
	getBlob func(string) ([]byte, error),
	getManifest func(string) ([]byte, error),
	seen map[string]bool,
	depth int,
) (map[string]any, error) {
	seenLayers := make(map[string]bool)
	mergedLayers := make([]layerDetail, 0)
	platforms := make([]platformDetail, 0, len(entries))
	var total int64

	for _, entry := range entries {
		childRaw, err := getManifest(entry.Digest)
		if err != nil {
			continue
		}
		// Each sibling gets its own copy of the ancestor-path set: `seen`
		// tracks digests on the current root-to-node path (to catch cycles),
		// not every digest visited anywhere in the tree — two independent
		// platform entries are allowed to reference the same digest.
		branchSeen := make(map[string]bool, len(seen)+1)
		for d := range seen {
			branchSeen[d] = true
		}
		child, err := parseManifestDetailsRec(childRaw, entry.Digest, getBlob, getManifest, branchSeen, depth)
		if err != nil {
			continue
		}

		childTotal, _ := child["total_size_bytes"].(int64)
		total += childTotal
		platforms = append(platforms, platformDetail{
			Architecture: entry.Platform.Architecture,
			OS:           entry.Platform.OS,
			Digest:       entry.Digest,
			SizeBytes:    childTotal,
			SizeHuman:    humanSize(childTotal),
		})

		if childLayers, ok := child["layers"].([]layerDetail); ok {
			for _, l := range childLayers {
				if seenLayers[l.Digest] {
					continue
				}
				seenLayers[l.Digest] = true
				mergedLayers = append(mergedLayers, l)
			}
		}
	}

	return map[string]any{
		"digest":           digest,
		"media_type":       mediaType,
		"total_size_bytes": total,
		"total_size_human": humanSize(total),
		"layers":           mergedLayers,
		"config_digest":    "",
		"platforms":        platforms,
	}, nil
}

// diffManifests compares two parsed manifest details (as returned by
// parseManifestDetails) and reports which layer digests each side has
// exclusively, which they share, and the total size delta (b - a).
// Comparing layer sets rather than the manifest JSON directly means a
// rebuild that reuses the same base layers reports "unchanged", not "every
// byte different".
func diffManifests(a, b map[string]any) map[string]any {
	layerDigests := func(m map[string]any) map[string]bool {
		set := map[string]bool{}
		layers, _ := m["layers"].([]layerDetail)
		for _, l := range layers {
			set[l.Digest] = true
		}
		return set
	}
	setA, setB := layerDigests(a), layerDigests(b)

	var onlyA, onlyB, common []string
	for d := range setA {
		if setB[d] {
			common = append(common, d)
		} else {
			onlyA = append(onlyA, d)
		}
	}
	for d := range setB {
		if !setA[d] {
			onlyB = append(onlyB, d)
		}
	}
	sort.Strings(onlyA)
	sort.Strings(onlyB)
	sort.Strings(common)

	totalA, _ := a["total_size_bytes"].(int64)
	totalB, _ := b["total_size_bytes"].(int64)

	return map[string]any{
		"a":                a,
		"b":                b,
		"layers_only_a":    emptyIfNil(onlyA),
		"layers_only_b":    emptyIfNil(onlyB),
		"layers_common":    emptyIfNil(common),
		"size_delta_bytes": totalB - totalA,
	}
}

func emptyIfNil(s []string) []string {
	if s == nil {
		return []string{}
	}
	return s
}
