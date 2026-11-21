package scan

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

// scratchDir is trivy's TMPDIR: a subdirectory of the cache, so it sits on
// the persistent volume. The final image runs FROM scratch, with no /tmp, and
// trivy stages large downloads there (the Java DB is ~1 GB compressed).
func scratchDir(cacheDir string) string {
	if cacheDir == "" {
		return os.TempDir()
	}
	return filepath.Join(cacheDir, "tmp")
}

// resetScratchDir empties the scratch dir and recreates it. Called when the
// dispatcher starts, before any trivy process runs: it removes what a killed
// trivy left behind, and the registry auth of a scan interrupted by a crash
// (its token has expired by then anyway).
func resetScratchDir(cacheDir string) error {
	if cacheDir == "" {
		return nil
	}
	dir := scratchDir(cacheDir)
	if err := os.RemoveAll(dir); err != nil {
		return err
	}
	return os.MkdirAll(dir, 0o700)
}

// dockerConfig is the subset of ~/.docker/config.json trivy reads through
// go-containerregistry's default keychain.
type dockerConfig struct {
	Auths map[string]dockerAuth `json:"auths"`
}

type dockerAuth struct {
	RegistryToken string `json:"registrytoken"`
}

// writeRegistryAuth creates a private DOCKER_CONFIG directory under parent
// for one scan, holding token for registry (host:port) and nothing else, and
// returns its path. The caller removes it once trivy exits.
//
// Why a docker config and not TRIVY_REGISTRY_TOKEN: trivy applies that
// variable to every registry it talks to, so the token also went to
// ghcr.io when trivy fetched the Java DB, which answered DENIED. Through the
// keychain, go-containerregistry looks up the exact registry host, uses the
// registrytoken as the Bearer header without a /v2/token round trip, and
// stays anonymous for any other host.
func writeRegistryAuth(parent, registry, token string) (string, error) {
	if err := os.MkdirAll(parent, 0o700); err != nil {
		return "", fmt.Errorf("scan: registry auth dir: %w", err)
	}
	dir, err := os.MkdirTemp(parent, "registry-auth-") // mode 0700
	if err != nil {
		return "", fmt.Errorf("scan: registry auth dir: %w", err)
	}
	raw, err := json.Marshal(dockerConfig{Auths: map[string]dockerAuth{registry: {RegistryToken: token}}})
	if err != nil {
		_ = os.RemoveAll(dir)
		return "", err
	}
	f, err := os.OpenFile(filepath.Join(dir, "config.json"), os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		_ = os.RemoveAll(dir)
		return "", fmt.Errorf("scan: registry auth file: %w", err)
	}
	_, werr := f.Write(raw)
	cerr := f.Close()
	if werr != nil || cerr != nil {
		_ = os.RemoveAll(dir)
		return "", fmt.Errorf("scan: registry auth file: %w", errors.Join(werr, cerr))
	}
	return dir, nil
}
