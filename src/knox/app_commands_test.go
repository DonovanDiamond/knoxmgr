package knox

import (
	"context"
	"net/http"
	"net/url"
	"reflect"
	"strconv"
	"strings"
	"testing"
)

func TestValidPackage(t *testing.T) {
	tests := []struct {
		in   string
		want bool
	}{
		{"com.android.settings", true},
		{"com.samsung.android.knox.kmagent", true},
		{"a.b_c", true},
		{"", false},
		{"nodots", false},
		{"1com.app", false},
		{"com..app", false},
		{"com.app;rm", false},
		{"com.app ", false},
		{"com." + strings.Repeat("a", 252), false},
	}
	for _, tt := range tests {
		if got := ValidPackage(tt.in); got != tt.want {
			t.Errorf("ValidPackage(%q) = %v, want %v", tt.in, got, tt.want)
		}
	}
}

func TestAppFlags(t *testing.T) {
	tests := []struct {
		in   string
		want bool
	}{{"Y", true}, {"y", true}, {"true", true}, {"1", true}, {"N", false}, {"false", false}, {"0", false}, {"", false}, {"null", false}}
	for _, tt := range tests {
		a := DeviceApp{SystemApp: tt.in, IsManaged: tt.in, MandatoryApp: tt.in}
		if a.System() != tt.want || a.Managed() != tt.want || a.Mandatory() != tt.want {
			t.Errorf("flag(%q) != %v", tt.in, tt.want)
		}
	}
}

func TestSelectDeviceAppListPages(t *testing.T) {
	f, c := newFake(t)
	var starts []string
	f.api = func(w http.ResponseWriter, r *http.Request, v url.Values) {
		starts = append(starts, v.Get("start"))
		n := 1000
		if v.Get("start") == "1000" {
			n = 5
		}
		ok(w, `{"devicelist":[`+strings.TrimSuffix(strings.Repeat(`{"packageName":"com.a","systemApp":"Y"},`, n), ",")+`],"total":"1005"}`)
	}
	apps, _, err := c.SelectDeviceAppList(context.Background(), "dev1")
	if err != nil || len(apps) != 1005 || strings.Join(starts, ",") != "0,1000" || !apps[0].System() {
		t.Fatalf("got %d apps, starts %v, err %v", len(apps), starts, err)
	}

	// Full pages forever must stop at the page bound.
	starts = nil
	f.api = func(w http.ResponseWriter, r *http.Request, v url.Values) {
		starts = append(starts, v.Get("start"))
		ok(w, `{"devicelist":[`+strings.TrimSuffix(strings.Repeat(`{"packageName":"com.a"},`, 1000), ",")+`]}`)
	}
	if _, _, err := c.SelectDeviceAppList(context.Background(), "dev1"); err != nil || len(starts) != maxAppPages {
		t.Fatalf("pages = %d (%s), err %v", len(starts), strconv.Itoa(maxAppPages), err)
	}
}

func TestAppCommands(t *testing.T) {
	f, c := newFake(t)
	ctx := context.Background()
	var path string
	var form url.Values
	f.api = func(w http.ResponseWriter, r *http.Request, v url.Values) {
		path, form = r.URL.Path, v
		ok(w, `{}`)
	}
	if err := c.UninstallApp(ctx, "dev1", "com.whatsapp", "kid"); err != nil ||
		path != "/emm/oapi/mdm/commonOTCServiceWrapper/sendDeviceControlForUninstallApp" ||
		form.Get("deviceId") != "dev1" || form.Get("appPackage") != "com.whatsapp" || form.Get("knoxId") != "kid" {
		t.Fatalf("uninstall: %v, %s, %v", err, path, form)
	}
	if err := c.UninstallApp(ctx, "dev1", "com.whatsapp", ""); err != nil || form.Has("knoxId") {
		t.Fatalf("uninstall without knoxId: %v, %v", err, form)
	}
	if err := c.SyncInstalledAppList(ctx, "dev1"); err != nil ||
		path != "/emm/oapi/mdm/commonOTCServiceWrapper/sendDeviceControlForSyncInstalledAppList" || form.Get("deviceId") != "dev1" {
		t.Fatalf("sync: %v, %s, %v", err, path, form)
	}

	path = ""
	for _, tt := range []struct{ id, pkg string }{{"", "com.a"}, {"dev1", ""}, {"dev1", "bad pkg"}, {"dev1", "nodots"}} {
		if err := c.UninstallApp(ctx, tt.id, tt.pkg, ""); err == nil {
			t.Errorf("uninstall(%q, %q) accepted", tt.id, tt.pkg)
		}
	}
	if err := c.SyncInstalledAppList(ctx, ""); err == nil {
		t.Error("sync: empty id accepted")
	}
	if path != "" {
		t.Errorf("invalid input reached Knox: %s", path)
	}
}

func TestSelectDeviceAppListDiagnostic(t *testing.T) {
	tests := []struct {
		name, value string
		want        *AppListDiagnostic
	}{
		{"normal list", `{"devicelist":[{"packageName":"com.a"}],"total":"1"}`, nil},
		{"alternate key", `{"devicelist":[],"appList":[{"packageName":"com.secret.name"}],"total":"0"}`,
			&AppListDiagnostic{ResultType: "object", Keys: map[string]string{"devicelist": "array", "appList": "array", "total": "string"}, Total: "0", AppKeys: []string{"appList"}}},
		{"no list numeric total", `{"total":3,"note":"x"}`,
			&AppListDiagnostic{ResultType: "object", Keys: map[string]string{"total": "number", "note": "string"}, Total: "3"}},
		{"not an object", `"nothing"`, &AppListDiagnostic{ResultType: "string", Keys: map[string]string{}}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f, c := newFake(t)
			f.api = func(w http.ResponseWriter, r *http.Request, v url.Values) { ok(w, tt.value) }
			_, got, err := c.SelectDeviceAppList(context.Background(), "dev1")
			if err != nil || !reflect.DeepEqual(got, tt.want) {
				t.Fatalf("diag = %+v, %v; want %+v", got, err, tt.want)
			}
		})
	}
}
