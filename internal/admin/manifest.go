package admin

import "encoding/json"

const (
	mediaTypeManifestList = "application/vnd.docker.distribution.manifest.list.v2+json"
	mediaTypeOCIIndex     = "application/vnd.oci.image.index.v1+json"
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
	var probe struct {
		MediaType string              `json:"mediaType"`
		Manifests []manifestListEntry `json:"manifests"`
	}
	if err := json.Unmarshal(raw, &probe); err != nil {
		return nil, err
	}

	if probe.MediaType == mediaTypeManifestList || probe.MediaType == mediaTypeOCIIndex || len(probe.Manifests) > 0 {
		return parseManifestList(probe.MediaType, digest, probe.Manifests, getBlob, getManifest)
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
		child, err := parseManifestDetails(childRaw, entry.Digest, getBlob, getManifest)
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
