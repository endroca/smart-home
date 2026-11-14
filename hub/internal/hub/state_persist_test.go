package hub

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"smart_home/hub/internal/config"
	"smart_home/hub/internal/db"
	"smart_home/hub/internal/model"
)

// newPersistentHub builds a hub backed by a real database, so the tests can
// exercise the durable replica of the device state.
func newPersistentHub(t *testing.T, store *db.Store) *Hub {
	t.Helper()
	h, err := New(config.Config{FirmwareDir: t.TempDir()}, store)
	if err != nil {
		t.Fatal(err)
	}

	return h
}

func openStateStore(t *testing.T) *db.Store {
	t.Helper()
	s, err := db.Open(filepath.Join(t.TempDir(), "hub.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = s.Close() })
	return s
}

func registerFan(h *Hub, thing, deviceID string) {
	h.onManifest(thing, &model.Manifest{
		SchemaVersion:   1,
		ManifestVersion: 1,
		Thing:           thing,
		Firmware:        model.Firmware{Version: "1.0.0"},
		Devices: []model.Device{
			{
				ID:   deviceID,
				Name: "Sala",
				Capabilities: map[string]model.Capability{
					"power": {Kind: "switch"},
				},
				Actions: []string{"state.correct"},
			},
		},
	})
}

// A reported state must be written through to the database as soon as it
// arrives, not only kept in memory.
func TestIngestStatePersistsToReplica(t *testing.T) {
	store := openStateStore(t)
	h := newPersistentHub(t, store)
	registerFan(h, "gw", "fan0")

	h.IngestState("gw", "fan0", map[string]any{
		"power": true, "light": false, "source": "rf_remote", "updatedAt": int64(1500),
	})

	rows, err := store.ThingStates(context.Background(), "gw")
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 1 {
		t.Fatalf("want 1 persisted state, got %d", len(rows))
	}
	if rows[0].DeviceID != "fan0" || rows[0].Source != "rf_remote" || rows[0].DeviceTS != 1500 {
		t.Fatalf("unexpected persisted row: %+v", rows[0])
	}
	if got, _ := rows[0].Reported["power"].(bool); !got {
		t.Fatalf("power was not persisted: %#v", rows[0].Reported)
	}
}

// After a restart the hub must seed the hot cache from the database and mark
// the restored state stale until the device confirms it.
func TestLoadStateSeedsStaleAndFreshReportClears(t *testing.T) {
	store := openStateStore(t)
	ctx := context.Background()

	first := newPersistentHub(t, store)
	registerFan(first, "gw", "fan0")
	first.IngestState("gw", "fan0", map[string]any{
		"power": true, "source": "http", "updatedAt": int64(7),
	})

	// Simulate a restart: a brand-new hub sharing the same database.
	second := newPersistentHub(t, store)
	if err := second.LoadState(ctx); err != nil {
		t.Fatal(err)
	}

	sh, ok := second.store.State("gw", "fan0")
	if !ok {
		t.Fatal("state was not seeded into the hot cache")
	}
	if !sh.Stale {
		t.Fatal("a restored state must be marked stale")
	}
	if got, _ := sh.Reported["power"].(bool); !got {
		t.Fatalf("seeded state lost its value: %#v", sh.Reported)
	}

	// A fresh report from the device clears the stale flag.
	second.IngestState("gw", "fan0", map[string]any{
		"power": false, "source": "rf_remote", "updatedAt": int64(9),
	})
	sh, _ = second.store.State("gw", "fan0")
	if sh.Stale {
		t.Fatal("a confirmed report must clear the stale flag")
	}
	if got, _ := sh.Reported["power"].(bool); got {
		t.Fatalf("fresh report was not applied: %#v", sh.Reported)
	}
}

// Removing a fan must drop its cached state so it does not resurrect on the
// next boot.
func TestFanRemoveDropsCachedState(t *testing.T) {
	store := openStateStore(t)
	ctx := context.Background()
	h := newPersistentHub(t, store)

	// A gateway with a LAN URL/token is required for the fallback path.
	if err := store.UpsertDevice(ctx, db.Device{ID: "gw", Kind: db.KindThing, Token: "secret", URL: "http://192.168.1.9"}); err != nil {
		t.Fatal(err)
	}
	registerFan(h, "gw", "fan0")
	if err := store.SetThingState(ctx, "gw", "fan0", map[string]any{"power": true}, "", 0); err != nil {
		t.Fatal(err)
	}
	if err := store.UpsertFan(ctx, "gw", "fan0", "Sala"); err != nil {
		t.Fatal(err)
	}

	h.forgetState("gw", "fan0")
	if rows, _ := store.ThingStates(ctx, "gw"); len(rows) != 0 {
		t.Fatalf("cached state survived removal: %#v", rows)
	}
}

// A silent gateway is marked offline by the watchdog, which keeps the last
// diagnostics; a new heartbeat brings it back.
func TestPresenceWatchdogMarksOffline(t *testing.T) {
	store := openStateStore(t)
	h := newPersistentHub(t, store)

	if err := h.IngestStatus("gw", &model.Status{Version: "1.0.0", IP: "192.168.1.9"}); err != nil {
		t.Fatal(err)
	}
	if t1, _ := h.Thing("gw"); t1.Status == nil || !t1.Status.Online {
		t.Fatal("a heartbeat must mark the gateway online")
	}

	// Simulate a heartbeat that stopped long ago.
	h.presenceMu.Lock()
	h.presence["gw"] = time.Now().Add(-2 * presenceTTL)
	h.presenceMu.Unlock()
	h.reapSilent()

	t1, _ := h.Thing("gw")
	if t1.Status.Online {
		t.Fatal("a silent gateway must be marked offline")
	}
	if t1.Status.Version != "1.0.0" || t1.Status.IP != "192.168.1.9" {
		t.Fatalf("the watchdog must keep the last diagnostics: %+v", t1.Status)
	}

	if err := h.IngestStatus("gw", &model.Status{}); err != nil {
		t.Fatal(err)
	}
	if t1, _ := h.Thing("gw"); !t1.Status.Online {
		t.Fatal("a new heartbeat must bring the gateway back online")
	}
}

// A gateway with no database wired in must keep working (LAN-only, no replica).
func TestHubWithoutStateStoreLoadsNothing(t *testing.T) {
	h := testHub(t)
	if err := h.LoadState(context.Background()); err != nil {
		t.Fatalf("LoadState without a store must be a no-op: %v", err)
	}
	h.persistReported("t1", "fan0", map[string]any{"power": true})
	h.forgetState("t1", "fan0")
	h.forgetThingState("t1")
}
