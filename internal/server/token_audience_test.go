package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

// A registry token is what /v2/token hands out for registry credentials — a
// password, but also a CLI secret, or nothing at all with anonymous pull. It
// must open /v2 and nothing else: were it accepted on the admin API, a CLI
// secret (meant for docker login on a CI runner) would be a full console
// credential, able among other things to mint itself a new CLI secret.
func TestRegistryTokensStayOnTheRegistry(t *testing.T) {
	srv := httptest.NewServer(newTestServer(t, func(s *Server) {
		s.v2AuthEnabled = true
		s.v2AnonymousPull = true
	}))
	t.Cleanup(srv.Close)
	adminJWT := loginAs(t, srv.URL, "admin", "test-password")

	status, raw := apiCall(t, srv.URL, http.MethodPost, "/api/admin/auth/cli-secret", adminJWT, []byte(`{}`))
	if status != http.StatusCreated {
		t.Fatalf("create CLI secret = %d: %s", status, raw)
	}
	var cli struct {
		Secret string `json:"secret"`
	}
	if err := json.Unmarshal(raw, &cli); err != nil {
		t.Fatal(err)
	}

	anonymous := func() string {
		resp, err := http.Get(srv.URL + "/v2/token?service=dockyard-registry")
		if err != nil {
			t.Fatal(err)
		}
		defer func() { _ = resp.Body.Close() }()
		var body struct {
			Token string `json:"token"`
		}
		if err := json.NewDecoder(resp.Body).Decode(&body); err != nil || body.Token == "" {
			t.Fatalf("anonymous token: %v", err)
		}
		return body.Token
	}

	tokens := map[string]string{
		"password":   fetchToken(t, srv.URL, "admin", "test-password"),
		"CLI secret": fetchToken(t, srv.URL, "admin", cli.Secret),
		"anonymous":  anonymous(),
	}
	for name, token := range tokens {
		t.Run(name+" token works on /v2 only", func(t *testing.T) {
			if got := authedStatus(t, srv.URL, http.MethodGet, "/v2/", token, nil); got != http.StatusOK {
				t.Fatalf("/v2/ = %d, want 200", got)
			}
			for _, call := range []struct{ method, path string }{
				{http.MethodGet, "/api/admin/repositories"},
				{http.MethodGet, "/api/admin/users"},
				{http.MethodPost, "/api/admin/auth/cli-secret"},
			} {
				if status, body := apiCall(t, srv.URL, call.method, call.path, token, []byte(`{}`)); status != http.StatusForbidden {
					t.Errorf("%s %s = %d (%s), want 403", call.method, call.path, status, body)
				}
			}
			// The event stream takes its token from the query string. Accepted,
			// it would stream forever: bound the wait so a regression fails
			// instead of hanging the suite.
			client := &http.Client{Timeout: 3 * time.Second}
			resp, err := client.Get(srv.URL + "/api/admin/events?token=" + token)
			if err != nil {
				t.Fatalf("event stream: %v (accepted the token?)", err)
			}
			_ = resp.Body.Close()
			if resp.StatusCode != http.StatusForbidden {
				t.Errorf("event stream = %d, want 403", resp.StatusCode)
			}
		})
	}

	t.Run("console tokens keep working on both", func(t *testing.T) {
		if status, _ := apiCall(t, srv.URL, http.MethodGet, "/api/admin/users", adminJWT, nil); status != http.StatusOK {
			t.Fatalf("console token on admin API = %d", status)
		}
		if got := authedStatus(t, srv.URL, http.MethodGet, "/v2/", adminJWT, nil); got != http.StatusOK {
			t.Fatalf("console token on /v2 = %d", got)
		}
	})
}
