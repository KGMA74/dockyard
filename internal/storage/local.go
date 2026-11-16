package storage

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"time"

	"dockyard/internal/reference"
)

// LocalBackend stores blobs, manifests, and uploads on the local filesystem.
// Layout:
//
//	<root>/blobs/sha256/<2-char-prefix>/<full-digest>/data
//	<root>/repositories/<name>/manifests/<digest>
//	<root>/repositories/<name>/tags/<tag>  (file content = digest)
//	<root>/uploads/<uuid>/data
//
// Every name, tag, digest and upload id is validated (internal/reference)
// before it is joined into a path: they all come from clients.
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

// ── path helpers (callers validate their arguments first) ────────────────────

func (b *LocalBackend) blobPath(digest string) string {
	d := strings.TrimPrefix(digest, "sha256:")
	return filepath.Join(b.root, "blobs", "sha256", d[:2], d, "data")
}

func (b *LocalBackend) uploadDir(id string) string {
	return filepath.Join(b.root, "uploads", id)
}

func (b *LocalBackend) uploadDataPath(id string) string {
	return filepath.Join(b.uploadDir(id), "data")
}

func (b *LocalBackend) repoDir(name string) string {
	return filepath.Join(b.root, "repositories", filepath.FromSlash(name))
}

func (b *LocalBackend) manifestPath(name, digest string) string {
	return filepath.Join(b.repoDir(name), "manifests", strings.TrimPrefix(digest, "sha256:"))
}

func (b *LocalBackend) tagPath(name, tag string) string {
	return filepath.Join(b.repoDir(name), "tags", tag)
}

// ── Backend interface — Blobs ─────────────────────────────────────────────────

// PutBlob streams content into a private temp file and only moves it to its
// content-addressed location once the hash matches. Writing straight to the
// final path (the previous behaviour) meant a failed or malicious upload
// truncated and then deleted a perfectly good blob that already had this digest.
func (b *LocalBackend) PutBlob(digest string, content io.Reader, _ int64) error {
	if err := reference.CheckDigest(digest); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Join(b.root, "uploads"), "put-*")
	if err != nil {
		return err
	}
	tmpPath := tmp.Name()
	h := sha256.New()
	_, err = io.Copy(io.MultiWriter(tmp, h), content)
	// Close before any removal: Windows cannot delete an open file.
	closeErr := tmp.Close()
	if err == nil {
		err = closeErr
	}
	if err != nil {
		_ = os.Remove(tmpPath)
		return err
	}
	if got := "sha256:" + hex.EncodeToString(h.Sum(nil)); got != digest {
		_ = os.Remove(tmpPath)
		return fmt.Errorf("digest mismatch: expected %s got %s", digest, got)
	}
	return b.installBlob(tmpPath, digest)
}

// installBlob moves a hash-verified file to its content-addressed location.
// If a blob with this digest and size is already there it is the same bytes,
// so the new copy is simply dropped — the existing file is never rewritten
// (which also keeps concurrent readers safe on Windows).
func (b *LocalBackend) installBlob(verified, digest string) error {
	dst := b.blobPath(digest)
	if existing, err := os.Stat(dst); err == nil {
		if incoming, err := os.Stat(verified); err == nil && incoming.Size() == existing.Size() {
			return os.Remove(verified)
		}
		// Same digest, different size: the stored copy is damaged — replace it.
	}
	if err := os.MkdirAll(filepath.Dir(dst), 0755); err != nil {
		_ = os.Remove(verified)
		return err
	}
	if err := os.Rename(verified, dst); err != nil {
		_ = os.Remove(verified)
		return err
	}
	return nil
}

func (b *LocalBackend) GetBlob(digest string) (io.ReadCloser, int64, error) {
	if err := reference.CheckDigest(digest); err != nil {
		return nil, 0, err
	}
	f, err := os.Open(b.blobPath(digest))
	if err != nil {
		return nil, 0, err
	}
	info, err := f.Stat()
	if err != nil {
		_ = f.Close()
		return nil, 0, err
	}
	return f, info.Size(), nil
}

func (b *LocalBackend) BlobExists(digest string) (bool, error) {
	if !reference.ValidDigest(digest) {
		return false, nil
	}
	_, err := os.Stat(b.blobPath(digest))
	if os.IsNotExist(err) {
		return false, nil
	}
	return err == nil, err
}

func (b *LocalBackend) DeleteBlob(digest string) error {
	return b.RemoveBlob(digest)
}

// ── Backend interface — Uploads ───────────────────────────────────────────────

func (b *LocalBackend) InitUpload(uuid string) error {
	if err := reference.CheckUploadID(uuid); err != nil {
		return err
	}
	if err := os.MkdirAll(b.uploadDir(uuid), 0755); err != nil {
		return err
	}
	f, err := os.Create(b.uploadDataPath(uuid))
	if err != nil {
		return err
	}
	return f.Close()
}

func (b *LocalBackend) AppendUpload(uuid string, content io.Reader) error {
	if err := reference.CheckUploadID(uuid); err != nil {
		return err
	}
	f, err := os.OpenFile(b.uploadDataPath(uuid), os.O_APPEND|os.O_WRONLY, 0644)
	if err != nil {
		return fmt.Errorf("upload %s not found", uuid)
	}
	defer func() { _ = f.Close() }()
	_, err = io.Copy(f, content)
	return err
}

func (b *LocalBackend) CommitUpload(uuid, digest string) error {
	if err := reference.CheckUploadID(uuid); err != nil {
		return err
	}
	if err := reference.CheckDigest(digest); err != nil {
		return err
	}
	src := b.uploadDataPath(uuid)
	f, err := os.Open(src)
	if err != nil {
		return fmt.Errorf("upload %s not found", uuid)
	}
	h := sha256.New()
	if _, err = io.Copy(h, f); err != nil {
		_ = f.Close()
		return err
	}
	_ = f.Close()
	if got := "sha256:" + hex.EncodeToString(h.Sum(nil)); got != digest {
		_ = os.RemoveAll(b.uploadDir(uuid))
		return fmt.Errorf("digest mismatch: expected %s got %s", digest, got)
	}
	if err := b.installBlob(src, digest); err != nil {
		return err
	}
	return os.RemoveAll(b.uploadDir(uuid))
}

func (b *LocalBackend) DeleteUpload(uuid string) error {
	if err := reference.CheckUploadID(uuid); err != nil {
		return err
	}
	return os.RemoveAll(b.uploadDir(uuid))
}

func (b *LocalBackend) GetUploadSize(uuid string) (int64, error) {
	if err := reference.CheckUploadID(uuid); err != nil {
		return 0, err
	}
	info, err := os.Stat(b.uploadDataPath(uuid))
	if err != nil {
		return 0, err
	}
	return info.Size(), nil
}

// ── Backend interface — Manifests ─────────────────────────────────────────────

func (b *LocalBackend) PutManifest(name, ref, digest string, content []byte) error {
	if err := checkManifestTarget(name, ref); err != nil {
		return err
	}
	if err := reference.CheckDigest(digest); err != nil {
		return err
	}
	mp := b.manifestPath(name, digest)
	if err := os.MkdirAll(filepath.Dir(mp), 0755); err != nil {
		return err
	}
	if err := os.WriteFile(mp, content, 0644); err != nil {
		return err
	}
	if !reference.IsDigest(ref) {
		tp := b.tagPath(name, ref)
		if err := os.MkdirAll(filepath.Dir(tp), 0755); err != nil {
			return err
		}
		return os.WriteFile(tp, []byte(digest), 0644)
	}
	return nil
}

func (b *LocalBackend) GetManifest(name, ref string) ([]byte, string, error) {
	if err := checkManifestTarget(name, ref); err != nil {
		return nil, "", err
	}
	digest, err := b.resolveDigest(name, ref)
	if err != nil {
		return nil, "", err
	}
	content, err := os.ReadFile(b.manifestPath(name, digest))
	if err != nil {
		return nil, "", fmt.Errorf("manifest %s not found", digest)
	}
	return content, digest, nil
}

// resolveDigest turns a tag into the digest it points at (a digest reference
// is returned as-is). The tag file content is validated too: it becomes part
// of a path right after.
func (b *LocalBackend) resolveDigest(name, ref string) (string, error) {
	if reference.IsDigest(ref) {
		return ref, nil
	}
	raw, err := os.ReadFile(b.tagPath(name, ref))
	if err != nil {
		return "", fmt.Errorf("tag %q not found in %s", ref, name)
	}
	digest := strings.TrimSpace(string(raw))
	if !reference.ValidDigest(digest) {
		return "", fmt.Errorf("tag %q in %s points at a malformed digest", ref, name)
	}
	return digest, nil
}

func (b *LocalBackend) DeleteManifest(name, ref string) error {
	if err := checkManifestTarget(name, ref); err != nil {
		return err
	}
	// Resolve a tag reference to its digest — deleting by tag must remove the
	// manifest it points at, not silently do nothing.
	digest, err := b.resolveDigest(name, ref)
	if err != nil {
		return err
	}

	// If this manifest is an index, prune child manifests nothing else needs.
	indexRaw, _ := os.ReadFile(b.manifestPath(name, digest))

	if err := os.Remove(b.manifestPath(name, digest)); err != nil && !os.IsNotExist(err) {
		return err
	}
	tagsDir := filepath.Join(b.repoDir(name), "tags")
	entries, _ := os.ReadDir(tagsDir)
	for _, e := range entries {
		raw, err := os.ReadFile(filepath.Join(tagsDir, e.Name()))
		if err == nil && strings.TrimSpace(string(raw)) == digest {
			_ = os.Remove(filepath.Join(tagsDir, e.Name()))
		}
	}

	if len(indexRaw) > 0 {
		b.pruneOrphanChildManifests(name, indexRaw)
	}

	// If no manifests remain, the repository is empty — drop it entirely so it
	// no longer shows up in ListRepositories (which detects repos by walking
	// for a "tags" directory, and an empty one would otherwise still match).
	manifestsDir := filepath.Join(b.repoDir(name), "manifests")
	if remaining, err := os.ReadDir(manifestsDir); err == nil && len(remaining) == 0 {
		_ = os.RemoveAll(b.repoDir(name))
	}
	return nil
}

func (b *LocalBackend) ManifestExists(name, ref string) (bool, error) {
	if checkManifestTarget(name, ref) != nil {
		return false, nil
	}
	var path string
	if reference.IsDigest(ref) {
		path = b.manifestPath(name, ref)
	} else {
		path = b.tagPath(name, ref)
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
	if err := reference.CheckName(name); err != nil {
		return nil, err
	}
	entries, err := os.ReadDir(filepath.Join(b.repoDir(name), "tags"))
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
	if err := reference.CheckName(name); err != nil {
		return err
	}
	dir := b.repoDir(name)
	if _, err := os.Stat(dir); os.IsNotExist(err) {
		return fmt.Errorf("repository %q not found", name)
	}
	return os.RemoveAll(dir)
}

func (b *LocalBackend) TagPushedAt(name, tag string) (time.Time, error) {
	if err := reference.CheckName(name); err != nil {
		return time.Time{}, err
	}
	if err := reference.CheckTag(tag); err != nil {
		return time.Time{}, err
	}
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

// ── GC helpers (not part of the Backend interface) ───────────────────────────

func (b *LocalBackend) BlobSize(digest string) (int64, error) {
	if err := reference.CheckDigest(digest); err != nil {
		return 0, err
	}
	info, err := os.Stat(b.blobPath(digest))
	if err != nil {
		return 0, err
	}
	return info.Size(), nil
}

// BlobModTime reports when a blob was written. GC uses it to leave freshly
// uploaded blobs alone: during a push, layers land before the manifest that
// references them.
func (b *LocalBackend) BlobModTime(digest string) (time.Time, error) {
	if err := reference.CheckDigest(digest); err != nil {
		return time.Time{}, err
	}
	info, err := os.Stat(b.blobPath(digest))
	if err != nil {
		return time.Time{}, err
	}
	return info.ModTime(), nil
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
			if reference.ValidDigest(digest) {
				blobs = append(blobs, digest)
			}
		}
		return nil
	})
	return blobs, err
}

// ReferencedBlobs returns every blob digest some manifest still needs. It
// fails rather than under-report: GC deletes whatever is missing from this
// set, so silently skipping a manifest that could not be read (the previous
// behaviour) meant deleting the layers of a live image.
func (b *LocalBackend) ReferencedBlobs() (map[string]struct{}, error) {
	repos, err := b.ListRepositories()
	if err != nil {
		return nil, err
	}
	referenced := make(map[string]struct{})
	for _, name := range repos {
		manifDir := filepath.Join(b.repoDir(name), "manifests")
		entries, err := os.ReadDir(manifDir)
		if os.IsNotExist(err) {
			continue // repository deleted while we were walking
		}
		if err != nil {
			return nil, fmt.Errorf("list manifests of %s: %w", name, err)
		}
		var childErr error
		fetchChild := func(digest string) ([]byte, error) {
			if err := reference.CheckDigest(digest); err != nil {
				return nil, err
			}
			raw, err := os.ReadFile(b.manifestPath(name, digest))
			if err != nil && !os.IsNotExist(err) {
				// An absent child (partial mirror, pruned platform)
				// references nothing; any other failure is unknown state.
				childErr = fmt.Errorf("read child manifest %s of %s: %w", digest, name, err)
			}
			return raw, err
		}
		for _, e := range entries {
			raw, err := os.ReadFile(filepath.Join(manifDir, e.Name()))
			if os.IsNotExist(err) {
				continue // deleted concurrently
			}
			if err != nil {
				return nil, fmt.Errorf("read manifest %s/%s: %w", name, e.Name(), err)
			}
			collectBlobRefs(raw, referenced, fetchChild)
			if childErr != nil {
				return nil, childErr
			}
		}
	}
	return referenced, nil
}

// pruneOrphanChildManifests removes the per-platform child manifest files of a
// just-deleted index when no other manifest in the repository still references
// them — otherwise ReferencedBlobs keeps every layer they list alive forever.
func (b *LocalBackend) pruneOrphanChildManifests(name string, deletedIndexRaw []byte) {
	children := childManifestDigests(deletedIndexRaw)
	if len(children) == 0 {
		return
	}
	manifDir := filepath.Join(b.repoDir(name), "manifests")
	entries, err := os.ReadDir(manifDir)
	if err != nil {
		return
	}
	stillReferenced := make(map[string]struct{})
	for _, e := range entries {
		raw, err := os.ReadFile(filepath.Join(manifDir, e.Name()))
		if err != nil {
			continue
		}
		for _, d := range childManifestDigests(raw) {
			stillReferenced[d] = struct{}{}
		}
	}
	for _, child := range children {
		if _, ok := stillReferenced[child]; ok {
			continue
		}
		// Child digests come from manifest JSON a client pushed — validate
		// before they become a path to delete.
		if !reference.ValidDigest(child) {
			continue
		}
		_ = os.Remove(b.manifestPath(name, child))
	}
}

func (b *LocalBackend) RemoveBlob(digest string) error {
	if err := reference.CheckDigest(digest); err != nil {
		return err
	}
	dir := filepath.Dir(b.blobPath(digest))
	if err := os.RemoveAll(dir); err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}

// PurgeStaleUploads removes upload sessions (and orphaned PutBlob temp files)
// untouched for longer than olderThan. A client that disconnects mid-push
// never sends the final PUT, so without this the uploads directory only grows.
func (b *LocalBackend) PurgeStaleUploads(olderThan time.Duration) (int, error) {
	dir := filepath.Join(b.root, "uploads")
	entries, err := os.ReadDir(dir)
	if err != nil {
		return 0, err
	}
	cutoff := time.Now().Add(-olderThan)
	removed := 0
	for _, e := range entries {
		path := filepath.Join(dir, e.Name())
		// A session directory's mtime does not move when its data file is
		// appended to — look at the data file as well.
		fresh := false
		for _, candidate := range []string{path, filepath.Join(path, "data")} {
			if info, err := os.Stat(candidate); err == nil && info.ModTime().After(cutoff) {
				fresh = true
			}
		}
		if fresh {
			continue
		}
		if err := os.RemoveAll(path); err == nil {
			removed++
		}
	}
	return removed, nil
}
