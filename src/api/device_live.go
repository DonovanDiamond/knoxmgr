package api

import (
	"context"
	"encoding/json"
	"errors"
	"math"
	"strconv"
	"time"

	"github.com/gofiber/fiber/v3"

	"github.com/DonovanDiamond/knoxmgr/src/auth"
	"github.com/DonovanDiamond/knoxmgr/src/db"
	"github.com/DonovanDiamond/knoxmgr/src/knox"
)

// liveOrCached fills v from fetch and caches it; when Knox is unreachable it fills v from the
// last cached answer of that kind instead. Any other failure, and an unreachable Knox with
// nothing cached, is returned as it is.
func (s *server) liveOrCached(ctx context.Context, deviceID, kind string, v any, fetch func() error) (time.Time, bool, error) {
	err := fetch()
	if err == nil {
		at := time.Now().UTC()
		if b, merr := json.Marshal(v); merr == nil {
			if perr := s.Mirror.PutDeviceCache(ctx, deviceID, kind, string(b), at); perr != nil {
				s.Log.Error().Err(perr).Str("deviceId", deviceID).Str("kind", kind).Msg("device cache write failed")
			}
		}
		return at, false, nil
	}
	if !errors.Is(err, knox.ErrUnreachable) {
		return time.Time{}, false, err
	}
	e, cerr := s.Mirror.DeviceCache(ctx, deviceID, kind)
	if errors.Is(cerr, db.ErrNotFound) {
		return time.Time{}, false, err
	}
	if cerr != nil {
		return time.Time{}, false, cerr
	}
	if json.Unmarshal([]byte(e.Payload), v) != nil {
		return time.Time{}, false, err
	}
	return e.FetchedAt, true, nil
}

type deviceInfo struct {
	Model          string `json:"model,omitempty"`
	ModelName      string `json:"modelName,omitempty"`
	OS             string `json:"os,omitempty"`
	OSVersion      string `json:"osVersion,omitempty"`
	IMEI           string `json:"imei,omitempty"`
	SerialNumber   string `json:"serialNumber,omitempty"`
	Carrier        string `json:"carrier,omitempty"`
	PhoneNumber    string `json:"phoneNumber,omitempty"`
	BatteryLevel   string `json:"batteryLevel,omitempty"`
	Rooted         *bool  `json:"rooted,omitempty"`
	AgentVersion   string `json:"agentVersion,omitempty"`
	ClientVersion  string `json:"clientVersion,omitempty"`
	CreatedAt      string `json:"createdAt,omitempty"`
	LastConnection string `json:"lastConnection,omitempty"`
}

type detailPayload struct {
	Info     deviceInfo `json:"info"`
	Profiles []string   `json:"profiles"`
}

type detailResponse struct {
	detailPayload
	FetchedAt       string `json:"fetchedAt"`
	Cached          bool   `json:"cached"`
	CanViewLocation bool   `json:"canViewLocation"`
}

// rfc3339 re-renders a Knox ISO 8601 time; anything unparseable is dropped.
func rfc3339(s string) string {
	t := parseConnection(s)
	if t.IsZero() {
		return ""
	}
	return t.UTC().Format(time.RFC3339)
}

func detailOf(d knox.DeviceDetail) detailPayload {
	info := deviceInfo{
		Model: d.DeviceModel, ModelName: d.DeviceModelKind, OS: platformText[d.Platform], OSVersion: d.DeviceVersionName,
		IMEI: d.IMEI, SerialNumber: d.SerialNumber, Carrier: d.Carrier, PhoneNumber: d.Phone, BatteryLevel: d.Battery,
		// Knox has no agent or client version field; the profile names are the nearest it reports.
		AgentVersion: d.AgentProfileName, ClientVersion: d.ClientProfileName,
		CreatedAt: rfc3339(d.Created), LastConnection: rfc3339(d.LastConnection),
	}
	switch d.IsRooting {
	case "Y":
		info.Rooted = new(true)
	case "N":
		info.Rooted = new(false)
	}
	p := detailPayload{Info: info, Profiles: d.Profiles}
	if p.Profiles == nil {
		p.Profiles = []string{}
	}
	return p
}

func canViewLocation(c fiber.Ctx) bool { return auth.RequireWrite(principalFrom(c)) == nil }

func (s *server) deviceDetail(c fiber.Ctx) error {
	d, err := s.inScopeDevice(c)
	if err != nil {
		return err
	}
	var p detailPayload
	at, cached, err := s.liveOrCached(c.Context(), d.DeviceID, "detail", &p, func() error {
		raw, err := s.Knox().SelectDeviceDetail(c.Context(), d.DeviceID)
		p = detailOf(raw)
		return err
	})
	if err != nil {
		return err
	}
	return c.JSON(detailResponse{detailPayload: p, FetchedAt: at.Format(time.RFC3339), Cached: cached, CanViewLocation: canViewLocation(c)})
}

type appView struct {
	Name        string `json:"name"`
	PackageName string `json:"packageName"`
	Version     string `json:"version,omitempty"`
}

type appsPayload struct {
	Apps []appView `json:"apps"`
}

func (s *server) deviceApps(c fiber.Ctx) error {
	d, err := s.inScopeDevice(c)
	if err != nil {
		return err
	}
	p := appsPayload{Apps: []appView{}}
	at, cached, err := s.liveOrCached(c.Context(), d.DeviceID, "apps", &p, func() error {
		apps, err := s.Knox().SelectDeviceAppList(c.Context(), d.DeviceID)
		if err != nil {
			return err
		}
		p.Apps = make([]appView, len(apps))
		for i, a := range apps {
			p.Apps[i] = appView{Name: a.AppName, PackageName: a.PackageName, Version: a.VersionName}
		}
		return nil
	})
	if err != nil {
		return err
	}
	return c.JSON(fiber.Map{"apps": p.Apps, "fetchedAt": at.Format(time.RFC3339), "cached": cached})
}

type locationPoint struct {
	Time      string   `json:"time"`
	Latitude  float64  `json:"latitude"`
	Longitude float64  `json:"longitude"`
	Altitude  *float64 `json:"altitude,omitempty"`
}

type locationPayload struct {
	Current *locationPoint  `json:"current"`
	History []locationPoint `json:"history"`
	Days    int             `json:"days"`
}

// pointOf converts a Knox fix, or reports false when its coordinates are not numbers in range.
func pointOf(l knox.DeviceLocation) (locationPoint, bool) {
	lat, err1 := strconv.ParseFloat(l.Latitude, 64)
	lon, err2 := strconv.ParseFloat(l.Longitude, 64)
	if err1 != nil || err2 != nil || math.IsNaN(lat) || math.IsNaN(lon) || math.Abs(lat) > 90 || math.Abs(lon) > 180 {
		return locationPoint{}, false
	}
	p := locationPoint{Time: rfc3339(l.Updated), Latitude: lat, Longitude: lon}
	if alt, err := strconv.ParseFloat(l.Altitude, 64); err == nil && !math.IsNaN(alt) && !math.IsInf(alt, 0) {
		p.Altitude = &alt
	}
	return p, true
}

var locationDays = map[string]int{"1": 1, "7": 7, "30": 30}

func (s *server) deviceLocation(c fiber.Ctx) error {
	d, err := s.inScopeDevice(c)
	if err != nil {
		return err
	}
	// Location is sensitive: read-only users are told the device does not exist.
	if !canViewLocation(c) {
		return errDeviceNotFound
	}
	raw := "7"
	if c.RequestCtx().QueryArgs().Has("days") {
		raw = c.Query("days")
	}
	days, ok := locationDays[raw]
	if !ok {
		return fiber.NewError(fiber.StatusBadRequest, "days must be 1, 7 or 30")
	}
	p := locationPayload{History: []locationPoint{}, Days: days}
	at, cached, err := s.liveOrCached(c.Context(), d.DeviceID, "location:"+strconv.Itoa(days), &p, func() error {
		p = locationPayload{History: []locationPoint{}, Days: days}
		cur, err := s.Knox().SelectDeviceLocation(c.Context(), d.DeviceID)
		if err != nil {
			return err
		}
		if pt, ok := pointOf(cur); ok {
			p.Current = &pt
		}
		to := time.Now()
		hist, err := s.Knox().SelectDeviceLocationHistory(c.Context(), d.DeviceID, to.AddDate(0, 0, -days), to)
		if err != nil {
			return err
		}
		for _, l := range hist {
			if pt, ok := pointOf(l); ok {
				p.History = append(p.History, pt)
			}
		}
		return nil
	})
	if err != nil {
		return err
	}
	s.record(c, "device.location.view", d.DeviceID, nil, fiber.Map{"days": days, "cached": cached})
	return c.JSON(fiber.Map{"current": p.Current, "history": p.History, "days": days, "fetchedAt": at.Format(time.RFC3339), "cached": cached,
		"showMap": s.Settings.ShowMap()})
}
