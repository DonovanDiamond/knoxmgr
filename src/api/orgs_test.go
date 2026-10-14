package api

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/DonovanDiamond/knoxmgr/src/knox"
)

// orgKnox records the org calls (the Knox mock has no org mutations) and delegates the rest.
type orgKnox struct {
	Knox
	calls []string
	err   error
}

func (f *orgKnox) CreateOrg(_ context.Context, code, name, parent string) error {
	f.calls = append(f.calls, fmt.Sprintf("create %s %s %s", code, name, parent))
	return f.err
}

func (f *orgKnox) UpdateOrg(_ context.Context, code, name, parent string) error {
	f.calls = append(f.calls, fmt.Sprintf("update %s %s %s", code, name, parent))
	return f.err
}

func (f *orgKnox) DeleteOrg(_ context.Context, code string) error {
	f.calls = append(f.calls, "delete "+code)
	return f.err
}

func (f *orgKnox) AddOrgMembers(_ context.Context, code string, ids []string) error {
	f.calls = append(f.calls, fmt.Sprintf("members %s %s", code, strings.Join(ids, ",")))
	return f.err
}

func newOrgEnv(t *testing.T) (*env, *orgKnox) {
	t.Helper()
	f := &orgKnox{}
	e := newEnvWith(t, func(d *Deps) {
		inner := d.Knox
		d.Knox = func() Knox { f.Knox = inner(); return f }
	})
	return e, f
}

func TestOrgRoutes(t *testing.T) {
	tests := []struct {
		name, user, method, path, body string
		want                           int
		calls                          string
	}{
		{"read admin", "root", "GET", "/api/knox/orgs/SALES", "", 200, ""},
		{"read in scope", "reader", "GET", "/api/knox/orgs/SALES_EMEA", "", 200, ""},
		{"read out of scope", "reader", "GET", "/api/knox/orgs/SUPPORT", "", 404, ""},
		{"read parent of scope", "writer", "GET", "/api/knox/orgs/ROOT", "", 404, ""},
		{"read unknown", "root", "GET", "/api/knox/orgs/NOPE", "", 404, ""},
		{"read bad code", "root", "GET", "/api/knox/orgs/a%20b", "", 400, ""},

		{"create", "root", "POST", "/api/knox/orgs", `{"orgCode":"NEW+1","orgName":"New","parentCode":"SALES"}`, 201, "create NEW+1 New SALES"},
		{"create write user", "writer", "POST", "/api/knox/orgs", `{"orgCode":"NEW","orgName":"New","parentCode":"SALES"}`, 403, ""},
		{"create read user", "reader", "POST", "/api/knox/orgs", `{"orgCode":"NEW","orgName":"New","parentCode":"SALES"}`, 403, ""},
		{"create bad code", "root", "POST", "/api/knox/orgs", `{"orgCode":"a b","orgName":"New","parentCode":"SALES"}`, 400, ""},
		{"create long code", "root", "POST", "/api/knox/orgs", `{"orgCode":"` + strings.Repeat("a", 51) + `","orgName":"New","parentCode":"SALES"}`, 400, ""},
		{"create empty name", "root", "POST", "/api/knox/orgs", `{"orgCode":"NEW","orgName":"","parentCode":"SALES"}`, 400, ""},
		{"create unknown parent", "root", "POST", "/api/knox/orgs", `{"orgCode":"NEW","orgName":"New","parentCode":"NOPE"}`, 400, ""},
		{"create existing", "root", "POST", "/api/knox/orgs", `{"orgCode":"SALES","orgName":"New","parentCode":"ROOT"}`, 409, ""},

		{"update", "root", "PUT", "/api/knox/orgs/SUPPORT", `{"orgName":"Help","parentCode":"SALES"}`, 200, "update SUPPORT Help SALES"},
		{"update top-level keeps top-level", "root", "PUT", "/api/knox/orgs/ROOT", `{"orgName":"Renamed","parentCode":""}`, 200, "update ROOT Renamed "},
		{"update sub-org to top-level", "root", "PUT", "/api/knox/orgs/SUPPORT", `{"orgName":"Help","parentCode":""}`, 400, ""},
		{"update write user", "writer", "PUT", "/api/knox/orgs/SALES_EMEA", `{"orgName":"x","parentCode":"SALES"}`, 403, ""},
		{"update self parent", "root", "PUT", "/api/knox/orgs/SALES", `{"orgName":"x","parentCode":"SALES"}`, 400, ""},
		{"update under descendant", "root", "PUT", "/api/knox/orgs/SALES", `{"orgName":"x","parentCode":"SALES_EMEA"}`, 400, ""},
		{"update unknown", "root", "PUT", "/api/knox/orgs/NOPE", `{"orgName":"x","parentCode":"ROOT"}`, 404, ""},

		{"delete", "root", "DELETE", "/api/knox/orgs/SUPPORT", "", 204, "delete SUPPORT"},
		{"delete write user", "writer", "DELETE", "/api/knox/orgs/SALES_EMEA", "", 403, ""},
		{"delete unknown", "root", "DELETE", "/api/knox/orgs/NOPE", "", 404, ""},

		{"move", "root", "POST", "/api/knox/orgs/SUPPORT/members", `{"userIds":["bob","carol"]}`, 204, "members SUPPORT bob,carol"},
		{"move write user", "writer", "POST", "/api/knox/orgs/SALES/members", `{"userIds":["bob"]}`, 403, ""},
		{"move read user", "reader", "POST", "/api/knox/orgs/SALES/members", `{"userIds":["bob"]}`, 403, ""},
		{"move hidden user", "root", "POST", "/api/knox/orgs/SUPPORT/members", `{"userIds":["bob","_AManagement"]}`, 404, ""},
		{"move unknown user", "root", "POST", "/api/knox/orgs/SUPPORT/members", `{"userIds":["nobody"]}`, 404, ""},
		{"move unknown org", "root", "POST", "/api/knox/orgs/NOPE/members", `{"userIds":["bob"]}`, 404, ""},
		{"move no ids", "root", "POST", "/api/knox/orgs/SUPPORT/members", `{"userIds":[]}`, 400, ""},

		{"create hidden user", "writer", "POST", "/api/knox/users", `{"userId":"_evil","userName":"E","email":"e@example.org","orgCode":"SALES"}`, 400, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			e, f := newOrgEnv(t)
			resp := do(t, e.app, tt.method, tt.path, tt.body, e.token(t, tt.user))
			if resp.StatusCode != tt.want {
				t.Fatalf("status %d, want %d", resp.StatusCode, tt.want)
			}
			if got := strings.Join(f.calls, ";"); got != tt.calls {
				t.Fatalf("knox calls %q, want %q", got, tt.calls)
			}
		})
	}
}

func TestOrgGetBody(t *testing.T) {
	e, _ := newOrgEnv(t)
	var body struct {
		Org     knox.Organization `json:"org"`
		Members []userView        `json:"members"`
	}
	decode(t, do(t, e.app, "GET", "/api/knox/orgs/SALES", "", e.token(t, "root")), &body)
	if body.Org.OrgCode != "SALES" || len(body.Members) == 0 {
		t.Fatalf("body %+v", body)
	}
	for _, m := range body.Members {
		if m.OrgCode != "SALES" || strings.HasPrefix(m.UserID, "_") || m.Devices.Freshness == "" {
			t.Fatalf("member %+v", m)
		}
	}
}

func TestOrgMutationsMirrorAndAudit(t *testing.T) {
	e, f := newOrgEnv(t)
	tok := e.token(t, "root")
	do(t, e.app, "POST", "/api/knox/orgs", `{"orgCode":"NEW","orgName":"New","parentCode":"SALES"}`, tok)
	do(t, e.app, "POST", "/api/knox/orgs/NEW/members", `{"userIds":["bob"]}`, tok)
	do(t, e.app, "PUT", "/api/knox/orgs/NEW", `{"orgName":"Newer","parentCode":"ROOT"}`, tok)
	var body struct {
		Members []knox.User `json:"members"`
	}
	decode(t, do(t, e.app, "GET", "/api/knox/orgs/NEW", "", tok), &body)
	if len(body.Members) != 1 || body.Members[0].UserID != "bob" || body.Members[0].OrgName != "Newer" {
		t.Fatalf("members after move %+v", body.Members)
	}
	got, err := e.audit.ListRecent(context.Background(), 10)
	if err != nil || len(got) != 3 {
		t.Fatalf("audit %v err %v", got, err)
	}
	if got[1].Action != "knox.org.add_members" || got[1].PreviousValue == nil || !strings.Contains(*got[1].PreviousValue, `"bob":"SALES"`) {
		t.Fatalf("move audit %+v", got[1])
	}
	if got[0].Action != "knox.org.update" || !strings.Contains(*got[0].PreviousValue, `"orgName":"New"`) {
		t.Fatalf("update audit %+v", got[0])
	}

	// A Knox refusal changes neither the mirror nor the audit log, and keeps the knox_error shape.
	f.err = &knox.Error{Code: "1", Message: "org has users"}
	resp := do(t, e.app, "DELETE", "/api/knox/orgs/NEW", "", tok)
	var eb map[string]any
	decode(t, resp, &eb)
	if resp.StatusCode != 502 {
		t.Fatalf("status %d body %v", resp.StatusCode, eb)
	}
	if b, _ := json.Marshal(eb); !strings.Contains(string(b), "org has users") {
		t.Fatalf("knox message lost: %s", b)
	}
	if o, err := e.mirror.Org(context.Background(), "NEW"); err != nil || o.OrgName != "Newer" {
		t.Fatalf("mirror org after refused delete: %+v %v", o, err)
	}
}
