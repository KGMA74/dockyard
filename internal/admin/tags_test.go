package admin

import (
	"encoding/json"
	"reflect"
	"strconv"
	"sync/atomic"
	"testing"
	"time"
)

func configWithHistory(t *testing.T, history string) ociImageConfig {
	t.Helper()
	var cfg ociImageConfig
	if err := json.Unmarshal([]byte(`{"architecture":"amd64","os":"linux","history":`+history+`}`), &cfg); err != nil {
		t.Fatal(err)
	}
	return cfg
}

func TestBuildHistoryPairsNonEmptyStepsWithLayers(t *testing.T) {
	cfg := configWithHistory(t, `[
		{"created_by":"ADD rootfs.tar.gz /"},
		{"created_by":"ENV PATH=/usr/bin","empty_layer":true},
		{"created_by":"RUN apk add curl"},
		{"created_by":"CMD [\"/app\"]","empty_layer":true}
	]`)
	layers := []layerDetail{{Digest: "sha256:base", SizeBytes: 300}, {Digest: "sha256:curl", SizeBytes: 20}}

	got := buildHistory(cfg, layers)
	want := []historyStep{
		{CreatedBy: "ADD rootfs.tar.gz /", LayerDigest: "sha256:base", SizeBytes: 300},
		{CreatedBy: "ENV PATH=/usr/bin", EmptyLayer: true},
		{CreatedBy: "RUN apk add curl", LayerDigest: "sha256:curl", SizeBytes: 20},
		{CreatedBy: `CMD ["/app"]`, EmptyLayer: true},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("history mismatch\n got: %+v\nwant: %+v", got, want)
	}
}

// A squashed or hand-made image can have history that doesn't line up with
// its layers: pairing them by position would put the wrong command next to
// a layer, so no step gets a layer at all.
func TestBuildHistoryDoesNotGuessWhenCountsDisagree(t *testing.T) {
	cfg := configWithHistory(t, `[{"created_by":"RUN a"},{"created_by":"RUN b"}]`)
	got := buildHistory(cfg, []layerDetail{{Digest: "sha256:squashed", SizeBytes: 9}})
	for _, step := range got {
		if step.LayerDigest != "" || step.SizeBytes != 0 {
			t.Fatalf("step %q was attributed a layer despite mismatched counts", step.CreatedBy)
		}
	}
	if len(got) != 2 {
		t.Fatalf("want both steps listed, got %d", len(got))
	}
}

func TestBuildHistoryAbsentWithoutConfigHistory(t *testing.T) {
	if got := buildHistory(ociImageConfig{}, []layerDetail{{Digest: "sha256:x"}}); got != nil {
		t.Fatalf("want nil, got %+v", got)
	}
}

func TestPlatformsOf(t *testing.T) {
	single := map[string]any{"os": "linux", "architecture": "arm", "variant": "v7"}
	if got := platformsOf(single); !reflect.DeepEqual(got, []string{"linux/arm/v7"}) {
		t.Fatalf("single-arch: got %v", got)
	}

	index := map[string]any{"platforms": []platformDetail{
		{OS: "linux", Architecture: "amd64"},
		{OS: "linux", Architecture: "arm64", Variant: "v8"},
		{OS: "unknown", Architecture: "unknown"}, // BuildKit attestation manifest
	}}
	if got := platformsOf(index); !reflect.DeepEqual(got, []string{"linux/amd64", "linux/arm64/v8"}) {
		t.Fatalf("index: got %v", got)
	}

	if got := platformsOf(map[string]any{}); got != nil {
		t.Fatalf("no platform info: want nil, got %v", got)
	}
}

func TestApplyDetailsLeavesUnknownFieldsAbsent(t *testing.T) {
	var summary TagSummary
	applyDetails(&summary, map[string]any{"digest": "sha256:d", "total_size_bytes": int64(42)})
	if summary.Digest != "sha256:d" || summary.SizeBytes == nil || *summary.SizeBytes != 42 {
		t.Fatalf("digest/size not copied: %+v", summary)
	}
	// No cosign keys configured → "signed" absent → must stay unknown, not false.
	if summary.Signed != nil {
		t.Fatalf("signed should be unknown, got %v", *summary.Signed)
	}
}

func TestSummarizeTagsKeepsOrderAndBoundsConcurrency(t *testing.T) {
	tags := make([]string, 50)
	for i := range tags {
		tags[i] = "t" + strconv.Itoa(i)
	}
	var running, peak atomic.Int32
	got := summarizeTags(tags, func(tag string) TagSummary {
		n := running.Add(1)
		for {
			p := peak.Load()
			if n <= p || peak.CompareAndSwap(p, n) {
				break
			}
		}
		time.Sleep(2 * time.Millisecond)
		running.Add(-1)
		return TagSummary{Tag: tag}
	})
	for i, summary := range got {
		if summary.Tag != tags[i] {
			t.Fatalf("result %d is %q, want %q", i, summary.Tag, tags[i])
		}
	}
	if p := peak.Load(); p > tagSummaryWorkers {
		t.Fatalf("ran %d at once, limit is %d", p, tagSummaryWorkers)
	}
}
