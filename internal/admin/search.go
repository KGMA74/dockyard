package admin

import (
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"

	"dockyard/internal/cosign"
	"dockyard/internal/store"

	"github.com/labstack/echo/v4"
)

// SearchResult is one repo:tag match. Signed and Scan are only populated
// when cosign keys / a scan history exist for that digest — an absent
// field means "unknown", not "false".
type SearchResult struct {
	Name     string          `json:"name"`
	Tag      string          `json:"tag"`
	Digest   string          `json:"digest"`
	PushedAt string          `json:"pushed_at,omitempty"`
	Signed   *bool           `json:"signed,omitempty"`
	Scan     *SearchScanInfo `json:"scan,omitempty"`
}

type SearchScanInfo struct {
	Status        string `json:"status"`
	CriticalCount int    `json:"critical_count"`
	HighCount     int    `json:"high_count"`
}

func parseSearchParams(c echo.Context) (q string, signedFilter *bool, limit, offset int) {
	q = strings.ToLower(strings.TrimSpace(c.QueryParam("q")))
	if v := c.QueryParam("signed"); v == "true" || v == "false" {
		b := v == "true"
		signedFilter = &b
	}
	limit, _ = strconv.Atoi(c.QueryParam("limit"))
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	offset, _ = strconv.Atoi(c.QueryParam("offset"))
	if offset < 0 {
		offset = 0
	}
	return q, signedFilter, limit, offset
}

// sortAndPage orders matches by name then tag for stable pagination and
// slices out the requested page.
func sortAndPage(results []SearchResult, limit, offset int) (page []SearchResult, total int) {
	sort.Slice(results, func(i, j int) bool {
		if results[i].Name != results[j].Name {
			return results[i].Name < results[j].Name
		}
		return results[i].Tag < results[j].Tag
	})
	total = len(results)
	if offset > total {
		offset = total
	}
	end := offset + limit
	if end > total {
		end = total
	}
	return results[offset:end], total
}

func scanInfoForDigest(db *store.Store, digest string) *SearchScanInfo {
	if db == nil {
		return nil
	}
	sc, err := db.LatestScanForDigest(digest)
	if err != nil {
		return nil
	}
	return &SearchScanInfo{Status: sc.Status, CriticalCount: sc.CriticalCount, HighCount: sc.HighCount}
}

func searchResponse(page []SearchResult, total int) map[string]any {
	if page == nil {
		page = []SearchResult{}
	}
	return map[string]any{"items": page, "total": total, "count": len(page)}
}

// GET /api/admin/repositories/search?q=&signed=true|false&limit=&offset=
// Matches repo name or tag substrings (case-insensitive). Signed status and
// scan info are resolved only for matches, not the whole registry — cost is
// bounded by how many repo:tag pairs match q, same as GetTags already pays
// per tag.
func (h *Handler) Search(c echo.Context) error {
	q, signedFilter, limit, offset := parseSearchParams(c)

	repos, err := h.store.ListRepositories()
	if err != nil {
		return c.JSON(http.StatusInternalServerError, err500(err))
	}

	var results []SearchResult
	for _, repo := range repos {
		tags, err := h.store.ListTags(repo)
		if err != nil {
			continue
		}
		repoMatches := q == "" || strings.Contains(strings.ToLower(repo), q)
		for _, tag := range tags {
			if cosign.IsArtifactTag(tag) {
				continue // cosign signature/attestation/sbom tags aren't browsable images
			}
			if !repoMatches && !strings.Contains(strings.ToLower(tag), q) {
				continue
			}
			_, digest, err := h.store.GetManifest(repo, tag)
			if err != nil {
				continue
			}
			var signedPtr *bool
			if h.signing.HasKeys() {
				s := h.signing.Signed(cosign.BackendFetcher{Backend: h.store}, repo, digest)
				signedPtr = &s
			}
			if signedFilter != nil && (signedPtr == nil || *signedPtr != *signedFilter) {
				continue
			}
			r := SearchResult{Name: repo, Tag: tag, Digest: digest, Signed: signedPtr, Scan: scanInfoForDigest(h.db, digest)}
			if pushedAt, err := h.store.TagPushedAt(repo, tag); err == nil && !pushedAt.IsZero() {
				r.PushedAt = pushedAt.UTC().Format(time.RFC3339)
			}
			results = append(results, r)
		}
	}

	page, total := sortAndPage(results, limit, offset)
	return c.JSON(http.StatusOK, searchResponse(page, total))
}

// GET /api/admin/repositories/search?q=&signed=true|false&limit=&offset=
func (h *RemoteHandler) Search(c echo.Context) error {
	q, signedFilter, limit, offset := parseSearchParams(c)

	repos, err := h.client.Catalog()
	if err != nil {
		return c.JSON(http.StatusBadGateway, map[string]string{"error": err.Error()})
	}

	var results []SearchResult
	for _, repo := range repos {
		tags, err := h.client.Tags(repo)
		if err != nil {
			continue
		}
		repoMatches := q == "" || strings.Contains(strings.ToLower(repo), q)
		for _, tag := range tags {
			if cosign.IsArtifactTag(tag) {
				continue // cosign signature/attestation/sbom tags aren't browsable images
			}
			if !repoMatches && !strings.Contains(strings.ToLower(tag), q) {
				continue
			}
			_, digest, err := h.client.RawManifest(repo, tag)
			if err != nil {
				continue
			}
			var signedPtr *bool
			if h.signing.HasKeys() {
				s := h.signing.Signed(cosign.ClientFetcher{Client: h.client}, repo, digest)
				signedPtr = &s
			}
			if signedFilter != nil && (signedPtr == nil || *signedPtr != *signedFilter) {
				continue
			}
			results = append(results, SearchResult{Name: repo, Tag: tag, Digest: digest, Signed: signedPtr, Scan: scanInfoForDigest(h.db, digest)})
		}
	}

	page, total := sortAndPage(results, limit, offset)
	return c.JSON(http.StatusOK, searchResponse(page, total))
}
