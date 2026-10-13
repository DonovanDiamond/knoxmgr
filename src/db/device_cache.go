package db

import (
	"context"
	"time"
)

// DeviceCacheEntry is a stored live Knox answer.
type DeviceCacheEntry struct {
	Payload   string
	FetchedAt time.Time
}

// DeviceCache returns the cached answer of kind for a device, or ErrNotFound.
func (m *Mirror) DeviceCache(ctx context.Context, deviceID, kind string) (DeviceCacheEntry, error) {
	var row struct {
		Payload   string `db:"payload"`
		FetchedAt string `db:"fetched_at"`
	}
	err := m.db.GetContext(ctx, &row, m.db.Rebind(`SELECT payload, fetched_at FROM knox_device_cache WHERE device_id = ? AND kind = ?`), deviceID, kind)
	if err != nil {
		return DeviceCacheEntry{}, notFound(err)
	}
	at, err := time.Parse(timeLayout, row.FetchedAt)
	return DeviceCacheEntry{Payload: row.Payload, FetchedAt: at}, err
}

// PutDeviceCache stores payload as the answer of kind for a device, replacing any earlier one.
func (m *Mirror) PutDeviceCache(ctx context.Context, deviceID, kind, payload string, fetchedAt time.Time) error {
	_, err := m.db.ExecContext(ctx, m.db.Rebind(`INSERT INTO knox_device_cache (device_id, kind, payload, fetched_at) VALUES (?, ?, ?, ?)
		ON CONFLICT (device_id, kind) DO UPDATE SET payload = excluded.payload, fetched_at = excluded.fetched_at`),
		deviceID, kind, payload, formatTime(fetchedAt))
	return err
}
