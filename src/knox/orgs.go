package knox

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"slices"
	"strings"
)

// SelectOrgList lists organizations (GET /org/selectList). An empty orgCode
// returns every organization; otherwise Knox returns that org's sub-orgs.
func (c *Client) SelectOrgList(ctx context.Context, orgCode string) ([]Organization, error) {
	f := url.Values{}
	set(f, "orgCode", orgCode)
	type result struct {
		Orgs []Organization `json:"orgs"`
	}
	r, err := call[result](ctx, c, http.MethodGet, "/org/selectList", f)
	return r.Orgs, err
}

// SelectOrg fetches one organization (GET /org/select).
func (c *Client) SelectOrg(ctx context.Context, orgCode string) (Organization, error) {
	if orgCode == "" {
		return Organization{}, errors.New("knox: orgCode is required")
	}
	type result struct {
		Org Organization `json:"org"`
	}
	r, err := call[result](ctx, c, http.MethodGet, "/org/select", url.Values{"orgCode": {orgCode}})
	return r.Org, err
}

// SelectOrgUsers lists the users directly in an organization (GET /org/selectUsers).
func (c *Client) SelectOrgUsers(ctx context.Context, orgCode string) ([]User, error) {
	if orgCode == "" {
		return nil, errors.New("knox: orgCode is required")
	}
	type result struct {
		Users []User `json:"users"`
	}
	r, err := call[result](ctx, c, http.MethodGet, "/org/selectUsers", url.Values{"orgCode": {orgCode}})
	return r.Users, err
}

// Orgs returns the full org list, cached for orgCacheTTL. The slice is a copy, so callers
// cannot alter the cache.
func (c *Client) Orgs(ctx context.Context) ([]Organization, error) {
	c.orgMu.Lock()
	defer c.orgMu.Unlock()
	if c.orgsSet && c.now().Sub(c.orgsAt) < orgCacheTTL {
		return slices.Clone(c.orgs), nil
	}
	orgs, err := c.SelectOrgList(ctx, "")
	if err != nil {
		return nil, err
	}
	c.orgs, c.orgsAt, c.orgsSet = orgs, c.now(), true
	return slices.Clone(orgs), nil
}

// SubOrgCodes returns orgCode followed by all of its descendants.
func (c *Client) SubOrgCodes(ctx context.Context, orgCode string) ([]string, error) {
	if orgCode == "" {
		return nil, errors.New("knox: orgCode is required")
	}
	orgs, err := c.Orgs(ctx)
	if err != nil {
		return nil, err
	}
	children := make(map[string][]string, len(orgs))
	for _, o := range orgs {
		children[o.ParentCode] = append(children[o.ParentCode], o.OrgCode)
	}
	// seen also guards against a cyclic parent chain in bad data.
	seen := map[string]bool{orgCode: true}
	out := []string{orgCode}
	for i := 0; i < len(out); i++ {
		for _, ch := range children[out[i]] {
			if !seen[ch] {
				seen[ch] = true
				out = append(out, ch)
			}
		}
	}
	return out, nil
}

// CreateOrg creates an organization (POST /org/create). An empty parentCode is omitted.
func (c *Client) CreateOrg(ctx context.Context, code, name, parentCode string) error {
	if code == "" || name == "" {
		return errors.New("knox: orgCode and orgName are required")
	}
	f := url.Values{"orgCode": {code}, "orgName": {name}}
	set(f, "parentCode", parentCode)
	return c.orgMutation(ctx, "/org/create", f)
}

// UpdateOrg renames and/or re-parents an organization (POST /org/update). Empty name or
// parentCode are omitted, which leaves that field unchanged.
func (c *Client) UpdateOrg(ctx context.Context, code, name, parentCode string) error {
	if code == "" {
		return errors.New("knox: orgCode is required")
	}
	f := url.Values{"orgCode": {code}}
	set(f, "orgName", name)
	set(f, "parentCode", parentCode)
	return c.orgMutation(ctx, "/org/update", f)
}

// DeleteOrg deletes an organization (POST /org/delete).
func (c *Client) DeleteOrg(ctx context.Context, code string) error {
	if code == "" {
		return errors.New("knox: orgCode is required")
	}
	return c.orgMutation(ctx, "/org/delete", url.Values{"orgCode": {code}})
}

// AddOrgMembers moves users into an organization (POST /org/addMembers). Unlike the group
// endpoints, the spec writes this userIds list as ['u1','u2'].
func (c *Client) AddOrgMembers(ctx context.Context, code string, userIDs []string) error {
	if code == "" {
		return errors.New("knox: orgCode is required")
	}
	if len(userIDs) == 0 {
		return errors.New("knox: at least one id is required")
	}
	quoted := make([]string, len(userIDs))
	for i, id := range userIDs {
		if id == "" || strings.ContainsAny(id, ";,'\"[]") {
			return errors.New("knox: invalid id")
		}
		quoted[i] = "'" + id + "'"
	}
	return c.orgMutation(ctx, "/org/addMembers", url.Values{"orgCode": {code}, "userIds": {"[" + strings.Join(quoted, ",") + "]"}})
}

// orgMutation posts an org change and drops the cached org list on success.
func (c *Client) orgMutation(ctx context.Context, path string, f url.Values) error {
	if _, err := call[ignored](ctx, c, http.MethodPost, path, f); err != nil {
		return err
	}
	c.orgMu.Lock()
	c.orgsSet = false
	c.orgMu.Unlock()
	return nil
}
