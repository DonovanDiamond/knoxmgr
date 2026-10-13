package knox

import (
	"context"
	"errors"
	"math"
	"net/http"
	"net/url"
	"strconv"
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

// DeviceApp is one installed application.
type DeviceApp struct {
	AppName     string `json:"appName"`
	PackageName string `json:"packageName"`
	VersionName string `json:"versionName"`
}

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

// SelectDeviceAppList lists a device's applications, up to 1000 (POST /device/selectDeviceAppList).
func (c *Client) SelectDeviceAppList(ctx context.Context, deviceID string) ([]DeviceApp, error) {
	if err := requireDeviceID(deviceID); err != nil {
		return nil, err
	}
	type result struct {
		Devicelist []DeviceApp `json:"devicelist"`
	}
	r, err := call[result](ctx, c, http.MethodPost, "/device/selectDeviceAppList",
		url.Values{"deviceId": {deviceID}, "start": {"0"}, "limit": {"1000"}})
	return r.Devicelist, err
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
