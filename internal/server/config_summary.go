package server

import (
	"net/http"
	"net/url"

	"dockyard/config"

	"github.com/labstack/echo/v4"
)

// ConfigItem is one effective setting, with the environment variable that
// controls it so an admin knows what to change (Helm values map onto these).
type ConfigItem struct {
	Key   string `json:"key"`
	Value any    `json:"value"`
	Env   string `json:"env,omitempty"`
}

// ConfigGroup is a titled block of settings.
type ConfigGroup struct {
	ID    string       `json:"id"`
	Items []ConfigItem `json:"items"`
}

// newConfigSummary keeps what an admin needs to understand how this instance
// behaves, and nothing that grants access: no passwords, keys, JWT secrets
// or S3 credentials, and credentials embedded in an upstream URL are cut.
func newConfigSummary(cfg *config.Config, mode mode, cosignKeys int, tracingEnabled bool, gcGrace string) []ConfigGroup {
	registry := []ConfigItem{{Key: "mode", Value: string(mode), Env: "REGISTRY_MODE"}}
	if mode == modeProxy || mode == modeMirror {
		registry = append(registry, ConfigItem{Key: "upstream", Value: redactURL(cfg.RegistryURL), Env: "REGISTRY_URL"})
	}
	if mode == modeMirror {
		registry = append(registry, ConfigItem{Key: "mirror_tag_ttl", Value: cfg.MirrorTagTTL.String(), Env: "MIRROR_TAG_TTL"})
	}
	if mode != modeProxy {
		registry = append(registry, ConfigItem{Key: "storage_backend", Value: string(cfg.StorageBackend), Env: "REGISTRY_STORAGE_BACKEND"})
		if cfg.StorageBackend == config.StorageS3 {
			endpoint := cfg.S3Endpoint
			if endpoint == "" {
				endpoint = "AWS S3"
			}
			registry = append(registry,
				ConfigItem{Key: "s3_bucket", Value: cfg.S3Bucket, Env: "S3_BUCKET"},
				ConfigItem{Key: "s3_endpoint", Value: endpoint, Env: "S3_ENDPOINT"},
			)
		}
	}
	registry = append(registry, ConfigItem{Key: "data_path", Value: cfg.StoragePath, Env: "REGISTRY_STORAGE_PATH"})

	tlsMode := cfg.TLSMode
	if tlsMode == "" {
		tlsMode = "off"
	}
	access := []ConfigItem{
		{Key: "v2_auth", Value: cfg.V2AuthEnabled, Env: "V2_AUTH_ENABLED"},
		{Key: "anonymous_pull", Value: cfg.V2AnonymousPull, Env: "V2_ANONYMOUS_PULL"},
		// Filled per request: SSO can be changed from the console.
		{Key: "sso", Value: "off", Env: "OIDC_ISSUER_URL"},
		{Key: "tls", Value: tlsMode, Env: "TLS_MODE"},
		{Key: "login_rate_limit", Value: cfg.RateLimitLoginPerMin, Env: "RATE_LIMIT_LOGIN_PER_MIN"},
		{Key: "global_rate_limit", Value: cfg.RateLimitGlobalRPS, Env: "RATE_LIMIT_GLOBAL_RPS"},
		{Key: "trusted_proxies", Value: nonNil(cfg.TrustedProxies), Env: "TRUSTED_PROXIES"},
		{Key: "cors_origins", Value: nonNil(cfg.CORSAllowedOrigins), Env: "CORS_ALLOWED_ORIGINS"},
	}

	scanner := "embedded"
	if cfg.TrivyServerURL != "" {
		scanner = redactURL(cfg.TrivyServerURL)
	}
	security := []ConfigItem{
		{Key: "scanning", Value: cfg.ScanEnabled, Env: "SCAN_ENABLED"},
	}
	if cfg.ScanEnabled {
		security = append(security,
			ConfigItem{Key: "scanner", Value: scanner, Env: "TRIVY_SERVER_URL"},
			ConfigItem{Key: "scan_offline", Value: cfg.TrivyOffline, Env: "TRIVY_OFFLINE"},
		)
	}
	security = append(security,
		ConfigItem{Key: "require_signed_push", Value: cfg.RequireSignedPush, Env: "REQUIRE_SIGNED_PUSH"},
		ConfigItem{Key: "cosign_keys", Value: cosignKeys, Env: "COSIGN_PUBLIC_KEYS_DIR"},
	)

	return []ConfigGroup{
		{ID: "registry", Items: registry},
		{ID: "access", Items: access},
		{ID: "security", Items: security},
		{ID: "maintenance", Items: []ConfigItem{
			{Key: "maintenance_schedule", Value: "00:00 UTC"},
			{Key: "gc_grace_period", Value: gcGrace, Env: "GC_GRACE_PERIOD"},
		}},
		{ID: "observability", Items: []ConfigItem{
			{Key: "metrics", Value: cfg.MetricsEnabled, Env: "METRICS_ENABLED"},
			{Key: "tracing", Value: tracingEnabled, Env: "OTEL_EXPORTER_OTLP_ENDPOINT"},
		}},
	}
}

// redactURL drops user:password@ from a URL; unparsable input is hidden
// entirely rather than echoed back.
func redactURL(raw string) string {
	if raw == "" {
		return ""
	}
	u, err := url.Parse(raw)
	if err != nil {
		return "(invalid URL)"
	}
	u.User = nil
	return u.String()
}

func nonNil(list []string) []string {
	if list == nil {
		return []string{}
	}
	return list
}

// GET /api/admin/config (admin only)
func (s *Server) handleConfig(c echo.Context) error {
	// Copy before patching: s.configSummary is shared by every request.
	groups := make([]ConfigGroup, len(s.configSummary))
	for i, g := range s.configSummary {
		g.Items = append([]ConfigItem(nil), g.Items...)
		for j, item := range g.Items {
			if item.Key == "sso" && s.auth != nil {
				issuer, managedBy := s.auth.OIDCSummary()
				if issuer != "off" {
					issuer = redactURL(issuer)
				}
				g.Items[j].Value = issuer
				if managedBy != "env" {
					g.Items[j].Env = "" // set in Settings → Single sign-on
				}
			}
		}
		groups[i] = g
	}
	return c.JSON(http.StatusOK, map[string]any{"groups": groups})
}
