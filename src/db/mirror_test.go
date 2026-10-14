package db

import (
	"context"
	"errors"
	"reflect"
	"testing"
	"time"
)

func mirrorSnapshot() Snapshot {
	return Snapshot{
		Orgs: []MirrorOrg{{"root", "Root", ""}, {"a", "A", "root"}, {"a1", "A1", "a"}, {"b", "B", "root"}},
		Users: []MirrorUser{
			{UserID: "u1", UserName: "one", OrgCode: "a", Enabled: true},
			{UserID: "u2", UserName: "two", OrgCode: "a1"},
			{UserID: "u3", UserName: "three", OrgCode: "b"},
			{UserID: "_AManagement", UserName: "_AManagement", OrgCode: "a"},
		},
		Groups:  []MirrorGroup{{GroupID: "g1", GroupName: "G1", GroupType: "USER", UserCount: 99, AppCount: 4, ProfileCount: 2}, {GroupID: "g2", GroupName: "G2"}},
		Members: []MirrorMember{{"g1", "u1"}, {"g1", "u2"}, {"g1", "u3"}},
		Devices: []MirrorDevice{
			{DeviceID: "d1", DeviceModel: "M", UserID: "u2", OrgCode: "stale"},
			{DeviceID: "d2", DeviceModel: "M", UserID: "u3"},
			{DeviceID: "d3", DeviceModel: "M"},
		},
	}
}

func newMirror(t *testing.T) (*Mirror, context.Context) {
	t.Helper()
	m := NewMirror(testDB(t))
	ctx := context.Background()
	if err := m.ReplaceAll(ctx, mirrorSnapshot(), time.Now()); err != nil {
		t.Fatal(err)
	}
	return m, ctx
}

func userIDs(us []MirrorUser) []string {
	ids := []string{}
	for _, u := range us {
		ids = append(ids, u.UserID)
	}
	return ids
}

func deviceIDs(ds []MirrorDevice) []string {
	ids := []string{}
	for _, d := range ds {
		ids = append(ids, d.DeviceID)
	}
	return ids
}

func TestMirrorGroupCountsRoundTrip(t *testing.T) {
	m, ctx := newMirror(t)
	if err := m.UpsertGroup(ctx, MirrorGroup{GroupID: "g1", GroupName: "Renamed", GroupType: "USER"}); err != nil {
		t.Fatal(err)
	}
	if err := m.UpsertGroup(ctx, MirrorGroup{GroupID: "g3", GroupName: "New", GroupType: "USER"}); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		id        string
		app, prof int
	}{{"g1", 4, 2}, {"g2", 0, 0}, {"g3", 0, 0}} {
		g, err := m.Group(ctx, tc.id)
		if err != nil || g.AppCount != tc.app || g.ProfileCount != tc.prof {
			t.Errorf("%s = %+v, %v; want counts %d/%d", tc.id, g, err, tc.app, tc.prof)
		}
	}
	ugs, _ := m.UserGroups(ctx, "u1")
	if len(ugs) != 1 || ugs[0].AppCount != 4 || ugs[0].ProfileCount != 2 {
		t.Errorf("UserGroups = %+v", ugs)
	}
	all, _ := m.Groups(ctx)
	if len(all) != 3 || all[2].GroupID != "g1" || all[2].AppCount != 4 {
		t.Errorf("Groups = %+v", all)
	}
}

func TestMirrorReplaceAllDropsAbsentRows(t *testing.T) {
	m, ctx := newMirror(t)
	g, err := m.Group(ctx, "g1")
	if err != nil || g.UserCount != 3 {
		t.Fatalf("group = %+v, %v (user_count must come from memberships)", g, err)
	}
	s := Snapshot{
		Orgs:    []MirrorOrg{{"root", "Root", ""}},
		Users:   []MirrorUser{{UserID: "u1", UserName: "one", OrgCode: "root"}},
		Groups:  []MirrorGroup{{GroupID: "g1", GroupName: "G1"}},
		Members: []MirrorMember{{"g1", "u1"}},
	}
	if err := m.ReplaceAll(ctx, s, time.Now()); err != nil {
		t.Fatal(err)
	}
	orgs, _ := m.Orgs(ctx)
	users, _ := m.Users(ctx, nil)
	groups, _ := m.Groups(ctx)
	devs, _ := m.Devices(ctx, nil)
	if len(orgs) != 1 || len(users) != 1 || len(groups) != 1 || len(devs) != 0 {
		t.Fatalf("orgs=%d users=%d groups=%d devices=%d", len(orgs), len(users), len(groups), len(devs))
	}
	if g, _ := m.Group(ctx, "g1"); g.UserCount != 1 {
		t.Fatalf("user_count = %d", g.UserCount)
	}
	if _, err := m.User(ctx, "u2"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("want ErrNotFound, got %v", err)
	}
}

func TestMirrorSubOrgCodes(t *testing.T) {
	m, ctx := newMirror(t)
	for _, tc := range []struct {
		code string
		want []string
	}{
		{"root", []string{"a", "a1", "b", "root"}},
		{"a", []string{"a", "a1"}},
		{"a1", []string{"a1"}},
		{"nope", []string{}},
	} {
		got, err := m.SubOrgCodes(ctx, tc.code)
		if err != nil || !reflect.DeepEqual(got, tc.want) {
			t.Errorf("SubOrgCodes(%q) = %v, %v; want %v", tc.code, got, err, tc.want)
		}
	}
}

func TestMirrorScope(t *testing.T) {
	m, ctx := newMirror(t)
	for _, tc := range []struct {
		name        string
		orgs        []string
		users       []string
		groupMember []string
		devices     []string
	}{
		{"unrestricted", nil, []string{"u1", "u3", "u2"}, []string{"u1", "u3", "u2"}, []string{"d1", "d2", "d3"}},
		{"nothing", []string{}, []string{}, []string{}, []string{}},
		{"a subtree", []string{"a", "a1"}, []string{"u1", "u2"}, []string{"u1", "u2"}, []string{"d1"}},
		{"a1 only", []string{"a1"}, []string{"u2"}, []string{"u2"}, []string{"d1"}},
		{"b", []string{"b"}, []string{"u3"}, []string{"u3"}, []string{"d2"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			us, err := m.Users(ctx, tc.orgs)
			if err != nil || !reflect.DeepEqual(userIDs(us), tc.users) {
				t.Errorf("Users = %v, %v; want %v", userIDs(us), err, tc.users)
			}
			gm, err := m.GroupMembers(ctx, "g1", tc.orgs)
			if err != nil || !reflect.DeepEqual(userIDs(gm), tc.groupMember) {
				t.Errorf("GroupMembers = %v, %v; want %v", userIDs(gm), err, tc.groupMember)
			}
			ds, err := m.Devices(ctx, tc.orgs)
			if err != nil || !reflect.DeepEqual(deviceIDs(ds), tc.devices) {
				t.Errorf("Devices = %v, %v; want %v", deviceIDs(ds), err, tc.devices)
			}
		})
	}
	if u, err := m.User(ctx, "_AManagement"); err != nil || u.UserID != "_AManagement" {
		t.Fatalf("system user must stay readable by id: %+v, %v", u, err)
	}
	d, err := m.Device(ctx, "d1")
	if err != nil || d.OrgCode != "a1" {
		t.Fatalf("device org must be the owner's: %+v, %v", d, err)
	}
	if _, err := m.Device(ctx, "zz"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("want ErrNotFound, got %v", err)
	}
	ds, err := m.DevicesOfUsers(ctx, []string{"u2", "u3"})
	if err != nil || !reflect.DeepEqual(deviceIDs(ds), []string{"d1", "d2"}) {
		t.Fatalf("DevicesOfUsers = %v, %v", deviceIDs(ds), err)
	}
}

func TestMirrorWriteThrough(t *testing.T) {
	m, ctx := newMirror(t)
	count := func(id string) int {
		g, err := m.Group(ctx, id)
		if err != nil {
			t.Fatal(err)
		}
		return g.UserCount
	}

	if err := m.UpsertUser(ctx, MirrorUser{UserID: "u4", UserName: "four", OrgCode: "b"}); err != nil {
		t.Fatal(err)
	}
	if err := m.UpsertUser(ctx, MirrorUser{UserID: "u4", UserName: "FOUR", OrgCode: "b"}); err != nil {
		t.Fatal(err)
	}
	if u, err := m.User(ctx, "u4"); err != nil || u.UserName != "FOUR" {
		t.Fatalf("upsert user = %+v, %v", u, err)
	}

	if err := m.AddMembers(ctx, "g2", []string{"u1", "u4", "u1"}); err != nil {
		t.Fatal(err)
	}
	if count("g2") != 2 {
		t.Fatalf("g2 count after add = %d", count("g2"))
	}
	if err := m.RemoveMembers(ctx, "g2", []string{"u1"}); err != nil {
		t.Fatal(err)
	}
	if count("g2") != 1 {
		t.Fatalf("g2 count after remove = %d", count("g2"))
	}
	gs, _ := m.UserGroups(ctx, "u4")
	if len(gs) != 1 || gs[0].GroupID != "g2" {
		t.Fatalf("UserGroups = %+v", gs)
	}

	if err := m.DeleteUser(ctx, "u4"); err != nil {
		t.Fatal(err)
	}
	if count("g2") != 0 {
		t.Fatalf("g2 count after user delete = %d", count("g2"))
	}
	if err := m.DeleteUser(ctx, "u3"); err != nil {
		t.Fatal(err)
	}
	if count("g1") != 2 {
		t.Fatalf("g1 count = %d", count("g1"))
	}

	if err := m.UpsertGroup(ctx, MirrorGroup{GroupID: "g1", GroupName: "Renamed", GroupType: "USER"}); err != nil {
		t.Fatal(err)
	}
	if g, _ := m.Group(ctx, "g1"); g.GroupName != "Renamed" || g.UserCount != 2 {
		t.Fatalf("upsert group kept wrong data: %+v", g)
	}
	if err := m.DeleteGroup(ctx, "g1"); err != nil {
		t.Fatal(err)
	}
	if _, err := m.Group(ctx, "g1"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("want ErrNotFound, got %v", err)
	}
	if gs, _ := m.UserGroups(ctx, "u1"); len(gs) != 0 {
		t.Fatalf("memberships of deleted group remain: %+v", gs)
	}
}

func TestMirrorRuns(t *testing.T) {
	m := NewMirror(testDB(t))
	ctx := context.Background()
	if _, err := m.LastSuccess(ctx); !errors.Is(err, ErrNotFound) {
		t.Fatalf("want ErrNotFound, got %v", err)
	}
	id1, err := m.StartRun(ctx, "schedule")
	if err != nil {
		t.Fatal(err)
	}
	if err := m.FinishRun(ctx, id1, "ok", "", SyncCounts{Orgs: 1, Users: 2, Groups: 3, Memberships: 4, Devices: 5}); err != nil {
		t.Fatal(err)
	}
	id2, _ := m.StartRun(ctx, "manual")
	if err := m.FinishRun(ctx, id2, "failed", "boom", SyncCounts{}); err != nil {
		t.Fatal(err)
	}
	id3, _ := m.StartRun(ctx, "manual")

	runs, err := m.RecentRuns(ctx, 2)
	if err != nil || len(runs) != 2 || runs[0].ID != id3 || runs[1].ID != id2 {
		t.Fatalf("RecentRuns = %+v, %v", runs, err)
	}
	if runs[0].Status != "running" || runs[0].FinishedAt != nil {
		t.Fatalf("running run = %+v", runs[0])
	}
	if runs[1].Status != "failed" || runs[1].Error != "boom" || runs[1].FinishedAt == nil {
		t.Fatalf("failed run = %+v", runs[1])
	}
	last, err := m.LastSuccess(ctx)
	if err != nil || last.ID != id1 || last.Trigger != "schedule" || last.SyncCounts != (SyncCounts{1, 2, 3, 4, 5}) {
		t.Fatalf("LastSuccess = %+v, %v", last, err)
	}
	if err := m.FinishRun(ctx, 9999, "ok", "", SyncCounts{}); !errors.Is(err, ErrNotFound) {
		t.Fatalf("want ErrNotFound, got %v", err)
	}
}
