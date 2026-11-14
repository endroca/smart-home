package db

import (
	"context"
	"encoding/json"
	"fmt"
	"time"
)

// ThingState is the last-known reported state of one sub-device (for example a
// fan) exposed by a gateway. It is a durable replica of what the device
// reported: the hub keeps the hot copy in memory and uses this table to render
// the inventory immediately after a restart, before the LAN probe answers.
type ThingState struct {
	Thing      string
	DeviceID   string
	Reported   map[string]any
	Source     string
	DeviceTS   int64
	ReceivedAt time.Time
}

// SetThingState upserts the reported state of one sub-device. The state is
// stored as JSON so booleans, numbers and strings round trip.
func (s *Store) SetThingState(ctx context.Context, thing, deviceID string, reported map[string]any, source string, deviceTS int64) error {
	if thing == "" || deviceID == "" {
		return fmt.Errorf("db: thing and device id are required")
	}
	if reported == nil {
		reported = map[string]any{}
	}
	raw, err := json.Marshal(reported)
	if err != nil {
		return fmt.Errorf("db: state %s/%s: %w", thing, deviceID, err)
	}
	if _, err := s.db.ExecContext(ctx, `
INSERT INTO thing_states (thing, device_id, reported, source, device_ts, received_at) VALUES (?, ?, ?, ?, ?, ?)
ON CONFLICT(thing, device_id) DO UPDATE SET
    reported = excluded.reported,
    source = excluded.source,
    device_ts = excluded.device_ts,
    received_at = excluded.received_at`,
		thing, deviceID, string(raw), source, deviceTS, formatTime(time.Now().UTC())); err != nil {
		return fmt.Errorf("db: upsert state %s/%s: %w", thing, deviceID, err)
	}
	return nil
}

// ThingStates returns the last-known state of every sub-device of one gateway.
func (s *Store) ThingStates(ctx context.Context, thing string) ([]ThingState, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT thing, device_id, reported, source, device_ts, received_at FROM thing_states WHERE thing = ? ORDER BY device_id`, thing)
	if err != nil {
		return nil, fmt.Errorf("db: list states %s: %w", thing, err)
	}
	defer rows.Close()
	return scanThingStates(rows)
}

// AllThingStates returns the last-known state of every sub-device of every
// gateway, used to seed the in-memory inventory at boot.
func (s *Store) AllThingStates(ctx context.Context) ([]ThingState, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT thing, device_id, reported, source, device_ts, received_at FROM thing_states ORDER BY thing, device_id`)
	if err != nil {
		return nil, fmt.Errorf("db: list states: %w", err)
	}
	defer rows.Close()
	return scanThingStates(rows)
}

func scanThingStates(rows interface {
	Next() bool
	Scan(dest ...any) error
	Err() error
}) ([]ThingState, error) {
	out := []ThingState{}
	for rows.Next() {
		var (
			st       ThingState
			raw      string
			received string
		)
		if err := rows.Scan(&st.Thing, &st.DeviceID, &raw, &st.Source, &st.DeviceTS, &received); err != nil {
			return nil, err
		}
		if err := json.Unmarshal([]byte(raw), &st.Reported); err != nil {
			st.Reported = map[string]any{}
		}
		st.ReceivedAt = parseTime(received)
		out = append(out, st)
	}
	return out, rows.Err()
}

// DeleteThingState removes the cached state of one sub-device.
func (s *Store) DeleteThingState(ctx context.Context, thing, deviceID string) error {
	if _, err := s.db.ExecContext(ctx, `DELETE FROM thing_states WHERE thing = ? AND device_id = ?`, thing, deviceID); err != nil {
		return fmt.Errorf("db: delete state %s/%s: %w", thing, deviceID, err)
	}
	return nil
}

// DeleteThingStates removes every cached state of a gateway.
func (s *Store) DeleteThingStates(ctx context.Context, thing string) error {
	if _, err := s.db.ExecContext(ctx, `DELETE FROM thing_states WHERE thing = ?`, thing); err != nil {
		return fmt.Errorf("db: delete states %s: %w", thing, err)
	}
	return nil
}
