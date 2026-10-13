package api

import (
	"errors"
	"time"

	"github.com/gofiber/fiber/v3"

	"github.com/DonovanDiamond/knoxmgr/src/update"
)

type updateView struct {
	Enabled         bool   `json:"enabled"`
	Current         string `json:"current"`
	Latest          string `json:"latest,omitempty"`
	URL             string `json:"url,omitempty"`
	UpdateAvailable bool   `json:"updateAvailable"`
	CheckedAt       string `json:"checkedAt,omitempty"`
	Error           string `json:"error,omitempty"`
}

func (s *server) updateView() updateView {
	st := s.Update.Status()
	v := updateView{Enabled: s.Settings.CheckUpdates(), Current: s.Version, Latest: st.Latest, URL: st.URL,
		UpdateAvailable: st.UpdateAvailable, Error: st.Error}
	if !st.CheckedAt.IsZero() {
		v.CheckedAt = st.CheckedAt.Format(time.RFC3339)
	}
	return v
}

func (s *server) updateStatus(c fiber.Ctx) error { return c.JSON(s.updateView()) }

func (s *server) updateCheck(c fiber.Ctx) error {
	// A failed check is reported in the view, not as an HTTP error: the request itself worked.
	if _, err := s.Update.Check(c.Context()); errors.Is(err, update.ErrDisabled) {
		return &fiberError{fiber.StatusConflict, "update checks are disabled", "updates_disabled"}
	}
	v := s.updateView()
	s.record(c, "admin.update.check", "update", nil, fiber.Map{"latest": v.Latest, "updateAvailable": v.UpdateAvailable, "error": v.Error})
	return c.JSON(v)
}
