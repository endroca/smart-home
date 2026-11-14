package db

import (
	"context"
	"fmt"
	"time"
)

// Placement kinds.
const (
	PlacementFan = "fan"
)

// Placement assigns a logical sub-device (for example a fan identified by its
// thing and device id) to a room.
type Placement struct {
	Kind   string `json:"kind"`
	Owner  string `json:"owner"`
	Device string `json:"device"`
	RoomID string `json:"room_id"`
}

// SetPlacement assigns a sub-device to a room. An empty roomID removes the
// assignment.
func (s *Store) SetPlacement(ctx context.Context, p Placement) error {
	if p.Owner == "" && p.Device == "" {
		return fmt.Errorf("db: placement needs an owner or a device")
	}
	if p.RoomID == "" {
		return s.DeletePlacement(ctx, p.Kind, p.Owner, p.Device)
	}
	_, err := s.db.ExecContext(ctx, `
INSERT INTO placements (kind, owner, device, room_id, updated_at)
VALUES (?, ?, ?, ?, ?)
ON CONFLICT(kind, owner, device) DO UPDATE SET
    room_id    = excluded.room_id,
    updated_at = excluded.updated_at`,
		p.Kind, p.Owner, p.Device, p.RoomID, formatTime(time.Now().UTC()))
	if err != nil {
		return fmt.Errorf("db: set placement %s/%s/%s: %w", p.Kind, p.Owner, p.Device, err)
	}
	return nil
}

// DeletePlacement removes a placement.
func (s *Store) DeletePlacement(ctx context.Context, kind, owner, device string) error {
	if _, err := s.db.ExecContext(ctx,
		`DELETE FROM placements WHERE kind = ? AND owner = ? AND device = ?`, kind, owner, device); err != nil {
		return fmt.Errorf("db: delete placement: %w", err)
	}
	return nil
}

// Placements returns every placement.
func (s *Store) Placements(ctx context.Context) ([]Placement, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT kind, owner, device, room_id FROM placements`)
	if err != nil {
		return nil, fmt.Errorf("db: list placements: %w", err)
	}
	defer rows.Close()
	out := []Placement{}
	for rows.Next() {
		var p Placement
		if err := rows.Scan(&p.Kind, &p.Owner, &p.Device, &p.RoomID); err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

// PlacementsByOwner returns the placements owned by one thing.
func (s *Store) PlacementsByOwner(ctx context.Context, owner string) ([]Placement, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT kind, owner, device, room_id FROM placements WHERE owner = ?`, owner)
	if err != nil {
		return nil, fmt.Errorf("db: list placements for %s: %w", owner, err)
	}
	defer rows.Close()
	out := []Placement{}
	for rows.Next() {
		var p Placement
		if err := rows.Scan(&p.Kind, &p.Owner, &p.Device, &p.RoomID); err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

// DeletePlacementsByOwner removes every placement of a thing.
func (s *Store) DeletePlacementsByOwner(ctx context.Context, owner string) error {
	if _, err := s.db.ExecContext(ctx, `DELETE FROM placements WHERE owner = ?`, owner); err != nil {
		return fmt.Errorf("db: delete placements for %s: %w", owner, err)
	}
	return nil
}
