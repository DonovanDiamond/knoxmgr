package knoxmock

import (
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/rs/zerolog"
)

const (
	uninstallPath = "/emm/oapi/mdm/commonOTCServiceWrapper/sendDeviceControlForUninstallApp"
	syncPath      = "/emm/oapi/mdm/commonOTCServiceWrapper/sendDeviceControlForSyncInstalledAppList"
)

func appPackages(e *env, id string) map[string]map[string]any {
	out := map[string]map[string]any{}
	for _, a := range e.value("POST", "/emm/oapi/device/selectDeviceAppList", url.Values{"deviceId": {id}})["appList"].([]any) {
		m := a.(map[string]any)
		out[m["packageName"].(string)] = m
	}
	return out
}

func TestAppInventoryShape(t *testing.T) {
	e := newEnv(t)
	apps := appPackages(e, "dev-alice-phone")
	system := 0
	for pkg, a := range apps {
		if a["systemApp"] == "Y" {
			system++
		}
		if a["systemApp"] != "Y" && a["systemApp"] != "N" || a["stdFormatInstalled"] == "" {
			t.Errorf("%s: bad flags %v", pkg, a)
		}
	}
	if system < 10 || apps[knoxAgentPackage] == nil || apps["com.android.settings"] == nil {
		t.Errorf("system apps = %d, agent/settings present = %v/%v", system, apps[knoxAgentPackage] != nil, apps["com.android.settings"] != nil)
	}
	if len(apps) == len(appPackages(e, "dev-bob-phone")) && len(apps) == len(appPackages(e, "dev-carol-phone")) {
		t.Error("every device has the same app count")
	}
}

func TestUninstallAndSync(t *testing.T) {
	e := newEnv(t)
	apps := appPackages(e, "dev-alice-phone")
	var user, system string
	for pkg, a := range apps {
		if a["systemApp"] == "Y" {
			system = pkg
		} else if pkg != knoxAgentPackage {
			user = pkg
		}
	}
	tests := []struct {
		name string
		path string
		form url.Values
		code string
	}{
		{"sync", syncPath, url.Values{"deviceId": {"dev-alice-phone"}}, codeOK},
		{"sync unknown device", syncPath, url.Values{"deviceId": {"nope"}}, codeNotFound},
		{"sync no device", syncPath, url.Values{}, codeBadParam},
		{"system app rejected", uninstallPath, url.Values{"deviceId": {"dev-alice-phone"}, "appPackage": {system}}, codeInvalidRequest},
		{"no package", uninstallPath, url.Values{"deviceId": {"dev-alice-phone"}}, codeBadParam},
		{"not installed", uninstallPath, url.Values{"deviceId": {"dev-alice-phone"}, "appPackage": {"com.not.installed"}}, codeNotFound},
		{"unknown device", uninstallPath, url.Values{"deviceId": {"nope"}, "appPackage": {user}}, codeNotFound},
		{"user app", uninstallPath, url.Values{"deviceId": {"dev-alice-phone"}, "appPackage": {user}}, codeOK},
		{"already gone", uninstallPath, url.Values{"deviceId": {"dev-alice-phone"}, "appPackage": {user}}, codeNotFound},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if _, body := e.call("POST", tt.path, tt.form); body["resultCode"] != tt.code {
				t.Fatalf("resultCode = %v, want %s: %v", body["resultCode"], tt.code, body)
			}
		})
	}
	after := appPackages(e, "dev-alice-phone")
	if after[user] != nil || len(after) != len(apps)-1 || after[system] == nil {
		t.Errorf("after uninstall: %d apps (was %d), user %v, system %v", len(after), len(apps), after[user] != nil, after[system] != nil)
	}
	if len(appPackages(e, "dev-bob-phone")) == 0 {
		t.Error("other devices lost their apps")
	}
}

func TestAppCommandsRejectNonAndroid(t *testing.T) {
	s := New(testID, testSecret, zerolog.Nop())
	s.devices["dev-spare"].Platform = "I"
	for _, h := range []struct {
		name string
		fn   func(*httptest.ResponseRecorder, *strings.Reader)
	}{
		{"uninstall", func(w *httptest.ResponseRecorder, b *strings.Reader) {
			r := httptest.NewRequest("POST", uninstallPath, b)
			r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
			s.uninstallApp(w, r)
		}},
		{"sync", func(w *httptest.ResponseRecorder, b *strings.Reader) {
			r := httptest.NewRequest("POST", syncPath, b)
			r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
			s.syncInstalledAppList(w, r)
		}},
	} {
		w := httptest.NewRecorder()
		h.fn(w, strings.NewReader("deviceId=dev-spare&appPackage=com.whatsapp"))
		if !strings.Contains(w.Body.String(), `"`+codeNotSupported+`"`) {
			t.Errorf("%s on iOS = %s", h.name, w.Body)
		}
	}
	if len(deviceApps(s.devices["dev-spare"])) != 0 {
		t.Error("iOS device has Android apps")
	}
}

func TestAlternateAppName(t *testing.T) {
	e := newEnv(t)
	names := map[string]int{}
	for _, id := range []string{"dev-alice-phone", "dev-alice-tab", "dev-bob-phone", "dev-carol-phone", "dev-frank-phone"} {
		if a := appPackages(e, id)[altNamePackage]; a != nil {
			names[a["appName"].(string)]++
		}
	}
	if names["Google Photos"] <= names[altName] || names[altName] == 0 {
		t.Errorf("names for %s = %v, want a majority of Google Photos and at least one %s", altNamePackage, names, altName)
	}
}
