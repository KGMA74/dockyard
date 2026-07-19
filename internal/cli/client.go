// Package cli implements dockyard-cli, a thin client for the admin API.
// Credentials live in ~/.dockyard/config.json; a 401 triggers one silent
// refresh (single-use rotating refresh tokens) before failing.
package cli

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"
)

type Config struct {
	Server       string `json:"server"`
	Token        string `json:"token"`
	RefreshToken string `json:"refresh_token"`
}

func configPath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".dockyard", "config.json"), nil
}

func LoadConfig() (*Config, error) {
	path, err := configPath()
	if err != nil {
		return nil, err
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("not logged in — run: dockyard-cli login <server> -u <user> -p <password>")
	}
	var cfg Config
	if err := json.Unmarshal(raw, &cfg); err != nil {
		return nil, err
	}
	return &cfg, nil
}

func (c *Config) Save() error {
	path, err := configPath()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return err
	}
	raw, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, raw, 0600)
}

type Client struct {
	cfg  *Config
	http *http.Client
}

func NewClient(cfg *Config) *Client {
	return &Client{cfg: cfg, http: &http.Client{Timeout: 5 * time.Minute}}
}

// Login authenticates and persists the session.
func Login(server, username, password string) error {
	server = strings.TrimRight(server, "/")
	body, _ := json.Marshal(map[string]string{"username": username, "password": password})
	resp, err := http.Post(server+"/api/admin/auth/login", "application/json", bytes.NewReader(body))
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return apiError(resp)
	}
	var tokens struct {
		Token        string `json:"token"`
		RefreshToken string `json:"refresh_token"`
		Role         string `json:"role"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&tokens); err != nil {
		return err
	}
	cfg := &Config{Server: server, Token: tokens.Token, RefreshToken: tokens.RefreshToken}
	if err := cfg.Save(); err != nil {
		return err
	}
	fmt.Printf("Logged in to %s as %s (%s)\n", server, username, tokens.Role)
	return nil
}

func (c *Client) refresh() error {
	body, _ := json.Marshal(map[string]string{"refresh_token": c.cfg.RefreshToken})
	resp, err := http.Post(c.cfg.Server+"/api/admin/auth/refresh", "application/json", bytes.NewReader(body))
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("session expired — log in again")
	}
	var tokens struct {
		Token        string `json:"token"`
		RefreshToken string `json:"refresh_token"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&tokens); err != nil {
		return err
	}
	c.cfg.Token = tokens.Token
	c.cfg.RefreshToken = tokens.RefreshToken
	return c.cfg.Save()
}

// Do performs an authenticated request, refreshing the token once on 401.
func (c *Client) Do(method, path string, body io.Reader, contentType string) (*http.Response, error) {
	var buffered []byte
	if body != nil {
		var err error
		if buffered, err = io.ReadAll(body); err != nil {
			return nil, err
		}
	}
	attempt := func() (*http.Response, error) {
		var reader io.Reader
		if buffered != nil {
			reader = bytes.NewReader(buffered)
		}
		req, err := http.NewRequest(method, c.cfg.Server+path, reader)
		if err != nil {
			return nil, err
		}
		req.Header.Set("Authorization", "Bearer "+c.cfg.Token)
		if contentType != "" {
			req.Header.Set("Content-Type", contentType)
		}
		return c.http.Do(req)
	}

	resp, err := attempt()
	if err != nil {
		return nil, err
	}
	if resp.StatusCode == http.StatusUnauthorized {
		_ = resp.Body.Close()
		if err := c.refresh(); err != nil {
			return nil, err
		}
		return attempt()
	}
	return resp, nil
}

// GetJSON fetches path and decodes the response into out.
func (c *Client) GetJSON(path string, out any) error {
	resp, err := c.Do(http.MethodGet, path, nil, "")
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return apiError(resp)
	}
	return json.NewDecoder(resp.Body).Decode(out)
}

// JSON performs a mutating call with an optional JSON body.
func (c *Client) JSON(method, path string, payload any, out any) error {
	var body io.Reader
	contentType := ""
	if payload != nil {
		raw, err := json.Marshal(payload)
		if err != nil {
			return err
		}
		body = bytes.NewReader(raw)
		contentType = "application/json"
	}
	resp, err := c.Do(method, path, body, contentType)
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode >= 300 {
		return apiError(resp)
	}
	if out != nil {
		return json.NewDecoder(resp.Body).Decode(out)
	}
	return nil
}

func apiError(resp *http.Response) error {
	var body struct {
		Error string `json:"error"`
	}
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
	if json.Unmarshal(raw, &body) == nil && body.Error != "" {
		return fmt.Errorf("%s (HTTP %d)", body.Error, resp.StatusCode)
	}
	return fmt.Errorf("HTTP %d", resp.StatusCode)
}
