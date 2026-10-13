package api

import (
	"strings"

	"github.com/gofiber/fiber/v3"

	"github.com/DonovanDiamond/knoxmgr/src/auth"
	"github.com/DonovanDiamond/knoxmgr/src/config"
	"github.com/DonovanDiamond/knoxmgr/src/db"
)

// setupLimiterKey is the "username" half of the limiter key: every setup guess shares one counter per IP.
const setupLimiterKey = "setup"

var errSetupDone = &fiberError{409, "setup is already complete", "setup_done"}

// fiberError carries a code next to the status; errorHandler renders it as errorBody.
type fiberError struct {
	status int
	msg    string
	code   string
}

func (e *fiberError) Error() string { return e.msg }

// checkSetupCode applies the limiter and compares the code. Callers must hold setupMu.
func (s *server) checkSetupCode(c fiber.Ctx, code string) error {
	if !s.Setup.Required() {
		return errSetupDone
	}
	ip := c.IP()
	if err := s.SetupLimiter.Allow(ip, setupLimiterKey); err != nil {
		return fiber.NewError(fiber.StatusTooManyRequests, "too many wrong setup codes, try again later")
	}
	if len(code) > 64 || !s.Setup.Verify(code) {
		s.SetupLimiter.Failure(ip, setupLimiterKey)
		s.Log.Warn().Str("ip", ip).Msg("wrong setup code")
		return &fiberError{401, "that setup code is not right", "invalid_setup_code"}
	}
	return nil
}

func (s *server) setupVerify(c fiber.Ctx) error {
	var req struct {
		Code string `json:"code"`
	}
	if err := bind(c, &req); err != nil {
		return err
	}
	s.setupMu.Lock()
	defer s.setupMu.Unlock()
	if err := s.checkSetupCode(c, req.Code); err != nil {
		return err
	}
	return c.SendStatus(fiber.StatusNoContent)
}

// setupAdmin creates the first admin and signs them in, exactly like /api/auth/login.
func (s *server) setupAdmin(c fiber.Ctx) error {
	var req struct {
		Code     string `json:"code"`
		Username string `json:"username"`
		Password string `json:"password"`
	}
	if err := bind(c, &req); err != nil {
		return err
	}
	// Held across the check and the create so two requests cannot both pass and make two admins.
	s.setupMu.Lock()
	defer s.setupMu.Unlock()
	if err := s.checkSetupCode(c, req.Code); err != nil {
		return err
	}
	req.Username = strings.ToLower(req.Username)
	if err := checkPattern(appUserRe, "username", req.Username); err != nil {
		return err
	}
	if len(req.Password) < config.MinPasswordLen || len(req.Password) > config.MaxPasswordLen {
		return badRequest("password must be 8 to 72 bytes")
	}
	if n, err := s.Users.CountAdmins(c.Context()); err != nil {
		return err
	} else if n > 0 {
		s.Setup.Clear()
		return errSetupDone
	}
	hash, err := auth.HashPassword(req.Password)
	if err != nil {
		return err
	}
	u := &db.User{Username: req.Username, DisplayName: req.Username, PasswordHash: &hash, Role: config.RoleAdmin, AuthSource: "local"}
	if err := s.Users.Create(c.Context(), u); err != nil {
		return err
	}
	s.logAuditFailure(s.Audit.Insert(c.Context(), &db.AuditEntry{ActorUserID: &u.ID, ActorUsername: u.Username,
		Action: "setup.create_admin", Target: u.Username}), "setup.create_admin", u.Username)
	s.Setup.Clear()

	sess, err := s.Auth.Login(c.Context(), req.Username, req.Password)
	if err != nil {
		return err
	}
	return s.respondSession(c, sess)
}
