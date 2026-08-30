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

	t.Run("leaves a non-empty cache dir untouched", func(t *testing.T) {
		cache := t.TempDir()
		if err := os.WriteFile(filepath.Join(cache, "existing"), []byte("keep"), 0o644); err != nil {
			t.Fatal(err)
		}
		seedTrivyCache(cache, seed)
		if _, err := os.Stat(filepath.Join(cache, "db")); !os.IsNotExist(err) {
			t.Fatalf("cache dir with existing content should not be seeded")
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
