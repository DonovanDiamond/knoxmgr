package auth

import (
	"crypto/ed25519"
	"errors"
	"fmt"
	"time"

	"github.com/DonovanDiamond/knoxmgr/src/config"
	"github.com/golang-jwt/jwt/v5"
)

var ErrInvalidToken = errors.New("invalid or expired token")

const issuer = "knoxmgr"

type claims struct {
	jwt.RegisteredClaims
	UID      int64  `json:"uid"`
	Username string `json:"username"`
	Role     string `json:"role"`
	OrgCode  string `json:"orgCode,omitempty"`
	Access   string `json:"access,omitempty"`
}

// Tokens signs and verifies access tokens (Ed25519 / EdDSA).
type Tokens struct {
	priv ed25519.PrivateKey
	pub  ed25519.PublicKey
	ttl  time.Duration
	now  func() time.Time
}

func NewTokens(priv ed25519.PrivateKey, pub ed25519.PublicKey, accessTTL time.Duration) *Tokens {
	return &Tokens{priv: priv, pub: pub, ttl: accessTTL, now: time.Now}
}

// Issue returns a signed access token for p and its expiry.
func (t *Tokens) Issue(p Principal) (string, time.Time, error) {
	now := t.now()
	exp := now.Add(t.ttl)
	tok := jwt.NewWithClaims(jwt.SigningMethodEdDSA, claims{
		RegisteredClaims: jwt.RegisteredClaims{Issuer: issuer, IssuedAt: jwt.NewNumericDate(now), ExpiresAt: jwt.NewNumericDate(exp)},
		UID:              p.UserID, Username: p.Username, Role: p.Role, OrgCode: p.OrgCode, Access: p.Access,
	})
	s, err := tok.SignedString(t.priv)
	if err != nil {
		return "", time.Time{}, fmt.Errorf("signing access token: %w", err)
	}
	return s, exp, nil
}

// Verify checks signature, algorithm, issuer and expiry. Any failure is ErrInvalidToken.
func (t *Tokens) Verify(raw string) (*Principal, error) {
	var c claims
	_, err := jwt.ParseWithClaims(raw, &c, func(*jwt.Token) (any, error) { return t.pub, nil },
		jwt.WithValidMethods([]string{jwt.SigningMethodEdDSA.Alg()}),
		jwt.WithIssuer(issuer), jwt.WithExpirationRequired(), jwt.WithTimeFunc(t.now))
	if err != nil {
		return nil, ErrInvalidToken
	}
	if c.UID == 0 || (c.Role != config.RoleAdmin && c.Role != config.RoleUser) {
		return nil, ErrInvalidToken
	}
	return &Principal{UserID: c.UID, Username: c.Username, Role: c.Role, OrgCode: c.OrgCode, Access: c.Access}, nil
}
