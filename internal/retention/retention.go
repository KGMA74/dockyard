// Package retention evaluates tag-cleanup policies and applies the resulting
// deletion plan. Evaluation is pure (dry-run by nature); Apply deletes
// manifests through the storage backend — the following GC then reclaims the
// orphaned blobs.
package retention

import (
	"fmt"
	"log/slog"
	"slices"
	"sort"
	"time"

	"dockyard/internal/auth"
	"dockyard/internal/events"
	"dockyard/internal/storage"
	"dockyard/internal/store"
)

type Engine struct {
	store   *store.Store
	backend storage.Backend
	hub     *events.Hub
}

func New(st *store.Store, backend storage.Backend) *Engine {
	return &Engine{store: st, backend: backend}
}

// SetHub makes Apply publish a "retention" event per deleted tag (webhooks,
// SSE). Optional.
func (e *Engine) SetHub(hub *events.Hub) { e.hub = hub }

// Candidate is one tag the plan wants gone, with the digest its deletion
// would remove.
type Candidate struct {
	Repo   string `json:"repo"`
	Tag    string `json:"tag"`
	Digest string `json:"digest"`
	Reason string `json:"reason"`
}

// Skipped is a tag condemned by policy but not deletable, e.g. because its
// manifest digest is shared with a tag that must be kept.
type Skipped struct {
	Repo   string `json:"repo"`
	Tag    string `json:"tag"`
	Reason string `json:"reason"`
}

type Plan struct {
	Delete  []Candidate `json:"delete"`
	Skipped []Skipped   `json:"skipped"`
}

// Evaluate builds the deletion plan for all enabled policies. It never
// deletes anything.
func (e *Engine) Evaluate(now time.Time) (*Plan, error) {
	policies, err := e.store.ListRetentionPolicies()
	if err != nil {
		return nil, err
	}
	repos, err := e.backend.ListRepositories()
	if err != nil {
		return nil, err
	}

	plan := &Plan{Delete: []Candidate{}, Skipped: []Skipped{}}
	for _, repo := range repos {
		policy := firstMatching(policies, repo)
		if policy == nil {
			continue
		}
		if err := e.evaluateRepo(plan, policy, repo, now); err != nil {
			return nil, fmt.Errorf("repo %s: %w", repo, err)
		}
	}
	return plan, nil
}

// firstMatching returns the first enabled policy covering repo (policies are
// ordered by creation; the earliest match wins).
func firstMatching(policies []*store.RetentionPolicy, repo string) *store.RetentionPolicy {
	for _, p := range policies {
		if p.Enabled && auth.MatchesRepo([]string{p.RepoPattern}, repo) {
			return p
		}
	}
	return nil
}

type tagInfo struct {
	tag      string
	digest   string
	pushedAt time.Time
}

func (e *Engine) evaluateRepo(plan *Plan, policy *store.RetentionPolicy, repo string, now time.Time) error {
	tags, err := e.backend.ListTags(repo)
	if err != nil {
		return err
	}

	var all []tagInfo
	condemned := map[string]string{} // tag → reason
	for _, tag := range tags {
		_, digest, err := e.backend.GetManifest(repo, tag)
		if err != nil {
			continue
		}
		pushedAt, _ := e.backend.TagPushedAt(repo, tag)
		all = append(all, tagInfo{tag: tag, digest: digest, pushedAt: pushedAt})
	}

	isKept := func(tag string) bool {
		if slices.Contains(policy.ProtectedTags, tag) {
			return true
		}
		// KeepPatterns are tag globs; MatchesRepo's wildcard match works on
		// any string, but an empty pattern list must mean "keeps nothing"
		// here — the opposite of its repo-authorization default.
		if len(policy.KeepPatterns) == 0 {
			return false
		}
		return auth.MatchesRepo(policy.KeepPatterns, tag)
	}

	// Age rule: unpulled (nor pushed) for more than UnpulledDays.
	if policy.UnpulledDays > 0 {
		cutoff := now.Add(-time.Duration(policy.UnpulledDays) * 24 * time.Hour)
		for _, ti := range all {
			if isKept(ti.tag) {
				continue
			}
			lastActivity := ti.pushedAt
			if at, _, ok := e.store.LastPull(repo, ti.tag); ok && at.After(lastActivity) {
				lastActivity = at
			}
			if !lastActivity.IsZero() && lastActivity.Before(cutoff) {
				condemned[ti.tag] = fmt.Sprintf("no pull or push for %d+ days", policy.UnpulledDays)
			}
		}
	}

	// Keep-N rule: keep the N most recently pushed non-kept tags.
	if policy.KeepN > 0 {
		var eligible []tagInfo
		for _, ti := range all {
			if !isKept(ti.tag) {
				eligible = append(eligible, ti)
			}
		}
		sort.Slice(eligible, func(i, j int) bool { return eligible[i].pushedAt.After(eligible[j].pushedAt) })
		for _, ti := range eligible[min(policy.KeepN, len(eligible)):] {
			if _, already := condemned[ti.tag]; !already {
				condemned[ti.tag] = fmt.Sprintf("beyond the %d most recent tags", policy.KeepN)
			}
		}
	}

	// Deleting a manifest removes every tag pointing at its digest, so a
	// digest is only deletable when ALL of its tags are condemned.
	tagsByDigest := map[string][]tagInfo{}
	for _, ti := range all {
		tagsByDigest[ti.digest] = append(tagsByDigest[ti.digest], ti)
	}
	for _, ti := range all {
		reason, isCondemned := condemned[ti.tag]
		if !isCondemned {
			continue
		}
		shared := false
		for _, sibling := range tagsByDigest[ti.digest] {
			if _, siblingCondemned := condemned[sibling.tag]; !siblingCondemned {
				shared = true
				plan.Skipped = append(plan.Skipped, Skipped{
					Repo: repo, Tag: ti.tag,
					Reason: fmt.Sprintf("digest shared with retained tag %q", sibling.tag),
				})
				break
			}
		}
		if !shared {
			plan.Delete = append(plan.Delete, Candidate{Repo: repo, Tag: ti.tag, Digest: ti.digest, Reason: reason})
		}
	}
	return nil
}

// Apply executes the plan. Deleting one digest removes all its (condemned)
// tags at once, so duplicates are coalesced.
func (e *Engine) Apply(plan *Plan) (deleted int, err error) {
	done := map[string]bool{} // repo\x00digest
	for _, c := range plan.Delete {
		key := c.Repo + "\x00" + c.Digest
		if done[key] {
			continue
		}
		if err := e.backend.DeleteManifest(c.Repo, c.Digest); err != nil {
			slog.Warn("retention: delete failed", "repo", c.Repo, "digest", c.Digest, "err", err)
			continue
		}
		done[key] = true
		deleted++
		if e.hub != nil {
			e.hub.Publish(events.Event{Type: "retention", Name: c.Repo, Tag: c.Tag, Actor: "retention-policy"})
		}
		slog.Info("retention: deleted", "repo", c.Repo, "tag", c.Tag, "digest", c.Digest, "reason", c.Reason)
	}
	return deleted, nil
}

// Run evaluates and applies in one step — used by the daily scheduler.
func (e *Engine) Run() {
	plan, err := e.Evaluate(time.Now())
	if err != nil {
		slog.Error("retention: evaluation failed", "err", err)
		return
	}
	if len(plan.Delete) == 0 {
		return
	}
	deleted, _ := e.Apply(plan)
	slog.Info("retention: run complete", "deleted_manifests", deleted, "skipped", len(plan.Skipped))
}
