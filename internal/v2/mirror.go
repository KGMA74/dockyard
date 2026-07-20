package v2

import (
	"crypto/sha256"
	"encoding/hex"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"time"

	"dockyard/internal/cosign"
	"dockyard/internal/events"
	"dockyard/internal/registry"
	"dockyard/internal/storage"
	"dockyard/internal/store"
)

// Mirror is a pull-through cache: reads are served from local storage and
// fetched from the configured upstream on miss (write-through). Writes behave
// exactly like the embedded registry — the cache is also a valid push target.
//
// Blobs are content-addressed, hence immutable: once cached they are never
// re-fetched. Tags are mutable upstream, so a tag→manifest resolution is
// revalidated when older than the TTL; if the upstream is unreachable the
// cached (stale) version keeps being served.
type Mirror struct {
	inner  *Handler
	store  storage.Backend
	client *registry.Client
	hub    *events.Hub
	ttl    time.Duration

	mu         sync.Mutex
	tagChecked map[string]time.Time // "name:tag" → last successful upstream check
	hits       uint64
	misses     uint64
}

func NewMirror(backend storage.Backend, hub *events.Hub, client *registry.Client, ttl time.Duration, signing *cosign.Policy, db *store.Store) *Mirror {
	return &Mirror{
		inner:      New(backend, hub, signing, db),
		store:      backend,
		client:     client,
		hub:        hub,
		ttl:        ttl,
		tagChecked: make(map[string]time.Time),
	}
}

// OnPull delegates to the embedded handler (pulls served from cache count too).
func (m *Mirror) OnPull(fn func(name, reference string)) { m.inner.OnPull(fn) }

// Stats reports cache hits and misses since boot.
func (m *Mirror) Stats() (hits, misses uint64) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.hits, m.misses
}

func (m *Mirror) countHit(hit bool) {
	m.mu.Lock()
	if hit {
		m.hits++
	} else {
		m.misses++
	}
	m.mu.Unlock()
}

func (m *Mirror) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodGet || r.Method == http.MethodHead {
		switch {
		case reManifests.MatchString(r.URL.Path):
			sub := reManifests.FindStringSubmatch(r.URL.Path)
			if err := m.ensureManifest(sub[1], sub[2]); err != nil {
				registryError(w, http.StatusNotFound, "MANIFEST_UNKNOWN", err.Error())
				return
			}
		case reBlobGet.MatchString(r.URL.Path):
			sub := reBlobGet.FindStringSubmatch(r.URL.Path)
			if err := m.ensureBlob(sub[1], sub[2]); err != nil {
				registryError(w, http.StatusNotFound, "BLOB_UNKNOWN", err.Error())
				return
			}
		}
	}
	m.inner.ServeHTTP(w, r)
}

// ensureManifest makes name:ref locally available, revalidating tags older
// than the TTL against the upstream.
func (m *Mirror) ensureManifest(name, ref string) error {
	byDigest := strings.HasPrefix(ref, "sha256:")
	exists, _ := m.store.ManifestExists(name, ref)

	if byDigest {
		if exists {
			m.countHit(true)
			return nil
		}
		return m.fetchManifest(name, ref)
	}

	m.mu.Lock()
	fresh := time.Since(m.tagChecked[name+":"+ref]) < m.ttl
	m.mu.Unlock()
	if exists && fresh {
		m.countHit(true)
		return nil
	}
	if err := m.fetchManifest(name, ref); err != nil {
		if exists {
			// Upstream unreachable — serve the cached version.
			slog.Warn("mirror: upstream unavailable, serving stale tag", "name", name, "tag", ref, "err", err)
			m.countHit(true)
			return nil
		}
		return err
	}
	return nil
}

func (m *Mirror) fetchManifest(name, ref string) error {
	raw, digest, err := m.client.RawManifest(name, ref)
	if err != nil {
		m.countHit(false)
		return err
	}
	if digest == "" {
		h := sha256.Sum256(raw)
		digest = "sha256:" + hex.EncodeToString(h[:])
	}

	// Skip the write when the tag still points at what we already have.
	changed := true
	if existing, existingDigest, err := m.store.GetManifest(name, ref); err == nil &&
		existingDigest == digest && len(existing) == len(raw) {
		changed = false
	}
	if changed {
		if err := m.store.PutManifest(name, ref, digest, raw); err != nil {
			m.countHit(false)
			return err
		}
	}

	byDigest := strings.HasPrefix(ref, "sha256:")
	if !byDigest {
		m.mu.Lock()
		m.tagChecked[name+":"+ref] = time.Now()
		m.mu.Unlock()
	}
	m.countHit(false)
	slog.Info("mirror: cached manifest", "name", name, "ref", ref, "digest", digest, "changed", changed)
	if changed && !byDigest && m.hub != nil {
		m.hub.Publish(events.Event{Type: "push", Name: name, Tag: ref})
	}
	return nil
}

// ensureBlob makes the blob locally available; blobs are immutable so a local
// hit never goes upstream.
func (m *Mirror) ensureBlob(name, digest string) error {
	if ok, _ := m.store.BlobExists(digest); ok {
		m.countHit(true)
		return nil
	}
	rc, err := m.client.BlobStream(name, digest)
	if err != nil {
		m.countHit(false)
		return err
	}
	defer func() { _ = rc.Close() }()
	// PutBlob hash-verifies against the digest, so a corrupted upstream
	// response can never enter the cache.
	if err := m.store.PutBlob(digest, rc, -1); err != nil {
		m.countHit(false)
		return err
	}
	m.countHit(false)
	slog.Info("mirror: cached blob", "name", name, "digest", digest)
	return nil
}
