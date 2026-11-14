package db

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"time"
)

// MideaSpec describes one action supported by a locally controlled appliance.
type MideaSpec struct {
	DeviceID string          `json:"device_id"`
	ID       string          `json:"key"`
	Name     string          `json:"label"`
	Type     string          `json:"type"`
	Values   json.RawMessage `json:"options,omitempty"`
	Writable bool            `json:"writable"`
}

// ReplaceMideaSpecs saves the actions discovered from one Midea appliance.
func (s *Store) ReplaceMideaSpecs(ctx context.Context, deviceID string, specs []MideaSpec) error {
	return s.inTx(ctx, func(tx *sql.Tx) error {
		if _, err := tx.ExecContext(ctx, `DELETE FROM midea_specs WHERE device_id=?`, deviceID); err != nil {
			return err
		}
		now := formatTime(time.Now().UTC())
		for _, spec := range specs {
			values := string(spec.Values)
			if values == "" {
				values = "null"
			}
			if _, err := tx.ExecContext(ctx, `INSERT INTO midea_specs(device_id,spec_id,name,type,values_json,writable,updated_at) VALUES(?,?,?,?,?,?,?)`, deviceID, spec.ID, spec.Name, spec.Type, values, boolToInt(spec.Writable), now); err != nil {
				return fmt.Errorf("db: save Midea spec %s/%s: %w", deviceID, spec.ID, err)
			}
		}
		return nil
	})
}

func (s *Store) ListMideaSpecs(ctx context.Context, deviceID string) ([]MideaSpec, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT device_id,spec_id,name,type,values_json,writable FROM midea_specs WHERE device_id=? ORDER BY spec_id`, deviceID)
	if err != nil {
		return nil, fmt.Errorf("db: list Midea specs: %w", err)
	}
	defer rows.Close()
	out := []MideaSpec{}
	for rows.Next() {
		var spec MideaSpec
		var values string
		var writable int
		if err := rows.Scan(&spec.DeviceID, &spec.ID, &spec.Name, &spec.Type, &values, &writable); err != nil {
			return nil, err
		}
		if values != "" && values != "null" {
			spec.Values = json.RawMessage(values)
		}
		spec.Writable = writable != 0
		out = append(out, spec)
	}
	return out, rows.Err()
}

func (s *Store) SetMideaStates(ctx context.Context, deviceID string, states map[string]any) error {
	if len(states) == 0 {
		return nil
	}
	return s.inTx(ctx, func(tx *sql.Tx) error {
		now := formatTime(time.Now().UTC())
		for key, value := range states {
			raw, err := json.Marshal(value)
			if err != nil {
				return err
			}
			if _, err := tx.ExecContext(ctx, `INSERT INTO midea_states(device_id,state_key,value,updated_at) VALUES(?,?,?,?) ON CONFLICT(device_id,state_key) DO UPDATE SET value=excluded.value,updated_at=excluded.updated_at`, deviceID, key, string(raw), now); err != nil {
				return fmt.Errorf("db: save Midea state %s/%s: %w", deviceID, key, err)
			}
		}
		return nil
	})
}

func (s *Store) MideaStates(ctx context.Context, deviceID string) (map[string]any, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT state_key,value FROM midea_states WHERE device_id=? ORDER BY state_key`, deviceID)
	if err != nil {
		return nil, fmt.Errorf("db: list Midea states: %w", err)
	}
	defer rows.Close()
	out := map[string]any{}
	for rows.Next() {
		var key, raw string
		if err := rows.Scan(&key, &raw); err != nil {
			return nil, err
		}
		out[key] = decodeState(raw)
	}
	return out, rows.Err()
}
