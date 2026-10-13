package api

import (
	"context"

	"github.com/gofiber/fiber/v3"

	"github.com/DonovanDiamond/knoxmgr/src/db"
)

const syncHistory = 10

type syncRunView struct {
	ID         int64   `json:"id"`
	Trigger    string  `json:"trigger"`
	StartedAt  string  `json:"startedAt"`
	FinishedAt *string `json:"finishedAt"`
	Status     string  `json:"status"`
	Error      string  `json:"error"`
	Counts     struct {
		Orgs        int `json:"orgs"`
		Users       int `json:"users"`
		Groups      int `json:"groups"`
		Memberships int `json:"memberships"`
		Devices     int `json:"devices"`
	} `json:"counts"`
}

func viewOfRun(r db.SyncRun) syncRunView {
	v := syncRunView{ID: r.ID, Trigger: r.Trigger, StartedAt: r.StartedAt, FinishedAt: r.FinishedAt, Status: r.Status, Error: r.Error}
	v.Counts.Orgs, v.Counts.Users, v.Counts.Groups, v.Counts.Memberships, v.Counts.Devices =
		r.Orgs, r.Users, r.Groups, r.Memberships, r.Devices
	return v
}

func (s *server) startSync(c fiber.Ctx) error {
	if s.Knox() == nil {
		return &fiberError{fiber.StatusServiceUnavailable, "Knox is not connected", "knox_not_configured"}
	}
	// The sync outlives this request, so it must not run on the request's context.
	id, already, err := s.Sync.SyncNow(context.Background())
	if err != nil {
		return err
	}
	// Joining a run that is already going changes nothing, so it is not an auditable action.
	if !already {
		s.record(c, "admin.sync.start", "sync", nil, fiber.Map{"id": id, "alreadyRunning": already})
	}
	return c.Status(fiber.StatusAccepted).JSON(fiber.Map{"id": id, "alreadyRunning": already})
}

func (s *server) syncStatus(c fiber.Ctx) error {
	runs, err := s.Mirror.RecentRuns(c.Context(), syncHistory)
	if err != nil {
		return err
	}
	views := make([]syncRunView, len(runs))
	for i, r := range runs {
		views[i] = viewOfRun(r)
	}
	body := fiber.Map{"running": false, "runs": views}
	if id, running := s.Sync.Running(); running {
		body["running"], body["runId"] = true, id
	}
	return c.JSON(body)
}
