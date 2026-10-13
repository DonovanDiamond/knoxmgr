package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"
	"testing"

	"github.com/DonovanDiamond/knoxmgr/src/auth"
	"github.com/DonovanDiamond/knoxmgr/src/db"
	"github.com/DonovanDiamond/knoxmgr/src/knoxmock"
)

func (e *env) token(t *testing.T, user string) string {
	t.Helper()
	b, _ := e.login(t, user)
	return b.AccessToken
}

// listIDs decodes {"<key>":[{<idKey>:..}]} into the id column.
func listIDs(t *testing.T, e *env, path, token, key, idKey string) []string {
	t.Helper()
	var body map[string]json.RawMessage
	decode(t, do(t, e.app, "GET", path, "", token), &body)
	var items []map[string]any
	if err := json.Unmarshal(body[key], &items); err != nil {
		t.Fatal(err)
	}
	out := []string{}
	for _, it := range items {
		out = append(out, fmt.Sprint(it[idKey]))
	}
	return out
}

func TestKnoxUserListScope(t *testing.T) {
	tests := []struct {
		user string
		want string
	}{
		{"root", "alice bob carol dave erin frank"},
		{"writer", "bob carol dave"}, // SALES and SALES_EMEA only
		{"reader", "bob carol dave"},
	}
	for _, tt := range tests {
		t.Run(tt.user, func(t *testing.T) {
			e := newEnv(t)
			got := strings.Join(listIDs(t, e, "/api/knox/users", e.token(t, tt.user), "users", "userId"), " ")
			if got != tt.want {
				t.Fatalf("got %q, want %q", got, tt.want)
			}
		})
	}
}

func TestKnoxRoutes(t *testing.T) {
	const newUser = `{"userId":"zoe","userName":"Zoe Z","email":"zoe@example.org","orgCode":"%s"}`
	tests := []struct {
		name, user, method, path, body string
		status                         int
		code                           string
	}{
		{"no token", "", "GET", "/api/knox/users", "", 401, ""},
		{"get in scope", "writer", "GET", "/api/knox/users/dave", "", 200, ""},
		{"get out of scope", "writer", "GET", "/api/knox/users/alice", "", 404, ""},
		{"get unknown", "root", "GET", "/api/knox/users/zed", "", 404, ""},
		{"get bad id", "root", "GET", "/api/knox/users/a;b", "", 400, ""},

		{"create in scope", "writer", "POST", "/api/knox/users", fmt.Sprintf(newUser, "SALES_EMEA"), 201, ""},
		{"create out of scope", "writer", "POST", "/api/knox/users", fmt.Sprintf(newUser, "SUPPORT"), 403, ""},
		{"create as reader", "reader", "POST", "/api/knox/users", fmt.Sprintf(newUser, "SALES"), 403, ""},
		{"create bad email", "root", "POST", "/api/knox/users", `{"userId":"zoe","userName":"Z","email":"nope","orgCode":"ROOT"}`, 400, ""},
		{"create bad id", "root", "POST", "/api/knox/users", `{"userId":"a b","userName":"Z","email":"z@example.org","orgCode":"ROOT"}`, 400, ""},
		{"create duplicate is a knox error", "root", "POST", "/api/knox/users", `{"userId":"bob","userName":"B","email":"b@example.org","orgCode":"ROOT"}`, 502, "knox_error"},

		{"update in scope", "writer", "PUT", "/api/knox/users/bob", `{"userName":"Bobby"}`, 200, ""},
		{"update move out of scope", "writer", "PUT", "/api/knox/users/bob", `{"orgCode":"SUPPORT"}`, 403, ""},
		{"update move within scope", "writer", "PUT", "/api/knox/users/bob", `{"orgCode":"SALES_EMEA"}`, 200, ""},
		{"update out of scope target", "writer", "PUT", "/api/knox/users/alice", `{"userName":"X"}`, 404, ""},
		{"update unknown", "root", "PUT", "/api/knox/users/zed", `{"userName":"X"}`, 404, ""},
		{"update empty", "root", "PUT", "/api/knox/users/bob", `{}`, 400, ""},
		{"update as reader", "reader", "PUT", "/api/knox/users/bob", `{"userName":"X"}`, 403, ""},

		{"purge ok", "writer", "DELETE", "/api/knox/users/bob", `{"confirmUserId":"bob"}`, 204, ""},
		{"purge confirm mismatch", "writer", "DELETE", "/api/knox/users/bob", `{"confirmUserId":"carol"}`, 400, ""},
		{"purge no body", "writer", "DELETE", "/api/knox/users/bob", ``, 400, ""},
		{"purge out of scope", "writer", "DELETE", "/api/knox/users/alice", `{"confirmUserId":"alice"}`, 404, ""},
		{"purge as reader", "reader", "DELETE", "/api/knox/users/bob", `{"confirmUserId":"bob"}`, 403, ""},
		{"purge unknown", "root", "DELETE", "/api/knox/users/zed", `{"confirmUserId":"zed"}`, 404, ""},

		{"invite ok", "writer", "POST", "/api/knox/users/invite", `{"userIds":["bob","dave"]}`, 200, ""},
		{"invite one out of scope", "writer", "POST", "/api/knox/users/invite", `{"userIds":["bob","alice"]}`, 404, ""},
		{"invite as reader", "reader", "POST", "/api/knox/users/invite", `{"userIds":["bob"]}`, 403, ""},
		{"invite empty", "root", "POST", "/api/knox/users/invite", `{"userIds":[]}`, 400, ""},
		{"invite too many", "root", "POST", "/api/knox/users/invite", `{"userIds":[` + strings.TrimSuffix(strings.Repeat(`"a",`, 101), ",") + `]}`, 400, ""},
		{"invite unknown", "root", "POST", "/api/knox/users/invite", `{"userIds":["zed"]}`, 404, ""},

		{"groups list", "reader", "GET", "/api/knox/groups", "", 200, ""},
		{"group get", "reader", "GET", "/api/knox/groups/" + knoxmock.GroupPilot, "", 200, ""},
		{"group get unknown", "reader", "GET", "/api/knox/groups/nope", "", 404, ""},
		{"group create as admin", "root", "POST", "/api/knox/groups", `{"name":"New"}`, 201, ""},
		{"group create as writer", "writer", "POST", "/api/knox/groups", `{"name":"New"}`, 403, ""},
		{"group create empty name", "root", "POST", "/api/knox/groups", `{"name":""}`, 400, ""},
		{"group delete as admin", "root", "DELETE", "/api/knox/groups/" + knoxmock.GroupField, "", 204, ""},
		{"group delete as writer", "writer", "DELETE", "/api/knox/groups/" + knoxmock.GroupField, "", 403, ""},
		{"group delete unknown", "root", "DELETE", "/api/knox/groups/nope", "", 404, ""},

		{"members list", "writer", "GET", "/api/knox/groups/" + knoxmock.GroupPilot + "/members", "", 200, ""},
		{"members add in scope", "writer", "POST", "/api/knox/groups/" + knoxmock.GroupPilot + "/members", `{"userIds":["carol"]}`, 204, ""},
		{"members add out of scope", "writer", "POST", "/api/knox/groups/" + knoxmock.GroupPilot + "/members", `{"userIds":["carol","erin"]}`, 404, ""},
		{"members add as reader", "reader", "POST", "/api/knox/groups/" + knoxmock.GroupPilot + "/members", `{"userIds":["carol"]}`, 403, ""},
		{"members add unknown group", "root", "POST", "/api/knox/groups/nope/members", `{"userIds":["carol"]}`, 404, ""},
		{"members remove in scope", "writer", "DELETE", "/api/knox/groups/" + knoxmock.GroupPilot + "/members", `{"userIds":["bob"]}`, 204, ""},
		{"members remove out of scope", "writer", "DELETE", "/api/knox/groups/" + knoxmock.GroupPilot + "/members", `{"userIds":["alice"]}`, 404, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			e := newEnv(t)
			tok := ""
			if tt.user != "" {
				tok = e.token(t, tt.user)
			}
			resp := do(t, e.app, tt.method, tt.path, tt.body, tok)
			if resp.StatusCode != tt.status {
				var eb errorBody
				decode(t, resp, &eb)
				t.Fatalf("status %d, want %d (%+v)", resp.StatusCode, tt.status, eb)
			}
			if tt.code != "" {
				var eb errorBody
				decode(t, resp, &eb)
				if eb.Code != tt.code {
					t.Fatalf("code %q, want %q", eb.Code, tt.code)
				}
			}
		})
	}
}

func TestKnoxMutationsAreAudited(t *testing.T) {
	e := newEnv(t)
	tok := e.token(t, "writer")
	do(t, e.app, "PUT", "/api/knox/users/bob", `{"userName":"Bobby"}`, tok)
	do(t, e.app, "POST", "/api/knox/users", `{"userId":"zoe","userName":"Zoe","email":"z@example.org","orgCode":"SALES","password":"hunter2hunter2"}`, tok)
	do(t, e.app, "DELETE", "/api/knox/users/bob", `{"confirmUserId":"bob"}`, tok)
	got, err := e.audit.ListRecent(context.Background(), 10)
	if err != nil || len(got) != 3 {
		t.Fatalf("entries %v err %v", got, err)
	}
	if got[0].Action != "knox.user.purge" || got[0].PreviousValue == nil || got[0].ActorUsername != "writer" {
		t.Fatalf("purge entry %+v", got[0])
	}
	if got[2].Action != "knox.user.update" || !strings.Contains(*got[2].PreviousValue, "Bob Brown") || !strings.Contains(*got[2].NewValue, "Bobby") {
		t.Fatalf("update entry %+v", got[2])
	}
	for _, g := range got {
		for _, v := range []*string{g.PreviousValue, g.NewValue} {
			if v != nil && strings.Contains(*v, "hunter2") {
				t.Fatalf("password leaked into audit: %s", *v)
			}
		}
	}
}

func TestMemberListIsScopeFiltered(t *testing.T) {
	e := newEnv(t)
	path := "/api/knox/groups/" + knoxmock.GroupPilot + "/members"
	if got := strings.Join(listIDs(t, e, path, e.token(t, "writer"), "users", "userId"), " "); got != "bob dave" {
		t.Fatalf("writer sees %q, want %q", got, "bob dave")
	}
	if got := strings.Join(listIDs(t, e, path, e.token(t, "root"), "users", "userId"), " "); got != "alice bob dave" {
		t.Fatalf("admin sees %q", got)
	}
}

func TestLoginRateLimit(t *testing.T) {
	e := newEnv(t)
	for i := range 5 {
		if got := do(t, e.app, "POST", "/api/auth/login", `{"username":"root","password":"wrongpass"}`, "").StatusCode; got != 401 {
			t.Fatalf("attempt %d status %d", i, got)
		}
	}
	if got := do(t, e.app, "POST", "/api/auth/login", `{"username":"root","password":"password1"}`, "").StatusCode; got != 429 {
		t.Fatalf("status %d, want 429", got)
	}
}

func TestAccountStateIsCheckedPerRequest(t *testing.T) {
	tests := []struct {
		name   string
		change func(t *testing.T, e *env, u *db.User)
		status int
	}{
		{"disabled", func(t *testing.T, e *env, u *db.User) {
			u.Disabled = true
			if err := e.users.Update(context.Background(), u); err != nil {
				t.Fatal(err)
			}
		}, 403},
		{"deleted", func(t *testing.T, e *env, u *db.User) {
			if err := e.users.Delete(context.Background(), u.ID); err != nil {
				t.Fatal(err)
			}
		}, 401},
		{"demoted to read", func(t *testing.T, e *env, u *db.User) {
			a := "read"
			u.Access = &a
			if err := e.users.Update(context.Background(), u); err != nil {
				t.Fatal(err)
			}
		}, 403}, // a create now needs write access
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			e := newEnv(t)
			tok := e.token(t, "writer")
			u, _ := e.users.GetByUsername(context.Background(), "writer")
			tt.change(t, e, u)
			resp := do(t, e.app, "POST", "/api/knox/users", `{"userId":"zoe","userName":"Z","email":"z@example.org","orgCode":"SALES"}`, tok)
			if resp.StatusCode != tt.status {
				t.Fatalf("status %d, want %d", resp.StatusCode, tt.status)
			}
		})
	}
}

func TestAuthStatusSourceConflict(t *testing.T) {
	wrapped := fmt.Errorf("login: %w", db.ErrSourceConflict)
	if authStatus(wrapped) != 409 || authError(wrapped).Error == "" {
		t.Fatalf("status %d", authStatus(wrapped))
	}
	if authStatus(errors.New("x")) != 500 {
		t.Fatal("unknown error should be 500")
	}
}

func TestAdminRoutes(t *testing.T) {
	tests := []struct {
		name, user, method, path, body string
		status                         int
	}{
		{"list as writer", "writer", "GET", "/api/admin/users", "", 403},
		{"list as admin", "root", "GET", "/api/admin/users", "", 200},
		{"get", "root", "GET", "/api/admin/users/1", "", 200},
		{"get unknown", "root", "GET", "/api/admin/users/999", "", 404},
		{"get bad id", "root", "GET", "/api/admin/users/x", "", 400},
		{"create user", "root", "POST", "/api/admin/users", `{"username":"Newbie","password":"password1","role":"user","orgCode":"SUPPORT","access":"read"}`, 201},
		{"create admin", "root", "POST", "/api/admin/users", `{"username":"boss","password":"password1","role":"admin"}`, 201},
		{"create admin with org", "root", "POST", "/api/admin/users", `{"username":"boss","password":"password1","role":"admin","orgCode":"X"}`, 400},
		{"create user without org", "root", "POST", "/api/admin/users", `{"username":"u1","password":"password1","role":"user","access":"read"}`, 400},
		{"create bad access", "root", "POST", "/api/admin/users", `{"username":"u1","password":"password1","role":"user","orgCode":"A","access":"all"}`, 400},
		{"create short password", "root", "POST", "/api/admin/users", `{"username":"u1","password":"x","role":"admin"}`, 400},
		{"create bad username", "root", "POST", "/api/admin/users", `{"username":"a b","password":"password1","role":"admin"}`, 400},
		{"create duplicate", "root", "POST", "/api/admin/users", `{"username":"writer","password":"password1","role":"admin"}`, 409},
		{"update org and access", "root", "PUT", "/api/admin/users/3", `{"orgCode":"SUPPORT","access":"read"}`, 200},
		{"update disable user", "root", "PUT", "/api/admin/users/3", `{"disabled":true}`, 200},
		{"update promote", "root", "PUT", "/api/admin/users/3", `{"role":"admin"}`, 200},
		{"demote needs org", "root", "PUT", "/api/admin/users/1", `{"role":"user"}`, 400},
		{"demote last admin", "root", "PUT", "/api/admin/users/1", `{"role":"user","orgCode":"SALES","access":"read"}`, 409},
		{"update bad role", "root", "PUT", "/api/admin/users/3", `{"role":"god"}`, 400},
		{"disable last admin", "root", "PUT", "/api/admin/users/1", `{"disabled":true}`, 409},
		{"rename last admin", "root", "PUT", "/api/admin/users/1", `{"displayName":"Boss"}`, 200},
		{"delete user", "root", "DELETE", "/api/admin/users/3", "", 204},
		{"delete self", "root", "DELETE", "/api/admin/users/1", "", 409},
		{"delete unknown", "root", "DELETE", "/api/admin/users/999", "", 404},
		{"audit", "root", "GET", "/api/admin/audit?limit=5", "", 200},
		{"audit bad limit", "root", "GET", "/api/admin/audit?limit=0", "", 400},
		{"audit as writer", "writer", "GET", "/api/admin/audit", "", 403},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			e := newEnv(t)
			resp := do(t, e.app, tt.method, tt.path, tt.body, e.token(t, tt.user))
			if resp.StatusCode != tt.status {
				var eb errorBody
				decode(t, resp, &eb)
				t.Fatalf("status %d, want %d (%+v)", resp.StatusCode, tt.status, eb)
			}
		})
	}
}

func TestAdminDemoteWithAnotherAdminRevokesSessions(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	hash, _ := auth.HashPassword("password1")
	second := &db.User{Username: "root2", DisplayName: "r2", PasswordHash: &hash, Role: "admin", AuthSource: "local"}
	if err := e.users.Create(ctx, second); err != nil {
		t.Fatal(err)
	}
	_, cookie := e.login(t, "root2")
	tok := e.token(t, "root")
	body := `{"role":"user","orgCode":"SALES","access":"read"}`
	if got := do(t, e.app, "PUT", fmt.Sprintf("/api/admin/users/%d", second.ID), body, tok).StatusCode; got != 200 {
		t.Fatalf("demote status %d", got)
	}
	if got := do(t, e.app, "POST", "/api/auth/refresh", "", "", cookie).StatusCode; got != 401 {
		t.Fatalf("refresh after demote = %d, want 401 (sessions revoked)", got)
	}
}

func TestAdminAuditHasNoPasswords(t *testing.T) {
	e := newEnv(t)
	tok := e.token(t, "root")
	do(t, e.app, "POST", "/api/admin/users", `{"username":"newbie","password":"s3cretpass","role":"admin"}`, tok)
	got, _ := e.audit.ListRecent(context.Background(), 5)
	if len(got) == 0 || got[0].Action != "app_user.create" || got[0].NewValue == nil {
		t.Fatalf("entries %+v", got)
	}
	if v := *got[0].NewValue; strings.Contains(v, "s3cretpass") || strings.Contains(v, "$2a$") {
		t.Fatalf("secret in audit: %s", v)
	}
}

func TestKnoxUserDevicesJSON(t *testing.T) {
	e := newEnv(t)
	tok := e.token(t, "root")
	if r := do(t, e.app, "POST", "/api/knox/users/invite", `{"userIds":["bob","erin"]}`, tok); r.StatusCode != 200 {
		t.Fatalf("invite status %d", r.StatusCode)
	}
	var list struct {
		Users []map[string]any `json:"users"`
		Total int              `json:"total"`
	}
	decode(t, do(t, e.app, "GET", "/api/knox/users", "", tok), &list)
	got := map[string]string{}
	for _, u := range list.Users {
		if _, has := u["enrollment"]; has {
			t.Errorf("%v still carries enrollment", u["userId"])
		}
		d := u["devices"].(map[string]any)
		got[u["userId"].(string)] = fmt.Sprint(d["enrolled"], "|", d["freshness"], "|", d["lastConnection"] != nil)
	}
	want := map[string]string{ // frank's only device is not enrolled; bob's never connected
		"alice": "2|stale|true", "bob": "1|old|false", "carol": "1|old|true", "dave": "0|none|false", "erin": "0|none|false", "frank": "0|none|false",
	}
	for id, w := range want {
		if got[id] != w {
			t.Errorf("%s = %q, want %q", id, got[id], w)
		}
	}
	var one map[string]any
	decode(t, do(t, e.app, "GET", "/api/knox/users/bob", "", tok), &one)
	if one["invitedAt"] == nil || one["userId"] != "bob" || one["devices"].(map[string]any)["freshness"] != "old" {
		t.Errorf("get bob = %v", one)
	}
}

func TestInviteRecordsOnlyEmailedUsers(t *testing.T) {
	e := newEnv(t)
	tok := e.token(t, "root")
	if r := do(t, e.app, "POST", "/api/knox/users/invite", `{"userIds":["bob","erin"]}`, tok); r.StatusCode != 200 {
		t.Fatalf("invite status %d", r.StatusCode)
	}
	for id, wantInvited := range map[string]bool{"bob": true, "erin": false} { // erin has no email
		var u map[string]any
		decode(t, do(t, e.app, "GET", "/api/knox/users/"+id, "", tok), &u)
		if got := u["invitedAt"] != nil && u["invitedAt"] != ""; got != wantInvited {
			t.Errorf("%s invitedAt = %v, want invited=%v", id, u["invitedAt"], wantInvited)
		}
	}
}

func TestKnoxMembersCarryDevices(t *testing.T) {
	e := newEnv(t)
	tok := e.token(t, "root")
	var got struct {
		Users []map[string]any `json:"users"`
	}
	decode(t, do(t, e.app, "GET", "/api/knox/groups/"+knoxmock.GroupPilot+"/members", "", tok), &got)
	if len(got.Users) == 0 {
		t.Fatal("no members")
	}
	for _, u := range got.Users {
		if d, _ := u["devices"].(map[string]any); d["freshness"] == nil {
			t.Errorf("member %v has no device summary", u["userId"])
		}
	}
}

func TestKnoxOrgsScope(t *testing.T) {
	tests := []struct {
		user string
		want string
	}{
		{"root", "ROOT SALES SALES_EMEA SUPPORT"},
		{"writer", "SALES SALES_EMEA"},
		{"reader", "SALES SALES_EMEA"},
	}
	for _, tt := range tests {
		t.Run(tt.user, func(t *testing.T) {
			e := newEnv(t)
			got := listIDs(t, e, "/api/knox/orgs", e.token(t, tt.user), "orgs", "orgCode")
			slices.Sort(got)
			if strings.Join(got, " ") != tt.want {
				t.Fatalf("got %v, want %q", got, tt.want)
			}
		})
	}
	if r := do(t, newEnv(t).app, "GET", "/api/knox/orgs", "", ""); r.StatusCode != 401 {
		t.Fatalf("no token: %d", r.StatusCode)
	}
}

func TestKnoxGroupCountsJSON(t *testing.T) {
	e := newEnv(t)
	tok := e.token(t, "root")
	if _, err := e.db.Exec(`UPDATE knox_groups SET app_count = 3, profile_count = 2 WHERE group_id = ?`, knoxmock.GroupPilot); err != nil {
		t.Fatal(err)
	}
	var one map[string]any
	decode(t, do(t, e.app, "GET", "/api/knox/groups/"+knoxmock.GroupPilot, "", tok), &one)
	if one["assignAppCount"] != float64(3) || one["assignProfileCount"] != float64(2) {
		t.Errorf("group detail = %v", one)
	}
}
