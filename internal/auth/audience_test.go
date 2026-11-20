package auth

import (
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

// Every token Dockyard signs shares one key; the audience is what keeps a
// token minted for one purpose from being replayed for another.
func TestValidateRefusesForeignAudiences(t *testing.T) {
	m := newManager(t)
	sign := func(claims jwt.MapClaims) string {
		claims["exp"] = time.Now().Add(time.Minute).Unix()
		raw, err := jwt.NewWithClaims(jwt.SigningMethodHS256, claims).SignedString(m.secrets[0])
		if err != nil {
			t.Fatal(err)
		}
		return raw
	}

	for name, claims := range map[string]jwt.MapClaims{
		"oidc flow cookie": {"aud": audOIDCFlow, "sub": "admin", "st": "x"},
		"unknown audience": {"aud": "someone-else", "sub": "admin"},
		"two audiences":    {"aud": []string{audRegistry, "someone-else"}, "sub": "admin"},
	} {
		if _, err := m.validate(sign(claims)); err == nil {
			t.Errorf("%s: accepted, want refused", name)
		}
	}

	p, err := m.validate(sign(jwt.MapClaims{"aud": audRegistry, "sub": "admin"}))
	if err != nil || !p.Registry || p.consoleAllowed() {
		t.Fatalf("registry token: %+v, %v — want a registry-only principal", p, err)
	}
	p, err = m.validate(sign(jwt.MapClaims{"sub": "admin"}))
	if err != nil || p.Registry || !p.consoleAllowed() {
		t.Fatalf("console token: %+v, %v — want a console principal", p, err)
	}
}
