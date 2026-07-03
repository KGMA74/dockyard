package storage

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// LocalBackend stores blobs, manifests, and uploads on the local filesystem.
// Layout:
//
//	<root>/blobs/sha256/<2-char-prefix>/<full-digest>/data
//	<root>/repositories/<name>/manifests/<digest>
//	<root>/repositories/<name>/tags/<tag>  (file content = digest)
//	<root>/uploads/<uuid>/data
type LocalBackend struct {
	root string
}

func NewLocal(root string) (*LocalBackend, error) {
	for _, d := range []string{
		filepath.Join(root, "blobs", "sha256"),
		filepath.Join(root, "repositories"),
		filepath.Join(root, "uploads"),
	} {
		if err := os.MkdirAll(d, 0755); err != nil {
			return nil, err
		}
	}
	return &LocalBackend{root: root}, nil
}

func (b *LocalBackend) Root() string { return b.root }

// ── path helpers ─────────────────────────────────────────────────────────────

func (b *LocalBackend) blobPath(digest string) string {
	d := strings.TrimPrefix(digest, "sha256:")
	return filepath.Join(b.root, "blobs", "sha256", d[:2], d, "data")
}

func (b *LocalBackend) uploadDataPath(id string) string {
	return filepath.Join(b.root, "uploads", id, "data")
}

func (b *LocalBackend) manifestPath(name, digest string) string {
	d := strings.TrimPrefix(digest, "sha256:")
	return filepath.Join(b.root, "repositories", filepath.FromSlash(name), "manifests", d)
}

func (b *LocalBackend) tagPath(name, tag string) string {
	return filepath.Join(b.root, "repositories", filepath.FromSlash(name), "tags", tag)
}

// ── Backend interface — Blobs ─────────────────────────────────────────────────

func (b *LocalBackend) PutBlob(digest string, content io.Reader, _ int64) error {
	path := b.blobPath(digest)
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return err
	}
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()
	h := sha256.New()
	if _, err = io.Copy(io.MultiWriter(f, h), content); err != nil {
		os.Remove(path)
		return err
	}
	if got := "sha256:" + hex.EncodeToString(h.Sum(nil)); got != digest {
		os.Remove(path)
		return fmt.Errorf("digest mismatch: expected %s got %s", digest, got)
	}
	return nil
}

func (b *LocalBackend) GetBlob(digest string) (io.ReadCloser, int64, error) {
	f, err := os.Open(b.blobPath(digest))
	if err != nil {
		return nil, 0, err
	}
	info, err := f.Stat()
	if err != nil {
		f.Close()
		return nil, 0, err
	}
	return f, info.Size(), nil
}

func (b *LocalBackend) BlobExists(digest string) (bool, error) {
	_, err := os.Stat(b.blobPath(digest))
	if os.IsNotExist(err) {
		return false, nil
	}
	return err == nil, err
}

func (b *LocalBackend) DeleteBlob(digest string) error {
	dir := filepath.Dir(b.blobPath(digest))
	if err := os.RemoveAll(dir); err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}

// ── Backend interface — Uploads ───────────────────────────────────────────────

func (b *LocalBackend) InitUpload(uuid string) error {
	dir := filepath.Join(b.root, "uploads", uuid)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return err
	}
	f, err := os.Create(filepath.Join(dir, "data"))
	if err != nil {
		return err
	}
	return f.Close()
}

func (b *LocalBackend) AppendUpload(uuid string, content io.Reader) error {
	f, err := os.OpenFile(b.uploadDataPath(uuid), os.O_APPEND|os.O_WRONLY, 0644)
	if err != nil {
		return fmt.Errorf("upload %s not found", uuid)
	}
	defer f.Close()
	_, err = io.Copy(f, content)
	return err
}

func (b *LocalBackend) CommitUpload(uuid, digest string) error {
	src := b.uploadDataPath(uuid)
	f, err := os.Open(src)
	if err != nil {
		return fmt.Errorf("upload %s not found", uuid)
	}
	h := sha256.New()
	if _, err = io.Copy(h, f); err != nil {
		f.Close()
		return err
	}
	f.Close()
	if got := "sha256:" + hex.EncodeToString(h.Sum(nil)); got != digest {
		os.RemoveAll(filepath.Join(b.root, "uploads", uuid))
		return fmt.Errorf("digest mismatch: expected %s got %s", digest, got)
	}
	dst := b.blobPath(digest)
	if err := os.MkdirAll(filepath.Dir(dst), 0755); err != nil {
		return err
	}
	if err := os.Rename(src, dst); err != nil {
		return err
	}
	return os.RemoveAll(filepath.Join(b.root, "uploads", uuid))
}

func (b *LocalBackend) DeleteUpload(uuid string) error {
	return os.RemoveAll(filepath.Join(b.root, "uploads", uuid))
}

func (b *LocalBackend) GetUploadSize(uuid string) (int64, error) {
	info, err := os.Stat(b.uploadDataPath(uuid))
	if err != nil {
		return 0, err
	}
	return info.Size(), nil
}

// ── Backend interface — Manifests ─────────────────────────────────────────────

func (b *LocalBackend) PutManifest(name, reference, digest string, content []byte) error {
	mp := b.manifestPath(name, digest)
	if err := os.MkdirAll(filepath.Dir(mp), 0755); err != nil {
		return err
	}
	if err := os.WriteFile(mp, content, 0644); err != nil {
		return err
	}
	if !strings.HasPrefix(reference, "sha256:") {
		tp := b.tagPath(name, reference)
		if err := os.MkdirAll(filepath.Dir(tp), 0755); err != nil {
			return err
		}
		return os.WriteFile(tp, []byte(digest), 0644)
	}
	return nil
}

func (b *LocalBackend) GetManifest(name, reference string) ([]byte, string, error) {
	var digest string
	if strings.HasPrefix(reference, "sha256:") {
		digest = reference
	} else {
		raw, err := os.ReadFile(b.tagPath(name, reference))
		if err != nil {
			return nil, "", fmt.Errorf("tag %q not found in %s", reference, name)
		}
		digest = string(raw)
	}
	content, err := os.ReadFile(b.manifestPath(name, digest))
	if err != nil {
		return nil, "", fmt.Errorf("manifest %s not found", digest)
	}
	return content, digest, nil
}

func (b *LocalBackend) DeleteManifest(name, digest string) error {
	if err := os.Remove(b.manifestPath(name, digest)); err != nil && !os.IsNotExist(err) {
		return err
	}
	tagsDir := filepath.Join(b.root, "repositories", filepath.FromSlash(name), "tags")
	entries, _ := os.ReadDir(tagsDir)
	for _, e := range entries {
		raw, err := os.ReadFile(filepath.Join(tagsDir, e.Name()))
		if err == nil && string(raw) == digest {
			os.Remove(filepath.Join(tagsDir, e.Name()))
		}
	}

	// If no manifests remain, the repository is empty — drop it entirely so it
	// no longer shows up in ListRepositories (which detects repos by walking
	// for a "tags" directory, and an empty one would otherwise still match).
	manifestsDir := filepath.Join(b.root, "repositories", filepath.FromSlash(name), "manifests")
	if remaining, err := os.ReadDir(manifestsDir); err == nil && len(remaining) == 0 {
		os.RemoveAll(filepath.Join(b.root, "repositories", filepath.FromSlash(name)))
	}
	return nil
}

func (b *LocalBackend) ManifestExists(name, reference string) (bool, error) {
	var path string
	if strings.HasPrefix(reference, "sha256:") {
		path = b.manifestPath(name, reference)
	} else {
		path = b.tagPath(name, reference)
	}
	_, err := os.Stat(path)
	if os.IsNotExist(err) {
		return false, nil
	}
	return err == nil, err
}

// ── Backend interface — Catalog ───────────────────────────────────────────────

func (b *LocalBackend) ListRepositories() ([]string, error) {
	base := filepath.Join(b.root, "repositories")
	var repos []string
	err := filepath.WalkDir(base, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			if os.IsNotExist(err) {
				return nil
			}
			return err
		}
		if d.IsDir() && d.Name() == "tags" {
			rel, _ := filepath.Rel(base, filepath.Dir(path))
			repos = append(repos, filepath.ToSlash(rel))
		}
		return nil
	})
	return repos, err
}

func (b *LocalBackend) ListTags(name string) ([]string, error) {
	dir := filepath.Join(b.root, "repositories", filepath.FromSlash(name), "tags")
	entries, err := os.ReadDir(dir)
	if os.IsNotExist(err) {
		return []string{}, nil
	}
	if err != nil {
		return nil, err
	}
	tags := make([]string, 0, len(entries))
	for _, e := range entries {
		if !e.IsDir() {
			tags = append(tags, e.Name())
		}
	}
	return tags, nil
}

// DeleteRepository removes a repository and all its manifests and tags.
// Blobs stay on disk until the next GC run, like manifest deletion.
func (b *LocalBackend) DeleteRepository(name string) error {
	dir := filepath.Join(b.root, "repositories", filepath.FromSlash(name))
	if _, err := os.Stat(dir); os.IsNotExist(err) {
		return fmt.Errorf("repository %q not found", name)
	}
	return os.RemoveAll(dir)
}

func (b *LocalBackend) TagPushedAt(name, tag string) (time.Time, error) {
	info, err := os.Stat(b.tagPath(name, tag))
	if err != nil {
		return time.Time{}, err
	}
	return info.ModTime(), nil
}

func (b *LocalBackend) Stats() (StorageStats, error) {
	blobs, err := b.AllBlobs()
	if err != nil {
		return StorageStats{}, err
	}
	var total int64
	for _, digest := range blobs {
		size, _ := b.BlobSize(digest)
		total += size
	}
	repos, _ := b.ListRepositories()
	return StorageStats{
		TotalSize: total,
		BlobCount: len(blobs),
		RepoCount: len(repos),
	}, nil
}

// ── GC helpers (local-only, not part of Backend interface) ───────────────────

func (b *LocalBackend) BlobSize(digest string) (int64, error) {
	info, err := os.Stat(b.blobPath(digest))
	if err != nil {
		return 0, err
	}
	return info.Size(), nil
}

func (b *LocalBackend) AllBlobs() ([]string, error) {
	base := filepath.Join(b.root, "blobs", "sha256")
	var blobs []string
	err := filepath.WalkDir(base, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if !d.IsDir() && d.Name() == "data" {
			digest := "sha256:" + filepath.Base(filepath.Dir(path))
			blobs = append(blobs, digest)
		}
		return nil
	})
	return blobs, err
}

func (b *LocalBackend) ReferencedBlobs() (map[string]struct{}, error) {
	repos, err := b.ListRepositories()
	if err != nil {
		return nil, err
	}
	referenced := make(map[string]struct{})
	for _, name := range repos {
		manifDir := filepath.Join(b.root, "repositories", filepath.FromSlash(name), "manifests")
		entries, err := os.ReadDir(manifDir)
		if err != nil {
			continue
		}
		for _, e := range entries {
			raw, err := os.ReadFile(filepath.Join(manifDir, e.Name()))
			if err != nil {
				continue
			}
			var m struct {
				Config struct {
					Digest string `json:"digest"`
				} `json:"config"`
				Layers []struct {
					Digest string `json:"digest"`
				} `json:"layers"`
			}
			if err := json.Unmarshal(raw, &m); err != nil {
				continue
			}
			if m.Config.Digest != "" {
				referenced[m.Config.Digest] = struct{}{}
			}
			for _, l := range m.Layers {
				if l.Digest != "" {
					referenced[l.Digest] = struct{}{}
				}
			}
		}
	}
	return referenced, nil
}

func (b *LocalBackend) RemoveBlob(digest string) error {
	dir := filepath.Dir(b.blobPath(digest))
	if err := os.RemoveAll(dir); err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}
