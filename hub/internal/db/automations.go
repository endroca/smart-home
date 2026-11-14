package db

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
)

// Automation is a stored rule. Source and Actions are opaque JSON documents
// interpreted by the dispatch package, which keeps this package free of import
// cycles.
type Automation struct {
	ID        string          `json:"id"`
	Name      string          `json:"name"`
	Enabled   bool            `json:"enabled"`
	Once      bool            `json:"once"`
	Source    json.RawMessage `json:"source"`
	Schedule  json.RawMessage `json:"schedule"`
	Delay     json.RawMessage `json:"delay"`
	Actions   json.RawMessage `json:"actions"`
	CreatedAt time.Time       `json:"created_at,omitempty"`
	UpdatedAt time.Time       `json:"updated_at,omitempty"`
}

// UpsertAutomation creates or replaces an automation.
func (s *Store) UpsertAutomation(ctx context.Context, a Automation) error {
	if strings.TrimSpace(a.ID) == "" {
		return errors.New("db: automation id is required")
	}
	source := a.Source
	if len(source) == 0 {
		source = json.RawMessage("{}")
	}
	schedule := a.Schedule
	if len(schedule) == 0 {
		schedule = json.RawMessage("{}")
	}
	delay := a.Delay
	if len(delay) == 0 {
		delay = json.RawMessage("{}")
	}
	actions := a.Actions
	if len(actions) == 0 {
		actions = json.RawMessage("[]")
	}
	now := time.Now().UTC()
	_, err := s.db.ExecContext(ctx, `
INSERT INTO automations (id, name, enabled, once, source, schedule, delay, actions, created_at, updated_at)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
ON CONFLICT(id) DO UPDATE SET
    name       = excluded.name,
    enabled    = excluded.enabled,
    once       = excluded.once,
    source     = excluded.source,
    schedule   = excluded.schedule,
    delay      = excluded.delay,
    actions    = excluded.actions,
    updated_at = excluded.updated_at`,
		a.ID, a.Name, boolToInt(a.Enabled), boolToInt(a.Once), string(source), string(schedule), string(delay), string(actions), formatTime(now), formatTime(now))
	if err != nil {
		return fmt.Errorf("db: upsert automation %s: %w", a.ID, err)
	}
	return nil
}

// GetAutomation returns one automation by ID.
func (s *Store) GetAutomation(ctx context.Context, id string) (Automation, error) {
	return scanAutomation(s.db.QueryRowContext(ctx,
		`SELECT id, name, enabled, once, source, schedule, delay, actions, created_at, updated_at FROM automations WHERE id = ?`, id))
}

// ListAutomations returns every automation sorted by name.
func (s *Store) ListAutomations(ctx context.Context) ([]Automation, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, name, enabled, once, source, schedule, delay, actions, created_at, updated_at FROM automations ORDER BY name, id`)
	if err != nil {
		return nil, fmt.Errorf("db: list automations: %w", err)
	}
	defer rows.Close()
	out := []Automation{}
	for rows.Next() {
		a, err := scanAutomation(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	return out, rows.Err()
}

// DeleteAutomation removes an automation.
func (s *Store) DeleteAutomation(ctx context.Context, id string) error {
	if _, err := s.db.ExecContext(ctx, `DELETE FROM automations WHERE id = ?`, id); err != nil {
		return fmt.Errorf("db: delete automation %s: %w", id, err)
	}
	return nil
}

func scanAutomation(sc scanner) (Automation, error) {
	var (
		a                            Automation
		enabled, once                int
		source, schedule, delay, act string
		created, upd                 string
	)
	if err := sc.Scan(&a.ID, &a.Name, &enabled, &once, &source, &schedule, &delay, &act, &created, &upd); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return Automation{}, ErrNotFound
		}
		return Automation{}, err
	}
	a.Enabled = enabled != 0
	a.Once = once != 0
	a.Source = json.RawMessage(source)
	a.Schedule = json.RawMessage(schedule)
	a.Delay = json.RawMessage(delay)
	a.Actions = json.RawMessage(act)
	a.CreatedAt = parseTime(created)
	a.UpdatedAt = parseTime(upd)
	return a, nil
}
