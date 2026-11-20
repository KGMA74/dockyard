package server

import (
	"os"
	"path/filepath"
	"testing"
)

func TestSeedTrivyCache(t *testing.T) {
	seed := t.TempDir()
	if err := os.MkdirAll(filepath.Join(seed, "db"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(seed, "db", "trivy.db"), []byte("fake-db"), 0o644); err != nil {
		t.Fatal(err)
	}

	t.Run("seeds an empty cache dir", func(t *testing.T) {
		cache := t.TempDir()
		seedTrivyCache(cache, seed)
		got, err := os.ReadFile(filepath.Join(cache, "db", "trivy.db"))
		if err != nil {
			t.Fatalf("expected seeded file: %v", err)
		}
		if string(got) != "fake-db" {
			t.Fatalf("seeded content = %q", got)
		}
	})

	t.Run("adds what the cache lacks, keeps what it has", func(t *testing.T) {
		withJava := t.TempDir()
		for dir, content := range map[string]string{"db": "baked-db", "java-db": "baked-java-db"} {
			if err := os.MkdirAll(filepath.Join(withJava, dir), 0o755); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(withJava, dir, "data"), []byte(content), 0o644); err != nil {
				t.Fatal(err)
			}
		}
		// A volume from an earlier version: a newer vulnerability DB, no Java DB.
		cache := t.TempDir()
		if err := os.MkdirAll(filepath.Join(cache, "db"), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(cache, "db", "data"), []byte("newer-db"), 0o644); err != nil {
			t.Fatal(err)
		}

		seedTrivyCache(cache, withJava)

		if got, _ := os.ReadFile(filepath.Join(cache, "db", "data")); string(got) != "newer-db" {
			t.Fatalf("vulnerability DB overwritten: %q", got)
		}
		if got, _ := os.ReadFile(filepath.Join(cache, "java-db", "data")); string(got) != "baked-java-db" {
			t.Fatalf("Java DB not seeded: %q", got)
		}
	})

	t.Run("no-op when the seed dir is missing", func(t *testing.T) {
		cache := t.TempDir()
		seedTrivyCache(cache, filepath.Join(seed, "does-not-exist"))
		entries, _ := os.ReadDir(cache)
		if len(entries) != 0 {
			t.Fatalf("cache should stay empty, got %d entries", len(entries))
		}
	})
}
