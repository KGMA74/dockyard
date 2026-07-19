package admin

import (
	"encoding/json"
	"net/http"
	"sort"

	"dockyard/internal/storage"
	"dockyard/internal/store"

	"github.com/labstack/echo/v4"
)

// InsightsHandler serves the storage-insights view: growth history (sampled
// snapshots) and the largest repositories.
type InsightsHandler struct {
	backend storage.Backend
	db      *store.Store
}

func NewInsights(backend storage.Backend, db *store.Store) *InsightsHandler {
	return &InsightsHandler{backend: backend, db: db}
}

type repoSize struct {
	Name      string `json:"name"`
	SizeBytes int64  `json:"size_bytes"`
	SizeHuman string `json:"size_human"`
	Tags      int    `json:"tags"`
}

// Get — GET /api/admin/insights (admin only, embedded/mirror)
func (h *InsightsHandler) Get(c echo.Context) error {
	history, err := h.db.ListStatsSamples(360)
	if err != nil {
		return c.JSON(http.StatusInternalServerError, err500(err))
	}
	if history == nil {
		history = []*store.StatsSample{}
	}

	top, err := h.topRepositories(10)
	if err != nil {
		return c.JSON(http.StatusInternalServerError, err500(err))
	}
	return c.JSON(http.StatusOK, map[string]any{
		"history":   history,
		"top_repos": top,
	})
}

// topRepositories sizes each repository by summing its unique referenced blob
// sizes (config + layers, deduplicated per repo). Multi-arch children tagged
// only by digest contribute through their own manifests.
func (h *InsightsHandler) topRepositories(limit int) ([]repoSize, error) {
	repos, err := h.backend.ListRepositories()
	if err != nil {
		return nil, err
	}
	out := make([]repoSize, 0, len(repos))
	for _, repo := range repos {
		tags, err := h.backend.ListTags(repo)
		if err != nil {
			continue
		}
		seen := map[string]int64{}
		for _, tag := range tags {
			raw, _, err := h.backend.GetManifest(repo, tag)
			if err != nil {
				continue
			}
			var m struct {
				Config struct {
					Size   int64  `json:"size"`
					Digest string `json:"digest"`
				} `json:"config"`
				Layers []struct {
					Size   int64  `json:"size"`
					Digest string `json:"digest"`
				} `json:"layers"`
			}
			if err := json.Unmarshal(raw, &m); err != nil {
				continue
			}
			if m.Config.Digest != "" {
				seen[m.Config.Digest] = m.Config.Size
			}
			for _, l := range m.Layers {
				if l.Digest != "" {
					seen[l.Digest] = l.Size
				}
			}
		}
		var total int64
		for _, size := range seen {
			total += size
		}
		out = append(out, repoSize{Name: repo, SizeBytes: total, SizeHuman: humanSize(total), Tags: len(tags)})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].SizeBytes > out[j].SizeBytes })
	if len(out) > limit {
		out = out[:limit]
	}
	return out, nil
}
