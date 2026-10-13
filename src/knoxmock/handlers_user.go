package knoxmock

import (
	"net/http"
	"net/mail"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
)

var idPattern = regexp.MustCompile(`^[A-Za-z0-9_.@+:-]{1,50}$`)

// splitIDs splits a ";"-separated (or ","-separated) id list, dropping blanks.
func splitIDs(v string) []string {
	var out []string
	for _, p := range strings.FieldsFunc(v, func(r rune) bool { return r == ';' || r == ',' }) {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return out
}

// page parses start/limit with the spec defaults (0, 1000) and bounds.
func page(r *http.Request, startKey, limitKey string) (start, limit int, valid bool) {
	start, limit = 0, 1000
	var err error
	if v := r.FormValue(startKey); v != "" {
		if start, err = strconv.Atoi(v); err != nil || start < 0 {
			return 0, 0, false
		}
	}
	if v := r.FormValue(limitKey); v != "" {
		if limit, err = strconv.Atoi(v); err != nil || limit <= 0 || limit > 1000 {
			return 0, 0, false
		}
	}
	return start, limit, true
}

func window[T any](items []T, start, limit int) []T {
	if start >= len(items) {
		return []T{}
	}
	return items[start:min(len(items), start+limit)]
}

func (s *Server) selectUsers(w http.ResponseWriter, r *http.Request) {
	start, limit, valid := page(r, "page.start", "page.limit")
	if !valid {
		fail(w, codeBadParam, "invalid page.start or page.limit")
		return
	}
	var after time.Time
	if v := r.FormValue("updatedAfter"); v != "" {
		t, err := time.Parse(time.RFC3339Nano, v)
		if err != nil {
			fail(w, codeBadParam, "invalid updatedAfter")
			return
		}
		after = t
	}
	id, name, like := r.FormValue("userId"), r.FormValue("userName"), r.FormValue("extraCondition.userIdLike")
	in := map[string]bool{}
	for _, v := range splitIDs(r.FormValue("extraCondition.userIds")) {
		in[v] = true
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	all := s.userViews(func(u *user) bool {
		upd, _ := time.Parse(timeLayout, u.Updated)
		return (id == "" || u.UserID == id) && (name == "" || u.UserName == name) &&
			(like == "" || strings.Contains(u.UserID, like)) && (len(in) == 0 || in[u.UserID]) &&
			(after.IsZero() || upd.After(after))
	})
	ok(w, map[string]any{"users": map[string]any{"total": len(all), "user": window(all, start, limit)}})
}

func (s *Server) selectUserWithID(w http.ResponseWriter, r *http.Request) {
	id := r.FormValue("userId")
	if !idPattern.MatchString(id) {
		fail(w, codeBadParam, "userId is required")
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	u := s.users[id]
	if u == nil {
		fail(w, codeNotFound, "user not found")
		return
	}
	ok(w, map[string]any{"user": s.userView(u)})
}

// applyUserForm copies the supplied, validated fields onto u. Caller holds s.mu.
func (s *Server) applyUserForm(r *http.Request, u *user) (msg string) {
	fields := map[string]*string{
		"userName": &u.UserName, "email": &u.Email, "firstName": &u.FirstName, "middleName": &u.MiddleName,
		"lastName": &u.LastName, "contact": &u.Contact, "mobilePhone": &u.MobilePhone, "empNo": &u.EmpNo, "orgCode": &u.OrgCode,
	}
	keys := make([]string, 0, len(fields))
	for k := range fields {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		if !r.Form.Has(k) {
			continue
		}
		v := strings.TrimSpace(r.Form.Get(k))
		switch {
		case len(v) > 100:
			return k + " too long"
		case k == "userName" && v == "":
			return "userName must not be empty"
		case k == "email":
			if a, err := mail.ParseAddress(v); err != nil || a.Address != v {
				return "invalid email"
			}
		case k == "orgCode" && s.orgs[v] == nil:
			return "unknown orgCode"
		}
		*fields[k] = v
	}
	u.Updated = now()
	return ""
}

func (s *Server) createUser(w http.ResponseWriter, r *http.Request) {
	_ = r.ParseForm()
	id := r.PostFormValue("userId")
	if !idPattern.MatchString(id) || r.PostFormValue("userName") == "" || r.PostFormValue("email") == "" {
		fail(w, codeBadParam, "userId, userName and email are required")
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.users[id] != nil {
		fail(w, codeConflict, "user already exists")
		return
	}
	u := &user{UserID: id, OrgCode: "ROOT", Enabled: "1", TenantID: tenantID, LicenseType: "KM", UserType: "0", Created: now()}
	if msg := s.applyUserForm(r, u); msg != "" {
		fail(w, codeBadParam, msg)
		return
	}
	s.users[id] = u
	ok(w, map[string]any{"enabled": true, "state": "SUCCESS", "userId": id})
}

func (s *Server) updateUser(w http.ResponseWriter, r *http.Request) {
	_ = r.ParseForm()
	id := r.PostFormValue("userId")
	if !idPattern.MatchString(id) {
		fail(w, codeBadParam, "userId is required")
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	u := s.users[id]
	if u == nil {
		fail(w, codeNotFound, "user not found")
		return
	}
	cp := *u // apply to a copy so a rejected field leaves the user untouched
	if msg := s.applyUserForm(r, &cp); msg != "" {
		fail(w, codeBadParam, msg)
		return
	}
	*u = cp
	ok(w, map[string]any{"enabled": u.Enabled == "1", "state": "SUCCESS", "userId": id})
}

func (s *Server) purgeUser(w http.ResponseWriter, r *http.Request) {
	_ = r.ParseForm()
	id := r.PostFormValue("userId")
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.users[id] == nil {
		fail(w, codeNotFound, "user not found")
		return
	}
	delete(s.users, id)
	for _, g := range s.groups {
		delete(g.members, id)
	}
	ok(w, nil)
}

func (s *Server) requestEnrollment(w http.ResponseWriter, r *http.Request) {
	_ = r.ParseForm()
	ids := splitIDs(r.PostFormValue("userIds"))
	if len(ids) == 0 {
		fail(w, codeBadParam, "userIds is required")
		return
	}
	missing, noEmail := []string{}, []string{}
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, id := range ids {
		switch u := s.users[id]; {
		case u == nil:
			missing = append(missing, id)
		case u.Email == "":
			noEmail = append(noEmail, id)
		default:
			u.RequestEnrollment = true
			s.log.Info().Str("userId", id).Str("to", u.Email).Msg("mock enrollment email would be sent")
		}
	}
	ok(w, map[string]any{"notExistUserId": missing, "userIdWithoutEmail": noEmail})
}
