package admin

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"dockyard/internal/cosign"
	"dockyard/internal/storage"
	"dockyard/internal/storage/storagetest"

	"github.com/labstack/echo/v4"
)

// manifestWithSizes builds a manifest JSON with accurate declared sizes,
// unlike storagetest.ManifestFor which always sets size:0 — needed here so
// the size delta the diff computes is meaningful.
func manifestWithSizes(configDigest string, configSize int, layerDigest string, layerSize int) []byte {
	return fmt.Appendf(nil,
		`{"schemaVersion":2,"mediaType":"application/vnd.docker.distribution.manifest.v2+json","config":{"digest":%q,"size":%d},"layers":[{"digest":%q,"size":%d}]}`,
		configDigest, configSize, layerDigest, layerSize,
	)
}

func TestDiffManifestsLayerSets(t *testing.T) {
	a := map[string]any{
		"total_size_bytes": int64(100),
		"layers": []layerDetail{
			{Digest: "sha256:shared", SizeBytes: 40},
			{Digest: "sha256:only-a", SizeBytes: 60},
		},
	}
	b := map[string]any{
		"total_size_bytes": int64(90),
		"layers": []layerDetail{
			{Digest: "sha256:shared", SizeBytes: 40},
			{Digest: "sha256:only-b", SizeBytes: 50},
		},
	}

	result := diffManifests(a, b)

	onlyA, _ := result["layers_only_a"].([]string)
	onlyB, _ := result["layers_only_b"].([]string)
	common, _ := result["layers_common"].([]string)
	delta, _ := result["size_delta_bytes"].(int64)

	if len(onlyA) != 1 || onlyA[0] != "sha256:only-a" {
		t.Fatalf("layers_only_a = %v, want [sha256:only-a]", onlyA)
	}
	if len(onlyB) != 1 || onlyB[0] != "sha256:only-b" {
		t.Fatalf("layers_only_b = %v, want [sha256:only-b]", onlyB)
	}
	if len(common) != 1 || common[0] != "sha256:shared" {
		t.Fatalf("layers_common = %v, want [sha256:shared]", common)
	}
	if delta != -10 {
		t.Fatalf("size_delta_bytes = %d, want -10", delta)
	}
}

func TestDiffManifestsIdenticalLayers(t *testing.T) {
	a := map[string]any{
		"total_size_bytes": int64(50),
		"layers":           []layerDetail{{Digest: "sha256:x", SizeBytes: 50}},
	}
	b := map[string]any{
		"total_size_bytes": int64(50),
		"layers":           []layerDetail{{Digest: "sha256:x", SizeBytes: 50}},
	}
	result := diffManifests(a, b)
	onlyA, _ := result["layers_only_a"].([]string)
	onlyB, _ := result["layers_only_b"].([]string)
	common, _ := result["layers_common"].([]string)
	if len(onlyA) != 0 || len(onlyB) != 0 {
		t.Fatalf("expected no exclusive layers, got onlyA=%v onlyB=%v", onlyA, onlyB)
	}
	if len(common) != 1 {
		t.Fatalf("expected 1 common layer, got %v", common)
	}
	if result["size_delta_bytes"].(int64) != 0 {
		t.Fatalf("size_delta_bytes = %v, want 0", result["size_delta_bytes"])
	}
}

func TestGetTagDiffHTTP(t *testing.T) {
	const name = "team/app"
	config := []byte(`{"architecture":"amd64"}`)
	configDgst := storagetest.Digest(config)

	backend, err := storage.NewLocal(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if err := backend.PutBlob(configDgst, bytes.NewReader(config), int64(len(config))); err != nil {
		t.Fatal(err)
	}

	layerV1 := []byte("layer-v1")
	layerV1Dgst := storagetest.Digest(layerV1)
	if err := backend.PutBlob(layerV1Dgst, bytes.NewReader(layerV1), int64(len(layerV1))); err != nil {
		t.Fatal(err)
	}
	manifestV1 := manifestWithSizes(configDgst, len(config), layerV1Dgst, len(layerV1))
	if err := backend.PutManifest(name, "v1", storagetest.Digest(manifestV1), manifestV1); err != nil {
		t.Fatal(err)
	}

	layerV2 := []byte("layer-v2-bigger-content")
	layerV2Dgst := storagetest.Digest(layerV2)
	if err := backend.PutBlob(layerV2Dgst, bytes.NewReader(layerV2), int64(len(layerV2))); err != nil {
		t.Fatal(err)
	}
	manifestV2 := manifestWithSizes(configDgst, len(config), layerV2Dgst, len(layerV2))
	if err := backend.PutManifest(name, "v2", storagetest.Digest(manifestV2), manifestV2); err != nil {
		t.Fatal(err)
	}

	h := New(backend, cosign.NewPolicy(false, nil, nil), nil)
	e := echo.New()

	t.Run("v1 vs v2", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/admin/repositories/diff?name="+name+"&reference_a=v1&reference_b=v2", nil)
		rec := httptest.NewRecorder()
		if err := h.GetTagDiff(e.NewContext(req, rec)); err != nil {
			t.Fatal(err)
		}
		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d: %s", rec.Code, rec.Body.String())
		}
		var result map[string]any
		if err := json.Unmarshal(rec.Body.Bytes(), &result); err != nil {
			t.Fatal(err)
		}
		onlyA, _ := result["layers_only_a"].([]any)
		onlyB, _ := result["layers_only_b"].([]any)
		if len(onlyA) != 1 || onlyA[0] != layerV1Dgst {
			t.Fatalf("layers_only_a = %v, want [%s]", onlyA, layerV1Dgst)
		}
		if len(onlyB) != 1 || onlyB[0] != layerV2Dgst {
			t.Fatalf("layers_only_b = %v, want [%s]", onlyB, layerV2Dgst)
		}
		delta, _ := result["size_delta_bytes"].(float64)
		if delta != float64(len(layerV2)-len(layerV1)) {
			t.Fatalf("size_delta_bytes = %v, want %d", delta, len(layerV2)-len(layerV1))
		}
	})

	t.Run("missing reference_b returns 404", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/admin/repositories/diff?name="+name+"&reference_a=v1&reference_b=missing", nil)
		rec := httptest.NewRecorder()
		if err := h.GetTagDiff(e.NewContext(req, rec)); err != nil {
			t.Fatal(err)
		}
		if rec.Code != http.StatusNotFound {
			t.Fatalf("status = %d, want 404", rec.Code)
		}
	})

	t.Run("missing params returns 400", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/admin/repositories/diff?name="+name+"&reference_a=v1", nil)
		rec := httptest.NewRecorder()
		if err := h.GetTagDiff(e.NewContext(req, rec)); err != nil {
			t.Fatal(err)
		}
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("status = %d, want 400", rec.Code)
		}
	})
}
