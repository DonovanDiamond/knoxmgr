package auth

import "github.com/DonovanDiamond/knoxmgr/src/config"

// Scope is the one access decision point: every route asks it whether an org is visible or
// writable. Build it per request with NewScope.
type Scope struct {
	p     *Principal
	orgs  map[string]struct{}
	admin bool
}

// NewScope builds the scope of p. orgCodes is p.OrgCode plus its descendants, resolved by the
// caller from Knox; it is ignored for admins. A nil or unknown principal gets an empty scope.
func NewScope(p *Principal, orgCodes []string) Scope {
	s := Scope{p: p, admin: p.IsAdmin()}
	if s.admin || p == nil || p.Role != config.RoleUser {
		return s
	}
	s.orgs = make(map[string]struct{}, len(orgCodes))
	for _, c := range orgCodes {
		if c != "" {
			s.orgs[c] = struct{}{}
		}
	}
	return s
}

// CanReadOrg reports whether orgCode is inside the scope.
func (s Scope) CanReadOrg(orgCode string) bool {
	if s.admin {
		return true
	}
	_, ok := s.orgs[orgCode]
	return ok && orgCode != ""
}

// CheckWriteOrg requires write access and orgCode inside the scope; otherwise ErrForbidden.
func (s Scope) CheckWriteOrg(orgCode string) error {
	if err := RequireWrite(s.p); err != nil {
		return err
	}
	if !s.CanReadOrg(orgCode) {
		return ErrForbidden
	}
	return nil
}

// CanManageGroups reports whether the caller may create or delete groups (admins only).
func (s Scope) CanManageGroups() bool { return s.admin }

// FilterOrgs keeps only the in-scope org codes, preserving order.
func (s Scope) FilterOrgs(orgCodes []string) []string {
	out := make([]string, 0, len(orgCodes))
	for _, c := range orgCodes {
		if s.CanReadOrg(c) {
			out = append(out, c)
		}
	}
	return out
}

// FilterByOrg keeps only the items whose org code is in scope, preserving order. It is a function
// rather than a method because Go methods cannot be generic.
func FilterByOrg[T any](s Scope, items []T, orgOf func(T) string) []T {
	out := make([]T, 0, len(items))
	for _, it := range items {
		if s.CanReadOrg(orgOf(it)) {
			out = append(out, it)
		}
	}
	return out
}
