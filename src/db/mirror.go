package db

import (
	"context"
	"fmt"
	"time"

	"github.com/jmoiron/sqlx"
)

// The Mirror* types use plain fields named after Knox's so this package imports no other internal one.

type MirrorOrg struct {
	OrgCode    string `db:"org_code"`
	OrgName    string `db:"org_name"`
	ParentCode string `db:"parent_code"`
}

type MirrorUser struct {
	UserID      string `db:"user_id"`
	UserName    string `db:"user_name"`
	Email       string `db:"email"`
	FirstName   string `db:"first_name"`
	LastName    string `db:"last_name"`
	MobilePhone string `db:"mobile_phone"`
	Contact     string `db:"contact"`
	EmpNo       string `db:"emp_no"`
	OrgCode     string `db:"org_code"`
	OrgName     string `db:"org_name"`
	Enabled     bool   `db:"enabled"`
	UserType    string `db:"user_type"`
	DeviceCount int    `db:"device_count"`
}

// MirrorGroup.UserCount is derived from the membership rows, never taken from the input.
type MirrorGroup struct {
	GroupID   string `db:"group_id"`
	GroupName string `db:"group_name"`
	GroupType string `db:"group_type"`
	UserCount int    `db:"user_count"`
	// Assigned app and profile counts as reported by Knox.
	AppCount     int `db:"app_count"`
	ProfileCount int `db:"profile_count"`
}

type MirrorMember struct {
	GroupID string `db:"group_id"`
	UserID  string `db:"user_id"`
}

// MirrorDevice.OrgCode is the owner user's org when the owner is mirrored.
type MirrorDevice struct {
	DeviceID          string `db:"device_id"`
	DeviceModel       string `db:"device_model"`
	DeviceModelKind   string `db:"device_model_kind"`
	DeviceStatus      string `db:"device_status"`
	DeviceVersionName string `db:"device_version_name"`
	Platform          string `db:"platform"`
	UserID            string `db:"user_id"`
	UserName          string `db:"user_name"`
	OrgCode           string `db:"org_code"`
	LastConnection    string `db:"last_connection"`
}

type Snapshot struct {
	Orgs    []MirrorOrg
	Users   []MirrorUser
	Groups  []MirrorGroup
	Members []MirrorMember
	Devices []MirrorDevice
}

type SyncCounts struct {
	Orgs        int `db:"orgs"`
	Users       int `db:"users"`
	Groups      int `db:"groups"`
	Memberships int `db:"memberships"`
	Devices     int `db:"devices"`
}

type SyncRun struct {
	ID         int64   `db:"id"`
	Trigger    string  `db:"trigger"`
	StartedAt  string  `db:"started_at"`
	FinishedAt *string `db:"finished_at"`
	Status     string  `db:"status"`
	Error      string  `db:"error"`
	SyncCounts
}

// idBatch keeps IN lists far below the engines' bind-variable limits.
const idBatch = 500

// Mirror stores the local copy of the Knox tenant.
//
// Scope arguments named orgs: nil means unrestricted, an empty non-nil slice means nothing.
type Mirror struct{ db *sqlx.DB }

func NewMirror(d *sqlx.DB) *Mirror { return &Mirror{db: d} }

const (
	mirrorUserCols   = `u.user_id, u.user_name, u.email, u.first_name, u.last_name, u.mobile_phone, u.contact, u.emp_no, u.org_code, u.org_name, u.enabled, u.user_type, u.device_count`
	mirrorDeviceCols = `d.device_id, d.device_model, d.device_model_kind, d.device_status, d.device_version_name, d.platform, d.user_id, d.user_name, COALESCE(u.org_code, d.org_code) AS org_code, d.last_connection`
	mirrorDeviceFrom = `FROM knox_devices d LEFT JOIN knox_users u ON u.user_id = d.user_id`
)

// ReplaceAll makes the mirror equal to s in one transaction; rows absent from s are removed.
func (m *Mirror) ReplaceAll(ctx context.Context, s Snapshot, syncedAt time.Time) error {
	at := formatTime(syncedAt)
	tx, err := m.db.BeginTxx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	for _, t := range []string{"knox_group_members", "knox_devices", "knox_groups", "knox_users", "knox_orgs"} {
		if _, err := tx.ExecContext(ctx, `DELETE FROM `+t); err != nil {
			return fmt.Errorf("clearing %s: %w", t, err)
		}
	}
	for _, o := range s.Orgs {
		if _, err := tx.ExecContext(ctx, tx.Rebind(`INSERT INTO knox_orgs (org_code, org_name, parent_code, synced_at) VALUES (?, ?, ?, ?)`),
			o.OrgCode, o.OrgName, o.ParentCode, at); err != nil {
			return fmt.Errorf("inserting org: %w", err)
		}
	}
	for _, u := range s.Users {
		if err := insertUser(ctx, tx, u, at); err != nil {
			return fmt.Errorf("inserting user: %w", err)
		}
	}
	for _, g := range s.Groups {
		if _, err := tx.ExecContext(ctx, tx.Rebind(`INSERT INTO knox_groups (group_id, group_name, group_type, app_count, profile_count, synced_at) VALUES (?, ?, ?, ?, ?, ?)`),
			g.GroupID, g.GroupName, g.GroupType, g.AppCount, g.ProfileCount, at); err != nil {
			return fmt.Errorf("inserting group: %w", err)
		}
	}
	for _, mb := range s.Members {
		if _, err := tx.ExecContext(ctx, tx.Rebind(`INSERT INTO knox_group_members (group_id, user_id, synced_at) VALUES (?, ?, ?) ON CONFLICT DO NOTHING`),
			mb.GroupID, mb.UserID, at); err != nil {
			return fmt.Errorf("inserting membership: %w", err)
		}
	}
	for _, d := range s.Devices {
		if _, err := tx.ExecContext(ctx, tx.Rebind(`INSERT INTO knox_devices
			(device_id, device_model, device_model_kind, device_status, device_version_name, platform, user_id, user_name, org_code, last_connection, synced_at)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`),
			d.DeviceID, d.DeviceModel, d.DeviceModelKind, d.DeviceStatus, d.DeviceVersionName, d.Platform, d.UserID, d.UserName, d.OrgCode, d.LastConnection, at); err != nil {
			return fmt.Errorf("inserting device: %w", err)
		}
	}
	// Cached live data (locations especially) must not outlive its device.
	if _, err := tx.ExecContext(ctx, `DELETE FROM knox_device_cache WHERE device_id NOT IN (SELECT device_id FROM knox_devices)`); err != nil {
		return fmt.Errorf("pruning device cache: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM device_apps WHERE device_id NOT IN (SELECT device_id FROM knox_devices)`); err != nil {
		return fmt.Errorf("pruning device apps: %w", err)
	}
	if err := recountGroups(ctx, tx); err != nil {
		return err
	}
	return tx.Commit()
}

func insertUser(ctx context.Context, tx *sqlx.Tx, u MirrorUser, at string) error {
	_, err := tx.ExecContext(ctx, tx.Rebind(`INSERT INTO knox_users
		(user_id, user_name, email, first_name, last_name, mobile_phone, contact, emp_no, org_code, org_name, enabled, user_type, device_count, synced_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT (user_id) DO UPDATE SET user_name = excluded.user_name, email = excluded.email, first_name = excluded.first_name,
			last_name = excluded.last_name, mobile_phone = excluded.mobile_phone, contact = excluded.contact, emp_no = excluded.emp_no,
			org_code = excluded.org_code, org_name = excluded.org_name, enabled = excluded.enabled, user_type = excluded.user_type,
			device_count = excluded.device_count, synced_at = excluded.synced_at`),
		u.UserID, u.UserName, u.Email, u.FirstName, u.LastName, u.MobilePhone, u.Contact, u.EmpNo, u.OrgCode, u.OrgName, u.Enabled, u.UserType, u.DeviceCount, at)
	return err
}

func recountGroups(ctx context.Context, tx *sqlx.Tx) error {
	_, err := tx.ExecContext(ctx, `UPDATE knox_groups SET user_count =
		(SELECT COUNT(*) FROM knox_group_members m WHERE m.group_id = knox_groups.group_id)`)
	return err
}

// selectScoped runs query (which must already contain a WHERE clause) restricted to orgCol IN orgs.
func (m *Mirror) selectScoped(ctx context.Context, dest any, query string, args []any, orgCol, order string, orgs []string) error {
	if orgs == nil {
		return m.db.SelectContext(ctx, dest, m.db.Rebind(query+" ORDER BY "+order), args...)
	}
	if len(orgs) == 0 {
		return nil
	}
	q, a, err := sqlx.In(query+" AND "+orgCol+" IN (?) ORDER BY "+order, append(args, orgs)...)
	if err != nil {
		return err
	}
	return m.db.SelectContext(ctx, dest, m.db.Rebind(q), a...)
}

func (m *Mirror) Orgs(ctx context.Context) ([]MirrorOrg, error) {
	out := []MirrorOrg{}
	err := m.db.SelectContext(ctx, &out, `SELECT org_code, org_name, parent_code FROM knox_orgs ORDER BY org_name, org_code`)
	return out, err
}

// SubOrgCodes returns code and all its descendants; empty if code is unknown.
func (m *Mirror) SubOrgCodes(ctx context.Context, code string) ([]string, error) {
	out := []string{}
	err := m.db.SelectContext(ctx, &out, m.db.Rebind(`WITH RECURSIVE tree(c) AS (
		SELECT org_code FROM knox_orgs WHERE org_code = ?
		UNION
		SELECT o.org_code FROM knox_orgs o JOIN tree t ON o.parent_code = t.c)
		SELECT c FROM tree ORDER BY c`), code)
	return out, err
}

// systemUserID is Knox's built-in "Android Management User"; it is hidden from the
// user list but stays readable by id (device owner links).
const systemUserID = "_AManagement"

func (m *Mirror) Users(ctx context.Context, orgs []string) ([]MirrorUser, error) {
	out := []MirrorUser{}
	err := m.selectScoped(ctx, &out, `SELECT `+mirrorUserCols+` FROM knox_users u WHERE u.user_id <> ?`, []any{systemUserID}, "u.org_code", "u.user_name, u.user_id", orgs)
	return out, err
}

func (m *Mirror) User(ctx context.Context, id string) (MirrorUser, error) {
	var u MirrorUser
	err := m.db.GetContext(ctx, &u, m.db.Rebind(`SELECT `+mirrorUserCols+` FROM knox_users u WHERE u.user_id = ?`), id)
	return u, notFound(err)
}

func (m *Mirror) Groups(ctx context.Context) ([]MirrorGroup, error) {
	out := []MirrorGroup{}
	err := m.db.SelectContext(ctx, &out, `SELECT group_id, group_name, group_type, user_count, app_count, profile_count FROM knox_groups ORDER BY group_name, group_id`)
	return out, err
}

func (m *Mirror) Group(ctx context.Context, id string) (MirrorGroup, error) {
	var g MirrorGroup
	err := m.db.GetContext(ctx, &g, m.db.Rebind(`SELECT group_id, group_name, group_type, user_count, app_count, profile_count FROM knox_groups WHERE group_id = ?`), id)
	return g, notFound(err)
}

// GroupMembers returns the members of a group that are inside orgs.
func (m *Mirror) GroupMembers(ctx context.Context, groupID string, orgs []string) ([]MirrorUser, error) {
	out := []MirrorUser{}
	err := m.selectScoped(ctx, &out, `SELECT `+mirrorUserCols+` FROM knox_users u
		JOIN knox_group_members gm ON gm.user_id = u.user_id WHERE gm.group_id = ?`, []any{groupID}, "u.org_code", "u.user_name, u.user_id", orgs)
	return out, err
}

func (m *Mirror) UserGroups(ctx context.Context, userID string) ([]MirrorGroup, error) {
	out := []MirrorGroup{}
	err := m.db.SelectContext(ctx, &out, m.db.Rebind(`SELECT g.group_id, g.group_name, g.group_type, g.user_count, g.app_count, g.profile_count FROM knox_groups g
		JOIN knox_group_members gm ON gm.group_id = g.group_id WHERE gm.user_id = ? ORDER BY g.group_name, g.group_id`), userID)
	return out, err
}

// Devices are scoped by their owner's org; unassigned devices appear only when orgs is nil.
func (m *Mirror) Devices(ctx context.Context, orgs []string) ([]MirrorDevice, error) {
	out := []MirrorDevice{}
	err := m.selectScoped(ctx, &out, `SELECT `+mirrorDeviceCols+` `+mirrorDeviceFrom+` WHERE 1=1`, nil, "u.org_code", "d.device_model, d.device_id", orgs)
	return out, err
}

func (m *Mirror) Device(ctx context.Context, id string) (MirrorDevice, error) {
	var d MirrorDevice
	err := m.db.GetContext(ctx, &d, m.db.Rebind(`SELECT `+mirrorDeviceCols+` `+mirrorDeviceFrom+` WHERE d.device_id = ?`), id)
	return d, notFound(err)
}

func (m *Mirror) DevicesOfUsers(ctx context.Context, userIDs []string) ([]MirrorDevice, error) {
	out := []MirrorDevice{}
	for i := 0; i < len(userIDs); i += idBatch {
		batch := userIDs[i:min(i+idBatch, len(userIDs))]
		q, a, err := sqlx.In(`SELECT `+mirrorDeviceCols+` `+mirrorDeviceFrom+` WHERE d.user_id IN (?) ORDER BY d.device_model, d.device_id`, batch)
		if err != nil {
			return nil, err
		}
		var part []MirrorDevice
		if err := m.db.SelectContext(ctx, &part, m.db.Rebind(q), a...); err != nil {
			return nil, err
		}
		out = append(out, part...)
	}
	return out, nil
}

// Write-through: called after Knox accepted the change.

func (m *Mirror) UpsertUser(ctx context.Context, u MirrorUser) error {
	tx, err := m.db.BeginTxx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if err := insertUser(ctx, tx, u, now()); err != nil {
		return err
	}
	return tx.Commit()
}

func (m *Mirror) DeleteUser(ctx context.Context, id string) error {
	tx, err := m.db.BeginTxx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, tx.Rebind(`DELETE FROM knox_group_members WHERE user_id = ?`), id); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, tx.Rebind(`DELETE FROM knox_users WHERE user_id = ?`), id); err != nil {
		return err
	}
	if err := recountGroups(ctx, tx); err != nil {
		return err
	}
	return tx.Commit()
}

// UpsertGroup writes name and type; user_count is left to the membership rows and the
// app/profile counts to the sync, so an update never zeroes them (a new group starts at 0).
func (m *Mirror) UpsertGroup(ctx context.Context, g MirrorGroup) error {
	_, err := m.db.ExecContext(ctx, m.db.Rebind(`INSERT INTO knox_groups (group_id, group_name, group_type, synced_at) VALUES (?, ?, ?, ?)
		ON CONFLICT (group_id) DO UPDATE SET group_name = excluded.group_name, group_type = excluded.group_type, synced_at = excluded.synced_at`),
		g.GroupID, g.GroupName, g.GroupType, now())
	return err
}

func (m *Mirror) DeleteGroup(ctx context.Context, id string) error {
	tx, err := m.db.BeginTxx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, tx.Rebind(`DELETE FROM knox_group_members WHERE group_id = ?`), id); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, tx.Rebind(`DELETE FROM knox_groups WHERE group_id = ?`), id); err != nil {
		return err
	}
	return tx.Commit()
}

func (m *Mirror) AddMembers(ctx context.Context, groupID string, userIDs []string) error {
	return m.changeMembers(ctx, groupID, func(tx *sqlx.Tx) error {
		at := now()
		for _, uid := range userIDs {
			if _, err := tx.ExecContext(ctx, tx.Rebind(`INSERT INTO knox_group_members (group_id, user_id, synced_at) VALUES (?, ?, ?) ON CONFLICT DO NOTHING`),
				groupID, uid, at); err != nil {
				return err
			}
		}
		return nil
	})
}

func (m *Mirror) RemoveMembers(ctx context.Context, groupID string, userIDs []string) error {
	return m.changeMembers(ctx, groupID, func(tx *sqlx.Tx) error {
		for i := 0; i < len(userIDs); i += idBatch {
			q, a, err := sqlx.In(`DELETE FROM knox_group_members WHERE group_id = ? AND user_id IN (?)`, groupID, userIDs[i:min(i+idBatch, len(userIDs))])
			if err != nil {
				return err
			}
			if _, err := tx.ExecContext(ctx, tx.Rebind(q), a...); err != nil {
				return err
			}
		}
		return nil
	})
}

func (m *Mirror) changeMembers(ctx context.Context, groupID string, change func(*sqlx.Tx) error) error {
	tx, err := m.db.BeginTxx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if err := change(tx); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, tx.Rebind(`UPDATE knox_groups SET user_count =
		(SELECT COUNT(*) FROM knox_group_members m WHERE m.group_id = knox_groups.group_id) WHERE group_id = ?`), groupID); err != nil {
		return err
	}
	return tx.Commit()
}

// Sync runs

func (m *Mirror) StartRun(ctx context.Context, trigger string) (int64, error) {
	var id int64
	err := m.db.GetContext(ctx, &id, m.db.Rebind(`INSERT INTO sync_runs ("trigger", started_at, status) VALUES (?, ?, 'running') RETURNING id`), trigger, now())
	return id, err
}

// FinishRun records the outcome; status must be "ok" or "failed".
func (m *Mirror) FinishRun(ctx context.Context, id int64, status, errMsg string, c SyncCounts) error {
	res, err := m.db.ExecContext(ctx, m.db.Rebind(`UPDATE sync_runs SET finished_at = ?, status = ?, error = ?,
		orgs = ?, users = ?, groups = ?, memberships = ?, devices = ? WHERE id = ?`),
		now(), status, errMsg, c.Orgs, c.Users, c.Groups, c.Memberships, c.Devices, id)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

const runCols = `id, "trigger" AS trigger, started_at, finished_at, status, error, orgs, users, groups, memberships, devices`

func (m *Mirror) RecentRuns(ctx context.Context, limit int) ([]SyncRun, error) {
	out := []SyncRun{}
	err := m.db.SelectContext(ctx, &out, m.db.Rebind(`SELECT `+runCols+` FROM sync_runs ORDER BY id DESC LIMIT ?`), limit)
	return out, err
}

// LastSuccess returns the most recent run with status ok, or ErrNotFound.
func (m *Mirror) LastSuccess(ctx context.Context) (SyncRun, error) {
	var r SyncRun
	err := m.db.GetContext(ctx, &r, `SELECT `+runCols+` FROM sync_runs WHERE status = 'ok' ORDER BY id DESC LIMIT 1`)
	return r, notFound(err)
}
