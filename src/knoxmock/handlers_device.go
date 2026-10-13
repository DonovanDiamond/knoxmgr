package knoxmock

import (
	"net/http"
	"strconv"
	"time"
)

func (s *Server) selectDeviceList(w http.ResponseWriter, r *http.Request) {
	start, limit, valid := page(r, "start", "limit")
	if !valid {
		fail(w, codeBadParam, "invalid start or limit")
		return
	}
	user, status := r.FormValue("userId"), r.FormValue("deviceStatus")
	s.mu.Lock()
	defer s.mu.Unlock()
	all := s.deviceViews(func(d *device) bool {
		return (user == "" || d.UserID == user) && (status == "" || d.DeviceStatus == status)
	})
	ok(w, map[string]any{"total": len(all), "devicelist": window(all, start, limit)})
}

func (s *Server) selectDeviceInfo(w http.ResponseWriter, r *http.Request) {
	id := r.FormValue("deviceId")
	if !idPattern.MatchString(id) {
		fail(w, codeBadParam, "deviceId is required")
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	d := s.devices[id]
	if d == nil {
		fail(w, codeNotFound, "device not found")
		return
	}
	ok(w, s.deviceDetail(d))
}

func (s *Server) selectDevicesByUser(w http.ResponseWriter, r *http.Request) {
	id := r.FormValue("userId")
	if !idPattern.MatchString(id) {
		fail(w, codeBadParam, "userId is required")
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.users[id] == nil {
		fail(w, codeNotFound, "user not found")
		return
	}
	ok(w, map[string]any{"devicelist": window(s.deviceViews(func(d *device) bool { return d.UserID == id }), 0, 1000)})
}

// knownDevice resolves the required deviceId and writes the error response itself when it cannot.
// Caller holds s.mu on return when d is non-nil.
func (s *Server) knownDevice(w http.ResponseWriter, r *http.Request) *device {
	id := r.FormValue("deviceId")
	if !idPattern.MatchString(id) {
		fail(w, codeBadParam, "deviceId is required")
		return nil
	}
	s.mu.Lock()
	d := s.devices[id]
	if d == nil {
		s.mu.Unlock()
		fail(w, codeNotFound, "device not found")
	}
	return d
}

func (s *Server) selectDeviceAppList(w http.ResponseWriter, r *http.Request) {
	start, limit, valid := page(r, "start", "limit")
	if !valid {
		fail(w, codeBadParam, "invalid start or limit")
		return
	}
	d := s.knownDevice(w, r)
	if d == nil {
		return
	}
	defer s.mu.Unlock()
	apps := deviceApps(d)
	ok(w, map[string]any{"total": strconv.Itoa(len(apps)), "devicelist": window(apps, start, limit)})
}

func (s *Server) selectDeviceLocation(w http.ResponseWriter, r *http.Request) {
	d := s.knownDevice(w, r)
	if d == nil {
		return
	}
	defer s.mu.Unlock()
	pts := locationHistory(d, time.Now().UTC(), 1)
	if len(pts) == 0 {
		ok(w, nil)
		return
	}
	// The single-location schema calls the date "updated"; only the history schema says locationInfoUpdated.
	pt := pts[0]
	pt["updated"] = pt["locationInfoUpdated"]
	delete(pt, "locationInfoUpdated")
	ok(w, pt)
}

func (s *Server) selectDeviceLocationHistory(w http.ResponseWriter, r *http.Request) {
	period, err := strconv.Atoi(r.FormValue("period"))
	if err != nil || period < 1 || period > locationDays {
		fail(w, codeBadParam, "period must be a number of days between 1 and "+strconv.Itoa(locationDays))
		return
	}
	d := s.knownDevice(w, r)
	if d == nil {
		return
	}
	defer s.mu.Unlock()
	pts := locationHistory(d, time.Now().UTC(), period)
	ok(w, map[string]any{"total": strconv.Itoa(len(pts)), "locationList": pts})
}
