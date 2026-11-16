package storage

import (
	"errors"
	"time"
)

// GCBackend is what garbage collection needs from a backend. Both
// LocalBackend and S3Backend implement it; it is not part of Backend because
// proxy mode has no storage to collect.
type GCBackend interface {
	AllBlobs() ([]string, error)
	BlobSize(digest string) (int64, error)
	ReferencedBlobs() (map[string]struct{}, error)
	RemoveBlob(digest string) error
}

// blobAger is optionally implemented by backends that can tell when a blob
// was written, enabling the grace period.
type blobAger interface {
	BlobModTime(digest string) (time.Time, error)
}

// UploadPurger is optionally implemented by backends that can drop abandoned
// upload sessions.
type UploadPurger interface {
	PurgeStaleUploads(olderThan time.Duration) (int, error)
}

type GCOptions struct {
	// DryRun reports what would be removed without removing anything.
	DryRun bool
	// GracePeriod protects blobs written more recently than this. A push
	// uploads its layers first and its manifest last; without a grace period
	// a collection running in between sees those layers as unreferenced,
	// deletes them, and the push still "succeeds" — leaving a tag that points
	// at missing layers. Zero disables the protection (tests, forced runs).
	GracePeriod time.Duration
}

type GCResult struct {
	Removed       []string // digests removed (or, on a dry run, removable)
	FreedBytes    int64
	ScannedBlobs  int // total blobs enumerated
	SkippedRecent int // unreferenced but younger than the grace period
	Failed        int // unreferenced blobs that could not be removed
}

// ErrGCUnsupported is returned when the backend cannot be collected.
var ErrGCUnsupported = errors.New("garbage collection is not supported by this storage backend")

// CollectGarbage removes every blob no manifest references. It is the single
// implementation behind both the scheduled run and POST /api/admin/gc.
//
// Blobs are listed *before* references are computed: a blob pushed while the
// collection runs is then simply absent from the candidate list, instead of
// being listed-but-not-yet-referenced. Any error while computing references
// aborts the run — deleting on incomplete information is how images get lost.
func CollectGarbage(backend any, opts GCOptions) (GCResult, error) {
	gc, ok := backend.(GCBackend)
	if !ok {
		return GCResult{}, ErrGCUnsupported
	}
	all, err := gc.AllBlobs()
	if err != nil {
		return GCResult{}, err
	}
	referenced, err := gc.ReferencedBlobs()
	if err != nil {
		return GCResult{}, err
	}

	ager, _ := backend.(blobAger)
	var cutoff time.Time
	if ager != nil && opts.GracePeriod > 0 {
		cutoff = time.Now().Add(-opts.GracePeriod)
	}

	res := GCResult{ScannedBlobs: len(all), Removed: []string{}}
	for _, digest := range all {
		if _, ok := referenced[digest]; ok {
			continue
		}
		if !cutoff.IsZero() {
			// An unknown age is treated as recent: when in doubt, keep.
			if modified, err := ager.BlobModTime(digest); err != nil || modified.After(cutoff) {
				res.SkippedRecent++
				continue
			}
		}
		size, _ := gc.BlobSize(digest)
		if !opts.DryRun {
			if err := gc.RemoveBlob(digest); err != nil {
				res.Failed++
				continue
			}
		}
		res.FreedBytes += size
		res.Removed = append(res.Removed, digest)
	}
	return res, nil
}
