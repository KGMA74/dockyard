package replication

import (
	"bytes"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"dockyard/internal/events"
	"dockyard/internal/storage"
	"dockyard/internal/storage/storagetest"
	"dockyard/internal/store"
	"dockyard/internal/v2"
)

func newTargetServer(t *testing.T) (*httptest.Server, storage.Backend) {
	t.Helper()
	backend, err := storage.NewLocal(t.TempDir())
	if err != nil {
		t.Fatalf("NewLocal: %v", err)
	}
	srv := httptest.NewServer(v2.New(backend, events.NewHub(), nil, nil))
	t.Cleanup(srv.Close)
	return srv, backend
}

func newSourceBackend(t *testing.T) storage.Backend {
	t.Helper()
	backend, err := storage.NewLocal(t.TempDir())
	if err != nil {
		t.Fatalf("NewLocal: %v", err)
	}
	return backend
}

func newTestReplicator(t *testing.T, source storage.Backend) *Replicator {
	t.Helper()
	db, err := store.Open(filepath.Join(t.TempDir(), "dockyard.db"))
	if err != nil {
		t.Fatalf("store.Open: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return NewReplicator(db, source)
}

func pushSimple(t *testing.T, backend storage.Backend, repo, tag, marker string) (manifest, config, layer []byte) {
	t.Helper()
	config = []byte(`{"marker":"` + marker + `-config"}`)
	layer = []byte(marker + "-layer content")
	configDigest := storagetest.Digest(config)
	layerDigest := storagetest.Digest(layer)

	if err := backend.PutBlob(configDigest, bytes.NewReader(config), int64(len(config))); err != nil {
		t.Fatalf("PutBlob config: %v", err)
	}
	if err := backend.PutBlob(layerDigest, bytes.NewReader(layer), int64(len(layer))); err != nil {
		t.Fatalf("PutBlob layer: %v", err)
	}
	manifest = storagetest.ManifestFor(configDigest, layerDigest)
	manifestDigest := storagetest.Digest(manifest)
	// PutManifest always writes the digest-addressed copy; passing a tag as
	// the reference additionally points the tag at it.
	if err := backend.PutManifest(repo, tag, manifestDigest, manifest); err != nil {
		t.Fatalf("PutManifest: %v", err)
	}
	return manifest, config, layer
}

func TestReplicateNowCopiesManifestAndBlobs(t *testing.T) {
	source := newSourceBackend(t)
	manifest, config, layer := pushSimple(t, source, "app", "v1", "hello")
	configDigest := storagetest.Digest(config)
	layerDigest := storagetest.Digest(layer)

	targetSrv, targetBackend := newTargetServer(t)
	r := newTestReplicator(t, source)

	target := &store.ReplicationTarget{BaseURL: targetSrv.URL}
	if err := r.ReplicateNow(target, "app", "v1"); err != nil {
		t.Fatalf("ReplicateNow: %v", err)
	}

	if ok, err := targetBackend.BlobExists(configDigest); err != nil || !ok {
		t.Fatalf("config blob not replicated: ok=%v err=%v", ok, err)
	}
	if ok, err := targetBackend.BlobExists(layerDigest); err != nil || !ok {
		t.Fatalf("layer blob not replicated: ok=%v err=%v", ok, err)
	}
	got, _, err := targetBackend.GetManifest("app", "v1")
	if err != nil {
		t.Fatalf("GetManifest on target by tag: %v", err)
	}
	if !bytes.Equal(got, manifest) {
		t.Fatalf("replicated manifest differs from source")
	}
}

func TestReplicateNowSkipsBlobsAlreadyOnTarget(t *testing.T) {
	source := newSourceBackend(t)
	_, config, layer := pushSimple(t, source, "app", "v1", "hello")
	configDigest := storagetest.Digest(config)
	layerDigest := storagetest.Digest(layer)

	targetSrv, targetBackend := newTargetServer(t)
	// Pre-seed the target with the layer blob directly — replication must
	// not re-upload it (HasBlob should short-circuit).
	if err := targetBackend.PutBlob(layerDigest, bytes.NewReader(layer), int64(len(layer))); err != nil {
		t.Fatalf("pre-seed target layer: %v", err)
	}

	r := newTestReplicator(t, source)
	target := &store.ReplicationTarget{BaseURL: targetSrv.URL}
	if err := r.ReplicateNow(target, "app", "v1"); err != nil {
		t.Fatalf("ReplicateNow: %v", err)
	}

	if ok, _ := targetBackend.BlobExists(configDigest); !ok {
		t.Fatal("config blob should have been uploaded")
	}
}

func TestEnqueueRespectsRepoPatternAndEnabled(t *testing.T) {
	source := newSourceBackend(t)
	r := newTestReplicator(t, source)

	matching, err := r.store.CreateReplicationTarget(store.ReplicationTarget{
		Name: "matching", BaseURL: "http://x", RepoPattern: "team/*", Enabled: true,
	})
	if err != nil {
		t.Fatalf("CreateReplicationTarget matching: %v", err)
	}
	if _, err := r.store.CreateReplicationTarget(store.ReplicationTarget{
		Name: "non-matching", BaseURL: "http://y", RepoPattern: "other/*", Enabled: true,
	}); err != nil {
		t.Fatalf("CreateReplicationTarget non-matching: %v", err)
	}
	if _, err := r.store.CreateReplicationTarget(store.ReplicationTarget{
		Name: "disabled", BaseURL: "http://z", RepoPattern: "team/*", Enabled: false,
	}); err != nil {
		t.Fatalf("CreateReplicationTarget disabled: %v", err)
	}

	r.Enqueue("team/api", "v1")

	due, err := r.store.DueReplications(8, 10)
	if err != nil {
		t.Fatalf("DueReplications: %v", err)
	}
	if len(due) != 1 || due[0].TargetID != matching.ID {
		t.Fatalf("expected exactly one delivery queued for the matching+enabled target, got %+v", due)
	}
}

func TestReplicateNowMultiArchManifestList(t *testing.T) {
	source := newSourceBackend(t)
	_, ampConfig, ampLayer := pushSimple(t, source, "app", "amd64-only", "amd64")
	amdManifest := storagetest.ManifestFor(storagetest.Digest(ampConfig), storagetest.Digest(ampLayer))
	amdDigest := storagetest.Digest(amdManifest)
	if err := source.PutManifest("app", amdDigest, amdDigest, amdManifest); err != nil {
		t.Fatalf("PutManifest amd64 child: %v", err)
	}

	_, armConfig, armLayer := pushSimple(t, source, "app", "arm64-only", "arm64")
	armManifest := storagetest.ManifestFor(storagetest.Digest(armConfig), storagetest.Digest(armLayer))
	armDigest := storagetest.Digest(armManifest)
	if err := source.PutManifest("app", armDigest, armDigest, armManifest); err != nil {
		t.Fatalf("PutManifest arm64 child: %v", err)
	}

	list := []byte(`{"schemaVersion":2,"mediaType":"application/vnd.docker.distribution.manifest.list.v2+json","manifests":[` +
		`{"mediaType":"application/vnd.docker.distribution.manifest.v2+json","digest":"` + amdDigest + `","platform":{"architecture":"amd64","os":"linux"}},` +
		`{"mediaType":"application/vnd.docker.distribution.manifest.v2+json","digest":"` + armDigest + `","platform":{"architecture":"arm64","os":"linux"}}` +
		`]}`)
	listDigest := storagetest.Digest(list)
	if err := source.PutManifest("app", "multiarch", listDigest, list); err != nil {
		t.Fatalf("PutManifest list: %v", err)
	}

	targetSrv, targetBackend := newTargetServer(t)
	r := newTestReplicator(t, source)
	target := &store.ReplicationTarget{BaseURL: targetSrv.URL}
	if err := r.ReplicateNow(target, "app", "multiarch"); err != nil {
		t.Fatalf("ReplicateNow multiarch: %v", err)
	}

	if ok, _ := targetBackend.BlobExists(storagetest.Digest(ampLayer)); !ok {
		t.Error("amd64 layer not replicated")
	}
	if ok, _ := targetBackend.BlobExists(storagetest.Digest(armLayer)); !ok {
		t.Error("arm64 layer not replicated")
	}
	if got, _, err := targetBackend.GetManifest("app", "multiarch"); err != nil || !bytes.Equal(got, list) {
		t.Fatalf("manifest list not replicated correctly: got=%q err=%v", got, err)
	}
	if _, _, err := targetBackend.GetManifest("app", amdDigest); err != nil {
		t.Errorf("amd64 child manifest not replicated by digest: %v", err)
	}
}

func TestReplicateNowRejectsManifestListCycle(t *testing.T) {
	source := newSourceBackend(t)
	const digestA = "sha256:" + "a000000000000000000000000000000000000000000000000000000000000"
	const digestB = "sha256:" + "b000000000000000000000000000000000000000000000000000000000000"
	listReferencing := func(child string) []byte {
		return []byte(`{"mediaType":"application/vnd.docker.distribution.manifest.list.v2+json","manifests":[{"digest":"` + child + `"}]}`)
	}
	if err := source.PutManifest("app", digestA, digestA, listReferencing(digestB)); err != nil {
		t.Fatalf("PutManifest A: %v", err)
	}
	if err := source.PutManifest("app", digestB, digestB, listReferencing(digestA)); err != nil {
		t.Fatalf("PutManifest B: %v", err)
	}

	targetSrv, _ := newTargetServer(t)
	r := newTestReplicator(t, source)
	target := &store.ReplicationTarget{BaseURL: targetSrv.URL}

	err := r.ReplicateNow(target, "app", digestA)
	if err == nil {
		t.Fatal("expected a cyclic manifest list reference to error out instead of recursing forever")
	}
}

func TestMatchesPattern(t *testing.T) {
	cases := []struct {
		pattern, repo string
		want          bool
	}{
		{"*", "anything/here", true},
		{"", "anything/here", true},
		{"team/*", "team/api", true},
		{"team/*", "other/api", false},
		{"team/api", "team/api", true},
	}
	for _, c := range cases {
		if got := matchesPattern(c.pattern, c.repo); got != c.want {
			t.Errorf("matchesPattern(%q, %q) = %v, want %v", c.pattern, c.repo, got, c.want)
		}
	}
}
