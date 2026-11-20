package auth

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/hkdf"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"strings"
)

// Secrets Dockyard must store and read back — today the SSO client secret
// set from the console — are sealed with AES-256-GCM under a key derived
// (HKDF-SHA256) from JWT_SECRET: a database copied without the deployment's
// secret reveals nothing. During a JWT_SECRET rotation the previous secret
// still opens what it sealed.

const sealPrefix = "v1:"

func sealKey(secret []byte, purpose string) ([]byte, error) {
	return hkdf.Key(sha256.New, secret, nil, "dockyard/"+purpose, 32)
}

func newGCM(key []byte) (cipher.AEAD, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	return cipher.NewGCM(block)
}

// seal encrypts plain for purpose, which is also bound as associated data:
// a value sealed for one use cannot be moved to another column.
func (m *Manager) seal(purpose, plain string) (string, error) {
	if plain == "" {
		return "", nil
	}
	key, err := sealKey(m.secrets[0], purpose)
	if err != nil {
		return "", err
	}
	gcm, err := newGCM(key)
	if err != nil {
		return "", err
	}
	nonce := make([]byte, gcm.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return "", err
	}
	out := gcm.Seal(nonce, nonce, []byte(plain), []byte(purpose))
	return sealPrefix + base64.RawStdEncoding.EncodeToString(out), nil
}

// unseal opens a value from seal. stale reports that it was sealed with a
// previous JWT secret and should be sealed again.
func (m *Manager) unseal(purpose, sealed string) (plain string, stale bool, err error) {
	if sealed == "" {
		return "", false, nil
	}
	body, ok := strings.CutPrefix(sealed, sealPrefix)
	if !ok {
		return "", false, errors.New("unknown sealed format")
	}
	data, err := base64.RawStdEncoding.DecodeString(body)
	if err != nil {
		return "", false, err
	}
	for i, secret := range m.secrets {
		key, err := sealKey(secret, purpose)
		if err != nil {
			return "", false, err
		}
		gcm, err := newGCM(key)
		if err != nil {
			return "", false, err
		}
		if len(data) < gcm.NonceSize() {
			return "", false, errors.New("sealed value too short")
		}
		out, err := gcm.Open(nil, data[:gcm.NonceSize()], data[gcm.NonceSize():], []byte(purpose))
		if err == nil {
			return string(out), i > 0, nil
		}
	}
	return "", false, errors.New("sealed with a JWT secret this instance no longer has")
}
