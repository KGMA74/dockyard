package admin

import "encoding/json"

type layerDetail struct {
	Digest    string `json:"digest"`
	SizeBytes int64  `json:"size_bytes"`
	SizeHuman string `json:"size_human"`
}

type ociImageConfig struct {
	Created      string `json:"created"`
	Architecture string `json:"architecture"`
	OS           string `json:"os"`
}

// parseManifestDetails parses a V2 manifest and its referenced config blob into a
// flat, UI-friendly shape. getBlob fetches a blob's raw bytes by digest (used to
// read the image config for created/architecture/os).
func parseManifestDetails(raw []byte, digest string, getBlob func(digest string) ([]byte, error)) (map[string]any, error) {
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
