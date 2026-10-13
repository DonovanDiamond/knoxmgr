package api

import (
	"github.com/gofiber/fiber/v3"

	"github.com/DonovanDiamond/knoxmgr/src/settings"
)

func (s *server) settingsRoutes(r fiber.Router) {
	r.Get("/knox", s.getKnoxSettings)
	r.Put("/knox", s.putKnoxSettings)
	r.Post("/knox/test", s.testKnoxSettings)
	r.Get("/ad", s.getADSettings)
	r.Put("/ad", s.putADSettings)
	r.Post("/ad/test", s.testADSettings)
	r.Get("/general", s.getGeneralSettings)
	r.Put("/general", s.putGeneralSettings)
}

func (s *server) getKnoxSettings(c fiber.Ctx) error {
	v, err := s.Settings.GetKnox(c.Context())
	if err != nil {
		return err
	}
	return c.JSON(v)
}

func (s *server) putKnoxSettings(c fiber.Ctx) error {
	var in settings.KnoxInput
	if err := bind(c, &in); err != nil {
		return err
	}
	v, err := s.Settings.SaveKnox(c.Context(), in, principalFrom(c))
	if err != nil {
		return err
	}
	return c.JSON(v)
}

func (s *server) testKnoxSettings(c fiber.Ctx) error {
	var in settings.KnoxInput
	if err := bind(c, &in); err != nil {
		return err
	}
	res, err := s.Settings.TestKnox(c.Context(), in)
	if err != nil {
		return err
	}
	return c.JSON(res)
}

func (s *server) getADSettings(c fiber.Ctx) error {
	v, err := s.Settings.GetAD(c.Context())
	if err != nil {
		return err
	}
	return c.JSON(v)
}

func (s *server) putADSettings(c fiber.Ctx) error {
	var in settings.ADInput
	if err := bind(c, &in); err != nil {
		return err
	}
	v, err := s.Settings.SaveAD(c.Context(), in, principalFrom(c))
	if err != nil {
		return err
	}
	return c.JSON(v)
}

func (s *server) testADSettings(c fiber.Ctx) error {
	var req struct {
		Username string            `json:"username"`
		Password string            `json:"password"`
		Settings *settings.ADInput `json:"settings"`
	}
	if err := bind(c, &req); err != nil {
		return err
	}
	if req.Username == "" || req.Password == "" || len(req.Username) > maxUsernameLen || len(req.Password) > maxPasswordLen {
		return badRequest("username and password are required")
	}
	res, err := s.Settings.TestAD(c.Context(), req.Username, req.Password, req.Settings)
	if err != nil {
		return err
	}
	return c.JSON(res)
}

func (s *server) getGeneralSettings(c fiber.Ctx) error {
	v, err := s.Settings.GetGeneral(c.Context())
	if err != nil {
		return err
	}
	return c.JSON(v)
}

func (s *server) putGeneralSettings(c fiber.Ctx) error {
	var in settings.GeneralInput
	if err := bind(c, &in); err != nil {
		return err
	}
	v, err := s.Settings.SaveGeneral(c.Context(), in, principalFrom(c))
	if err != nil {
		return err
	}
	return c.JSON(v)
}
