// Package secretbox encrypts small secrets (Knox client secret, AD bind password) for storage.
package secretbox

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"io/fs"
	"os"
)

const keyLen = 32

// ErrInvalid means the ciphertext is malformed, was tampered with, or was sealed with another key.
var ErrInvalid = errors.New("secretbox: cannot decrypt")

type Box struct{ aead cipher.AEAD }

// New builds a Box from a 32-byte key.
func New(key []byte) (*Box, error) {
	if len(key) != keyLen {
		return nil, fmt.Errorf("secretbox: key must be %d bytes", keyLen)
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

// LoadOrCreate reads the key file, creating it (random, mode 0600, exclusive) when absent.
// The key is created once and never rotated here: losing it makes stored secrets unreadable.
func LoadOrCreate(path string) (*Box, error) {
	key, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		key = make([]byte, keyLen)
		if _, err := rand.Read(key); err != nil {
			return nil, fmt.Errorf("generating secrets key: %w", err)
		}
		f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
		if err != nil {
			return nil, fmt.Errorf("creating %q: %w", path, err)
		}
		if _, err := f.Write(key); err != nil {
			f.Close()
			return nil, fmt.Errorf("writing %q: %w", path, err)
		}
		if err := f.Close(); err != nil {
			return nil, fmt.Errorf("writing %q: %w", path, err)
		}
	} else if err != nil {
		return nil, fmt.Errorf("reading secrets key %q: %w", path, err)
	}
	b, err := New(key)
	if err != nil {
		return nil, fmt.Errorf("secrets key %q: %w", path, err)
	}
	return b, nil
}

// Seal encrypts plaintext with a fresh random nonce; the result is base64(nonce||ciphertext).
func (b *Box) Seal(plaintext string) (string, error) {
	nonce := make([]byte, b.aead.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return "", fmt.Errorf("secretbox: generating nonce: %w", err)
	}
	return base64.StdEncoding.EncodeToString(b.aead.Seal(nonce, nonce, []byte(plaintext), nil)), nil
}

// Open reverses Seal.
func (b *Box) Open(sealed string) (string, error) {
	raw, err := base64.StdEncoding.DecodeString(sealed)
	if err != nil || len(raw) < b.aead.NonceSize()+b.aead.Overhead() {
		return "", ErrInvalid
	}
	n := b.aead.NonceSize()
	plain, err := b.aead.Open(nil, raw[:n], raw[n:], nil)
	if err != nil {
		return "", ErrInvalid
	}
	return string(plain), nil
}
