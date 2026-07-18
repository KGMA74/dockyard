package registry

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strings"
	"sync"
	"time"
)

type Client struct {
	baseURL    string
	username   string
	password   string
	httpClient *http.Client

	// Bearer tokens obtained via the Docker token-auth dance (Docker Hub,
	// ghcr, quay…), cached per scope until shortly before expiry.
	mu     sync.Mutex
	tokens map[string]bearerToken
}

type bearerToken struct {
	value   string
	expires time.Time
}

type catalogResponse struct {
	Repositories []string `json:"repositories"`
}

type tagsResponse struct {
	Name string   `json:"name"`
	Tags []string `json:"tags"`
}

type Manifest struct {
	SchemaVersion int             `json:"schemaVersion"`
	MediaType     string          `json:"mediaType"`
	Digest        string          `json:"digest,omitempty"`
	Config        ManifestConfig  `json:"config"`
	Layers        []ManifestLayer `json:"layers"`
}

type ManifestConfig struct {
	MediaType string `json:"mediaType"`
	Size      int64  `json:"size"`
	Digest    string `json:"digest"`
}

type ManifestLayer struct {
	MediaType string `json:"mediaType"`
	Size      int64  `json:"size"`
	Digest    string `json:"digest"`
}

func NewClient(baseURL, username, password string) *Client {
	return &Client{
		baseURL:  baseURL,
		username: username,
		password: password,
		httpClient: &http.Client{
			Timeout: 30 * time.Second,
		},
		tokens: make(map[string]bearerToken),
	}
}

func (c *Client) BaseURL() string  { return c.baseURL }
func (c *Client) Username() string { return c.username }
func (c *Client) Password() string { return c.password }

// repoFromPath extracts the repository from a V2 path, used as the token
// cache key ("/v2/library/alpine/manifests/latest" → "library/alpine").
var reRepoPath = regexp.MustCompile(`^/v2/(.+)/(?:manifests|blobs|tags)/`)

func repoFromPath(path string) string {
	if m := reRepoPath.FindStringSubmatch(path); m != nil {
		return m[1]
	}
	return ""
}

func (c *Client) do(method, path, accept string) (*http.Response, error) {
	resp, err := c.roundTrip(method, path, accept, c.cachedToken(repoFromPath(path)))
	if err != nil {
		return nil, err
	}

	// Docker token auth: a 401 with a Bearer challenge means we must trade
	// our credentials for a scoped token at the advertised realm and retry.
	if resp.StatusCode == http.StatusUnauthorized {
		challenge := resp.Header.Get("Www-Authenticate")
		_ = resp.Body.Close()
		token, err := c.fetchBearerToken(challenge)
		if err != nil {
			return nil, fmt.Errorf("registry auth: %s %s → %w", method, path, err)
		}
		resp, err = c.roundTrip(method, path, accept, token)
		if err != nil {
			return nil, err
		}
	}

	if resp.StatusCode >= 400 {
		_ = resp.Body.Close()
		return nil, fmt.Errorf("registry error: %s %s → %d", method, path, resp.StatusCode)
	}
	return resp, nil
}

func (c *Client) roundTrip(method, path, accept, bearer string) (*http.Response, error) {
	req, err := http.NewRequest(method, c.baseURL+path, nil)
	if err != nil {
		return nil, err
	}
	if accept != "" {
		req.Header.Set("Accept", accept)
	}
	switch {
	case bearer != "":
		req.Header.Set("Authorization", "Bearer "+bearer)
	case c.username != "":
		req.SetBasicAuth(c.username, c.password)
	}
	return c.httpClient.Do(req)
}

var reChallengeParam = regexp.MustCompile(`(\w+)="([^"]*)"`)

// fetchBearerToken performs the token dance described by a Bearer challenge
// (realm/service/scope) and caches the result per scope.
func (c *Client) fetchBearerToken(challenge string) (string, error) {
	if !strings.HasPrefix(challenge, "Bearer ") {
		return "", fmt.Errorf("unsupported challenge %q", challenge)
	}
	params := map[string]string{}
	for _, m := range reChallengeParam.FindAllStringSubmatch(challenge, -1) {
		params[m[1]] = m[2]
	}
	realm := params["realm"]
	if realm == "" {
		return "", fmt.Errorf("challenge without realm: %q", challenge)
	}

	req, err := http.NewRequest(http.MethodGet, realm, nil)
	if err != nil {
		return "", err
	}
	q := req.URL.Query()
	if params["service"] != "" {
		q.Set("service", params["service"])
	}
	if params["scope"] != "" {
		q.Set("scope", params["scope"])
	}
	req.URL.RawQuery = q.Encode()
	if c.username != "" {
		req.SetBasicAuth(c.username, c.password)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return "", err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("token endpoint %s → %d", realm, resp.StatusCode)
	}
	var body struct {
		Token       string `json:"token"`
		AccessToken string `json:"access_token"`
		ExpiresIn   int    `json:"expires_in"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		return "", err
	}
	token := body.Token
	if token == "" {
		token = body.AccessToken
	}
	if token == "" {
		return "", fmt.Errorf("token endpoint returned no token")
	}

	ttl := time.Duration(body.ExpiresIn) * time.Second
	if ttl <= 0 {
		ttl = 60 * time.Second // Docker Hub default is 300s; stay conservative
	}
	if repo := repoFromScope(params["scope"]); repo != "" {
		c.mu.Lock()
		c.tokens[repo] = bearerToken{value: token, expires: time.Now().Add(ttl - 10*time.Second)}
		c.mu.Unlock()
	}
	return token, nil
}

// repoFromScope: "repository:library/alpine:pull" → "library/alpine"
func repoFromScope(scope string) string {
	parts := strings.Split(scope, ":")
	if len(parts) >= 2 && parts[0] == "repository" {
		return parts[1]
	}
	return ""
}

func (c *Client) cachedToken(repo string) string {
	if repo == "" {
		return ""
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if tok, ok := c.tokens[repo]; ok && time.Now().Before(tok.expires) {
		return tok.value
	}
	return ""
}

func (c *Client) Ping() error {
	resp, err := c.do(http.MethodGet, "/v2/", "")
	if err != nil {
		return err
	}
	_ = resp.Body.Close()
	return nil
}

func (c *Client) Catalog() ([]string, error) {
	resp, err := c.do(http.MethodGet, "/v2/_catalog", "application/json")
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()
	var cr catalogResponse
	if err := json.NewDecoder(resp.Body).Decode(&cr); err != nil {
		return nil, err
	}
	return cr.Repositories, nil
}

func (c *Client) Tags(name string) ([]string, error) {
	resp, err := c.do(http.MethodGet, "/v2/"+name+"/tags/list", "application/json")
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()
	var tr tagsResponse
	if err := json.NewDecoder(resp.Body).Decode(&tr); err != nil {
		return nil, err
	}
	return tr.Tags, nil
}

// acceptManifestTypes covers both single-platform manifests and multi-arch
// manifest lists / OCI indexes, so upstream returns whatever it actually stores
// under the ref instead of 404ing when the tag points at a manifest list.
const acceptManifestTypes = "application/vnd.docker.distribution.manifest.v2+json, " +
	"application/vnd.docker.distribution.manifest.list.v2+json, " +
	"application/vnd.oci.image.manifest.v1+json, " +
	"application/vnd.oci.image.index.v1+json"

// Manifest fetches the manifest for a given image name and tag/digest ref.
// The returned Manifest.Digest is populated from the Docker-Content-Digest header,
// which is the value required for deletion (immutable, unlike tags).
func (c *Client) Manifest(name, ref string) (*Manifest, error) {
	resp, err := c.do(http.MethodGet, "/v2/"+name+"/manifests/"+ref, acceptManifestTypes)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()
	var m Manifest
	if err := json.NewDecoder(resp.Body).Decode(&m); err != nil {
		return nil, err
	}
	m.Digest = resp.Header.Get("Docker-Content-Digest")
	return &m, nil
}

// RawManifest fetches a manifest's raw bytes and digest without decoding it into
// the single-platform Manifest struct — needed so callers can inspect multi-arch
// manifest lists / OCI indexes, which Manifest silently can't represent.
func (c *Client) RawManifest(name, ref string) ([]byte, string, error) {
	resp, err := c.do(http.MethodGet, "/v2/"+name+"/manifests/"+ref, acceptManifestTypes)
	if err != nil {
		return nil, "", err
	}
	defer func() { _ = resp.Body.Close() }()
	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, "", err
	}
	return raw, resp.Header.Get("Docker-Content-Digest"), nil
}

// Blob fetches a blob's raw content (used to read the image config for manifest details).
func (c *Client) Blob(name, digest string) ([]byte, error) {
	resp, err := c.do(http.MethodGet, "/v2/"+name+"/blobs/"+digest, "")
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()
	return io.ReadAll(resp.Body)
}

// BlobStream fetches a blob as a stream rather than buffering it fully — used
// for layers, which can be hundreds of MB. Caller must close the returned body.
func (c *Client) BlobStream(name, digest string) (io.ReadCloser, error) {
	resp, err := c.do(http.MethodGet, "/v2/"+name+"/blobs/"+digest, "")
	if err != nil {
		return nil, err
	}
	return resp.Body, nil
}

func (c *Client) DeleteManifest(name, digest string) error {
	resp, err := c.do(http.MethodDelete, "/v2/"+name+"/manifests/"+digest, "")
	if err != nil {
		return err
	}
	_ = resp.Body.Close()
	return nil
}
