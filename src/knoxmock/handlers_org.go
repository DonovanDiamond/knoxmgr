package knoxmock

import (
	"net/http"
	"sort"
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
