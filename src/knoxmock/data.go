package knoxmock

import (
	"fmt"
	"hash/fnv"
	"sort"
	"strconv"
	"strings"
	"time"
)

type org struct {
	OrgCode    string `json:"orgCode"`
	OrgName    string `json:"orgName"`
	ParentCode string `json:"parentCode"`
	Depth      string `json:"depth"`
	ChildFlag  string `json:"childFlag"`
	OrgType    string `json:"orgType"`
	TenantID   string `json:"tenantId"`
	ValidOrg   string `json:"validOrg"`
	Created    string `json:"created"`
	Updated    string `json:"updated"`
}

type user struct {
	UserID             string      `json:"userId"`
	UserName           string      `json:"userName"`
	Email              string      `json:"email"`
	FirstName          string      `json:"firstName"`
	MiddleName         string      `json:"middleName"`
	LastName           string      `json:"lastName"`
	OrgCode            string      `json:"orgCode"`
	OrgName            string      `json:"orgName"`
	Contact            string      `json:"contact"`
	MobilePhone        string      `json:"mobilePhone"`
	EmpNo              string      `json:"empNo"`
	Enabled            string      `json:"enabled"`
	TenantID           string      `json:"tenantId"`
	LicenseType        string      `json:"licenseType"`
	UserType           string      `json:"userType"`
	RequestEnrollment  bool        `json:"requestEnrollment"`
	AssignedGroupCount int         `json:"assignedGroupCount"`
	DeviceCount        deviceCount `json:"deviceCount"`
	Created            string      `json:"created"`
	Updated            string      `json:"updated"`
}

// deviceCount mirrors the spec's DeviceCount object, limited to the fields the client reads.
type deviceCount struct {
	Total    int `json:"total"`
	Enrolled int `json:"enrolled"`
}

// device is the SimpleDevice/DeviceDetail subset the client reads. OrgCode is only filled for
// selectDeviceInfo, as in the spec.
type device struct {
	DeviceID          string `json:"deviceId"`
	DeviceModel       string `json:"deviceModel"`
	DeviceModelKind   string `json:"deviceModelKind"`
	DeviceStatus      string `json:"deviceStatus"`
	DeviceVersionName string `json:"deviceVersionName"`
	Platform          string `json:"platform"`
	UserID            string `json:"userId,omitempty"`
	UserName          string `json:"userName,omitempty"`
	OrgCode           string `json:"orgCode,omitempty"`
	LastConnection    string `json:"stdFormatLastConnectionDate"`
	lastConnected     time.Time
}

type group struct {
	GroupID      string `json:"groupId"`
	GroupName    string `json:"groupName"`
	GroupType    string `json:"groupType"`
	GroupAdminID string `json:"groupAdminId"`
	MakeType     string `json:"makeType"`
	TargetType   string `json:"targetType"`
	TenantID     string `json:"tenantId"`
	Created      string `json:"created"`
	UserCount    *int   `json:"userCount,omitempty"`
	AppCount     *int   `json:"assignAppCount,omitempty"`
	ProfileCount *int   `json:"assignProfileCount,omitempty"`
	members      map[string]struct{}
	apps         int
	profiles     int
}

func now() string { return time.Now().UTC().Format(timeLayout) }

// Seed identifiers, documented for tests and local use.
const (
	GroupPilot = "0a1b2c3d4e5f60718293a4b5c6d7e8f1"
	GroupField = "0a1b2c3d4e5f60718293a4b5c6d7e8f2"
)

func (s *Server) seed() {
	for _, o := range [][3]string{
		{"ROOT", "Root", ""}, {"SALES", "Sales", "ROOT"}, {"SUPPORT", "Support", "ROOT"}, {"SALES_EMEA", "Sales EMEA", "SALES"},
	} {
		s.orgs[o[0]] = &org{OrgCode: o[0], OrgName: o[1], ParentCode: o[2], OrgType: "0", TenantID: tenantID, ValidOrg: "1", Created: now(), Updated: now()}
	}
	s.refreshOrgShape()

	// devices/enrolled match the seeded devices below (frank's one device is not enrolled);
	// erin has no email and no devices.
	for _, u := range []struct {
		id, first, last, org string
		devices, enrolled    int
	}{
		{"alice", "Alice", "Adams", "ROOT", 2, 2}, {"bob", "Bob", "Brown", "SALES", 1, 1}, {"carol", "Carol", "Clark", "SALES", 1, 1},
		{"dave", "Dave", "Davis", "SALES_EMEA", 0, 0}, {"erin", "Erin", "Evans", "SUPPORT", 0, 0}, {"frank", "Frank", "Fox", "SUPPORT", 1, 0},
	} {
		email := u.id + "@example.org"
		if u.id == "erin" {
			email = ""
		}
		s.users[u.id] = &user{
			UserID: u.id, UserName: u.first + " " + u.last, FirstName: u.first, LastName: u.last, Email: email,
			DeviceCount: deviceCount{Total: u.devices, Enrolled: u.enrolled},
			OrgCode:     u.org, Enabled: "1", TenantID: tenantID, LicenseType: "KM", UserType: "0", Created: now(), Updated: now(),
		}
	}
	s.seedDevices()
	for _, g := range []struct {
		id, name string
		members  []string
		apps     int
		profiles int
	}{
		{GroupPilot, "Pilot Users", []string{"alice", "bob", "dave"}, 5, 3},
		{GroupField, "Field Staff", []string{"carol", "erin"}, 2, 1},
	} {
		grp := s.newGroup(g.id, g.name)
		grp.apps, grp.profiles = g.apps, g.profiles
		for _, m := range g.members {
			grp.members[m] = struct{}{}
		}
	}
}

func (s *Server) newGroup(id, name string) *group {
	g := &group{GroupID: id, GroupName: name, GroupType: "USER", GroupAdminID: "mockadmin", MakeType: "0", TargetType: "0",
		TenantID: tenantID, Created: now(), members: map[string]struct{}{}}
	s.groups[id] = g
	return g
}

// refreshOrgShape recomputes depth and childFlag; orgs are only seeded, never mutated by the API.
func (s *Server) refreshOrgShape() {
	for _, o := range s.orgs {
		d := 1
		for p := s.orgs[o.ParentCode]; p != nil; p = s.orgs[p.ParentCode] {
			d++
		}
		o.Depth = strconv.Itoa(d)
		o.ChildFlag = "0"
		for _, c := range s.orgs {
			if c.ParentCode == o.OrgCode {
				o.ChildFlag = "1"
			}
		}
	}
}

// within reports whether code is root or a descendant of root.
func (s *Server) within(code, root string) bool {
	for o := s.orgs[code]; o != nil; o = s.orgs[o.ParentCode] {
		if o.OrgCode == root {
			return true
		}
	}
	return false
}

// userView returns a copy with derived fields filled in. Caller holds s.mu.
func (s *Server) userView(u *user) user {
	v := *u
	if o := s.orgs[u.OrgCode]; o != nil {
		v.OrgName = o.OrgName
	}
	for _, g := range s.groups {
		if _, in := g.members[u.UserID]; in {
			v.AssignedGroupCount++
		}
	}
	return v
}

func (s *Server) userViews(match func(*user) bool) []user {
	out := []user{}
	for _, u := range s.users {
		if match(u) {
			out = append(out, s.userView(u))
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].UserID < out[j].UserID })
	return out
}

func containsFold(s, sub string) bool {
	return strings.Contains(strings.ToLower(s), strings.ToLower(sub))
}

// seedDevices spans every freshness band relative to now: under 24h, 2-6 days, over 7 days and
// never connected, plus an unassigned device and one that is not enrolled.
func (s *Server) seedDevices() {
	ago := func(d time.Duration) time.Time { return time.Now().UTC().Add(-d) }
	const hour = time.Hour
	for _, d := range []struct {
		id, model, status, user string
		seen                    time.Time
	}{
		{"dev-alice-phone", "Galaxy S23", "A", "alice", ago(2 * hour)},
		{"dev-alice-tab", "Galaxy Tab S9", "A", "alice", ago(3 * 24 * hour)},
		{"dev-bob-phone", "Galaxy A54", "A", "bob", time.Time{}},
		{"dev-carol-phone", "Galaxy XCover6 Pro", "A", "carol", ago(10 * 24 * hour)},
		{"dev-frank-phone", "Galaxy S21", "P", "frank", ago(5 * 24 * hour)},
		{"dev-spare", "Galaxy A14", "A", "", ago(6 * 24 * hour)},
	} {
		dev := &device{DeviceID: d.id, DeviceModel: d.model, DeviceModelKind: d.model, DeviceStatus: d.status,
			DeviceVersionName: "14", Platform: "A", UserID: d.user, lastConnected: d.seen}
		if u := s.users[d.user]; u != nil {
			dev.UserName = u.UserName
		}
		s.devices[d.id] = dev
	}
}

// deviceView returns a copy with the connection time rendered and, when withOrg, the owner's org
// filled in. Caller holds s.mu.
func (s *Server) deviceView(d *device, withOrg bool) device {
	v := *d
	if !d.lastConnected.IsZero() {
		v.LastConnection = d.lastConnected.Format(time.RFC3339)
	}
	if u := s.users[d.UserID]; withOrg && u != nil {
		v.OrgCode = u.OrgCode
	}
	return v
}

func (s *Server) deviceViews(match func(*device) bool) []device {
	out := []device{}
	for _, d := range s.devices {
		if match(d) {
			out = append(out, s.deviceView(d, false))
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].DeviceID < out[j].DeviceID })
	return out
}

// locationDays is how far back the generated location history reaches; locationEvery is the spacing.
const (
	locationDays  = 30
	locationEvery = 8 * time.Hour
)

// dateScheme renders t as the spec's legacy java.util.Date-style object.
func dateScheme(t time.Time) map[string]any {
	t = t.UTC()
	return map[string]any{
		"date": t.Day(), "day": int(t.Weekday()), "hour": t.Hour(), "minutes": t.Minute(), "month": int(t.Month()) - 1,
		"seconds": t.Second(), "time": t.UnixMilli(), "timezoneOffset": 0, "year": t.Year() - 1900,
	}
}

func stdTime(t time.Time) string { return t.UTC().Format("2006-01-02T15:04:05Z") }

// seedHash gives each device stable pseudo-random attributes without storing them.
func seedHash(id, salt string) uint32 {
	h := fnv.New32a()
	h.Write([]byte(id + "/" + salt))
	return h.Sum32()
}

func digits(id, salt string, n int) string {
	h := seedHash(id, salt)
	b := make([]byte, n)
	for i := range b {
		h = h*1664525 + 1013904223
		b[i] = '0' + byte(h>>24)%10
	}
	return string(b)
}

// enrolled mirrors the seed convention that provisioned ("P") devices are not yet enrolled.
func (d *device) enrolled() bool { return d.DeviceStatus != "P" }

// hasLocation: devices that never connected or have no owner have never reported a position.
func (d *device) hasLocation() bool {
	return d.enrolled() && d.UserID != "" && !d.lastConnected.IsZero()
}

var carriers = [][3]string{{"Vodacom", "655", "01"}, {"MTN", "655", "10"}, {"Cell C", "655", "07"}}

// deviceDetail returns the spec's DeviceDetail as a map (DateScheme fields are nested objects).
// Caller holds s.mu.
func (s *Server) deviceDetail(d *device) map[string]any {
	v := s.deviceView(d, true)
	id := d.DeviceID
	created := time.Now().UTC().Add(-time.Duration(30+seedHash(id, "age")%300) * 24 * time.Hour)
	carrier := carriers[seedHash(id, "carrier")%uint32(len(carriers))]
	rooted := "N"
	if seedHash(id, "root")%10 == 0 {
		rooted = "Y"
	}
	out := map[string]any{
		"deviceId": id, "deviceModel": d.DeviceModel, "deviceModelKind": d.DeviceModelKind, "deviceStatus": d.DeviceStatus,
		"deviceVersionName": d.DeviceVersionName, "platform": d.Platform, "deviceKind": "0",
		"battery": strconv.Itoa(10 + int(seedHash(id, "bat")%90)), "imei": "35" + digits(id, "imei", 13),
		"serialNumber": "R" + strings.ToUpper(strconv.FormatUint(uint64(seedHash(id, "sn")), 36)) + "X",
		"mobileId":     id, "macAddress": "02:00:" + strings.ToUpper(hexPairs(seedHash(id, "mac"))),
		"phone": "+2782" + digits(id, "phone", 7), "iccid": "8927" + digits(id, "iccid", 15),
		"currentMncName": carrier[0], "currentMcc": carrier[1], "subscriberMncName": carrier[0], "subscriberMcc": carrier[1],
		"simStatus": "5", "isRoaming": "N", "isRooting": rooted, "appIsRooting": rooted, "isContainMalware": "N", "isDeviceLock": "N",
		"lostMode": "Off", "enrolledType": "KME", "managementType": "DO", "ownerShip": "EC",
		"agentProfileName": "Knox Manage Agent 25.07.1", "clientProfileName": "Knox Manage Client 25.07.0",
		"assignedProfileList":     []map[string]string{{"profileId": "prof-base", "profileName": "Base Policy", "templateFlag": "N"}, {"profileId": "prof-sec", "profileName": "Security Baseline", "templateFlag": "N"}},
		"assignedProfileNameList": []string{"Base Policy", "Security Baseline"},
		"created":                 dateScheme(created), "stdFormatCreated": stdTime(created),
		"updated": dateScheme(created), "stdFormatUpdated": stdTime(created),
		"stdFormatLastConnectionDate": v.LastConnection,
	}
	if !d.lastConnected.IsZero() {
		out["lastConnectionDate"] = dateScheme(d.lastConnected)
		out["stdFormatLastConnectionDate"] = stdTime(d.lastConnected)
	}
	if v.UserID != "" {
		out["userId"], out["userName"], out["orgCode"] = v.UserID, v.UserName, v.OrgCode
		out["email"] = s.users[v.UserID].Email
		out["orgName"] = s.orgs[v.OrgCode].OrgName
	}
	return out
}

func hexPairs(h uint32) string {
	return fmt.Sprintf("%02x:%02x:%02x:%02x", byte(h>>24), byte(h>>16), byte(h>>8), byte(h))
}

var appCatalog = [][2]string{
	{"Knox Manage Agent", "com.samsung.android.knox.kmagent"}, {"Chrome", "com.android.chrome"}, {"Gmail", "com.google.android.gm"},
	{"Microsoft Teams", "com.microsoft.teams"}, {"Outlook", "com.microsoft.office.outlook"}, {"Google Maps", "com.google.android.apps.maps"},
	{"Samsung Internet", "com.sec.android.app.sbrowser"}, {"Calendar", "com.samsung.android.calendar"}, {"WhatsApp", "com.whatsapp"},
	{"Zoom", "us.zoom.videomeetings"}, {"Adobe Acrobat", "com.adobe.reader"}, {"Samsung Notes", "com.samsung.android.app.notes"},
	{"Google Drive", "com.google.android.apps.docs"}, {"Authenticator", "com.azure.authenticator"}, {"Camera", "com.sec.android.app.camera"},
}

// deviceApps returns DeviceAppInfo entries: 8-15 apps for enrolled devices, none otherwise.
func deviceApps(d *device) []map[string]any {
	out := []map[string]any{}
	if !d.enrolled() {
		return out
	}
	n := 8 + int(seedHash(d.DeviceID, "apps")%8)
	off := int(seedHash(d.DeviceID, "appoff") % uint32(len(appCatalog)))
	for i := 0; i < n; i++ {
		a := appCatalog[(off+i)%len(appCatalog)]
		h := seedHash(d.DeviceID, a[1])
		installed := time.Now().UTC().Add(-time.Duration(1+h%200) * 24 * time.Hour)
		out = append(out, map[string]any{
			"deviceId": d.DeviceID, "appName": a[0], "packageName": a[1],
			"versionName": fmt.Sprintf("%d.%d.%d", 1+h%20, h>>8%10, h>>16%100), "versionCode": strconv.Itoa(int(100000 + h%900000)),
			"installed": installed.UnixMilli(), "stdFormatInstalled": stdTime(installed),
			"platform": d.Platform, "systemApp": map[bool]string{true: "Y", false: "N"}[i%5 == 0], "enabled": "Y", "isRooting": "N",
		})
	}
	return out
}

// locationHistory returns points newest first, locationEvery apart, for the last `days` days.
// Positions wander around a per-device base near Johannesburg.
func locationHistory(d *device, at time.Time, days int) []map[string]any {
	out := []map[string]any{}
	if !d.hasLocation() {
		return out
	}
	lat := -26.2041 + float64(seedHash(d.DeviceID, "lat")%1000)/10000
	lon := 28.0473 + float64(seedHash(d.DeviceID, "lon")%1000)/10000
	for i := 0; time.Duration(i)*locationEvery <= time.Duration(days)*24*time.Hour; i++ {
		t := at.Add(-time.Duration(i) * locationEvery)
		h := seedHash(d.DeviceID, "pt"+strconv.Itoa(i))
		out = append(out, map[string]any{
			"latitude":            strconv.FormatFloat(lat+float64(h%200)/20000, 'f', 6, 64),
			"longitude":           strconv.FormatFloat(lon+float64(h>>10%200)/20000, 'f', 6, 64),
			"altitude":            strconv.Itoa(1400 + int(h>>20%60)),
			"locationInfoUpdated": dateScheme(t), "stdFormatUpdated": stdTime(t),
		})
	}
	return out
}
