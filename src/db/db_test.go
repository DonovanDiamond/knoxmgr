package db

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/jmoiron/sqlx"
)

func testDB(t *testing.T) *sqlx.DB {
	t.Helper()
	d, err := Open("sqlite", filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { d.Close() })
	if err := Migrate(context.Background(), d); err != nil {
		t.Fatal(err)
	}
	return d
}

func ptr[T any](v T) *T { return &v }

func localUser(name, role string, org, access *string) *User {
	return &User{Username: name, PasswordHash: ptr("hash"), Role: role, OrgCode: org, Access: access, AuthSource: "local"}
}

func TestOpenPragmas(t *testing.T) {
	d := testDB(t)
	var fk int
	var mode string
	if err := d.Get(&fk, `PRAGMA foreign_keys`); err != nil || fk != 1 {
		t.Fatalf("foreign_keys = %d, %v", fk, err)
	}
	if err := d.Get(&mode, `PRAGMA journal_mode`); err != nil || mode != "wal" {
		t.Fatalf("journal_mode = %q, %v", mode, err)
	}
	if _, err := Open("mysql", "x"); err == nil {
		t.Fatal("want error for unsupported driver")
	}
}

func TestMigrateIdempotent(t *testing.T) {
	d := testDB(t)
	if err := Migrate(context.Background(), d); err != nil {
		t.Fatal(err)
	}
	var n int
	if err := d.Get(&n, `SELECT COUNT(*) FROM schema_migrations`); err != nil || n != 8 {
		t.Fatalf("schema_migrations rows = %d, %v", n, err)
	}
}

func TestUserConstraints(t *testing.T) {
	d := testDB(t)
	ctx := context.Background()
	repo := NewUsers(d)

	tests := []struct {
		name string
		user *User
		want error // nil means must succeed
		sub  string
	}{
		{"admin", localUser("root", "admin", nil, nil), nil, ""},
		{"user with org and access", localUser("alice", "user", ptr("ORG1"), ptr("read")), nil, ""},
		{"mixed case is lowered", localUser("Bob", "user", ptr("ORG1"), ptr("write")), nil, ""},
		{"user without org", localUser("c1", "user", nil, ptr("read")), nil, "CHECK"},
		{"user with empty org", localUser("c2", "user", ptr(""), ptr("read")), nil, "CHECK"},
		{"user without access", localUser("c3", "user", ptr("O"), nil), nil, "CHECK"},
		{"bad role", localUser("c4", "root", nil, nil), nil, "CHECK"},
		{"bad access", localUser("c5", "user", ptr("O"), ptr("rw")), nil, "CHECK"},
		{"local without password", &User{Username: "c6", Role: "admin", AuthSource: "local"}, nil, "CHECK"},
		{"bad auth source", &User{Username: "c7", Role: "admin", AuthSource: "ldap", PasswordHash: ptr("h")}, nil, "CHECK"},
		{"duplicate (case-insensitive)", localUser("ROOT", "admin", nil, nil), ErrDuplicate, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := repo.Create(ctx, tt.user)
			switch {
			case tt.want != nil:
				if !errors.Is(err, tt.want) {
					t.Fatalf("want %v, got %v", tt.want, err)
				}
			case tt.sub != "":
				if err == nil || !strings.Contains(err.Error(), tt.sub) {
					t.Fatalf("want constraint error containing %q, got %v", tt.sub, err)
				}
			case err != nil:
				t.Fatal(err)
			case tt.user.ID == 0 || tt.user.CreatedAt == "":
				t.Fatalf("ID/CreatedAt not filled: %+v", tt.user)
			}
		})
	}

	got, err := repo.GetByUsername(ctx, "BOB")
	if err != nil || got.Username != "bob" || got.Access == nil || *got.Access != "write" {
		t.Fatalf("GetByUsername = %+v, %v", got, err)
	}
}

func TestUserGetUpdateCount(t *testing.T) {
	d := testDB(t)
	ctx := context.Background()
	repo := NewUsers(d)

	if _, err := repo.GetByID(ctx, 99); !errors.Is(err, ErrNotFound) {
		t.Fatalf("want ErrNotFound, got %v", err)
	}
	if _, err := repo.GetByUsername(ctx, "ghost"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("want ErrNotFound, got %v", err)
	}
	if n, err := repo.CountAdmins(ctx); err != nil || n != 0 {
		t.Fatalf("CountAdmins = %d, %v", n, err)
	}

	u := localUser("root", "admin", nil, nil)
	if err := repo.Create(ctx, u); err != nil {
		t.Fatal(err)
	}
	if n, _ := repo.CountAdmins(ctx); n != 1 {
		t.Fatalf("CountAdmins = %d, want 1", n)
	}

	u.DisplayName, u.Disabled = "Root", true
	if err := repo.Update(ctx, u); err != nil {
		t.Fatal(err)
	}
	got, err := repo.GetByID(ctx, u.ID)
	if err != nil || got.DisplayName != "Root" || !got.Disabled {
		t.Fatalf("after update: %+v, %v", got, err)
	}

	// Update must respect the table constraints too.
	u.Role = "user"
	if err := repo.Update(ctx, u); err == nil {
		t.Fatal("want constraint error demoting admin to user without org/access")
	}
	if err := repo.Update(ctx, &User{ID: 99, Role: "admin", PasswordHash: ptr("h")}); !errors.Is(err, ErrNotFound) {
		t.Fatalf("want ErrNotFound, got %v", err)
	}
}

func TestUserListDelete(t *testing.T) {
	d := testDB(t)
	ctx := context.Background()
	repo := NewUsers(d)

	if got, err := repo.List(ctx); err != nil || got == nil || len(got) != 0 {
		t.Fatalf("empty List = %#v, %v", got, err)
	}
	b, a := localUser("b", "admin", nil, nil), localUser("a", "user", ptr("ORG"), ptr("read"))
	for _, u := range []*User{b, a} {
		if err := repo.Create(ctx, u); err != nil {
			t.Fatal(err)
		}
	}
	got, err := repo.List(ctx)
	if err != nil || len(got) != 2 || got[0].Username != "a" || got[1].Username != "b" {
		t.Fatalf("List = %+v, %v", got, err)
	}

	// Admin edits of role, org, access and disabled go through Update.
	a.Role, a.OrgCode, a.Access, a.Disabled, a.DisplayName = "user", ptr("ORG2"), ptr("write"), true, "Ann"
	if err := repo.Update(ctx, a); err != nil {
		t.Fatal(err)
	}
	if u, _ := repo.GetByID(ctx, a.ID); *u.OrgCode != "ORG2" || *u.Access != "write" || !u.Disabled || u.DisplayName != "Ann" {
		t.Fatalf("after update: %+v", u)
	}

	// Delete must cope with dependants: refresh tokens go, audit rows stay.
	if err := NewRefreshTokens(d).Insert(ctx, a.ID, "h", time.Now().Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	audit := NewAudit(d)
	if err := audit.Insert(ctx, &AuditEntry{ActorUserID: &a.ID, ActorUsername: "a", Action: "x", Target: "t"}); err != nil {
		t.Fatal(err)
	}
	if err := repo.Delete(ctx, a.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := repo.GetByID(ctx, a.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("deleted user still loads: %v", err)
	}
	if _, err := NewRefreshTokens(d).GetByHash(ctx, "h"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("refresh token should be gone: %v", err)
	}
	entries, err := audit.ListRecent(ctx, 10)
	if err != nil || len(entries) != 1 || entries[0].ActorUserID != nil || entries[0].ActorUsername != "a" {
		t.Fatalf("audit after delete = %+v, %v", entries, err)
	}
	if err := repo.Delete(ctx, a.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("second delete: want ErrNotFound, got %v", err)
	}

	// A local account still blocks an AD login of the same name.
	if _, err := repo.UpsertADUser(ctx, &User{Username: "b", Role: "admin"}); !errors.Is(err, ErrSourceConflict) {
		t.Fatalf("want ErrSourceConflict, got %v", err)
	}
}

func TestRefreshGetByHash(t *testing.T) {
	d := testDB(t)
	ctx := context.Background()
	u := localUser("a", "admin", nil, nil)
	if err := NewUsers(d).Create(ctx, u); err != nil {
		t.Fatal(err)
	}
	repo := NewRefreshTokens(d)
	_ = repo.Insert(ctx, u.ID, "h", time.Now().Add(time.Hour))
	tok, err := repo.GetByHash(ctx, "h")
	if err != nil || tok.RevokedAt != nil {
		t.Fatalf("GetByHash = %+v, %v", tok, err)
	}
	_ = repo.Revoke(ctx, tok.ID)
	if tok, err = repo.GetByHash(ctx, "h"); err != nil || tok.RevokedAt == nil {
		t.Fatalf("revoked token must still load with RevokedAt set: %+v, %v", tok, err)
	}
	if _, err := repo.GetByHash(ctx, "nope"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("want ErrNotFound, got %v", err)
	}
}

func TestUpsertADUser(t *testing.T) {
	d := testDB(t)
	ctx := context.Background()
	repo := NewUsers(d)

	first, err := repo.UpsertADUser(ctx, &User{Username: "Carol", DisplayName: "Carol A", Role: "user", OrgCode: ptr("ORG1"), Access: ptr("read")})
	if err != nil || first.AuthSource != "ad" || first.PasswordHash != nil {
		t.Fatalf("insert: %+v, %v", first, err)
	}

	first.Disabled = true
	if err := repo.Update(ctx, first); err != nil {
		t.Fatal(err)
	}
	second, err := repo.UpsertADUser(ctx, &User{Username: "carol", DisplayName: "Carol B", Role: "admin"})
	if err != nil {
		t.Fatal(err)
	}
	if second.ID != first.ID || second.DisplayName != "Carol B" || second.Role != "admin" || second.OrgCode != nil || !second.Disabled {
		t.Fatalf("refresh should follow AD but keep disabled: %+v", second)
	}

	if err := repo.Create(ctx, localUser("dave", "admin", nil, nil)); err != nil {
		t.Fatal(err)
	}
	if _, err := repo.UpsertADUser(ctx, &User{Username: "dave", Role: "admin"}); !errors.Is(err, ErrSourceConflict) {
		t.Fatalf("want ErrSourceConflict, got %v", err)
	}
}

func TestRefreshTokens(t *testing.T) {
	d := testDB(t)
	ctx := context.Background()
	users, repo := NewUsers(d), NewRefreshTokens(d)
	u1, u2 := localUser("a", "admin", nil, nil), localUser("b", "admin", nil, nil)
	for _, u := range []*User{u1, u2} {
		if err := users.Create(ctx, u); err != nil {
			t.Fatal(err)
		}
	}
	base := time.Date(2030, 1, 1, 12, 0, 0, 0, time.UTC)
	if err := repo.Insert(ctx, u1.ID, "h-valid", base.Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	if err := repo.Insert(ctx, 999, "h-orphan", base); err == nil {
		t.Fatal("foreign key to app_users must be enforced")
	}
	if err := repo.Insert(ctx, u1.ID, "h-valid", base); err == nil {
		t.Fatal("token_hash must be unique")
	}

	tests := []struct {
		name string
		hash string
		at   time.Time
		ok   bool
	}{
		{"valid", "h-valid", base, true},
		{"just before expiry", "h-valid", base.Add(time.Hour - time.Second), true},
		{"at expiry", "h-valid", base.Add(time.Hour), false},
		{"after expiry", "h-valid", base.Add(2 * time.Hour), false},
		{"unknown", "nope", base, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := repo.GetValid(ctx, tt.hash, tt.at)
			if tt.ok && err != nil {
				t.Fatal(err)
			}
			if !tt.ok && !errors.Is(err, ErrNotFound) {
				t.Fatalf("want ErrNotFound, got %v", err)
			}
		})
	}

	tok, _ := repo.GetValid(ctx, "h-valid", base)
	if err := repo.Revoke(ctx, tok.ID); err != nil {
		t.Fatal(err)
	}
	if err := repo.Revoke(ctx, tok.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("second revoke: want ErrNotFound, got %v", err)
	}
	if _, err := repo.GetValid(ctx, "h-valid", base); !errors.Is(err, ErrNotFound) {
		t.Fatalf("revoked token must not be valid, got %v", err)
	}

	for _, h := range []string{"x1", "x2"} {
		_ = repo.Insert(ctx, u1.ID, h, base.Add(time.Hour))
	}
	_ = repo.Insert(ctx, u2.ID, "y1", base.Add(time.Hour))
	if err := repo.RevokeAllForUser(ctx, u1.ID); err != nil {
		t.Fatal(err)
	}
	for h, ok := range map[string]bool{"x1": false, "x2": false, "y1": true} {
		if _, err := repo.GetValid(ctx, h, base); (err == nil) != ok {
			t.Fatalf("token %s valid=%v, want %v", h, err == nil, ok)
		}
	}
}

func TestAuditAndSettings(t *testing.T) {
	d := testDB(t)
	ctx := context.Background()
	users, audit, settings := NewUsers(d), NewAudit(d), NewSettings(d)
	u := localUser("root", "admin", nil, nil)
	if err := users.Create(ctx, u); err != nil {
		t.Fatal(err)
	}

	if _, err := settings.Get(ctx, "motd"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("want ErrNotFound, got %v", err)
	}
	for _, v := range []string{"one", "two"} {
		if err := settings.Set(ctx, "motd", v, &u.ID, "root"); err != nil {
			t.Fatal(err)
		}
	}
	if v, err := settings.Get(ctx, "motd"); err != nil || v != "two" {
		t.Fatalf("Get = %q, %v", v, err)
	}

	if err := audit.Insert(ctx, &AuditEntry{ActorUsername: "system", Action: "x", Target: "y"}); err != nil {
		t.Fatal(err)
	}
	got, err := audit.ListRecent(ctx, 2)
	if err != nil || len(got) != 2 {
		t.Fatalf("ListRecent = %d entries, %v", len(got), err)
	}
	if got[0].Action != "x" || got[0].ActorUserID != nil || got[0].PreviousValue != nil {
		t.Fatalf("newest entry wrong: %+v", got[0])
	}
	second := got[1]
	if second.Action != "settings.set" || second.Target != "motd" ||
		second.PreviousValue == nil || *second.PreviousValue != "one" || *second.NewValue != "two" ||
		second.ActorUserID == nil || *second.ActorUserID != u.ID {
		t.Fatalf("settings audit entry wrong: %+v", second)
	}

	all, _ := audit.ListRecent(ctx, 50)
	if len(all) != 3 || all[2].PreviousValue != nil {
		t.Fatalf("first set should have nil previous value: %+v", all)
	}
}

func TestLatestInvites(t *testing.T) {
	audit := NewAudit(testDB(t))
	ctx := context.Background()
	invite := func(ids ...string) {
		t.Helper()
		if err := audit.InsertInvites(ctx, &AuditEntry{ActorUsername: "a", Action: "knox.user.invite", Target: strings.Join(ids, ",")}, ids); err != nil {
			t.Fatal(err)
		}
	}
	if got, err := audit.LatestInvites(ctx, []string{"bob"}); err != nil || len(got) != 0 {
		t.Fatalf("empty table: %v, %v", got, err)
	}
	invite("bob", "carol")
	invite("carol")
	invite("a_c", "100%")
	// Backdate bob's only row so "latest" is observable at one-second resolution.
	old := "2020-01-02T03:04:05Z"
	if _, err := audit.db.ExecContext(ctx, `UPDATE invites SET invited_at = ? WHERE user_id = 'bob'`, old); err != nil {
		t.Fatal(err)
	}
	if _, err := audit.db.ExecContext(ctx, `UPDATE invites SET invited_at = ? WHERE id = 2`, old); err != nil {
		t.Fatal(err)
	}
	many := make([]string, 1200) // spans several batches
	for i := range many {
		many[i] = fmt.Sprintf("u%d", i)
	}
	invite(many[1100])

	tests := []struct {
		name string
		ids  []string
		want []string
	}{
		{"nil ids", nil, nil},
		{"unknown absent", []string{"dave"}, nil},
		{"latest wins", []string{"bob", "carol", "dave"}, []string{"bob", "carol"}},
		{"underscore is literal", []string{"a_c", "abc", "a__"}, []string{"a_c"}},
		{"percent is literal", []string{"100%", "1%", "%"}, []string{"100%"}},
		{"batched", append(append([]string{}, many...), "bob"), []string{"u1100", "bob"}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := audit.LatestInvites(ctx, tc.ids)
			if err != nil {
				t.Fatal(err)
			}
			if len(got) != len(tc.want) {
				t.Fatalf("got %v, want keys %v", got, tc.want)
			}
			for _, id := range tc.want {
				if _, ok := got[id]; !ok {
					t.Errorf("missing %q in %v", id, got)
				}
			}
		})
	}

	got, err := audit.LatestInvites(ctx, []string{"bob", "carol"})
	if err != nil {
		t.Fatal(err)
	}
	if want := time.Date(2020, 1, 2, 3, 4, 5, 0, time.UTC); !got["bob"].Equal(want) {
		t.Errorf("bob = %v, want %v", got["bob"], want)
	}
	if !got["carol"].After(got["bob"]) {
		t.Errorf("carol = %v should be later than her backdated first invite and bob = %v", got["carol"], got["bob"])
	}
}

func TestInviteBackfill(t *testing.T) {
	d := testDB(t)
	ctx := context.Background()
	audit := NewAudit(d)
	actor := localUser("admin", "admin", nil, nil)
	if err := NewUsers(d).Create(ctx, actor); err != nil {
		t.Fatal(err)
	}
	for _, e := range []AuditEntry{
		{ActorUserID: &actor.ID, ActorUsername: "admin", Action: "knox.user.invite", Target: "bob,carol"},
		{ActorUsername: "gone", Action: "knox.user.invite", Target: "dave,,"},
		{ActorUsername: "admin", Action: "knox.user.update", Target: "erin"},
	} {
		if err := audit.Insert(ctx, &e); err != nil {
			t.Fatal(err)
		}
	}
	for _, q := range []string{`DROP TABLE invites`, `DELETE FROM schema_migrations WHERE version = 2`} {
		if _, err := d.ExecContext(ctx, q); err != nil {
			t.Fatal(err)
		}
	}
	if err := Migrate(ctx, d); err != nil {
		t.Fatal(err)
	}
	var rows []struct {
		UserID    string `db:"user_id"`
		InvitedAt string `db:"invited_at"`
		InvitedBy *int64 `db:"invited_by"`
	}
	if err := d.SelectContext(ctx, &rows, `SELECT user_id, invited_at, invited_by FROM invites ORDER BY id`); err != nil {
		t.Fatal(err)
	}
	if len(rows) != 3 {
		t.Fatalf("rows = %+v, want bob, carol, dave", rows)
	}
	for i, id := range []string{"bob", "carol", "dave"} {
		if rows[i].UserID != id || rows[i].InvitedAt == "" {
			t.Errorf("row %d = %+v, want %s", i, rows[i], id)
		}
	}
	if rows[0].InvitedBy == nil || *rows[0].InvitedBy != actor.ID || rows[1].InvitedBy == nil || rows[2].InvitedBy != nil {
		t.Errorf("invited_by = %+v", rows)
	}
}

func TestUserDeleteWithInvites(t *testing.T) {
	d := testDB(t)
	ctx := context.Background()
	u := localUser("admin", "admin", nil, nil)
	if err := NewUsers(d).Create(ctx, u); err != nil {
		t.Fatal(err)
	}
	if err := NewAudit(d).InsertInvites(ctx, &AuditEntry{ActorUserID: &u.ID, ActorUsername: "admin", Action: "knox.user.invite", Target: "bob"}, []string{"bob"}); err != nil {
		t.Fatal(err)
	}
	if err := NewUsers(d).Delete(ctx, u.ID); err != nil {
		t.Fatal(err)
	}
	var by *int64
	if err := d.Get(&by, `SELECT invited_by FROM invites WHERE user_id = 'bob'`); err != nil || by != nil {
		t.Fatalf("invited_by = %v, %v; want kept row with NULL", by, err)
	}
}

func TestSettingsSetRollsBackWithAuditFailure(t *testing.T) {
	d := testDB(t)
	ctx := context.Background()
	bogus := int64(12345) // violates audit_log.actor_user_id FK
	if err := NewSettings(d).Set(ctx, "k", "v", &bogus, "ghost"); err == nil {
		t.Fatal("want error")
	}
	if _, err := NewSettings(d).Get(ctx, "k"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("setting must roll back with its audit entry, got %v", err)
	}
}

func TestAppSettingsDefaults(t *testing.T) {
	r := NewAppSettings(testDB(t))
	ctx := context.Background()
	k, err := r.GetKnox(ctx)
	if err != nil || k != (KnoxSettings{}) {
		t.Fatalf("knox defaults = %+v, %v", k, err)
	}
	a, err := r.GetAD(ctx)
	if err != nil || a.Enabled || len(a.Mappings) != 0 {
		t.Fatalf("ad defaults = %+v, %v", a, err)
	}
	g, err := r.GetGeneral(ctx)
	if err != nil || g != (GeneralSettings{InstallationName: "knoxmgr", SyncIntervalMinutes: 15, CheckUpdates: true, ShowMap: true}) {
		t.Fatalf("general defaults = %+v, %v", g, err)
	}
}

func TestAppSettingsRoundTripAndAudit(t *testing.T) {
	d := testDB(t)
	r, audit := NewAppSettings(d), NewAudit(d)
	ctx := context.Background()
	const sealed1, sealed2 = "SEALED-ONE-ciphertext", "SEALED-TWO-ciphertext"

	if err := r.SaveKnox(ctx, KnoxSettings{Region: "eu01", BaseURL: "https://eu01.manage.samsungknox.com", ClientID: "c", TenantID: "t", ClientSecretSeal: sealed1}, true, nil, "root"); err != nil {
		t.Fatal(err)
	}
	if err := r.SaveKnox(ctx, KnoxSettings{Region: "custom", BaseURL: "http://x", ClientID: "c2", TenantID: "t", ClientSecretSeal: sealed1, AllowInsecureHTTP: true}, false, nil, "root"); err != nil {
		t.Fatal(err)
	}
	if got, _ := r.GetKnox(ctx); got.Region != "custom" || got.ClientSecretSeal != sealed1 || !got.AllowInsecureHTTP {
		t.Fatalf("knox = %+v", got)
	}

	maps := []ADMapping{{"Zeta", "user", "A", "read"}, {"Alpha", "admin", "", ""}, {"Mid", "user", "B", "write"}}
	if err := r.SaveAD(ctx, ADSettings{Enabled: true, URL: "ldaps://dc", BaseDN: "DC=x", BindUser: "svc", BindPasswordSeal: sealed2, Mappings: maps}, true, nil, "root"); err != nil {
		t.Fatal(err)
	}
	if err := r.SaveAD(ctx, ADSettings{Enabled: true, URL: "ldaps://dc", BaseDN: "DC=x", BindUser: "svc", BindPasswordSeal: sealed2, Mappings: maps[:1]}, false, nil, "root"); err != nil {
		t.Fatal(err)
	}
	if got, _ := r.GetAD(ctx); len(got.Mappings) != 1 || got.Mappings[0] != maps[0] || got.BindPasswordSeal != sealed2 {
		t.Fatalf("ad = %+v", got)
	}
	if err := r.SaveAD(ctx, ADSettings{Mappings: maps}, false, nil, "root"); err != nil { // order must survive a reload
		t.Fatal(err)
	}
	if got, _ := r.GetAD(ctx); len(got.Mappings) != 3 || got.Mappings[0].Group != "Zeta" || got.Mappings[2].Group != "Mid" {
		t.Fatalf("mapping order lost: %+v", got.Mappings)
	}

	if err := r.SaveGeneral(ctx, GeneralSettings{InstallationName: "Acme", SyncIntervalMinutes: 30, CheckUpdates: false, ShowMap: false}, nil, "root"); err != nil {
		t.Fatal(err)
	}
	if got, _ := r.GetGeneral(ctx); got.InstallationName != "Acme" || got.SyncIntervalMinutes != 30 || got.CheckUpdates || got.ShowMap {
		t.Fatalf("general = %+v", got)
	}

	entries, err := audit.ListRecent(ctx, 50)
	if err != nil {
		t.Fatal(err)
	}
	counts := map[string]int{}
	for _, e := range entries {
		counts[e.Action]++
		for _, v := range []*string{e.PreviousValue, e.NewValue} {
			if v == nil {
				continue
			}
			if strings.Contains(*v, "SEALED") || strings.Contains(*v, "ciphertext") {
				t.Fatalf("audit entry %s leaks a secret: %s", e.Action, *v)
			}
		}
	}
	if counts["settings.knox"] != 2 || counts["settings.ad"] != 3 || counts["settings.general"] != 1 {
		t.Fatalf("audit counts = %v", counts)
	}
	for _, e := range entries {
		if e.Action == "settings.knox" && e.NewValue != nil && strings.Contains(*e.NewValue, `"region":"custom"`) {
			if !strings.Contains(*e.NewValue, `"clientSecret":"unchanged"`) || e.PreviousValue == nil || !strings.Contains(*e.PreviousValue, `"region":"eu01"`) {
				t.Fatalf("knox audit = prev %v new %v", e.PreviousValue, *e.NewValue)
			}
		}
		if e.Action == "settings.knox" && e.NewValue != nil && strings.Contains(*e.NewValue, `"region":"eu01"`) && !strings.Contains(*e.NewValue, `"clientSecret":"changed"`) {
			t.Fatalf("first knox save should say changed: %s", *e.NewValue)
		}
	}
}

func TestAppSettingsConstraints(t *testing.T) {
	r := NewAppSettings(testDB(t))
	ctx := context.Background()
	tests := []struct {
		name string
		run  func() error
	}{
		{"interval zero", func() error {
			return r.SaveGeneral(ctx, GeneralSettings{InstallationName: "x", SyncIntervalMinutes: 0}, nil, "r")
		}},
		{"interval too big", func() error {
			return r.SaveGeneral(ctx, GeneralSettings{InstallationName: "x", SyncIntervalMinutes: 1441}, nil, "r")
		}},
		{"bad mapping role", func() error {
			return r.SaveAD(ctx, ADSettings{Mappings: []ADMapping{{"g", "root", "", ""}}}, false, nil, "r")
		}},
		{"bad mapping access", func() error {
			return r.SaveAD(ctx, ADSettings{Mappings: []ADMapping{{"g", "user", "A", "rw"}}}, false, nil, "r")
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if err := tt.run(); err == nil {
				t.Fatal("want constraint error")
			}
		})
	}
	// A failed save rolls back its audit entry too.
	if n, _ := NewAudit(r.db).ListRecent(ctx, 10); len(n) != 0 {
		t.Fatalf("failed saves left audit rows: %+v", n)
	}
}
