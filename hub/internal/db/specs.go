package db

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"time"
)

// Spec is one cached Tuya data-point specification for a device.
type Spec struct {
	DeviceID string          `json:"device_id"`
	DPID     string          `json:"dp_id"`
	Code     string          `json:"code"`
	Name     string          `json:"name,omitempty"`
	Desc     string          `json:"desc,omitempty"`
	Type     string          `json:"type"`
	Values   json.RawMessage `json:"values,omitempty"`
	Writable bool            `json:"writable"`
}

// ReplaceSpecs replaces every cached spec of one device.
func (s *Store) ReplaceSpecs(ctx context.Context, deviceID string, specs []Spec) error {
	return s.inTx(ctx, func(tx *sql.Tx) error {
		if _, err := tx.ExecContext(ctx, `DELETE FROM tuya_specs WHERE device_id = ?`, deviceID); err != nil {
			return fmt.Errorf("db: clear specs %s: %w", deviceID, err)
		}
		now := formatTime(time.Now().UTC())
		for _, sp := range specs {
			values := "null"
			if len(sp.Values) > 0 {
				values = string(sp.Values)
			}
			if _, err := tx.ExecContext(ctx, `
INSERT OR REPLACE INTO tuya_specs (device_id, dp_id, code, name, descr, type, values_json, writable, updated_at)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
				deviceID, sp.DPID, sp.Code, sp.Name, sp.Desc, sp.Type, values, boolToInt(sp.Writable), now); err != nil {
				return fmt.Errorf("db: insert spec %s/%s: %w", deviceID, sp.DPID, err)
			}
		}
		return nil
	})
}

// ListSpecs returns the cached specs of one device ordered by dp id.
func (s *Store) ListSpecs(ctx context.Context, deviceID string) ([]Spec, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT device_id, dp_id, code, name, descr, type, values_json, writable FROM tuya_specs WHERE device_id = ? ORDER BY dp_id`, deviceID)
	if err != nil {
		return nil, fmt.Errorf("db: list specs %s: %w", deviceID, err)
	}
	defer rows.Close()
	out := []Spec{}
	for rows.Next() {
		var (
			sp       Spec
			values   string
			writable int
		)
		if err := rows.Scan(&sp.DeviceID, &sp.DPID, &sp.Code, &sp.Name, &sp.Desc, &sp.Type, &values, &writable); err != nil {
			return nil, err
		}
		if values != "" && values != "null" {
			sp.Values = json.RawMessage(values)
		}
		sp.Writable = writable != 0
		out = append(out, sp)
	}
	return out, rows.Err()
}
