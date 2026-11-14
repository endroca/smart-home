// Package db is the hub's SQLite storage. It is the source of truth for the
// addressable devices (Tuya equipment and ESP32 things), the rooms they live
// in, the placements of individual fans, the automation rules, the cached
// Tuya data-point specifications and the durable replica of the state reported
// by a gateway's sub-devices.
//
// The database is versioned with PRAGMA user_version and migrated on open, so
// an existing registry created by earlier versions keeps its data.
package db

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	_ "modernc.org/sqlite"
)

// ErrNotFound is returned when a row does not exist.
var ErrNotFound = errors.New("db: not found")

// schemaVersion is the current on-disk schema version.
const schemaVersion = 17

// Store is a SQLite-backed hub store. It is safe for concurrent use.
type Store struct {
	db *sql.DB
}

// Open creates or opens the database at path, applying migrations.
func Open(path string) (*Store, error) {
	if strings.TrimSpace(path) == "" {
		return nil, errors.New("db: database path is required")
	}
	if dir := filepath.Dir(path); dir != "" && dir != "." {
		if err := os.MkdirAll(dir, 0o700); err != nil {
			return nil, fmt.Errorf("db: %w", err)
		}
	}
	dsn := "file:" + path + "?_pragma=busy_timeout(5000)&_pragma=journal_mode(WAL)&_pragma=foreign_keys(1)"
	sqlDB, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("db: open %s: %w", path, err)
	}
	// A single connection avoids SQLITE_BUSY between the hub and any tooling.
	sqlDB.SetMaxOpenConns(1)
	s := &Store{db: sqlDB}
	if err := s.migrate(); err != nil {
		_ = sqlDB.Close()
		return nil, err
	}
	// The database stores local keys and per-device tokens; SQLite copies the
	// file mode to its -wal/-shm sidecars, so a private main file covers them.
	for _, p := range []string{path, path + "-wal", path + "-shm"} {
		_ = os.Chmod(p, 0o600)
	}
	return s, nil
}

// Close releases the database.
func (s *Store) Close() error {
	if s == nil || s.db == nil {
		return nil
	}
	return s.db.Close()
}

func (s *Store) migrate() error {
	var v int
	if err := s.db.QueryRow("PRAGMA user_version").Scan(&v); err != nil {
		return fmt.Errorf("db: read user_version: %w", err)
	}
	if v < 1 {
		if _, err := s.db.Exec(schemaV1); err != nil {
			return fmt.Errorf("db: schema v1: %w", err)
		}
	}
	if v < 2 {
		if err := s.migrateV2(); err != nil {
			return err
		}
	}
	if v < 3 {
		if _, err := s.db.Exec(schemaV3); err != nil {
			return fmt.Errorf("db: schema v3: %w", err)
		}
	}
	if v < 4 {
		if err := s.migrateV4(); err != nil {
			return err
		}
	}
	if v < 5 {
		// The port belongs to the Tuya LAN protocol only; clear the default
		// that was stored for gateways and fans.
		if _, err := s.db.Exec(`UPDATE devices SET port = 0 WHERE kind <> 'tuya'`); err != nil {
			return fmt.Errorf("db: schema v5: %w", err)
		}
	}
	if v < 6 {
		if err := s.migrateV6(); err != nil {
			return err
		}
	}
	if v < 7 {
		if err := s.migrateV7(); err != nil {
			return err
		}
	}
	if v < 8 {
		if _, err := s.db.Exec(schemaV8); err != nil {
			return fmt.Errorf("db: schema v8: %w", err)
		}
	}
	if v < 9 {
		if _, err := s.db.Exec(schemaV9); err != nil {
			return fmt.Errorf("db: schema v9: %w", err)
		}
	}
	if v < 10 {
		if _, err := s.db.Exec(schemaV10); err != nil {
			return fmt.Errorf("db: schema v10: %w", err)
		}
	}
	if v < 12 {
		if err := s.migrateV12(); err != nil {
			return err
		}
	}
	if v < 13 {
		if err := s.migrateV13(); err != nil {
			return err
		}
	}
	if v < 14 {
		if err := s.migrateV14(); err != nil {
			return err
		}
	}
	if v < 15 {
		if err := s.migrateV15(); err != nil {
			return err
		}
	}
	if v < 16 {
		if err := s.migrateV16(); err != nil {
			return err
		}
	}
	if v < 17 {
		if err := s.migrateV17(); err != nil {
			return err
		}
	}
	if _, err := s.db.Exec(fmt.Sprintf("PRAGMA user_version = %d", schemaVersion)); err != nil {
		return fmt.Errorf("db: set user_version: %w", err)
	}
	return nil
}

// schemaV1 is the original registry schema. It is kept verbatim so databases
// created by earlier versions migrate cleanly.
const schemaV1 = `
CREATE TABLE IF NOT EXISTS devices (
    id             TEXT PRIMARY KEY,
    name           TEXT NOT NULL DEFAULT '',
    ip             TEXT NOT NULL DEFAULT '',
    port           INTEGER NOT NULL DEFAULT 6668,
    version        TEXT NOT NULL DEFAULT '',
    local_key      TEXT NOT NULL DEFAULT '',
    product_key    TEXT NOT NULL DEFAULT '',
    device22       INTEGER NOT NULL DEFAULT 0,
    key_source     TEXT NOT NULL DEFAULT '',
    key_updated_at TEXT NOT NULL DEFAULT '',
    created_at     TEXT NOT NULL DEFAULT '',
    updated_at     TEXT NOT NULL DEFAULT ''
);
CREATE INDEX IF NOT EXISTS devices_name_idx ON devices(name);
`

// schemaV2 adds the generalized columns and the new tables.
const schemaV2 = `
CREATE TABLE IF NOT EXISTS rooms (
    id         TEXT PRIMARY KEY,
    name       TEXT NOT NULL DEFAULT '',
    icon       TEXT NOT NULL DEFAULT '',
    sort       INTEGER NOT NULL DEFAULT 0,
    created_at TEXT NOT NULL DEFAULT '',
    updated_at TEXT NOT NULL DEFAULT ''
);
CREATE INDEX IF NOT EXISTS rooms_sort_idx ON rooms(sort, name);

CREATE TABLE IF NOT EXISTS placements (
    kind       TEXT NOT NULL,
    owner      TEXT NOT NULL DEFAULT '',
    device     TEXT NOT NULL DEFAULT '',
    room_id    TEXT NOT NULL DEFAULT '',
    updated_at TEXT NOT NULL DEFAULT '',
    PRIMARY KEY (kind, owner, device)
);
CREATE INDEX IF NOT EXISTS placements_room_idx ON placements(room_id);

CREATE TABLE IF NOT EXISTS automations (
    id         TEXT PRIMARY KEY,
    name       TEXT NOT NULL DEFAULT '',
    enabled    INTEGER NOT NULL DEFAULT 1,
    source     TEXT NOT NULL DEFAULT '{}',
    actions    TEXT NOT NULL DEFAULT '[]',
    created_at TEXT NOT NULL DEFAULT '',
    updated_at TEXT NOT NULL DEFAULT ''
);

CREATE TABLE IF NOT EXISTS tuya_specs (
    device_id  TEXT NOT NULL,
    dp_id      TEXT NOT NULL,
    code       TEXT NOT NULL DEFAULT '',
    type       TEXT NOT NULL DEFAULT '',
    values_json TEXT NOT NULL DEFAULT '',
    writable   INTEGER NOT NULL DEFAULT 0,
    updated_at TEXT NOT NULL DEFAULT '',
    PRIMARY KEY (device_id, dp_id)
);
`

// schemaV3 adds the last-known DP state per device.
const schemaV3 = `
CREATE TABLE IF NOT EXISTS tuya_state (
    device_id  TEXT NOT NULL,
    dp_id      TEXT NOT NULL,
    value      TEXT NOT NULL DEFAULT 'null',
    updated_at TEXT NOT NULL DEFAULT '',
    PRIMARY KEY (device_id, dp_id)
);
`

const schemaV8 = `
CREATE TABLE IF NOT EXISTS midea_specs (
    device_id TEXT NOT NULL,
    spec_id TEXT NOT NULL,
    name TEXT NOT NULL DEFAULT '',
    type TEXT NOT NULL DEFAULT '',
    values_json TEXT NOT NULL DEFAULT 'null',
    writable INTEGER NOT NULL DEFAULT 1,
    updated_at TEXT NOT NULL DEFAULT '',
    PRIMARY KEY (device_id, spec_id)
);
CREATE TABLE IF NOT EXISTS midea_states (
    device_id TEXT NOT NULL,
    state_key TEXT NOT NULL,
    value TEXT NOT NULL DEFAULT 'null',
    updated_at TEXT NOT NULL DEFAULT '',
    PRIMARY KEY (device_id, state_key)
);
`

// schemaV9 adds the durable replica of the state reported by a gateway's
// sub-devices (for example the fans of an ESP32). The hub keeps the hot copy in
// memory; this table is what lets it render the inventory immediately after a
// restart, before the LAN probe answers.
const schemaV9 = `
CREATE TABLE IF NOT EXISTS thing_states (
    thing       TEXT NOT NULL,
    device_id   TEXT NOT NULL,
    reported    TEXT NOT NULL DEFAULT '{}',
    source      TEXT NOT NULL DEFAULT '',
    device_ts   INTEGER NOT NULL DEFAULT 0,
    received_at TEXT NOT NULL DEFAULT '',
    PRIMARY KEY (thing, device_id)
);
CREATE INDEX IF NOT EXISTS thing_states_thing_idx ON thing_states(thing);
`

// schemaV10 adds the durable replica of a gateway's own status (liveness and
// diagnostics). Presence itself is not trusted across a restart; the persisted
// value is seeded with online=false until the gateway is heard from again.
const schemaV10 = `
CREATE TABLE IF NOT EXISTS thing_status (
    thing            TEXT PRIMARY KEY,
    status           TEXT NOT NULL DEFAULT '{}',
    manifest_version INTEGER NOT NULL DEFAULT 0,
    last_seen        TEXT NOT NULL DEFAULT ''
);
`

func (s *Store) migrateV2() error {
	existing, err := s.columns("devices")
	if err != nil {
		return err
	}
	adds := []struct{ name, def string }{
		{"kind", "TEXT NOT NULL DEFAULT 'tuya'"},
		{"token", "TEXT NOT NULL DEFAULT ''"},
		{"url", "TEXT NOT NULL DEFAULT ''"},
		{"room_id", "TEXT NOT NULL DEFAULT ''"},
	}
	for _, a := range adds {
		if existing[a.name] {
			continue
		}
		if _, err := s.db.Exec("ALTER TABLE devices ADD COLUMN " + a.name + " " + a.def); err != nil {
			return fmt.Errorf("db: add devices.%s: %w", a.name, err)
		}
	}
	if _, err := s.db.Exec(schemaV2); err != nil {
		return fmt.Errorf("db: schema v2: %w", err)
	}
	return nil
}

// migrateV4 adds the owner/device_id columns and seeds fan rows from the legacy
// placements table.
func (s *Store) migrateV4() error {
	cols, err := s.columns("devices")
	if err != nil {
		return err
	}
	if !cols["owner"] {
		if _, err := s.db.Exec("ALTER TABLE devices ADD COLUMN owner TEXT NOT NULL DEFAULT ''"); err != nil {
			return fmt.Errorf("db: add devices.owner: %w", err)
		}
	}
	if !cols["device_id"] {
		if _, err := s.db.Exec("ALTER TABLE devices ADD COLUMN device_id TEXT NOT NULL DEFAULT ''"); err != nil {
			return fmt.Errorf("db: add devices.device_id: %w", err)
		}
	}
	// Seed fan rows from the legacy placements table when it exists, so rooms
	// assigned before fans became registered devices are not lost.
	if pcols, err := s.columns("placements"); err == nil && len(pcols) > 0 {
		now := formatTime(time.Now().UTC())
		if _, err := s.db.Exec(`INSERT OR IGNORE INTO devices (id, kind, owner, device_id, name, port, room_id, created_at, updated_at)
			SELECT owner || '/' || device, 'fan', owner, device, device, 0, room_id, ?, ?
			FROM placements WHERE kind = 'fan' AND owner <> '' AND device <> ''`, now, now); err != nil {
			return fmt.Errorf("db: seed fan rows: %w", err)
		}
	}
	return nil
}

// migrateV6 adds the UI layout template chosen for a device. An empty value
// means "infer from the device kind/capabilities", so existing rows keep
// working without a layout decision.
func (s *Store) migrateV6() error {
	cols, err := s.columns("devices")
	if err != nil {
		return err
	}
	if !cols["template"] {
		if _, err := s.db.Exec("ALTER TABLE devices ADD COLUMN template TEXT NOT NULL DEFAULT ''"); err != nil {
			return fmt.Errorf("db: add devices.template: %w", err)
		}
	}
	return nil
}

// migrateV7 adds the optional DP name/description columns to the cached specs,
// so the UI can show the instruction name the Tuya cloud exposes per data
// point (the LAN protocol carries only ids).
func (s *Store) migrateV7() error {
	cols, err := s.columns("tuya_specs")
	if err != nil {
		return err
	}
	adds := []struct{ name, def string }{
		{"name", "TEXT NOT NULL DEFAULT ''"},
		{"descr", "TEXT NOT NULL DEFAULT ''"},
	}
	for _, a := range adds {
		if cols[a.name] {
			continue
		}
		if _, err := s.db.Exec("ALTER TABLE tuya_specs ADD COLUMN " + a.name + " " + a.def); err != nil {
			return fmt.Errorf("db: add tuya_specs.%s: %w", a.name, err)
		}
	}
	return nil
}

// migrateV12 adds the firmware-side name of a sub-device. The hub keeps its own
// display name in devices.name: a rename on either side must not leak into the
// other. On a fresh database the INSERT seeds both columns from the manifest.
func (s *Store) migrateV12() error {
	cols, err := s.columns("devices")
	if err != nil {
		return err
	}
	if !cols["firmware_name"] {
		if _, err := s.db.Exec("ALTER TABLE devices ADD COLUMN firmware_name TEXT NOT NULL DEFAULT ''"); err != nil {
			return fmt.Errorf("db: add devices.firmware_name: %w", err)
		}
	}
	return nil
}

// migrateV13 adds the Tuya product category and the cloud-only flag. The
// category (for example "jtmspro" for a Wi-Fi smart lock) lets the hub and the
// UI recognize a device family that must be reached through the cloud instead
// of the LAN. cloud_only marks a device the hub should not open a LAN listener
// for (battery devices that sleep); its state is polled from the cloud instead.
func (s *Store) migrateV13() error {
	cols, err := s.columns("devices")
	if err != nil {
		return err
	}
	adds := []struct{ name, def string }{
		{"category", "TEXT NOT NULL DEFAULT ''"},
		{"cloud_only", "INTEGER NOT NULL DEFAULT 0"},
	}
	for _, a := range adds {
		if cols[a.name] {
			continue
		}
		if _, err := s.db.Exec("ALTER TABLE devices ADD COLUMN " + a.name + " " + a.def); err != nil {
			return fmt.Errorf("db: add devices.%s: %w", a.name, err)
		}
	}
	return nil
}

// migrateV14 adds the report-log cursor of a cloud_only device. The Tuya
// /report-logs endpoint returns a moving window; the cursor (the last event_time
// already processed) lets the hub turn each report into an event exactly once,
// even across restarts, so an automation on an unlock does not replay history.
func (s *Store) migrateV14() error {
	if _, err := s.db.Exec(`CREATE TABLE IF NOT EXISTS tuya_report_cursor (
    device_id       TEXT PRIMARY KEY,
    last_event_time INTEGER NOT NULL DEFAULT 0,
    updated_at      TEXT NOT NULL DEFAULT ''
);`); err != nil {
		return fmt.Errorf("db: schema v14: %w", err)
	}
	return nil
}

// migrateV15 adds the optional time window of an automation. The JSON document
// is opaque to this package: the dispatch package interprets it.
func (s *Store) migrateV15() error {
	cols, err := s.columns("automations")
	if err != nil {
		return err
	}
	if cols["schedule"] {
		return nil
	}
	if _, err := s.db.Exec("ALTER TABLE automations ADD COLUMN schedule TEXT NOT NULL DEFAULT '{}'"); err != nil {
		return fmt.Errorf("db: add automations.schedule: %w", err)
	}
	return nil
}

// migrateV16 adds the optional per-automation action delay.
func (s *Store) migrateV16() error {
	cols, err := s.columns("automations")
	if err != nil {
		return err
	}
	if cols["delay"] {
		return nil
	}
	if _, err := s.db.Exec("ALTER TABLE automations ADD COLUMN delay TEXT NOT NULL DEFAULT '{}'"); err != nil {
		return fmt.Errorf("db: add automations.delay: %w", err)
	}
	return nil
}

// migrateV17 adds the volatile (one-shot) flag of an automation. A volatile rule
// fires once and is then removed by the dispatcher; time-triggered automations
// set it so a "desliga às 14" does not linger.
func (s *Store) migrateV17() error {
	cols, err := s.columns("automations")
	if err != nil {
		return err
	}
	if cols["once"] {
		return nil
	}
	if _, err := s.db.Exec("ALTER TABLE automations ADD COLUMN once INTEGER NOT NULL DEFAULT 0"); err != nil {
		return fmt.Errorf("db: add automations.once: %w", err)
	}
	return nil
}

// columns returns the set of column names of a table.
func (s *Store) columns(table string) (map[string]bool, error) {
	rows, err := s.db.Query("PRAGMA table_info(" + table + ")")
	if err != nil {
		return nil, fmt.Errorf("db: table_info %s: %w", table, err)
	}
	defer rows.Close()
	out := map[string]bool{}
	for rows.Next() {
		var (
			cid     int
			name    string
			ctype   string
			notnull int
			dflt    sql.NullString
			pk      int
		)
		if err := rows.Scan(&cid, &name, &ctype, &notnull, &dflt, &pk); err != nil {
			return nil, fmt.Errorf("db: table_info %s: %w", table, err)
		}
		out[name] = true
	}
	return out, rows.Err()
}

// inTx runs fn inside a transaction, committing on success.
func (s *Store) inTx(ctx context.Context, fn func(*sql.Tx) error) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	if err := fn(tx); err != nil {
		_ = tx.Rollback()
		return err
	}
	return tx.Commit()
}

func boolToInt(b bool) int {
	if b {
		return 1
	}
	return 0
}

func formatTime(t time.Time) string {
	return t.UTC().Format(time.RFC3339Nano)
}

func parseTime(v string) time.Time {
	if v == "" {
		return time.Time{}
	}
	t, err := time.Parse(time.RFC3339Nano, v)
	if err != nil {
		return time.Time{}
	}
	return t
}
