package export

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"dockyard/internal/events"

	"github.com/labstack/echo/v4"
)

func TestImportPublishesEventOnSuccess(t *testing.T) {
	src := newBackend(t)
	seedRepo(t, src, "team/app")
	var buf bytes.Buffer
	if err := Export(&buf, src, "team/app"); err != nil {
		t.Fatalf("Export: %v", err)
	}

	dst := newBackend(t)
	h := NewHandler(dst)
	hub := events.NewHub()
	h.SetHub(hub)
	ch := hub.Subscribe()
	defer hub.Unsubscribe(ch)

	e := echo.New()
	req := httptest.NewRequest(http.MethodPost, "/api/admin/repositories/import?name=team/app", bytes.NewReader(buf.Bytes()))
	rec := httptest.NewRecorder()
	if err := h.Import(e.NewContext(req, rec)); err != nil {
		t.Fatal(err)
	}
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d: %s", rec.Code, rec.Body.String())
	}

	select {
	case ev := <-ch:
		if ev.Type != "import" || ev.Name != "team/app" {
			t.Fatalf("unexpected event: %+v", ev)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("expected an import event after a successful import")
	}
}

func TestImportDoesNotPublishOnFailure(t *testing.T) {
	dst := newBackend(t)
	h := NewHandler(dst)
	hub := events.NewHub()
	h.SetHub(hub)
	ch := hub.Subscribe()
	defer hub.Unsubscribe(ch)

	e := echo.New()
	req := httptest.NewRequest(http.MethodPost, "/api/admin/repositories/import?name=team/app", bytes.NewReader([]byte("not a tar")))
	rec := httptest.NewRecorder()
	if err := h.Import(e.NewContext(req, rec)); err != nil {
		t.Fatal(err)
	}
	if rec.Code == http.StatusOK {
		t.Fatalf("expected a failure status for a malformed tarball, got %d", rec.Code)
	}

	select {
	case ev := <-ch:
		t.Fatalf("unexpected event on a failed import: %+v", ev)
	case <-time.After(100 * time.Millisecond):
	}
}
