package api

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"strings"
	"time"

	"github.com/gofiber/fiber/v3"

	"github.com/DonovanDiamond/knoxmgr/src/auth"
	"github.com/DonovanDiamond/knoxmgr/src/db"
)

const (
	refreshCookie = "knoxmgr_refresh"
	// Scoped to the auth endpoints so the long-lived token is not sent with every API call.
	cookiePath = "/api/auth"

	maxUsernameLen = 256
	maxPasswordLen = 1024
)

type userBody struct {
	ID          int64   `json:"id"`
	Username    string  `json:"username"`
	DisplayName string  `json:"displayName"`
	Role        string  `json:"role"`
	OrgCode     *string `json:"orgCode"`
	Access      *string `json:"access"`
	AuthSource  string  `json:"authSource"`
}

type authBody struct {
	AccessToken string   `json:"accessToken"`
	ExpiresAt   string   `json:"expiresAt"`
	User        userBody `json:"user"`
}

func toUserBody(u *db.User) userBody {
	return userBody{u.ID, u.Username, u.DisplayName, u.Role, u.OrgCode, u.Access, u.AuthSource}
}

func (s *server) login(c fiber.Ctx) error {
	var req struct {
		Username string `json:"username"`
		Password string `json:"password"`
	}
	if err := c.Bind().JSON(&req); err != nil {
		return fiber.NewError(fiber.StatusBadRequest, "body must be JSON with username and password")
	}
	if req.Username == "" || req.Password == "" || len(req.Username) > maxUsernameLen || len(req.Password) > maxPasswordLen {
		return fiber.NewError(fiber.StatusBadRequest, "username and password are required")
	}
	ip := c.IP()
	if err := s.LoginLimiter.Allow(ip, req.Username); err != nil {
		return fiber.NewError(fiber.StatusTooManyRequests, "too many failed login attempts, try again later")
	}
	sess, err := s.Auth.Login(c.Context(), req.Username, req.Password)
	if errors.Is(err, auth.ErrInvalidCredentials) {
		s.LoginLimiter.Failure(ip, req.Username)
	}
	if err == nil {
		s.LoginLimiter.Success(ip, req.Username)
	}
	if err != nil {
		s.logLoginFailure(c, req.Username, err)
		return c.Status(authStatus(err)).JSON(authError(err))
	}
	return s.respondSession(c, sess)
}

// logLoginFailure logs the username only when it names a real account; a typo can be a password.
func (s *server) logLoginFailure(c fiber.Ctx, username string, err error) {
	ev := s.Log.Warn().Err(err)
	norm := strings.ToLower(strings.TrimSpace(username))
	if _, lookupErr := s.Users.GetByUsername(c.Context(), norm); lookupErr == nil {
		ev = ev.Str("username", norm)
	} else {
		sum := sha256.Sum256([]byte(username))
		ev = ev.Str("userHash", hex.EncodeToString(sum[:])[:12])
	}
	ev.Msg("login failed")
}

func (s *server) refresh(c fiber.Ctx) error {
	raw := c.Cookies(refreshCookie)
	if raw == "" {
		return fiber.NewError(fiber.StatusUnauthorized, "no session")
	}
	sess, err := s.Auth.Refresh(c.Context(), raw)
	if err != nil {
		s.clearCookie(c)
		s.Log.Warn().Err(err).Msg("refresh failed")
		return c.Status(authStatus(err)).JSON(authError(err))
	}
	return s.respondSession(c, sess)
}

func (s *server) logout(c fiber.Ctx) error {
	if raw := c.Cookies(refreshCookie); raw != "" {
		if err := s.Auth.Logout(c.Context(), raw); err != nil {
			return err
		}
	}
	s.clearCookie(c)
	return c.SendStatus(fiber.StatusNoContent)
}

func (s *server) me(c fiber.Ctx) error {
	p := principalFrom(c)
	u, err := s.Users.GetByID(c.Context(), p.UserID)
	if errors.Is(err, db.ErrNotFound) {
		return fiber.NewError(fiber.StatusUnauthorized, "unknown user")
	}
	if err != nil {
		return err
	}
	if u.Disabled {
		return fiber.NewError(fiber.StatusForbidden, "account disabled")
	}
	return c.JSON(toUserBody(u))
}

func (s *server) respondSession(c fiber.Ctx, sess *auth.Session) error {
	c.Cookie(&fiber.Cookie{
		Name: refreshCookie, Value: sess.RefreshToken, Path: cookiePath,
		MaxAge: int(s.RefreshTTL / time.Second), HTTPOnly: true, SameSite: fiber.CookieSameSiteStrictMode,
		Secure: s.SecureCookies,
	})
	return c.JSON(authBody{
		AccessToken: sess.AccessToken,
		ExpiresAt:   sess.AccessExpiresAt.UTC().Format(time.RFC3339),
		User:        toUserBody(sess.User),
	})
}

func (s *server) clearCookie(c fiber.Ctx) {
	c.Cookie(&fiber.Cookie{
		Name: refreshCookie, Path: cookiePath, MaxAge: -1, HTTPOnly: true,
		SameSite: fiber.CookieSameSiteStrictMode, Secure: s.SecureCookies,
	})
}

func authStatus(err error) int {
	switch {
	case errors.Is(err, auth.ErrInvalidCredentials), errors.Is(err, auth.ErrInvalidToken):
		return fiber.StatusUnauthorized
	case errors.Is(err, auth.ErrNoMappedGroup), errors.Is(err, auth.ErrAccountDisabled):
		return fiber.StatusForbidden
	case errors.Is(err, auth.ErrDirectoryUnreachable):
		return fiber.StatusServiceUnavailable
	case errors.Is(err, db.ErrSourceConflict):
		return fiber.StatusConflict
	}
	return fiber.StatusInternalServerError
}

// authError never echoes the underlying error, which can carry directory or database detail.
func authError(err error) errorBody {
	switch {
	case errors.Is(err, auth.ErrInvalidCredentials):
		return errorBody{Error: "invalid username or password"}
	case errors.Is(err, auth.ErrInvalidToken):
		return errorBody{Error: "session expired"}
	case errors.Is(err, auth.ErrNoMappedGroup):
		return errorBody{Error: "your account has no access to this application"}
	case errors.Is(err, auth.ErrAccountDisabled):
		return errorBody{Error: "account disabled"}
	case errors.Is(err, db.ErrSourceConflict):
		return errorBody{Error: "username belongs to an account from another source"}
	case errors.Is(err, auth.ErrDirectoryUnreachable):
		return errorBody{Error: "the directory server cannot be reached", Code: "ad_unreachable"}
	}
	return errorBody{Error: "internal error"}
}
