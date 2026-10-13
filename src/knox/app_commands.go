package knox

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"regexp"
)

var packagePattern = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9_]*(\.[A-Za-z][A-Za-z0-9_]*)+$`)

// ValidPackage reports whether s is a well-formed Android package name.
func ValidPackage(s string) bool { return len(s) <= 255 && packagePattern.MatchString(s) }

// UninstallApp asks the device to uninstall an Android app (POST
// /mdm/commonOTCServiceWrapper/sendDeviceControlForUninstallApp). knoxID may be empty.
func (c *Client) UninstallApp(ctx context.Context, deviceID, pkg, knoxID string) error {
	if err := requireDeviceID(deviceID); err != nil {
		return err
	}
	if !ValidPackage(pkg) {
		return errors.New("knox: invalid package name")
	}
	f := url.Values{"deviceId": {deviceID}, "appPackage": {pkg}}
	set(f, "knoxId", knoxID)
	_, err := call[ignored](ctx, c, http.MethodPost, "/mdm/commonOTCServiceWrapper/sendDeviceControlForUninstallApp", f)
	return err
}

// SyncInstalledAppList asks the device to report its installed apps again (POST
// /mdm/commonOTCServiceWrapper/sendDeviceControlForSyncInstalledAppList).
func (c *Client) SyncInstalledAppList(ctx context.Context, deviceID string) error {
	if err := requireDeviceID(deviceID); err != nil {
		return err
	}
	_, err := call[ignored](ctx, c, http.MethodPost, "/mdm/commonOTCServiceWrapper/sendDeviceControlForSyncInstalledAppList", url.Values{"deviceId": {deviceID}})
	return err
}
