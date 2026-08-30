// Package storagetest provides a reusable conformance suite for storage.Backend
// implementations. Any backend (local, S3, future ones) can be validated with:
//
//	storagetest.RunBackendContract(t, func(t *testing.T) storage.Backend { ... })
package storagetest

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"slices"
	"strings"
	"testing"

	"dockyard/internal/storage"
)

// Digest returns the canonical sha256 digest string for content.
func Digest(content []byte) string {
	h := sha256.Sum256(content)
	return "sha256:" + hex.EncodeToString(h[:])
}

// ManifestFor builds a minimal Docker v2 manifest referencing the given config
// and layer digests, in the shape ReferencedBlobs() parses for GC.
func ManifestFor(configDigest string, layerDigests ...string) []byte {
	var layers []string
	for _, d := range layerDigests {
		layers = append(layers, fmt.Sprintf(`{"digest":%q,"size":0}`, d))
	}
	return fmt.Appendf(nil,
		`{"schemaVersion":2,"mediaType":"application/vnd.docker.distribution.manifest.v2+json","config":{"digest":%q,"size":0},"layers":[%s]}`,
		configDigest, strings.Join(layers, ","),
	)
}

// IndexFor builds a minimal OCI image index / manifest list referencing the
// given child manifest digests.
func IndexFor(childDigests ...string) []byte {
	var entries []string
	for _, d := range childDigests {
		entries = append(entries, fmt.Sprintf(`{"digest":%q,"size":0,"mediaType":"application/vnd.oci.image.manifest.v1+json"}`, d))
	}
	return fmt.Appendf(nil,
		`{"schemaVersion":2,"mediaType":"application/vnd.oci.image.index.v1+json","manifests":[%s]}`,
		strings.Join(entries, ","),
	)
}

// gcBackend mirrors the GC helper methods implemented by every real backend but
// absent from storage.Backend (they're reached by type assertion in admin/server).
type gcBackend interface {
	AllBlobs() ([]string, error)
	BlobSize(digest string) (int64, error)
	ReferencedBlobs() (map[string]struct{}, error)
	RemoveBlob(digest string) error
}

// RunBackendContract exercises the full storage.Backend interface against a
// fresh backend per subtest. newBackend must return an empty, isolated store.
func RunBackendContract(t *testing.T, newBackend func(t *testing.T) storage.Backend) {
	t.Run("BlobRoundTrip", func(t *testing.T) {
		b := newBackend(t)
		content := []byte("hello blob")
		dgst := Digest(content)

		if ok, err := b.BlobExists(dgst); err != nil || ok {
			t.Fatalf("BlobExists before put = (%v, %v), want (false, nil)", ok, err)
		}
		if err := b.PutBlob(dgst, bytes.NewReader(content), int64(len(content))); err != nil {
			t.Fatalf("PutBlob: %v", err)
		}
		if ok, err := b.BlobExists(dgst); err != nil || !ok {
			t.Fatalf("BlobExists after put = (%v, %v), want (true, nil)", ok, err)
		}
		rc, size, err := b.GetBlob(dgst)
		if err != nil {
			t.Fatalf("GetBlob: %v", err)
		}
		defer func() { _ = rc.Close() }()
		got, err := io.ReadAll(rc)
		if err != nil {
			t.Fatalf("read blob: %v", err)
		}
		if !bytes.Equal(got, content) {
			t.Errorf("blob content = %q, want %q", got, content)
		}
		if size != int64(len(content)) {
			t.Errorf("blob size = %d, want %d", size, len(content))
		}
	})

	t.Run("BlobDigestMismatchRejected", func(t *testing.T) {
		b := newBackend(t)
		content := []byte("actual content")
		wrong := Digest([]byte("other content"))
		if err := b.PutBlob(wrong, bytes.NewReader(content), int64(len(content))); err == nil {
			t.Fatal("PutBlob with wrong digest succeeded, want error")
		}
		if ok, _ := b.BlobExists(wrong); ok {
			t.Error("mismatched blob was persisted")
		}
	})

	t.Run("BlobDelete", func(t *testing.T) {
		b := newBackend(t)
		content := []byte("to delete")
		dgst := Digest(content)
		if err := b.PutBlob(dgst, bytes.NewReader(content), int64(len(content))); err != nil {
			t.Fatalf("PutBlob: %v", err)
		}
		if err := b.DeleteBlob(dgst); err != nil {
			t.Fatalf("DeleteBlob: %v", err)
		}
		if ok, _ := b.BlobExists(dgst); ok {
			t.Error("blob still exists after delete")
		}
	})

	t.Run("ChunkedUpload", func(t *testing.T) {
		b := newBackend(t)
		part1, part2 := []byte("first-half|"), []byte("second-half")
		full := append(append([]byte{}, part1...), part2...)
		dgst := Digest(full)

		const id = "upload-test-uuid"
		if err := b.InitUpload(id); err != nil {
			t.Fatalf("InitUpload: %v", err)
		}
		if err := b.AppendUpload(id, bytes.NewReader(part1)); err != nil {
			t.Fatalf("AppendUpload part1: %v", err)
		}
		if n, err := b.GetUploadSize(id); err != nil || n != int64(len(part1)) {
			t.Fatalf("GetUploadSize after part1 = (%d, %v), want (%d, nil)", n, err, len(part1))
		}
		if err := b.AppendUpload(id, bytes.NewReader(part2)); err != nil {
			t.Fatalf("AppendUpload part2: %v", err)
		}
		if err := b.CommitUpload(id, dgst); err != nil {
			t.Fatalf("CommitUpload: %v", err)
		}
		rc, _, err := b.GetBlob(dgst)
		if err != nil {
			t.Fatalf("GetBlob after commit: %v", err)
		}
		defer func() { _ = rc.Close() }()
		got, _ := io.ReadAll(rc)
		if !bytes.Equal(got, full) {
			t.Errorf("committed blob = %q, want %q", got, full)
		}
	})

	t.Run("CommitUploadDigestMismatch", func(t *testing.T) {
		b := newBackend(t)
		const id = "upload-mismatch-uuid"
		if err := b.InitUpload(id); err != nil {
			t.Fatalf("InitUpload: %v", err)
		}
		if err := b.AppendUpload(id, bytes.NewReader([]byte("data"))); err != nil {
			t.Fatalf("AppendUpload: %v", err)
		}
		wrong := Digest([]byte("not the data"))
		if err := b.CommitUpload(id, wrong); err == nil {
			t.Fatal("CommitUpload with wrong digest succeeded, want error")
		}
		if ok, _ := b.BlobExists(wrong); ok {
			t.Error("mismatched upload was committed as blob")
		}
	})

	t.Run("DeleteUpload", func(t *testing.T) {
		b := newBackend(t)
		const id = "upload-abandoned-uuid"
		if err := b.InitUpload(id); err != nil {
			t.Fatalf("InitUpload: %v", err)
		}
		if err := b.DeleteUpload(id); err != nil {
			t.Fatalf("DeleteUpload: %v", err)
		}
		if _, err := b.GetUploadSize(id); err == nil {
			t.Error("GetUploadSize succeeded after DeleteUpload, want error")
		}
	})

	t.Run("ManifestByTagAndDigest", func(t *testing.T) {
		b := newBackend(t)
		const name = "team/app"
		manifest := ManifestFor(Digest([]byte("cfg")), Digest([]byte("layer")))
		dgst := Digest(manifest)

		if err := b.PutManifest(name, "v1", dgst, manifest); err != nil {
			t.Fatalf("PutManifest: %v", err)
		}
		for _, ref := range []string{"v1", dgst} {
			content, gotDigest, err := b.GetManifest(name, ref)
			if err != nil {
				t.Fatalf("GetManifest(%q): %v", ref, err)
			}
			if !bytes.Equal(content, manifest) {
				t.Errorf("GetManifest(%q) content mismatch", ref)
			}
			if gotDigest != dgst {
				t.Errorf("GetManifest(%q) digest = %q, want %q", ref, gotDigest, dgst)
			}
			if ok, err := b.ManifestExists(name, ref); err != nil || !ok {
				t.Errorf("ManifestExists(%q) = (%v, %v), want (true, nil)", ref, ok, err)
			}
		}
		if _, _, err := b.GetManifest(name, "missing-tag"); err == nil {
			t.Error("GetManifest of unknown tag succeeded, want error")
		}
	})

	t.Run("CatalogAndTags", func(t *testing.T) {
		b := newBackend(t)
		manifest := ManifestFor(Digest([]byte("c")))
		dgst := Digest(manifest)
		for _, repo := range []string{"alpha", "team/nested/app"} {
			if err := b.PutManifest(repo, "latest", dgst, manifest); err != nil {
				t.Fatalf("PutManifest(%q): %v", repo, err)
			}
		}
		if err := b.PutManifest("alpha", "v2", dgst, manifest); err != nil {
			t.Fatalf("PutManifest(alpha, v2): %v", err)
		}

		repos, err := b.ListRepositories()
		if err != nil {
			t.Fatalf("ListRepositories: %v", err)
		}
		for _, want := range []string{"alpha", "team/nested/app"} {
			if !slices.Contains(repos, want) {
				t.Errorf("ListRepositories = %v, missing %q", repos, want)
			}
		}
		tags, err := b.ListTags("alpha")
		if err != nil {
			t.Fatalf("ListTags: %v", err)
		}
		slices.Sort(tags)
		if !slices.Equal(tags, []string{"latest", "v2"}) {
			t.Errorf("ListTags(alpha) = %v, want [latest v2]", tags)
		}
		if tags, err := b.ListTags("unknown-repo"); err != nil || len(tags) != 0 {
			t.Errorf("ListTags(unknown) = (%v, %v), want empty and nil error", tags, err)
		}
		if at, err := b.TagPushedAt("alpha", "latest"); err != nil || at.IsZero() {
			t.Errorf("TagPushedAt = (%v, %v), want non-zero time", at, err)
		}
	})

	t.Run("DeleteManifestRemovesTags", func(t *testing.T) {
		b := newBackend(t)
		const name = "delete/me"
		m1 := ManifestFor(Digest([]byte("cfg-1")))
		m2 := ManifestFor(Digest([]byte("cfg-2")))
		d1, d2 := Digest(m1), Digest(m2)
		if err := b.PutManifest(name, "old", d1, m1); err != nil {
			t.Fatalf("PutManifest old: %v", err)
		}
		if err := b.PutManifest(name, "new", d2, m2); err != nil {
			t.Fatalf("PutManifest new: %v", err)
		}

		if err := b.DeleteManifest(name, d1); err != nil {
			t.Fatalf("DeleteManifest: %v", err)
		}
		tags, err := b.ListTags(name)
		if err != nil {
			t.Fatalf("ListTags: %v", err)
		}
		if slices.Contains(tags, "old") {
			t.Errorf("tag 'old' still present after deleting its manifest: %v", tags)
		}
		if !slices.Contains(tags, "new") {
			t.Errorf("tag 'new' disappeared: %v", tags)
		}
	})

	t.Run("DeleteRepository", func(t *testing.T) {
		b := newBackend(t)
		manifest := ManifestFor(Digest([]byte("c")))
		if err := b.PutManifest("gone/soon", "v1", Digest(manifest), manifest); err != nil {
			t.Fatalf("PutManifest: %v", err)
		}
		if err := b.DeleteRepository("gone/soon"); err != nil {
			t.Fatalf("DeleteRepository: %v", err)
		}
		repos, _ := b.ListRepositories()
		if slices.Contains(repos, "gone/soon") {
			t.Errorf("repository still listed after delete: %v", repos)
		}
		if err := b.DeleteRepository("never/existed"); err == nil {
			t.Error("DeleteRepository of unknown repo succeeded, want error")
		}
	})

	t.Run("Stats", func(t *testing.T) {
		b := newBackend(t)
		blob := []byte("some blob bytes")
		if err := b.PutBlob(Digest(blob), bytes.NewReader(blob), int64(len(blob))); err != nil {
			t.Fatalf("PutBlob: %v", err)
		}
		manifest := ManifestFor(Digest(blob))
		if err := b.PutManifest("stats/app", "v1", Digest(manifest), manifest); err != nil {
			t.Fatalf("PutManifest: %v", err)
		}
		stats, err := b.Stats()
		if err != nil {
			t.Fatalf("Stats: %v", err)
		}
		if stats.BlobCount != 1 {
			t.Errorf("BlobCount = %d, want 1", stats.BlobCount)
		}
		if stats.TotalSize != int64(len(blob)) {
			t.Errorf("TotalSize = %d, want %d", stats.TotalSize, len(blob))
		}
		if stats.RepoCount != 1 {
			t.Errorf("RepoCount = %d, want 1", stats.RepoCount)
		}
	})

	t.Run("GarbageCollectionSet", func(t *testing.T) {
		b := newBackend(t)
		gc, ok := b.(gcBackend)
		if !ok {
			t.Skip("backend does not implement GC helpers")
		}
		put := func(content []byte) string {
			t.Helper()
			d := Digest(content)
			if err := b.PutBlob(d, bytes.NewReader(content), int64(len(content))); err != nil {
				t.Fatalf("PutBlob: %v", err)
			}
			return d
		}
		cfg := put([]byte("gc-config"))
		layer := put([]byte("gc-layer"))
		orphan := put([]byte("gc-orphan"))

		m := ManifestFor(cfg, layer)
		if err := b.PutManifest("gc/app", "v1", Digest(m), m); err != nil {
			t.Fatalf("PutManifest: %v", err)
		}

		all, err := gc.AllBlobs()
		if err != nil {
			t.Fatalf("AllBlobs: %v", err)
		}
		if len(all) != 3 {
			t.Fatalf("AllBlobs = %d, want 3", len(all))
		}
		ref, err := gc.ReferencedBlobs()
		if err != nil {
			t.Fatalf("ReferencedBlobs: %v", err)
		}
		if _, ok := ref[cfg]; !ok {
			t.Errorf("config blob not marked referenced")
		}
		if _, ok := ref[layer]; !ok {
			t.Errorf("layer blob not marked referenced")
		}
		if _, ok := ref[orphan]; ok {
			t.Errorf("orphan blob was marked referenced")
		}
		if err := gc.RemoveBlob(orphan); err != nil {
			t.Fatalf("RemoveBlob: %v", err)
		}
		if ok, _ := b.BlobExists(orphan); ok {
			t.Errorf("orphan blob still present after RemoveBlob")
		}
	})

	t.Run("MultiArchDeleteFreesChildLayers", func(t *testing.T) {
		b := newBackend(t)
		gc, ok := b.(gcBackend)
		if !ok {
			t.Skip("backend does not implement GC helpers")
		}
		put := func(content []byte) string {
			t.Helper()
			d := Digest(content)
			if err := b.PutBlob(d, bytes.NewReader(content), int64(len(content))); err != nil {
				t.Fatalf("PutBlob: %v", err)
			}
			return d
		}
		const name = "gc/multi"
		amdCfg, amdLayer := put([]byte("amd-cfg")), put([]byte("amd-layer"))
		armCfg, armLayer := put([]byte("arm-cfg")), put([]byte("arm-layer"))

		amd := ManifestFor(amdCfg, amdLayer)
		arm := ManifestFor(armCfg, armLayer)
		if err := b.PutManifest(name, Digest(amd), Digest(amd), amd); err != nil {
			t.Fatalf("PutManifest amd: %v", err)
		}
		if err := b.PutManifest(name, Digest(arm), Digest(arm), arm); err != nil {
			t.Fatalf("PutManifest arm: %v", err)
		}
		index := IndexFor(Digest(amd), Digest(arm))
		if err := b.PutManifest(name, "latest", Digest(index), index); err != nil {
			t.Fatalf("PutManifest index: %v", err)
		}

		// All four child layers/configs must be referenced through the index.
		ref, err := gc.ReferencedBlobs()
		if err != nil {
			t.Fatalf("ReferencedBlobs: %v", err)
		}
		for _, d := range []string{amdCfg, amdLayer, armCfg, armLayer} {
			if _, ok := ref[d]; !ok {
				t.Fatalf("blob %s not referenced through the index", d)
			}
		}

		// Delete the tag; the child manifests must be pruned so GC reclaims
		// every layer.
		if err := b.DeleteManifest(name, Digest(index)); err != nil {
			t.Fatalf("DeleteManifest: %v", err)
		}
		ref, err = gc.ReferencedBlobs()
		if err != nil {
			t.Fatalf("ReferencedBlobs after delete: %v", err)
		}
		for _, d := range []string{amdCfg, amdLayer, armCfg, armLayer} {
			if _, ok := ref[d]; ok {
				t.Errorf("blob %s still referenced after deleting the multi-arch tag", d)
			}
		}
	})
}
