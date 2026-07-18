package audit

import (
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"dockyard/internal/auth"
	"dockyard/internal/store"

	"github.com/labstack/echo/v4"
)

func newRecorder(t *testing.T) (*Recorder, *store.Store) {
	t.Helper()
	st, err := store.Open(filepath.Join(t.TempDir(), "dockyard.db"))
	if err != nil {
		t.Fatalf("store.Open: %v", err)
	}
	t.Cleanup(func() { _ = st.Close() })
	return New(st), st
}

func lastEntries(t *testing.T, st *store.Store) []*store.AuditEntry {
	t.Helper()
	entries, _, err := st.ListAudit("", "", 50, 0)
	if err != nil {
		t.Fatalf("ListAudit: %v", err)
	}
	return entries
}

func TestStoreAuditRoundTrip(t *testing.T) {
	_, st := newRecorder(t)
	for range 3 {
		if err := st.AddAudit(store.AuditEntry{Actor: "alice", Action: "push", Repo: "team/app", Result: "201"}); err != nil {
			t.Fatal(err)
		}
	}
	if err := st.AddAudit(store.AuditEntry{Actor: "bob", Action: "login", Result: "failure"}); err != nil {
		t.Fatal(err)
	}

	entries, total, err := st.ListAudit("", "", 2, 0)
	if err != nil || total != 4 || len(entries) != 2 {
		t.Fatalf("ListAudit all = (%d entries, total %d, %v), want (2, 4)", len(entries), total, err)
	}
	if entries[0].Actor != "bob" {
		t.Errorf("newest first expected, got %+v", entries[0])
	}
	_, total, _ = st.ListAudit("team/app", "", 50, 0)
	if total != 3 {
		t.Errorf("repo filter total = %d, want 3", total)
	}
	_, total, _ = st.ListAudit("", "bob", 50, 0)
	if total != 1 {
		t.Errorf("actor filter total = %d, want 1", total)
	}
}

func TestAdminMiddlewareRecordsMutations(t *testing.T) {
	rec, st := newRecorder(t)
	e := echo.New()
	handler := func(c echo.Context) error { return c.NoContent(http.StatusOK) }
	mw := rec.AdminMiddleware()(handler)

	run := func(method, path string) {
		t.Helper()
		req := httptest.NewRequest(method, path, nil)
		res := httptest.NewRecorder()
		c := e.NewContext(req, res)
		// simulate the auth middleware having run
		cAny := c
		cAny.Set("auth.principal", auth.Principal{Username: "root", Role: store.RoleAdmin})
		if err := mw(c); err != nil {
			t.Fatal(err)
		}
	}

	run(http.MethodGet, "/api/admin/repositories")                       // not recorded
	run(http.MethodDelete, "/api/admin/repositories?name=team/app")      // recorded
	run(http.MethodPost, "/api/admin/gc")                                // recorded
	run(http.MethodPost, "/api/admin/auth/password")                     // skipped (auth manager records it)
	entries := lastEntries(t, st)
	if len(entries) != 2 {
		t.Fatalf("recorded %d entries, want 2: %+v", len(entries), entries)
	}
	if entries[1].Action != "DELETE /api/admin/repositories" || entries[1].Repo != "team/app" || entries[1].Actor != "root" {
		t.Errorf("delete entry = %+v", entries[1])
	}
	if entries[0].Action != "POST /api/admin/gc" {
		t.Errorf("gc entry = %+v", entries[0])
	}
}

func TestV2WrapperRecordsManifestWrites(t *testing.T) {
	rec, st := newRecorder(t)
	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusCreated)
	})
	h := rec.V2Wrapper()(inner)

	send := func(method, path string, withPrincipal bool) {
		t.Helper()
		req := httptest.NewRequest(method, path, nil)
		if withPrincipal {
			req = auth.WithPrincipal(req, auth.Principal{Username: "pusher1", Role: store.RolePusher})
		}
		h.ServeHTTP(httptest.NewRecorder(), req)
	}

	send(http.MethodGet, "/v2/team/app/manifests/v1", true)    // pull → not recorded
	send(http.MethodPut, "/v2/team/app/manifests/v1", true)    // push → recorded with actor
	send(http.MethodPut, "/v2/team/app/manifests/v2", false)   // push, no auth → anonymous
	send(http.MethodDelete, "/v2/team/app/manifests/sha256:abc", true)
	send(http.MethodPatch, "/v2/team/app/blobs/uploads/uuid1", true) // blob noise → not recorded

	entries := lastEntries(t, st)
	if len(entries) != 3 {
		t.Fatalf("recorded %d entries, want 3: %+v", len(entries), entries)
	}
	del, anon, push := entries[0], entries[1], entries[2]
	if push.Action != "push" || push.Actor != "pusher1" || push.Repo != "team/app" || push.Tag != "v1" || push.Result != "201" {
		t.Errorf("push entry = %+v", push)
	}
	if anon.Actor != "anonymous" {
		t.Errorf("anonymous push entry = %+v", anon)
	}
	if del.Action != "delete-manifest" || del.Tag != "" || del.Details != "sha256:abc" {
		t.Errorf("delete entry = %+v", del)
	}
}
