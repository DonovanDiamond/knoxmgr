package sync

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/rs/zerolog"

	"github.com/DonovanDiamond/knoxmgr/src/db"
	"github.com/DonovanDiamond/knoxmgr/src/knox"
)

func newInventory(t *testing.T) (*Inventory, *db.Apps, *db.Audit, []db.MirrorDevice) {
	t.Helper()
	ctx := context.Background()
	s, m, _, d := newEnvDB(t, nil)
	if _, _, err := s.SyncNow(ctx); err != nil {
		t.Fatal(err)
	}
	s.wg.Wait()
	devs, err := m.Devices(ctx, nil)
	if err != nil || len(devs) == 0 {
		t.Fatalf("devices: %d, %v", len(devs), err)
	}
	apps, audit := db.NewApps(d), db.NewAudit(d)
	inv := NewInventory(m, apps, audit, s.knox, func(context.Context) int { return 6 }, func(context.Context) bool { return true }, zerolog.Nop())
	return inv, apps, audit, devs
}

func TestPullAllAndIfDue(t *testing.T) {
	ctx := context.Background()
	inv, apps, _, devs := newInventory(t)

	if err := inv.PullAll(ctx); err != nil {
		t.Fatal(err)
	}
	st, _ := apps.Inventory(ctx)
	if st.LastPullAt == nil {
		t.Fatal("last pull not recorded")
	}
	withApps := 0
	for _, d := range devs {
		if rows, _ := apps.DeviceApps(ctx, d.DeviceID); len(rows) > 0 {
			withApps++
			if !knox.IsEnrolled(d.DeviceStatus) {
				t.Fatalf("pulled non-enrolled device %s (%s)", d.DeviceID, d.DeviceStatus)
			}
		}
	}
	if withApps == 0 {
		t.Fatal("no device got apps")
	}

	// Fresh pull: not due, so the timestamp must not move.
	first := *st.LastPullAt
	inv.now = func() time.Time { return first.Add(time.Hour) }
	inv.PullIfDue(ctx)
	if st, _ = apps.Inventory(ctx); !st.LastPullAt.Equal(first) {
		t.Fatalf("pulled before due: %v -> %v", first, st.LastPullAt)
	}
	inv.now = func() time.Time { return first.Add(7 * time.Hour) }
	inv.PullIfDue(ctx)
	if st, _ = apps.Inventory(ctx); !st.LastPullAt.After(first) {
		t.Fatalf("not pulled when due: %v", st.LastPullAt)
	}
}

func TestPullAllSingleFlightAndNoKnox(t *testing.T) {
	ctx := context.Background()
	inv, _, _, _ := newInventory(t)
	inv.pulling.Lock()
	if err := inv.PullAll(ctx); !errors.Is(err, ErrInventoryRunning) {
		t.Fatalf("err = %v", err)
	}
	inv.pulling.Unlock()
	inv.knox = func() *knox.Client { return nil }
	if err := inv.PullAll(ctx); !errors.Is(err, ErrNotConfigured) {
		t.Fatalf("err = %v", err)
	}
	if err := inv.PullDevice(ctx, "x"); !errors.Is(err, ErrNotConfigured) {
		t.Fatalf("err = %v", err)
	}
}

func TestReportRequestAuditedOnce(t *testing.T) {
	ctx := context.Background()
	inv, apps, audit, devs := newInventory(t)
	want := 0
	for _, d := range devs {
		if knox.IsEnrolled(d.DeviceStatus) && d.Platform == "A" {
			want++
		}
	}
	inv.reportIfDue(ctx)
	inv.reportIfDue(ctx) // second call is within 24h: no new row
	rows, err := audit.ListRecent(ctx, 10)
	if err != nil {
		t.Fatal(err)
	}
	var got []db.AuditEntry
	for _, r := range rows {
		if r.Action == "device.apps.report_request" {
			got = append(got, r)
		}
	}
	if len(got) != 1 || got[0].ActorUsername != "system" || got[0].NewValue == nil || *got[0].NewValue != fmt.Sprintf("sent=%d failed=0", want) {
		t.Fatalf("audit rows = %+v (want sent=%d)", got, want)
	}
	if st, _ := apps.Inventory(ctx); st.LastReportRequestAt == nil {
		t.Fatal("report request time not recorded")
	}
}

func TestAfterSyncManualPullsAll(t *testing.T) {
	ctx := context.Background()
	inv, apps, _, _ := newInventory(t)
	if err := inv.PullAll(ctx); err != nil {
		t.Fatal(err)
	}
	st, _ := apps.Inventory(ctx)
	first := *st.LastPullAt
	inv.now = func() time.Time { return first.Add(time.Hour) } // not due

	inv.AfterSync(ctx, "scheduled")
	if st, _ = apps.Inventory(ctx); !st.LastPullAt.Equal(first) {
		t.Fatalf("scheduled run pulled before due: %v", st.LastPullAt)
	}
	inv.AfterSync(ctx, "manual")
	if st, _ = apps.Inventory(ctx); !st.LastPullAt.After(first) {
		t.Fatalf("manual run did not pull: %v", st.LastPullAt)
	}
}

func TestPullAllLogsCountsAndEmptySample(t *testing.T) {
	inv, _, _, _ := newInventory(t)
	var buf bytes.Buffer
	inv.log = zerolog.New(&buf)
	if err := inv.PullAll(context.Background()); err != nil {
		t.Fatal(err)
	}
	out := buf.String()
	if !strings.Contains(out, `"appRows":`) || !strings.Contains(out, `"devicesWithoutApps":`) {
		t.Fatalf("summary line lacks counts: %s", out)
	}
	if n := strings.Count(out, "Knox app item shape"); n != 1 || !strings.Contains(out, `"packageName":"string"`) {
		t.Fatalf("item shape logged %d times: %s", n, out)
	}
	if n := strings.Count(out, "Knox returned no apps for device"); n > 1 {
		t.Fatalf("empty-list warning logged %d times in one pull", n)
	}
}
