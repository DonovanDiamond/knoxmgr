package auth

import (
	"errors"
	"testing"
)

func TestScope(t *testing.T) {
	admin := &Principal{Role: "admin"}
	writer := &Principal{Role: "user", OrgCode: "A", Access: "write"}
	reader := &Principal{Role: "user", OrgCode: "A", Access: "read"}
	orgs := []string{"A", "A1"}

	tests := []struct {
		name      string
		p         *Principal
		orgs      []string
		org       string
		read      bool
		write     bool
		groupsMgr bool
	}{
		{"admin any org", admin, nil, "Z", true, true, true},
		{"admin ignores orgCodes", admin, orgs, "Z", true, true, true},
		{"writer own org", writer, orgs, "A", true, true, false},
		{"writer sub org", writer, orgs, "A1", true, true, false},
		{"writer other org", writer, orgs, "B", false, false, false},
		{"writer empty org", writer, append([]string{""}, orgs...), "", false, false, false},
		{"reader in scope", reader, orgs, "A1", true, false, false},
		{"reader out of scope", reader, orgs, "B", false, false, false},
		{"user with no orgs", writer, nil, "A", false, false, false},
		{"nil principal", nil, orgs, "A", false, false, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := NewScope(tt.p, tt.orgs)
			if got := s.CanReadOrg(tt.org); got != tt.read {
				t.Errorf("CanReadOrg = %v, want %v", got, tt.read)
			}
			err := s.CheckWriteOrg(tt.org)
			if tt.write && err != nil || !tt.write && !errors.Is(err, ErrForbidden) {
				t.Errorf("CheckWriteOrg = %v, want write=%v", err, tt.write)
			}
			if got := s.CanManageGroups(); got != tt.groupsMgr {
				t.Errorf("CanManageGroups = %v, want %v", got, tt.groupsMgr)
			}
		})
	}
}

func TestScopeFilters(t *testing.T) {
	type member struct{ name, org string }
	members := []member{{"a", "A"}, {"b", "B"}, {"c", "A1"}}
	s := NewScope(&Principal{Role: "user", OrgCode: "A", Access: "read"}, []string{"A", "A1"})

	if got := s.FilterOrgs([]string{"B", "A1", "A", "C"}); len(got) != 2 || got[0] != "A1" || got[1] != "A" {
		t.Errorf("FilterOrgs = %v", got)
	}
	got := FilterByOrg(s, members, func(m member) string { return m.org })
	if len(got) != 2 || got[0].name != "a" || got[1].name != "c" {
		t.Errorf("FilterByOrg = %v", got)
	}
	adm := NewScope(&Principal{Role: "admin"}, nil)
	if got := FilterByOrg(adm, members, func(m member) string { return m.org }); len(got) != 3 {
		t.Errorf("admin FilterByOrg = %v", got)
	}
	if got := NewScope(nil, nil).FilterOrgs([]string{"A"}); got == nil || len(got) != 0 {
		t.Errorf("empty scope FilterOrgs = %#v, want empty non-nil", got)
	}
}
