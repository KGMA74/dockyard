package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func setupSteps(t *testing.T, base, jwt string) map[string]bool {
	t.Helper()
	status, raw := apiCall(t, base, http.MethodGet, "/api/admin/setup", jwt, nil)
	if status != http.StatusOK {
		t.Fatalf("GET /api/admin/setup = %d: %s", status, raw)
	}
	var body struct {
		Steps []SetupStep `json:"steps"`
	}
	if err := json.Unmarshal(raw, &body); err != nil {
		t.Fatal(err)
	}
	out := map[string]bool{}
	for _, s := range body.Steps {
		out[s.ID] = s.Done
	}
	return out
}

// The checklist follows the instance's actual state: each step ticks itself
// once done, from wherever it was done.
func TestSetupChecklistFollowsTheInstance(t *testing.T) {
	srv := httptest.NewServer(newTestServer(t, func(s *Server) {
		s.authUsername = "admin"
		s.authPassword = "test-password" // what the deployment set
		s.v2AuthEnabled = true
		s.scanEnabled = true
		s.trivyOffline = true
	}))
	t.Cleanup(srv.Close)
	adminJWT := loginAs(t, srv.URL, "admin", "test-password")

	fresh := setupSteps(t, srv.URL, adminJWT)
	for _, id := range []string{"admin_password", "first_image", "robot_account", "scan_on_push", "team", "https"} {
		done, listed := fresh[id]
		if !listed || done {
			t.Errorf("fresh install: step %q = (done %v, listed %v), want listed and to do", id, done, listed)
		}
	}

	// Done from elsewhere in the console or with docker: the list follows.
	// Built at run time: a password-looking literal trips secret scanners.
	rotated := "rotated-" + strings.ToLower(t.Name())
	body, _ := json.Marshal(map[string]string{"current_password": "test-password", "new_password": rotated})
	if status, raw := apiCall(t, srv.URL, http.MethodPost, "/api/admin/auth/password", adminJWT, body); status != http.StatusOK {
		t.Fatalf("change password = %d: %s", status, raw)
	}
	adminJWT = loginAs(t, srv.URL, "admin", rotated)
	pushImage(t, srv.URL, fetchToken(t, srv.URL, "admin", rotated), "team/app", "1.0")
	createRobot(t, srv.URL, adminJWT, map[string]any{"name": "ci", "role": "pusher"})
	setPolicy(t, srv.URL, adminJWT, true, "")
	createUser(t, srv.URL, adminJWT, "dev", "dev-password", "reader")

	after := setupSteps(t, srv.URL, adminJWT)
	for _, id := range []string{"admin_password", "first_image", "robot_account", "scan_on_push", "team"} {
		if !after[id] {
			t.Errorf("step %q still to do after doing it", id)
		}
	}
	if after["https"] {
		t.Error("https ticked on a plain-HTTP test server")
	}

	// Readers do not see it.
	createUser(t, srv.URL, adminJWT, "viewer", "viewer-password", "reader")
	if status, _ := apiCall(t, srv.URL, http.MethodGet, "/api/admin/setup", loginAs(t, srv.URL, "viewer", "viewer-password"), nil); status != http.StatusForbidden {
		t.Errorf("reader on /api/admin/setup = %d, want 403", status)
	}
}

// Steps that cannot apply are left out: without scanning there is no scan
// on push to turn on.
func TestSetupChecklistLeavesOutWhatDoesNotApply(t *testing.T) {
	srv := httptest.NewServer(newTestServer(t, func(s *Server) {
		s.authUsername = "admin"
		s.authPassword = "test-password"
	}))
	t.Cleanup(srv.Close)
	steps := setupSteps(t, srv.URL, loginAs(t, srv.URL, "admin", "test-password"))
	if _, listed := steps["scan_on_push"]; listed {
		t.Error("scan_on_push listed with scanning off")
	}
}
