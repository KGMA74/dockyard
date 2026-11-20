package store

import "encoding/json"

// OIDCSettings is the single sign-on configuration saved from the console.
// ClientSecretEnc is sealed by the auth package; the store never sees the
// secret in clear.
type OIDCSettings struct {
	Enabled         bool
	IssuerURL       string
	ClientID        string
	ClientSecretEnc string
	RedirectURL     string
	DisplayName     string
	Scopes          []string
	UsernameClaim   string
	GroupsClaim     string
	AdminGroups     []string
	PusherGroups    []string
	ReaderGroups    []string
	DefaultRole     string
	UpdatedBy       string
	UpdatedAt       string
}

func (s *Store) GetOIDCSettings() (*OIDCSettings, error) {
	var (
		o                                OIDCSettings
		scopes, admins, pushers, readers string
	)
	err := s.db.QueryRow(`SELECT enabled, issuer_url, client_id, client_secret_enc, redirect_url, display_name,
		scopes, username_claim, groups_claim, admin_groups, pusher_groups, reader_groups, default_role,
		updated_by, updated_at FROM oidc_settings WHERE id = 1`).Scan(
		&o.Enabled, &o.IssuerURL, &o.ClientID, &o.ClientSecretEnc, &o.RedirectURL, &o.DisplayName,
		&scopes, &o.UsernameClaim, &o.GroupsClaim, &admins, &pushers, &readers, &o.DefaultRole,
		&o.UpdatedBy, &o.UpdatedAt)
	if err != nil {
		return nil, err
	}
	for _, f := range []struct {
		src string
		dst *[]string
	}{{scopes, &o.Scopes}, {admins, &o.AdminGroups}, {pushers, &o.PusherGroups}, {readers, &o.ReaderGroups}} {
		if err := json.Unmarshal([]byte(f.src), f.dst); err != nil {
			return nil, err
		}
	}
	return &o, nil
}

// SaveOIDCSettings replaces the saved configuration; UpdatedAt is set here.
func (s *Store) SaveOIDCSettings(o *OIDCSettings) error {
	enc := func(list []string) string {
		if list == nil {
			list = []string{}
		}
		b, _ := json.Marshal(list)
		return string(b)
	}
	_, err := s.db.Exec(`UPDATE oidc_settings SET enabled = ?, issuer_url = ?, client_id = ?, client_secret_enc = ?,
		redirect_url = ?, display_name = ?, scopes = ?, username_claim = ?, groups_claim = ?, admin_groups = ?,
		pusher_groups = ?, reader_groups = ?, default_role = ?, updated_by = ?, updated_at = ? WHERE id = 1`,
		o.Enabled, o.IssuerURL, o.ClientID, o.ClientSecretEnc, o.RedirectURL, o.DisplayName,
		enc(o.Scopes), o.UsernameClaim, o.GroupsClaim, enc(o.AdminGroups), enc(o.PusherGroups), enc(o.ReaderGroups),
		o.DefaultRole, o.UpdatedBy, nowRFC3339())
	return err
}
