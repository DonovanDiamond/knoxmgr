package api

import (
	"strconv"
	"strings"

	"github.com/gofiber/fiber/v3"

	"github.com/DonovanDiamond/knoxmgr/src/auth"
	"github.com/DonovanDiamond/knoxmgr/src/config"
	"github.com/DonovanDiamond/knoxmgr/src/db"
)

const (
	defaultAuditLimit = 100
	maxAuditLimit     = 1000
)

var errLastAdmin = fiber.NewError(fiber.StatusConflict, "cannot remove, demote or disable the last enabled admin")

func (s *server) adminRoutes(r fiber.Router) {
	r.Get("/users", s.adminListUsers)
	r.Post("/users", s.adminCreateUser)
	r.Get("/users/:id", s.adminGetUser)
	r.Put("/users/:id", s.adminUpdateUser)
	r.Delete("/users/:id", s.adminDeleteUser)
	r.Get("/audit", s.adminAudit)
	r.Post("/sync", s.startSync)
	r.Get("/sync", s.syncStatus)
	r.Get("/update", s.updateStatus)
	r.Post("/update/check", s.updateCheck)
	s.settingsRoutes(r.Group("/settings"))
}

func (s *server) appUser(c fiber.Ctx) (*db.User, error) {
	id, err := strconv.ParseInt(c.Params("id"), 10, 64)
	if err != nil || id <= 0 {
		return nil, badRequest("invalid user id")
	}
	return s.Users.GetByID(c.Context(), id)
}

func (s *server) adminListUsers(c fiber.Ctx) error {
	users, err := s.Users.List(c.Context())
	if err != nil {
		return err
	}
	return c.JSON(fiber.Map{"users": users, "total": len(users)})
}

func (s *server) adminGetUser(c fiber.Ctx) error {
	u, err := s.appUser(c)
	if err != nil {
		return err
	}
	return c.JSON(u)
}

// checkRoleShape enforces that admins carry no org/access and users carry both.
func checkRoleShape(role string, org, access *string) error {
	switch role {
	case config.RoleAdmin:
		if org != nil || access != nil {
			return badRequest("admins have no orgCode or access")
		}
	case config.RoleUser:
		if org == nil || checkPattern(orgCodeRe, "orgCode", *org) != nil {
			return badRequest("users need a valid orgCode")
		}
		if access == nil || (*access != config.AccessRead && *access != config.AccessWrite) {
			return badRequest("access must be read or write")
		}
	default:
		return badRequest("role must be admin or user")
	}
	return nil
}

func emptyToNil(v string) *string {
	if v == "" {
		return nil
	}
	return &v
}

func (s *server) adminCreateUser(c fiber.Ctx) error {
	var req struct {
		Username    string `json:"username"`
		DisplayName string `json:"displayName"`
		Password    string `json:"password"`
		Role        string `json:"role"`
		OrgCode     string `json:"orgCode"`
		Access      string `json:"access"`
	}
	if err := bind(c, &req); err != nil {
		return err
	}
	req.Username = strings.ToLower(req.Username)
	if err := checkPattern(appUserRe, "username", req.Username); err != nil {
		return err
	}
	if len(req.Password) < config.MinPasswordLen || len(req.Password) > config.MaxPasswordLen {
		return badRequest("password must be " + strconv.Itoa(config.MinPasswordLen) + " to " + strconv.Itoa(config.MaxPasswordLen) + " bytes")
	}
	if err := checkText("displayName", req.DisplayName, maxTextLen, false); err != nil {
		return err
	}
	if req.DisplayName == "" {
		req.DisplayName = req.Username
	}
	u := &db.User{Username: req.Username, DisplayName: req.DisplayName, Role: req.Role,
		OrgCode: emptyToNil(req.OrgCode), Access: emptyToNil(req.Access), AuthSource: "local"}
	if err := checkRoleShape(u.Role, u.OrgCode, u.Access); err != nil {
		return err
	}
	hash, err := auth.HashPassword(req.Password)
	if err != nil {
		return err
	}
	u.PasswordHash = &hash
	if err := s.Users.Create(c.Context(), u); err != nil {
		return err
	}
	s.record(c, "app_user.create", u.Username, nil, u)
	return c.Status(fiber.StatusCreated).JSON(u)
}

// otherEnabledAdmin reports whether an enabled admin besides exceptID exists.
func (s *server) otherEnabledAdmin(c fiber.Ctx, exceptID int64) (bool, error) {
	all, err := s.Users.List(c.Context())
	if err != nil {
		return false, err
	}
	for _, u := range all {
		if u.ID != exceptID && u.Role == config.RoleAdmin && !u.Disabled {
			return true, nil
		}
	}
	return false, nil
}

func (s *server) adminUpdateUser(c fiber.Ctx) error {
	old, err := s.appUser(c)
	if err != nil {
		return err
	}
	var req struct {
		DisplayName *string `json:"displayName"`
		Role        *string `json:"role"`
		OrgCode     *string `json:"orgCode"`
		Access      *string `json:"access"`
		Disabled    *bool   `json:"disabled"`
	}
	if err := bind(c, &req); err != nil {
		return err
	}
	next := *old
	if req.DisplayName != nil {
		if err := checkText("displayName", *req.DisplayName, maxTextLen, true); err != nil {
			return err
		}
		next.DisplayName = *req.DisplayName
	}
	if req.Disabled != nil {
		next.Disabled = *req.Disabled
	}
	if req.Role != nil || req.OrgCode != nil || req.Access != nil {
		if req.Role != nil {
			next.Role = *req.Role
		}
		if next.Role == config.RoleAdmin && old.Role != config.RoleAdmin {
			// Promoting clears org/access; an admin sent with them is rejected below.
			next.OrgCode, next.Access = nil, nil
		}
		if req.OrgCode != nil {
			next.OrgCode = req.OrgCode
		}
		if req.Access != nil {
			next.Access = req.Access
		}
		if err := checkRoleShape(next.Role, next.OrgCode, next.Access); err != nil {
			return err
		}
	}
	losesAdmin := old.Role == config.RoleAdmin && !old.Disabled && (next.Role != config.RoleAdmin || next.Disabled)
	if losesAdmin {
		if ok, err := s.otherEnabledAdmin(c, old.ID); err != nil {
			return err
		} else if !ok {
			return errLastAdmin
		}
	}
	if err := s.Users.Update(c.Context(), &next); err != nil {
		return err
	}
	if (next.Disabled && !old.Disabled) || next.Role != old.Role {
		if err := s.Auth.LogoutAll(c.Context(), old.ID); err != nil {
			return err
		}
	}
	s.record(c, "app_user.update", old.Username, old, &next)
	return c.JSON(&next)
}

func (s *server) adminDeleteUser(c fiber.Ctx) error {
	old, err := s.appUser(c)
	if err != nil {
		return err
	}
	if old.ID == principalFrom(c).UserID {
		// The caller is an enabled admin, so refusing self-deletion also means the last admin can
		// never be removed, and the audit row's actor still exists.
		return fiber.NewError(fiber.StatusConflict, "cannot delete your own account")
	}
	if err := s.Users.Delete(c.Context(), old.ID); err != nil {
		return err
	}
	s.record(c, "app_user.delete", old.Username, old, nil)
	return c.SendStatus(fiber.StatusNoContent)
}

func (s *server) adminAudit(c fiber.Ctx) error {
	limit := defaultAuditLimit
	if v := c.Query("limit"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n < 1 || n > maxAuditLimit {
			return badRequest("limit must be 1 to 1000")
		}
		limit = n
	}
	entries, err := s.Audit.ListRecent(c.Context(), limit)
	if err != nil {
		return err
	}
	return c.JSON(fiber.Map{"entries": entries})
}
