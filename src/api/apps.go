package api

import (
	"errors"
	"strconv"
	"strings"
	"time"

	"github.com/gofiber/fiber/v3"

	"github.com/DonovanDiamond/knoxmgr/src/auth"
	"github.com/DonovanDiamond/knoxmgr/src/db"
	"github.com/DonovanDiamond/knoxmgr/src/knox"
)

func (s *server) appRoutes(r fiber.Router) {
	r.Get("/apps", s.listApps)
	r.Get("/apps/:package", s.getApp)
	r.Post("/apps/:package/uninstall", RequireWrite(), s.uninstallApp)
	r.Get("/app-categories", s.listAppCategories)
	r.Get("/devices/:id/apps", s.deviceApps)
	r.Post("/devices/:id/apps/refresh", RequireWrite(), s.refreshDeviceApps)
}

func (s *server) appAdminRoutes(r fiber.Router) {
	r.Post("/app-categories", s.createAppCategory)
	r.Put("/app-categories/:id", s.renameAppCategory)
	r.Delete("/app-categories/:id", s.deleteAppCategory)
	r.Put("/apps/:package/category", s.setAppCategory)
}

type appSummaryView struct {
	Package     string `json:"package"`
	Name        string `json:"name"`
	System      bool   `json:"system"`
	Category    string `json:"category"`
	CategoryID  *int64 `json:"categoryId"`
	DeviceCount int    `json:"deviceCount"`
	Managed     bool   `json:"managed"`
	Mandatory   bool   `json:"mandatory"`
	Protected   bool   `json:"protected"`
	// ProtectedReason is shown to the user; empty when the app can be uninstalled.
	ProtectedReason string `json:"protectedReason"`
}

const categoryDeviceManagement = "Device management"

// mdmPackages and mdmPrefixes name the Knox/MDM agents that must never be uninstalled remotely,
// whatever Knox reports for them.
var (
	mdmPackages = map[string]bool{
		"com.samsung.android.knox.kmagent":        true,
		"com.samsung.android.knox.containeragent": true,
		"com.samsung.klmsagent":                   true,
	}
	mdmPrefixes = []string{"com.sds.emm.", "com.sec.enterprise.", "com.samsung.android.knox."}
)

// protectedReason says why pkg must not be uninstalled, or "" when it may be.
func protectedReason(pkg, category string, system, managed, mandatory bool) string {
	switch {
	case system:
		return "System apps cannot be uninstalled."
	case managed:
		return "This app is managed by Knox. Remove it from the Knox policy instead."
	case mandatory:
		return "This app is mandatory in the Knox policy."
	case category == categoryDeviceManagement:
		return "This app is part of device management."
	case mdmPackages[pkg]:
		return "This is a Knox management component."
	}
	for _, p := range mdmPrefixes {
		if strings.HasPrefix(pkg, p) {
			return "This is a Knox management component."
		}
	}
	return ""
}

func errAppProtected(reason string) error {
	return &fiberError{fiber.StatusForbidden, reason, "app_protected"}
}

func appSummaryOf(a db.AppSummary) appSummaryView {
	reason := protectedReason(a.Package, a.Category, a.System, a.Managed, a.Mandatory)
	return appSummaryView{a.Package, a.Name, a.System, a.Category, a.CategoryID, a.DeviceCount, a.Managed, a.Mandatory, reason != "", reason}
}

type appDeviceView struct {
	DeviceID           string  `json:"deviceId"`
	DeviceModel        string  `json:"deviceModel"`
	UserID             string  `json:"userId"`
	UserName           string  `json:"userName"`
	Platform           string  `json:"platform"`
	Android            bool    `json:"android"`
	Version            string  `json:"version"`
	Managed            bool    `json:"managed"`
	LastConnection     *string `json:"lastConnection"`
	Freshness          string  `json:"freshness"`
	UninstallRequested bool    `json:"uninstallRequested"`
}

const platformAndroid = "A"

func (s *server) appPackage(c fiber.Ctx) (string, error) {
	pkg := c.Params("package")
	if !knox.ValidPackage(pkg) {
		return "", badRequest("invalid package")
	}
	return pkg, nil
}

func (s *server) listApps(c fiber.Ctx) error {
	search, category := c.Query("search"), c.Query("category")
	if err := checkText("search", search, maxTextLen, false); err != nil {
		return err
	}
	if err := checkText("category", category, maxTextLen, false); err != nil {
		return err
	}
	_, orgs, err := s.scopeOrgs(c)
	if err != nil {
		return err
	}
	apps, err := s.Apps.Apps(c.Context(), orgs, db.AppFilter{Search: search, Category: category, ShowSystem: c.Query("system") == "true"})
	if err != nil {
		return err
	}
	out := make([]appSummaryView, len(apps))
	for i, a := range apps {
		out[i] = appSummaryOf(a)
	}
	return c.JSON(fiber.Map{"apps": out})
}

func (s *server) getApp(c fiber.Ctx) error {
	pkg, err := s.appPackage(c)
	if err != nil {
		return err
	}
	_, orgs, err := s.scopeOrgs(c)
	if err != nil {
		return err
	}
	app, err := s.Apps.App(c.Context(), orgs, pkg)
	if err != nil {
		return err
	}
	devices, err := s.Apps.AppDevices(c.Context(), orgs, pkg)
	if err != nil {
		return err
	}
	now := time.Now()
	out := make([]appDeviceView, len(devices))
	for i, d := range devices {
		last := parseConnection(d.LastConnection)
		v := appDeviceView{DeviceID: d.DeviceID, DeviceModel: d.DeviceModel, UserID: d.UserID, UserName: d.UserName,
			Platform: platformText[d.Platform], Android: d.Platform == platformAndroid, Version: d.Version, Managed: d.Managed,
			Freshness: deviceFreshness(knox.IsEnrolled(d.DeviceStatus), last, now), UninstallRequested: d.UninstallRequested}
		if !last.IsZero() {
			v.LastConnection = new(last.UTC().Format(time.RFC3339))
		}
		out[i] = v
	}
	return c.JSON(fiber.Map{"app": appSummaryOf(app), "devices": out})
}

type uninstallResult struct {
	DeviceID string `json:"deviceId"`
	OK       bool   `json:"ok"`
	Error    string `json:"error,omitempty"`
}

// uninstallOne asks one device to uninstall pkg. The returned message is safe to show the caller.
func (s *server) uninstallOne(c fiber.Ctx, sc auth.Scope, id, pkg string) string {
	d, err := s.scopedDevice(c, sc, id)
	if err == nil {
		// Reading the device is not enough; the caller needs write access to its org.
		org := d.OrgCode
		if d.UserID == "" {
			org = ""
		}
		if sc.CheckWriteOrg(org) != nil {
			err = errDeviceNotFound
		}
	}
	if err != nil {
		if errors.Is(err, errDeviceNotFound) {
			return "device not found"
		}
		s.Log.Error().Err(err).Str("deviceId", id).Msg("app uninstall: device lookup failed")
		return "internal error"
	}
	if d.Platform != platformAndroid {
		return "not an Android device"
	}
	rows, err := s.Apps.DeviceApps(c.Context(), id)
	if err != nil {
		s.Log.Error().Err(err).Str("deviceId", id).Msg("app uninstall: reading inventory failed")
		return "internal error"
	}
	var row *db.DeviceApp
	for i := range rows {
		if rows[i].Package == pkg {
			row = &rows[i]
		}
	}
	if row == nil {
		return "app not installed on this device"
	}
	if reason := protectedReason(pkg, row.Category, row.System, row.Managed, row.Mandatory); reason != "" {
		return reason
	}
	if err := s.Knox().UninstallApp(c.Context(), id, pkg, row.KnoxID); err != nil {
		s.Log.Warn().Err(err).Str("deviceId", id).Str("package", pkg).Msg("app uninstall refused")
		if errors.Is(err, knox.ErrUnreachable) {
			return "Knox could not be reached"
		}
		return "Knox rejected the request"
	}
	s.mirrorWrite("mark uninstall requested", s.Apps.MarkUninstallRequested(c.Context(), id, pkg, time.Now().UTC()))
	s.record(c, "device.app.uninstall", id, fiber.Map{"package": pkg, "version": row.Version, "managed": row.Managed}, fiber.Map{"package": pkg})
	return ""
}

func (s *server) uninstallApp(c fiber.Ctx) error {
	pkg, err := s.appPackage(c)
	if err != nil {
		return err
	}
	var body struct {
		DeviceIDs []string `json:"deviceIds"`
	}
	if err := bind(c, &body); err != nil {
		return err
	}
	ids, err := checkNamedIDs("deviceId", body.DeviceIDs)
	if err != nil {
		return err
	}
	sc, orgs, err := s.scopeOrgs(c)
	if err != nil {
		return err
	}
	app, err := s.Apps.App(c.Context(), orgs, pkg)
	if err != nil {
		return err
	}
	if app.System {
		return badRequest("system apps cannot be uninstalled")
	}
	if reason := protectedReason(pkg, app.Category, app.System, app.Managed, app.Mandatory); reason != "" {
		return errAppProtected(reason)
	}
	results := make([]uninstallResult, len(ids))
	for i, id := range ids {
		msg := s.uninstallOne(c, sc, id, pkg)
		results[i] = uninstallResult{DeviceID: id, OK: msg == "", Error: msg}
	}
	return c.JSON(fiber.Map{"results": results})
}

type deviceAppView struct {
	Package            string  `json:"package"`
	Name               string  `json:"name"`
	Version            string  `json:"version"`
	System             bool    `json:"system"`
	Managed            bool    `json:"managed"`
	Mandatory          bool    `json:"mandatory"`
	InstalledAt        *string `json:"installedAt"`
	Category           string  `json:"category"`
	CategoryID         *int64  `json:"categoryId"`
	UninstallRequested bool    `json:"uninstallRequested"`
	Protected          bool    `json:"protected"`
	ProtectedReason    string  `json:"protectedReason"`
}

func (s *server) deviceApps(c fiber.Ctx) error {
	d, err := s.inScopeDevice(c)
	if err != nil {
		return err
	}
	android := d.Platform == platformAndroid
	var syncedAt *time.Time
	stale := false
	if android {
		if perr := s.Inventory.PullDevice(c.Context(), d.DeviceID); perr != nil {
			// The mirror still answers; the screen says how old it is.
			s.Log.Warn().Err(perr).Str("deviceId", d.DeviceID).Msg("app inventory pull failed, serving mirror")
			stale = true
		} else {
			syncedAt = new(time.Now().UTC())
		}
	}
	if syncedAt == nil {
		st, err := s.Apps.Inventory(c.Context())
		if err != nil {
			return err
		}
		syncedAt = st.LastPullAt
	}
	rows, err := s.Apps.DeviceApps(c.Context(), d.DeviceID)
	if err != nil {
		return err
	}
	apps := make([]deviceAppView, len(rows))
	for i, r := range rows {
		reason := protectedReason(r.Package, r.Category, r.System, r.Managed, r.Mandatory)
		apps[i] = deviceAppView{r.Package, r.Name, r.Version, r.System, r.Managed, r.Mandatory, r.InstalledAt, r.Category, r.CategoryID,
			r.UninstallRequestedAt != nil, reason != "", reason}
	}
	var at *string
	if syncedAt != nil {
		at = new(syncedAt.UTC().Format(time.RFC3339))
	}
	return c.JSON(fiber.Map{"apps": apps, "syncedAt": at, "stale": stale, "android": android})
}

func (s *server) refreshDeviceApps(c fiber.Ctx) error {
	d, err := s.inScopeDevice(c)
	if err != nil {
		return err
	}
	sc, err := s.scope(c)
	if err != nil {
		return err
	}
	org := d.OrgCode
	if d.UserID == "" {
		org = ""
	}
	if err := sc.CheckWriteOrg(org); err != nil {
		if errors.Is(err, auth.ErrForbidden) {
			return errDeviceNotFound
		}
		return err
	}
	if d.Platform != platformAndroid {
		return badRequest("only Android devices report their apps")
	}
	if err := s.Knox().SyncInstalledAppList(c.Context(), d.DeviceID); err != nil {
		return err
	}
	s.record(c, "device.apps.refresh", d.DeviceID, nil, nil)
	return c.Status(fiber.StatusAccepted).JSON(fiber.Map{"requested": true})
}

type categoryView struct {
	ID       int64  `json:"id"`
	Name     string `json:"name"`
	AppCount int    `json:"appCount"`
	BuiltIn  bool   `json:"builtIn"`
}

func (s *server) categories(c fiber.Ctx) ([]categoryView, error) {
	rows, err := s.Apps.Categories(c.Context())
	if err != nil {
		return nil, err
	}
	out := make([]categoryView, len(rows))
	for i, r := range rows {
		out[i] = categoryView{r.ID, r.Name, r.AppCount, r.IsBuiltIn}
	}
	return out, nil
}

func (s *server) category(c fiber.Ctx, id int64) (categoryView, error) {
	all, err := s.categories(c)
	if err != nil {
		return categoryView{}, err
	}
	for _, v := range all {
		if v.ID == id {
			return v, nil
		}
	}
	return categoryView{}, db.ErrNotFound
}

func (s *server) listAppCategories(c fiber.Ctx) error {
	out, err := s.categories(c)
	if err != nil {
		return err
	}
	// Counts span every device in the tenant, so only admins may see them.
	if !principalFrom(c).IsAdmin() {
		for i := range out {
			out[i].AppCount = 0
		}
	}
	return c.JSON(fiber.Map{"categories": out})
}

func categoryErr(err error) error {
	if errors.Is(err, db.ErrBuiltInCategory) {
		return &fiberError{fiber.StatusBadRequest, "the Other category is built in and cannot be changed", "category_builtin"}
	}
	return err
}

func categoryID(c fiber.Ctx) (int64, error) {
	id, err := strconv.ParseInt(c.Params("id"), 10, 64)
	if err != nil || id <= 0 {
		return 0, badRequest("invalid category id")
	}
	return id, nil
}

func categoryNameBody(c fiber.Ctx) (string, error) {
	var body struct {
		Name string `json:"name"`
	}
	if err := bind(c, &body); err != nil {
		return "", err
	}
	return body.Name, checkText("name", body.Name, maxTextLen, true)
}

func (s *server) createAppCategory(c fiber.Ctx) error {
	name, err := categoryNameBody(c)
	if err != nil {
		return err
	}
	id, err := s.Apps.CreateCategory(c.Context(), name)
	if err != nil {
		return err
	}
	v, err := s.category(c, id)
	if err != nil {
		return err
	}
	s.record(c, "app.category.create", strconv.FormatInt(id, 10), nil, fiber.Map{"name": v.Name})
	return c.Status(fiber.StatusCreated).JSON(v)
}

func (s *server) renameAppCategory(c fiber.Ctx) error {
	id, err := categoryID(c)
	if err != nil {
		return err
	}
	name, err := categoryNameBody(c)
	if err != nil {
		return err
	}
	before, err := s.category(c, id)
	if err != nil {
		return err
	}
	if err := s.Apps.RenameCategory(c.Context(), id, name); err != nil {
		return categoryErr(err)
	}
	after, err := s.category(c, id)
	if err != nil {
		return err
	}
	s.record(c, "app.category.rename", strconv.FormatInt(id, 10), fiber.Map{"name": before.Name}, fiber.Map{"name": after.Name})
	return c.JSON(after)
}

func (s *server) deleteAppCategory(c fiber.Ctx) error {
	id, err := categoryID(c)
	if err != nil {
		return err
	}
	before, err := s.category(c, id)
	if err != nil {
		return err
	}
	if err := s.Apps.DeleteCategory(c.Context(), id); err != nil {
		return categoryErr(err)
	}
	s.record(c, "app.category.delete", strconv.FormatInt(id, 10), fiber.Map{"name": before.Name, "appCount": before.AppCount}, nil)
	return c.SendStatus(fiber.StatusNoContent)
}

func (s *server) setAppCategory(c fiber.Ctx) error {
	pkg, err := s.appPackage(c)
	if err != nil {
		return err
	}
	var body struct {
		CategoryID *int64 `json:"categoryId"`
	}
	if err := bind(c, &body); err != nil {
		return err
	}
	if body.CategoryID != nil && *body.CategoryID <= 0 {
		return badRequest("invalid categoryId")
	}
	prev, err := s.Apps.SetAppCategory(c.Context(), pkg, body.CategoryID)
	if err != nil {
		return err
	}
	s.record(c, "app.category.assign", pkg, fiber.Map{"categoryId": prev}, fiber.Map{"categoryId": body.CategoryID})
	return c.SendStatus(fiber.StatusNoContent)
}
