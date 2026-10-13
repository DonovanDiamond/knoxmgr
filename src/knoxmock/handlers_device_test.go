package knoxmock

import (
	"net/url"
	"strconv"
	"strings"
	"testing"
)

func deviceIDs(v map[string]any) string {
	var ids []string
	for _, d := range v["devicelist"].([]any) {
		ids = append(ids, d.(map[string]any)["deviceId"].(string))
	}
	return strings.Join(ids, ",")
}

func TestDeviceReads(t *testing.T) {
	e := newEnv(t)
	all := "dev-alice-phone,dev-alice-tab,dev-bob-phone,dev-carol-phone,dev-frank-phone,dev-spare"
	tests := []struct {
		name, path string
		form       url.Values
		want       string
	}{
		{"all", "selectDeviceList", url.Values{}, all},
		{"paged", "selectDeviceList", url.Values{"start": {"1"}, "limit": {"2"}}, "dev-alice-tab,dev-bob-phone"},
		{"by user filter", "selectDeviceList", url.Values{"userId": {"alice"}}, "dev-alice-phone,dev-alice-tab"},
		{"by status", "selectDeviceList", url.Values{"deviceStatus": {"P"}}, "dev-frank-phone"},
		{"by user", "selectDevicesByUser", url.Values{"userId": {"alice"}}, "dev-alice-phone,dev-alice-tab"},
		{"by user without devices", "selectDevicesByUser", url.Values{"userId": {"dave"}}, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := deviceIDs(e.value("POST", "/emm/oapi/device/"+tt.path, tt.form)); got != tt.want {
				t.Fatalf("got %q, want %q", got, tt.want)
			}
		})
	}
}

func TestDeviceInfoAndSeedShape(t *testing.T) {
	e := newEnv(t)
	info := func(id string) map[string]any {
		return e.value("POST", "/emm/oapi/device/selectDeviceInfo", url.Values{"deviceId": {id}})
	}
	if d := info("dev-carol-phone"); d["orgCode"] != "SALES" || d["userId"] != "carol" || d["stdFormatLastConnectionDate"] == "" {
		t.Errorf("carol device = %v", d)
	}
	if d := info("dev-bob-phone"); d["stdFormatLastConnectionDate"] != "" {
		t.Errorf("never-connected device has a connection date: %v", d)
	}
	if d := info("dev-spare"); d["userId"] != nil || d["orgCode"] != nil {
		t.Errorf("unassigned device has an owner: %v", d)
	}
	if st, _ := e.call("POST", "/emm/oapi/device/selectDeviceInfo", url.Values{"deviceId": {"nope"}}); st != 200 {
		t.Errorf("status %d", st)
	}
	if _, body := e.call("POST", "/emm/oapi/device/selectDeviceInfo", url.Values{"deviceId": {"nope"}}); body["resultCode"] != codeNotFound {
		t.Errorf("unknown device = %v", body)
	}
}

func TestDeviceAppList(t *testing.T) {
	e := newEnv(t)
	call := func(id string, extra url.Values) map[string]any {
		extra.Set("deviceId", id)
		return e.value("POST", "/emm/oapi/device/selectDeviceAppList", extra)
	}
	tests := []struct {
		name, id string
		form     url.Values
		wantLen  int
		minTotal int
		maxTotal int
	}{
		{"all apps", "dev-alice-phone", url.Values{}, -1, 8, 15},
		{"limit", "dev-alice-phone", url.Values{"limit": {"3"}}, 3, 8, 15},
		{"start past end", "dev-alice-phone", url.Values{"start": {"100"}}, 0, 8, 15},
		{"not enrolled has none", "dev-frank-phone", url.Values{}, 0, 0, 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			v := call(tt.id, tt.form)
			list := v["devicelist"].([]any)
			total, _ := strconv.Atoi(v["total"].(string))
			if total < tt.minTotal || total > tt.maxTotal {
				t.Fatalf("total %d outside %d-%d", total, tt.minTotal, tt.maxTotal)
			}
			if tt.wantLen >= 0 && len(list) != tt.wantLen && tt.wantLen != -1 {
				t.Fatalf("len %d, want %d", len(list), tt.wantLen)
			}
			if tt.wantLen == -1 && len(list) != total {
				t.Fatalf("len %d, total %d", len(list), total)
			}
			for _, a := range list {
				m := a.(map[string]any)
				if m["appName"] == "" || m["packageName"] == "" || m["versionName"] == "" {
					t.Fatalf("incomplete app %v", m)
				}
			}
		})
	}
	for _, bad := range []url.Values{{"deviceId": {"nope"}}, {"deviceId": {"dev-spare"}, "limit": {"0"}}, {}} {
		if _, body := e.call("POST", "/emm/oapi/device/selectDeviceAppList", bad); body["resultCode"] == codeOK {
			t.Errorf("%v succeeded: %v", bad, body)
		}
	}
	if _, body := e.call("POST", "/emm/oapi/device/selectDeviceAppList", url.Values{"deviceId": {"nope"}}); body["resultCode"] != codeNotFound {
		t.Errorf("unknown device = %v", body)
	}
}

func TestDeviceInfoDetailFields(t *testing.T) {
	e := newEnv(t)
	d := e.value("POST", "/emm/oapi/device/selectDeviceInfo", url.Values{"deviceId": {"dev-alice-phone"}})
	for _, k := range []string{"deviceModel", "deviceVersionName", "battery", "currentMncName", "imei", "serialNumber", "agentProfileName",
		"clientProfileName", "isRooting", "created", "stdFormatCreated", "lastConnectionDate", "assignedProfileList", "assignedProfileNameList"} {
		if v, found := d[k]; !found || v == "" {
			t.Errorf("missing %s in %v", k, d)
		}
	}
	if len(d["assignedProfileList"].([]any)) != len(d["assignedProfileNameList"].([]any)) {
		t.Errorf("profile lists disagree: %v", d)
	}
}

func TestDeviceLocation(t *testing.T) {
	e := newEnv(t)
	loc := func(id string) (map[string]any, map[string]any) {
		_, body := e.call("POST", "/emm/oapi/device/selectDeviceLocation", url.Values{"deviceId": {id}})
		v, _ := body["resultValue"].(map[string]any)
		return body, v
	}
	body, v := loc("dev-alice-phone")
	if body["resultCode"] != codeOK || v["latitude"] == "" || v["longitude"] == "" || v["stdFormatUpdated"] == "" || v["updated"] == nil || v["locationInfoUpdated"] != nil {
		t.Errorf("location = %v", body)
	}
	for _, id := range []string{"dev-bob-phone", "dev-spare", "dev-frank-phone"} {
		if body, v := loc(id); body["resultCode"] != codeOK || v != nil {
			t.Errorf("%s: want empty success, got %v", id, body)
		}
	}
	if body, _ := loc("nope"); body["resultCode"] != codeNotFound {
		t.Errorf("unknown device = %v", body)
	}
}

func TestDeviceLocationHistory(t *testing.T) {
	e := newEnv(t)
	count := func(id, period string) (int, string) {
		_, body := e.call("POST", "/emm/oapi/device/selectDeviceLocationHistory", url.Values{"deviceId": {id}, "period": {period}})
		if body["resultCode"] != codeOK {
			return -1, body["resultCode"].(string)
		}
		v := body["resultValue"].(map[string]any)
		n := len(v["locationList"].([]any))
		if v["total"] != strconv.Itoa(n) {
			t.Errorf("total %v != %d", v["total"], n)
		}
		return n, ""
	}
	tests := []struct {
		name, id, period string
		wantMin, wantMax int
		wantCode         string
	}{
		{"1 day", "dev-alice-phone", "1", 3, 5, ""},
		{"7 days", "dev-alice-phone", "7", 20, 30, ""},
		{"30 days", "dev-alice-phone", "30", 80, 100, ""},
		{"no location", "dev-bob-phone", "30", 0, 0, ""},
		{"unknown device", "nope", "7", -1, -1, codeNotFound},
		{"zero period", "dev-alice-phone", "0", -1, -1, codeBadParam},
		{"huge period", "dev-alice-phone", "31", -1, -1, codeBadParam},
		{"non-numeric period", "dev-alice-phone", "x", -1, -1, codeBadParam},
	}
	prev := 0
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			n, code := count(tt.id, tt.period)
			if code != tt.wantCode || n < tt.wantMin || n > tt.wantMax {
				t.Fatalf("n=%d code=%q, want %d-%d code %q", n, code, tt.wantMin, tt.wantMax, tt.wantCode)
			}
			if tt.wantCode == "" && tt.id == "dev-alice-phone" {
				if n <= prev {
					t.Errorf("window not wider than previous: %d <= %d", n, prev)
				}
				prev = n
			}
		})
	}
}
