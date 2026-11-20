package scan

import (
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"sync/atomic"
	"time"

	"dockyard/internal/auth"
	"dockyard/internal/events"
	"dockyard/internal/reference"
	"dockyard/internal/store"
)

// PolicyState holds the vulnerability policy in memory: the pull gate reads
// it on every manifest GET, so it must not cost a query. The admin handler
// writes through it, which keeps the copy current.
type PolicyState struct {
	store   *store.Store
	current atomic.Pointer[store.VulnerabilityPolicy]
}

func NewPolicyState(st *store.Store) (*PolicyState, error) {
	p, err := st.GetVulnerabilityPolicy()
	if err != nil {
		return nil, err
	}
	s := &PolicyState{store: st}
	s.current.Store(p)
	return s, nil
}

func (s *PolicyState) Get() store.VulnerabilityPolicy { return *s.current.Load() }

func (s *PolicyState) Update(scanOnPush bool, blockSeverity, by string) (*store.VulnerabilityPolicy, error) {
	p, err := s.store.UpdateVulnerabilityPolicy(scanOnPush, blockSeverity, by)
	if err != nil {
		return nil, err
	}
	s.current.Store(p)
	return p, nil
}

// Blockers returns what stops digest from being pulled under the current
// policy: the vulnerabilities at or above the threshold that are not
// allowlisted, from the latest successful scan. Unscanned images pass —
// blocking every fresh push until its scan finishes would break CI.
func (s *PolicyState) Blockers(digest string) ([]store.ScanVulnerability, error) {
	severities := store.SeveritiesAtOrAbove(s.Get().BlockSeverity)
	if len(severities) == 0 || digest == "" {
		return nil, nil
	}
	sc, err := s.store.LatestScanForDigest(digest)
	if errors.Is(err, store.ErrScanNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return s.store.BlockingVulnerabilities(sc.ID, severities, time.Now())
}

// Blocks is the yes/no form, for listings.
func (s *PolicyState) Blocks(digest string) bool {
	blockers, err := s.Blockers(digest)
	return err == nil && len(blockers) > 0
}

// ── Pull gate ────────────────────────────────────────────────────────────────

// Gate refuses manifest GETs of images the policy blocks. Blobs are not
// checked: without the manifest a client cannot know which blobs to fetch,
// and checking each layer would cost a lookup per layer for nothing.
type Gate struct {
	policy   *PolicyState
	resolver manifestResolver
	audit    auditSink
}

type auditSink interface {
	Record(actor, action, repo, tag, ip, result, details string)
	ClientIP(r *http.Request) string
}

func NewGate(policy *PolicyState, resolver manifestResolver, audit auditSink) *Gate {
	return &Gate{policy: policy, resolver: resolver, audit: audit}
}

func (g *Gate) Middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || g.policy.Get().BlockSeverity == "" {
			next.ServeHTTP(w, r)
			return
		}
		route := reference.ParseV2Path(r.URL.Path)
		// The scanner must be able to re-scan a blocked image, or fixing
		// the allowlist would be the only way to ever unblock it.
		if route.Route != reference.RouteManifest || auth.IsScannerRequest(r) {
			next.ServeHTTP(w, r)
			return
		}
		digest := route.Ref
		if !reference.IsDigest(digest) {
			_, resolved, err := g.resolver.GetManifest(route.Name, route.Ref)
			if err != nil {
				// Unknown tag: let the handler answer MANIFEST_UNKNOWN.
				next.ServeHTTP(w, r)
				return
			}
			digest = resolved
		}
		blockers, err := g.policy.Blockers(digest)
		if err != nil {
			// Fail open on a database error: an outage of the policy store
			// must not take every deployment down with it.
			slog.Error("vulnerability gate: lookup failed, allowing pull", "repo", route.Name, "digest", digest, "err", err)
			next.ServeHTTP(w, r)
			return
		}
		if len(blockers) == 0 {
			next.ServeHTTP(w, r)
			return
		}
		message := BlockedMessage(route.Name, route.Ref, g.policy.Get().BlockSeverity, blockers)
		if g.audit != nil {
			actor := "anonymous"
			if p, ok := auth.PrincipalFromRequest(r); ok {
				actor = p.Username
			}
			tag := route.Ref
			if reference.IsDigest(tag) {
				tag = ""
			}
			g.audit.Record(actor, "pull-blocked", route.Name, tag, g.audit.ClientIP(r), "403", summarize(blockers))
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusForbidden)
		_, _ = fmt.Fprintf(w, `{"errors":[{"code":"DENIED","message":%q}]}`, message)
	})
}

// BlockedMessage is what docker pull prints: which image, why, and what to do.
func BlockedMessage(name, ref, threshold string, blockers []store.ScanVulnerability) string {
	sep := ":"
	if reference.IsDigest(ref) {
		sep = "@"
	}
	return fmt.Sprintf("%s%s%s is blocked by the vulnerability policy (%s and above): %s. "+
		"Rebuild the image with fixed packages, or ask an administrator to allowlist the CVEs if the risk is accepted.",
		name, sep, ref, strings.ToLower(threshold), summarize(blockers))
}

// summarize turns blockers into "2 critical, 1 high (CVE-a, CVE-b, CVE-c)",
// listing at most five IDs.
func summarize(blockers []store.ScanVulnerability) string {
	counts := map[string]int{}
	for _, b := range blockers {
		counts[b.Severity]++
	}
	var parts []string
	for _, sev := range store.Severities {
		if n := counts[sev]; n > 0 {
			parts = append(parts, fmt.Sprintf("%d %s", n, strings.ToLower(sev)))
		}
	}
	ids := make([]string, 0, 5)
	for i, b := range blockers {
		if i == 5 {
			ids = append(ids, fmt.Sprintf("+%d more", len(blockers)-5))
			break
		}
		ids = append(ids, b.ID)
	}
	return strings.Join(parts, ", ") + " (" + strings.Join(ids, ", ") + ")"
}

// ── Scan on push ─────────────────────────────────────────────────────────────

// RequestedOnPush is the requested_by of scans queued by a push.
const RequestedOnPush = "on-push"

// ScanOnPush queues a scan for every pushed tag while the policy asks for
// it. Events are handed to a worker through a large buffer: the hub drops
// events for subscribers that fall behind, and resolving a digest on S3 is
// slower than a burst of pushes.
func ScanOnPush(hub *events.Hub, policy *PolicyState, resolver manifestResolver, d *Dispatcher) {
	sub := hub.Subscribe()
	queue := make(chan events.Event, 1024)
	go func() {
		for e := range sub {
			if e.Type != "push" || e.Tag == "" || !policy.Get().ScanOnPush {
				continue
			}
			select {
			case queue <- e:
			default:
				slog.Warn("scan on push: queue full, skipping", "repo", e.Name, "tag", e.Tag)
			}
		}
	}()
	go func() {
		for e := range queue {
			_, digest, err := resolver.GetManifest(e.Name, e.Tag)
			if err != nil {
				slog.Warn("scan on push: cannot resolve pushed tag", "repo", e.Name, "tag", e.Tag, "err", err)
				continue
			}
			if _, err := d.Enqueue(e.Name, e.Tag, digest, RequestedOnPush); err != nil {
				slog.Error("scan on push: enqueue failed", "repo", e.Name, "tag", e.Tag, "err", err)
			}
		}
	}()
}

// BackfillVulnerabilities fills the vulnerability rows of scans that finished
// before they were recorded, from their stored reports, so the pull gate and
// the allowlist apply to them too.
func BackfillVulnerabilities(st *store.Store) {
	for {
		ids, err := st.ScansMissingVulnerabilities(50)
		if err != nil || len(ids) == 0 {
			if err != nil {
				slog.Warn("scan: vulnerability backfill stopped", "err", err)
			}
			return
		}
		progressed := false
		for _, id := range ids {
			report, err := st.ScanReport(id)
			if err != nil {
				continue
			}
			vulns, err := distinctVulnerabilities(report)
			if err != nil || len(vulns) == 0 {
				continue
			}
			if err := st.RecordScanVulnerabilities(id, vulns); err == nil {
				progressed = true
			}
		}
		if !progressed {
			// What is left cannot be backfilled (no report, unparsable).
			return
		}
	}
}
