package db

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/jmoiron/sqlx"
)

// KnoxSettings and ADSettings carry secrets only as ciphertext (see src/secretbox).
type KnoxSettings struct {
	Region            string `db:"region"`
	BaseURL           string `db:"base_url"`
	ClientID          string `db:"client_id"`
	TenantID          string `db:"tenant_id"`
	ClientSecretSeal  string `db:"client_secret_cipher"`
	AllowInsecureHTTP bool   `db:"allow_insecure_http"`
}

type ADMapping struct {
	Group   string `db:"group_dn" json:"group"`
	Role    string `db:"role" json:"role"`
	OrgCode string `db:"org_code" json:"orgCode"`
	Access  string `db:"access" json:"access"`
}

type ADSettings struct {
	Enabled          bool        `db:"enabled"`
	URL              string      `db:"url"`
	BaseDN           string      `db:"base_dn"`
	UPNSuffix        string      `db:"upn_suffix"`
	BindUser         string      `db:"bind_user"`
	BindPasswordSeal string      `db:"bind_password_cipher"`
	Mappings         []ADMapping `db:"-"`
}

type GeneralSettings struct {
	InstallationName    string `db:"installation_name" json:"installationName"`
	SyncIntervalMinutes int    `db:"sync_interval_minutes" json:"syncIntervalMinutes"`
	CheckUpdates        bool   `db:"check_updates" json:"checkUpdates"`
	ShowMap             bool   `db:"show_map" json:"showMap"`
	AppInventoryHours   int    `db:"app_inventory_hours" json:"appInventoryHours"`
	AppReportDaily      bool   `db:"app_report_daily" json:"appReportDaily"`
}

// AppSettings stores the admin-editable settings. Every save audits in the same transaction.
type AppSettings struct{ db *sqlx.DB }

func NewAppSettings(d *sqlx.DB) *AppSettings { return &AppSettings{db: d} }

func (r *AppSettings) GetKnox(ctx context.Context) (KnoxSettings, error) {
	return getKnox(ctx, r.db)
}

func getKnox(ctx context.Context, q sqlx.QueryerContext) (KnoxSettings, error) {
	var k KnoxSettings
	if err := sqlx.GetContext(ctx, q, &k, `SELECT region, base_url, client_id, tenant_id, client_secret_cipher, allow_insecure_http FROM knox_settings WHERE id = 1`); err != nil {
		return k, fmt.Errorf("loading knox settings: %w", err)
	}
	return k, nil
}

func (r *AppSettings) GetAD(ctx context.Context) (ADSettings, error) {
	return getAD(ctx, r.db)
}

func getAD(ctx context.Context, q sqlx.QueryerContext) (ADSettings, error) {
	var a ADSettings
	if err := sqlx.GetContext(ctx, q, &a, `SELECT enabled, url, base_dn, upn_suffix, bind_user, bind_password_cipher FROM ad_settings WHERE id = 1`); err != nil {
		return a, fmt.Errorf("loading AD settings: %w", err)
	}
	a.Mappings = []ADMapping{}
	if err := sqlx.SelectContext(ctx, q, &a.Mappings, `SELECT group_dn, role, org_code, access FROM ad_group_mappings ORDER BY position, id`); err != nil {
		return a, fmt.Errorf("loading AD group mappings: %w", err)
	}
	return a, nil
}

func (r *AppSettings) GetGeneral(ctx context.Context) (GeneralSettings, error) {
	return getGeneral(ctx, r.db)
}

func getGeneral(ctx context.Context, q sqlx.QueryerContext) (GeneralSettings, error) {
	var g GeneralSettings
	if err := sqlx.GetContext(ctx, q, &g, `SELECT installation_name, sync_interval_minutes, check_updates, show_map, app_inventory_hours, app_report_daily FROM general_settings WHERE id = 1`); err != nil {
		return g, fmt.Errorf("loading general settings: %w", err)
	}
	return g, nil
}

// changed is the only thing an audit entry says about a secret.
func changed(b bool) string {
	if b {
		return "changed"
	}
	return "unchanged"
}

type knoxAudit struct {
	Region            string `json:"region"`
	BaseURL           string `json:"baseURL"`
	ClientID          string `json:"clientId"`
	TenantID          string `json:"tenantId"`
	ClientSecret      string `json:"clientSecret,omitempty"`
	AllowInsecureHTTP bool   `json:"allowInsecureHTTP"`
}

func auditJSON(v any) *string {
	b, _ := json.Marshal(v) // plain structs of strings, bools and ints cannot fail
	s := string(b)
	return &s
}

// SaveKnox replaces the Knox settings and audits them. next.ClientSecretSeal must already hold the
// ciphertext to keep (the stored one when the secret is not being replaced); secretChanged says which.
func (r *AppSettings) SaveKnox(ctx context.Context, next KnoxSettings, secretChanged bool, actorID *int64, actor string) error {
	return r.save(ctx, "settings.knox", actorID, actor, func(tx *sqlx.Tx) (*string, *string, error) {
		prev, err := getKnox(ctx, tx)
		if err != nil {
			return nil, nil, err
		}
		if _, err := tx.ExecContext(ctx, tx.Rebind(`
			UPDATE knox_settings SET region = ?, base_url = ?, client_id = ?, tenant_id = ?,
				client_secret_cipher = ?, allow_insecure_http = ?, updated_at = ? WHERE id = 1`),
			next.Region, next.BaseURL, next.ClientID, next.TenantID, next.ClientSecretSeal, next.AllowInsecureHTTP, now()); err != nil {
			return nil, nil, fmt.Errorf("saving knox settings: %w", err)
		}
		return auditJSON(knoxAudit{Region: prev.Region, BaseURL: prev.BaseURL, ClientID: prev.ClientID, TenantID: prev.TenantID, AllowInsecureHTTP: prev.AllowInsecureHTTP}),
			auditJSON(knoxAudit{next.Region, next.BaseURL, next.ClientID, next.TenantID, changed(secretChanged), next.AllowInsecureHTTP}), nil
	})
}

type adAudit struct {
	Enabled      bool        `json:"enabled"`
	URL          string      `json:"url"`
	BaseDN       string      `json:"baseDN"`
	UPNSuffix    string      `json:"upnSuffix"`
	BindUser     string      `json:"bindUser"`
	BindPassword string      `json:"bindPassword,omitempty"`
	Mappings     []ADMapping `json:"mappings"`
}

func (a ADSettings) audit(secret string) adAudit {
	return adAudit{a.Enabled, a.URL, a.BaseDN, a.UPNSuffix, a.BindUser, secret, a.Mappings}
}

// SaveAD replaces the AD settings and group mappings (kept in order) and audits them; see SaveKnox
// for the secret rule.
func (r *AppSettings) SaveAD(ctx context.Context, next ADSettings, secretChanged bool, actorID *int64, actor string) error {
	return r.save(ctx, "settings.ad", actorID, actor, func(tx *sqlx.Tx) (*string, *string, error) {
		prev, err := getAD(ctx, tx)
		if err != nil {
			return nil, nil, err
		}
		if _, err := tx.ExecContext(ctx, tx.Rebind(`
			UPDATE ad_settings SET enabled = ?, url = ?, base_dn = ?, upn_suffix = ?, bind_user = ?,
				bind_password_cipher = ?, updated_at = ? WHERE id = 1`),
			next.Enabled, next.URL, next.BaseDN, next.UPNSuffix, next.BindUser, next.BindPasswordSeal, now()); err != nil {
			return nil, nil, fmt.Errorf("saving AD settings: %w", err)
		}
		if _, err := tx.ExecContext(ctx, `DELETE FROM ad_group_mappings`); err != nil {
			return nil, nil, fmt.Errorf("saving AD group mappings: %w", err)
		}
		for i, m := range next.Mappings {
			if _, err := tx.ExecContext(ctx, tx.Rebind(`
				INSERT INTO ad_group_mappings (position, group_dn, role, org_code, access) VALUES (?, ?, ?, ?, ?)`),
				i, m.Group, m.Role, m.OrgCode, m.Access); err != nil {
				return nil, nil, fmt.Errorf("saving AD group mapping %q: %w", m.Group, err)
			}
		}
		return auditJSON(prev.audit("")), auditJSON(next.audit(changed(secretChanged))), nil
	})
}

func (r *AppSettings) SaveGeneral(ctx context.Context, next GeneralSettings, actorID *int64, actor string) error {
	return r.save(ctx, "settings.general", actorID, actor, func(tx *sqlx.Tx) (*string, *string, error) {
		prev, err := getGeneral(ctx, tx)
		if err != nil {
			return nil, nil, err
		}
		if _, err := tx.ExecContext(ctx, tx.Rebind(`
			UPDATE general_settings SET installation_name = ?, sync_interval_minutes = ?, check_updates = ?, show_map = ?, app_inventory_hours = ?, app_report_daily = ?, updated_at = ? WHERE id = 1`),
			next.InstallationName, next.SyncIntervalMinutes, next.CheckUpdates, next.ShowMap, next.AppInventoryHours, b2i(next.AppReportDaily), now()); err != nil {
			return nil, nil, fmt.Errorf("saving general settings: %w", err)
		}
		return auditJSON(prev), auditJSON(next), nil
	})
}

// save runs apply and the audit entry it describes in one transaction.
func (r *AppSettings) save(ctx context.Context, action string, actorID *int64, actor string, apply func(*sqlx.Tx) (prev, next *string, err error)) error {
	tx, err := r.db.BeginTxx(ctx, nil)
	if err != nil {
		return fmt.Errorf("%s: %w", action, err)
	}
	defer tx.Rollback()
	prev, next, err := apply(tx)
	if err != nil {
		return err
	}
	if err := insertAudit(ctx, tx, &AuditEntry{ActorUserID: actorID, ActorUsername: actor, Action: action, Target: action, PreviousValue: prev, NewValue: next}); err != nil {
		return err
	}
	return tx.Commit()
}
