// Package secret encrypts credentials NivaroOS has to store in a usable
// form (a network share's password must be replayed to mount it, so it
// can't be hashed). Values are sealed with AES-256-GCM under one host key
// kept in a root-only file; a copied database alone reveals nothing.
package secret

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/F-e-n-y-x/NivaroOS/services/common/utils/constants"
)

// DefaultKeyPath is the host key: 32 random bytes, hex, mode 0600, owned by
// root. It is created by the service that first needs it (core) and only
// read by the others (backup).
var DefaultKeyPath = filepath.Join(constants.DefaultConfigPath, "secret.key")

// ConnectionPassword is the context of a saved network-share password
// (core's o_connections.password; the backup service reads it too).
const ConnectionPassword = "o_connections.password"

// prefix marks a sealed value; anything without it is legacy plaintext.
const prefix = "enc:v1:"

// ErrNoKey: a sealed value was met but the host key doesn't exist.
var ErrNoKey = errors.New("the host secret key is missing")

// Box seals and opens values under one key.
type Box struct{ aead cipher.AEAD }

// New returns a Box for a 32-byte key.
func New(key []byte) (*Box, error) {
	if len(key) != 32 {
		return nil, fmt.Errorf("secret key must be 32 bytes, got %d", len(key))
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	return &Box{aead: aead}, nil
}

// IsSealed reports whether v was produced by Seal.
func IsSealed(v string) bool { return strings.HasPrefix(v, prefix) }

// Seal encrypts plain. context binds the value to where it is stored (e.g.
// "o_connections.password"), so a sealed value copied into another field
// doesn't open there. An empty value stays empty.
func (b *Box) Seal(plain, context string) (string, error) {
	if plain == "" {
		return "", nil
	}
	nonce := make([]byte, b.aead.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return "", err
	}
	out := b.aead.Seal(nonce, nonce, []byte(plain), []byte(context))
	return prefix + base64.RawStdEncoding.EncodeToString(out), nil
}

// Open decrypts a sealed value. A value that isn't sealed (stored before
// encryption existed) is returned as it is, so callers can read rows the
// migration hasn't reached yet.
func (b *Box) Open(v, context string) (string, error) {
	if !IsSealed(v) {
		return v, nil
	}
	raw, err := base64.RawStdEncoding.DecodeString(strings.TrimPrefix(v, prefix))
	if err != nil {
		return "", fmt.Errorf("sealed value is damaged: %w", err)
	}
	n := b.aead.NonceSize()
	if len(raw) < n+b.aead.Overhead() {
		return "", errors.New("sealed value is damaged: too short")
	}
	plain, err := b.aead.Open(nil, raw[:n], raw[n:], []byte(context))
	if err != nil {
		return "", errors.New("sealed value can't be opened with this host's key")
	}
	return string(plain), nil
}

func readKey(path string) ([]byte, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	key, err := hex.DecodeString(strings.TrimSpace(string(raw)))
	if err != nil {
		return nil, fmt.Errorf("%s is not a valid key: %w", path, err)
	}
	return key, nil
}

// Load opens the key at path without creating it (for services that only
// read secrets another service stores).
func Load(path string) (*Box, error) {
	key, err := readKey(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, ErrNoKey
	}
	if err != nil {
		return nil, err
	}
	return New(key)
}

// LoadOrCreate opens the key at path, creating it (0600, in a directory it
// creates 0755) on first run. An existing key readable by others is made
// 0600 again.
func LoadOrCreate(path string) (*Box, error) {
	if _, err := os.Stat(path); errors.Is(err, os.ErrNotExist) {
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			return nil, err
		}
		key := make([]byte, 32)
		if _, err := io.ReadFull(rand.Reader, key); err != nil {
			return nil, err
		}
		// O_EXCL: if another process created it meanwhile, use theirs.
		f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
		if err == nil {
			_, werr := f.WriteString(hex.EncodeToString(key) + "\n")
			serr := f.Sync()
			cerr := f.Close()
			if werr != nil || serr != nil || cerr != nil {
				os.Remove(path)
				return nil, fmt.Errorf("writing %s: %v", path, errors.Join(werr, serr, cerr))
			}
		} else if !errors.Is(err, os.ErrExist) {
			return nil, err
		}
	}
	if fi, err := os.Stat(path); err == nil && fi.Mode().Perm()&0o077 != 0 {
		if err := os.Chmod(path, 0o600); err != nil {
			return nil, err
		}
	}
	return Load(path)
}

var (
	defaultMu  sync.Mutex
	defaultBox *Box
)

// Default returns the Box for DefaultKeyPath, read-only (see Load), cached
// once it loaded.
func Default() (*Box, error) {
	defaultMu.Lock()
	defer defaultMu.Unlock()
	if defaultBox != nil {
		return defaultBox, nil
	}
	b, err := Load(DefaultKeyPath)
	if err != nil {
		return nil, err
	}
	defaultBox = b
	return b, nil
}
