package knoxmock

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"

	"github.com/rs/zerolog"
)

const (
	testID     = "client1"
	testSecret = "s3cret"
)

type env struct {
	t     *testing.T
	ts    *httptest.Server
	token string
	logs  *bytes.Buffer
}

func newEnv(t *testing.T) *env {
	t.Helper()
	logs := &bytes.Buffer{}
	ts := httptest.NewServer(New(testID, testSecret, zerolog.New(logs)))
	t.Cleanup(ts.Close)
	e := &env{t: t, ts: ts, logs: logs}
	status, body := e.rawToken(url.Values{"grant_type": {"client_credentials"}, "client_id": {testID + "@tenant"}, "client_secret": {testSecret}}, "", "")
	if status != 200 {
		t.Fatalf("token: %d %v", status, body)
	}
	e.token = body["access_token"].(string)
	return e
}

func (e *env) rawToken(form url.Values, user, pass string) (int, map[string]any) {
	req, _ := http.NewRequest("POST", e.ts.URL+"/emm/oauth/token", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	if user != "" {
		req.SetBasicAuth(user, pass)
	}
	return e.do(req)
}

func (e *env) do(req *http.Request) (int, map[string]any) {
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		e.t.Fatal(err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	var m map[string]any
	if err := json.Unmarshal(raw, &m); err != nil {
		e.t.Fatalf("non-JSON %d: %s", resp.StatusCode, raw)
	}
	return resp.StatusCode, m
}

// call performs an authenticated op: GET with a query string or POST with a form body.
func (e *env) call(method, path string, form url.Values) (int, map[string]any) {
	var req *http.Request
	if method == "GET" {
		req, _ = http.NewRequest("GET", e.ts.URL+path+"?"+form.Encode(), nil)
	} else {
		req, _ = http.NewRequest("POST", e.ts.URL+path, strings.NewReader(form.Encode()))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	}
	req.Header.Set("Authorization", "Bearer "+e.token)
	return e.do(req)
}

func (e *env) value(method, path string, form url.Values) map[string]any {
	e.t.Helper()
	status, m := e.call(method, path, form)
	if status != 200 || m["resultCode"] != "0" {
		e.t.Fatalf("%s %s: %d %v", method, path, status, m)
	}
	v, _ := m["resultValue"].(map[string]any)
	return v
}

func TestToken(t *testing.T) {
	e := newEnv(t)
	cc := url.Values{"grant_type": {"client_credentials"}}
	tests := []struct {
		name       string
		form       url.Values
		user, pass string
		want       int
	}{
		{"form with tenant", url.Values{"grant_type": {"client_credentials"}, "client_id": {"client1@t"}, "client_secret": {testSecret}}, "", "", 200},
		{"form bare id", url.Values{"grant_type": {"client_credentials"}, "client_id": {testID}, "client_secret": {testSecret}}, "", "", 200},
		{"basic auth", cc, "client1@t", testSecret, 200},
		{"bad secret", cc, "client1@t", "nope", 401},
		{"bad client", cc, "other@t", testSecret, 401},
		{"no creds", cc, "", "", 401},
		{"bad grant", url.Values{"grant_type": {"password"}, "client_id": {testID}, "client_secret": {testSecret}}, "", "", 400},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			status, body := e.rawToken(tc.form, tc.user, tc.pass)
			if status != tc.want {
				t.Fatalf("status %d, want %d (%v)", status, tc.want, body)
			}
			if tc.want == 200 && (body["token_type"] != "bearer" || body["access_token"] == "") {
				t.Fatalf("bad token body %v", body)
			}
		})
	}
}

func TestBearerRequired(t *testing.T) {
	e := newEnv(t)
	for _, tc := range []struct{ name, header string }{{"none", ""}, {"wrong", "Bearer nope"}, {"basic", "Basic abc"}} {
		t.Run(tc.name, func(t *testing.T) {
			req, _ := http.NewRequest("GET", e.ts.URL+"/emm/oapi/org/selectList", nil)
			if tc.header != "" {
				req.Header.Set("Authorization", tc.header)
			}
			if status, body := e.do(req); status != 401 || body["error"] != "invalid_token" {
				t.Fatalf("got %d %v", status, body)
			}
		})
	}
	// Wrong method on a real path is rejected by routing, after auth.
	if status, _ := e.callRaw("POST", "/emm/oapi/org/selectList"); status != 405 {
		t.Fatalf("method check: %d", status)
	}
}

func (e *env) callRaw(method, path string) (int, []byte) {
	req, _ := http.NewRequest(method, e.ts.URL+path, nil)
	req.Header.Set("Authorization", "Bearer "+e.token)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		e.t.Fatal(err)
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, b
}

func userIDs(v map[string]any, path ...string) []string {
	var cur any = v
	for _, p := range path {
		cur = cur.(map[string]any)[p]
	}
	out := []string{}
	for _, u := range cur.([]any) {
		out = append(out, u.(map[string]any)["userId"].(string))
	}
	return out
}

func TestReads(t *testing.T) {
	e := newEnv(t)
	tests := []struct {
		name, method, path string
		form               url.Values
		want               string // comma-joined ids / codes
		get                func(map[string]any) string
	}{
		{"all users", "GET", "/emm/oapi/user/selectUsers", url.Values{}, "alice,bob,carol,dave,erin,frank",
			func(v map[string]any) string { return strings.Join(userIDs(v, "users", "user"), ",") }},
		{"users like", "GET", "/emm/oapi/user/selectUsers", url.Values{"extraCondition.userIdLike": {"a"}}, "alice,carol,dave,frank",
			func(v map[string]any) string { return strings.Join(userIDs(v, "users", "user"), ",") }},
		{"users in", "GET", "/emm/oapi/user/selectUsers", url.Values{"extraCondition.userIds": {"bob;erin"}}, "bob,erin",
			func(v map[string]any) string { return strings.Join(userIDs(v, "users", "user"), ",") }},
		{"users paged", "GET", "/emm/oapi/user/selectUsers", url.Values{"page.start": {"1"}, "page.limit": {"2"}}, "bob,carol",
			func(v map[string]any) string { return strings.Join(userIDs(v, "users", "user"), ",") }},
		{"user by id", "GET", "/emm/oapi/user/selectUserWithID", url.Values{"userId": {"dave"}}, "SALES_EMEA/Sales EMEA",
			func(v map[string]any) string {
				u := v["user"].(map[string]any)
				return u["orgCode"].(string) + "/" + u["orgName"].(string)
			}},
		{"org list all", "GET", "/emm/oapi/org/selectList", url.Values{}, "ROOT,SALES,SALES_EMEA,SUPPORT", orgCodes},
		{"org subtree", "GET", "/emm/oapi/org/selectList", url.Values{"orgCode": {"SALES"}}, "SALES,SALES_EMEA", orgCodes},
		{"org like", "GET", "/emm/oapi/org/selectList", url.Values{"orgCodeLike": {"emea"}}, "SALES_EMEA", orgCodes},
		{"org select", "GET", "/emm/oapi/org/select", url.Values{"orgCode": {"SALES_EMEA"}}, "SALES/3",
			func(v map[string]any) string {
				o := v["org"].(map[string]any)
				return o["parentCode"].(string) + "/" + o["depth"].(string)
			}},
		{"org users", "GET", "/emm/oapi/org/selectUsers", url.Values{"orgCode": {"SALES"}}, "bob,carol",
			func(v map[string]any) string { return strings.Join(userIDs(v, "users"), ",") }},
		{"groups", "GET", "/emm/oapi/group/selectGroups", url.Values{"groupType": {"USER"}, "needDeviceUserCount": {"yes"}}, "Field Staff:2,Pilot Users:3",
			func(v map[string]any) string {
				var parts []string
				for _, g := range v["groups"].([]any) {
					m := g.(map[string]any)
					parts = append(parts, m["groupName"].(string)+":"+string(rune('0'+int(m["userCount"].(float64)))))
				}
				return strings.Join(parts, ",")
			}},
		{"groups by name", "GET", "/emm/oapi/group/selectGroups", url.Values{"searchName": {"pilot"}}, "1",
			func(v map[string]any) string { return v["total"].(string) }},
		{"group users", "POST", "/emm/oapi/group/selectGroupUsers", url.Values{"groupId": {GroupPilot}}, "alice,bob,dave",
			func(v map[string]any) string { return strings.Join(userIDs(v, "userlist"), ",") }},
		{"group users search", "POST", "/emm/oapi/group/selectGroupUsers", url.Values{"groupId": {GroupPilot}, "searchUserKey": {"BO"}}, "bob",
			func(v map[string]any) string { return strings.Join(userIDs(v, "userlist"), ",") }},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.get(e.value(tc.method, tc.path, tc.form)); got != tc.want {
				t.Fatalf("got %q, want %q", got, tc.want)
			}
		})
	}
}

func orgCodes(v map[string]any) string {
	var c []string
	for _, o := range v["orgs"].([]any) {
		c = append(c, o.(map[string]any)["orgCode"].(string))
	}
	return strings.Join(c, ",")
}

func TestErrors(t *testing.T) {
	e := newEnv(t)
	tests := []struct {
		name, method, path string
		form               url.Values
		code               string
	}{
		{"user missing id", "GET", "/emm/oapi/user/selectUserWithID", url.Values{}, codeBadParam},
		{"user not found", "GET", "/emm/oapi/user/selectUserWithID", url.Values{"userId": {"zed"}}, codeNotFound},
		{"bad paging", "GET", "/emm/oapi/user/selectUsers", url.Values{"page.limit": {"5000"}}, codeBadParam},
		{"create missing email", "POST", "/emm/oapi/user/createUser", url.Values{"userId": {"n1"}, "userName": {"N"}}, codeBadParam},
		{"create bad email", "POST", "/emm/oapi/user/createUser", url.Values{"userId": {"n1"}, "userName": {"N"}, "email": {"nope"}}, codeBadParam},
		{"create bad id", "POST", "/emm/oapi/user/createUser", url.Values{"userId": {"a b"}, "userName": {"N"}, "email": {"n@x.org"}}, codeBadParam},
		{"create bad org", "POST", "/emm/oapi/user/createUser", url.Values{"userId": {"n1"}, "userName": {"N"}, "email": {"n@x.org"}, "orgCode": {"NOPE"}}, codeBadParam},
		{"create duplicate", "POST", "/emm/oapi/user/createUser", url.Values{"userId": {"alice"}, "userName": {"N"}, "email": {"n@x.org"}}, codeConflict},
		{"update not found", "POST", "/emm/oapi/user/updateUser", url.Values{"userId": {"zed"}}, codeNotFound},
		{"update bad org", "POST", "/emm/oapi/user/updateUser", url.Values{"userId": {"alice"}, "orgCode": {"NOPE"}}, codeBadParam},
		{"purge not found", "POST", "/emm/oapi/user/purgeUser", url.Values{"userId": {"zed"}}, codeNotFound},
		{"enroll no ids", "POST", "/emm/oapi/user/requestEnrollment", url.Values{}, codeBadParam},
		{"group wrong type", "POST", "/emm/oapi/group/insertGroup", url.Values{"groupName": {"G"}, "groupType": {"DEVICE"}}, codeBadParam},
		{"group no name", "POST", "/emm/oapi/group/insertGroup", url.Values{"groupType": {"USER"}}, codeBadParam},
		{"group unknown member", "POST", "/emm/oapi/group/insertGroup", url.Values{"groupName": {"G"}, "groupType": {"USER"}, "userIds": {"zed"}}, codeNotFound},
		{"delete no ids", "POST", "/emm/oapi/group/deleteGroup", url.Values{}, codeBadParam},
		{"group users unknown group", "POST", "/emm/oapi/group/selectGroupUsers", url.Values{"groupId": {"nope"}}, codeNotFound},
		{"add unknown group", "POST", "/emm/oapi/group/insertGroupUnits", url.Values{"groupId": {"nope"}, "userIds": {"bob"}}, codeNotFound},
		{"add unknown user", "POST", "/emm/oapi/group/insertGroupUnits", url.Values{"groupId": {GroupPilot}, "userIds": {"zed"}}, codeNotFound},
		{"remove no users", "POST", "/emm/oapi/group/deleteGroupUnits", url.Values{"groupId": {GroupPilot}}, codeBadParam},
		{"org not found", "GET", "/emm/oapi/org/select", url.Values{"orgCode": {"NOPE"}}, codeNotFound},
		{"org users not found", "GET", "/emm/oapi/org/selectUsers", url.Values{"orgCode": {"NOPE"}}, codeNotFound},
		{"org list not found", "GET", "/emm/oapi/org/selectList", url.Values{"orgCode": {"NOPE"}}, codeNotFound},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			status, m := e.call(tc.method, tc.path, tc.form)
			if status != 200 || m["resultCode"] != tc.code || m["resultMessage"] == "" {
				t.Fatalf("got %d %v, want resultCode %s", status, m, tc.code)
			}
		})
	}
}

func TestUserLifecycle(t *testing.T) {
	e := newEnv(t)
	e.value("POST", "/emm/oapi/user/createUser", url.Values{"userId": {"gina"}, "userName": {"Gina G"}, "email": {"gina@example.org"}, "orgCode": {"SUPPORT"}, "userPassword": {"hunter2!"}})
	u := e.value("GET", "/emm/oapi/user/selectUserWithID", url.Values{"userId": {"gina"}})["user"].(map[string]any)
	if u["orgName"] != "Support" || u["email"] != "gina@example.org" {
		t.Fatalf("created user %v", u)
	}
	if strings.Contains(u["userName"].(string), "hunter2") || u["userPassword"] != nil {
		t.Fatal("password must not be stored or returned")
	}

	// A rejected field must not partially apply the update.
	if _, m := e.call("POST", "/emm/oapi/user/updateUser", url.Values{"userId": {"gina"}, "userName": {"Changed"}, "email": {"bad"}}); m["resultCode"] == "0" {
		t.Fatal("expected rejection")
	}
	e.value("POST", "/emm/oapi/user/updateUser", url.Values{"userId": {"gina"}, "email": {"g2@example.org"}, "orgCode": {"SALES"}})
	u = e.value("GET", "/emm/oapi/user/selectUserWithID", url.Values{"userId": {"gina"}})["user"].(map[string]any)
	if u["userName"] != "Gina G" || u["email"] != "g2@example.org" || u["orgCode"] != "SALES" {
		t.Fatalf("updated user %v", u)
	}

	// Purge removes the user from groups too.
	e.value("POST", "/emm/oapi/group/insertGroupUnits", url.Values{"groupId": {GroupField}, "userIds": {"gina"}})
	e.value("POST", "/emm/oapi/user/purgeUser", url.Values{"userId": {"gina"}})
	if _, m := e.call("GET", "/emm/oapi/user/selectUserWithID", url.Values{"userId": {"gina"}}); m["resultCode"] != codeNotFound {
		t.Fatalf("after purge: %v", m)
	}
	got := strings.Join(userIDs(e.value("POST", "/emm/oapi/group/selectGroupUsers", url.Values{"groupId": {GroupField}}), "userlist"), ",")
	if got != "carol,erin" {
		t.Fatalf("group members after purge: %s", got)
	}
}

func TestRequestEnrollment(t *testing.T) {
	e := newEnv(t)
	v := e.value("POST", "/emm/oapi/user/requestEnrollment", url.Values{"userIds": {"alice;zed;bob"}})
	if got := v["notExistUserId"].([]any); len(got) != 1 || got[0] != "zed" {
		t.Fatalf("notExistUserId %v", got)
	}
	logs := e.logs.String()
	if !strings.Contains(logs, "alice@example.org") || !strings.Contains(logs, "bob@example.org") || strings.Contains(logs, testSecret) {
		t.Fatalf("log output: %s", logs)
	}
}

func TestGroupLifecycle(t *testing.T) {
	e := newEnv(t)
	v := e.value("POST", "/emm/oapi/group/insertGroup", url.Values{"groupName": {"Temp"}, "groupType": {"USER"}, "userIds": {"alice;frank"}})
	id := v["groupInfo"].(map[string]any)["groupId"].(string)
	members := func() string {
		return strings.Join(userIDs(e.value("POST", "/emm/oapi/group/selectGroupUsers", url.Values{"groupId": {id}}), "userlist"), ",")
	}
	steps := []struct {
		name, path string
		form       url.Values
		want       string
	}{
		{"initial", "", nil, "alice,frank"},
		{"add (idempotent)", "/emm/oapi/group/insertGroupUnits", url.Values{"groupId": {id}, "userIds": {"alice;bob"}}, "alice,bob,frank"},
		{"remove", "/emm/oapi/group/deleteGroupUnits", url.Values{"groupId": {id}, "userIds": {"alice;carol"}}, "bob,frank"},
	}
	for _, s := range steps {
		if s.path != "" {
			e.value("POST", s.path, s.form)
		}
		if got := members(); got != s.want {
			t.Fatalf("%s: members %s, want %s", s.name, got, s.want)
		}
	}
	// Atomic: an unknown user rejects the whole add.
	e.call("POST", "/emm/oapi/group/insertGroupUnits", url.Values{"groupId": {id}, "userIds": {"carol;zed"}})
	if got := members(); got != "bob,frank" {
		t.Fatalf("not atomic: %s", got)
	}

	d := e.value("POST", "/emm/oapi/group/deleteGroup", url.Values{"groupIdString": {id + ";missing"}})["deleteGroupResp"].(map[string]any)
	if d["successCount"] != 1.0 || d["failCount"] != 1.0 || d["total"] != 2.0 || d["failGroupIds"].([]any)[0] != "missing" {
		t.Fatalf("deleteGroupResp %v", d)
	}
	if got := e.value("GET", "/emm/oapi/group/selectGroups", url.Values{"groupId": {id}})["total"]; got != "0" {
		t.Fatalf("group still present: %v", got)
	}
}

func TestConcurrent(t *testing.T) {
	e := newEnv(t)
	var wg sync.WaitGroup
	for i := range 20 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			id := "u" + string(rune('a'+i))
			e.call("POST", "/emm/oapi/user/createUser", url.Values{"userId": {id}, "userName": {id}, "email": {id + "@x.org"}})
			e.call("POST", "/emm/oapi/group/insertGroupUnits", url.Values{"groupId": {GroupPilot}, "userIds": {id}})
			e.call("GET", "/emm/oapi/user/selectUsers", url.Values{})
			e.call("POST", "/emm/oapi/user/purgeUser", url.Values{"userId": {id}})
		}()
	}
	wg.Wait()
}
