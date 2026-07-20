package admin

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"dockyard/internal/cosign"
	"dockyard/internal/storage"
	"dockyard/internal/storage/storagetest"

	"github.com/labstack/echo/v4"
)

// pushSimpleTag pushes a manifest unique to (name, tag) — the config content
// embeds both so distinct tags never collide on the same digest.
func pushSimpleTag(t *testing.T, backend *storage.LocalBackend, name, tag string) string {
	t.Helper()
	config := []byte(`{"architecture":"amd64","name":"` + name + `","tag":"` + tag + `"}`)
	configDgst := storagetest.Digest(config)
	if err := backend.PutBlob(configDgst, bytes.NewReader(config), int64(len(config))); err != nil {
		t.Fatal(err)
	}
	manifest := manifestWithSizes(configDgst, len(config), configDgst, 0)
	digest := storagetest.Digest(manifest)
	if err := backend.PutManifest(name, tag, digest, manifest); err != nil {
		t.Fatal(err)
	}
	return digest
}

func callSearch(t *testing.T, h *Handler, query string) map[string]any {
	t.Helper()
	e := echo.New()
	req := httptest.NewRequest(http.MethodGet, "/api/admin/repositories/search"+query, nil)
	rec := httptest.NewRecorder()
	if err := h.Search(e.NewContext(req, rec)); err != nil {
		t.Fatal(err)
	}
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d: %s", rec.Code, rec.Body.String())
	}
	var result map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	return result
}

func TestSearchMatchesRepoOrTagName(t *testing.T) {
	backend, err := storage.NewLocal(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	pushSimpleTag(t, backend, "team/frontend", "v1")
	pushSimpleTag(t, backend, "team/backend", "v1")
	pushSimpleTag(t, backend, "other/app", "staging")

	h := New(backend, cosign.NewPolicy(false, nil, nil), nil)

	t.Run("matches by repo name substring", func(t *testing.T) {
		result := callSearch(t, h, "?q=front")
		items, _ := result["items"].([]any)
		if len(items) != 1 {
			t.Fatalf("got %d items, want 1", len(items))
		}
	})

	t.Run("matches by tag name", func(t *testing.T) {
		result := callSearch(t, h, "?q=staging")
		items, _ := result["items"].([]any)
		if len(items) != 1 {
			t.Fatalf("got %d items, want 1", len(items))
		}
		first, _ := items[0].(map[string]any)
		if first["name"] != "other/app" {
			t.Fatalf("name = %v, want other/app", first["name"])
		}
	})

	t.Run("empty query returns everything", func(t *testing.T) {
		result := callSearch(t, h, "")
		total, _ := result["total"].(float64)
		if total != 3 {
			t.Fatalf("total = %v, want 3", total)
		}
	})

	t.Run("no match returns empty items, not null", func(t *testing.T) {
		result := callSearch(t, h, "?q=doesnotexist")
		items, ok := result["items"].([]any)
		if !ok {
			t.Fatalf("items = %v, want an array", result["items"])
		}
		if len(items) != 0 {
			t.Fatalf("got %d items, want 0", len(items))
		}
	})
}

func TestSearchPagination(t *testing.T) {
	backend, err := storage.NewLocal(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	for _, tag := range []string{"a", "b", "c", "d", "e"} {
		pushSimpleTag(t, backend, "team/app", tag)
	}
	h := New(backend, cosign.NewPolicy(false, nil, nil), nil)

	result := callSearch(t, h, "?limit=2&offset=1")
	items, _ := result["items"].([]any)
	total, _ := result["total"].(float64)
	count, _ := result["count"].(float64)
	if total != 5 {
		t.Fatalf("total = %v, want 5", total)
	}
	if count != 2 || len(items) != 2 {
		t.Fatalf("count/len = %v/%d, want 2/2", count, len(items))
	}
	// Sorted by name then tag: a, b, c, d, e — offset 1 limit 2 -> b, c.
	first, _ := items[0].(map[string]any)
	if first["tag"] != "b" {
		t.Fatalf("first tag = %v, want b", first["tag"])
	}
}

func TestSearchSignedFilter(t *testing.T) {
	backend, err := storage.NewLocal(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	priv := genTestKey(t)
	dir := t.TempDir()
	writeTestPublicKeyPEM(t, dir, &priv.PublicKey)
	keys, err := cosign.LoadPublicKeys(dir)
	if err != nil {
		t.Fatal(err)
	}

	signedDigest := pushSimpleTag(t, backend, "team/app", "signed")
	pushSimpleTag(t, backend, "team/app", "unsigned")
	pushSignatureManifest(t, backend, "team/app", signedDigest, priv)

	h := New(backend, cosign.NewPolicy(false, keys, nil), nil)

	t.Run("signed=true", func(t *testing.T) {
		result := callSearch(t, h, "?signed=true")
		items, _ := result["items"].([]any)
		if len(items) != 1 {
			t.Fatalf("got %d items, want 1", len(items))
		}
		first, _ := items[0].(map[string]any)
		if first["tag"] != "signed" {
			t.Fatalf("tag = %v, want signed", first["tag"])
		}
	})

	t.Run("signed=false", func(t *testing.T) {
		result := callSearch(t, h, "?signed=false")
		items, _ := result["items"].([]any)
		if len(items) != 1 {
			t.Fatalf("got %d items, want 1", len(items))
		}
		first, _ := items[0].(map[string]any)
		if first["tag"] != "unsigned" {
			t.Fatalf("tag = %v, want unsigned", first["tag"])
		}
	})

	t.Run("no filter returns both", func(t *testing.T) {
		result := callSearch(t, h, "")
		items, _ := result["items"].([]any)
		if len(items) != 2 {
			t.Fatalf("got %d items, want 2", len(items))
		}
	})
}
