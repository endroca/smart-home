package db

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"
)

// Room is a location (sala, quarto, cozinha, ...) that devices and fans belong
// to.
type Room struct {
	ID        string    `json:"id"`
	Name      string    `json:"name"`
	Icon      string    `json:"icon,omitempty"`
	Sort      int       `json:"sort"`
	CreatedAt time.Time `json:"created_at,omitempty"`
	UpdatedAt time.Time `json:"updated_at,omitempty"`
}

// UpsertRoom creates or updates a room. Name must not be empty.
func (s *Store) UpsertRoom(ctx context.Context, r Room) error {
	if strings.TrimSpace(r.ID) == "" {
		return errors.New("db: room id is required")
	}
	if strings.TrimSpace(r.Name) == "" {
		return errors.New("db: room name is required")
	}
	now := time.Now().UTC()
	_, err := s.db.ExecContext(ctx, `
INSERT INTO rooms (id, name, icon, sort, created_at, updated_at)
VALUES (?, ?, ?, ?, ?, ?)
ON CONFLICT(id) DO UPDATE SET
    name       = excluded.name,
    icon       = excluded.icon,
    sort       = excluded.sort,
    updated_at = excluded.updated_at`,
		r.ID, r.Name, r.Icon, r.Sort, formatTime(now), formatTime(now))
	if err != nil {
		return fmt.Errorf("db: upsert room %s: %w", r.ID, err)
	}
	return nil
}

// GetRoom returns a room by ID.
func (s *Store) GetRoom(ctx context.Context, id string) (Room, error) {
	var (
		r            Room
		created, upd string
	)
	err := s.db.QueryRowContext(ctx,
		`SELECT id, name, icon, sort, created_at, updated_at FROM rooms WHERE id = ?`, id).
		Scan(&r.ID, &r.Name, &r.Icon, &r.Sort, &created, &upd)
	if errors.Is(err, sql.ErrNoRows) {
		return Room{}, ErrNotFound
	}
	if err != nil {
		return Room{}, err
	}
	r.CreatedAt = parseTime(created)
	r.UpdatedAt = parseTime(upd)
	return r, nil
}

// ListRooms returns every room sorted by sort then name.
func (s *Store) ListRooms(ctx context.Context) ([]Room, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, name, icon, sort, created_at, updated_at FROM rooms ORDER BY sort, name, id`)
	if err != nil {
		return nil, fmt.Errorf("db: list rooms: %w", err)
	}
	defer rows.Close()
	out := []Room{}
	for rows.Next() {
		var (
			r            Room
			created, upd string
		)
		if err := rows.Scan(&r.ID, &r.Name, &r.Icon, &r.Sort, &created, &upd); err != nil {
			return nil, err
		}
		r.CreatedAt = parseTime(created)
		r.UpdatedAt = parseTime(upd)
		out = append(out, r)
	}
	return out, rows.Err()
}

// DeleteRoom removes a room and clears references to it from devices and
// placements.
func (s *Store) DeleteRoom(ctx context.Context, id string) error {
	return s.inTx(ctx, func(tx *sql.Tx) error {
		if _, err := tx.ExecContext(ctx, `UPDATE devices SET room_id = '' WHERE room_id = ?`, id); err != nil {
			return fmt.Errorf("db: clear device rooms %s: %w", id, err)
		}
		if _, err := tx.ExecContext(ctx, `UPDATE placements SET room_id = '' WHERE room_id = ?`, id); err != nil {
			return fmt.Errorf("db: clear placements %s: %w", id, err)
		}
		res, err := tx.ExecContext(ctx, `DELETE FROM rooms WHERE id = ?`, id)
		if err != nil {
			return fmt.Errorf("db: delete room %s: %w", id, err)
		}
		return ensureAffected(res, id)
	})
}
