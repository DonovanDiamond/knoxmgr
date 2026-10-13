package knoxmock

import (
	"net/http"
	"sort"
	"strconv"
)

// unknownUser returns the first id that is not a known user, or "" when all exist. Caller holds s.mu.
func (s *Server) unknownUser(ids []string) string {
	for _, id := range ids {
		if s.users[id] == nil {
			return id
		}
	}
	return ""
}

func (s *Server) selectGroups(w http.ResponseWriter, r *http.Request) {
	start, limit, valid := page(r, "start", "limit")
	if !valid {
		fail(w, codeBadParam, "invalid start or limit")
		return
	}
	id, name, typ := r.FormValue("groupId"), r.FormValue("searchName"), r.FormValue("groupType")
	counts := r.FormValue("needDeviceUserCount") == "yes"

	s.mu.Lock()
	defer s.mu.Unlock()
	out := []group{}
	for _, g := range s.groups {
		if (id == "" || g.GroupID == id) && (name == "" || containsFold(g.GroupName, name)) && (typ == "" || g.GroupType == typ) {
			v := *g
			if counts {
				n := len(g.members)
				v.UserCount = &n
			}
			out = append(out, v)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].GroupName < out[j].GroupName })
	ok(w, map[string]any{"groups": window(out, start, limit), "total": strconv.Itoa(len(out))})
}

func (s *Server) insertGroup(w http.ResponseWriter, r *http.Request) {
	_ = r.ParseForm()
	name := r.PostFormValue("groupName")
	if name == "" || len(name) > 50 || r.PostFormValue("groupType") != "USER" {
		fail(w, codeBadParam, "groupName (max 50) is required and groupType must be USER")
		return
	}
	ids := splitIDs(r.PostFormValue("userIds"))
	s.mu.Lock()
	defer s.mu.Unlock()
	if bad := s.unknownUser(ids); bad != "" {
		fail(w, codeNotFound, "user not found: "+bad)
		return
	}
	g := s.newGroup(randHex(16), name)
	for _, id := range ids {
		g.members[id] = struct{}{}
	}
	ok(w, map[string]any{"groupInfo": g})
}

func (s *Server) deleteGroup(w http.ResponseWriter, r *http.Request) {
	_ = r.ParseForm()
	ids := splitIDs(r.PostFormValue("groupIdString"))
	if len(ids) == 0 {
		fail(w, codeBadParam, "groupIdString is required")
		return
	}
	failIDs, failMsgs := []string{}, []string{}
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, id := range ids {
		if s.groups[id] == nil {
			failIDs, failMsgs = append(failIDs, id), append(failMsgs, "can not find group id.")
			continue
		}
		delete(s.groups, id)
	}
	ok(w, map[string]any{"deleteGroupResp": map[string]any{
		"total": len(ids), "successCount": len(ids) - len(failIDs), "failCount": len(failIDs),
		"failGroupIds": failIDs, "failMessages": failMsgs,
	}})
}

func (s *Server) selectGroupUsers(w http.ResponseWriter, r *http.Request) {
	_ = r.ParseForm()
	start, limit, valid := page(r, "start", "limit")
	if !valid {
		fail(w, codeBadParam, "invalid start or limit")
		return
	}
	key := r.FormValue("searchUserKey")
	s.mu.Lock()
	defer s.mu.Unlock()
	g := s.groups[r.FormValue("groupId")]
	if g == nil {
		fail(w, codeNotFound, "group not found")
		return
	}
	all := s.userViews(func(u *user) bool {
		_, member := g.members[u.UserID]
		return member && (key == "" || containsFold(u.UserID, key) || containsFold(u.UserName, key))
	})
	ok(w, map[string]any{"total": strconv.Itoa(len(all)), "userlist": window(all, start, limit)})
}

func (s *Server) insertGroupUnits(w http.ResponseWriter, r *http.Request) { s.changeUnits(w, r, true) }
func (s *Server) deleteGroupUnits(w http.ResponseWriter, r *http.Request) { s.changeUnits(w, r, false) }

// changeUnits is all-or-nothing: any unknown group or user rejects the whole request.
func (s *Server) changeUnits(w http.ResponseWriter, r *http.Request, add bool) {
	_ = r.ParseForm()
	ids := splitIDs(r.PostFormValue("userIds"))
	if len(ids) == 0 {
		fail(w, codeBadParam, "userIds is required")
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	g := s.groups[r.PostFormValue("groupId")]
	if g == nil {
		fail(w, codeNotFound, "group not found")
		return
	}
	if bad := s.unknownUser(ids); bad != "" {
		fail(w, codeNotFound, "user not found: "+bad)
		return
	}
	for _, id := range ids {
		if add {
			g.members[id] = struct{}{}
		} else {
			delete(g.members, id)
		}
	}
	ok(w, nil)
}
