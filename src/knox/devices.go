package knox

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"strconv"
)

// DeviceQuery pages /device/selectDeviceList. Zero values are not sent.
type DeviceQuery struct {
	UserID string
	Start  int // row index, >= 0
	Limit  int // 1..1000; 0 lets Knox default to 1000
}

// SelectDeviceList lists devices (POST /device/selectDeviceList).
func (c *Client) SelectDeviceList(ctx context.Context, q DeviceQuery) (DevicePage, error) {
	if q.Start < 0 || q.Limit < 0 || q.Limit > 1000 {
		return DevicePage{}, errors.New("knox: invalid paging")
	}
	f := url.Values{}
	set(f, "userId", q.UserID)
	f.Set("start", strconv.Itoa(q.Start))
	if q.Limit > 0 {
		f.Set("limit", strconv.Itoa(q.Limit))
	}
	type result struct {
		Devicelist []Device `json:"devicelist"`
		Total      Count    `json:"total"`
	}
	r, err := call[result](ctx, c, http.MethodPost, "/device/selectDeviceList", f)
	return DevicePage{Devices: r.Devicelist, Total: r.Total}, err
}

// SelectDeviceInfo fetches one device with its org (POST /device/selectDeviceInfo).
func (c *Client) SelectDeviceInfo(ctx context.Context, deviceID string) (Device, error) {
	if deviceID == "" {
		return Device{}, errors.New("knox: deviceId is required")
	}
	return call[Device](ctx, c, http.MethodPost, "/device/selectDeviceInfo", url.Values{"deviceId": {deviceID}})
}

// SelectDevicesByUser lists a user's devices, up to 1000 (POST /device/selectDevicesByUser).
// The spec types this resultValue as a single device although it documents a list; a
// devicelist wrapper, as selectDeviceList uses, is assumed.
func (c *Client) SelectDevicesByUser(ctx context.Context, userID string) ([]Device, error) {
	if userID == "" {
		return nil, errors.New("knox: userId is required")
	}
	type result struct {
		Devicelist []Device `json:"devicelist"`
	}
	r, err := call[result](ctx, c, http.MethodPost, "/device/selectDevicesByUser", url.Values{"userId": {userID}})
	return r.Devicelist, err
}
