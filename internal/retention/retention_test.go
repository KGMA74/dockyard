package retention

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"dockyard/internal/storage"
	"dockyard/internal/storage/storagetest"
	"dockyard/internal/store"
)

type fixture struct {
	engine  *Engine
	backend *storage.LocalBackend
	store   *store.Store
	dir     string
}

func newFixture(t *testing.T) *fixture {
	t.Helper()
	dir := t.TempDir()
	st, err := store.Open(filepath.Join(dir, "dockyard.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	backend, err := storage.NewLocal(filepath.Join(dir, "registry"))
	if err != nil {
		t.Fatal(err)
	}
	return &fixture{engine: New(st, backend), backend: backend, store: st, dir: dir}
}

// pushTag stores a manifest under tag with a unique digest and backdates the
// tag file's mtime (TagPushedAt reads it).
func (f *fixture) pushTag(t *testing.T, repo, tag string, age time.Duration) string {
	t.Helper()
	manifest := storagetest.ManifestFor(storagetest.Digest([]byte(repo + tag)))
	digest := storagetest.Digest(manifest)
	if err := f.backend.PutManifest(repo, tag, digest, manifest); err != nil {
		t.Fatal(err)
	}
	when := time.Now().Add(-age)
	tagPath := filepath.Join(f.dir, "registry", "repositories", filepath.FromSlash(repo), "tags", tag)
	if err := os.Chtimes(tagPath, when, when); err != nil {
		t.Fatal(err)
	}
	return digest
}

// pushTagSharedDigest points tag at an existing digest.
func (f *fixture) pushTagSharedDigest(t *testing.T, repo, tag, digest string, content []byte, age time.Duration) {
	t.Helper()
	if err := f.backend.PutManifest(repo, tag, digest, content); err != nil {
		t.Fatal(err)
	}
	when := time.Now().Add(-age)
	tagPath := filepath.Join(f.dir, "registry", "repositories", filepath.FromSlash(repo), "tags", tag)
	if err := os.Chtimes(tagPath, when, when); err != nil {
		t.Fatal(err)
	}
}

func (f *fixture) addPolicy(t *testing.T, p store.RetentionPolicy) {
	t.Helper()
	p.Enabled = true
	if _, err := f.store.CreateRetentionPolicy(p); err != nil {
		t.Fatal(err)
	}
}

func planTags(plan *Plan) map[string]bool {
	out := map[string]bool{}
	for _, c := range plan.Delete {
		out[c.Repo+":"+c.Tag] = true
	}
	return out
}

func TestKeepNRule(t *testing.T) {
	f := newFixture(t)
	f.pushTag(t, "app", "v1", 96*time.Hour)
	f.pushTag(t, "app", "v2", 72*time.Hour)
	f.pushTag(t, "app", "v3", 48*time.Hour)
	f.pushTag(t, "app", "v4", 24*time.Hour)
	f.addPolicy(t, store.RetentionPolicy{RepoPattern: "app", KeepN: 2})

	plan, err := f.engine.Evaluate(time.Now())
	if err != nil {
		t.Fatal(err)
	}
	got := planTags(plan)
	if len(got) != 2 || !got["app:v1"] || !got["app:v2"] {
		t.Errorf("keep-2 plan = %v, want v1+v2 deleted", got)
	}
}

func TestAgeRuleUsesPullsOverPush(t *testing.T) {
	f := newFixture(t)
	f.pushTag(t, "app", "old-unused", 40*24*time.Hour)
	f.pushTag(t, "app", "old-but-pulled", 40*24*time.Hour)
	if err := f.store.RecordPulls(map[[2]string]int{{"app", "old-but-pulled"}: 1}); err != nil {
		t.Fatal(err)
	}
	f.addPolicy(t, store.RetentionPolicy{RepoPattern: "app", UnpulledDays: 30})

	plan, err := f.engine.Evaluate(time.Now())
	if err != nil {
		t.Fatal(err)
	}
	got := planTags(plan)
	if !got["app:old-unused"] {
		t.Errorf("stale tag not condemned: %v", got)
	}
	if got["app:old-but-pulled"] {
		t.Error("recently pulled tag condemned despite pull tracking")
	}
}

func TestKeepPatternsAndProtectedTags(t *testing.T) {
	f := newFixture(t)
	f.pushTag(t, "app", "v1.0.0", 90*24*time.Hour)
	f.pushTag(t, "app", "nightly-42", 90*24*time.Hour)
	f.pushTag(t, "app", "special", 90*24*time.Hour)
	f.addPolicy(t, store.RetentionPolicy{
		RepoPattern:   "app",
		UnpulledDays:  30,
		KeepPatterns:  []string{"v*"},
		ProtectedTags: []string{"special"},
	})

	plan, err := f.engine.Evaluate(time.Now())
	if err != nil {
		t.Fatal(err)
	}
	got := planTags(plan)
	if !got["app:nightly-42"] || got["app:v1.0.0"] || got["app:special"] {
		t.Errorf("plan = %v, want only nightly-42", got)
	}
}

func TestSharedDigestGuard(t *testing.T) {
	f := newFixture(t)
	manifest := storagetest.ManifestFor(storagetest.Digest([]byte("shared")))
	digest := storagetest.Digest(manifest)
	f.pushTagSharedDigest(t, "app", "old-alias", digest, manifest, 90*24*time.Hour)
	f.pushTagSharedDigest(t, "app", "latest", digest, manifest, time.Hour)
	f.addPolicy(t, store.RetentionPolicy{RepoPattern: "app", UnpulledDays: 30})

	plan, err := f.engine.Evaluate(time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if len(plan.Delete) != 0 {
		t.Errorf("shared-digest tag scheduled for deletion: %+v", plan.Delete)
	}
	if len(plan.Skipped) != 1 || plan.Skipped[0].Tag != "old-alias" {
		t.Errorf("skipped = %+v, want old-alias skipped", plan.Skipped)
	}
}

func TestPolicyScopeAndApply(t *testing.T) {
	f := newFixture(t)
	f.pushTag(t, "team-a/app", "stale", 90*24*time.Hour)
	f.pushTag(t, "team-b/app", "stale", 90*24*time.Hour)
	f.addPolicy(t, store.RetentionPolicy{RepoPattern: "team-a/*", UnpulledDays: 30})

	plan, err := f.engine.Evaluate(time.Now())
	if err != nil {
		t.Fatal(err)
	}
	got := planTags(plan)
	if !got["team-a/app:stale"] || got["team-b/app:stale"] {
		t.Fatalf("scope leak: %v", got)
	}

	deleted, err := f.engine.Apply(plan)
	if err != nil || deleted != 1 {
		t.Fatalf("Apply = (%d, %v), want 1 deletion", deleted, err)
	}
	if tags, _ := f.backend.ListTags("team-a/app"); len(tags) != 0 {
		t.Errorf("team-a tag survived apply: %v", tags)
	}
	if tags, _ := f.backend.ListTags("team-b/app"); len(tags) != 1 {
		t.Errorf("team-b tag deleted out of scope: %v", tags)
	}
}

func TestNoPolicyNoDeletion(t *testing.T) {
	f := newFixture(t)
	f.pushTag(t, "app", "ancient", 900*24*time.Hour)
	plan, err := f.engine.Evaluate(time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if len(plan.Delete) != 0 {
		t.Errorf("deletion without any policy: %+v", plan.Delete)
	}
}
