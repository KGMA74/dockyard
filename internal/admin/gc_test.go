package admin

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"dockyard/internal/storage"
	"dockyard/internal/storage/storagetest"

	"github.com/labstack/echo/v4"
)

type gcResponse struct {
	Removed    []string `json:"removed"`
	Count      int      `json:"count"`
	FreedBytes int64    `json:"freed_bytes"`
	DryRun     bool     `json:"dry_run"`
}

func TestGarbageCollectDryRun(t *testing.T) {
	backend, err := storage.NewLocal(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	orphan := []byte("orphan-blob-content")
	orphanDgst := storagetest.Digest(orphan)
	if err := backend.PutBlob(orphanDgst, bytes.NewReader(orphan), int64(len(orphan))); err != nil {
		t.Fatal(err)
	}

	h := New(backend)
	e := echo.New()
	call := func(query string) gcResponse {
		t.Helper()
		req := httptest.NewRequest(http.MethodPost, "/api/admin/gc"+query, nil)
		rec := httptest.NewRecorder()
		if err := h.GarbageCollect(e.NewContext(req, rec)); err != nil {
			t.Fatal(err)
		}
		if rec.Code != http.StatusOK {
			t.Fatalf("gc status = %d: %s", rec.Code, rec.Body.String())
		}
		var resp gcResponse
		if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
			t.Fatal(err)
		}
		return resp
	}

	preview := call("?dryRun=true")
	if !preview.DryRun || preview.Count != 1 || preview.FreedBytes != int64(len(orphan)) {
		t.Errorf("dry run = %+v, want 1 blob / %d bytes", preview, len(orphan))
	}
	if ok, _ := backend.BlobExists(orphanDgst); !ok {
		t.Fatal("dry run deleted the blob!")
	}

	// The real run must remove exactly what the preview announced.
	real := call("")
	if real.DryRun || real.Count != preview.Count || real.FreedBytes != preview.FreedBytes {
		t.Errorf("real run = %+v, preview promised %+v", real, preview)
	}
	if ok, _ := backend.BlobExists(orphanDgst); ok {
		t.Error("real run did not delete the orphan")
	}
}
