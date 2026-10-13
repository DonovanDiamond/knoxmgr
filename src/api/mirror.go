package api

import (
	"errors"

	"github.com/gofiber/fiber/v3"

	"github.com/DonovanDiamond/knoxmgr/src/auth"
	"github.com/DonovanDiamond/knoxmgr/src/db"
	"github.com/DonovanDiamond/knoxmgr/src/knox"
)

// The mirror stores plain rows; handlers render the same knox.* shapes the API always served.

func userOfMirror(u db.MirrorUser) knox.User {
	enabled := "0"
	if u.Enabled {
		enabled = "1"
	}
	return knox.User{UserID: u.UserID, UserName: u.UserName, Email: u.Email, FirstName: u.FirstName, LastName: u.LastName,
		MobilePhone: u.MobilePhone, Contact: u.Contact, EmpNo: u.EmpNo, OrgCode: u.OrgCode, OrgName: u.OrgName,
		Enabled: enabled, UserType: u.UserType, DeviceCount: knox.DeviceCount(u.DeviceCount)}
}

func usersOfMirror(in []db.MirrorUser) []knox.User {
	out := make([]knox.User, len(in))
	for i, u := range in {
		out[i] = userOfMirror(u)
	}
	return out
}

func mirrorOfUser(u knox.User) db.MirrorUser {
	return db.MirrorUser{UserID: u.UserID, UserName: u.UserName, Email: u.Email, FirstName: u.FirstName, LastName: u.LastName,
		MobilePhone: u.MobilePhone, Contact: u.Contact, EmpNo: u.EmpNo, OrgCode: u.OrgCode, OrgName: u.OrgName,
		Enabled: u.Enabled == "1", UserType: u.UserType, DeviceCount: int(u.DeviceCount)}
}

func orgsOfMirror(in []db.MirrorOrg) []knox.Organization {
	out := make([]knox.Organization, len(in))
	for i, o := range in {
		out[i] = knox.Organization{OrgCode: o.OrgCode, OrgName: o.OrgName, ParentCode: o.ParentCode}
	}
	return out
}

func groupOfMirror(g db.MirrorGroup) knox.Group {
	return knox.Group{GroupID: g.GroupID, GroupName: g.GroupName, GroupType: g.GroupType, UserCount: knox.Count(g.UserCount)}
}

func groupsOfMirror(in []db.MirrorGroup) []knox.Group {
	out := make([]knox.Group, len(in))
	for i, g := range in {
		out[i] = groupOfMirror(g)
	}
	return out
}

func devicesOfMirror(in []db.MirrorDevice) []knox.Device {
	out := make([]knox.Device, len(in))
	for i, d := range in {
		out[i] = knox.Device{DeviceID: d.DeviceID, DeviceModel: d.DeviceModel, DeviceModelKind: d.DeviceModelKind,
			DeviceStatus: d.DeviceStatus, DeviceVersionName: d.DeviceVersionName, Platform: d.Platform,
			UserID: d.UserID, UserName: d.UserName, OrgCode: d.OrgCode, LastConnection: d.LastConnection}
	}
	return out
}

// scope builds the caller's Scope. Org codes come from the mirror, never from the client.
func (s *server) scope(c fiber.Ctx) (auth.Scope, error) {
	sc, _, err := s.scopeOrgs(c)
	return sc, err
}

// scopeOrgs also returns the org list for SQL filtering: nil for an admin (unrestricted), else
// the caller's org and its descendants (empty, non-nil, when the org is unknown).
func (s *server) scopeOrgs(c fiber.Ctx) (auth.Scope, []string, error) {
	p := principalFrom(c)
	if p.IsAdmin() {
		return auth.NewScope(p, nil), nil, nil
	}
	codes := []string{}
	if p.OrgCode != "" {
		var err error
		if codes, err = s.Mirror.SubOrgCodes(c.Context(), p.OrgCode); err != nil {
			return auth.Scope{}, nil, err
		}
	}
	return auth.NewScope(p, codes), codes, nil
}

// mirrorUser reads a user from the mirror; absent looks like out of scope.
func (s *server) mirrorUser(c fiber.Ctx, id string) (knox.User, error) {
	u, err := s.Mirror.User(c.Context(), id)
	if errors.Is(err, db.ErrNotFound) {
		return knox.User{}, errUserNotFound
	}
	return userOfMirror(u), err
}

// mirrorGroup reads a group from the mirror by the :id param.
func (s *server) mirrorGroup(c fiber.Ctx) (knox.Group, error) {
	id := c.Params("id")
	if err := checkPattern(groupIDRe, "group id", id); err != nil {
		return knox.Group{}, err
	}
	g, err := s.Mirror.Group(c.Context(), id)
	if errors.Is(err, db.ErrNotFound) {
		return knox.Group{}, errGroupNotFound
	}
	return groupOfMirror(g), err
}

var errGroupNotFound = fiber.NewError(fiber.StatusNotFound, "group not found")

// mirrorWrite runs a mirror update after Knox accepted the change. The change already happened,
// so a failure is logged, not returned; the next sync repairs the mirror.
func (s *server) mirrorWrite(what string, err error) {
	if err != nil {
		s.Log.Error().Err(err).Str("op", what).Msg("mirror update failed after Knox success")
	}
}

// refreshMirrorUser re-reads a user from Knox and stores it in the mirror.
func (s *server) refreshMirrorUser(c fiber.Ctx, id string) {
	u, err := s.Knox().SelectUserWithID(c.Context(), id)
	if err == nil {
		err = s.Mirror.UpsertUser(c.Context(), mirrorOfUser(u))
	}
	s.mirrorWrite("upsert user", err)
}
