package knox

import (
	"context"
	"net/http"
	"net/url"
	"testing"
)

func TestOrgMutations(t *testing.T) {
	ctx := context.Background()
	tests := []struct {
		name     string
		do       func(c *Client) error
		wantPath string
		want     url.Values
	}{
		{"create", func(c *Client) error { return c.CreateOrg(ctx, "A", "Alpha", "ROOT") }, "/emm/oapi/org/create",
			url.Values{"orgCode": {"A"}, "orgName": {"Alpha"}, "parentCode": {"ROOT"}}},
		{"create without parent", func(c *Client) error { return c.CreateOrg(ctx, "A", "Alpha", "") }, "/emm/oapi/org/create",
			url.Values{"orgCode": {"A"}, "orgName": {"Alpha"}}},
		{"update", func(c *Client) error { return c.UpdateOrg(ctx, "A", "Beta", "") }, "/emm/oapi/org/update",
			url.Values{"orgCode": {"A"}, "orgName": {"Beta"}}},
		{"delete", func(c *Client) error { return c.DeleteOrg(ctx, "A") }, "/emm/oapi/org/delete", url.Values{"orgCode": {"A"}}},
		{"addMembers", func(c *Client) error { return c.AddOrgMembers(ctx, "A", []string{"u1", "u2"}) }, "/emm/oapi/org/addMembers",
			url.Values{"orgCode": {"A"}, "userIds": {"['u1','u2']"}}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			f, c := newFake(t)
			var gotPath string
			var got url.Values
			f.api = func(w http.ResponseWriter, r *http.Request, form url.Values) {
				if r.Method != http.MethodPost {
					t.Errorf("method %s", r.Method)
				}
				gotPath, got = r.URL.Path, form
				ok(w, `{}`)
			}
			if err := tc.do(c); err != nil {
				t.Fatal(err)
			}
			if gotPath != tc.wantPath || got.Encode() != tc.want.Encode() {
				t.Fatalf("got %s %v, want %s %v", gotPath, got, tc.wantPath, tc.want)
			}
		})
	}
}

func TestOrgMutationValidation(t *testing.T) {
	ctx := context.Background()
	_, c := newFake(t)
	for name, err := range map[string]error{
		"create no name":      c.CreateOrg(ctx, "A", "", ""),
		"update no code":      c.UpdateOrg(ctx, "", "x", ""),
		"delete no code":      c.DeleteOrg(ctx, ""),
		"members no ids":      c.AddOrgMembers(ctx, "A", nil),
		"members quote in id": c.AddOrgMembers(ctx, "A", []string{"a'b"}),
	} {
		if err == nil {
			t.Errorf("%s: want error", name)
		}
	}
}

func TestOrgMutationDropsCache(t *testing.T) {
	ctx := context.Background()
	f, c := newFake(t)
	lists := 0
	f.api = func(w http.ResponseWriter, r *http.Request, _ url.Values) {
		if r.URL.Path == "/emm/oapi/org/selectList" {
			lists++
		}
		ok(w, `{"orgs":[]}`)
	}
	for i := 0; i < 2; i++ {
		if _, err := c.Orgs(ctx); err != nil {
			t.Fatal(err)
		}
	}
	if lists != 1 {
		t.Fatalf("cache not used: %d lists", lists)
	}
	if err := c.DeleteOrg(ctx, "A"); err != nil {
		t.Fatal(err)
	}
	if _, err := c.Orgs(ctx); err != nil || lists != 2 {
		t.Fatalf("cache not dropped: %d lists, err %v", lists, err)
	}
}
