package db

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"testing"
	"time"
)

var ctx0 = context.Background()

func newApps(t *testing.T) (*Apps, *Mirror) {
	t.Helper()
	m, ctx := newMirror(t)
	a := NewApps(m.db)
	at := time.Now()
	// d1 (org a1) and d2 (org b) both have whatsapp; d3 is unassigned.
	for id, rows := range map[string][]DeviceAppRow{
		"d1": {{Package: "com.whatsapp", Name: "WhatsApp", Version: "1"}, {Package: "com.x.sys", Name: "Sys", System: true}, {Package: "com.unknown", Name: "Zed"}},
		"d2": {{Package: "com.whatsapp", Name: "WhatsApp", Version: "2", Managed: true}},
		"d3": {{Package: "com.whatsapp", Name: "WhatsApp"}},
	} {
		if err := a.ReplaceDeviceApps(ctx, id, rows, at); err != nil {
			t.Fatal(err)
		}
	}
	return a, m
}

func TestAppsSeedAndMigration(t *testing.T) {
	d := testDB(t)
	a := NewApps(d)
	cats, err := a.Categories(ctx0)
	if err != nil || len(cats) != 9 {
		t.Fatalf("categories = %+v, %v", cats, err)
	}
	var mapped int
	if err := d.Get(&mapped, `SELECT COUNT(*) FROM app_category_map`); err != nil || mapped < 60 {
		t.Fatalf("seeded mappings = %d, %v", mapped, err)
	}
	var n int
	if err := d.Get(&n, `SELECT COUNT(*) FROM app_category_map m JOIN app_categories c ON c.id = m.category_id
		WHERE (m.package, c.name) IN (('com.Slack','Messaging'), ('com.google.android.apps.tachyon','Messaging'),
		('com.azure.authenticator','Productivity'), ('com.adobe.reader','Productivity'))`); err != nil || n != 4 {
		t.Fatalf("0010 mappings = %d, %v", n, err)
	}
}

func TestAppsScopeAndFilter(t *testing.T) {
	a, _ := newApps(t)
	tests := []struct {
		name string
		orgs []string
		f    AppFilter
		want map[string]int
	}{
		{"all", nil, AppFilter{ShowSystem: true}, map[string]int{"com.whatsapp": 3, "com.x.sys": 1, "com.unknown": 1}},
		{"hide system", nil, AppFilter{}, map[string]int{"com.whatsapp": 3, "com.unknown": 1}},
		{"sub-org scope", []string{"a", "a1"}, AppFilter{}, map[string]int{"com.whatsapp": 1, "com.unknown": 1}},
		{"empty scope", []string{}, AppFilter{ShowSystem: true}, map[string]int{}},
		{"search name", nil, AppFilter{Search: "zEd"}, map[string]int{"com.unknown": 1}},
		{"search package", nil, AppFilter{Search: "whats"}, map[string]int{"com.whatsapp": 3}},
		{"category", nil, AppFilter{Category: "Messaging"}, map[string]int{"com.whatsapp": 3}},
		{"other", nil, AppFilter{Category: "Other"}, map[string]int{"com.unknown": 1}},
		{"system", nil, AppFilter{Category: "System", ShowSystem: true}, map[string]int{"com.x.sys": 1}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := a.Apps(ctx0, tt.orgs, tt.f)
			if err != nil {
				t.Fatal(err)
			}
			m := map[string]int{}
			for _, s := range got {
				m[s.Package] = s.DeviceCount
			}
			if !reflect.DeepEqual(m, tt.want) {
				t.Fatalf("got %v, want %v", m, tt.want)
			}
		})
	}
}

func TestAppAndDevices(t *testing.T) {
	a, _ := newApps(t)
	s, err := a.App(ctx0, nil, "com.whatsapp")
	if err != nil || s.Category != "Messaging" || s.CategoryID == nil || s.DeviceCount != 3 {
		t.Fatalf("App = %+v, %v", s, err)
	}
	if _, err := a.App(ctx0, []string{"b"}, "com.unknown"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("out of scope err = %v", err)
	}
	if _, err := a.App(ctx0, []string{}, "com.whatsapp"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("empty scope err = %v", err)
	}
	ds, err := a.AppDevices(ctx0, []string{"b"}, "com.whatsapp")
	if err != nil || len(ds) != 1 || ds[0].DeviceID != "d2" || ds[0].Version != "2" || !ds[0].Managed || ds[0].UserID != "u3" {
		t.Fatalf("AppDevices = %+v, %v", ds, err)
	}
	if ds, _ := a.AppDevices(ctx0, nil, "com.whatsapp"); len(ds) != 3 {
		t.Fatalf("unscoped devices = %d", len(ds))
	}
	if ds, _ := a.AppDevices(ctx0, []string{}, "com.whatsapp"); len(ds) != 0 {
		t.Fatalf("empty scope devices = %d", len(ds))
	}
}

func TestDeviceAppsAndUninstall(t *testing.T) {
	a, _ := newApps(t)
	apps, err := a.DeviceApps(ctx0, "d1")
	if err != nil || len(apps) != 3 {
		t.Fatalf("DeviceApps = %+v, %v", apps, err)
	}
	cat := map[string]string{}
	for _, x := range apps {
		cat[x.Package] = x.Category
	}
	if !reflect.DeepEqual(cat, map[string]string{"com.whatsapp": "Messaging", "com.x.sys": "System", "com.unknown": "Other"}) {
		t.Fatalf("categories = %v", cat)
	}
	if err := a.MarkUninstallRequested(ctx0, "d1", "com.whatsapp", time.Now()); err != nil {
		t.Fatal(err)
	}
	if err := a.MarkUninstallRequested(ctx0, "d1", "nope", time.Now()); !errors.Is(err, ErrNotFound) {
		t.Fatalf("err = %v", err)
	}
	if ds, _ := a.AppDevices(ctx0, []string{"a1"}, "com.whatsapp"); len(ds) != 1 || !ds[0].UninstallRequested {
		t.Fatalf("flag not set: %+v", ds)
	}
	if err := a.ReplaceDeviceApps(ctx0, "d1", []DeviceAppRow{{Package: "com.whatsapp", Name: "WhatsApp"}}, time.Now()); err != nil {
		t.Fatal(err)
	}
	apps, _ = a.DeviceApps(ctx0, "d1")
	if len(apps) != 1 || apps[0].UninstallRequestedAt != nil {
		t.Fatalf("replace must clear the flag and drop gone apps: %+v", apps)
	}
}

func TestCategoriesOtherCountsUnmapped(t *testing.T) {
	a, _ := newApps(t)
	cats, _ := a.Categories(ctx0)
	for _, c := range cats {
		// com.unknown is the only unmapped non-system package; com.x.sys is system.
		if c.Name == "Other" && c.AppCount != 1 {
			t.Fatalf("Other count = %+v", c)
		}
	}
}

func TestCategories(t *testing.T) {
	a, _ := newApps(t)
	id, err := a.CreateCategory(ctx0, " Games ")
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"Games", "Browsers"} {
		if _, err := a.CreateCategory(ctx0, name); !errors.Is(err, ErrDuplicate) {
			t.Fatalf("create %q err = %v", name, err)
		}
	}
	for _, name := range []string{"", "  ", "System", "system"} {
		if _, err := a.CreateCategory(ctx0, name); !errors.Is(err, ErrInvalidCategoryName) {
			t.Fatalf("create %q err = %v", name, err)
		}
	}
	if err := a.RenameCategory(ctx0, id, "System"); !errors.Is(err, ErrInvalidCategoryName) {
		t.Fatalf("rename to System err = %v", err)
	}
	if err := a.RenameCategory(ctx0, id, "Browsers"); !errors.Is(err, ErrDuplicate) {
		t.Fatalf("rename dup err = %v", err)
	}
	if err := a.RenameCategory(ctx0, id, "Play"); err != nil {
		t.Fatal(err)
	}
	if err := a.RenameCategory(ctx0, 9999, "X"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("rename missing err = %v", err)
	}

	prev, err := a.SetAppCategory(ctx0, "com.unknown", &id)
	if err != nil || prev != nil {
		t.Fatalf("first set = %v, %v", prev, err)
	}
	if s, _ := a.App(ctx0, nil, "com.unknown"); s.Category != "Play" {
		t.Fatalf("category = %q", s.Category)
	}
	other := int64(9999)
	if _, err := a.SetAppCategory(ctx0, "com.unknown", &other); !errors.Is(err, ErrNotFound) {
		t.Fatalf("unknown category err = %v", err)
	}
	cats, _ := a.Categories(ctx0)
	for _, c := range cats {
		if c.ID == id && c.AppCount != 1 {
			t.Fatalf("app count = %+v", c)
		}
	}
	if prev, err := a.SetAppCategory(ctx0, "com.unknown", nil); err != nil || prev == nil || *prev != id {
		t.Fatalf("clear = %v, %v", prev, err)
	}
	if _, err := a.SetAppCategory(ctx0, "com.unknown", &id); err != nil {
		t.Fatal(err)
	}
	if err := a.DeleteCategory(ctx0, id); err != nil {
		t.Fatal(err)
	}
	if s, _ := a.App(ctx0, nil, "com.unknown"); s.Category != "Other" || s.CategoryID != nil {
		t.Fatalf("mapping must go with its category: %+v", s)
	}
	if err := a.DeleteCategory(ctx0, id); !errors.Is(err, ErrNotFound) {
		t.Fatalf("delete twice err = %v", err)
	}
}

func TestInventoryState(t *testing.T) {
	a, _ := newApps(t)
	s, err := a.Inventory(ctx0)
	if err != nil || s.LastPullAt != nil || s.LastReportRequestAt != nil {
		t.Fatalf("initial = %+v, %v", s, err)
	}
	at := time.Date(2026, 10, 1, 12, 0, 0, 0, time.UTC)
	if err := a.SetLastPull(ctx0, at); err != nil {
		t.Fatal(err)
	}
	if err := a.SetLastReportRequest(ctx0, at.Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	s, err = a.Inventory(ctx0)
	if err != nil || !s.LastPullAt.Equal(at) || !s.LastReportRequestAt.Equal(at.Add(time.Hour)) {
		t.Fatalf("state = %+v, %v", s, err)
	}
}

func TestReplaceAllPrunesDeviceApps(t *testing.T) {
	a, m := newApps(t)
	snap := mirrorSnapshot()
	snap.Devices = snap.Devices[:1] // d1 only
	if err := m.ReplaceAll(ctx0, snap, time.Now()); err != nil {
		t.Fatal(err)
	}
	if s, err := a.App(ctx0, nil, "com.whatsapp"); err != nil || s.DeviceCount != 1 {
		t.Fatalf("App = %+v, %v", s, err)
	}
}

// Categories and the Apps list must agree: both count distinct installed non-system packages per
// resolved category, whether the package is mapped to the category or falls into Other unmapped.
func TestCategoryCountsMatchAppsList(t *testing.T) {
	tests := []struct {
		name  string
		apps  map[string][]DeviceAppRow
		other int
	}{
		{"unmapped only", map[string][]DeviceAppRow{"d1": {{Package: "p.a", Name: "A"}, {Package: "p.b", Name: "B"}}}, 2},
		{"mapped to Other counts once", map[string][]DeviceAppRow{"d1": {{Package: "p.m", Name: "M"}, {Package: "p.a", Name: "A"}}, "d2": {{Package: "p.m", Name: "M"}}}, 2},
		{"system excluded even when mapped", map[string][]DeviceAppRow{"d1": {{Package: "p.s", Name: "S", System: true}, {Package: "p.a", Name: "A"}}}, 1},
		{"mapped but not installed", map[string][]DeviceAppRow{"d1": {{Package: "p.a", Name: "A"}}}, 1},
		{"device unknown to the mirror", map[string][]DeviceAppRow{"d1": {{Package: "p.a", Name: "A"}}, "ghost": {{Package: "p.g", Name: "G"}}}, 1},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			a, m := newApps(t)
			for _, id := range []string{"d1", "d2", "d3"} {
				if err := a.ReplaceDeviceApps(ctx0, id, tt.apps[id], time.Now()); err != nil {
					t.Fatal(err)
				}
			}
			if err := a.ReplaceDeviceApps(ctx0, "ghost", tt.apps["ghost"], time.Now()); err != nil {
				t.Fatal(err)
			}
			var other int64
			if err := m.db.Get(&other, `SELECT id FROM app_categories WHERE name = 'Other'`); err != nil {
				t.Fatal(err)
			}
			for _, p := range []string{"p.m", "p.s", "p.x"} {
				if _, err := a.SetAppCategory(ctx0, p, &other); err != nil {
					t.Fatal(err)
				}
			}
			cats, err := a.Categories(ctx0)
			if err != nil {
				t.Fatal(err)
			}
			list, err := a.Apps(ctx0, nil, AppFilter{Category: "Other"})
			if err != nil {
				t.Fatal(err)
			}
			for _, c := range cats {
				if c.Name == "Other" && (c.AppCount != tt.other || c.AppCount != len(list)) {
					t.Fatalf("Other count = %d, list = %d, want %d", c.AppCount, len(list), tt.other)
				}
			}
		})
	}
}

func TestOtherIsBuiltIn(t *testing.T) {
	a := NewApps(testDB(t))
	cats, _ := a.Categories(ctx0)
	var other, social int64
	for _, c := range cats {
		if c.IsBuiltIn != (c.Name == "Other") {
			t.Fatalf("IsBuiltIn wrong for %+v", c)
		}
		switch c.Name {
		case "Other":
			other = c.ID
		case "Social media":
			social = c.ID
		}
	}
	for name, err := range map[string]error{
		"rename Other": a.RenameCategory(ctx0, other, "Misc"),
		"delete Other": a.DeleteCategory(ctx0, other),
	} {
		if !errors.Is(err, ErrBuiltInCategory) {
			t.Errorf("%s: %v", name, err)
		}
	}
	if err := a.RenameCategory(ctx0, social, "other"); !errors.Is(err, ErrInvalidCategoryName) {
		t.Errorf("rename to Other: %v", err)
	}
	if err := a.RenameCategory(ctx0, 9999, "Zzz"); !errors.Is(err, ErrNotFound) {
		t.Errorf("rename unknown: %v", err)
	}
}

func TestMandatoryAndSummaryFlags(t *testing.T) {
	a, m := newApps(t)
	if err := a.ReplaceDeviceApps(ctx0, "d2", []DeviceAppRow{{Package: "com.whatsapp", Name: "WhatsApp", Mandatory: true}}, time.Now()); err != nil {
		t.Fatal(err)
	}
	_ = m
	s, err := a.App(ctx0, nil, "com.whatsapp")
	if err != nil || !s.Mandatory || s.Managed {
		t.Fatalf("summary = %+v, %v", s, err)
	}
	if s, _ = a.App(ctx0, []string{"a", "a1"}, "com.whatsapp"); s.Mandatory {
		t.Fatalf("out-of-scope row leaked into flags: %+v", s)
	}
	devs, _ := a.AppDevices(ctx0, nil, "com.whatsapp")
	var got int
	for _, d := range devs {
		if d.Mandatory {
			got++
		}
	}
	if got != 1 {
		t.Fatalf("mandatory devices = %d", got)
	}
}

func TestAppNameMajorityAndAlternates(t *testing.T) {
	tests := []struct {
		name     string
		names    []string // one device each
		wantName string
		wantAlts []string
	}{
		{"single", []string{"Gallery"}, "Gallery", []string{}},
		{"majority wins", []string{"Galería", "Gallery", "Gallery"}, "Gallery", []string{"Galería"}},
		{"tie goes alphabetical", []string{"Galería", "Gallery"}, "Galería", []string{"Gallery"}},
		{"alternates by count then alpha", []string{"A", "B", "B", "C", "C", "C", "D"}, "C", []string{"B", "A", "D"}},
		{"empty names ignored", []string{"", "Gallery", ""}, "Gallery", []string{}},
		{"all empty", []string{""}, "", []string{}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			a, _ := newApps(t)
			for i, n := range tt.names {
				id := fmt.Sprintf("dn%d", i)
				if _, err := a.db.Exec(a.db.Rebind(`INSERT INTO knox_devices (device_id, synced_at) VALUES (?, ?)`), id, formatTime(time.Now())); err != nil {
					t.Fatal(err)
				}
				if err := a.ReplaceDeviceApps(ctx0, id, []DeviceAppRow{{Package: "com.gal", Name: n}}, time.Now()); err != nil {
					t.Fatal(err)
				}
			}
			s, err := a.App(ctx0, nil, "com.gal")
			if err != nil {
				t.Fatal(err)
			}
			if s.Name != tt.wantName || !reflect.DeepEqual(s.AlternateNames, tt.wantAlts) {
				t.Errorf("name %q alts %v, want %q %v", s.Name, s.AlternateNames, tt.wantName, tt.wantAlts)
			}
		})
	}
}
