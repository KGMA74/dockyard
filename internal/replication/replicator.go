// Package replication copies pushed tags to other Dockyard (or any V2)
// instances: push-based, triggered by the same in-process event hub as
// webhooks, delivered through a SQLite outbox with retry/backoff so a
// target being briefly unreachable doesn't lose the copy.
package replication

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"path"
	"time"

	"dockyard/internal/events"
	"dockyard/internal/registry"
	"dockyard/internal/storage"
	"dockyard/internal/store"
)

const (
	maxAttempts = 8
	baseBackoff = 30 * time.Second

	mediaTypeManifestList = "application/vnd.docker.distribution.manifest.list.v2+json"
	mediaTypeOCIIndex     = "application/vnd.oci.image.index.v1+json"

	// maxManifestDepth bounds nested manifest-list resolution — see the
	// identical guard (and the recursion bug it fixes) in
	// internal/admin/manifest.go.
	maxManifestDepth = 8
)

type manifestProbe struct {
	MediaType string `json:"mediaType"`
	Config    *struct {
		Digest string `json:"digest"`
		Size   int64  `json:"size"`
	} `json:"config,omitempty"`
	Layers []struct {
		Digest string `json:"digest"`
		Size   int64  `json:"size"`
	} `json:"layers,omitempty"`
	Manifests []struct {
		Digest string `json:"digest"`
	} `json:"manifests,omitempty"`
}

// Replicator drains the replication outbox against local storage — embedded
// or mirror mode only, since it needs direct read access to blobs/manifests
// (proxy mode doesn't store content locally).
type Replicator struct {
	store    *store.Store
	backend  storage.Backend
	wake     chan struct{}
	interval time.Duration
}

func NewReplicator(st *store.Store, backend storage.Backend) *Replicator {
	r := &Replicator{
		store:    st,
		backend:  backend,
		wake:     make(chan struct{}, 1),
		interval: 15 * time.Second,
	}
	go r.loop()
	return r
}

// Subscribe wires the replicator to the in-process event hub. Only tag
// pushes are replicated — pushes by digest (multi-arch platform manifests,
// cosign signature/attestation objects) are picked up as a side effect of
// replicating the tag that references them.
func (r *Replicator) Subscribe(hub *events.Hub) {
	ch := hub.Subscribe()
	go func() {
		for ev := range ch {
			if ev.Type != "push" || ev.Tag == "" {
				continue
			}
			r.Enqueue(ev.Name, ev.Tag)
		}
	}()
}

func (r *Replicator) Enqueue(repo, tag string) {
	targets, err := r.store.ListReplicationTargets()
	if err != nil {
		slog.Error("replication: list targets failed", "err", err)
		return
	}
	queued := false
	for _, t := range targets {
		if !t.Enabled || !matchesPattern(t.RepoPattern, repo) {
			continue
		}
		if err := r.store.EnqueueReplication(t.ID, repo, tag); err == nil {
			queued = true
		}
	}
	if queued {
		select {
		case r.wake <- struct{}{}:
		default:
		}
	}
}

func matchesPattern(pattern, repo string) bool {
	if pattern == "" || pattern == "*" {
		return true
	}
	ok, err := path.Match(pattern, repo)
	return err == nil && ok
}

// ReplicateNow copies one repo/tag to one target synchronously — used by the
// admin "test" action so the operator sees the outcome immediately, bypassing
// the outbox.
func (r *Replicator) ReplicateNow(target *store.ReplicationTarget, repo, tag string) error {
	client := registry.NewClient(target.BaseURL, target.Username, target.Password)
	return r.replicate(client, repo, tag)
}

func (r *Replicator) loop() {
	ticker := time.NewTicker(r.interval)
	defer ticker.Stop()
	for {
		select {
		case <-r.wake:
		case <-ticker.C:
		}
		r.drain()
	}
}

func (r *Replicator) drain() {
	due, err := r.store.DueReplications(maxAttempts, 32)
	if err != nil {
		slog.Error("replication: outbox query failed", "err", err)
		return
	}
	for _, d := range due {
		target, err := r.store.ReplicationTargetByID(d.TargetID)
		if err != nil || !target.Enabled {
			// Target gone or disabled — retire the delivery, nothing to retry for.
			_ = r.store.MarkReplicationDelivered(d.ID)
			continue
		}
		client := registry.NewClient(target.BaseURL, target.Username, target.Password)
		if err := r.replicate(client, d.Repo, d.Tag); err != nil {
			backoff := baseBackoff * (1 << min(d.Attempts, 6)) // 30s → 32m cap
			_ = r.store.MarkReplicationFailed(d.ID, time.Now().Add(backoff), err.Error())
			slog.Warn("replication: delivery failed", "target", target.Name, "repo", d.Repo, "tag", d.Tag, "attempt", d.Attempts+1, "err", err)
			continue
		}
		_ = r.store.MarkReplicationDelivered(d.ID)
	}
}

func (r *Replicator) replicate(client *registry.Client, repo, tag string) error {
	return r.copyManifest(client, repo, tag, map[string]bool{}, 0, true)
}

// copyManifest resolves repo/ref from local storage, replicates whatever it
// references (child manifests for a list, blobs for a single manifest), then
// pushes the manifest itself — by digest always, and additionally under the
// original ref when that ref is a tag (isTopLevel), so the target resolves
// the tag directly instead of only having orphaned digest-addressed content.
func (r *Replicator) copyManifest(client *registry.Client, repo, ref string, seen map[string]bool, depth int, isTopLevel bool) error {
	if depth >= maxManifestDepth {
		return errors.New("manifest nesting too deep")
	}
	raw, digest, err := r.backend.GetManifest(repo, ref)
	if err != nil {
		return fmt.Errorf("read local manifest %s: %w", ref, err)
	}
	if seen[digest] {
		return fmt.Errorf("manifest reference cycle detected at %s", digest)
	}

	var probe manifestProbe
	if err := json.Unmarshal(raw, &probe); err != nil {
		return fmt.Errorf("parse manifest %s: %w", digest, err)
	}

	if probe.MediaType == mediaTypeManifestList || probe.MediaType == mediaTypeOCIIndex || len(probe.Manifests) > 0 {
		branchSeen := make(map[string]bool, len(seen)+1)
		for d := range seen {
			branchSeen[d] = true
		}
		branchSeen[digest] = true
		for _, m := range probe.Manifests {
			if err := r.copyManifest(client, repo, m.Digest, branchSeen, depth+1, false); err != nil {
				return fmt.Errorf("replicate child manifest %s: %w", m.Digest, err)
			}
		}
	} else {
		if probe.Config != nil && probe.Config.Digest != "" {
			if err := r.copyBlob(client, repo, probe.Config.Digest, probe.Config.Size); err != nil {
				return fmt.Errorf("replicate config blob: %w", err)
			}
		}
		for _, l := range probe.Layers {
			if err := r.copyBlob(client, repo, l.Digest, l.Size); err != nil {
				return fmt.Errorf("replicate layer %s: %w", l.Digest, err)
			}
		}
	}

	if err := client.PushManifest(repo, digest, probe.MediaType, raw); err != nil {
		return fmt.Errorf("push manifest %s: %w", digest, err)
	}
	if isTopLevel && ref != digest {
		if err := client.PushManifest(repo, ref, probe.MediaType, raw); err != nil {
			return fmt.Errorf("push tag %s: %w", ref, err)
		}
	}
	return nil
}

func (r *Replicator) copyBlob(client *registry.Client, repo, digest string, size int64) error {
	return client.PushBlob(repo, digest, size, func() (io.ReadCloser, error) {
		rc, _, err := r.backend.GetBlob(digest)
		return rc, err
	})
}
