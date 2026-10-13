package auth

import "github.com/DonovanDiamond/knoxmgr/src/config"

// Grant is the role a directory user receives from group membership.
type Grant struct {
	Role    string
	OrgCode string
	Access  string
}

// MapGroups resolves memberOf against the configured mappings. Admin wins over any user mapping;
// among user mappings the first in config order wins. ok is false when no mapping matches.
func MapGroups(mappings []config.ADGroup, memberOf []string) (g Grant, ok bool) {
	in := make(map[string]bool, len(memberOf))
	for _, m := range memberOf {
		in[m] = true
	}
	for _, m := range mappings {
		if !in[m.Group] {
			continue
		}
		if m.Role == config.RoleAdmin {
			return Grant{Role: config.RoleAdmin}, true
		}
		if !ok {
			g, ok = Grant{Role: config.RoleUser, OrgCode: m.OrgCode, Access: m.Access}, true
		}
	}
	return g, ok
}

func groupNames(mappings []config.ADGroup) []string {
	names := make([]string, len(mappings))
	for i, m := range mappings {
		names[i] = m.Group
	}
	return names
}
