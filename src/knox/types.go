package knox

import (
	"bytes"
	"encoding/json"
	"strconv"
)

// Count decodes a number that Knox sends as a JSON number or a string.
type Count int

func (n *Count) UnmarshalJSON(b []byte) error {
	b = bytes.Trim(b, `"`)
	if len(b) == 0 || string(b) == "null" {
		*n = 0
		return nil
	}
	v, err := strconv.Atoi(string(b))
	if err != nil {
		return err
	}
	*n = Count(v)
	return nil
}

// DeviceCount is a user's device count. Knox sends the spec's DeviceCount object, of which we
// keep the enrolled devices (falling back to total when enrolled is absent); a plain number or
// string is taken as the total.
type DeviceCount int

func (d *DeviceCount) UnmarshalJSON(b []byte) error {
	if bytes.HasPrefix(b, []byte("{")) {
		var o struct {
			Total    Count
			Enrolled *Count
		}
		if err := json.Unmarshal(b, &o); err != nil {
			return err
		}
		if o.Enrolled != nil {
			*d = DeviceCount(*o.Enrolled)
		} else {
			*d = DeviceCount(o.Total)
		}
		return nil
	}
	var n Count
	if err := n.UnmarshalJSON(b); err != nil {
		return err
	}
	*d = DeviceCount(n)
	return nil
}

// User is a Knox Manage user (UserEmm in the spec), limited to fields we use.
type User struct {
	UserID      string      `json:"userId"`
	UserName    string      `json:"userName"`
	Email       string      `json:"email"`
	FirstName   string      `json:"firstName"`
	LastName    string      `json:"lastName"`
	MobilePhone string      `json:"mobilePhone"`
	Contact     string      `json:"contact"`
	EmpNo       string      `json:"empNo"`
	OrgCode     string      `json:"orgCode"`
	OrgName     string      `json:"orgName"`
	Enabled     string      `json:"enabled"` // "1" enabled, "0" disabled
	UserType    string      `json:"userType"`
	DeviceCount DeviceCount `json:"deviceCount"`
}

// UserPage is one page of users and the total match count.
type UserPage struct {
	Users []User
	Total Count
}

// Group is a Knox user group.
type Group struct {
	GroupID   string `json:"groupId"`
	GroupName string `json:"groupName"`
	GroupType string `json:"groupType"`
	UserCount Count  `json:"userCount"`
	// Assigned app/profile counts; Knox only sends them when needDeviceUserCount=yes.
	AssignAppCount     Count `json:"assignAppCount"`
	AssignProfileCount Count `json:"assignProfileCount"`
}

// GroupPage is one page of groups and the total match count.
type GroupPage struct {
	Groups []Group
	Total  Count
}

// Organization is a Knox organization node.
type Organization struct {
	OrgCode    string `json:"orgCode"`
	OrgName    string `json:"orgName"`
	ParentCode string `json:"parentCode"`
}

// Device is a Knox device (SimpleDevice / DeviceDetail in the spec), limited to fields we use.
// The list shape carries no orgCode; only DeviceDetail does.
type Device struct {
	DeviceID          string `json:"deviceId"`
	DeviceModel       string `json:"deviceModel"`
	DeviceModelKind   string `json:"deviceModelKind"`
	DeviceStatus      string `json:"deviceStatus"` // I, P, A, B, BS, BA or BL
	DeviceVersionName string `json:"deviceVersionName"`
	Platform          string `json:"platform"` // A, I or W
	UserID            string `json:"userId"`
	UserName          string `json:"userName"`
	OrgCode           string `json:"orgCode"`
	// LastConnection is stdFormatLastConnectionDate (RFC 3339); empty when never connected.
	LastConnection string `json:"stdFormatLastConnectionDate"`
}

// DevicePage is one page of devices and the total match count.
type DevicePage struct {
	Devices []Device
	Total   Count
}
