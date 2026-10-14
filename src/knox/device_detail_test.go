package knox

import (
	"context"
	"net/http"
	"net/url"
	"testing"
	"time"
)

func TestDeviceDetailCalls(t *testing.T) {
	f, c := newFake(t)
	ctx := context.Background()
	var path string
	var form url.Values
	f.api = func(w http.ResponseWriter, r *http.Request, v url.Values) {
		path, form = r.URL.Path, v
		switch r.URL.Path {
		case "/emm/oapi/device/selectDeviceInfo":
			ok(w, `{"deviceModel":"SM-G","imei":"123","isRooting":"N","assignedProfileNameList":["p1"],"stdFormatCreated":"2024-12-03T22:30:01Z"}`)
		case "/emm/oapi/device/selectDeviceAppList":
			ok(w, `{"devicelist":[{"appName":"Mail","packageName":"com.mail","versionName":"1.2"}],"total":"1"}`)
		case "/emm/oapi/device/selectDeviceLocation":
			ok(w, `{"latitude":"39.99","longitude":"116.3","altitude":"null","stdFormatUpdated":"2024-05-19T21:41:33Z"}`)
		default:
			ok(w, `{"locationList":[{"latitude":"1","longitude":"2"}],"total":"1"}`)
		}
	}

	d, err := c.SelectDeviceDetail(ctx, "dev1")
	if err != nil || d.IMEI != "123" || d.IsRooting != "N" || len(d.Profiles) != 1 || d.Created != "2024-12-03T22:30:01Z" || form.Get("deviceId") != "dev1" {
		t.Fatalf("detail = %+v, %v, form %v", d, err, form)
	}
	apps, diag, err := c.SelectDeviceAppList(ctx, "dev1")
	if err != nil || diag != nil || len(apps) != 1 || apps[0].PackageName != "com.mail" {
		t.Fatalf("apps = %+v, %v", apps, err)
	}
	loc, err := c.SelectDeviceLocation(ctx, "dev1")
	if err != nil || loc.Latitude != "39.99" || loc.Altitude != "null" {
		t.Fatalf("location = %+v, %v", loc, err)
	}
	now := time.Now()
	hist, err := c.SelectDeviceLocationHistory(ctx, "dev1", now.Add(-25*time.Hour), now)
	if err != nil || len(hist) != 1 || path != "/emm/oapi/device/selectDeviceLocationHistory" || form.Get("period") != "2" {
		t.Fatalf("history = %+v, %v, path %s, form %v", hist, err, path, form)
	}
}

func TestDeviceDetailRejectsBadInput(t *testing.T) {
	_, c := newFake(t)
	ctx := context.Background()
	now := time.Now()
	if _, err := c.SelectDeviceDetail(ctx, ""); err == nil {
		t.Error("detail: empty id accepted")
	}
	if _, _, err := c.SelectDeviceAppList(ctx, ""); err == nil {
		t.Error("apps: empty id accepted")
	}
	if _, err := c.SelectDeviceLocation(ctx, ""); err == nil {
		t.Error("location: empty id accepted")
	}
	if _, err := c.SelectDeviceLocationHistory(ctx, "d", now, now.Add(-time.Hour)); err == nil {
		t.Error("history: inverted window accepted")
	}
}
