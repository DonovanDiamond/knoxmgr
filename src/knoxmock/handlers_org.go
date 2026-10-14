package knoxmock

import (
	"net/http"
	"sort"
	"strings"
)

func (s *Server) orgSelectList(w http.ResponseWriter, r *http.Request) {
	code, like, name := r.FormValue("orgCode"), r.FormValue("orgCodeLike"), r.FormValue("orgName")
	s.mu.Lock()
	defer s.mu.Unlock()
	if code != "" && s.orgs[code] == nil {
		fail(w, codeNotFound, "organization not found")
		return
	}
	out := []org{}
	for _, o := range s.orgs {
		if (code == "" || s.within(o.OrgCode, code)) && (like == "" || containsFold(o.OrgCode, like)) && (name == "" || containsFold(o.OrgName, name)) {
			out = append(out, *o)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].OrgCode < out[j].OrgCode })
	ok(w, map[string]any{"orgs": out})
}

func (s *Server) orgSelect(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	defer s.mu.Unlock()
	o := s.orgs[r.FormValue("orgCode")]
	if o == nil {
		fail(w, codeNotFound, "organization not found")
		return
	}
	ok(w, map[string]any{"org": o})
}

func (s *Server) orgSelectUsers(w http.ResponseWriter, r *http.Request) {
	code := r.FormValue("orgCode")
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.orgs[code] == nil {
		fail(w, codeNotFound, "organization not found")
		return
	}
	ok(w, map[string]any{"users": s.userViews(func(u *user) bool { return u.OrgCode == code })})
}

func (s *Server) orgCreate(w http.ResponseWriter, r *http.Request) {
	_ = r.ParseForm()
	code, name, parent := r.PostFormValue("orgCode"), r.PostFormValue("orgName"), r.PostFormValue("parentCode")
	if code == "" || name == "" || len(code) > 50 {
		fail(w, codeBadParam, "orgCode (max 50) and orgName are required")
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.orgs[code] != nil {
		fail(w, codeConflict, "organization code already exists")
		return
	}
	if parent != "" && s.orgs[parent] == nil {
		fail(w, codeNotFound, "parent organization not found")
		return
	}
	o := &org{OrgCode: code, OrgName: name, ParentCode: parent, OrgType: "0", TenantID: tenantID, ValidOrg: "1", Created: now(), Updated: now()}
	s.orgs[code] = o
	s.refreshOrgShape()
	ok(w, map[string]any{"org": o})
}

func (s *Server) orgUpdate(w http.ResponseWriter, r *http.Request) {
	_ = r.ParseForm()
	code, name, parent := r.PostFormValue("orgCode"), r.PostFormValue("orgName"), r.PostFormValue("parentCode")
	s.mu.Lock()
	defer s.mu.Unlock()
	o := s.orgs[code]
	if o == nil {
		fail(w, codeNotFound, "organization not found")
		return
	}
	if parent != "" {
		if s.orgs[parent] == nil {
			fail(w, codeNotFound, "parent organization not found")
			return
		}
		// The new parent may not be the org itself or one of its descendants.
		if s.within(parent, code) {
			fail(w, codeBadParam, "parentCode would create a cycle")
			return
		}
		o.ParentCode = parent
	}
	if name != "" {
		o.OrgName = name
	}
	o.Updated = now()
	s.refreshOrgShape()
	ok(w, nil)
}

func (s *Server) orgDelete(w http.ResponseWriter, r *http.Request) {
	_ = r.ParseForm()
	code := r.PostFormValue("orgCode")
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.orgs[code] == nil {
		fail(w, codeNotFound, "organization not found")
		return
	}
	// Real Knox orphans sub-orgs; the mock refuses so the UI's error path can be exercised.
	for _, o := range s.orgs {
		if o.ParentCode == code {
			fail(w, codeConflict, "organization has sub-organizations")
			return
		}
	}
	for _, u := range s.users {
		if u.OrgCode == code {
			fail(w, codeConflict, "organization has users")
			return
		}
	}
	delete(s.orgs, code)
	s.refreshOrgShape()
	ok(w, nil)
}

func (s *Server) orgAddMembers(w http.ResponseWriter, r *http.Request) {
	_ = r.ParseForm()
	code := r.PostFormValue("orgCode")
	// Accepts the spec's ['a','b'] form as well as a plain ';' list.
	ids := splitIDs(strings.NewReplacer("[", "", "]", "", "'", "", `"`, "").Replace(r.PostFormValue("userIds")))
	if len(ids) == 0 {
		fail(w, codeBadParam, "userIds is required")
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.orgs[code] == nil {
		fail(w, codeNotFound, "organization not found")
		return
	}
	if bad := s.unknownUser(ids); bad != "" {
		fail(w, codeNotFound, "user not found: "+bad)
		return
	}
	for _, id := range ids {
		s.users[id].OrgCode = code
	}
	ok(w, nil)
}
