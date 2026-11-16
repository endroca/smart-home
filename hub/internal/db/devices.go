package db

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"
)

// Device kinds.
const (
	KindTuya  = "tuya"
	KindMidea = "midea"
	KindThing = "thing"
	KindFan   = "fan"
)

// Device is one addressable device stored by the hub.
//
// Tuya equipment uses the LAN fields (IP, LocalKey, ...); ESP32 things use the
// LAN fallback fields (Token, URL). A row is identified by its unique ID, which
// is the Tuya device ID or the thing name respectively.
type Device struct {
	ID           string    `json:"id"`
	Kind         string    `json:"kind"`
	Name         string    `json:"name"`
	FirmwareName string    `json:"firmware_name,omitempty"`
	IP           string    `json:"ip"`
	Port         int       `json:"port"`
	Version      string    `json:"version"`
	LocalKey     string    `json:"local_key,omitempty"`
	ProductKey   string    `json:"product_key,omitempty"`
	Device22     bool      `json:"device22"`
	KeySource    string    `json:"key_source,omitempty"`
	KeyUpdatedAt time.Time `json:"key_updated_at,omitempty"`
	Token        string    `json:"token,omitempty"`
	URL          string    `json:"url,omitempty"`
	RoomID       string    `json:"room_id,omitempty"`
	Owner        string    `json:"owner,omitempty"`
	DeviceID     string    `json:"device_id,omitempty"`
	Template     string    `json:"template,omitempty"`
	// Category is the Tuya product category (for example "jtmspro" for a Wi-Fi
	// smart lock); empty for non-Tuya devices.
	Category string `json:"category,omitempty"`
	// CloudOnly marks a device reached through the Tuya cloud instead of the
	// LAN: the hub does not open a LAN listener for it and polls its state from
	// the cloud. Used for battery devices that sleep (locks).
	CloudOnly bool `json:"cloud_only,omitempty"`
	// Sort is the display order of the device inside its room (lower first).
	Sort int `json:"sort"`
	// FunctionsJSON stores the per-device function labels the voice resolver
	// and "desligar tudo" use. An empty string means "derive the defaults"; an
	// empty JSON array means "configured, with no functions". Opaque here.
	FunctionsJSON string    `json:"functions,omitempty"`
	CreatedAt     time.Time `json:"created_at,omitempty"`
	UpdatedAt     time.Time `json:"updated_at,omitempty"`
}

// FanID is the stable id of a discovered sub-device of a gateway.
func FanID(owner, deviceID string) string { return owner + "/" + deviceID }

const deviceCols = `id, kind, name, firmware_name, ip, port, version, local_key, product_key, device22, key_source, key_updated_at, token, url, room_id, owner, device_id, template, created_at, updated_at, category, cloud_only, sort, functions_json`

// UpsertDevice writes a device, merging with any existing row. Empty fields do
// not overwrite a stored value, so a scan (no key) never erases a key learned
// from the cloud and a cloud registration (no IP) never erases a scanned IP.
// Use SetDeviceRoom to clear a room assignment explicitly.
func (s *Store) UpsertDevice(ctx context.Context, d Device) error {
	if strings.TrimSpace(d.ID) == "" {
		return errors.New("db: device id is required")
	}
	if d.Kind == "" {
		d.Kind = KindTuya
	}
	now := time.Now().UTC()
	keyStamp := ""
	if d.LocalKey != "" {
		if d.KeyUpdatedAt.IsZero() {
			d.KeyUpdatedAt = now
		}
		keyStamp = formatTime(d.KeyUpdatedAt)
	}
	_, err := s.db.ExecContext(ctx, `
INSERT INTO devices (id, kind, name, firmware_name, ip, port, version, local_key, product_key, device22, key_source, key_updated_at, token, url, room_id, owner, device_id, template, created_at, updated_at, category, cloud_only)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
ON CONFLICT(id) DO UPDATE SET
    kind           = CASE WHEN excluded.kind <> '' THEN excluded.kind ELSE devices.kind END,
    name           = CASE WHEN excluded.name <> '' THEN excluded.name ELSE devices.name END,
    firmware_name  = CASE WHEN excluded.firmware_name <> '' THEN excluded.firmware_name ELSE devices.firmware_name END,
    ip             = CASE WHEN excluded.ip <> '' THEN excluded.ip ELSE devices.ip END,
    port           = excluded.port,
    version        = CASE WHEN excluded.version <> '' AND excluded.version <> 'auto' THEN excluded.version ELSE devices.version END,
    local_key      = CASE WHEN excluded.local_key <> '' THEN excluded.local_key ELSE devices.local_key END,
    product_key    = CASE WHEN excluded.product_key <> '' THEN excluded.product_key ELSE devices.product_key END,
    device22       = excluded.device22,
    key_source     = CASE WHEN excluded.local_key <> '' THEN excluded.key_source ELSE devices.key_source END,
    key_updated_at = CASE WHEN excluded.local_key <> '' THEN excluded.key_updated_at ELSE devices.key_updated_at END,
    token          = CASE WHEN excluded.token <> '' THEN excluded.token ELSE devices.token END,
    url            = CASE WHEN excluded.url <> '' THEN excluded.url ELSE devices.url END,
    room_id        = CASE WHEN excluded.room_id <> '' THEN excluded.room_id ELSE devices.room_id END,
    owner          = CASE WHEN excluded.owner <> '' THEN excluded.owner ELSE devices.owner END,
    device_id      = CASE WHEN excluded.device_id <> '' THEN excluded.device_id ELSE devices.device_id END,
    template       = CASE WHEN excluded.template <> '' THEN excluded.template ELSE devices.template END,
    category       = CASE WHEN excluded.category <> '' THEN excluded.category ELSE devices.category END,
    updated_at     = excluded.updated_at`,
		d.ID, d.Kind, d.Name, d.FirmwareName, d.IP, d.Port, d.Version, d.LocalKey, d.ProductKey, boolToInt(d.Device22),
		d.KeySource, keyStamp, d.Token, d.URL, d.RoomID, d.Owner, d.DeviceID, d.Template, formatTime(now), formatTime(now),
		d.Category, boolToInt(d.CloudOnly))
	if err != nil {
		return fmt.Errorf("db: upsert device %s: %w", d.ID, err)
	}
	return nil
}

// SetDeviceKey stores a device's local key. It fails if the device is unknown.
func (s *Store) SetDeviceKey(ctx context.Context, id, key, source string) error {
	if strings.TrimSpace(id) == "" {
		return errors.New("db: device id is required")
	}
	if strings.TrimSpace(key) == "" {
		return errors.New("db: local key is required")
	}
	now := formatTime(time.Now().UTC())
	res, err := s.db.ExecContext(ctx,
		`UPDATE devices SET local_key = ?, key_source = ?, key_updated_at = ?, updated_at = ? WHERE id = ?`,
		key, source, now, now, id)
	if err != nil {
		return fmt.Errorf("db: set key %s: %w", id, err)
	}
	return ensureAffected(res, id)
}

// SetDeviceIP updates the LAN address of a device.
func (s *Store) SetDeviceIP(ctx context.Context, id, ip string) error {
	if strings.TrimSpace(id) == "" {
		return errors.New("db: device id is required")
	}
	if strings.TrimSpace(ip) == "" {
		return errors.New("db: ip is required")
	}
	res, err := s.db.ExecContext(ctx,
		`UPDATE devices SET ip = ?, updated_at = ? WHERE id = ?`,
		ip, formatTime(time.Now().UTC()), id)
	if err != nil {
		return fmt.Errorf("db: set ip %s: %w", id, err)
	}
	return ensureAffected(res, id)
}

// SetDeviceToken sets the LAN fallback bearer token of an ESP32 thing.
func (s *Store) SetDeviceToken(ctx context.Context, id, token string) error {
	return s.setDeviceField(ctx, id, "token", token)
}

// SetDeviceName renames a device without touching its kind.
func (s *Store) SetDeviceName(ctx context.Context, id, name string) error {
	return s.setDeviceField(ctx, id, "name", name)
}

// RenameFan changes the hub display name of a fan, leaving the firmware-side
// name untouched.
func (s *Store) RenameFan(ctx context.Context, owner, deviceID, name string) error {
	return s.setDeviceField(ctx, FanID(owner, deviceID), "name", name)
}

// SetFanFirmwareName updates the device-side name of a fan without touching the
// hub display name.
func (s *Store) SetFanFirmwareName(ctx context.Context, owner, deviceID, name string) error {
	return s.setDeviceField(ctx, FanID(owner, deviceID), "firmware_name", name)
}

// SetDeviceVersion caches the detected protocol version of a device.
func (s *Store) SetDeviceVersion(ctx context.Context, id, version string) error {
	return s.setDeviceField(ctx, id, "version", version)
}

// SetDeviceURL sets the optional LAN URL override of a thing.
func (s *Store) SetDeviceURL(ctx context.Context, id, url string) error {
	return s.setDeviceField(ctx, id, "url", url)
}

// SetDeviceRoom assigns (or clears with an empty roomID) a device's room. When
// the room changes, the device is appended to the end of the destination room's
// order, so it does not jump in front of devices the operator already arranged.
func (s *Store) SetDeviceRoom(ctx context.Context, id, roomID string) error {
	if strings.TrimSpace(id) == "" {
		return errors.New("db: device id is required")
	}
	var current string
	if err := s.db.QueryRowContext(ctx, `SELECT room_id FROM devices WHERE id = ?`, id).Scan(&current); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return fmt.Errorf("%w: %s", ErrNotFound, id)
		}
		return fmt.Errorf("db: read room of %s: %w", id, err)
	}
	if current == roomID {
		return nil
	}
	now := formatTime(time.Now().UTC())
	res, err := s.db.ExecContext(ctx, `
UPDATE devices SET
    room_id = ?,
    sort = COALESCE((SELECT MAX(sort) FROM devices WHERE room_id = ? AND id <> ?), -1) + 1,
    updated_at = ?
WHERE id = ?`, roomID, roomID, id, now, id)
	if err != nil {
		return fmt.Errorf("db: set room %s: %w", id, err)
	}
	return ensureAffected(res, id)
}

// SetDeviceOrder writes the display order of one room's devices. ids lists the
// device ids in the wanted order, top first; every id must exist and belong to
// the same room, otherwise nothing is written.
func (s *Store) SetDeviceOrder(ctx context.Context, ids []string) error {
	if len(ids) == 0 {
		return errors.New("db: device order is empty")
	}
	return s.inTx(ctx, func(tx *sql.Tx) error {
		room := ""
		roomSet := false
		seen := make(map[string]bool, len(ids))
		for _, id := range ids {
			if seen[id] {
				return fmt.Errorf("db: duplicate device %s in order", id)
			}
			seen[id] = true
			var r string
			if err := tx.QueryRowContext(ctx, `SELECT room_id FROM devices WHERE id = ?`, id).Scan(&r); err != nil {
				if errors.Is(err, sql.ErrNoRows) {
					return fmt.Errorf("%w: %s", ErrNotFound, id)
				}
				return fmt.Errorf("db: read room of %s: %w", id, err)
			}
			if !roomSet {
				room, roomSet = r, true
			} else if r != room {
				return errors.New("db: device order must be within a single room")
			}
		}
		now := formatTime(time.Now().UTC())
		for i, id := range ids {
			if _, err := tx.ExecContext(ctx,
				`UPDATE devices SET sort = ?, updated_at = ? WHERE id = ?`, i, now, id); err != nil {
				return fmt.Errorf("db: set order of %s: %w", id, err)
			}
		}
		return nil
	})
}

// SetDeviceFunctions stores the opaque function labels of a device. An empty
// value restores the derived defaults.
func (s *Store) SetDeviceFunctions(ctx context.Context, id, functionsJSON string) error {
	return s.setDeviceField(ctx, id, "functions_json", functionsJSON)
}

// SetDeviceTemplate sets (or clears with an empty value, meaning "infer") the
// UI layout template of a device.
func (s *Store) SetDeviceTemplate(ctx context.Context, id, template string) error {
	return s.setDeviceField(ctx, id, "template", template)
}

// SetDeviceCategory stores the Tuya product category of a device.
func (s *Store) SetDeviceCategory(ctx context.Context, id, category string) error {
	return s.setDeviceField(ctx, id, "category", category)
}

// SetDeviceCloudOnly marks a device as reached through the cloud, so the hub
// keeps no LAN listener open for it and polls its state from the cloud.
func (s *Store) SetDeviceCloudOnly(ctx context.Context, id string, cloudOnly bool) error {
	if strings.TrimSpace(id) == "" {
		return errors.New("db: device id is required")
	}
	res, err := s.db.ExecContext(ctx,
		`UPDATE devices SET cloud_only = ?, updated_at = ? WHERE id = ?`,
		boolToInt(cloudOnly), formatTime(time.Now().UTC()), id)
	if err != nil {
		return fmt.Errorf("db: set cloud_only %s: %w", id, err)
	}
	return ensureAffected(res, id)
}

func (s *Store) setDeviceField(ctx context.Context, id, col, value string) error {
	if strings.TrimSpace(id) == "" {
		return errors.New("db: device id is required")
	}
	res, err := s.db.ExecContext(ctx,
		`UPDATE devices SET `+col+` = ?, updated_at = ? WHERE id = ?`,
		value, formatTime(time.Now().UTC()), id)
	if err != nil {
		return fmt.Errorf("db: set %s %s: %w", col, id, err)
	}
	return ensureAffected(res, id)
}

// GetDevice returns a device by ID.
func (s *Store) GetDevice(ctx context.Context, id string) (Device, error) {
	return scanDevice(s.db.QueryRowContext(ctx, `SELECT `+deviceCols+` FROM devices WHERE id = ?`, id))
}

// GetDeviceByName returns a device by its local name.
func (s *Store) GetDeviceByName(ctx context.Context, name string) (Device, error) {
	return scanDevice(s.db.QueryRowContext(ctx, `SELECT `+deviceCols+` FROM devices WHERE name = ?`, name))
}

// ListDevices returns devices of one kind (kind=="" returns every device),
// sorted by name.
func (s *Store) ListDevices(ctx context.Context, kind string) ([]Device, error) {
	query := `SELECT ` + deviceCols + ` FROM devices`
	args := []any{}
	if kind != "" {
		query += ` WHERE kind = ?`
		args = append(args, kind)
	}
	query += ` ORDER BY name, id`
	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("db: list devices: %w", err)
	}
	defer rows.Close()
	out := []Device{}
	for rows.Next() {
		d, err := scanDevice(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, d)
	}
	return out, rows.Err()
}

// CountDevices returns the number of devices of a kind (kind=="" counts all).
func (s *Store) CountDevices(ctx context.Context, kind string) (int, error) {
	var n int
	var err error
	if kind == "" {
		err = s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM devices`).Scan(&n)
	} else {
		err = s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM devices WHERE kind = ?`, kind).Scan(&n)
	}
	if err != nil {
		return 0, fmt.Errorf("db: count devices: %w", err)
	}
	return n, nil
}

// DeleteDevice removes a device and its specs.
func (s *Store) DeleteDevice(ctx context.Context, id string) error {
	return s.inTx(ctx, func(tx *sql.Tx) error {
		res, err := tx.ExecContext(ctx, `DELETE FROM devices WHERE id = ?`, id)
		if err != nil {
			return fmt.Errorf("db: delete %s: %w", id, err)
		}
		if err := ensureAffected(res, id); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `DELETE FROM tuya_specs WHERE device_id = ?`, id); err != nil {
			return fmt.Errorf("db: delete specs %s: %w", id, err)
		}
		if _, err := tx.ExecContext(ctx, `DELETE FROM midea_specs WHERE device_id = ?`, id); err != nil {
			return fmt.Errorf("db: delete midea specs %s: %w", id, err)
		}
		if _, err := tx.ExecContext(ctx, `DELETE FROM midea_states WHERE device_id = ?`, id); err != nil {
			return fmt.Errorf("db: delete midea states %s: %w", id, err)
		}
		if _, err := tx.ExecContext(ctx, `DELETE FROM tuya_report_cursor WHERE device_id = ?`, id); err != nil {
			return fmt.Errorf("db: delete report cursor %s: %w", id, err)
		}
		// Deleting a gateway also drops the sub-devices it discovered.
		if _, err := tx.ExecContext(ctx, `DELETE FROM devices WHERE kind = ? AND owner = ?`, KindFan, id); err != nil {
			return fmt.Errorf("db: delete fans of %s: %w", id, err)
		}
		// Drop the cached state of the gateway itself and of its sub-devices
		// (a fan row is identified by owner/device_id).
		if _, err := tx.ExecContext(ctx, `DELETE FROM thing_states WHERE thing = ? OR thing || '/' || device_id = ?`, id, id); err != nil {
			return fmt.Errorf("db: delete states of %s: %w", id, err)
		}
		if _, err := tx.ExecContext(ctx, `DELETE FROM thing_status WHERE thing = ?`, id); err != nil {
			return fmt.Errorf("db: delete status of %s: %w", id, err)
		}
		return nil
	})
}

// DeviceToken returns the LAN bearer token for a thing ("" when unknown).
func (s *Store) DeviceToken(thing string) string {
	var token string
	_ = s.db.QueryRow(`SELECT token FROM devices WHERE id = ?`, thing).Scan(&token)
	return token
}

// DeviceURL returns the LAN URL override for a thing ("" when unknown).
func (s *Store) DeviceURL(thing string) string {
	var url string
	_ = s.db.QueryRow(`SELECT url FROM devices WHERE id = ?`, thing).Scan(&url)
	return url
}

func scanDevice(sc scanner) (Device, error) {
	var (
		d            Device
		device22     int
		cloudOnly    int
		keyUpdated   string
		created, upd string
	)
	if err := sc.Scan(&d.ID, &d.Kind, &d.Name, &d.FirmwareName, &d.IP, &d.Port, &d.Version, &d.LocalKey, &d.ProductKey,
		&device22, &d.KeySource, &keyUpdated, &d.Token, &d.URL, &d.RoomID, &d.Owner, &d.DeviceID, &d.Template, &created, &upd,
		&d.Category, &cloudOnly, &d.Sort, &d.FunctionsJSON); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return Device{}, ErrNotFound
		}
		return Device{}, err
	}
	d.Device22 = device22 != 0
	d.CloudOnly = cloudOnly != 0
	d.KeyUpdatedAt = parseTime(keyUpdated)
	d.CreatedAt = parseTime(created)
	d.UpdatedAt = parseTime(upd)
	return d, nil
}

type scanner interface {
	Scan(dest ...any) error
}

func ensureAffected(res sql.Result, id string) error {
	n, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if n == 0 {
		return fmt.Errorf("%w: %s", ErrNotFound, id)
	}
	return nil
}

// UpsertFan registers a sub-device discovered in a gateway manifest. The room
// assignment and the hub display name of an existing fan are preserved.
//
// firmwareName is the name the device uses; devices.name is the hub's display
// name and is independent of it. On insert both start equal, so a freshly
// discovered fan looks familiar; on update only firmware_name changes, so a
// rename on either side never leaks into the other.
func (s *Store) UpsertFan(ctx context.Context, owner, deviceID, firmwareName string) error {
	if owner == "" || deviceID == "" {
		return errors.New("db: fan needs an owner and a device id")
	}
	now := formatTime(time.Now().UTC())
	_, err := s.db.ExecContext(ctx, `
INSERT INTO devices (id, kind, owner, device_id, name, firmware_name, port, created_at, updated_at)
VALUES (?, ?, ?, ?, ?, ?, 0, ?, ?)
ON CONFLICT(id) DO UPDATE SET
    kind          = ?,
    firmware_name = CASE WHEN excluded.firmware_name <> '' THEN excluded.firmware_name ELSE devices.firmware_name END,
    updated_at    = excluded.updated_at`,
		FanID(owner, deviceID), KindFan, owner, deviceID, firmwareName, firmwareName, now, now,
		KindFan)
	if err != nil {
		return fmt.Errorf("db: upsert fan %s/%s: %w", owner, deviceID, err)
	}
	return nil
}

// DeleteFan removes a discovered sub-device. It is a no-op when the fan does
// not exist.
func (s *Store) DeleteFan(ctx context.Context, owner, deviceID string) error {
	if _, err := s.db.ExecContext(ctx, `DELETE FROM devices WHERE id = ? AND kind = ?`, FanID(owner, deviceID), KindFan); err != nil {
		return fmt.Errorf("db: delete fan %s/%s: %w", owner, deviceID, err)
	}
	if _, err := s.db.ExecContext(ctx, `DELETE FROM thing_states WHERE thing = ? AND device_id = ?`, owner, deviceID); err != nil {
		return fmt.Errorf("db: delete fan state %s/%s: %w", owner, deviceID, err)
	}
	return nil
}

// ListFansByOwner returns the sub-devices discovered on a gateway ordered by
// device id.
func (s *Store) ListFansByOwner(ctx context.Context, owner string) ([]Device, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT `+deviceCols+` FROM devices WHERE kind = ? AND owner = ? ORDER BY device_id`, KindFan, owner)
	if err != nil {
		return nil, fmt.Errorf("db: list fans of %s: %w", owner, err)
	}
	defer rows.Close()
	out := []Device{}
	for rows.Next() {
		d, err := scanDevice(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, d)
	}
	return out, rows.Err()
}
