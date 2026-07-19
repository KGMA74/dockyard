// Package export dumps a repository to an OCI image-layout tarball and
// restores one — the format skopeo/crane understand (oci-layout + index.json
// + blobs/sha256/<hex>), so exports interoperate beyond Dockyard.
package export

import (
	"archive/tar"
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"time"

	"dockyard/internal/storage"
)

const refNameAnnotation = "org.opencontainers.image.ref.name"

type descriptor struct {
	MediaType   string            `json:"mediaType,omitempty"`
	Digest      string            `json:"digest"`
	Size        int64             `json:"size"`
	Annotations map[string]string `json:"annotations,omitempty"`
}

type ociIndex struct {
	SchemaVersion int          `json:"schemaVersion"`
	Manifests     []descriptor `json:"manifests"`
}

// manifestRefs extracts every digest a manifest references: config, layers,
// and child manifests for multi-arch indexes.
func manifestRefs(raw []byte) (blobs []string, children []string) {
	var m struct {
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
	if err := json.Unmarshal(raw, &m); err != nil {
		return nil, nil
	}
	if m.Config.Digest != "" {
		blobs = append(blobs, m.Config.Digest)
	}
	for _, l := range m.Layers {
		if l.Digest != "" {
			blobs = append(blobs, l.Digest)
		}
	}
	for _, c := range m.Manifests {
		if c.Digest != "" {
			children = append(children, c.Digest)
		}
	}
	return blobs, children
}

func blobPath(digest string) string {
	return "blobs/sha256/" + strings.TrimPrefix(digest, "sha256:")
}

// Export streams the repository as an OCI image-layout tar. Each tag becomes
// an index.json entry annotated with its name; blobs are deduplicated.
func Export(w io.Writer, backend storage.Backend, name string) error {
	tags, err := backend.ListTags(name)
	if err != nil {
		return err
	}
	if len(tags) == 0 {
		return fmt.Errorf("repository %q has no tags", name)
	}

	tw := tar.NewWriter(w)
	defer func() { _ = tw.Close() }()

	writeFile := func(path string, content []byte) error {
		if err := tw.WriteHeader(&tar.Header{
			Name: path, Mode: 0644, Size: int64(len(content)), ModTime: time.Now(),
		}); err != nil {
			return err
		}
		_, err := tw.Write(content)
		return err
	}

	written := map[string]bool{}
	index := ociIndex{SchemaVersion: 2}

	// writeManifestTree stores the manifest bytes as a blob and recursively
	// its children (multi-arch) and referenced blobs.
	var writeManifestTree func(digest string, raw []byte) error
	writeManifestTree = func(digest string, raw []byte) error {
		if !written[digest] {
			if err := writeFile(blobPath(digest), raw); err != nil {
				return err
			}
			written[digest] = true
		}
		blobs, children := manifestRefs(raw)
		for _, bd := range blobs {
			if written[bd] {
				continue
			}
			rc, size, err := backend.GetBlob(bd)
			if err != nil {
				return fmt.Errorf("blob %s: %w", bd, err)
			}
			if err := tw.WriteHeader(&tar.Header{
				Name: blobPath(bd), Mode: 0644, Size: size, ModTime: time.Now(),
			}); err != nil {
				_ = rc.Close()
				return err
			}
			_, err = io.Copy(tw, rc)
			_ = rc.Close()
			if err != nil {
				return err
			}
			written[bd] = true
		}
		for _, child := range children {
			if written[child] {
				continue
			}
			childRaw, _, err := backend.GetManifest(name, child)
			if err != nil {
				return fmt.Errorf("child manifest %s: %w", child, err)
			}
			if err := writeManifestTree(child, childRaw); err != nil {
				return err
			}
		}
		return nil
	}

	for _, tag := range tags {
		raw, digest, err := backend.GetManifest(name, tag)
		if err != nil {
			continue
		}
		if err := writeManifestTree(digest, raw); err != nil {
			return err
		}
		index.Manifests = append(index.Manifests, descriptor{
			MediaType:   manifestMediaType(raw),
			Digest:      digest,
			Size:        int64(len(raw)),
			Annotations: map[string]string{refNameAnnotation: tag},
		})
	}

	layout, _ := json.Marshal(map[string]string{"imageLayoutVersion": "1.0.0"})
	if err := writeFile("oci-layout", layout); err != nil {
		return err
	}
	indexRaw, err := json.Marshal(index)
	if err != nil {
		return err
	}
	return writeFile("index.json", indexRaw)
}

// Import restores an OCI image-layout tar into the repository. Blobs stream
// straight into storage (hash-verified by PutBlob); manifests are small and
// buffered so index.json can be resolved regardless of entry order.
func Import(r io.Reader, backend storage.Backend, name string) (tagsImported int, err error) {
	tr := tar.NewReader(r)

	const smallBlobLimit = 4 << 20 // manifests/configs; layers stream to storage
	small := map[string][]byte{}
	var indexRaw []byte

	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return 0, err
		}
		cleanName := strings.TrimPrefix(hdr.Name, "./")
		switch {
		case cleanName == "index.json":
			indexRaw, err = io.ReadAll(io.LimitReader(tr, smallBlobLimit))
			if err != nil {
				return 0, err
			}
		case cleanName == "oci-layout":
			// informational only
		case strings.HasPrefix(cleanName, "blobs/sha256/"):
			digest := "sha256:" + strings.TrimPrefix(cleanName, "blobs/sha256/")
			if hdr.Size <= smallBlobLimit {
				raw, err := io.ReadAll(io.LimitReader(tr, smallBlobLimit+1))
				if err != nil {
					return 0, err
				}
				small[digest] = raw
				if err := backend.PutBlob(digest, strings.NewReader(string(raw)), int64(len(raw))); err != nil {
					return 0, fmt.Errorf("blob %s: %w", digest, err)
				}
			} else {
				if err := backend.PutBlob(digest, tr, hdr.Size); err != nil {
					return 0, fmt.Errorf("blob %s: %w", digest, err)
				}
			}
		}
	}

	if indexRaw == nil {
		return 0, fmt.Errorf("archive has no index.json — not an OCI image layout")
	}
	var index ociIndex
	if err := json.Unmarshal(indexRaw, &index); err != nil {
		return 0, fmt.Errorf("bad index.json: %w", err)
	}

	// storeManifestTree registers the manifest (and its multi-arch children)
	// from the buffered small blobs.
	var storeManifestTree func(digest, ref string) error
	storeManifestTree = func(digest, ref string) error {
		raw, ok := small[digest]
		if !ok {
			return fmt.Errorf("manifest %s missing from archive", digest)
		}
		if err := backend.PutManifest(name, ref, digest, raw); err != nil {
			return err
		}
		_, children := manifestRefs(raw)
		for _, child := range children {
			if err := storeManifestTree(child, child); err != nil {
				return err
			}
		}
		return nil
	}

	for _, desc := range index.Manifests {
		ref := desc.Annotations[refNameAnnotation]
		if ref == "" {
			ref = desc.Digest
		}
		if err := storeManifestTree(desc.Digest, ref); err != nil {
			return tagsImported, err
		}
		tagsImported++
	}
	return tagsImported, nil
}

func manifestMediaType(raw []byte) string {
	var m struct {
		MediaType string `json:"mediaType"`
	}
	if json.Unmarshal(raw, &m) == nil && m.MediaType != "" {
		return m.MediaType
	}
	return "application/vnd.docker.distribution.manifest.v2+json"
}
