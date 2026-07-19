package cosign

import (
	"io"

	"dockyard/internal/registry"
	"dockyard/internal/storage"
)

// Fetcher is the minimal read surface Policy needs to locate and read a
// cosign signature manifest — satisfied by both storage modes via the
// adapters below, so the same verification logic works in embedded/mirror
// (BackendFetcher) and proxy mode (ClientFetcher).
type Fetcher interface {
	GetManifest(name, reference string) ([]byte, string, error)
	GetBlob(name, digest string) ([]byte, error)
}

// BackendFetcher adapts storage.Backend (embedded/mirror mode).
type BackendFetcher struct{ Backend storage.Backend }

func (f BackendFetcher) GetManifest(name, reference string) ([]byte, string, error) {
	return f.Backend.GetManifest(name, reference)
}

func (f BackendFetcher) GetBlob(_, digest string) ([]byte, error) {
	rc, _, err := f.Backend.GetBlob(digest)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rc.Close() }()
	return io.ReadAll(rc)
}

// ClientFetcher adapts registry.Client (proxy mode).
type ClientFetcher struct{ Client *registry.Client }

func (f ClientFetcher) GetManifest(name, reference string) ([]byte, string, error) {
	return f.Client.RawManifest(name, reference)
}

func (f ClientFetcher) GetBlob(name, digest string) ([]byte, error) {
	return f.Client.Blob(name, digest)
}
