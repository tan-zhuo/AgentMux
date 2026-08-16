package store

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/zalando/go-keyring"
)

const (
	keyringService = "AgentMux"
	keyringUser    = "master-key"
	masterKeyFile  = "master.key"
)

// Cipher encrypts the handful of values that must never hit disk in the clear:
// SSH passwords and private-key passphrases.
type Cipher struct {
	aead cipher.AEAD
	// FellBackToFile reports that the OS keychain was unavailable and the master
	// key sits in a 0600 file instead. Surfaced in the UI so the weaker
	// guarantee is never silent.
	FellBackToFile bool
}

// NewCipher derives an AES-256-GCM cipher from a 32 byte master key.
func NewCipher(key []byte) (*Cipher, error) {
	if len(key) != 32 {
		return nil, fmt.Errorf("master key must be 32 bytes, got %d", len(key))
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	return &Cipher{aead: aead}, nil
}

// Seal returns nonce||ciphertext. An empty plaintext seals to nil so callers can
// treat "no secret" and "empty secret" identically.
func (c *Cipher) Seal(plain string) ([]byte, error) {
	if plain == "" {
		return nil, nil
	}
	nonce := make([]byte, c.aead.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return nil, err
	}
	return c.aead.Seal(nonce, nonce, []byte(plain), nil), nil
}

// Open reverses Seal.
func (c *Cipher) Open(blob []byte) (string, error) {
	if len(blob) == 0 {
		return "", nil
	}
	ns := c.aead.NonceSize()
	if len(blob) < ns {
		return "", errors.New("ciphertext too short")
	}
	out, err := c.aead.Open(nil, blob[:ns], blob[ns:], nil)
	if err != nil {
		return "", fmt.Errorf("decrypt failed (master key changed?): %w", err)
	}
	return string(out), nil
}

// LoadOrCreateMasterKey fetches the master key from the OS keychain (Credential
// Manager on Windows, Keychain on macOS, Secret Service on Linux), creating it
// on first run. If the keychain is unreachable — headless Linux, locked
// keyring — it falls back to a 0600 file and reports that fact.
func LoadOrCreateMasterKey(appDir string) (key []byte, usedFile bool, err error) {
	if k, err := keyring.Get(keyringService, keyringUser); err == nil {
		if raw, derr := base64.StdEncoding.DecodeString(k); derr == nil && len(raw) == 32 {
			return raw, false, nil
		}
		// Corrupt entry: fall through and mint a new one.
	} else if !errors.Is(err, keyring.ErrNotFound) {
		// Keychain itself is unavailable — use the file fallback.
		k, ferr := loadOrCreateKeyFile(appDir)
		return k, true, ferr
	}

	fresh := make([]byte, 32)
	if _, err := rand.Read(fresh); err != nil {
		return nil, false, err
	}
	if err := keyring.Set(keyringService, keyringUser, base64.StdEncoding.EncodeToString(fresh)); err != nil {
		k, ferr := loadOrCreateKeyFile(appDir)
		return k, true, ferr
	}
	return fresh, false, nil
}

func loadOrCreateKeyFile(appDir string) ([]byte, error) {
	path := filepath.Join(appDir, masterKeyFile)
	if b, err := os.ReadFile(path); err == nil {
		raw, derr := base64.StdEncoding.DecodeString(string(b))
		if derr == nil && len(raw) == 32 {
			return raw, nil
		}
		return nil, fmt.Errorf("master key file %s is corrupt", path)
	} else if !os.IsNotExist(err) {
		return nil, err
	}

	fresh := make([]byte, 32)
	if _, err := rand.Read(fresh); err != nil {
		return nil, err
	}
	enc := base64.StdEncoding.EncodeToString(fresh)
	if err := os.WriteFile(path, []byte(enc), 0o600); err != nil {
		return nil, err
	}
	return fresh, nil
}
