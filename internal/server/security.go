package server

import (
	"fmt"
	"net"
	"net/url"
	"strings"

	"github.com/labstack/echo/v4"
)

// clientIPExtractor builds the function that decides which address a request
// "comes from" — the key for rate limiting and the IP written to the audit
// log.
//
// Echo's default believes X-Forwarded-For from anyone. That header is plain
// client input: with the default, ten wrong passwords per minute became
// unlimited by sending a different fake address each time, and audit entries
// could be made to point at any IP. Here the header only counts when the
// direct peer is a proxy we trust:
//
//   - no TRUSTED_PROXIES: loopback, link-local and private ranges (a reverse
//     proxy or ingress next to Dockyard). A client connecting straight from a
//     public address is identified by its socket address, full stop.
//   - TRUSTED_PROXIES set: exactly those CIDR ranges, nothing else.
func clientIPExtractor(trustedProxies []string) (echo.IPExtractor, error) {
	if len(trustedProxies) == 0 {
		return echo.ExtractIPFromXFFHeader(), nil
	}
	opts := []echo.TrustOption{
		echo.TrustLoopback(false),
		echo.TrustLinkLocal(false),
		echo.TrustPrivateNet(false),
	}
	for _, entry := range trustedProxies {
		cidr := entry
		if !strings.Contains(cidr, "/") { // bare address → single-host range
			if strings.Contains(cidr, ":") {
				cidr += "/128"
			} else {
				cidr += "/32"
			}
		}
		_, network, err := net.ParseCIDR(cidr)
		if err != nil {
			return nil, fmt.Errorf("TRUSTED_PROXIES: %q is not an IP address or CIDR range", entry)
		}
		opts = append(opts, echo.TrustIPRange(network))
	}
	return echo.ExtractIPFromXFFHeader(opts...), nil
}

// sensitiveQueryParams never reach the logs. The SSE feed authenticates with
// ?token=<JWT> (EventSource cannot set headers), and the request logger used
// to print that URL verbatim — every log line of every open dashboard carried
// a live admin token.
var sensitiveQueryParams = []string{"token", "access_token", "refresh_token", "password", "secret"}

// redactURI masks credentials carried in a request URI's query string.
func redactURI(uri string) string {
	path, rawQuery, hasQuery := strings.Cut(uri, "?")
	if !hasQuery || rawQuery == "" {
		return uri
	}
	query, err := url.ParseQuery(rawQuery)
	if err != nil {
		// Unparseable query: log the path only rather than risk a leak.
		return path + "?[unparseable query redacted]"
	}
	redacted := false
	for key := range query {
		for _, sensitive := range sensitiveQueryParams {
			if strings.EqualFold(key, sensitive) {
				query.Set(key, "REDACTED")
				redacted = true
			}
		}
	}
	if !redacted {
		return uri
	}
	return path + "?" + query.Encode()
}

// contentSecurityPolicy is served with the UI and the admin API. The UI keeps
// its session tokens in localStorage, so script injection is the threat that
// matters: only same-origin scripts run, nothing may frame the app, and the
// page can only talk back to its own origin. Inline *styles* stay allowed —
// the component library positions popovers with style attributes.
const contentSecurityPolicy = "default-src 'self'; " +
	"script-src 'self'; " +
	"style-src 'self' 'unsafe-inline'; " +
	"img-src 'self' data: blob:; " +
	"font-src 'self' data:; " +
	"connect-src 'self'; " +
	"object-src 'none'; " +
	"base-uri 'self'; " +
	"form-action 'self'; " +
	"frame-ancestors 'none'"

// securityHeaders hardens every non-registry response (UI, admin API,
// health). Registry (/v2) traffic never reaches it: that is consumed by
// container runtimes, not browsers.
func securityHeaders(next echo.HandlerFunc) echo.HandlerFunc {
	return func(c echo.Context) error {
		h := c.Response().Header()
		h.Set("X-Content-Type-Options", "nosniff")
		h.Set("X-Frame-Options", "DENY")
		h.Set("Referrer-Policy", "no-referrer")
		h.Set("Content-Security-Policy", contentSecurityPolicy)
		if c.Request().TLS != nil {
			h.Set("Strict-Transport-Security", "max-age=31536000")
		}
		return next(c)
	}
}
