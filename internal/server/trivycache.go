package server

import (
	"log/slog"
	"os"
	"path/filepath"
)

// seedTrivyCache copies into the trivy cache dir the databases baked into
// the image at build time (see Dockerfile) that the cache does not have yet:
// the vulnerability DB always, the Java DB when the image was built with
// TRIVY_JAVA_DB=true. This makes the first scan work with no network. Each
// top-level entry (db/, java-db/) is seeded on its own, so a cache that
// already holds a vulnerability DB — a PVC from an earlier version — still
// gets a Java DB shipped later; what the cache has is never overwritten.
func seedTrivyCache(cacheDir, seedDir string) {
	if seedDir == "" {
		return
	}
	entries, err := os.ReadDir(seedDir)
	if err != nil {
		return // no baked DB to seed from (e.g. running outside the image)
	}
	for _, e := range entries {
		dst := filepath.Join(cacheDir, e.Name())
		if _, err := os.Lstat(dst); err == nil || !os.IsNotExist(err) {
			continue // already there (or unreadable): leave it alone
		}
		src := filepath.Join(seedDir, e.Name())
		if e.IsDir() {
			err = os.CopyFS(dst, os.DirFS(src))
		} else {
			err = copyFile(src, dst)
		}
		if err != nil {
			slog.Warn("scan: could not seed the trivy cache from the image", "entry", e.Name(), "err", err)
			_ = os.RemoveAll(dst) // no half-copied DB for trivy to trip on
			continue
		}
		slog.Info("scan: seeded the trivy cache from the image", "entry", e.Name(), "to", cacheDir)
	}
}

func copyFile(src, dst string) error {
	raw, err := os.ReadFile(src)
	if err != nil {
		return err
	}
	return os.WriteFile(dst, raw, 0o600)
}
