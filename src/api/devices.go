package api

import (
	"errors"
	"strings"
	"time"

	"github.com/gofiber/fiber/v3"

	"github.com/DonovanDiamond/knoxmgr/src/db"
	"github.com/DonovanDiamond/knoxmgr/src/knox"
)

const (
	staleAfter = 24 * time.Hour
	oldAfter   = 7 * 24 * time.Hour
)

const (
	freshNone  = "none"
	freshFresh = "fresh"
	freshStale = "stale"
	freshOld   = "old"
	// Per-device grades only: a summary of enrolled devices never uses them.
	freshNever       = "never"
	freshNotEnrolled = "not_enrolled"
)

// deviceFreshness grades one device. Unlike freshnessOf, a device that never connected is its
// own grade, and a device that is not enrolled is not graded on connection at all.
func deviceFreshness(enrolled bool, last, now time.Time) string {
	switch {
	case !enrolled:
		return freshNotEnrolled
	case last.IsZero():
		return freshNever
	default:
		return freshnessOf(1, last, now)
	}
}

// freshnessOf grades how recently enrolled devices connected. last is the stalest enrolled
// connection; the zero time means an enrolled device never connected.
func freshnessOf(enrolled int, last, now time.Time) string {
	switch age := now.Sub(last); {
	case enrolled == 0:
		return freshNone
	case last.IsZero() || age > oldAfter:
		return freshOld
	case age > staleAfter:
		return freshStale
	default:
		return freshFresh
	}
}

// deviceView is the API shape of a Knox device.
type deviceView struct {
	DeviceID       string `json:"deviceId"`
	ModelName      string `json:"modelName"`
	DeviceName     string `json:"deviceName,omitempty"`
	OS             string `json:"os"`
	OSVersion      string `json:"osVersion,omitempty"`
	Status         string `json:"status"`
	Enrolled       bool   `json:"enrolled"`
	LastConnection string `json:"lastConnection,omitempty"`
	Freshness      string `json:"freshness"`
	UserID         string `json:"userId,omitempty"`
	UserName       string `json:"userName,omitempty"`
	OrgCode        string `json:"orgCode,omitempty"`
}

// deviceSummary is a user's enrolled devices; LastConnection is that of the stalest one, and is
// absent when any enrolled device never connected.
type deviceSummary struct {
	Enrolled       int    `json:"enrolled"`
	LastConnection string `json:"lastConnection,omitempty"`
	Freshness      string `json:"freshness"`
}

var (
	statusText = map[string]string{
		"I": "Deactivated", "P": "Provisioned", "A": "Activated", "B": "Blocked",
		"BS": "Blocked (system)", "BA": "Blocked (admin)", "BL": "License expired",
	}
	platformText = map[string]string{"A": "Android", "I": "iOS", "W": "Windows"}
)

// isEnrolled: provisioned and deactivated devices are not enrolled; blocked ones still are.
func isEnrolled(status string) bool {
	_, known := statusText[status]
	return known && status != "I" && status != "P"
}

func parseConnection(s string) time.Time {
	t, _ := time.Parse(time.RFC3339, s)
	return t
}

// viewOfDevice renders d; orgCode and userName fill what the Knox list shape lacks.
func viewOfDevice(d knox.Device, orgCode, userName string, now time.Time) deviceView {
	last := parseConnection(d.LastConnection)
	enrolled := isEnrolled(d.DeviceStatus)
	v := deviceView{
		DeviceID: d.DeviceID, ModelName: d.DeviceModelKind, OS: platformText[d.Platform], OSVersion: d.DeviceVersionName,
		Status: statusText[d.DeviceStatus], Enrolled: enrolled, UserID: d.UserID, UserName: userName, OrgCode: orgCode,
	}
	if v.ModelName == "" {
		v.ModelName = d.DeviceModel
	}
	if !last.IsZero() {
		v.LastConnection = last.UTC().Format(time.RFC3339)
	}
	v.Freshness = deviceFreshness(enrolled, last, now)
	return v
}

// summarize groups enrolled devices by user. Users without enrolled devices are absent.
func summarize(devices []knox.Device, now time.Time) map[string]deviceSummary {
	type acc struct {
		n     int
		stale time.Time // zero once any enrolled device never connected
		never bool
	}
	by := map[string]*acc{}
	for _, d := range devices {
		if d.UserID == "" || !isEnrolled(d.DeviceStatus) {
			continue
		}
		a := by[d.UserID]
		if a == nil {
			a = &acc{}
			by[d.UserID] = a
		}
		a.n++
		if last := parseConnection(d.LastConnection); last.IsZero() {
			a.never = true
		} else if a.stale.IsZero() || last.Before(a.stale) {
			a.stale = last
		}
	}
	out := make(map[string]deviceSummary, len(by))
	for id, a := range by {
		sum := deviceSummary{Enrolled: a.n}
		last := a.stale
		if a.never {
			last = time.Time{}
		} else {
			sum.LastConnection = last.UTC().Format(time.RFC3339)
		}
		sum.Freshness = freshnessOf(a.n, last, now)
		out[id] = sum
	}
	return out
}

var errDeviceNotFound = fiber.NewError(fiber.StatusNotFound, "device not found")

func (s *server) listDevices(c fiber.Ctx) error {
	q := strings.ToLower(c.Query("q"))
	if err := checkText("q", q, maxTextLen, false); err != nil {
		return err
	}
	_, orgs, err := s.scopeOrgs(c)
	if err != nil {
		return err
	}
	// Devices are scoped by their owner's org in SQL.
	devices, err := s.Mirror.Devices(c.Context(), orgs)
	if err != nil {
		return err
	}
	now := time.Now()
	out := []deviceView{}
	for _, d := range devicesOfMirror(devices) {
		v := viewOfDevice(d, d.OrgCode, d.UserName, now)
		if q == "" || strings.Contains(strings.ToLower(v.ModelName+"\x00"+v.DeviceName+"\x00"+v.UserName+"\x00"+v.UserID), q) {
			out = append(out, v)
		}
	}
	return c.JSON(fiber.Map{"devices": out, "total": len(out)})
}

// inScopeDevice loads the device named by :id for the caller. Out of scope looks like absent.
func (s *server) inScopeDevice(c fiber.Ctx) (knox.Device, error) {
	id := c.Params("id")
	if err := checkPattern(knoxIDRe, "device id", id); err != nil {
		return knox.Device{}, err
	}
	sc, err := s.scope(c)
	if err != nil {
		return knox.Device{}, err
	}
	md, err := s.Mirror.Device(c.Context(), id)
	if errors.Is(err, db.ErrNotFound) {
		return knox.Device{}, errDeviceNotFound
	}
	if err != nil {
		return knox.Device{}, err
	}
	d := devicesOfMirror([]db.MirrorDevice{md})[0]
	// An unassigned device has no org, which only an admin's scope admits.
	org := d.OrgCode
	if d.UserID == "" {
		org = ""
	}
	if !sc.CanReadOrg(org) {
		return knox.Device{}, errDeviceNotFound
	}
	return d, nil
}

func (s *server) getDevice(c fiber.Ctx) error {
	d, err := s.inScopeDevice(c)
	if err != nil {
		return err
	}
	return c.JSON(viewOfDevice(d, d.OrgCode, d.UserName, time.Now()))
}

func (s *server) listUserDevices(c fiber.Ctx) error {
	id := c.Params("id")
	if err := checkPattern(knoxIDRe, "user id", id); err != nil {
		return err
	}
	sc, err := s.scope(c)
	if err != nil {
		return err
	}
	u, err := s.mirrorUser(c, id)
	if err != nil {
		return err
	}
	if !sc.CanReadOrg(u.OrgCode) {
		return errUserNotFound
	}
	devices, err := s.Mirror.DevicesOfUsers(c.Context(), []string{id})
	if err != nil {
		return err
	}
	now := time.Now()
	out := make([]deviceView, len(devices))
	for i, d := range devicesOfMirror(devices) {
		out[i] = viewOfDevice(d, u.OrgCode, u.UserName, now)
	}
	return c.JSON(fiber.Map{"devices": out, "total": len(out)})
}
