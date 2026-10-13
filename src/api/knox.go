package api

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/gofiber/fiber/v3"

	"github.com/DonovanDiamond/knoxmgr/src/auth"
	"github.com/DonovanDiamond/knoxmgr/src/db"
	"github.com/DonovanDiamond/knoxmgr/src/knox"
)

func (s *server) knoxRoutes(r fiber.Router) {
	r.Get("/orgs", s.listOrgs)
	r.Get("/users", s.listUsers)
	r.Post("/users/invite", RequireWrite(), s.inviteUsers)
	r.Post("/users", RequireWrite(), s.createUser)
	r.Get("/users/:id", s.getUser)
	r.Get("/users/:id/devices", s.listUserDevices)
	r.Get("/users/:id/groups", s.listUserGroups)
	r.Put("/users/:id", RequireWrite(), s.updateUser)
	r.Delete("/users/:id", RequireWrite(), s.purgeUser)

	r.Get("/devices", s.listDevices)
	r.Get("/devices/:id", s.getDevice)
	r.Get("/devices/:id/detail", s.deviceDetail)
	r.Get("/devices/:id/apps", s.deviceApps)
	r.Get("/devices/:id/location", s.deviceLocation)

	r.Get("/groups", s.listGroups)
	r.Post("/groups", RequireAdmin(), s.createGroup)
	r.Get("/groups/:id", s.getGroup)
	r.Delete("/groups/:id", RequireAdmin(), s.deleteGroup)
	r.Get("/groups/:id/members", s.listMembers)
	r.Post("/groups/:id/members", RequireWrite(), s.addMembers)
	r.Delete("/groups/:id/members", RequireWrite(), s.removeMembers)
}

// knoxUser looks a target up in Knox; a Knox refusal for a single-user lookup means it is absent.
func (s *server) knoxUser(c fiber.Ctx, id string) (knox.User, error) {
	u, err := s.Knox().SelectUserWithID(c.Context(), id)
	var ke *knox.Error
	if errors.As(err, &ke) {
		return u, errUserNotFound
	}
	return u, err
}

// errUserNotFound is also what an out-of-scope target gets, so existence is not revealed.
var errUserNotFound = fiber.NewError(fiber.StatusNotFound, "user not found")

// checkTargetWrite is CheckWriteOrg for an existing user: out of scope looks like not found.
func checkTargetWrite(sc auth.Scope, orgCode string) error {
	err := sc.CheckWriteOrg(orgCode)
	if errors.Is(err, auth.ErrForbidden) {
		return errUserNotFound
	}
	return err
}

func (s *server) knoxGroup(c fiber.Ctx) (knox.Group, error) {
	id := c.Params("id")
	if err := checkPattern(groupIDRe, "group id", id); err != nil {
		return knox.Group{}, err
	}
	pg, err := s.Knox().SelectGroups(c.Context(), knox.GroupQuery{GroupID: id, Limit: 1})
	if err != nil {
		return knox.Group{}, err
	}
	for _, g := range pg.Groups {
		if g.GroupID == id {
			return g, nil
		}
	}
	return knox.Group{}, fiber.NewError(fiber.StatusNotFound, "group not found")
}

// targetsInWriteScope looks up each id in Knox and requires the caller to have write access to
// its org. Any miss or denial rejects the whole batch.
func (s *server) targetsInWriteScope(c fiber.Ctx, sc auth.Scope, ids []string) error {
	for _, id := range ids {
		u, err := s.knoxUser(c, id)
		if err != nil {
			return err
		}
		if err := checkTargetWrite(sc, u.OrgCode); err != nil {
			return err
		}
	}
	return nil
}

// userFields is the editable part of a Knox user; empty means unset. It is also what is audited,
// so it must never hold a password.
type userFields struct {
	UserName    string `json:"userName,omitempty"`
	Email       string `json:"email,omitempty"`
	FirstName   string `json:"firstName,omitempty"`
	LastName    string `json:"lastName,omitempty"`
	MobilePhone string `json:"mobilePhone,omitempty"`
	Contact     string `json:"contact,omitempty"`
	EmpNo       string `json:"empNo,omitempty"`
	OrgCode     string `json:"orgCode,omitempty"`
}

func (f userFields) validate(create bool) error {
	for _, e := range []error{
		checkText("userName", f.UserName, maxTextLen, create),
		checkEmail(f.Email, create),
		checkText("firstName", f.FirstName, maxTextLen, false),
		checkText("lastName", f.LastName, maxTextLen, false),
		checkText("mobilePhone", f.MobilePhone, maxTextLen, false),
		checkText("contact", f.Contact, maxTextLen, false),
		checkText("empNo", f.EmpNo, maxTextLen, false),
	} {
		if e != nil {
			return e
		}
	}
	if f.OrgCode != "" || create {
		return checkPattern(orgCodeRe, "orgCode", f.OrgCode)
	}
	return nil
}

func (f userFields) input(id string) knox.UserInput {
	return knox.UserInput{UserID: id, UserName: f.UserName, Email: f.Email, FirstName: f.FirstName, LastName: f.LastName,
		MobilePhone: f.MobilePhone, Contact: f.Contact, EmpNo: f.EmpNo, OrgCode: f.OrgCode}
}

func fieldsOf(u knox.User) userFields {
	return userFields{u.UserName, u.Email, u.FirstName, u.LastName, u.MobilePhone, u.Contact, u.EmpNo, u.OrgCode}
}

// userView is a Knox user plus its enrolled-device summary and the last invite on record.
type userView struct {
	knox.User
	Devices   deviceSummary `json:"devices"`
	InvitedAt string        `json:"invitedAt,omitempty"`
}

// viewsOf attaches the device summary and invite time to users: one mirror query for their
// devices, and the audit log is asked only about these users.
func (s *server) viewsOf(c fiber.Ctx, users []knox.User) ([]userView, error) {
	ids := make([]string, len(users))
	for i, u := range users {
		ids[i] = u.UserID
	}
	invites, err := s.Audit.LatestInvites(c.Context(), ids)
	if err != nil {
		return nil, err
	}
	devices, err := s.Mirror.DevicesOfUsers(c.Context(), ids)
	if err != nil {
		return nil, err
	}
	summaries := summarize(devicesOfMirror(devices), time.Now())
	views := make([]userView, len(users))
	for i, u := range users {
		d, found := summaries[u.UserID]
		if !found {
			d = deviceSummary{Freshness: freshNone}
		}
		views[i] = userView{User: u, Devices: d}
		if at, ok := invites[u.UserID]; ok {
			views[i].InvitedAt = at.UTC().Format(time.RFC3339)
		}
	}
	return views, nil
}

func (s *server) listOrgs(c fiber.Ctx) error {
	sc, err := s.scope(c)
	if err != nil {
		return err
	}
	all, err := s.Mirror.Orgs(c.Context())
	if err != nil {
		return err
	}
	return c.JSON(fiber.Map{"orgs": auth.FilterByOrg(sc, orgsOfMirror(all), func(o knox.Organization) string { return o.OrgCode })})
}

func (s *server) listUsers(c fiber.Ctx) error {
	_, orgs, err := s.scopeOrgs(c)
	if err != nil {
		return err
	}
	like := strings.ToLower(c.Query("q"))
	if err := checkText("q", like, maxGroupName, false); err != nil {
		return err
	}
	all, err := s.Mirror.Users(c.Context(), orgs)
	if err != nil {
		return err
	}
	users := []knox.User{}
	for _, u := range usersOfMirror(all) {
		if strings.Contains(strings.ToLower(u.UserID), like) {
			users = append(users, u)
		}
	}
	views, err := s.viewsOf(c, users)
	if err != nil {
		return err
	}
	return c.JSON(fiber.Map{"users": views, "total": len(views)})
}

func (s *server) getUser(c fiber.Ctx) error {
	id := c.Params("id")
	if err := checkPattern(knoxIDRe, "user id", id); err != nil {
		return err
	}
	sc, err := s.scope(c)
	if err != nil {
		return err
	}
	u, err := s.mirrorUser(c, id)
	if err != nil {
		return err
	}
	if !sc.CanReadOrg(u.OrgCode) {
		return errUserNotFound
	}
	views, err := s.viewsOf(c, []knox.User{u})
	if err != nil {
		return err
	}
	return c.JSON(views[0])
}

func (s *server) listUserGroups(c fiber.Ctx) error {
	id := c.Params("id")
	if err := checkPattern(knoxIDRe, "user id", id); err != nil {
		return err
	}
	sc, err := s.scope(c)
	if err != nil {
		return err
	}
	u, err := s.mirrorUser(c, id)
	if err != nil {
		return err
	}
	if !sc.CanReadOrg(u.OrgCode) {
		return errUserNotFound
	}
	groups, err := s.Mirror.UserGroups(c.Context(), id)
	if err != nil {
		return err
	}
	return c.JSON(fiber.Map{"groups": groupsOfMirror(groups), "total": len(groups)})
}

func (s *server) createUser(c fiber.Ctx) error {
	var req struct {
		UserID string `json:"userId"`
		userFields
		Password          string `json:"password"`
		RequestEnrollment bool   `json:"requestEnrollment"`
	}
	if err := bind(c, &req); err != nil {
		return err
	}
	if err := checkPattern(knoxIDRe, "userId", req.UserID); err != nil {
		return err
	}
	if err := req.validate(true); err != nil {
		return err
	}
	if err := checkText("password", req.Password, 128, false); err != nil {
		return err
	}
	sc, err := s.scope(c)
	if err != nil {
		return err
	}
	if err := sc.CheckWriteOrg(req.OrgCode); err != nil {
		return err
	}
	in := req.input(req.UserID)
	in.UserPassword, in.RequestEnrollment = req.Password, req.RequestEnrollment
	if err := s.Knox().CreateUser(c.Context(), in); err != nil {
		return err
	}
	s.refreshMirrorUser(c, req.UserID)
	s.record(c, "knox.user.create", req.UserID, nil, req.userFields)
	return c.Status(fiber.StatusCreated).JSON(fiber.Map{"userId": req.UserID})
}

func (s *server) updateUser(c fiber.Ctx) error {
	id := c.Params("id")
	if err := checkPattern(knoxIDRe, "user id", id); err != nil {
		return err
	}
	var req userFields
	if err := bind(c, &req); err != nil {
		return err
	}
	if err := req.validate(false); err != nil {
		return err
	}
	if req == (userFields{}) {
		return badRequest("nothing to update")
	}
	sc, err := s.scope(c)
	if err != nil {
		return err
	}
	old, err := s.knoxUser(c, id)
	if err != nil {
		return err
	}
	if err := checkTargetWrite(sc, old.OrgCode); err != nil {
		return err
	}
	if req.OrgCode != "" {
		if err := sc.CheckWriteOrg(req.OrgCode); err != nil {
			return err
		}
	}
	if err := s.Knox().UpdateUser(c.Context(), req.input(id)); err != nil {
		return err
	}
	s.refreshMirrorUser(c, id)
	s.record(c, "knox.user.update", id, fieldsOf(old), req)
	return c.JSON(fiber.Map{"userId": id})
}

func (s *server) purgeUser(c fiber.Ctx) error {
	id := c.Params("id")
	if err := checkPattern(knoxIDRe, "user id", id); err != nil {
		return err
	}
	var req struct {
		ConfirmUserID string `json:"confirmUserId"`
	}
	if err := bind(c, &req); err != nil {
		return err
	}
	if req.ConfirmUserID != id {
		return badRequest("confirmUserId must equal the user id")
	}
	sc, err := s.scope(c)
	if err != nil {
		return err
	}
	old, err := s.knoxUser(c, id)
	if err != nil {
		return err
	}
	if err := checkTargetWrite(sc, old.OrgCode); err != nil {
		return err
	}
	if err := s.Knox().PurgeUser(c.Context(), id); err != nil {
		return err
	}
	s.mirrorWrite("delete user", s.Mirror.DeleteUser(c.Context(), id))
	s.record(c, "knox.user.purge", id, old, nil)
	return c.SendStatus(fiber.StatusNoContent)
}

func (s *server) inviteUsers(c fiber.Ctx) error {
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
	sc, err := s.scope(c)
	if err != nil {
		return err
	}
	if err := s.targetsInWriteScope(c, sc, ids); err != nil {
		return err
	}
	res, err := s.Knox().RequestEnrollment(c.Context(), ids)
	if err != nil {
		return err
	}
	// Only users Knox actually emailed count as invited; the audit target still names every request.
	target := strings.Join(ids, ",")
	e := auditEntry(c, "knox.user.invite", target, nil, res)
	s.logAuditFailure(s.Audit.InsertInvites(c.Context(), e, invitedIDs(ids, res)), e.Action, target)
	return c.JSON(res)
}

func invitedIDs(ids []string, res knox.EnrollmentResult) []string {
	skip := map[string]bool{}
	for _, id := range res.NotExistUserIDs {
		skip[id] = true
	}
	for _, id := range res.UserIDsWithoutEmail {
		skip[id] = true
	}
	var ok []string
	for _, id := range ids {
		if !skip[id] {
			ok = append(ok, id)
		}
	}
	return ok
}

func (s *server) listGroups(c fiber.Ctx) error {
	groups, err := s.Mirror.Groups(c.Context())
	if err != nil {
		return err
	}
	return c.JSON(fiber.Map{"groups": groupsOfMirror(groups), "total": len(groups)})
}

func (s *server) getGroup(c fiber.Ctx) error {
	g, err := s.mirrorGroup(c)
	if err != nil {
		return err
	}
	return c.JSON(g)
}

func (s *server) createGroup(c fiber.Ctx) error {
	var req struct {
		Name string `json:"name"`
	}
	if err := bind(c, &req); err != nil {
		return err
	}
	if err := checkText("name", req.Name, maxGroupName, true); err != nil {
		return err
	}
	g, err := s.Knox().InsertGroup(c.Context(), req.Name, nil)
	if err != nil {
		return err
	}
	s.mirrorWrite("upsert group", s.Mirror.UpsertGroup(c.Context(), db.MirrorGroup{GroupID: g.GroupID, GroupName: g.GroupName, GroupType: g.GroupType}))
	s.record(c, "knox.group.create", g.GroupID, nil, g)
	return c.Status(fiber.StatusCreated).JSON(g)
}

func (s *server) deleteGroup(c fiber.Ctx) error {
	g, err := s.knoxGroup(c)
	if err != nil {
		return err
	}
	if err := s.Knox().DeleteGroup(c.Context(), g.GroupID); err != nil {
		return err
	}
	s.mirrorWrite("delete group", s.Mirror.DeleteGroup(c.Context(), g.GroupID))
	s.record(c, "knox.group.delete", g.GroupID, g, nil)
	return c.SendStatus(fiber.StatusNoContent)
}

func (s *server) listMembers(c fiber.Ctx) error {
	g, err := s.mirrorGroup(c)
	if err != nil {
		return err
	}
	_, orgs, err := s.scopeOrgs(c)
	if err != nil {
		return err
	}
	members, err := s.Mirror.GroupMembers(c.Context(), g.GroupID, orgs)
	if err != nil {
		return err
	}
	views, err := s.viewsOf(c, usersOfMirror(members))
	if err != nil {
		return err
	}
	return c.JSON(fiber.Map{"users": views, "total": len(views)})
}

func (s *server) addMembers(c fiber.Ctx) error {
	return s.changeMembers(c, "knox.group.add_members", s.Knox().InsertGroupUnits, s.Mirror.AddMembers)
}

func (s *server) removeMembers(c fiber.Ctx) error {
	return s.changeMembers(c, "knox.group.remove_members", s.Knox().DeleteGroupUnits, s.Mirror.RemoveMembers)
}

func (s *server) changeMembers(c fiber.Ctx, action string, apply, mirror func(ctx context.Context, groupID string, ids []string) error) error {
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
	g, err := s.knoxGroup(c)
	if err != nil {
		return err
	}
	sc, err := s.scope(c)
	if err != nil {
		return err
	}
	if err := s.targetsInWriteScope(c, sc, ids); err != nil {
		return err
	}
	if err := apply(c.Context(), g.GroupID, ids); err != nil {
		return err
	}
	s.mirrorWrite(action, mirror(c.Context(), g.GroupID, ids))
	s.record(c, action, g.GroupID, nil, fiber.Map{"userIds": ids})
	return c.SendStatus(fiber.StatusNoContent)
}
