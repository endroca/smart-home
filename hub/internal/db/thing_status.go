package db

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"
)

// ThingStatus is the durable replica of a gateway's own status: liveness and
// diagnostics (IP, RSSI, heap, uptime, firmware version, OTA state) plus the
// manifest version it last advertised.
//
// Presence is not trusted across a restart: a restored status is seeded with
// online=false until the gateway is heard from again.
type ThingStatus struct {
	Thing           string
	Status          map[string]any
	ManifestVersion int
	LastSeen        time.Time
}

// SetThingStatus upserts the status of one gateway.
func (s *Store) SetThingStatus(ctx context.Context, thing string, status any, manifestVersion int) error {
	if thing == "" {
		return errors.New("db: thing is required")
	}
	if status == nil {
		status = map[string]any{}
	}
	raw, err := json.Marshal(status)
	if err != nil {
		return fmt.Errorf("db: status %s: %w", thing, err)
	}
	if _, err := s.db.ExecContext(ctx, `
INSERT INTO thing_status (thing, status, manifest_version, last_seen) VALUES (?, ?, ?, ?)
ON CONFLICT(thing) DO UPDATE SET
    status = excluded.status,
    manifest_version = excluded.manifest_version,
    last_seen = excluded.last_seen`,
		thing, string(raw), manifestVersion, formatTime(time.Now().UTC())); err != nil {
		return fmt.Errorf("db: upsert status %s: %w", thing, err)
	}
	return nil
}

// ThingStatusFor returns the stored status of one gateway.
func (s *Store) ThingStatusFor(ctx context.Context, thing string) (ThingStatus, error) {
	var (
		st       ThingStatus
		raw      string
		lastSeen string
	)
	err := s.db.QueryRowContext(ctx,
		`SELECT thing, status, manifest_version, last_seen FROM thing_status WHERE thing = ?`, thing).
		Scan(&st.Thing, &raw, &st.ManifestVersion, &lastSeen)
	if errors.Is(err, sql.ErrNoRows) {
		return ThingStatus{}, ErrNotFound
	}
	if err != nil {
		return ThingStatus{}, fmt.Errorf("db: status %s: %w", thing, err)
	}
	st.Status = decodeJSONObject(raw)
	st.LastSeen = parseTime(lastSeen)
	return st, nil
}

// AllThingStatuses returns every stored gateway status, used to seed the
// in-memory inventory at boot.
func (s *Store) AllThingStatuses(ctx context.Context) ([]ThingStatus, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT thing, status, manifest_version, last_seen FROM thing_status ORDER BY thing`)
	if err != nil {
		return nil, fmt.Errorf("db: list statuses: %w", err)
	}
	defer rows.Close()
	out := []ThingStatus{}
	for rows.Next() {
		var (
			st       ThingStatus
			raw      string
			lastSeen string
		)
		if err := rows.Scan(&st.Thing, &raw, &st.ManifestVersion, &lastSeen); err != nil {
			return nil, err
		}
		st.Status = decodeJSONObject(raw)
		st.LastSeen = parseTime(lastSeen)
		out = append(out, st)
	}
	return out, rows.Err()
}

// DeleteThingStatus removes the stored status of a gateway.
func (s *Store) DeleteThingStatus(ctx context.Context, thing string) error {
	if _, err := s.db.ExecContext(ctx, `DELETE FROM thing_status WHERE thing = ?`, thing); err != nil {
		return fmt.Errorf("db: delete status %s: %w", thing, err)
	}
	return nil
}

func decodeJSONObject(raw string) map[string]any {
	out := map[string]any{}
	if raw == "" {
		return out
	}
	if err := json.Unmarshal([]byte(raw), &out); err != nil {
		return map[string]any{}
	}
	return out
}
