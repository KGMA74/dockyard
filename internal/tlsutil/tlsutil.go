// Package tlsutil builds the server's TLS configuration for the three native
// modes: static cert files, a persisted self-signed cert, or ACME
// (Let's Encrypt) via autocert with the TLS-ALPN-01 challenge.
package tlsutil

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"fmt"
	"math/big"
	"net"
	"os"
	"path/filepath"
	"slices"
	"time"

	"golang.org/x/crypto/acme/autocert"
)

const (
	ModeOff        = "off"
	ModeStatic     = "static"
	ModeSelfSigned = "self-signed"
	ModeACME       = "acme"
)

type Options struct {
	Mode      string
	CertFile  string // static
	KeyFile   string // static
	Domain    string // self-signed SAN + acme host whitelist
	ACMEEmail string
	Dir       string // persistence dir (<StoragePath>/tls)
}

// Config returns the tls.Config for the requested mode, or nil for ModeOff.
func Config(o Options) (*tls.Config, error) {
	switch o.Mode {
	case "", ModeOff:
		return nil, nil

	case ModeStatic:
		if o.CertFile == "" || o.KeyFile == "" {
			return nil, fmt.Errorf("TLS_MODE=static requires TLS_CERT_FILE and TLS_KEY_FILE")
		}
		cert, err := tls.LoadX509KeyPair(o.CertFile, o.KeyFile)
		if err != nil {
			return nil, fmt.Errorf("load static cert: %w", err)
		}
		return &tls.Config{MinVersion: tls.VersionTLS12, Certificates: []tls.Certificate{cert}}, nil

	case ModeSelfSigned:
		cert, err := selfSigned(o.Dir, o.Domain)
		if err != nil {
			return nil, err
		}
		return &tls.Config{MinVersion: tls.VersionTLS12, Certificates: []tls.Certificate{*cert}}, nil

	case ModeACME:
		if o.Domain == "" {
			return nil, fmt.Errorf("TLS_MODE=acme requires TLS_DOMAIN")
		}
		m := &autocert.Manager{
			Prompt:     autocert.AcceptTOS,
			HostPolicy: autocert.HostWhitelist(o.Domain),
			Cache:      autocert.DirCache(filepath.Join(o.Dir, "acme")),
			Email:      o.ACMEEmail,
		}
		// TLS-ALPN-01: the challenge is answered on the TLS port itself, so
		// the server must be reachable from the internet on :443.
		cfg := m.TLSConfig()
		cfg.MinVersion = tls.VersionTLS12
		return cfg, nil

	default:
		return nil, fmt.Errorf("unknown TLS_MODE %q (off, static, self-signed, acme)", o.Mode)
	}
}

// selfSigned loads the persisted certificate from dir when still valid for
// domain, generating (and persisting) a fresh one otherwise.
func selfSigned(dir, domain string) (*tls.Certificate, error) {
	certPath := filepath.Join(dir, "cert.pem")
	keyPath := filepath.Join(dir, "key.pem")

	if cert, err := tls.LoadX509KeyPair(certPath, keyPath); err == nil {
		if leaf, err := x509.ParseCertificate(cert.Certificate[0]); err == nil &&
			time.Now().Before(leaf.NotAfter.Add(-30*24*time.Hour)) &&
			coversDomain(leaf, domain) {
			return &cert, nil
		}
	}

	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return nil, err
	}
	serial, err := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 128))
	if err != nil {
		return nil, err
	}
	tmpl := x509.Certificate{
		SerialNumber:          serial,
		Subject:               pkix.Name{CommonName: "Dockyard Registry", Organization: []string{"Dockyard"}},
		NotBefore:             time.Now().Add(-time.Hour),
		NotAfter:              time.Now().AddDate(10, 0, 0),
		KeyUsage:              x509.KeyUsageDigitalSignature | x509.KeyUsageKeyEncipherment,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		BasicConstraintsValid: true,
		DNSNames:              []string{"localhost"},
		IPAddresses:           []net.IP{net.ParseIP("127.0.0.1"), net.ParseIP("::1")},
	}
	if domain != "" {
		if ip := net.ParseIP(domain); ip != nil {
			tmpl.IPAddresses = append(tmpl.IPAddresses, ip)
		} else {
			tmpl.DNSNames = append(tmpl.DNSNames, domain)
		}
	}

	der, err := x509.CreateCertificate(rand.Reader, &tmpl, &tmpl, &key.PublicKey, key)
	if err != nil {
		return nil, err
	}
	keyDER, err := x509.MarshalECPrivateKey(key)
	if err != nil {
		return nil, err
	}
	certPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})
	keyPEM := pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: keyDER})

	if err := os.MkdirAll(dir, 0700); err != nil {
		return nil, err
	}
	if err := os.WriteFile(certPath, certPEM, 0644); err != nil {
		return nil, err
	}
	if err := os.WriteFile(keyPath, keyPEM, 0600); err != nil {
		return nil, err
	}

	cert, err := tls.X509KeyPair(certPEM, keyPEM)
	if err != nil {
		return nil, err
	}
	return &cert, nil
}

func coversDomain(leaf *x509.Certificate, domain string) bool {
	if domain == "" {
		return true
	}
	if ip := net.ParseIP(domain); ip != nil {
		return slices.ContainsFunc(leaf.IPAddresses, ip.Equal)
	}
	return slices.Contains(leaf.DNSNames, domain)
}
