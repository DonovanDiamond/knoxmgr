package knox

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"strconv"
	"strings"
)

const groupTypeUser = "USER"

// applyProfile makes Knox push the group's profiles itself when membership changes or the group goes.
const applyProfile = "1"

// GroupQuery filters and pages /group/selectGroups; only USER groups are ever requested.
type GroupQuery struct {
	GroupID    string
	SearchName string
	Start      int  // >= 0
	Limit      int  // 1..1000; 0 lets Knox default to 1000
	WithCounts bool // also ask Knox for the assigned app/profile counts
}

// SelectGroups lists user groups (GET /group/selectGroups).
func (c *Client) SelectGroups(ctx context.Context, q GroupQuery) (GroupPage, error) {
	if q.Start < 0 || q.Limit < 0 || q.Limit > 1000 {
		return GroupPage{}, errors.New("knox: invalid paging")
	}
	f := url.Values{"groupType": {groupTypeUser}, "start": {strconv.Itoa(q.Start)}}
	set(f, "groupId", q.GroupID)
	set(f, "searchName", q.SearchName)
	if q.Limit > 0 {
		f.Set("limit", strconv.Itoa(q.Limit))
	}
	if q.WithCounts {
		f.Set("needDeviceUserCount", "yes")
	}
	type result struct {
		Groups []Group `json:"groups"`
		Total  Count   `json:"total"`
	}
	r, err := call[result](ctx, c, http.MethodGet, "/group/selectGroups", f)
	return GroupPage{Groups: r.Groups, Total: r.Total}, err
}

// InsertGroup creates a USER group, optionally with initial members,
// and returns it (POST /group/insertGroup).
func (c *Client) InsertGroup(ctx context.Context, name string, userIDs []string) (Group, error) {
	if name == "" {
		return Group{}, errors.New("knox: group name is required")
	}
	f := url.Values{"groupName": {name}, "groupType": {groupTypeUser}}
	if len(userIDs) > 0 {
		ids, err := joinIDs(userIDs)
		if err != nil {
			return Group{}, err
		}
		f.Set("userIds", ids)
	}
	type result struct {
		GroupInfo Group `json:"groupInfo"`
	}
	r, err := call[result](ctx, c, http.MethodPost, "/group/insertGroup", f)
	return r.GroupInfo, err
}

// DeleteGroup deletes one group (POST /group/deleteGroup). Knox can answer
// success while failing the delete, so a non-zero failCount becomes an *Error.
func (c *Client) DeleteGroup(ctx context.Context, groupID string) error {
	ids, err := joinIDs([]string{groupID})
	if err != nil {
		return err
	}
	type result struct {
		Resp struct {
			FailCount    int      `json:"failCount"`
			FailMessages []string `json:"failMessages"`
		} `json:"deleteGroupResp"`
	}
	r, err := call[result](ctx, c, http.MethodPost, "/group/deleteGroup", url.Values{"groupIdString": {ids}, "applyProfile": {applyProfile}})
	if err == nil && r.Resp.FailCount > 0 {
		return &Error{Code: "deleteGroup", Message: strings.Join(r.Resp.FailMessages, "; ")}
	}
	return err
}

// SelectGroupUsers pages through a group's members (POST /group/selectGroupUsers).
func (c *Client) SelectGroupUsers(ctx context.Context, groupID string, start, limit int) (UserPage, error) {
	if groupID == "" || start < 0 || limit < 0 || limit > 1000 {
		return UserPage{}, errors.New("knox: invalid group query")
	}
	f := url.Values{"groupId": {groupID}, "start": {strconv.Itoa(start)}}
	if limit > 0 {
		f.Set("limit", strconv.Itoa(limit))
	}
	type result struct {
		Userlist []User `json:"userlist"`
		Total    Count  `json:"total"`
	}
	r, err := call[result](ctx, c, http.MethodPost, "/group/selectGroupUsers", f)
	return UserPage{Users: r.Userlist, Total: r.Total}, err
}

// InsertGroupUnits adds users to a group (POST /group/insertGroupUnits).
func (c *Client) InsertGroupUnits(ctx context.Context, groupID string, userIDs []string) error {
	return c.groupUnits(ctx, "/group/insertGroupUnits", groupID, userIDs)
}

// DeleteGroupUnits removes users from a group (POST /group/deleteGroupUnits).
func (c *Client) DeleteGroupUnits(ctx context.Context, groupID string, userIDs []string) error {
	return c.groupUnits(ctx, "/group/deleteGroupUnits", groupID, userIDs)
}

func (c *Client) groupUnits(ctx context.Context, path, groupID string, userIDs []string) error {
	ids, err := joinIDs(userIDs)
	if err != nil {
		return err
	}
	if groupID == "" {
		return errors.New("knox: groupId is required")
	}
	_, err = call[ignored](ctx, c, http.MethodPost, path, url.Values{"groupId": {groupID}, "userIds": {ids}, "applyProfile": {applyProfile}})
	return err
}
