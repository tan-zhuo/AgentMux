// Package portable carries an AgentMux configuration from one machine to
// another: the hosts, the tree that hangs off them, and — when asked for — the
// skill library and the settings that make the app feel the same on the other
// side.
//
// The file is encrypted with a passphrase rather than with this machine's
// master key. The master key lives in this computer's keychain and cannot
// travel, which is the whole reason a separate format exists: a copy of the
// database would be unreadable on the machine it was carried to.
package portable

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"golang.org/x/crypto/argon2"
)

// FileFormat identifies the envelope, so a file from a later version is
// refused with a sentence rather than misread.
const FileFormat = "agentmux.config/v1"

// MinPassphrase is the shortest passphrase that will be accepted.
//
// The file may carry SSH passwords and key passphrases, and once it is on a USB
// stick or in a chat window the only thing standing between those and whoever
// has it is this. Eight characters is not a strong passphrase; it is the point
// below which the encryption is theatre.
const MinPassphrase = 8

const (
	kdfArgon2id  = "argon2id"
	saltLen      = 16
	keyLen       = 32
	argonTime    = 3
	argonMemory  = 64 * 1024 // KiB
	argonThreads = 4
)

// envelope is the file on disk: a readable header and one sealed blob.
//
// JSON rather than a packed binary layout, because a file people move between
// machines by hand should be identifiable by looking at it — and because the
// KDF parameters have to travel in the clear for the passphrase to be turned
// back into a key at all. They are authenticated, not hidden: the header is
// fed to the AEAD as additional data, so changing the cost parameters to
// something cheap invalidates the file instead of weakening it.
type envelope struct {
	Format     string `json:"format"`
	KDF        string `json:"kdf"`
	Time       uint32 `json:"time"`
	Memory     uint32 `json:"memory"`
	Threads    uint8  `json:"threads"`
	Salt       string `json:"salt"`
	Nonce      string `json:"nonce"`
	Ciphertext string `json:"ciphertext"`
	ExportedAt int64  `json:"exportedAt"`
}

// aad is the authenticated header, built by hand rather than re-marshalled.
// JSON field order is stable for a struct but not something to bet a file
// format on, and the bytes here have to be identical on the way out and back.
func (e envelope) aad() []byte {
	return []byte(fmt.Sprintf("%s|%s|%d|%d|%d|%s|%d",
		e.Format, e.KDF, e.Time, e.Memory, e.Threads, e.Salt, e.ExportedAt))
}

// Seal encrypts a payload under a passphrase and returns the file's bytes.
func Seal(plain []byte, passphrase string, exportedAt int64) ([]byte, error) {
	if err := CheckPassphrase(passphrase); err != nil {
		return nil, err
	}
	salt := make([]byte, saltLen)
	if _, err := rand.Read(salt); err != nil {
		return nil, err
	}
	env := envelope{
		Format:     FileFormat,
		KDF:        kdfArgon2id,
		Time:       argonTime,
		Memory:     argonMemory,
		Threads:    argonThreads,
		Salt:       base64.StdEncoding.EncodeToString(salt),
		ExportedAt: exportedAt,
	}
	aead, err := aeadFor(passphrase, salt, env.Time, env.Memory, env.Threads)
	if err != nil {
		return nil, err
	}
	nonce := make([]byte, aead.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return nil, err
	}
	env.Nonce = base64.StdEncoding.EncodeToString(nonce)
	env.Ciphertext = base64.StdEncoding.EncodeToString(aead.Seal(nil, nonce, plain, env.aad()))
	return json.MarshalIndent(env, "", "  ")
}

// ErrPassphrase is what a wrong passphrase looks like, so callers can say so
// instead of reporting a decryption failure nobody can act on.
var ErrPassphrase = errors.New("the passphrase does not open this file")

// Open reverses Seal.
//
// A failed unseal cannot tell a wrong passphrase from a tampered file — that is
// what makes the authentication work — so it reports the one the person can do
// something about, and says the other is also possible.
func Open(file []byte, passphrase string) ([]byte, error) {
	var env envelope
	if err := json.Unmarshal(file, &env); err != nil {
		return nil, errors.New("this does not look like an AgentMux configuration file")
	}
	if env.Format != FileFormat {
		return nil, fmt.Errorf("unknown configuration format %q — this file was written by a different version", env.Format)
	}
	if env.KDF != kdfArgon2id {
		return nil, fmt.Errorf("unknown key derivation %q", env.KDF)
	}
	if strings.TrimSpace(passphrase) == "" {
		return nil, errors.New("a passphrase is required")
	}
	salt, err := base64.StdEncoding.DecodeString(env.Salt)
	if err != nil || len(salt) == 0 {
		return nil, errors.New("the file's salt is unreadable")
	}
	nonce, err := base64.StdEncoding.DecodeString(env.Nonce)
	if err != nil {
		return nil, errors.New("the file's nonce is unreadable")
	}
	blob, err := base64.StdEncoding.DecodeString(env.Ciphertext)
	if err != nil {
		return nil, errors.New("the file's contents are unreadable")
	}
	aead, err := aeadFor(passphrase, salt, env.Time, env.Memory, env.Threads)
	if err != nil {
		return nil, err
	}
	if len(nonce) != aead.NonceSize() {
		return nil, errors.New("the file's nonce is the wrong size")
	}
	out, err := aead.Open(nil, nonce, blob, env.aad())
	if err != nil {
		return nil, ErrPassphrase
	}
	return out, nil
}

// ExportedAt reads the timestamp out of a file without opening it, so a picker
// can say when the file was written before anyone types a passphrase.
func ExportedAt(file []byte) int64 {
	var env envelope
	if err := json.Unmarshal(file, &env); err != nil {
		return 0
	}
	return env.ExportedAt
}

// CheckPassphrase applies the one rule there is, in the words the person typing
// needs to hear.
func CheckPassphrase(passphrase string) error {
	if len([]rune(passphrase)) < MinPassphrase {
		return fmt.Errorf("the passphrase needs at least %d characters — this file may carry your passwords", MinPassphrase)
	}
	return nil
}

func aeadFor(passphrase string, salt []byte, time, memory uint32, threads uint8) (cipher.AEAD, error) {
	if time == 0 || memory == 0 || threads == 0 {
		return nil, errors.New("the file's key derivation parameters are not usable")
	}
	key := argon2.IDKey([]byte(passphrase), salt, time, memory, threads, keyLen)
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	return cipher.NewGCM(block)
}
