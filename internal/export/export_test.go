package export

import (
	"bytes"
	"slices"
	"strings"
	"testing"

	"dockyard/internal/storage"
	"dockyard/internal/storage/storagetest"
)

func newBackend(t *testing.T) *storage.LocalBackend {
	t.Helper()
	b, err := storage.NewLocal(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func seedRepo(t *testing.T, b *storage.LocalBackend, name string) (tagDigests map[string]string, blobDigests []string) {
	t.Helper()
	tagDigests = map[string]string{}

	config := []byte(`{"architecture":"amd64"}`)
	layer := []byte("layer-bytes-for-export-roundtrip")
	configDgst := storagetest.Digest(config)
	layerDgst := storagetest.Digest(layer)
	for digest, content := range map[string][]byte{configDgst: config, layerDgst: layer} {
		if err := b.PutBlob(digest, bytes.NewReader(content), int64(len(content))); err != nil {
			t.Fatal(err)
		}
	}
	blobDigests = []string{configDgst, layerDgst}

	// Two tags: one own manifest, one sharing the same blobs.
	for _, tag := range []string{"v1", "v2"} {
		manifest := storagetest.ManifestFor(configDgst, layerDgst)
		if tag == "v2" {
			manifest = append(manifest, ' ') // different bytes → different digest
		}
		digest := storagetest.Digest(manifest)
		if err := b.PutManifest(name, tag, digest, manifest); err != nil {
			t.Fatal(err)
		}
		tagDigests[tag] = digest
	}
	return tagDigests, blobDigests
}

func TestExportImportRoundTripPreservesDigests(t *testing.T) {
	src := newBackend(t)
	tagDigests, blobDigests := seedRepo(t, src, "team/app")

	var buf bytes.Buffer
	if err := Export(&buf, src, "team/app"); err != nil {
		t.Fatalf("Export: %v", err)
	}

	dst := newBackend(t)
	tags, err := Import(bytes.NewReader(buf.Bytes()), dst, "team/app")
	if err != nil {
		t.Fatalf("Import: %v", err)
	}
	if tags != 2 {
		t.Errorf("imported tags = %d, want 2", tags)
	}

	for tag, wantDigest := range tagDigests {
		raw, gotDigest, err := dst.GetManifest("team/app", tag)
		if err != nil {
			t.Fatalf("imported manifest %s: %v", tag, err)
		}
		if gotDigest != wantDigest {
			t.Errorf("tag %s digest = %s, want %s", tag, gotDigest, wantDigest)
		}
		if storagetest.Digest(raw) != wantDigest {
			t.Errorf("tag %s content does not hash to its digest", tag)
		}
	}
	for _, digest := range blobDigests {
		if ok, _ := dst.BlobExists(digest); !ok {
			t.Errorf("blob %s missing after import", digest)
		}
	}

	gotTags, _ := dst.ListTags("team/app")
	slices.Sort(gotTags)
	if !slices.Equal(gotTags, []string{"v1", "v2"}) {
		t.Errorf("imported tags = %v", gotTags)
	}
}

func TestImportRejectsNonLayout(t *testing.T) {
	dst := newBackend(t)
	if _, err := Import(bytes.NewReader([]byte("not a tar at all")), dst, "x"); err == nil {
		t.Fatal("garbage accepted as OCI layout")
	}
}

func TestPreflightCatchesMissingBlob(t *testing.T) {
	b := newBackend(t)
	// Manifest referencing a blob that was never stored.
	manifest := storagetest.ManifestFor("sha256:" + strings.Repeat("0", 64))
	if err := b.PutManifest("broken/app", "v1", storagetest.Digest(manifest), manifest); err != nil {
		t.Fatal(err)
	}
	if err := Preflight(b, "broken/app"); err == nil {
		t.Fatal("preflight passed despite missing blob")
	}

	src := newBackend(t)
	seedRepo(t, src, "ok/app")
	if err := Preflight(src, "ok/app"); err != nil {
		t.Fatalf("preflight on healthy repo: %v", err)
	}
}

func TestExportEmptyRepoFails(t *testing.T) {
	b := newBackend(t)
	var buf bytes.Buffer
	if err := Export(&buf, b, "ghost/repo"); err == nil {
		t.Fatal("export of empty repo succeeded")
	}
}
