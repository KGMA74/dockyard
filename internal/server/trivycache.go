package server

import (
	"log/slog"
	"os"
)

// seedTrivyCache populates an empty trivy cache dir from a DB baked into the
// image at build time (see Dockerfile). This makes the first scan work with no
// network and without waiting on a ~50 MB download. A non-empty cache dir (a
// mounted PVC that already has a DB, or a previous run) is left untouched.
func seedTrivyCache(cacheDir, seedDir string) {
	if seedDir == "" {
		return
	}
	if entries, err := os.ReadDir(cacheDir); err != nil || len(entries) > 0 {
		return // cache dir unreadable or already populated
	}
	if entries, err := os.ReadDir(seedDir); err != nil || len(entries) == 0 {
		return // no baked DB to seed from (e.g. running outside the image)
	}
	if err := os.CopyFS(cacheDir, os.DirFS(seedDir)); err != nil {
		slog.Warn("scan: could not seed trivy cache from baked DB", "seed", seedDir, "err", err)
		return
	}
	slog.Info("scan: seeded trivy vulnerability DB from the image", "from", seedDir, "to", cacheDir)
}
