package functions

import (
	"context"
	"encoding/json"
	"errors"
	"path/filepath"
	"testing"

	"smart_home/hub/internal/db"
	"smart_home/hub/internal/model"
)

type fakeThings struct{ things []*model.Thing }

func (f fakeThings) Snapshot() []*model.Thing { return f.things }

func openStore(t *testing.T) *db.Store {
	t.Helper()
	s, err := db.Open(filepath.Join(t.TempDir(), "hub.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = s.Close() })
	return s
}

func fptr(v float64) *float64 { return &v }

func byKey(cat *Catalog) map[string]Function {
	m := make(map[string]Function, len(cat.Functions))
	for _, f := range cat.Functions {
		m[f.Key] = f
	}
	return m
}

func TestFanCatalogDefaults(t *testing.T) {
	store := openStore(t)
	ctx := context.Background()
	if err := store.UpsertDevice(ctx, db.Device{ID: "rf01/fan0", Kind: db.KindFan, Owner: "rf01", DeviceID: "fan0"}); err != nil {
		t.Fatal(err)
	}
	things := fakeThings{[]*model.Thing{{
		ID: "rf01",
		Manifest: &model.Manifest{Devices: []model.Device{{
			ID: "fan0",
			Capabilities: map[string]model.Capability{
				"power":     {Kind: "switch"},
				"light":     {Kind: "switch"},
				"speed":     {Kind: "number", Min: fptr(0), Max: fptr(6), Step: fptr(1)},
				"direction": {Kind: "enum", Values: []string{"forward", "reverse"}},
			},
		}}},
	}}}
	svc := New(store, things)

	cat, err := svc.Catalog(ctx, "rf01/fan0")
	if err != nil {
		t.Fatal(err)
	}
	if cat.Driver != DriverFan || cat.Configured {
		t.Fatalf("unexpected catalog header: %+v", cat)
	}
	got := byKey(cat)
	if len(got) != 4 {
		t.Fatalf("want 4 functions, got %d: %+v", len(got), cat.Functions)
	}
	if got["power"].Kind != KindBool || got["power"].Label != "Ligar/desligar" {
		t.Fatalf("power: %+v", got["power"])
	}
	if !got["power"].Off || got["power"].OffValue != false {
		t.Fatalf("fan power should default to off: %+v", got["power"])
	}
	if got["speed"].Kind != KindNumber || got["speed"].Max == nil || *got["speed"].Max != 6 {
		t.Fatalf("speed: %+v", got["speed"])
	}
	if got["direction"].Kind != KindEnum || len(got["direction"].Values) != 2 {
		t.Fatalf("direction: %+v", got["direction"])
	}
}

func TestTuyaCatalogSkipsConfigAndReadOnly(t *testing.T) {
	store := openStore(t)
	ctx := context.Background()
	if err := store.UpsertDevice(ctx, db.Device{ID: "plug", Kind: db.KindTuya}); err != nil {
		t.Fatal(err)
	}
	if err := store.ReplaceSpecs(ctx, "plug", []db.Spec{
		{DeviceID: "plug", DPID: "1", Code: "switch_1", Type: "Boolean", Writable: true},
		{DeviceID: "plug", DPID: "9", Code: "countdown_1", Type: "Integer", Writable: true},
		{DeviceID: "plug", DPID: "17", Code: "child_lock", Type: "Boolean", Writable: true},
		{DeviceID: "plug", DPID: "19", Code: "cur_power", Type: "Integer", Writable: false},
		{DeviceID: "plug", DPID: "12", Code: "switch_2", Name: "Tomada 2", Type: "Boolean", Writable: true},
	}); err != nil {
		t.Fatal(err)
	}
	svc := New(store, nil)

	cat, err := svc.Catalog(ctx, "plug")
	if err != nil {
		t.Fatal(err)
	}
	got := byKey(cat)
	if len(got) != 2 {
		t.Fatalf("want only the two switch gangs, got %+v", cat.Functions)
	}
	if !got["1"].Off {
		t.Fatalf("switch_1 should default to off: %+v", got["1"])
	}
	if got["12"].Label != "Tomada 2" {
		t.Fatalf("spec name should win: %+v", got["12"])
	}
}

// TestTuyaCatalogMultiGang covers a three-key wall switch: only the three gangs
// become functions (and "off" targets) — the key backlight and the per-key
// countdowns configure the appliance and must stay out.
func TestTuyaCatalogMultiGang(t *testing.T) {
	store := openStore(t)
	ctx := context.Background()
	if err := store.UpsertDevice(ctx, db.Device{ID: "sw3", Kind: db.KindTuya}); err != nil {
		t.Fatal(err)
	}
	if err := store.ReplaceSpecs(ctx, "sw3", []db.Spec{
		{DeviceID: "sw3", DPID: "1", Code: "switch_1", Type: "Boolean", Writable: true},
		{DeviceID: "sw3", DPID: "2", Code: "switch_2", Type: "Boolean", Writable: true},
		{DeviceID: "sw3", DPID: "3", Code: "switch_3", Type: "Boolean", Writable: true},
		{DeviceID: "sw3", DPID: "16", Code: "switch_backlight", Type: "Boolean", Writable: true},
		{DeviceID: "sw3", DPID: "17", Code: "switch_inching", Type: "String", Writable: true},
		{DeviceID: "sw3", DPID: "7", Code: "countdown_1", Type: "Integer", Writable: true},
		{DeviceID: "sw3", DPID: "8", Code: "countdown_2", Type: "Integer", Writable: true},
		{DeviceID: "sw3", DPID: "9", Code: "countdown_3", Type: "Integer", Writable: true},
		{DeviceID: "sw3", DPID: "14", Code: "relay_status", Type: "Enum", Writable: true},
	}); err != nil {
		t.Fatal(err)
	}
	svc := New(store, nil)

	cat, err := svc.Catalog(ctx, "sw3")
	if err != nil {
		t.Fatal(err)
	}
	got := byKey(cat)
	if len(got) != 3 {
		t.Fatalf("want only the three gangs, got %+v", cat.Functions)
	}
	for _, key := range []string{"1", "2", "3"} {
		f, ok := got[key]
		if !ok {
			t.Fatalf("gang %s missing: %+v", key, cat.Functions)
		}
		if !f.Off || f.OffValue != false {
			t.Fatalf("gang %s should default to off: %+v", key, f)
		}
	}
	if _, ok := got["16"]; ok {
		t.Fatalf("backlight must not be a function: %+v", cat.Functions)
	}

	cmds, err := svc.Off(ctx, "sw3")
	if err != nil {
		t.Fatal(err)
	}
	if len(cmds) != 3 {
		t.Fatalf("off should touch only the three gangs, got %+v", cmds)
	}
	for _, c := range cmds {
		if c.Key == "16" {
			t.Fatalf("off must never touch the backlight: %+v", cmds)
		}
	}
}

func TestMideaCatalogDefaults(t *testing.T) {
	store := openStore(t)
	ctx := context.Background()
	if err := store.UpsertDevice(ctx, db.Device{ID: "ac", Kind: db.KindMidea}); err != nil {
		t.Fatal(err)
	}
	if err := store.ReplaceMideaSpecs(ctx, "ac", []db.MideaSpec{
		{DeviceID: "ac", ID: "power", Name: "Energia", Type: "boolean", Writable: true},
		{DeviceID: "ac", ID: "mode", Name: "Modo", Type: "enum", Values: json.RawMessage(`["auto","cool","heat"]`), Writable: true},
		{DeviceID: "ac", ID: "target_temp", Name: "Temperatura", Type: "integer", Values: json.RawMessage(`{"min":16,"max":30,"step":1}`), Writable: true},
	}); err != nil {
		t.Fatal(err)
	}
	svc := New(store, nil)

	cat, err := svc.Catalog(ctx, "ac")
	if err != nil {
		t.Fatal(err)
	}
	got := byKey(cat)
	if !got["power"].Off || got["power"].OffValue != false {
		t.Fatalf("midea power should default to off: %+v", got["power"])
	}
	if got["mode"].Kind != KindEnum || len(got["mode"].Values) != 3 {
		t.Fatalf("mode: %+v", got["mode"])
	}
	if got["target_temp"].Min == nil || *got["target_temp"].Min != 16 || *got["target_temp"].Max != 30 {
		t.Fatalf("target_temp: %+v", got["target_temp"])
	}
}

func TestSaveOverridesDefaultsAndReset(t *testing.T) {
	store := openStore(t)
	ctx := context.Background()
	if err := store.UpsertDevice(ctx, db.Device{ID: "plug", Kind: db.KindTuya}); err != nil {
		t.Fatal(err)
	}
	if err := store.ReplaceSpecs(ctx, "plug", []db.Spec{
		{DeviceID: "plug", DPID: "1", Code: "switch_1", Type: "Boolean", Writable: true},
		{DeviceID: "plug", DPID: "2", Code: "switch_2", Type: "Boolean", Writable: true},
	}); err != nil {
		t.Fatal(err)
	}
	svc := New(store, nil)

	cat, err := svc.Save(ctx, "plug", []Config{
		{Key: "1", Label: "Tomada do PC", Aliases: []string{"tomada", "pc", "Tomada"}, Voice: true, Off: true, OffValue: false},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !cat.Configured || len(cat.Functions) != 1 {
		t.Fatalf("explicit config should win: %+v", cat)
	}
	f := cat.Functions[0]
	if f.Label != "Tomada do PC" || !f.Voice || !f.Off {
		t.Fatalf("config not applied: %+v", f)
	}
	// Aliases are de-duplicated (case-insensitively) and trimmed.
	if len(f.Aliases) != 2 || f.Aliases[0] != "tomada" || f.Aliases[1] != "pc" {
		t.Fatalf("aliases: %+v", f.Aliases)
	}

	// The saved value round-trips through the store.
	stored, err := store.GetDevice(ctx, "plug")
	if err != nil {
		t.Fatal(err)
	}
	if stored.FunctionsJSON == "" {
		t.Fatal("functions_json was not stored")
	}

	// Reset goes back to the derived defaults.
	cat, err = svc.Reset(ctx, "plug")
	if err != nil {
		t.Fatal(err)
	}
	if cat.Configured || len(cat.Functions) != 2 {
		t.Fatalf("reset should restore the defaults: %+v", cat)
	}
}

func TestSaveRejectsBadConfig(t *testing.T) {
	store := openStore(t)
	ctx := context.Background()
	if err := store.UpsertDevice(ctx, db.Device{ID: "plug", Kind: db.KindTuya}); err != nil {
		t.Fatal(err)
	}
	if err := store.ReplaceSpecs(ctx, "plug", []db.Spec{
		{DeviceID: "plug", DPID: "1", Code: "switch_1", Type: "Boolean", Writable: true},
		{DeviceID: "plug", DPID: "2", Code: "mode", Type: "Enum", Values: json.RawMessage(`["auto","cool"]`), Writable: true},
	}); err != nil {
		t.Fatal(err)
	}
	svc := New(store, nil)

	if _, err := svc.Save(ctx, "plug", []Config{{Key: "ghost", Label: "x"}}); err == nil {
		t.Fatal("unknown key should be rejected")
	}
	if _, err := svc.Save(ctx, "plug", []Config{{Key: "1", Label: "x", Off: true, OffValue: "nope"}}); err == nil {
		t.Fatal("non-boolean off value should be rejected")
	}
	if _, err := svc.Save(ctx, "plug", []Config{{Key: "2", Label: "x", Off: true, OffValue: "dry"}}); err == nil {
		t.Fatal("off value outside the enum should be rejected")
	}
	if _, err := svc.Save(ctx, "plug", []Config{{Key: "1", Label: "a"}, {Key: "1", Label: "b"}}); err == nil {
		t.Fatal("duplicate keys should be rejected")
	}
	// A rejected save must not have written anything.
	d, _ := store.GetDevice(ctx, "plug")
	if d.FunctionsJSON != "" {
		t.Fatalf("failed save wrote the config: %q", d.FunctionsJSON)
	}
}

func TestOffCommands(t *testing.T) {
	store := openStore(t)
	ctx := context.Background()
	if err := store.UpsertDevice(ctx, db.Device{ID: "rf01/fan0", Kind: db.KindFan, Owner: "rf01", DeviceID: "fan0"}); err != nil {
		t.Fatal(err)
	}
	things := fakeThings{[]*model.Thing{{
		ID: "rf01",
		Manifest: &model.Manifest{Devices: []model.Device{{
			ID: "fan0",
			Capabilities: map[string]model.Capability{
				"power": {Kind: "switch"},
				"light": {Kind: "switch"},
			},
		}}},
	}}}
	svc := New(store, things)

	cmds, err := svc.Off(ctx, "rf01/fan0")
	if err != nil {
		t.Fatal(err)
	}
	if len(cmds) != 1 || cmds[0].Key != "power" || cmds[0].Value != false {
		t.Fatalf("fan off commands: %+v", cmds)
	}

	// A custom selection: turn the light off by sending true.
	if _, err := svc.Save(ctx, "rf01/fan0", []Config{
		{Key: "power", Label: "Motor", Off: true, OffValue: false},
		{Key: "light", Label: "Luz", Off: true, OffValue: true},
	}); err != nil {
		t.Fatal(err)
	}
	cmds, err = svc.Off(ctx, "rf01/fan0")
	if err != nil {
		t.Fatal(err)
	}
	if len(cmds) != 2 {
		t.Fatalf("custom off commands: %+v", cmds)
	}
}

func TestCatalogUnknownDevice(t *testing.T) {
	store := openStore(t)
	svc := New(store, nil)
	if _, err := svc.Catalog(context.Background(), "ghost"); !errors.Is(err, db.ErrNotFound) {
		t.Fatalf("want ErrNotFound, got %v", err)
	}
}
