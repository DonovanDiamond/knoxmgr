package auth

import (
	"fmt"
	"sync"

	"github.com/DonovanDiamond/knoxmgr/src/config"
	"golang.org/x/crypto/bcrypt"
)

var bcryptCost = bcrypt.DefaultCost

// HashPassword bcrypt-hashes a password after checking its length bounds.
func HashPassword(password string) (string, error) {
	if n := len(password); n < config.MinPasswordLen || n > config.MaxPasswordLen {
		return "", fmt.Errorf("password must be %d-%d bytes", config.MinPasswordLen, config.MaxPasswordLen)
	}
	h, err := bcrypt.GenerateFromPassword([]byte(password), bcryptCost)
	if err != nil {
		return "", fmt.Errorf("hashing password: %w", err)
	}
	return string(h), nil
}

// CheckPassword reports whether password matches hash. A nil hash (unknown user) still costs a
// bcrypt comparison so response time does not reveal whether the account exists.
func CheckPassword(hash *string, password string) bool {
	if hash == nil {
		_ = bcrypt.CompareHashAndPassword(dummyHash(), []byte(password))
		return false
	}
	return bcrypt.CompareHashAndPassword([]byte(*hash), []byte(password)) == nil
}

var dummyHash = sync.OnceValue(func() []byte {
	h, _ := bcrypt.GenerateFromPassword([]byte("not-a-real-password"), bcryptCost)
	return h
})
