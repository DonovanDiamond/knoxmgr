package auth

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"sync/atomic"
	"time"

	"github.com/DonovanDiamond/knoxmgr/src/config"
	"github.com/DonovanDiamond/knoxmgr/src/db"
)

// Session is what a successful login or refresh hands back to the client.
type Session struct {
	AccessToken     string
	AccessExpiresAt time.Time
	RefreshToken    string // opaque; only its hash is stored
	User            *db.User
}

type Service struct {
	users      *db.Users
	refresh    *db.RefreshTokens
	tokens     *Tokens
	refreshTTL time.Duration
	ad         atomic.Pointer[adState] // swapped live when the admin saves AD settings
	now        func() time.Time
}

// adState is replaced as a whole, so a login never sees a directory with another save's mappings.
type adState struct {
	dir      Directory // nil when AD is disabled
	mappings []config.ADGroup
}

// NewService wires the pieces; dir may be nil to disable AD.
func NewService(users *db.Users, refresh *db.RefreshTokens, tokens *Tokens, refreshTTL time.Duration, dir Directory, mappings []config.ADGroup) *Service {
	s := &Service{users: users, refresh: refresh, tokens: tokens, refreshTTL: refreshTTL, now: time.Now}
	s.SetDirectory(dir, mappings)
	return s
}

// SetDirectory swaps the AD directory and group mappings; nil dir disables AD logins.
func (s *Service) SetDirectory(dir Directory, mappings []config.ADGroup) {
	s.ad.Store(&adState{dir: dir, mappings: mappings})
}

// Verify only checks the access token (signature, issuer, expiry); it does not consult the
// database. Callers must reload the account, as the API middleware does on every request.
func (s *Service) Verify(token string) (*Principal, error) { return s.tokens.Verify(token) }

// Login authenticates a username/password. Local accounts are tried first and never fall through
// to AD, so they keep working when the directory is down. Returns ErrInvalidCredentials,
// ErrNoMappedGroup, ErrAccountDisabled or ErrDirectoryUnreachable.
func (s *Service) Login(ctx context.Context, username, password string) (*Session, error) {
	username = strings.ToLower(strings.TrimSpace(username))
	if username == "" || password == "" {
		return nil, ErrInvalidCredentials
	}
	ad := s.ad.Load()
	u, err := s.users.GetByUsername(ctx, username)
	if err != nil && !errors.Is(err, db.ErrNotFound) {
		return nil, err
	}

	switch {
	case err == nil && u.AuthSource == "local":
		if !CheckPassword(u.PasswordHash, password) {
			return nil, ErrInvalidCredentials
		}
	case ad.dir != nil:
		CheckPassword(nil, password) // non-local attempts cost one bcrypt, same as the no-AD path
		if u, err = s.loginAD(ctx, ad, username, password); err != nil {
			return nil, err
		}
	default:
		CheckPassword(nil, password) // keep timing similar to a real check
		return nil, ErrInvalidCredentials
	}

	if u.Disabled {
		return nil, ErrAccountDisabled
	}
	return s.newSession(ctx, u)
}

func (s *Service) loginAD(ctx context.Context, ad *adState, username, password string) (*db.User, error) {
	du, err := ad.dir.Authenticate(ctx, username, password, groupNames(ad.mappings))
	if err != nil {
		return nil, err
	}
	grant, ok := MapGroups(ad.mappings, du.Groups)
	if !ok {
		return nil, ErrNoMappedGroup
	}
	au := &db.User{Username: username, DisplayName: du.DisplayName, Role: grant.Role}
	if grant.Role == config.RoleUser {
		au.OrgCode, au.Access = &grant.OrgCode, &grant.Access
	}
	return s.users.UpsertADUser(ctx, au)
}

// Refresh rotates a refresh token: the old one is revoked and a new session issued. The user is
// reloaded so role changes and disabling take effect here. Presenting an already-revoked token
// means it was stolen or replayed, so every session of that user is revoked.
func (s *Service) Refresh(ctx context.Context, rawToken string) (*Session, error) {
	t, err := s.refresh.GetByHash(ctx, hashToken(rawToken))
	if errors.Is(err, db.ErrNotFound) {
		return nil, ErrInvalidToken
	}
	if err != nil {
		return nil, err
	}
	if t.RevokedAt != nil {
		if err := s.refresh.RevokeAllForUser(ctx, t.UserID); err != nil {
			return nil, err
		}
		return nil, ErrInvalidToken
	}
	if exp, err := time.Parse(time.RFC3339, t.ExpiresAt); err != nil || !exp.After(s.now()) {
		return nil, ErrInvalidToken
	}
	u, err := s.users.GetByID(ctx, t.UserID)
	if err != nil {
		return nil, err
	}
	if u.Disabled {
		if err := s.refresh.RevokeAllForUser(ctx, u.ID); err != nil {
			return nil, err
		}
		return nil, ErrAccountDisabled
	}
	if err := s.refresh.Revoke(ctx, t.ID); err != nil {
		if errors.Is(err, db.ErrNotFound) { // lost a race with another use of the same token
			return nil, ErrInvalidToken
		}
		return nil, err
	}
	return s.newSession(ctx, u)
}

// Logout revokes one refresh token. Unknown or already-revoked tokens are not an error.
func (s *Service) Logout(ctx context.Context, rawToken string) error {
	t, err := s.refresh.GetValid(ctx, hashToken(rawToken), s.now())
	if errors.Is(err, db.ErrNotFound) {
		return nil
	}
	if err != nil {
		return err
	}
	if err := s.refresh.Revoke(ctx, t.ID); err != nil && !errors.Is(err, db.ErrNotFound) {
		return err
	}
	return nil
}

// LogoutAll revokes every refresh token of a user.
func (s *Service) LogoutAll(ctx context.Context, userID int64) error {
	return s.refresh.RevokeAllForUser(ctx, userID)
}

// LogoutOthers revokes every refresh token of a user except the one for keepRawToken, so a
// password change ends other sessions but not the caller's.
func (s *Service) LogoutOthers(ctx context.Context, userID int64, keepRawToken string) error {
	return s.refresh.RevokeAllForUserExcept(ctx, userID, hashToken(keepRawToken))
}

func (s *Service) newSession(ctx context.Context, u *db.User) (*Session, error) {
	p := Principal{UserID: u.ID, Username: u.Username, Role: u.Role}
	if u.OrgCode != nil {
		p.OrgCode = *u.OrgCode
	}
	if u.Access != nil {
		p.Access = *u.Access
	}
	access, exp, err := s.tokens.Issue(p)
	if err != nil {
		return nil, err
	}
	raw, err := newOpaqueToken()
	if err != nil {
		return nil, err
	}
	if err := s.refresh.Insert(ctx, u.ID, hashToken(raw), s.now().Add(s.refreshTTL)); err != nil {
		return nil, err
	}
	return &Session{AccessToken: access, AccessExpiresAt: exp, RefreshToken: raw, User: u}, nil
}

func newOpaqueToken() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("generating refresh token: %w", err)
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}

func hashToken(raw string) string {
	sum := sha256.Sum256([]byte(raw))
	return hex.EncodeToString(sum[:])
}
