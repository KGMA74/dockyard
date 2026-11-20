package admin

import (
	"sync"

	"dockyard/internal/store"
)

// TagSummary is one row of a repository's tag list: enough to compare tags
// at a glance — size, platforms, vulnerabilities, signature — without
// opening each one. Optional fields are absent when unknown (manifest
// unreadable, no cosign keys, never scanned), never zero-valued guesses.
type TagSummary struct {
	Tag       string       `json:"tag"`
	Digest    string       `json:"digest"`
	PushedAt  string       `json:"pushed_at,omitempty"`
	SizeBytes *int64       `json:"size_bytes,omitempty"`
	Platforms []string     `json:"platforms,omitempty"`
	Signed    *bool        `json:"signed,omitempty"`
	Scan      *TagScanInfo `json:"scan,omitempty"`
	// PullBlocked: the vulnerability policy refuses docker pull of this tag.
	PullBlocked bool `json:"pull_blocked,omitempty"`
}

// PullPolicy tells whether the vulnerability policy blocks pulling a digest
// (scan.PolicyState). Nil when scanning is off.
type PullPolicy interface{ Blocks(digest string) bool }

func pullBlocked(policy PullPolicy, summary TagSummary) bool {
	return policy != nil && summary.Scan != nil && policy.Blocks(summary.Digest)
}

// TagScanInfo is the latest scan of a tag's digest.
type TagScanInfo struct {
	Status        string `json:"status"`
	CriticalCount int    `json:"critical_count"`
	HighCount     int    `json:"high_count"`
	MediumCount   int    `json:"medium_count"`
	LowCount      int    `json:"low_count"`
	UnknownCount  int    `json:"unknown_count"`
}

func tagScanInfo(db *store.Store, digest string) *TagScanInfo {
	if db == nil || digest == "" {
		return nil
	}
	sc, err := db.LatestScanForDigest(digest)
	if err != nil {
		return nil
	}
	return &TagScanInfo{
		Status:        sc.Status,
		CriticalCount: sc.CriticalCount,
		HighCount:     sc.HighCount,
		MediumCount:   sc.MediumCount,
		LowCount:      sc.LowCount,
		UnknownCount:  sc.UnknownCount,
	}
}

// applyDetails copies what the list needs out of parseManifestDetails' result.
func applyDetails(summary *TagSummary, details map[string]any) {
	if digest, ok := details["digest"].(string); ok && digest != "" {
		summary.Digest = digest
	}
	if size, ok := details["total_size_bytes"].(int64); ok {
		summary.SizeBytes = &size
	}
	if signed, ok := details["signed"].(bool); ok {
		summary.Signed = &signed
	}
	summary.Platforms = platformsOf(details)
}

// platformsOf lists "os/arch[/variant]" for a single image or each entry of
// a multi-arch index. BuildKit adds attestation manifests to indexes with
// the platform "unknown/unknown": they are not something you can run.
func platformsOf(details map[string]any) []string {
	if entries, ok := details["platforms"].([]platformDetail); ok {
		var out []string
		for _, p := range entries {
			if s := platformString(p.OS, p.Architecture, p.Variant); s != "" {
				out = append(out, s)
			}
		}
		return out
	}
	os, _ := details["os"].(string)
	arch, _ := details["architecture"].(string)
	variant, _ := details["variant"].(string)
	if s := platformString(os, arch, variant); s != "" {
		return []string{s}
	}
	return nil
}

func platformString(os, arch, variant string) string {
	if os == "" || arch == "" || os == "unknown" || arch == "unknown" {
		return ""
	}
	if variant != "" {
		return os + "/" + arch + "/" + variant
	}
	return os + "/" + arch
}

// tagSummaryWorkers bounds how many tags are resolved at once: each one
// reads a manifest and its config (more for multi-arch images), which on S3
// or an upstream registry is a network round trip.
const tagSummaryWorkers = 8

// summarizeTags runs summarize for every tag with bounded concurrency and
// returns the results in the input order.
func summarizeTags(tags []string, summarize func(tag string) TagSummary) []TagSummary {
	out := make([]TagSummary, len(tags))
	sem := make(chan struct{}, tagSummaryWorkers)
	var wg sync.WaitGroup
	for i, tag := range tags {
		wg.Add(1)
		sem <- struct{}{}
		go func() {
			defer wg.Done()
			defer func() { <-sem }()
			out[i] = summarize(tag)
		}()
	}
	wg.Wait()
	return out
}
