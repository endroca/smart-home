package db

import (
	"context"
	"database/sql"
	"errors"
	"path/filepath"
	"testing"

	_ "modernc.org/sqlite"
)

func openTemp(t *testing.T) *Store {
	t.Helper()
	path := filepath.Join(t.TempDir(), "data", "hub.db")
	s, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = s.Close() })
	return s
}

func TestDeviceRoundTrip(t *testing.T) {
	s := openTemp(t)
	ctx := context.Background()

	if err := s.UpsertDevice(ctx, Device{ID: "tv", Kind: KindTuya, Name: "TV", IP: "192.168.1.20", Version: "3.5", LocalKey: "0123456789abcdef", KeySource: "cloud"}); err != nil {
		t.Fatal(err)
	}
	if err := s.UpsertDevice(ctx, Device{ID: "rf01", Kind: KindThing, Name: "ESP32", Token: "secret", URL: "http://192.168.1.30"}); err != nil {
		t.Fatal(err)
	}

	tv, err := s.GetDevice(ctx, "tv")
	if err != nil {
		t.Fatal(err)
	}
	if tv.Kind != KindTuya || tv.Name != "TV" || tv.LocalKey != "0123456789abcdef" {
		t.Fatalf("unexpected device: %+v", tv)
	}
	if tv.Port != 0 {
		t.Fatalf("port should not be defaulted, got %d", tv.Port)
	}

	tuya, err := s.ListDevices(ctx, KindTuya)
	if err != nil {
		t.Fatal(err)
	}
	if len(tuya) != 1 || tuya[0].ID != "tv" {
		t.Fatalf("tuya list: %+v", tuya)
	}

	if s.DeviceToken("rf01") != "secret" || s.DeviceURL("rf01") != "http://192.168.1.30" {
		t.Fatal("directory lookup failed")
	}
	if s.DeviceToken("unknown") != "" {
		t.Fatal("unknown thing should have no token")
	}
}

func TestUpsertMergesWithoutClobbering(t *testing.T) {
	s := openTemp(t)
	ctx := context.Background()

	if err := s.UpsertDevice(ctx, Device{ID: "dev1", Name: "sala", IP: "192.168.1.10", Version: "3.5"}); err != nil {
		t.Fatal(err)
	}
	// A cloud registration carries a key but no LAN address.
	if err := s.UpsertDevice(ctx, Device{ID: "dev1", LocalKey: "0123456789abcdef", KeySource: "cloud"}); err != nil {
		t.Fatal(err)
	}
	d, _ := s.GetDevice(ctx, "dev1")
	if d.LocalKey != "0123456789abcdef" {
		t.Fatalf("key not stored: %+v", d)
	}
	if d.IP != "192.168.1.10" || d.Name != "sala" {
		t.Fatalf("cloud registration clobbered fields: %+v", d)
	}
	if d.Kind != KindTuya {
		t.Fatalf("kind default not applied: %q", d.Kind)
	}
}

func TestRoomsAndPlacements(t *testing.T) {
	s := openTemp(t)
	ctx := context.Background()

	if err := s.UpsertRoom(ctx, Room{ID: "r1", Name: "Sala", Sort: 1}); err != nil {
		t.Fatal(err)
	}
	if err := s.UpsertRoom(ctx, Room{ID: "r2", Name: "Quarto", Sort: 2}); err != nil {
		t.Fatal(err)
	}
	rooms, err := s.ListRooms(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(rooms) != 2 || rooms[0].ID != "r1" {
		t.Fatalf("rooms: %+v", rooms)
	}

	if err := s.UpsertDevice(ctx, Device{ID: "rf01", Kind: KindThing, Name: "ESP32"}); err != nil {
		t.Fatal(err)
	}
	if err := s.SetPlacement(ctx, Placement{Kind: PlacementFan, Owner: "rf01", Device: "fan0", RoomID: "r1"}); err != nil {
		t.Fatal(err)
	}
	ps, err := s.Placements(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(ps) != 1 || ps[0].RoomID != "r1" {
		t.Fatalf("placements: %+v", ps)
	}

	// Deleting a room clears references on both devices and placements.
	if err := s.SetDeviceRoom(ctx, "rf01", "r2"); err != nil {
		t.Fatal(err)
	}
	if err := s.DeleteRoom(ctx, "r2"); err != nil {
		t.Fatal(err)
	}
	d, _ := s.GetDevice(ctx, "rf01")
	if d.RoomID != "" {
		t.Fatalf("device room not cleared: %+v", d)
	}
	ps, _ = s.Placements(ctx)
	if len(ps) != 1 || ps[0].RoomID != "r1" {
		t.Fatalf("placement for r1 should survive: %+v", ps)
	}
	if err := s.DeleteRoom(ctx, "r1"); err != nil {
		t.Fatal(err)
	}
	ps, _ = s.Placements(ctx)
	if len(ps) != 1 || ps[0].RoomID != "" {
		t.Fatalf("placement for r1 not cleared: %+v", ps)
	}
}

func TestAutomations(t *testing.T) {
	s := openTemp(t)
	ctx := context.Background()

	if err := s.UpsertAutomation(ctx, Automation{ID: "a1", Name: "Luz", Enabled: true, Source: []byte(`{"type":"tuya_dp"}`), Actions: []byte(`[{"kind":"fan"}]`)}); err != nil {
		t.Fatal(err)
	}
	a, err := s.GetAutomation(ctx, "a1")
	if err != nil {
		t.Fatal(err)
	}
	if !a.Enabled || a.Name != "Luz" || string(a.Source) != `{"type":"tuya_dp"}` {
		t.Fatalf("automation: %+v", a)
	}
	if err := s.DeleteAutomation(ctx, "a1"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.GetAutomation(ctx, "a1"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
}

// TestReplaceSpecsToleratesDuplicateDP ensures a duplicate dp id (the cloud
// lists a DP in both functions and status) does not break the insert.
func TestReplaceSpecsToleratesDuplicateDP(t *testing.T) {
	s := openTemp(t)
	ctx := context.Background()
	specs := []Spec{
		{DeviceID: "d1", DPID: "1", Code: "switch_led", Name: "Luz", Desc: "Liga/desliga", Type: "Boolean", Writable: true},
		{DeviceID: "d1", DPID: "1", Code: "switch_led", Name: "Luz", Desc: "Liga/desliga", Type: "Boolean", Writable: false},
	}
	if err := s.ReplaceSpecs(ctx, "d1", specs); err != nil {
		t.Fatal(err)
	}
	got, err := s.ListSpecs(ctx, "d1")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 {
		t.Fatalf("want 1 spec after dedupe, got %d", len(got))
	}
	if got[0].Name != "Luz" || got[0].Desc != "Liga/desliga" {
		t.Fatalf("spec name/desc not persisted: %+v", got[0])
	}
}

// TestDeviceTemplateRoundTrip stores a layout template, keeps it across a
// clobbering upsert and clears it explicitly.
func TestDeviceTemplateRoundTrip(t *testing.T) {
	s := openTemp(t)
	ctx := context.Background()

	if err := s.UpsertDevice(ctx, Device{ID: "plug", Kind: KindTuya, Name: "Tomada", Template: "tuya.socket"}); err != nil {
		t.Fatal(err)
	}
	d, err := s.GetDevice(ctx, "plug")
	if err != nil {
		t.Fatal(err)
	}
	if d.Template != "tuya.socket" {
		t.Fatalf("template not stored: %+v", d)
	}
	// A refresh without a template must not clobber the choice.
	if err := s.UpsertDevice(ctx, Device{ID: "plug", Kind: KindTuya, IP: "192.168.1.9"}); err != nil {
		t.Fatal(err)
	}
	d, _ = s.GetDevice(ctx, "plug")
	if d.Template != "tuya.socket" || d.IP != "192.168.1.9" {
		t.Fatalf("upsert clobbered template: %+v", d)
	}
	// Clearing is explicit.
	if err := s.SetDeviceTemplate(ctx, "plug", ""); err != nil {
		t.Fatal(err)
	}
	d, _ = s.GetDevice(ctx, "plug")
	if d.Template != "" {
		t.Fatalf("template not cleared: %+v", d)
	}
}

// TestMigrateV1UpgradesAnExistingRegistry ensures a database created by the
// previous registry (user_version=1, no kind/token/room columns) migrates
// without losing data.
func TestMigrateV1UpgradesAnExistingRegistry(t *testing.T) {
	path := filepath.Join(t.TempDir(), "old.db")
	raw, err := sql.Open("sqlite", "file:"+path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := raw.Exec(schemaV1); err != nil {
		t.Fatal(err)
	}
	if _, err := raw.Exec(`INSERT INTO devices (id, name, ip, port, version, local_key, created_at, updated_at)
		VALUES ('dev1', 'sala', '192.168.1.10', 6668, '3.5', '0123456789abcdef', '', '')`); err != nil {
		t.Fatal(err)
	}
	if _, err := raw.Exec("PRAGMA user_version = 1"); err != nil {
		t.Fatal(err)
	}
	if err := raw.Close(); err != nil {
		t.Fatal(err)
	}

	s, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()

	d, err := s.GetDevice(context.Background(), "dev1")
	if err != nil {
		t.Fatal(err)
	}
	if d.Kind != KindTuya || d.LocalKey != "0123456789abcdef" || d.IP != "192.168.1.10" {
		t.Fatalf("migrated row is wrong: %+v", d)
	}
	if d.Template != "" {
		t.Fatalf("new template column should default empty: %+v", d)
	}
	if d.Category != "" || d.CloudOnly {
		t.Fatalf("new category/cloud_only columns should default empty/false: %+v", d)
	}
	// The new tables exist after the migration.
	if _, err := s.ListRooms(context.Background()); err != nil {
		t.Fatalf("rooms table missing after migration: %v", err)
	}
}

// TestDeviceCategoryAndCloudOnly covers the v13 columns: they persist, a later
// partial upsert does not clobber them, and the flag can be cleared explicitly.
func TestDeviceCategoryAndCloudOnly(t *testing.T) {
	s := openTemp(t)
	ctx := context.Background()

	if err := s.UpsertDevice(ctx, Device{ID: "lock", Kind: KindTuya, Name: "Fechadura", Category: "jtmspro", CloudOnly: true}); err != nil {
		t.Fatal(err)
	}
	d, err := s.GetDevice(ctx, "lock")
	if err != nil {
		t.Fatal(err)
	}
	if d.Category != "jtmspro" || !d.CloudOnly {
		t.Fatalf("category/cloud_only not stored: %+v", d)
	}

	// A later upsert (for example a key refresh from the cloud) must not clear
	// cloud_only, and keeps the category when it is omitted.
	if err := s.UpsertDevice(ctx, Device{ID: "lock", Kind: KindTuya, LocalKey: "0123456789abcdef"}); err != nil {
		t.Fatal(err)
	}
	if d, _ = s.GetDevice(ctx, "lock"); d.Category != "jtmspro" || !d.CloudOnly || d.LocalKey != "0123456789abcdef" {
		t.Fatalf("upsert clobbered fields: %+v", d)
	}

	if err := s.SetDeviceCloudOnly(ctx, "lock", false); err != nil {
		t.Fatal(err)
	}
	if d, _ = s.GetDevice(ctx, "lock"); d.CloudOnly {
		t.Fatalf("cloud_only not cleared: %+v", d)
	}
}

// TestReportCursor covers the v14 report-log cursor: it round-trips, updates,
// and is dropped with the device.
func TestReportCursor(t *testing.T) {
	s := openTemp(t)
	ctx := context.Background()

	if _, ok, err := s.ReportCursor(ctx, "lock1"); err != nil || ok {
		t.Fatalf("expected no cursor yet: ok=%v err=%v", ok, err)
	}
	if err := s.SetReportCursor(ctx, "lock1", 1000); err != nil {
		t.Fatal(err)
	}
	ts, ok, err := s.ReportCursor(ctx, "lock1")
	if err != nil || !ok || ts != 1000 {
		t.Fatalf("cursor: ts=%d ok=%v err=%v", ts, ok, err)
	}
	if err := s.SetReportCursor(ctx, "lock1", 2000); err != nil {
		t.Fatal(err)
	}
	if ts, _, _ := s.ReportCursor(ctx, "lock1"); ts != 2000 {
		t.Fatalf("cursor not updated: %d", ts)
	}

	if err := s.UpsertDevice(ctx, Device{ID: "lock1", Kind: KindTuya}); err != nil {
		t.Fatal(err)
	}
	if err := s.DeleteDevice(ctx, "lock1"); err != nil {
		t.Fatal(err)
	}
	if _, ok, _ := s.ReportCursor(ctx, "lock1"); ok {
		t.Fatal("cursor should be gone with the device")
	}
}

func TestStatesRoundTrip(t *testing.T) {
	s := openTemp(t)
	ctx := context.Background()

	if err := s.SetStates(ctx, "d1", map[string]any{"1": true, "2": float64(3), "3": "white"}); err != nil {
		t.Fatal(err)
	}
	got, err := s.States(ctx, "d1")
	if err != nil {
		t.Fatal(err)
	}
	if got["1"] != true || got["2"] != float64(3) || got["3"] != "white" {
		t.Fatalf("states: %+v", got)
	}

	all, err := s.AllStates(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if all["d1"]["1"] != true {
		t.Fatalf("all states: %+v", all)
	}

	// ReplaceStates wipes the previous DPs of the device.
	if err := s.ReplaceStates(ctx, "d1", map[string]any{"9": "x"}); err != nil {
		t.Fatal(err)
	}
	got, _ = s.States(ctx, "d1")
	if len(got) != 1 || got["9"] != "x" {
		t.Fatalf("after replace: %+v", got)
	}
}

func TestFanRegistration(t *testing.T) {
	s := openTemp(t)
	ctx := context.Background()

	if err := s.UpsertDevice(ctx, Device{ID: "gw", Kind: KindThing, Name: "Gateway"}); err != nil {
		t.Fatal(err)
	}
	if err := s.UpsertFan(ctx, "gw", "fan0", "Sala"); err != nil {
		t.Fatal(err)
	}
	if err := s.UpsertFan(ctx, "gw", "fan1", "Quarto"); err != nil {
		t.Fatal(err)
	}
	fans, err := s.ListFansByOwner(ctx, "gw")
	if err != nil {
		t.Fatal(err)
	}
	if len(fans) != 2 {
		t.Fatalf("fans=%d", len(fans))
	}
	f0 := fans[0]
	if f0.ID != "gw/fan0" || f0.Kind != KindFan || f0.Owner != "gw" || f0.DeviceID != "fan0" {
		t.Fatalf("unexpected fan: %+v", f0)
	}

	// A room survives a later upsert (device-side rename), the hub display
	// name is independent, and the fan is not listed as a thing or Tuya
	// device.
	if err := s.SetDeviceRoom(ctx, "gw/fan0", "r1"); err != nil {
		t.Fatal(err)
	}
	if err := s.UpsertFan(ctx, "gw", "fan0", "Sala Nova"); err != nil {
		t.Fatal(err)
	}
	got, _ := s.GetDevice(ctx, "gw/fan0")
	if got.RoomID != "r1" || got.Name != "Sala" || got.FirmwareName != "Sala Nova" {
		t.Fatalf("room/names wrong after upsert: %+v", got)
	}
	if n, _ := s.CountDevices(ctx, KindFan); n != 2 {
		t.Fatalf("fans not all registered: %d", n)
	}
	if n, _ := s.CountDevices(ctx, KindThing); n != 1 {
		t.Fatalf("gateway not counted as a thing: %d", n)
	}

	if err := s.DeleteFan(ctx, "gw", "fan1"); err != nil {
		t.Fatal(err)
	}
	fans, _ = s.ListFansByOwner(ctx, "gw")
	if len(fans) != 1 {
		t.Fatalf("delete failed: %+v", fans)
	}
	// Deleting a missing fan is a no-op.
	if err := s.DeleteFan(ctx, "gw", "ghost"); err != nil {
		t.Fatalf("delete missing fan: %v", err)
	}

	// Deleting the gateway drops its remaining sub-devices too.
	if err := s.DeleteDevice(ctx, "gw"); err != nil {
		t.Fatal(err)
	}
	fans, _ = s.ListFansByOwner(ctx, "gw")
	if len(fans) != 0 {
		t.Fatalf("fans not removed with the gateway: %+v", fans)
	}
}

// TestMigrateV18AddsColumns checks the v18 migration adds the order and function
// columns with safe defaults on an existing registry.
func TestMigrateV18AddsColumns(t *testing.T) {
	s := openTemp(t)
	ctx := context.Background()

	if err := s.UpsertDevice(ctx, Device{ID: "tv", Kind: KindTuya, Name: "TV", RoomID: "sala"}); err != nil {
		t.Fatal(err)
	}
	// openTemp already applied every migration, so calling it again is a no-op
	// that must not fail on the existing columns.
	if err := s.migrateV18(); err != nil {
		t.Fatal(err)
	}
	d, err := s.GetDevice(ctx, "tv")
	if err != nil {
		t.Fatal(err)
	}
	if d.Sort != 0 || d.FunctionsJSON != "" {
		t.Fatalf("new columns should default to zero/empty: sort=%d functions=%q", d.Sort, d.FunctionsJSON)
	}

	// The functions column round-trips through the setter.
	if err := s.SetDeviceFunctions(ctx, "tv", `[{"key":"1","label":"Luz","off":true,"off_value":false}]`); err != nil {
		t.Fatal(err)
	}
	d, _ = s.GetDevice(ctx, "tv")
	if d.FunctionsJSON == "" {
		t.Fatal("functions_json was not stored")
	}
}

// TestSetDeviceOrderRejectsMixedRooms checks the order write is atomic and only
// accepts devices of one room.
func TestSetDeviceOrderRejectsMixedRooms(t *testing.T) {
	s := openTemp(t)
	ctx := context.Background()

	for _, d := range []Device{
		{ID: "a", Kind: KindTuya, RoomID: "sala"},
		{ID: "b", Kind: KindTuya, RoomID: "sala"},
		{ID: "c", Kind: KindTuya, RoomID: "quarto"},
	} {
		if err := s.UpsertDevice(ctx, d); err != nil {
			t.Fatal(err)
		}
	}

	if err := s.SetDeviceOrder(ctx, []string{"b", "a"}); err != nil {
		t.Fatal(err)
	}
	b, _ := s.GetDevice(ctx, "b")
	a, _ := s.GetDevice(ctx, "a")
	if b.Sort != 0 || a.Sort != 1 {
		t.Fatalf("order not applied: a=%d b=%d", a.Sort, b.Sort)
	}

	if err := s.SetDeviceOrder(ctx, []string{"a", "c"}); err == nil {
		t.Fatal("mixed rooms should be rejected")
	}
	// The rejection must not have written anything.
	a2, _ := s.GetDevice(ctx, "a")
	if a2.Sort != 1 {
		t.Fatalf("failed order changed a: sort=%d", a2.Sort)
	}

	if err := s.SetDeviceOrder(ctx, []string{"a", "a"}); err == nil {
		t.Fatal("duplicate ids should be rejected")
	}
	if err := s.SetDeviceOrder(ctx, []string{"ghost"}); err == nil {
		t.Fatal("unknown device should be rejected")
	}
	if err := s.SetDeviceOrder(ctx, nil); err == nil {
		t.Fatal("empty order should be rejected")
	}
}

// TestSetDeviceRoomAppendsToEnd checks moving a device to a room puts it last
// and that re-setting the same room is a no-op.
func TestSetDeviceRoomAppendsToEnd(t *testing.T) {
	s := openTemp(t)
	ctx := context.Background()

	for _, d := range []Device{
		{ID: "a", Kind: KindTuya, RoomID: "sala"},
		{ID: "b", Kind: KindTuya, RoomID: "sala"},
		{ID: "c", Kind: KindTuya, RoomID: "quarto"},
	} {
		if err := s.UpsertDevice(ctx, d); err != nil {
			t.Fatal(err)
		}
	}
	if err := s.SetDeviceOrder(ctx, []string{"a", "b"}); err != nil {
		t.Fatal(err)
	}

	if err := s.SetDeviceRoom(ctx, "c", "sala"); err != nil {
		t.Fatal(err)
	}
	c, _ := s.GetDevice(ctx, "c")
	if c.RoomID != "sala" || c.Sort != 2 {
		t.Fatalf("moved device should land at the end: room=%q sort=%d", c.RoomID, c.Sort)
	}

	// Re-assigning the same room must not shuffle it.
	if err := s.SetDeviceRoom(ctx, "c", "sala"); err != nil {
		t.Fatal(err)
	}
	c2, _ := s.GetDevice(ctx, "c")
	if c2.Sort != 2 {
		t.Fatalf("re-setting the same room changed the order: sort=%d", c2.Sort)
	}
}
