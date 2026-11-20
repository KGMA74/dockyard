package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
)

type createdRobot struct {
	ID       int64  `json:"id"`
	Username string `json:"username"`
	Secret   string `json:"secret"`
}

func createRobot(t *testing.T, base, adminJWT string, body map[string]any) createdRobot {
	t.Helper()
	raw, _ := json.Marshal(body)
	status, resp := apiCall(t, base, http.MethodPost, "/api/admin/robots", adminJWT, raw)
	if status != http.StatusCreated {
		t.Fatalf("create robot = %d: %s", status, resp)
	}
	var r createdRobot
	if err := json.Unmarshal(resp, &r); err != nil {
		t.Fatal(err)
	}
	return r
}

func basicStatus(t *testing.T, base, method, path, user, pass string) int {
	t.Helper()
	req, _ := http.NewRequest(method, base+path, nil)
	req.SetBasicAuth(user, pass)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	_ = resp.Body.Close()
	return resp.StatusCode
}

func TestRobotAccounts(t *testing.T) {
	srv := httptest.NewServer(newTestServer(t, func(s *Server) { s.v2AuthEnabled = true }))
	t.Cleanup(srv.Close)
	adminJWT := loginAs(t, srv.URL, "admin", "test-password")

	ci := createRobot(t, srv.URL, adminJWT, map[string]any{
		"name": "ci-team-a", "role": "pusher", "repo_patterns": []string{"team-a/*"},
	})
	if ci.Username != "robot+ci-team-a" || !strings.HasPrefix(ci.Secret, "dkr_") {
		t.Fatalf("unexpected robot credentials: %+v", ci)
	}

	t.Run("pushes within its patterns through docker token auth", func(t *testing.T) {
		token := fetchToken(t, srv.URL, ci.Username, ci.Secret)
		pushImage(t, srv.URL, token, "team-a/app", "1.0")
		if got := authedStatus(t, srv.URL, http.MethodGet, "/v2/team-a/app/manifests/1.0", token, nil); got != http.StatusOK {
			t.Fatalf("robot pull = %d", got)
		}
		if got := authedStatus(t, srv.URL, http.MethodPut, "/v2/team-b/app/manifests/1.0", token, []byte(`{}`)); got != http.StatusForbidden {
			t.Fatalf("robot push outside its patterns = %d, want 403", got)
		}
	})

	t.Run("cannot delete, even within its patterns", func(t *testing.T) {
		token := fetchToken(t, srv.URL, ci.Username, ci.Secret)
		if got := authedStatus(t, srv.URL, http.MethodDelete, "/v2/team-a/app/manifests/1.0", token, nil); got != http.StatusForbidden {
			t.Fatalf("robot delete = %d, want 403", got)
		}
	})

	t.Run("works with Basic auth on every request", func(t *testing.T) {
		if got := basicStatus(t, srv.URL, http.MethodGet, "/v2/team-a/app/manifests/1.0", ci.Username, ci.Secret); got != http.StatusOK {
			t.Fatalf("basic pull = %d", got)
		}
		if got := basicStatus(t, srv.URL, http.MethodGet, "/v2/team-a/app/manifests/1.0", ci.Username, "dkr_wrong"); got != http.StatusUnauthorized {
			t.Fatalf("wrong secret = %d, want 401", got)
		}
	})

	t.Run("cannot use the admin API or the console", func(t *testing.T) {
		token := fetchToken(t, srv.URL, ci.Username, ci.Secret)
		if status, _ := apiCall(t, srv.URL, http.MethodGet, "/api/admin/repositories", token, nil); status != http.StatusForbidden {
			t.Fatalf("robot token on admin API = %d, want 403", status)
		}
		body, _ := json.Marshal(map[string]string{"username": ci.Username, "password": ci.Secret})
		if status, _ := apiCall(t, srv.URL, http.MethodPost, "/api/admin/auth/login", "", body); status != http.StatusUnauthorized {
			t.Fatalf("robot console login = %d, want 401", status)
		}
	})

	t.Run("listing never shows the secret", func(t *testing.T) {
		status, raw := apiCall(t, srv.URL, http.MethodGet, "/api/admin/robots", adminJWT, nil)
		if status != http.StatusOK {
			t.Fatalf("list = %d", status)
		}
		if strings.Contains(string(raw), ci.Secret) || strings.Contains(string(raw), "secret_hash") {
			t.Fatalf("robot list leaks the secret: %s", raw)
		}
	})

	t.Run("rotating invalidates the old secret at once", func(t *testing.T) {
		token := fetchToken(t, srv.URL, ci.Username, ci.Secret)
		status, raw := apiCall(t, srv.URL, http.MethodPost, "/api/admin/robots/"+strconv.FormatInt(ci.ID, 10)+"/secret", adminJWT, []byte(`{}`))
		if status != http.StatusOK {
			t.Fatalf("rotate = %d: %s", status, raw)
		}
		var rotated createdRobot
		_ = json.Unmarshal(raw, &rotated)
		if got := basicStatus(t, srv.URL, http.MethodGet, "/v2/team-a/app/manifests/1.0", ci.Username, ci.Secret); got != http.StatusUnauthorized {
			t.Fatalf("old secret after rotation = %d, want 401", got)
		}
		if got := basicStatus(t, srv.URL, http.MethodGet, "/v2/team-a/app/manifests/1.0", ci.Username, rotated.Secret); got != http.StatusOK {
			t.Fatalf("new secret = %d", got)
		}
		// Tokens already issued stay valid until they expire (5 minutes):
		// rotation is for leaked secrets, deletion is the kill switch.
		_ = token
	})

	t.Run("deleting kills outstanding registry tokens immediately", func(t *testing.T) {
		reader := createRobot(t, srv.URL, adminJWT, map[string]any{"name": "k8s-pull", "role": "reader"})
		token := fetchToken(t, srv.URL, reader.Username, reader.Secret)
		if got := authedStatus(t, srv.URL, http.MethodGet, "/v2/team-a/app/manifests/1.0", token, nil); got != http.StatusOK {
			t.Fatalf("reader robot pull = %d", got)
		}
		if status, _ := apiCall(t, srv.URL, http.MethodDelete, "/api/admin/robots/"+strconv.FormatInt(reader.ID, 10), adminJWT, nil); status != http.StatusOK {
			t.Fatalf("delete = %d", status)
		}
		if got := authedStatus(t, srv.URL, http.MethodGet, "/v2/team-a/app/manifests/1.0", token, nil); got != http.StatusUnauthorized {
			t.Fatalf("deleted robot's token = %d, want 401", got)
		}
	})

	t.Run("rejects admin role, bad names and duplicates", func(t *testing.T) {
		for _, body := range []map[string]any{
			{"name": "ops", "role": "admin"},
			{"name": "Bad Name", "role": "reader"},
			{"name": "ci-team-a", "role": "reader"},
		} {
			raw, _ := json.Marshal(body)
			if status, _ := apiCall(t, srv.URL, http.MethodPost, "/api/admin/robots", adminJWT, raw); status != http.StatusBadRequest && status != http.StatusConflict {
				t.Errorf("create %v = %d, want 400/409", body, status)
			}
		}
	})
}
