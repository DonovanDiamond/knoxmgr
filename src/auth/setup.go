package auth

import (
	"crypto/rand"
	"crypto/subtle"
	"strings"
	"sync"
)

// setupAlphabet has 32 symbols (5 bits each) and leaves out 0, 1, I and O, which read alike.
const setupAlphabet = "23456789ABCDEFGHJKLMNPQRSTUVWXYZ"

// SetupCode is the one-time first-run code, kept in memory only. A nil *SetupCode is never required.
type SetupCode struct {
	mu   sync.Mutex
	code string // normalised (no dashes); empty once cleared
}

// NewSetupCode returns a fresh code of 12 symbols, 60 bits of randomness.
func NewSetupCode() (*SetupCode, error) {
	b := make([]byte, 12)
	if _, err := rand.Read(b); err != nil {
		return nil, err
	}
	for i := range b {
		b[i] = setupAlphabet[b[i]&31]
	}
	return &SetupCode{code: string(b)}, nil
}

// String is the code as shown to the operator: XXXX-XXXX-XXXX.
func (s *SetupCode) String() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.code) != 12 {
		return ""
	}
	return s.code[:4] + "-" + s.code[4:8] + "-" + s.code[8:]
}

// Required reports whether setup is still pending.
func (s *SetupCode) Required() bool {
	if s == nil {
		return false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.code != ""
}

// Verify compares in constant time, ignoring case, dashes and spaces.
func (s *SetupCode) Verify(attempt string) bool {
	if s == nil {
		return false
	}
	attempt = strings.ToUpper(strings.NewReplacer("-", "", " ", "").Replace(attempt))
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.code != "" && subtle.ConstantTimeCompare([]byte(attempt), []byte(s.code)) == 1
}

// Clear ends setup mode for good.
func (s *SetupCode) Clear() {
	s.mu.Lock()
	s.code = ""
	s.mu.Unlock()
}
