package knox

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"math"
	"net/http"
	"net/url"
	"slices"
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
	AppName      flexString `json:"appName"`
	PackageName  flexString `json:"packageName"`
	VersionName  flexString `json:"versionName"`
	SystemApp    flexString `json:"systemApp"`
	IsManaged    flexString `json:"isManaged"`
	MandatoryApp flexString `json:"mandatoryApp"`
	Installed    flexString `json:"stdFormatInstalled"` // UTC ISO 8601
	KnoxID       flexString `json:"knoxId"`             // empty for the default area
}

// flexString reads a JSON string, number or boolean as text and anything else (null, object,
// array) as empty. The real selectDeviceAppList item types differ from the spec and are not all
// confirmed, so one odd type must not fail a whole device.
type flexString string

func (f *flexString) UnmarshalJSON(b []byte) error {
	switch b = bytes.TrimSpace(b); {
	case len(b) == 0:
		*f = ""
	case b[0] == '"':
		var s string
		if err := json.Unmarshal(b, &s); err != nil {
			return err
		}
		*f = flexString(s)
	case b[0] == 't', b[0] == 'f', b[0] == '-', b[0] >= '0' && b[0] <= '9':
		*f = flexString(b)
	default:
		*f = ""
	}
	return nil
}

// flag reads Knox's loosely typed booleans: Y/N, true/false or 1/0.
func flag(v flexString) bool {
	switch strings.ToLower(strings.TrimSpace(string(v))) {
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

// AppListDiagnostic describes the shape of an empty selectDeviceAppList answer, so an unexpected
// payload can be diagnosed from logs. It holds key names and types only, never values.
type AppListDiagnostic struct {
	ResultType string            // type of resultValue when it is not an object
	Keys       map[string]string // resultValue key -> JSON type
	Total      string            // the numeric total, "" when absent or not a number
	AppKeys    []string          // arrays other than appList and devicelist whose items have a packageName
}

// jsonType names the JSON type of a raw value.
func jsonType(raw json.RawMessage) string {
	raw = bytes.TrimSpace(raw)
	if len(raw) == 0 {
		return "missing"
	}
	switch raw[0] {
	case '[':
		return "array"
	case '{':
		return "object"
	case '"':
		return "string"
	case 'n':
		return "null"
	case 't', 'f':
		return "boolean"
	}
	return "number"
}

func diagnoseAppList(raw json.RawMessage, fields map[string]json.RawMessage) *AppListDiagnostic {
	d := &AppListDiagnostic{Keys: map[string]string{}}
	if fields == nil {
		d.ResultType = jsonType(raw)
		return d
	}
	d.ResultType = "object"
	for k, v := range fields {
		d.Keys[k] = jsonType(v)
		if k == "appList" || k == "devicelist" {
			continue
		}
		var items []struct {
			PackageName string `json:"packageName"`
		}
		if json.Unmarshal(v, &items) == nil && slices.ContainsFunc(items, func(i struct {
			PackageName string `json:"packageName"`
		}) bool {
			return i.PackageName != ""
		}) {
			d.AppKeys = append(d.AppKeys, k)
		}
	}
	slices.Sort(d.AppKeys)
	var total string
	if json.Unmarshal(fields["total"], &total) != nil {
		total = strings.TrimSpace(string(fields["total"]))
	}
	if _, err := strconv.Atoi(total); err == nil {
		d.Total = total
	}
	return d
}

// itemShape maps the key names of a raw app item to their JSON types, never values.
func itemShape(raw json.RawMessage) map[string]string {
	var item map[string]json.RawMessage
	if json.Unmarshal(raw, &item) != nil {
		return nil
	}
	shape := make(map[string]string, len(item))
	for k, v := range item {
		shape[k] = jsonType(v)
	}
	return shape
}

// SelectDeviceAppList lists all of a device's applications, paging through
// POST /device/selectDeviceAppList. Real Knox answers {"appList": [...], "total": <number>}, unlike
// the spec's devicelist and string total; devicelist is read as a fallback. When Knox answers
// success with no apps the diagnostic is non-nil. shape holds the first item's key names and JSON
// types (nil when there are no apps).
func (c *Client) SelectDeviceAppList(ctx context.Context, deviceID string) (apps []DeviceApp, diag *AppListDiagnostic, shape map[string]string, err error) {
	if err := requireDeviceID(deviceID); err != nil {
		return nil, nil, nil, err
	}
	for page := 0; page < maxAppPages; page++ {
		raw, err := call[json.RawMessage](ctx, c, http.MethodPost, "/device/selectDeviceAppList",
			url.Values{"deviceId": {deviceID}, "start": {strconv.Itoa(page * appPageSize)}, "limit": {strconv.Itoa(appPageSize)}})
		if err != nil {
			return nil, nil, nil, err
		}
		var fields map[string]json.RawMessage
		_ = json.Unmarshal(raw, &fields) // a non-object resultValue is reported by the diagnostic
		items, found := fields["appList"]
		if !found {
			items, found = fields["devicelist"]
		}
		var list []DeviceApp
		if found {
			if err := json.Unmarshal(items, &list); err != nil {
				return nil, nil, nil, errors.New("knox: /device/selectDeviceAppList returned an unreadable response")
			}
		}
		if page == 0 {
			if len(list) == 0 {
				diag = diagnoseAppList(raw, fields)
			} else {
				var rawItems []json.RawMessage
				if json.Unmarshal(items, &rawItems) == nil && len(rawItems) > 0 {
					shape = itemShape(rawItems[0])
				}
			}
		}
		apps = append(apps, list...)
		if len(list) < appPageSize {
			break
		}
	}
	return apps, diag, shape, nil
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
