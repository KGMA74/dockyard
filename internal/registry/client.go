package registry

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

type Client struct {
	baseURL    string
	username   string
	password   string
	httpClient *http.Client
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
	}
}

func (c *Client) BaseURL() string  { return c.baseURL }
func (c *Client) Username() string { return c.username }
func (c *Client) Password() string { return c.password }

func (c *Client) do(method, path, accept string) (*http.Response, error) {
	req, err := http.NewRequest(method, c.baseURL+path, nil)
	if err != nil {
		return nil, err
	}
	if accept != "" {
		req.Header.Set("Accept", accept)
	}
	if c.username != "" {
		req.SetBasicAuth(c.username, c.password)
	}
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode >= 400 {
		resp.Body.Close()
		return nil, fmt.Errorf("registry error: %s %s → %d", method, path, resp.StatusCode)
	}
	return resp, nil
}

func (c *Client) Ping() error {
	resp, err := c.do(http.MethodGet, "/v2/", "")
	if err != nil {
		return err
	}
	resp.Body.Close()
	return nil
}

func (c *Client) Catalog() ([]string, error) {
	resp, err := c.do(http.MethodGet, "/v2/_catalog", "application/json")
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
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
	defer resp.Body.Close()
	var tr tagsResponse
	if err := json.NewDecoder(resp.Body).Decode(&tr); err != nil {
		return nil, err
	}
	return tr.Tags, nil
}

// Manifest fetches the manifest for a given image name and tag/digest ref.
// The returned Manifest.Digest is populated from the Docker-Content-Digest header,
// which is the value required for deletion (immutable, unlike tags).
func (c *Client) Manifest(name, ref string) (*Manifest, error) {
	const mediaType = "application/vnd.docker.distribution.manifest.v2+json"
	resp, err := c.do(http.MethodGet, "/v2/"+name+"/manifests/"+ref, mediaType)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	var m Manifest
	if err := json.NewDecoder(resp.Body).Decode(&m); err != nil {
		return nil, err
	}
	m.Digest = resp.Header.Get("Docker-Content-Digest")
	return &m, nil
}

// Blob fetches a blob's raw content (used to read the image config for manifest details).
func (c *Client) Blob(name, digest string) ([]byte, error) {
	resp, err := c.do(http.MethodGet, "/v2/"+name+"/blobs/"+digest, "")
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	return io.ReadAll(resp.Body)
}

func (c *Client) DeleteManifest(name, digest string) error {
	resp, err := c.do(http.MethodDelete, "/v2/"+name+"/manifests/"+digest, "")
	if err != nil {
		return err
	}
	resp.Body.Close()
	return nil
}
