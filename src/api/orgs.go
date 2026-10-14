package api

import (
	"errors"
	"slices"

	"github.com/gofiber/fiber/v3"

	"github.com/DonovanDiamond/knoxmgr/src/db"
)

var errOrgNotFound = fiber.NewError(fiber.StatusNotFound, "organization not found")

// mirrorOrg reads the org named by the :code param from the mirror.
func (s *server) mirrorOrg(c fiber.Ctx) (db.MirrorOrg, error) {
	code := c.Params("code")
	if err := checkPattern(orgCodeRe, "org code", code); err != nil {
		return db.MirrorOrg{}, err
	}
	o, err := s.Mirror.Org(c.Context(), code)
	if errors.Is(err, db.ErrNotFound) {
		return o, errOrgNotFound
	}
	return o, err
}

// orgFields is the editable part of an org and what is audited.
type orgFields struct {
	OrgName    string `json:"orgName"`
	ParentCode string `json:"parentCode"`
}

// validateOrg requires a name and an existing parent. self is the org's own code ("" on create);
// its descendants are refused too, as a parent there would make a cycle.
func (s *server) validateOrg(c fiber.Ctx, f orgFields, self string) error {
	if err := checkText("orgName", f.OrgName, maxOrgName, true); err != nil {
		return err
	}
	if err := checkPattern(orgCodeRe, "parentCode", f.ParentCode); err != nil {
		return err
	}
	if _, err := s.Mirror.Org(c.Context(), f.ParentCode); err != nil {
		if errors.Is(err, db.ErrNotFound) {
			return badRequest("parent organization does not exist")
		}
		return err
	}
	if self == "" {
		return nil
	}
	tree, err := s.Mirror.SubOrgCodes(c.Context(), self)
	if err != nil {
		return err
	}
	if slices.Contains(tree, f.ParentCode) {
		return badRequest("an organization cannot be its own parent or sit under its own descendant")
	}
	return nil
}

func (s *server) getOrg(c fiber.Ctx) error {
	o, err := s.mirrorOrg(c)
	if err != nil {
		return err
	}
	sc, err := s.scope(c)
	if err != nil {
		return err
	}
	if !sc.CanReadOrg(o.OrgCode) {
		return errOrgNotFound
	}
	members, err := s.Mirror.OrgMembers(c.Context(), o.OrgCode)
	if err != nil {
		return err
	}
	views, err := s.viewsOf(c, usersOfMirror(members))
	if err != nil {
		return err
	}
	return c.JSON(fiber.Map{"org": orgsOfMirror([]db.MirrorOrg{o})[0], "members": views})
}

func (s *server) createOrg(c fiber.Ctx) error {
	var req struct {
		OrgCode string `json:"orgCode"`
		orgFields
	}
	if err := bind(c, &req); err != nil {
		return err
	}
	if err := checkPattern(orgCodeRe, "orgCode", req.OrgCode); err != nil {
		return err
	}
	if err := s.validateOrg(c, req.orgFields, ""); err != nil {
		return err
	}
	if _, err := s.Mirror.Org(c.Context(), req.OrgCode); err == nil {
		return fiber.NewError(fiber.StatusConflict, "organization already exists")
	} else if !errors.Is(err, db.ErrNotFound) {
		return err
	}
	if err := s.Knox().CreateOrg(c.Context(), req.OrgCode, req.OrgName, req.ParentCode); err != nil {
		return err
	}
	o := db.MirrorOrg{OrgCode: req.OrgCode, OrgName: req.OrgName, ParentCode: req.ParentCode}
	s.mirrorWrite("upsert org", s.Mirror.UpsertOrg(c.Context(), o))
	s.record(c, "knox.org.create", req.OrgCode, nil, req.orgFields)
	return c.Status(fiber.StatusCreated).JSON(orgsOfMirror([]db.MirrorOrg{o})[0])
}

func (s *server) updateOrg(c fiber.Ctx) error {
	old, err := s.mirrorOrg(c)
	if err != nil {
		return err
	}
	var req orgFields
	if err := bind(c, &req); err != nil {
		return err
	}
	if err := s.validateOrg(c, req, old.OrgCode); err != nil {
		return err
	}
	if err := s.Knox().UpdateOrg(c.Context(), old.OrgCode, req.OrgName, req.ParentCode); err != nil {
		return err
	}
	o := db.MirrorOrg{OrgCode: old.OrgCode, OrgName: req.OrgName, ParentCode: req.ParentCode}
	s.mirrorWrite("upsert org", s.Mirror.UpsertOrg(c.Context(), o))
	s.record(c, "knox.org.update", old.OrgCode, orgFields{old.OrgName, old.ParentCode}, req)
	return c.JSON(orgsOfMirror([]db.MirrorOrg{o})[0])
}

func (s *server) deleteOrg(c fiber.Ctx) error {
	old, err := s.mirrorOrg(c)
	if err != nil {
		return err
	}
	if err := s.Knox().DeleteOrg(c.Context(), old.OrgCode); err != nil {
		return err
	}
	s.mirrorWrite("delete org", s.Mirror.DeleteOrg(c.Context(), old.OrgCode))
	s.record(c, "knox.org.delete", old.OrgCode, orgFields{old.OrgName, old.ParentCode}, nil)
	return c.SendStatus(fiber.StatusNoContent)
}

// addOrgMembers moves users into the org. Hidden and unknown ids are 404 via knoxUser.
func (s *server) addOrgMembers(c fiber.Ctx) error {
	var req struct {
		UserIDs []string `json:"userIds"`
	}
	if err := bind(c, &req); err != nil {
		return err
	}
	ids, err := checkIDList(req.UserIDs)
	if err != nil {
		return err
	}
	o, err := s.mirrorOrg(c)
	if err != nil {
		return err
	}
	previous := make(map[string]string, len(ids))
	for _, id := range ids {
		u, err := s.knoxUser(c, id)
		if err != nil {
			return err
		}
		previous[id] = u.OrgCode
	}
	if err := s.Knox().AddOrgMembers(c.Context(), o.OrgCode, ids); err != nil {
		return err
	}
	s.mirrorWrite("move users", s.Mirror.MoveUsers(c.Context(), o.OrgCode, ids))
	s.record(c, "knox.org.add_members", o.OrgCode, fiber.Map{"orgCodes": previous}, fiber.Map{"userIds": ids})
	return c.SendStatus(fiber.StatusNoContent)
}
