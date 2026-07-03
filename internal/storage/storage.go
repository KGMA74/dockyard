package storage

import (
	"io"
	"time"
)

// Backend is the common interface for all storage backends.
type Backend interface {
	// Blobs
	PutBlob(digest string, content io.Reader, size int64) error
	GetBlob(digest string) (io.ReadCloser, int64, error)
	BlobExists(digest string) (bool, error)
	DeleteBlob(digest string) error

	// Chunked uploads
	InitUpload(uuid string) error
	AppendUpload(uuid string, content io.Reader) error
	CommitUpload(uuid, digest string) error
	DeleteUpload(uuid string) error
	GetUploadSize(uuid string) (int64, error)

	// Manifests
	PutManifest(name, reference, digest string, content []byte) error
	GetManifest(name, reference string) ([]byte, string, error)
	DeleteManifest(name, digest string) error
	ManifestExists(name, reference string) (bool, error)

	// Catalog
	ListRepositories() ([]string, error)
	ListTags(name string) ([]string, error)
	DeleteRepository(name string) error

	// TagPushedAt returns when a tag was last (re)pointed at a manifest — i.e.
	// the last time `docker push` wrote this tag, used to sort/display recency.
	TagPushedAt(name, tag string) (time.Time, error)

	// Stats
	Stats() (StorageStats, error)
}

type StorageStats struct {
	TotalSize int64
	BlobCount int
	RepoCount int
}
