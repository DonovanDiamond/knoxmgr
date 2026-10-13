package auth

import (
	"crypto/ed25519"
	"crypto/rand"
	"crypto/x509"
	"encoding/pem"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
)

// LoadOrGenerateKeys reads the Ed25519 PEM key pair, creating it (mode 0600) if the private key is
// missing. If only the public key is missing it is derived from the private key.
func LoadOrGenerateKeys(privatePath, publicPath string) (ed25519.PrivateKey, ed25519.PublicKey, error) {
	raw, err := os.ReadFile(privatePath)
	if errors.Is(err, fs.ErrNotExist) {
		return generateKeys(privatePath, publicPath)
	}
	if err != nil {
		return nil, nil, fmt.Errorf("reading private key %q: %w", privatePath, err)
	}
	priv, err := parsePrivate(raw)
	if err != nil {
		return nil, nil, fmt.Errorf("private key %q: %w", privatePath, err)
	}
	pub := priv.Public().(ed25519.PublicKey)

	if rawPub, err := os.ReadFile(publicPath); err == nil {
		onDisk, err := parsePublic(rawPub)
		if err != nil {
			return nil, nil, fmt.Errorf("public key %q: %w", publicPath, err)
		}
		if !pub.Equal(onDisk) {
			return nil, nil, fmt.Errorf("public key %q does not match private key %q", publicPath, privatePath)
		}
	} else if errors.Is(err, fs.ErrNotExist) {
		if err := writePublic(publicPath, pub); err != nil {
			return nil, nil, err
		}
	} else {
		return nil, nil, fmt.Errorf("reading public key %q: %w", publicPath, err)
	}
	return priv, pub, nil
}

func generateKeys(privatePath, publicPath string) (ed25519.PrivateKey, ed25519.PublicKey, error) {
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return nil, nil, fmt.Errorf("generating key pair: %w", err)
	}
	der, err := x509.MarshalPKCS8PrivateKey(priv)
	if err != nil {
		return nil, nil, err
	}
	if err := writeNew(privatePath, pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: der})); err != nil {
		return nil, nil, err
	}
	if err := writePublic(publicPath, pub); err != nil {
		return nil, nil, err
	}
	return priv, pub, nil
}

func writePublic(path string, pub ed25519.PublicKey) error {
	der, err := x509.MarshalPKIXPublicKey(pub)
	if err != nil {
		return err
	}
	// The public key may be stale from an earlier install, so replace rather than refuse.
	if err := os.Remove(path); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return fmt.Errorf("replacing public key %q: %w", path, err)
	}
	return writeNew(path, pem.EncodeToMemory(&pem.Block{Type: "PUBLIC KEY", Bytes: der}))
}

// writeNew creates path exclusively with mode 0600, so a key is never overwritten or world-readable.
func writeNew(path string, data []byte) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return fmt.Errorf("creating directory for %q: %w", path, err)
	}
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return fmt.Errorf("creating %q: %w", path, err)
	}
	if _, err := f.Write(data); err != nil {
		f.Close()
		return fmt.Errorf("writing %q: %w", path, err)
	}
	return f.Close()
}

func parsePrivate(pemBytes []byte) (ed25519.PrivateKey, error) {
	block, _ := pem.Decode(pemBytes)
	if block == nil {
		return nil, errors.New("not PEM encoded")
	}
	k, err := x509.ParsePKCS8PrivateKey(block.Bytes)
	if err != nil {
		return nil, err
	}
	priv, ok := k.(ed25519.PrivateKey)
	if !ok {
		return nil, errors.New("not an Ed25519 key")
	}
	return priv, nil
}

func parsePublic(pemBytes []byte) (ed25519.PublicKey, error) {
	block, _ := pem.Decode(pemBytes)
	if block == nil {
		return nil, errors.New("not PEM encoded")
	}
	k, err := x509.ParsePKIXPublicKey(block.Bytes)
	if err != nil {
		return nil, err
	}
	pub, ok := k.(ed25519.PublicKey)
	if !ok {
		return nil, errors.New("not an Ed25519 key")
	}
	return pub, nil
}
