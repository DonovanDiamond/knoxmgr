package secretbox

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRoundTrip(t *testing.T) {
	b, err := LoadOrCreate(filepath.Join(t.TempDir(), "secret.key"))
	if err != nil {
		t.Fatal(err)
	}
	for _, plain := range []string{"s3cret", "", "unicode-é-密码", strings.Repeat("x", 4096)} {
		sealed, err := b.Seal(plain)
		if err != nil {
			t.Fatal(err)
		}
		if plain != "" && strings.Contains(sealed, plain) {
			t.Fatal("ciphertext contains plaintext")
		}
		got, err := b.Open(sealed)
		if err != nil || got != plain {
			t.Fatalf("Open = %q, %v; want %q", got, err, plain)
		}
	}
	a, _ := b.Seal("same")
	c, _ := b.Seal("same")
	if a == c {
		t.Fatal("nonce reused: identical ciphertexts")
	}
}

func TestOpenRejects(t *testing.T) {
	b, _ := LoadOrCreate(filepath.Join(t.TempDir(), "k"))
	other, _ := LoadOrCreate(filepath.Join(t.TempDir(), "k"))
	sealed, _ := b.Seal("secret")
	flipped := []byte(sealed)
	flipped[len(flipped)-6] ^= 1 // changes the ciphertext body, still valid base64 alphabet
	if flipped[len(flipped)-6] == sealed[len(sealed)-6] {
		t.Fatal("test did not flip")
	}
	tests := []struct{ name, in string }{
		{"tampered", string(flipped)},
		{"other key", sealed},
		{"not base64", "!!!"},
		{"too short", "QUJD"},
		{"empty", ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			box := b
			if tt.name == "other key" {
				box = other
			}
			if _, err := box.Open(tt.in); err != ErrInvalid {
				t.Fatalf("err = %v, want ErrInvalid", err)
			}
		})
	}
}

func TestKeyFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "secret.key")
	b1, err := LoadOrCreate(path)
	if err != nil {
		t.Fatal(err)
	}
	info, _ := os.Stat(path)
	if info.Mode().Perm() != 0o600 || info.Size() != 32 {
		t.Fatalf("key file mode %v size %d", info.Mode().Perm(), info.Size())
	}
	first, _ := os.ReadFile(path)
	sealed, _ := b1.Seal("x")
	b2, err := LoadOrCreate(path) // reload: same key, not regenerated
	if err != nil {
		t.Fatal(err)
	}
	if again, _ := os.ReadFile(path); !bytes.Equal(first, again) {
		t.Fatal("key regenerated")
	}
	if got, err := b2.Open(sealed); err != nil || got != "x" {
		t.Fatalf("reload cannot open: %q %v", got, err)
	}
	os.WriteFile(path, []byte("short"), 0o600)
	if _, err := LoadOrCreate(path); err == nil {
		t.Fatal("want error for wrong-size key")
	}
}
