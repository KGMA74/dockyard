package server

import "time"

// scannerTokenSource is how the scan dispatcher gets the credential trivy
// pulls with, or nil when it should pull anonymously, as it always has:
//
//   - V2 auth off: Dockyard's registry asks for nothing.
//   - Proxy mode: the pull goes through to the upstream registry, which
//     Dockyard reaches with its own REGISTRY_USERNAME/REGISTRY_PASSWORD. A
//     Dockyard token there would mean nothing upstream — and with no
//     upstream credentials set, the reverse proxy forwards the client's
//     Authorization header as-is.
//
// Otherwise each scan gets its own token: the internal scanner identity,
// pull only, on the scanned repository alone, valid for the scan.
func (s *Server) scannerTokenSource() func(repo string, ttl time.Duration) (string, error) {
	if !s.v2AuthEnabled || s.mode == modeProxy {
		return nil
	}
	return s.auth.IssueScannerToken
}
