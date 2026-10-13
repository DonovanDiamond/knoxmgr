package knox

import (
	"context"
	"errors"
	"math"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// DeviceDetail is the part of the spec's DeviceDetail that knoxmgr shows.
type DeviceDetail struct {
	DeviceModel       string   `json:"deviceModel"`
	DeviceModelKind   string   `json:"deviceModelKind"`
	Platform          string   `json:"platform"`
	DeviceVersionName string   `json:"deviceVersionName"`
	IMEI              string   `json:"imei"`
	SerialNumber      string   `json:"serialNumber"`
	Carrier           string   `json:"currentMncName"`
	Phone             string   `json:"phone"`
	Battery           string   `json:"battery"`
	IsRooting         string   `json:"isRooting"` // Y or N
	AgentProfileName  string   `json:"agentProfileName"`
	ClientProfileName string   `json:"clientProfileName"`
	Profiles          []string `json:"assignedProfileNameList"`
	// Created and LastConnection are the spec's ISO 8601 stdFormat* fields.
	Created        string `json:"stdFormatCreated"`
	LastConnection string `json:"stdFormatLastConnectionDate"`
}

// DeviceApp is one installed application. The spec types systemApp, isManaged and mandatoryApp
// as plain strings without listing values, so read them through System, Managed and Mandatory.
type DeviceApp struct {
	AppName      string `json:"appName"`
	PackageName  string `json:"packageName"`
	VersionName  string `json:"versionName"`
	SystemApp    string `json:"systemApp"`
	IsManaged    string `json:"isManaged"`
	MandatoryApp string `json:"mandatoryApp"`
	Installed    string `json:"stdFormatInstalled"` // UTC ISO 8601
	KnoxID       string `json:"knoxId"`             // empty for the default area
}

// flag reads Knox's loosely typed booleans: Y/N, true/false or 1/0.
func flag(v string) bool {
	switch strings.ToLower(strings.TrimSpace(v)) {
	case "y", "yes", "true", "1":
		return true
	}
	return false
}

func (a DeviceApp) System() bool    { return flag(a.SystemApp) }
func (a DeviceApp) Managed() bool   { return flag(a.IsManaged) }
func (a DeviceApp) Mandatory() bool { return flag(a.MandatoryApp) }

// DeviceLocation is one GPS fix; Knox sends every value as a string.
type DeviceLocation struct {
	Latitude  string `json:"latitude"`
	Longitude string `json:"longitude"`
	Altitude  string `json:"altitude"`
	Updated   string `json:"stdFormatUpdated"` // UTC ISO 8601
}

func requireDeviceID(id string) error {
	if id == "" {
		return errors.New("knox: deviceId is required")
	}
	return nil
}

// SelectDeviceDetail fetches one device's full record (POST /device/selectDeviceInfo).
func (c *Client) SelectDeviceDetail(ctx context.Context, deviceID string) (DeviceDetail, error) {
	if err := requireDeviceID(deviceID); err != nil {
		return DeviceDetail{}, err
	}
	return call[DeviceDetail](ctx, c, http.MethodPost, "/device/selectDeviceInfo", url.Values{"deviceId": {deviceID}})
}

const (
	appPageSize = 1000 // the spec's maximum limit
	maxAppPages = 10   // bounds the loop if Knox keeps returning full pages
)

// SelectDeviceAppList lists all of a device's applications, paging through
// POST /device/selectDeviceAppList.
func (c *Client) SelectDeviceAppList(ctx context.Context, deviceID string) ([]DeviceApp, error) {
	if err := requireDeviceID(deviceID); err != nil {
		return nil, err
	}
	type result struct {
		Devicelist []DeviceApp `json:"devicelist"`
	}
	var apps []DeviceApp
	for page := 0; page < maxAppPages; page++ {
		r, err := call[result](ctx, c, http.MethodPost, "/device/selectDeviceAppList",
			url.Values{"deviceId": {deviceID}, "start": {strconv.Itoa(page * appPageSize)}, "limit": {strconv.Itoa(appPageSize)}})
		if err != nil {
			return nil, err
		}
		apps = append(apps, r.Devicelist...)
		if len(r.Devicelist) < appPageSize {
			break
		}
	}
	return apps, nil
}

// SelectDeviceLocation fetches the device's latest GPS fix (POST /device/selectDeviceLocation).
func (c *Client) SelectDeviceLocation(ctx context.Context, deviceID string) (DeviceLocation, error) {
	if err := requireDeviceID(deviceID); err != nil {
		return DeviceLocation{}, err
	}
	return call[DeviceLocation](ctx, c, http.MethodPost, "/device/selectDeviceLocation", url.Values{"deviceId": {deviceID}})
}

// SelectDeviceLocationHistory lists GPS fixes between from and to (POST
// /device/selectDeviceLocationHistory). Knox takes a whole number of days back from now, so the
// window is rounded up to it.
func (c *Client) SelectDeviceLocationHistory(ctx context.Context, deviceID string, from, to time.Time) ([]DeviceLocation, error) {
	if err := requireDeviceID(deviceID); err != nil {
		return nil, err
	}
	if !to.After(from) {
		return nil, errors.New("knox: invalid location history window")
	}
	days := int(math.Ceil(to.Sub(from).Hours() / 24))
	type result struct {
		LocationList []DeviceLocation `json:"locationList"`
	}
	r, err := call[result](ctx, c, http.MethodPost, "/device/selectDeviceLocationHistory",
		url.Values{"deviceId": {deviceID}, "period": {strconv.Itoa(days)}})
	return r.LocationList, err
}
