package knox

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/DonovanDiamond/knoxmgr/src/config"
)

// fake is a Knox stand-in: it issues tokens and hands every API call to api.
type fake struct {
	srv      *httptest.Server
	tokens   atomic.Int32
	expires  int64 // expires_in to report
	lastAuth atomic.Value
	tokForm  atomic.Value // url.Values of the last token request
	mu       sync.Mutex
	api      func(w http.ResponseWriter, r *http.Request, form url.Values)
}

func newFake(t *testing.T) (*fake, *Client) {
	t.Helper()
	f := &fake{expires: 3600}
	mux := http.NewServeMux()
	mux.HandleFunc("/emm/oauth/token", func(w http.ResponseWriter, r *http.Request) {
		_ = r.ParseForm()
		f.tokForm.Store(r.PostForm)
		n := f.tokens.Add(1)
		fmt.Fprintf(w, `{"access_token":"tok%d","token_type":"bearer","expires_in":%d}`, n, f.expires)
	})
	mux.HandleFunc("/emm/oapi/", func(w http.ResponseWriter, r *http.Request) {
		f.lastAuth.Store(r.Header.Get("Authorization"))
		form := r.URL.Query()
		if r.Method == http.MethodPost {
			b, _ := io.ReadAll(r.Body)
			form, _ = url.ParseQuery(string(b))
		}
		f.mu.Lock()
		defer f.mu.Unlock()
		f.api(w, r, form)
	})
	f.srv = httptest.NewServer(mux)
	t.Cleanup(f.srv.Close)
	f.api = func(w http.ResponseWriter, r *http.Request, _ url.Values) { ok(w, `{}`) }
	c := New(config.Knox{
		BaseURL: f.srv.URL, TokenURL: f.srv.URL + "/emm/oauth/token",
		ClientID: "cid", TenantID: "tid", ClientSecret: "s3cret",
	}, f.srv.Client())
	return f, c
}

func ok(w http.ResponseWriter, value string) {
	fmt.Fprintf(w, `{"resultCode":"0","resultMessage":"Success","resultValue":%s}`, value)
}

func TestTokenRequestAndReuse(t *testing.T) {
	f, c := newFake(t)
	for range 3 {
		if _, err := c.SelectUserWithID(context.Background(), "u1"); err != nil {
			t.Fatal(err)
		}
	}
	if got := f.tokens.Load(); got != 1 {
		t.Fatalf("token fetched %d times, want 1", got)
	}
	if got := f.lastAuth.Load(); got != "Bearer tok1" {
		t.Errorf("Authorization = %v", got)
	}
	form := f.tokForm.Load().(url.Values)
	if form.Get("grant_type") != "client_credentials" || form.Get("client_id") != "cid@tid" || form.Get("client_secret") != "s3cret" {
		t.Errorf("token form = %v", form)
	}
}

func TestTokenExpiryRefresh(t *testing.T) {
	tests := []struct {
		name      string
		expiresIn int64
		advance   time.Duration
		want      int32
	}{
		{"still valid", 3600, 30 * time.Minute, 1},
		{"inside skew window", 3600, 3560 * time.Second, 2},
		{"expired", 3600, 2 * time.Hour, 2},
		{"huge lifetime does not overflow", 997331046, 24 * time.Hour, 1},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			f, c := newFake(t)
			f.expires = tc.expiresIn
			now := time.Now()
			c.now = func() time.Time { return now }
			ctx := context.Background()
			if _, err := c.SelectUserWithID(ctx, "u"); err != nil {
				t.Fatal(err)
			}
			now = now.Add(tc.advance)
			if _, err := c.SelectUserWithID(ctx, "u"); err != nil {
				t.Fatal(err)
			}
			if got := f.tokens.Load(); got != tc.want {
				t.Errorf("tokens fetched = %d, want %d", got, tc.want)
			}
		})
	}
}

func TestUnauthorizedRetry(t *testing.T) {
	tests := []struct {
		name       string
		failures   int
		wantErr    bool
		wantTokens int32
		wantCalls  int
	}{
		{"one 401 then success", 1, false, 2, 2},
		{"persistent 401 gives up after one retry", 5, true, 2, 2},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			f, c := newFake(t)
			calls := 0
			f.api = func(w http.ResponseWriter, r *http.Request, _ url.Values) {
				calls++
				if calls <= tc.failures {
					w.WriteHeader(http.StatusUnauthorized)
					return
				}
				ok(w, `{"user":{"userId":"u"}}`)
			}
			_, err := c.SelectUserWithID(context.Background(), "u")
			if (err != nil) != tc.wantErr {
				t.Fatalf("err = %v", err)
			}
			if tc.wantErr && !errors.Is(err, ErrUnreachable) {
				t.Errorf("err = %v, want ErrUnreachable", err)
			}
			if f.tokens.Load() != tc.wantTokens || calls != tc.wantCalls {
				t.Errorf("tokens=%d calls=%d, want %d/%d", f.tokens.Load(), calls, tc.wantTokens, tc.wantCalls)
			}
			if !tc.wantErr && f.lastAuth.Load() != "Bearer tok2" {
				t.Errorf("retry used %v, want the refetched token", f.lastAuth.Load())
			}
		})
	}
}

func TestConcurrentCallersFetchTokenOnce(t *testing.T) {
	f, c := newFake(t)
	var wg sync.WaitGroup
	for range 20 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, err := c.SelectUserWithID(context.Background(), "u"); err != nil {
				t.Error(err)
			}
		}()
	}
	wg.Wait()
	if got := f.tokens.Load(); got != 1 {
		t.Fatalf("token fetched %d times, want 1", got)
	}
}

func TestResultCodeError(t *testing.T) {
	f, c := newFake(t)
	f.api = func(w http.ResponseWriter, _ *http.Request, _ url.Values) {
		fmt.Fprint(w, `{"resultCode":"1003","resultMessage":"User not found","resultValue":null}`)
	}
	_, err := c.SelectUserWithID(context.Background(), "nobody")
	var ke *Error
	if !errors.As(err, &ke) || ke.Code != "1003" || ke.Message != "User not found" {
		t.Fatalf("err = %#v", err)
	}
}

func TestErrorsLeakNoSecrets(t *testing.T) {
	f, c := newFake(t)
	f.api = func(w http.ResponseWriter, _ *http.Request, _ url.Values) {
		w.WriteHeader(http.StatusInternalServerError)
	}
	_, err := c.SelectUserWithID(context.Background(), "u")
	if err == nil {
		t.Fatal("want error")
	}
	for _, bad := range []string{"s3cret", "tok1", f.srv.URL} {
		if strings.Contains(err.Error(), bad) {
			t.Errorf("error %q leaks %q", err, bad)
		}
	}
	// A dead token endpoint must not leak the secret via the transport error either.
	c2 := New(config.Knox{BaseURL: f.srv.URL, TokenURL: "http://127.0.0.1:1/t", ClientSecret: "s3cret"}, nil)
	_, err = c2.SelectUserWithID(context.Background(), "u")
	if err == nil || strings.Contains(err.Error(), "s3cret") || strings.Contains(err.Error(), "127.0.0.1") {
		t.Errorf("token transport error = %v", err)
	}
}

func TestFormEncoding(t *testing.T) {
	ctx := context.Background()
	tests := []struct {
		name   string
		call   func(c *Client) error
		method string
		path   string
		want   url.Values
	}{
		{"createUser", func(c *Client) error {
			return c.CreateUser(ctx, UserInput{UserID: "a b&c", UserName: "N=1", Email: "a@x.io", OrgCode: "o1", RequestEnrollment: true})
		}, "POST", "/user/createUser", url.Values{"userId": {"a b&c"}, "userName": {"N=1"}, "email": {"a@x.io"}, "orgCode": {"o1"}, "requestEnrollment": {"true"}}},
		{"updateUser sends only set fields", func(c *Client) error {
			return c.UpdateUser(ctx, UserInput{UserID: "u1", Email: "n@x.io"})
		}, "POST", "/user/updateUser", url.Values{"userId": {"u1"}, "email": {"n@x.io"}}},
		{"purgeUser", func(c *Client) error { return c.PurgeUser(ctx, "u1") }, "POST", "/user/purgeUser", url.Values{"userId": {"u1"}}},
		{"requestEnrollment", func(c *Client) error {
			_, err := c.RequestEnrollment(ctx, []string{"u1", "u2"})
			return err
		}, "POST", "/user/requestEnrollment", url.Values{"userIds": {"u1;u2"}}},
		{"selectUsers", func(c *Client) error {
			_, err := c.SelectUsers(ctx, UserQuery{UserIDLike: "jo", OrgCode: "o1", Start: 20, Limit: 10})
			return err
		}, "GET", "/user/selectUsers", url.Values{"extraCondition.userIdLike": {"jo"}, "extraCondition.orgCodes": {"o1"}, "page.start": {"20"}, "page.limit": {"10"}}},
		{"selectGroups forces USER", func(c *Client) error {
			_, err := c.SelectGroups(ctx, GroupQuery{SearchName: "g"})
			return err
		}, "GET", "/group/selectGroups", url.Values{"groupType": {"USER"}, "searchName": {"g"}, "start": {"0"}}},
		{"selectGroups with counts", func(c *Client) error {
			_, err := c.SelectGroups(ctx, GroupQuery{WithCounts: true})
			return err
		}, "GET", "/group/selectGroups", url.Values{"groupType": {"USER"}, "needDeviceUserCount": {"yes"}, "start": {"0"}}},
		{"insertGroup", func(c *Client) error {
			_, err := c.InsertGroup(ctx, "Team A", []string{"u1", "u2"})
			return err
		}, "POST", "/group/insertGroup", url.Values{"groupName": {"Team A"}, "groupType": {"USER"}, "userIds": {"u1;u2"}}},
		{"deleteGroup", func(c *Client) error { return c.DeleteGroup(ctx, "g1") }, "POST", "/group/deleteGroup", url.Values{"groupIdString": {"g1"}}},
		{"selectGroupUsers", func(c *Client) error {
			_, err := c.SelectGroupUsers(ctx, "g1", 0, 50)
			return err
		}, "POST", "/group/selectGroupUsers", url.Values{"groupId": {"g1"}, "start": {"0"}, "limit": {"50"}}},
		{"insertGroupUnits", func(c *Client) error { return c.InsertGroupUnits(ctx, "g1", []string{"u1"}) }, "POST", "/group/insertGroupUnits", url.Values{"groupId": {"g1"}, "userIds": {"u1"}}},
		{"deleteGroupUnits", func(c *Client) error { return c.DeleteGroupUnits(ctx, "g1", []string{"u1", "u2"}) }, "POST", "/group/deleteGroupUnits", url.Values{"groupId": {"g1"}, "userIds": {"u1;u2"}}},
		{"org selectList", func(c *Client) error { _, err := c.SelectOrgList(ctx, "o1"); return err }, "GET", "/org/selectList", url.Values{"orgCode": {"o1"}}},
		{"org select", func(c *Client) error { _, err := c.SelectOrg(ctx, "o1"); return err }, "GET", "/org/select", url.Values{"orgCode": {"o1"}}},
		{"org selectUsers", func(c *Client) error { _, err := c.SelectOrgUsers(ctx, "o1"); return err }, "GET", "/org/selectUsers", url.Values{"orgCode": {"o1"}}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			f, c := newFake(t)
			var gotPath, gotMethod, gotCT string
			var got url.Values
			f.api = func(w http.ResponseWriter, r *http.Request, form url.Values) {
				gotPath, gotMethod, gotCT, got = strings.TrimPrefix(r.URL.Path, "/emm/oapi"), r.Method, r.Header.Get("Content-Type"), form
				ok(w, `{}`)
			}
			if err := tc.call(c); err != nil {
				t.Fatal(err)
			}
			if gotPath != tc.path || gotMethod != tc.method {
				t.Errorf("got %s %s, want %s %s", gotMethod, gotPath, tc.method, tc.path)
			}
			if tc.method == "POST" && gotCT != "application/x-www-form-urlencoded" {
				t.Errorf("Content-Type = %q", gotCT)
			}
			if fmt.Sprint(got) != fmt.Sprint(tc.want) { // fmt sorts map keys
				t.Errorf("form = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestInputValidation(t *testing.T) {
	_, c := newFake(t)
	ctx := context.Background()
	tests := []struct {
		name string
		err  error
	}{
		{"id list with delimiter", c.InsertGroupUnits(ctx, "g", []string{"a;b"})},
		{"empty id list", c.DeleteGroupUnits(ctx, "g", nil)},
		{"missing group id", c.InsertGroupUnits(ctx, "", []string{"a"})},
		{"create without email", c.CreateUser(ctx, UserInput{UserID: "u", UserName: "n"})},
		{"delete group id with delimiter", c.DeleteGroup(ctx, "g1;g2")},
		{"empty purge", c.PurgeUser(ctx, "")},
	}
	for _, tc := range tests {
		if tc.err == nil {
			t.Errorf("%s: want error", tc.name)
		}
	}
	if _, err := c.SelectUsers(ctx, UserQuery{Limit: 5000}); err == nil {
		t.Error("limit above 1000 accepted")
	}
}

func TestResponseDecoding(t *testing.T) {
	f, c := newFake(t)
	ctx := context.Background()
	f.api = func(w http.ResponseWriter, r *http.Request, _ url.Values) {
		switch r.URL.Path {
		case "/emm/oapi/user/selectUsers":
			ok(w, `{"users":{"user":[{"userId":"u1","orgCode":"o1","enabled":"1"}],"total":7}}`)
		case "/emm/oapi/group/selectGroups":
			ok(w, `{"groups":[{"groupId":"g1","groupName":"A","userCount":3,"assignAppCount":"4","assignProfileCount":5}],"total":"1"}`)
		case "/emm/oapi/group/deleteGroup":
			ok(w, `{"deleteGroupResp":{"failCount":1,"failMessages":["in use"]}}`)
		}
	}
	up, err := c.SelectUsers(ctx, UserQuery{})
	if err != nil || len(up.Users) != 1 || up.Users[0].OrgCode != "o1" || up.Total != 7 {
		t.Errorf("users = %+v, %v", up, err)
	}
	gp, err := c.SelectGroups(ctx, GroupQuery{})
	if err != nil || len(gp.Groups) != 1 || gp.Groups[0].UserCount != 3 || gp.Groups[0].AssignAppCount != 4 || gp.Groups[0].AssignProfileCount != 5 || gp.Total != 1 {
		t.Errorf("groups = %+v, %v", gp, err)
	}
	var ke *Error
	if err := c.DeleteGroup(ctx, "g1"); !errors.As(err, &ke) || ke.Message != "in use" {
		t.Errorf("deleteGroup err = %v", err)
	}
}

func TestSubOrgCodes(t *testing.T) {
	tree := []Organization{
		{OrgCode: "root"},
		{OrgCode: "a", ParentCode: "root"},
		{OrgCode: "a1", ParentCode: "a"},
		{OrgCode: "a1x", ParentCode: "a1"},
		{OrgCode: "b", ParentCode: "root"},
		{OrgCode: "loop1", ParentCode: "loop2"},
		{OrgCode: "loop2", ParentCode: "loop1"},
	}
	tests := []struct {
		name string
		org  string
		want string
	}{
		{"leaf", "a1x", "[a1x]"},
		{"subtree", "a", "[a a1 a1x]"},
		{"whole tree", "root", "[root a b a1 a1x]"},
		{"unknown org is only itself", "zzz", "[zzz]"},
		{"cycle terminates", "loop1", "[loop1 loop2]"},
	}
	f, c := newFake(t)
	var lists atomic.Int32
	f.api = func(w http.ResponseWriter, r *http.Request, _ url.Values) {
		lists.Add(1)
		var sb strings.Builder
		for i, o := range tree {
			if i > 0 {
				sb.WriteByte(',')
			}
			fmt.Fprintf(&sb, `{"orgCode":%q,"parentCode":%q}`, o.OrgCode, o.ParentCode)
		}
		ok(w, `{"orgs":[`+sb.String()+`]}`)
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := c.SubOrgCodes(context.Background(), tc.org)
			if err != nil || fmt.Sprint(got) != tc.want {
				t.Errorf("got %v, %v; want %s", got, err, tc.want)
			}
		})
	}
	if lists.Load() != 1 {
		t.Errorf("org list fetched %d times, want 1 (cached)", lists.Load())
	}
}

func TestOrgListCacheExpiry(t *testing.T) {
	f, c := newFake(t)
	var lists atomic.Int32
	f.api = func(w http.ResponseWriter, _ *http.Request, _ url.Values) {
		lists.Add(1)
		ok(w, `{"orgs":[{"orgCode":"r"}]}`)
	}
	now := time.Now()
	c.now = func() time.Time { return now }
	for _, advance := range []time.Duration{0, 59 * time.Second, 2 * time.Second} {
		now = now.Add(advance)
		if _, err := c.SubOrgCodes(context.Background(), "r"); err != nil {
			t.Fatal(err)
		}
	}
	if lists.Load() != 2 {
		t.Errorf("org list fetched %d times, want 2", lists.Load())
	}
}

func TestTransportFailuresAreUnreachable(t *testing.T) {
	f, c := newFake(t)
	f.srv.Close()
	if _, err := c.SelectUserWithID(context.Background(), "u1"); !errors.Is(err, ErrUnreachable) {
		t.Fatalf("got %v, want ErrUnreachable", err)
	}
}

func TestHTTPStatusClassification(t *testing.T) {
	tests := []struct {
		name       string
		api        func(w http.ResponseWriter, r *http.Request, _ url.Values)
		tokenFails bool
		timeout    bool
		wantStatus int // >0: *Error with this HTTPStatus; 0: ErrUnreachable
	}{
		{name: "400", api: func(w http.ResponseWriter, _ *http.Request, _ url.Values) { w.WriteHeader(400) }, wantStatus: 400},
		{name: "403", api: func(w http.ResponseWriter, _ *http.Request, _ url.Values) { w.WriteHeader(403) }, wantStatus: 403},
		{name: "404", api: func(w http.ResponseWriter, _ *http.Request, _ url.Values) { w.WriteHeader(404) }, wantStatus: 404},
		{name: "500", api: func(w http.ResponseWriter, _ *http.Request, _ url.Values) { w.WriteHeader(500) }},
		{name: "502", api: func(w http.ResponseWriter, _ *http.Request, _ url.Values) { w.WriteHeader(502) }},
		{name: "timeout", timeout: true, api: func(w http.ResponseWriter, r *http.Request, _ url.Values) { <-r.Context().Done() }},
		{name: "token endpoint failure", tokenFails: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f, c := newFake(t)
			if tt.api != nil {
				f.api = tt.api
			}
			if tt.timeout {
				c.hc.Timeout = 50 * time.Millisecond
			}
			if tt.tokenFails {
				c.cfg.TokenURL = f.srv.URL + "/missing"
			}
			_, err := c.SelectUserWithID(context.Background(), "u")
			var ke *Error
			if tt.wantStatus == 0 {
				if !errors.Is(err, ErrUnreachable) || errors.As(err, &ke) {
					t.Fatalf("err = %v, want ErrUnreachable only", err)
				}
				return
			}
			if !errors.As(err, &ke) || ke.HTTPStatus != tt.wantStatus || errors.Is(err, ErrUnreachable) {
				t.Fatalf("err = %v, want *Error with HTTPStatus %d", err, tt.wantStatus)
			}
			if !strings.Contains(err.Error(), fmt.Sprint(tt.wantStatus)) {
				t.Errorf("message %q lacks the HTTP status", err)
			}
		})
	}
}

func TestResultCodeErrorHasNoHTTPStatus(t *testing.T) {
	f, c := newFake(t)
	f.api = func(w http.ResponseWriter, _ *http.Request, _ url.Values) {
		fmt.Fprint(w, `{"resultCode":"1001","resultMessage":"nope"}`)
	}
	_, err := c.SelectUserWithID(context.Background(), "u")
	var ke *Error
	if !errors.As(err, &ke) || ke.HTTPStatus != 0 || ke.Code != "1001" {
		t.Fatalf("err = %#v", err)
	}
}

func TestOrgsCache(t *testing.T) {
	f, c := newFake(t)
	var lists atomic.Int32
	f.api = func(w http.ResponseWriter, _ *http.Request, _ url.Values) {
		lists.Add(1)
		ok(w, `{"orgs":[{"orgCode":"r"},{"orgCode":"c","parentCode":"r"}]}`)
	}
	ctx := context.Background()
	first, err := c.Orgs(ctx)
	if err != nil || len(first) != 2 {
		t.Fatalf("first = %v, %v", first, err)
	}
	first[0].OrgCode = "mutated"
	second, err := c.Orgs(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if lists.Load() != 1 {
		t.Errorf("HTTP list calls = %d, want 1 within TTL", lists.Load())
	}
	if second[0].OrgCode != "r" {
		t.Errorf("caller mutation reached the cache: %q", second[0].OrgCode)
	}
}

func TestUserDeviceCountDecodes(t *testing.T) {
	tests := []struct {
		in   string
		want DeviceCount
	}{
		{`{"deviceCount":{"total":3,"enrolled":2}}`, 2},
		{`{"deviceCount":{"total":3,"enrolled":0}}`, 0},
		{`{"deviceCount":{"total":3}}`, 3},
		{`{"deviceCount":3}`, 3},
		{`{"deviceCount":"3"}`, 3},
		{`{"deviceCount":null}`, 0},
	}
	for _, tt := range tests {
		var u User
		if err := json.Unmarshal([]byte(tt.in), &u); err != nil || u.DeviceCount != tt.want {
			t.Errorf("%s -> %d, %v; want %d", tt.in, u.DeviceCount, err, tt.want)
		}
	}
}
