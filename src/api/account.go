package api

import (
	"errors"

	"github.com/gofiber/fiber/v3"

	"github.com/DonovanDiamond/knoxmgr/src/auth"
	"github.com/DonovanDiamond/knoxmgr/src/db"
)

func errADManaged() error {
	return &fiberError{fiber.StatusForbidden, "this account is managed by the directory", "ad_managed"}
}

// selfLocal loads the signed-in account and rejects directory accounts, whose profile and
// password live in AD.
func (s *server) selfLocal(c fiber.Ctx) (*db.User, error) {
	u, err := s.Users.GetByID(c.Context(), principalFrom(c).UserID)
	if errors.Is(err, db.ErrNotFound) {
		return nil, fiber.NewError(fiber.StatusUnauthorized, "unknown user")
	}
	if err != nil {
		return nil, err
	}
	if u.Disabled {
		return nil, fiber.NewError(fiber.StatusForbidden, "account disabled")
	}
	if u.AuthSource != "local" {
		return nil, errADManaged()
	}
	return u, nil
}

func (s *server) updateAccount(c fiber.Ctx) error {
	var req struct {
		DisplayName string `json:"displayName"`
	}
	if err := bind(c, &req); err != nil {
		return err
	}
	if err := checkText("displayName", req.DisplayName, maxTextLen, true); err != nil {
		return err
	}
	u, err := s.selfLocal(c)
	if err != nil {
		return err
	}
	if u.DisplayName == req.DisplayName {
		return c.JSON(toUserBody(u))
	}
	prev := u.DisplayName
	u.DisplayName = req.DisplayName
	if err := s.Users.Update(c.Context(), u); err != nil {
		return err
	}
	s.record(c, "account.update", "user:"+u.Username, fiber.Map{"displayName": prev}, fiber.Map{"displayName": u.DisplayName})
	return c.JSON(toUserBody(u))
}

func (s *server) changePassword(c fiber.Ctx) error {
	var req struct {
		CurrentPassword string `json:"currentPassword"`
		NewPassword     string `json:"newPassword"`
	}
	if err := bind(c, &req); err != nil {
		return err
	}
	if req.CurrentPassword == "" || len(req.CurrentPassword) > maxPasswordLen {
		return badRequest("currentPassword is required")
	}
	u, err := s.selfLocal(c)
	if err != nil {
		return err
	}
	ip := c.IP()
	if err := s.LoginLimiter.Allow(ip, u.Username); err != nil {
		return fiber.NewError(fiber.StatusTooManyRequests, "too many failed attempts, try again later")
	}
	if !auth.CheckPassword(u.PasswordHash, req.CurrentPassword) {
		s.LoginLimiter.Failure(ip, u.Username)
		return &fiberError{fiber.StatusForbidden, "current password is incorrect", "wrong_password"}
	}
	s.LoginLimiter.Success(ip, u.Username)
	hash, err := auth.HashPassword(req.NewPassword)
	if err != nil {
		return &fiberError{fiber.StatusBadRequest, err.Error(), "invalid_password"}
	}
	u.PasswordHash = &hash
	if err := s.Users.Update(c.Context(), u); err != nil {
		return err
	}
	if err := s.Auth.LogoutOthers(c.Context(), u.ID, c.Cookies(refreshCookie)); err != nil {
		return err
	}
	s.record(c, "account.password", "user:"+u.Username, nil, nil)
	return c.SendStatus(fiber.StatusNoContent)
}
