package api

import (
	"errors"
	"strings"

	"github.com/gofiber/fiber/v3"

	"github.com/DonovanDiamond/knoxmgr/src/auth"
	"github.com/DonovanDiamond/knoxmgr/src/db"
)

const principalKey = "principal"

// Authenticate verifies the Bearer access token, then re-loads the app account so a deleted,
// disabled or re-privileged account takes effect immediately rather than at token expiry. The
// Principal comes from the current database row, not from the token claims.
func Authenticate(svc *auth.Service, users *db.Users) fiber.Handler {
	return func(c fiber.Ctx) error {
		scheme, token, ok := strings.Cut(c.Get(fiber.HeaderAuthorization), " ")
		if !ok || !strings.EqualFold(scheme, "Bearer") || token == "" {
			return fiber.NewError(fiber.StatusUnauthorized, "missing bearer token")
		}
		claims, err := svc.Verify(token)
		if err != nil {
			return fiber.NewError(fiber.StatusUnauthorized, "invalid or expired token")
		}
		u, err := users.GetByID(c.Context(), claims.UserID)
		if errors.Is(err, db.ErrNotFound) {
			return fiber.NewError(fiber.StatusUnauthorized, "unknown user")
		}
		if err != nil {
			return err
		}
		if u.Disabled {
			return fiber.NewError(fiber.StatusForbidden, "account disabled")
		}
		p := &auth.Principal{UserID: u.ID, Username: u.Username, Role: u.Role}
		if u.OrgCode != nil {
			p.OrgCode = *u.OrgCode
		}
		if u.Access != nil {
			p.Access = *u.Access
		}
		c.Locals(principalKey, p)
		return c.Next()
	}
}

// RequireAdmin and RequireWrite must be chained after Authenticate.
func RequireAdmin() fiber.Handler { return require(auth.RequireAdmin) }
func RequireWrite() fiber.Handler { return require(auth.RequireWrite) }

func require(check func(*auth.Principal) error) fiber.Handler {
	return func(c fiber.Ctx) error {
		if err := check(principalFrom(c)); err != nil {
			return fiber.NewError(fiber.StatusForbidden, "forbidden")
		}
		return c.Next()
	}
}

func principalFrom(c fiber.Ctx) *auth.Principal {
	p, _ := c.Locals(principalKey).(*auth.Principal)
	return p
}
