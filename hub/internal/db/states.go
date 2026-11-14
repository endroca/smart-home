package db

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"time"
)

// SetStates upserts the last-known value of one or more data points of a
// device. The values are stored as JSON so booleans, numbers and strings round
// trip.
func (s *Store) SetStates(ctx context.Context, deviceID string, dps map[string]any) error {
	if len(dps) == 0 {
		return nil
	}
	return s.inTx(ctx, func(tx *sql.Tx) error {
		now := formatTime(time.Now().UTC())
		for dp, v := range dps {
			raw, err := json.Marshal(v)
			if err != nil {
				return fmt.Errorf("db: state %s/%s: %w", deviceID, dp, err)
			}
			if _, err := tx.ExecContext(ctx, `
INSERT INTO tuya_state (device_id, dp_id, value, updated_at) VALUES (?, ?, ?, ?)
ON CONFLICT(device_id, dp_id) DO UPDATE SET value = excluded.value, updated_at = excluded.updated_at`,
				deviceID, dp, string(raw), now); err != nil {
				return fmt.Errorf("db: upsert state %s/%s: %w", deviceID, dp, err)
			}
		}
		return nil
	})
}

// States returns the last-known data points of one device.
func (s *Store) States(ctx context.Context, deviceID string) (map[string]any, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT dp_id, value FROM tuya_state WHERE device_id = ? ORDER BY dp_id`, deviceID)
	if err != nil {
		return nil, fmt.Errorf("db: list states %s: %w", deviceID, err)
	}
	defer rows.Close()
	out := map[string]any{}
	for rows.Next() {
		var dp, raw string
		if err := rows.Scan(&dp, &raw); err != nil {
			return nil, err
		}
		out[dp] = decodeState(raw)
	}
	return out, rows.Err()
}

// AllStates returns the last-known data points of every device keyed by device
// id.
func (s *Store) AllStates(ctx context.Context) (map[string]map[string]any, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT device_id, dp_id, value FROM tuya_state`)
	if err != nil {
		return nil, fmt.Errorf("db: list states: %w", err)
	}
	defer rows.Close()
	out := map[string]map[string]any{}
	for rows.Next() {
		var id, dp, raw string
		if err := rows.Scan(&id, &dp, &raw); err != nil {
			return nil, err
		}
		if out[id] == nil {
			out[id] = map[string]any{}
		}
		out[id][dp] = decodeState(raw)
	}
	return out, rows.Err()
}

// ReplaceStates replaces every stored data point of one device (used after a
// full status refresh).
func (s *Store) ReplaceStates(ctx context.Context, deviceID string, dps map[string]any) error {
	return s.inTx(ctx, func(tx *sql.Tx) error {
		if _, err := tx.ExecContext(ctx, `DELETE FROM tuya_state WHERE device_id = ?`, deviceID); err != nil {
			return fmt.Errorf("db: clear states %s: %w", deviceID, err)
		}
		now := formatTime(time.Now().UTC())
		for dp, v := range dps {
			raw, err := json.Marshal(v)
			if err != nil {
				return fmt.Errorf("db: state %s/%s: %w", deviceID, dp, err)
			}
			if _, err := tx.ExecContext(ctx,
				`INSERT INTO tuya_state (device_id, dp_id, value, updated_at) VALUES (?, ?, ?, ?)`,
				deviceID, dp, string(raw), now); err != nil {
				return fmt.Errorf("db: insert state %s/%s: %w", deviceID, dp, err)
			}
		}
		return nil
	})
}

func decodeState(raw string) any {
	var v any
	if err := json.Unmarshal([]byte(raw), &v); err != nil {
		return raw
	}
	return v
}
