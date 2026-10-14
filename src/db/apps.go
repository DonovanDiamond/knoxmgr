package db

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/jmoiron/sqlx"
)

const (
	// SystemCategory is derived from the app's system flag and cannot be created or assigned.
	SystemCategory = "System"
	otherCategory  = "Other"
	maxCategoryLen = 100
)

// ErrInvalidCategoryName is returned for an empty, over-long or reserved category name.
var ErrInvalidCategoryName = errors.New("invalid category name")

// ErrBuiltInCategory is returned when renaming or deleting "Other", which unmapped apps fall into.
var ErrBuiltInCategory = errors.New("built-in category cannot be changed")

// DeviceAppRow is one installed app as reported by Knox for a device.
type DeviceAppRow struct {
	Package     string  `db:"package"`
	Name        string  `db:"name"`
	Version     string  `db:"version"`
	System      bool    `db:"system"`
	Managed     bool    `db:"managed"`
	Mandatory   bool    `db:"mandatory"`
	InstalledAt *string `db:"installed_at"`
	KnoxID      string  `db:"knox_id"`
}

// DeviceApp is a DeviceAppRow with its resolved category. CategoryID is the mapped category, if any,
// even for system apps (whose Category is always "System").
type DeviceApp struct {
	DeviceAppRow
	Category             string  `db:"category"`
	CategoryID           *int64  `db:"category_id"`
	UninstallRequestedAt *string `db:"uninstall_requested_at"`
}

type AppFilter struct {
	Search     string // case-insensitive substring of name or package
	Category   string // resolved category name; empty means any
	ShowSystem bool
}

type AppSummary struct {
	Package     string `db:"package"`
	Name        string `db:"name"`
	System      bool   `db:"system"`
	Category    string `db:"category"`
	CategoryID  *int64 `db:"category_id"`
	DeviceCount int    `db:"device_count"`
	Managed     bool   `db:"managed"`   // on at least one in-scope device
	Mandatory   bool   `db:"mandatory"` // on at least one in-scope device
	// AlternateNames are the other distinct non-empty names devices report for the package; Name is
	// the most common one.
	AlternateNames []string `db:"-"`
}

// AppDevice is one in-scope device that has an app installed.
type AppDevice struct {
	DeviceID           string `db:"device_id"`
	DeviceModel        string `db:"device_model"`
	UserID             string `db:"user_id"`
	UserName           string `db:"user_name"`
	Platform           string `db:"platform"`
	OrgCode            string `db:"org_code"`
	DeviceStatus       string `db:"device_status"`
	LastConnection     string `db:"last_connection"`
	Version            string `db:"version"`
	Managed            bool   `db:"managed"`
	Mandatory          bool   `db:"mandatory"`
	UninstallRequested bool   `db:"uninstall_requested"`
}

type Category struct {
	ID        int64  `db:"id"`
	Name      string `db:"name"`
	AppCount  int    `db:"app_count"`
	IsBuiltIn bool   `db:"-"`
}

// InventoryState records when the inventory was last pulled and last asked to report.
type InventoryState struct {
	LastPullAt          *time.Time
	LastReportRequestAt *time.Time
}

// Apps stores the per-device app inventory and the package categories.
//
// Scope arguments named orgs follow Mirror: nil means unrestricted, an empty non-nil slice means
// nothing, and a device is scoped by its owner's org.
type Apps struct{ db *sqlx.DB }

func NewApps(d *sqlx.DB) *Apps { return &Apps{db: d} }

func b2i(b bool) int {
	if b {
		return 1
	}
	return 0
}

// ReplaceDeviceApps makes rows the device's app list in one transaction; this also clears any
// pending uninstall flag.
func (a *Apps) ReplaceDeviceApps(ctx context.Context, deviceID string, rows []DeviceAppRow, at time.Time) error {
	tx, err := a.db.BeginTxx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, tx.Rebind(`DELETE FROM device_apps WHERE device_id = ?`), deviceID); err != nil {
		return fmt.Errorf("clearing apps of %s: %w", deviceID, err)
	}
	ts := formatTime(at)
	for _, r := range rows {
		if _, err := tx.ExecContext(ctx, tx.Rebind(`INSERT INTO device_apps
			(device_id, package, name, version, system, managed, mandatory, installed_at, knox_id, synced_at)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
			ON CONFLICT (device_id, package) DO UPDATE SET name = excluded.name, version = excluded.version,
				system = excluded.system, managed = excluded.managed, mandatory = excluded.mandatory, installed_at = excluded.installed_at,
				knox_id = excluded.knox_id, synced_at = excluded.synced_at`),
			deviceID, r.Package, r.Name, r.Version, b2i(r.System), b2i(r.Managed), b2i(r.Mandatory), r.InstalledAt, r.KnoxID, ts); err != nil {
			return fmt.Errorf("inserting app %q: %w", r.Package, err)
		}
	}
	return tx.Commit()
}

func (a *Apps) DeviceApps(ctx context.Context, deviceID string) ([]DeviceApp, error) {
	out := []DeviceApp{}
	err := a.db.SelectContext(ctx, &out, a.db.Rebind(`SELECT a.package, a.name, a.version, a.system, a.managed, a.mandatory, a.installed_at, a.knox_id,
			CASE WHEN a.system = 1 THEN '`+SystemCategory+`' ELSE COALESCE(c.name, '`+otherCategory+`') END AS category,
			m.category_id, a.uninstall_requested_at
		FROM device_apps a
		LEFT JOIN app_category_map m ON m.package = a.package
		LEFT JOIN app_categories c ON c.id = m.category_id
		WHERE a.device_id = ? ORDER BY a.name, a.package`), deviceID)
	return out, err
}

// scoped appends the org restriction to a query that already has a WHERE clause and reads
// devices as d and their owners as u. ok is false when the scope admits nothing.
func scoped(query string, args []any, orgs []string) (q string, a []any, ok bool, err error) {
	if orgs == nil {
		return query, args, true, nil
	}
	if len(orgs) == 0 {
		return "", nil, false, nil
	}
	q, a, err = sqlx.In(query+" AND u.org_code IN (?)", append(args, orgs)...)
	return q, a, err == nil, err
}

const appScopeFrom = `FROM device_apps a
	JOIN knox_devices d ON d.device_id = a.device_id
	LEFT JOIN knox_users u ON u.user_id = d.user_id`

func (a *Apps) summaries(ctx context.Context, orgs []string, pkg string) ([]AppSummary, error) {
	out := []AppSummary{}
	query, args := `SELECT a.package, '' AS name, MAX(a.system) AS system,
			CASE WHEN MAX(a.system) = 1 THEN '`+SystemCategory+`' ELSE COALESCE(c.name, '`+otherCategory+`') END AS category,
			m.category_id AS category_id, COUNT(*) AS device_count,
			MAX(a.managed) AS managed, MAX(a.mandatory) AS mandatory
		`+appScopeFrom+`
		LEFT JOIN app_category_map m ON m.package = a.package
		LEFT JOIN app_categories c ON c.id = m.category_id
		WHERE 1=1`, []any(nil)
	if pkg != "" {
		query += " AND a.package = ?"
		args = append(args, pkg)
	}
	q, args, ok, err := scoped(query, args, orgs)
	if err != nil || !ok {
		return out, err
	}
	err = a.db.SelectContext(ctx, &out, a.db.Rebind(q+" GROUP BY a.package, m.category_id, c.name"), args...)
	if err != nil {
		return out, err
	}
	names, err := a.appNames(ctx, orgs, pkg)
	if err != nil {
		return out, err
	}
	for i := range out {
		out[i].Name, out[i].AlternateNames = pickNames(names[out[i].Package])
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Name != out[j].Name {
			return out[i].Name < out[j].Name
		}
		return out[i].Package < out[j].Package
	})
	return out, nil
}

type nameCount struct {
	Package string `db:"package"`
	Name    string `db:"name"`
	N       int    `db:"n"`
}

// appNames counts, per package, how many in-scope devices report each non-empty name.
func (a *Apps) appNames(ctx context.Context, orgs []string, pkg string) (map[string][]nameCount, error) {
	query, args := "SELECT a.package, a.name, COUNT(*) AS n "+appScopeFrom+" WHERE a.name <> ''", []any(nil)
	if pkg != "" {
		query += " AND a.package = ?"
		args = append(args, pkg)
	}
	q, args, ok, err := scoped(query, args, orgs)
	if err != nil || !ok {
		return nil, err
	}
	var rows []nameCount
	if err := a.db.SelectContext(ctx, &rows, a.db.Rebind(q+" GROUP BY a.package, a.name"), args...); err != nil {
		return nil, err
	}
	out := map[string][]nameCount{}
	for _, r := range rows {
		out[r.Package] = append(out[r.Package], r)
	}
	return out, nil
}

// pickNames returns the most common name (ties: alphabetically first) and the rest ordered the same way.
func pickNames(counts []nameCount) (string, []string) {
	sort.Slice(counts, func(i, j int) bool {
		if counts[i].N != counts[j].N {
			return counts[i].N > counts[j].N
		}
		return counts[i].Name < counts[j].Name
	})
	alts := []string{}
	for _, c := range counts[min(1, len(counts)):] {
		alts = append(alts, c.Name)
	}
	if len(counts) == 0 {
		return "", alts
	}
	return counts[0].Name, alts
}

func (a *Apps) Apps(ctx context.Context, orgs []string, f AppFilter) ([]AppSummary, error) {
	all, err := a.summaries(ctx, orgs, "")
	if err != nil {
		return nil, err
	}
	search := strings.ToLower(strings.TrimSpace(f.Search))
	out := make([]AppSummary, 0, len(all))
	for _, s := range all {
		if s.System && !f.ShowSystem {
			continue
		}
		if f.Category != "" && s.Category != f.Category {
			continue
		}
		if search != "" && !strings.Contains(strings.ToLower(s.Name), search) && !strings.Contains(strings.ToLower(s.Package), search) {
			continue
		}
		out = append(out, s)
	}
	return out, nil
}

// App returns one app's summary, or ErrNotFound when no in-scope device has it.
func (a *Apps) App(ctx context.Context, orgs []string, pkg string) (AppSummary, error) {
	if pkg == "" {
		return AppSummary{}, ErrNotFound
	}
	s, err := a.summaries(ctx, orgs, pkg)
	if err != nil {
		return AppSummary{}, err
	}
	if len(s) == 0 {
		return AppSummary{}, ErrNotFound
	}
	return s[0], nil
}

func (a *Apps) AppDevices(ctx context.Context, orgs []string, pkg string) ([]AppDevice, error) {
	out := []AppDevice{}
	q, args, ok, err := scoped(`SELECT d.device_id, d.device_model, d.user_id, d.user_name, d.platform,
			COALESCE(u.org_code, d.org_code) AS org_code, d.device_status, d.last_connection, a.version, a.managed, a.mandatory,
			(a.uninstall_requested_at IS NOT NULL) AS uninstall_requested
		`+appScopeFrom+` WHERE a.package = ?`, []any{pkg}, orgs)
	if err != nil || !ok {
		return out, err
	}
	err = a.db.SelectContext(ctx, &out, a.db.Rebind(q+" ORDER BY d.device_model, d.device_id"), args...)
	return out, err
}

func (a *Apps) MarkUninstallRequested(ctx context.Context, deviceID, pkg string, at time.Time) error {
	res, err := a.db.ExecContext(ctx, a.db.Rebind(`UPDATE device_apps SET uninstall_requested_at = ? WHERE device_id = ? AND package = ?`),
		formatTime(at), deviceID, pkg)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

// Categories lists every category with the number of distinct installed non-system packages that
// resolve to it, counted from the same summaries as the Apps list (unrestricted scope) so the two agree.
func (a *Apps) Categories(ctx context.Context) ([]Category, error) {
	out := []Category{}
	if err := a.db.SelectContext(ctx, &out, `SELECT id, name FROM app_categories ORDER BY name`); err != nil {
		return nil, err
	}
	sums, err := a.summaries(ctx, nil, "")
	if err != nil {
		return nil, err
	}
	counts := map[string]int{}
	for _, s := range sums {
		if !s.System {
			counts[s.Category]++
		}
	}
	for i := range out {
		out[i].AppCount = counts[out[i].Name]
		out[i].IsBuiltIn = out[i].Name == otherCategory
	}
	return out, nil
}

// guardBuiltIn returns ErrNotFound for an unknown id and ErrBuiltInCategory for "Other".
func (a *Apps) guardBuiltIn(ctx context.Context, id int64) error {
	var names []string
	if err := a.db.SelectContext(ctx, &names, a.db.Rebind(`SELECT name FROM app_categories WHERE id = ?`), id); err != nil {
		return err
	}
	switch {
	case len(names) == 0:
		return ErrNotFound
	case names[0] == otherCategory:
		return ErrBuiltInCategory
	}
	return nil
}

func categoryName(name string) (string, error) {
	name = strings.TrimSpace(name)
	switch {
	case name == "":
		return "", fmt.Errorf("%w: name is required", ErrInvalidCategoryName)
	case len(name) > maxCategoryLen:
		return "", fmt.Errorf("%w: longer than %d bytes", ErrInvalidCategoryName, maxCategoryLen)
	case strings.EqualFold(name, SystemCategory):
		return "", fmt.Errorf("%w: %q is reserved", ErrInvalidCategoryName, SystemCategory)
	case strings.EqualFold(name, otherCategory):
		return "", fmt.Errorf("%w: %q is reserved", ErrInvalidCategoryName, otherCategory)
	}
	return name, nil
}

func (a *Apps) CreateCategory(ctx context.Context, name string) (int64, error) {
	name, err := categoryName(name)
	if err != nil {
		return 0, err
	}
	var id int64
	err = a.db.GetContext(ctx, &id, a.db.Rebind(`INSERT INTO app_categories (name) VALUES (?) RETURNING id`), name)
	if isUniqueViolation(err) {
		return 0, fmt.Errorf("creating category %q: %w", name, ErrDuplicate)
	}
	return id, err
}

func (a *Apps) RenameCategory(ctx context.Context, id int64, name string) error {
	name, err := categoryName(name)
	if err != nil {
		return err
	}
	if err := a.guardBuiltIn(ctx, id); err != nil {
		return err
	}
	res, err := a.db.ExecContext(ctx, a.db.Rebind(`UPDATE app_categories SET name = ? WHERE id = ?`), name, id)
	if isUniqueViolation(err) {
		return fmt.Errorf("renaming category %d: %w", id, ErrDuplicate)
	}
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

// DeleteCategory removes the category and, by cascade, its package mappings.
func (a *Apps) DeleteCategory(ctx context.Context, id int64) error {
	if err := a.guardBuiltIn(ctx, id); err != nil {
		return err
	}
	res, err := a.db.ExecContext(ctx, a.db.Rebind(`DELETE FROM app_categories WHERE id = ?`), id)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

// SetAppCategory maps pkg to categoryID (nil clears the mapping) and returns the previous category id.
func (a *Apps) SetAppCategory(ctx context.Context, pkg string, categoryID *int64) (*int64, error) {
	tx, err := a.db.BeginTxx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	var prev []int64
	if err := tx.SelectContext(ctx, &prev, tx.Rebind(`SELECT category_id FROM app_category_map WHERE package = ?`), pkg); err != nil {
		return nil, err
	}
	var before *int64
	if len(prev) > 0 {
		before = &prev[0]
	}
	if categoryID == nil {
		_, err = tx.ExecContext(ctx, tx.Rebind(`DELETE FROM app_category_map WHERE package = ?`), pkg)
	} else {
		var n int
		if err := tx.GetContext(ctx, &n, tx.Rebind(`SELECT COUNT(*) FROM app_categories WHERE id = ?`), *categoryID); err != nil {
			return nil, err
		}
		if n == 0 {
			return nil, ErrNotFound
		}
		_, err = tx.ExecContext(ctx, tx.Rebind(`INSERT INTO app_category_map (package, category_id) VALUES (?, ?)
			ON CONFLICT (package) DO UPDATE SET category_id = excluded.category_id`), pkg, *categoryID)
	}
	if err != nil {
		return nil, err
	}
	return before, tx.Commit()
}

func (a *Apps) Inventory(ctx context.Context) (InventoryState, error) {
	var row struct {
		Pull   *string `db:"last_pull_at"`
		Report *string `db:"last_report_request_at"`
	}
	if err := a.db.GetContext(ctx, &row, `SELECT last_pull_at, last_report_request_at FROM app_inventory WHERE id = 1`); err != nil {
		return InventoryState{}, fmt.Errorf("loading app inventory state: %w", err)
	}
	var s InventoryState
	var err error
	if s.LastPullAt, err = parseOptTime(row.Pull); err != nil {
		return s, err
	}
	s.LastReportRequestAt, err = parseOptTime(row.Report)
	return s, err
}

func parseOptTime(s *string) (*time.Time, error) {
	if s == nil {
		return nil, nil
	}
	t, err := time.Parse(timeLayout, *s)
	return &t, err
}

func (a *Apps) SetLastPull(ctx context.Context, at time.Time) error {
	_, err := a.db.ExecContext(ctx, a.db.Rebind(`UPDATE app_inventory SET last_pull_at = ? WHERE id = 1`), formatTime(at))
	return err
}

func (a *Apps) SetLastReportRequest(ctx context.Context, at time.Time) error {
	_, err := a.db.ExecContext(ctx, a.db.Rebind(`UPDATE app_inventory SET last_report_request_at = ? WHERE id = 1`), formatTime(at))
	return err
}
