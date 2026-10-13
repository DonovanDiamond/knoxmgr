package db

import (
	"errors"
	"testing"
	"time"
)

func TestDeviceCache(t *testing.T) {
	m, ctx := newMirror(t)
	at := time.Date(2026, 5, 1, 10, 0, 0, 0, time.UTC)
	if _, err := m.DeviceCache(ctx, "d1", "detail"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("empty cache: %v", err)
	}
	for _, p := range []string{`{"a":1}`, `{"a":2}`} { // second put replaces the first
		if err := m.PutDeviceCache(ctx, "d1", "detail", p, at); err != nil {
			t.Fatal(err)
		}
	}
	if err := m.PutDeviceCache(ctx, "d1", "apps", `{}`, at); err != nil {
		t.Fatal(err)
	}
	e, err := m.DeviceCache(ctx, "d1", "detail")
	if err != nil || e.Payload != `{"a":2}` || !e.FetchedAt.Equal(at) {
		t.Fatalf("got %+v, %v", e, err)
	}
	if _, err := m.DeviceCache(ctx, "d1", "apps"); err != nil {
		t.Fatalf("kinds must not collide: %v", err)
	}
}

func TestReplaceAllPrunesCacheOfGoneDevices(t *testing.T) {
	m, ctx := newMirror(t)
	for _, id := range []string{"d1", "d2", "gone"} {
		if err := m.PutDeviceCache(ctx, id, "location:7", `{}`, time.Now()); err != nil {
			t.Fatal(err)
		}
	}
	s := mirrorSnapshot()
	s.Devices = s.Devices[:1] // only d1 remains
	if err := m.ReplaceAll(ctx, s, time.Now()); err != nil {
		t.Fatal(err)
	}
	for id, want := range map[string]bool{"d1": true, "d2": false, "gone": false} {
		_, err := m.DeviceCache(ctx, id, "location:7")
		if got := err == nil; got != want {
			t.Errorf("%s cached = %v, want %v (%v)", id, got, want, err)
		}
	}
}
