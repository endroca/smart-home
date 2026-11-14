package service

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"smart_home/hub/internal/db"
	"smart_home/hub/internal/tuya"
	"smart_home/hub/internal/tuya/cloudstream"
)

// openStore opens a shared database for a test.
func openStore(t *testing.T, path string) *db.Store {
	t.Helper()
	s, err := db.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = s.Close() })
	return s
}

// newService opens the service over a fresh store on dbPath.
func newService(t *testing.T, dbPath string, cfg Config) *Service {
	t.Helper()
	cfg.Store = openStore(t, dbPath)
	svc, err := Open(cfg)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = svc.Close() })
	return svc
}

func addDevice(t *testing.T, dbPath string, d db.Device) {
	t.Helper()
	s, err := db.Open(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if err := s.UpsertDevice(context.Background(), d); err != nil {
		t.Fatal(err)
	}
}

func TestOpenReadsRegistry(t *testing.T) {
	path := filepath.Join(t.TempDir(), "hub.db")
	addDevice(t, path, db.Device{ID: "dev1", Name: "sala", IP: "192.168.1.10", Version: "3.5", LocalKey: "0123456789abcdef", KeySource: "cloud"})
	addDevice(t, path, db.Device{ID: "dev2", Name: "quarto", IP: "192.168.1.11", Version: "3.5"})

	svc := newService(t, path, Config{})

	devices, err := svc.Devices(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(devices) != 2 {
		t.Fatalf("got %d devices, want 2", len(devices))
	}
	byID := map[string]string{}
	for _, d := range devices {
		byID[d.ID] = d.LocalKey
	}
	if byID["dev1"] != "0123456789abcdef" {
		t.Fatalf("key not read: %+v", byID)
	}
	if byID["dev2"] != "" {
		t.Fatalf("device without key got one: %+v", byID)
	}
	if svc.CloudConfigured() {
		t.Fatal("cloud should not be configured without credentials")
	}
}

func TestRegisterFromCloud(t *testing.T) {
	path := filepath.Join(t.TempDir(), "hub.db")
	addDevice(t, path, db.Device{ID: "seed", Name: "sala", IP: "192.168.1.10", Version: "3.5"})

	mux := http.NewServeMux()
	writeJSON := func(w http.ResponseWriter, v any) { _ = json.NewEncoder(w).Encode(v) }
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1.0/token":
			writeJSON(w, map[string]any{"success": true, "result": map[string]any{"access_token": "tok", "expire_time": 3600}})
		case "/v1.0/devices/seed", "/v1.0/devices/dev2":
			writeJSON(w, map[string]any{"success": true, "result": map[string]any{"uid": "uid-1"}})
		case "/v1.0/users/uid-1/devices":
			writeJSON(w, map[string]any{"success": true, "result": []any{
				map[string]any{"id": "seed", "name": "Sala", "local_key": "0123456789abcdef", "product_id": "pk1"},
				map[string]any{"id": "dev2", "name": "Quarto", "local_key": "fedcba9876543210", "product_id": "pk1"},
			}})
		default:
			t.Logf("unexpected cloud path: %s", r.URL.Path)
			writeJSON(w, map[string]any{"success": false, "code": 404, "msg": "not found"})
		}
	})
	cloud := httptest.NewServer(mux)
	defer cloud.Close()

	svc := newService(t, path, Config{
		Cloud: tuya.CloudConfig{AccessID: "id", AccessSecret: "secret", Region: "us", BaseURL: cloud.URL},
	})
	if !svc.CloudConfigured() {
		t.Fatal("cloud should be configured")
	}
	// A device registered only from the cloud has no address; the service
	// resolves it from the LAN instead of dialing an empty host.
	svc.discover = func(context.Context, time.Duration) ([]tuya.Discovered, error) {
		return []tuya.Discovered{{ID: "dev2", IP: "192.168.1.77"}}, nil
	}

	registered, err := svc.Register(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(registered) != 2 {
		t.Fatalf("registered %d devices, want 2", len(registered))
	}
	for _, d := range registered {
		if d.ID == "dev2" && d.IP != "192.168.1.77" {
			t.Fatalf("dev2 IP not filled from the LAN: %+v", d)
		}
	}

	d, err := svc.Devices(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	keys := map[string]string{}
	names := map[string]string{}
	for _, dev := range d {
		keys[dev.ID] = dev.LocalKey
		names[dev.ID] = dev.Name
	}
	if keys["seed"] != "0123456789abcdef" || keys["dev2"] != "fedcba9876543210" {
		t.Fatalf("keys not stored: %+v", keys)
	}
	if names["seed"] != "sala" {
		t.Fatalf("register renamed an existing device: %+v", names)
	}

	only, err := svc.Register(context.Background(), []string{"dev2"})
	if err != nil {
		t.Fatal(err)
	}
	if len(only) != 1 || only[0].ID != "dev2" {
		t.Fatalf("targeted register: %+v", only)
	}
}

func TestRegisterWithoutCloud(t *testing.T) {
	svc := newService(t, filepath.Join(t.TempDir(), "hub.db"), Config{})
	if _, err := svc.Register(context.Background(), nil); !errors.Is(err, ErrCloudNotConfigured) {
		t.Fatalf("expected ErrCloudNotConfigured, got %v", err)
	}
}

func TestListenRequiresKey(t *testing.T) {
	path := filepath.Join(t.TempDir(), "hub.db")
	addDevice(t, path, db.Device{ID: "dev1", Name: "sala", IP: "192.168.1.10"})

	svc := newService(t, path, Config{})

	ctx := context.Background()
	if err := svc.StartListen(ctx, "dev1"); !errors.Is(err, ErrNoKey) {
		t.Fatalf("expected ErrNoKey, got %v", err)
	}
	if err := svc.StartListen(ctx, "missing"); !errors.Is(err, db.ErrNotFound) {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
	if got := svc.Listening(); len(got) != 0 {
		t.Fatalf("listening should be empty: %v", got)
	}
	if _, ok := svc.LastEvent("dev1"); ok {
		t.Fatal("no event should be recorded yet")
	}
	svc.StopListen("dev1") // no-op, must not panic
}

func TestStartListenResolvesIP(t *testing.T) {
	path := filepath.Join(t.TempDir(), "hub.db")
	addDevice(t, path, db.Device{ID: "dev1", Name: "sala", Version: "3.3", LocalKey: "0123456789abcdef"})

	svc := newService(t, path, Config{ScanTimeout: time.Second})

	ctx := context.Background()

	// The device does not answer discovery: report a clear, actionable error.
	svc.discover = func(context.Context, time.Duration) ([]tuya.Discovered, error) {
		return nil, nil
	}
	if err := svc.StartListen(ctx, "dev1"); !errors.Is(err, ErrNoIP) {
		t.Fatalf("expected ErrNoIP, got %v", err)
	}

	// Now the device is found: the address is persisted before dialing.
	svc.discover = func(context.Context, time.Duration) ([]tuya.Discovered, error) {
		return []tuya.Discovered{{ID: "dev1", IP: "127.0.0.1"}}, nil
	}
	if err := svc.StartListen(ctx, "dev1"); err != nil {
		t.Fatal(err)
	}
	defer svc.StopListen("dev1")

	d, err := svc.store.GetDevice(ctx, "dev1")
	if err != nil {
		t.Fatal(err)
	}
	if d.IP != "127.0.0.1" {
		t.Fatalf("resolved IP not persisted: %+v", d)
	}
}

func TestEventHandler(t *testing.T) {
	svc := newService(t, filepath.Join(t.TempDir(), "hub.db"), Config{})

	var mu sync.Mutex
	var events []EventRecord
	var device string
	svc.SetEventHandler(func(id string, e EventRecord) {
		mu.Lock()
		defer mu.Unlock()
		device = id
		events = append(events, e)
	})

	// appendEvent only reports for a device that is being listened to.
	_, cancel := context.WithCancel(context.Background())
	defer cancel()
	svc.mu.Lock()
	svc.listeners["dev1"] = &listener{cancel: cancel}
	svc.mu.Unlock()

	svc.appendEvent("dev1", EventRecord{Kind: "dp", DPs: map[string]any{"1": true}})
	mu.Lock()
	if device != "dev1" || len(events) != 1 || events[0].Kind != "dp" {
		t.Fatalf("handler not called correctly: %q %+v", device, events)
	}
	mu.Unlock()

	svc.SetEventHandler(func(string, EventRecord) { t.Error("handler fired without a listener") })
	svc.appendEvent("ghost", EventRecord{Kind: "dp"})
}

func TestStartListenErrorEvent(t *testing.T) {
	path := filepath.Join(t.TempDir(), "hub.db")
	// Port 1 has nothing listening, so the dial fails and the service must
	// surface the failure as an "error" event instead of staying silent.
	addDevice(t, path, db.Device{ID: "dev1", Name: "sala", IP: "127.0.0.1", Port: 1, Version: "3.3", LocalKey: "0123456789abcdef"})

	svc := newService(t, path, Config{ScanTimeout: 200 * time.Millisecond})

	if err := svc.StartListen(context.Background(), "dev1"); err != nil {
		t.Fatal(err)
	}
	defer svc.StopListen("dev1")

	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		ev, ok := svc.LastEvent("dev1")
		if ok {
			if ev.Kind != "error" {
				t.Fatalf("unexpected event: %+v", ev)
			}
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatal("no error event was emitted for a failing dial")
}

func TestStopListenDisablesAutoListen(t *testing.T) {
	svc := newService(t, filepath.Join(t.TempDir(), "hub.db"), Config{})

	svc.StopListen("dev1")
	svc.mu.Lock()
	disabled := svc.disabled["dev1"]
	svc.mu.Unlock()
	if !disabled {
		t.Fatal("StopListen should mark the device disabled so auto-listen leaves it alone")
	}

	// An explicit start clears the flag (the missing device then fails with
	// ErrNotFound, but only after the flag is cleared).
	if err := svc.StartListen(context.Background(), "dev1"); !errors.Is(err, db.ErrNotFound) {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
	svc.mu.Lock()
	disabled = svc.disabled["dev1"]
	svc.mu.Unlock()
	if disabled {
		t.Fatal("StartListen should clear the disabled flag")
	}
}

func TestRefreshFallsBackWithoutListener(t *testing.T) {
	path := filepath.Join(t.TempDir(), "hub.db")
	addDevice(t, path, db.Device{ID: "dev1", Name: "sala", IP: "127.0.0.1"})
	svc := newService(t, path, Config{})

	if err := svc.queryViaListener("dev1"); !errors.Is(err, errNoListener) {
		t.Fatalf("expected errNoListener, got %v", err)
	}
	// Without a listener, Refresh falls back to a short-lived connection; with
	// no local key there is nothing to dial, so it reports ErrNoKey.
	if _, err := svc.Refresh(context.Background(), "dev1"); !errors.Is(err, ErrNoKey) {
		t.Fatalf("expected ErrNoKey, got %v", err)
	}
}

func TestAutoListenSkipsUnkeyedAndRefreshStaleSafe(t *testing.T) {
	path := filepath.Join(t.TempDir(), "hub.db")
	addDevice(t, path, db.Device{ID: "nokey", Name: "a"})
	svc := newService(t, path, Config{})

	// A device without a local key cannot be dialed; auto-listen skips it.
	svc.autoListen(context.Background())
	if got := svc.Listening(); len(got) != 0 {
		t.Fatalf("no device should be listening: %v", got)
	}
	// refreshStale must be safe with no listeners.
	svc.refreshStale(time.Second)
}

func TestOnlineReflectsListenerAndRecentSeen(t *testing.T) {
	svc := newService(t, filepath.Join(t.TempDir(), "hub.db"), Config{})

	if svc.Online("dev1") {
		t.Fatal("unknown device should not be online")
	}
	svc.markSeen("dev1")
	if !svc.Online("dev1") {
		t.Fatal("a recently seen device should be online")
	}
	svc.mu.Lock()
	svc.lastSeen["dev1"] = time.Now().Add(-10 * time.Minute)
	svc.mu.Unlock()
	if svc.Online("dev1") {
		t.Fatal("a stale device should be offline")
	}

	// A live listener counts as online even without recent events.
	_, cancel := context.WithCancel(context.Background())
	defer cancel()
	svc.mu.Lock()
	svc.listeners["dev2"] = &listener{cancel: cancel, client: &tuya.Client{}}
	svc.mu.Unlock()
	if !svc.Online("dev2") {
		t.Fatal("a device with a live listener should be online")
	}
}

func TestForget(t *testing.T) {
	path := filepath.Join(t.TempDir(), "hub.db")
	addDevice(t, path, db.Device{ID: "dev1", Name: "sala", LocalKey: "0123456789abcdef"})
	svc := newService(t, path, Config{})

	if err := svc.Forget(context.Background(), "dev1"); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.store.GetDevice(context.Background(), "dev1"); !errors.Is(err, db.ErrNotFound) {
		t.Fatalf("expected ErrNotFound after forget, got %v", err)
	}
	if err := svc.Forget(context.Background(), "dev1"); !errors.Is(err, db.ErrNotFound) {
		t.Fatalf("expected ErrNotFound on second forget, got %v", err)
	}
}

func TestControlCloudFallback(t *testing.T) {
	path := filepath.Join(t.TempDir(), "hub.db")
	addDevice(t, path, db.Device{ID: "dev1", Kind: db.KindTuya, Name: "lamp"})

	var mu sync.Mutex
	commanded := false
	mux := http.NewServeMux()
	writeJSON := func(w http.ResponseWriter, v any) { _ = json.NewEncoder(w).Encode(v) }
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1.0/token":
			writeJSON(w, map[string]any{"success": true, "result": map[string]any{"access_token": "tok", "expire_time": 3600}})
		case "/v1.1/devices/dev1/specifications":
			// The same DP appears in functions and status: it must be merged.
			writeJSON(w, map[string]any{"success": true, "result": map[string]any{
				"functions": []any{map[string]any{"code": "switch_led", "dp_id": "1", "type": "Boolean"}},
				"status":    []any{map[string]any{"code": "switch_led", "dp_id": "1", "type": "Boolean"}},
			}})
		case "/v1.0/devices/dev1/commands":
			mu.Lock()
			commanded = true
			mu.Unlock()
			writeJSON(w, map[string]any{"success": true, "result": true})
		default:
			writeJSON(w, map[string]any{"success": false, "code": 404, "msg": "not found"})
		}
	})
	cloud := httptest.NewServer(mux)
	defer cloud.Close()

	svc := newService(t, path, Config{Cloud: tuya.CloudConfig{AccessID: "id", AccessSecret: "secret", Region: "us", BaseURL: cloud.URL}})

	specs, err := svc.RefreshSpecs(context.Background(), "dev1")
	if err != nil {
		t.Fatal(err)
	}
	if len(specs) != 1 || specs[0].Code != "switch_led" || !specs[0].Writable {
		t.Fatalf("specs not cached correctly: %+v", specs)
	}

	// A device without a local key can only be controlled through the cloud,
	// which needs the DP code resolved from the cached specs.
	res, err := svc.Control(context.Background(), "dev1", "1", true)
	if err != nil {
		t.Fatal(err)
	}
	if res["mode"] != "cloud" {
		t.Fatalf("mode=%v", res["mode"])
	}
	mu.Lock()
	defer mu.Unlock()
	if !commanded {
		t.Fatal("cloud command was not sent")
	}
}

func TestObservedSpecsInference(t *testing.T) {
	path := filepath.Join(t.TempDir(), "hub.db")
	addDevice(t, path, db.Device{ID: "dev1", Kind: db.KindTuya, Name: "sala"})
	svc := newService(t, path, Config{})

	// No cloud and no cached specs: the service infers them from events.
	if specs, err := svc.Specs(context.Background(), "dev1"); err != nil || len(specs) != 0 {
		t.Fatalf("expected no specs yet: %v %+v", err, specs)
	}

	_, cancel := context.WithCancel(context.Background())
	defer cancel()
	svc.mu.Lock()
	svc.listeners["dev1"] = &listener{cancel: cancel}
	svc.mu.Unlock()

	svc.appendEvent("dev1", EventRecord{Kind: "status", DPs: map[string]any{"1": true, "2": float64(3), "3": "white"}})

	specs, err := svc.Specs(context.Background(), "dev1")
	if err != nil {
		t.Fatal(err)
	}
	byID := map[string]string{}
	for _, s := range specs {
		byID[s.DPID] = s.Type
	}
	if byID["1"] != "Boolean" || byID["2"] != "Integer" || byID["3"] != "String" {
		t.Fatalf("inferred types wrong: %+v", byID)
	}

	// Forget clears the inferred specs.
	if err := svc.Forget(context.Background(), "dev1"); err != nil {
		t.Fatal(err)
	}
	if got := svc.observedSpecs("dev1"); len(got) != 0 {
		t.Fatalf("observed specs not cleared: %+v", got)
	}
}

func TestParseVersion(t *testing.T) {
	if parseVersion("") != "" || parseVersion("auto") != "" {
		t.Fatal("empty/auto should yield an empty version")
	}
	if parseVersion("3.5") != tuya.V35 || parseVersion("3.3") != tuya.V33 {
		t.Fatal("version parsing failed")
	}
	if parseVersion("nope") != "" {
		t.Fatal("invalid version should be empty")
	}
}

func TestStatePersistAndCloudRefresh(t *testing.T) {
	path := filepath.Join(t.TempDir(), "hub.db")
	addDevice(t, path, db.Device{ID: "dev1", Kind: db.KindTuya, Name: "tomada"})

	var stateValue any = true
	mux := http.NewServeMux()
	writeJSON := func(w http.ResponseWriter, v any) { _ = json.NewEncoder(w).Encode(v) }
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1.0/token":
			writeJSON(w, map[string]any{"success": true, "result": map[string]any{"access_token": "tok", "expire_time": 3600}})
		case "/v1.1/devices/dev1/specifications":
			writeJSON(w, map[string]any{"success": true, "result": map[string]any{
				"functions": []any{map[string]any{"code": "switch_1", "dp_id": "1", "type": "Boolean"}},
				"status":    []any{},
			}})
		case "/v1.0/devices/dev1/status":
			writeJSON(w, map[string]any{"success": true, "result": []any{
				map[string]any{"code": "switch_1", "value": stateValue},
			}})
		default:
			writeJSON(w, map[string]any{"success": false, "code": 404, "msg": "not found"})
		}
	})
	cloud := httptest.NewServer(mux)
	defer cloud.Close()

	svc := newService(t, path, Config{Cloud: tuya.CloudConfig{AccessID: "id", AccessSecret: "secret", Region: "us", BaseURL: cloud.URL}})
	ctx := context.Background()

	// State observed while listening is persisted.
	_, cancel := context.WithCancel(ctx)
	defer cancel()
	svc.mu.Lock()
	svc.listeners["dev1"] = &listener{cancel: cancel}
	svc.mu.Unlock()
	svc.appendEvent("dev1", EventRecord{Kind: "status", DPs: map[string]any{"1": false}})
	if st, _ := svc.State(ctx, "dev1"); st["1"] != false {
		t.Fatalf("observed state not persisted: %+v", st)
	}

	// A refresh reads the device through the cloud (the DP code maps to dp 1).
	if _, err := svc.RefreshSpecs(ctx, "dev1"); err != nil {
		t.Fatal(err)
	}
	dps, err := svc.RefreshState(ctx, "dev1")
	if err != nil {
		t.Fatal(err)
	}
	if dps["1"] != true {
		t.Fatalf("refreshed state wrong: %+v", dps)
	}
	if st, _ := svc.State(ctx, "dev1"); st["1"] != true {
		t.Fatalf("refreshed state not persisted: %+v", st)
	}
}

func TestIsCloudOnlyCategory(t *testing.T) {
	for _, c := range []string{"jtmspro", "ms", "mk", "JTMSBH", " hotelms "} {
		if !IsCloudOnlyCategory(c) {
			t.Fatalf("%q should be a cloud-only (sleeping) category", c)
		}
	}
	for _, c := range []string{"cz", "qt", ""} {
		if IsCloudOnlyCategory(c) {
			t.Fatalf("%q should not be cloud-only", c)
		}
	}
}

// TestRegisterMarksLockCloudOnly checks that registering a sleeping battery
// device (a lock, category jtmspro) from the cloud stores its category and
// defaults it to cloud_only, so the hub never opens a LAN listener for it.
func TestRegisterMarksLockCloudOnly(t *testing.T) {
	path := filepath.Join(t.TempDir(), "hub.db")
	addDevice(t, path, db.Device{ID: "seed", Name: "sala"})

	mux := http.NewServeMux()
	writeJSON := func(w http.ResponseWriter, v any) { _ = json.NewEncoder(w).Encode(v) }
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1.0/token":
			writeJSON(w, map[string]any{"success": true, "result": map[string]any{"access_token": "tok", "expire_time": 3600}})
		case "/v1.0/devices/seed", "/v1.0/devices/lock1":
			writeJSON(w, map[string]any{"success": true, "result": map[string]any{"uid": "uid-1"}})
		case "/v1.0/users/uid-1/devices":
			writeJSON(w, map[string]any{"success": true, "result": []any{
				map[string]any{"id": "lock1", "name": "Fechadura", "local_key": "0123456789abcdef", "product_id": "pk1", "category": "jtmspro"},
			}})
		case "/v1.1/devices/lock1/specifications":
			writeJSON(w, map[string]any{"success": true, "result": map[string]any{"functions": []any{}, "status": []any{}}})
		default:
			writeJSON(w, map[string]any{"success": false, "code": 404, "msg": "not found"})
		}
	})
	cloud := httptest.NewServer(mux)
	defer cloud.Close()

	svc := newService(t, path, Config{Cloud: tuya.CloudConfig{AccessID: "id", AccessSecret: "secret", Region: "us", BaseURL: cloud.URL}})
	svc.discover = func(context.Context, time.Duration) ([]tuya.Discovered, error) { return nil, nil }

	registered, err := svc.Register(context.Background(), []string{"lock1"})
	if err != nil {
		t.Fatal(err)
	}
	if len(registered) != 1 {
		t.Fatalf("registered %d devices, want 1", len(registered))
	}
	got := registered[0]
	if got.Category != "jtmspro" || !got.CloudOnly {
		t.Fatalf("lock not stored as cloud_only: %+v", got)
	}
}

// TestAutoListenSkipsCloudOnly ensures a cloud_only device is never dialed on
// the LAN, even though it has a key and an address, and that the cached set is
// populated (used by Online).
func TestAutoListenSkipsCloudOnly(t *testing.T) {
	path := filepath.Join(t.TempDir(), "hub.db")
	addDevice(t, path, db.Device{
		ID: "lock1", Kind: db.KindTuya, Name: "Fechadura", LocalKey: "0123456789abcdef",
		IP: "127.0.0.1", Category: "jtmspro", CloudOnly: true,
	})
	svc := newService(t, path, Config{})

	svc.autoListen(context.Background())
	if got := svc.Listening(); len(got) != 0 {
		t.Fatalf("a cloud_only device must not be listened to: %v", got)
	}
	svc.mu.Lock()
	cached := svc.cloudOnly["lock1"]
	svc.mu.Unlock()
	if !cached {
		t.Fatal("cloud_only set not cached for Online")
	}
}

// TestControlCloudOnlyFallsBackToLAN checks that a cloud_only device still tries
// the LAN when the cloud is unavailable (the fallback), instead of failing
// outright.
func TestControlCloudOnlyFallsBackToLAN(t *testing.T) {
	path := filepath.Join(t.TempDir(), "hub.db")
	addDevice(t, path, db.Device{
		ID: "lock1", Kind: db.KindTuya, Name: "Fechadura", LocalKey: "0123456789abcdef",
		IP: "127.0.0.1", Port: 1, CloudOnly: true,
	})
	svc := newService(t, path, Config{ScanTimeout: 200 * time.Millisecond})

	// No cloud configured and a dead LAN address: the LAN was attempted, so the
	// error is a connection failure, not ErrNoKey.
	if _, err := svc.Control(context.Background(), "lock1", "1", true); err == nil || errors.Is(err, ErrNoKey) {
		t.Fatalf("expected a LAN failure after the cloud was unavailable, got %v", err)
	}
}

// TestControlCloudOnlyReportsBothFailures checks that when the cloud fails and
// the LAN fallback also fails, the error names both.
func TestControlCloudOnlyReportsBothFailures(t *testing.T) {
	path := filepath.Join(t.TempDir(), "hub.db")
	addDevice(t, path, db.Device{
		ID: "lock1", Kind: db.KindTuya, Name: "Fechadura", LocalKey: "0123456789abcdef",
		IP: "127.0.0.1", Port: 1, CloudOnly: true,
	})

	mux := http.NewServeMux()
	writeJSON := func(w http.ResponseWriter, v any) { _ = json.NewEncoder(w).Encode(v) }
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1.0/token":
			writeJSON(w, map[string]any{"success": true, "result": map[string]any{"access_token": "tok", "expire_time": 3600}})
		case "/v1.0/devices/lock1/commands":
			writeJSON(w, map[string]any{"success": false, "code": 500, "msg": "device offline"})
		default:
			writeJSON(w, map[string]any{"success": false, "code": 404, "msg": "not found"})
		}
	})
	cloud := httptest.NewServer(mux)
	defer cloud.Close()

	svc := newService(t, path, Config{Cloud: tuya.CloudConfig{AccessID: "id", AccessSecret: "secret", Region: "us", BaseURL: cloud.URL}, ScanTimeout: 200 * time.Millisecond})
	_, err := svc.Control(context.Background(), "lock1", "1", true)
	if err == nil || !strings.Contains(err.Error(), "LAN fallback failed") {
		t.Fatalf("esperado erro citando o fallback da LAN, got %v", err)
	}
}

// TestOnlineCloudOnlyWindow checks that a cloud_only device stays online for
// the poll window instead of the short LAN window.
func TestOnlineCloudOnlyWindow(t *testing.T) {
	svc := newService(t, filepath.Join(t.TempDir(), "hub.db"), Config{})

	svc.mu.Lock()
	svc.cloudOnly["lock1"] = true
	svc.cloudTTL = 5 * time.Minute
	svc.lastSeen["lock1"] = time.Now().Add(-3 * time.Minute)
	svc.mu.Unlock()
	if !svc.Online("lock1") {
		t.Fatal("a cloud_only device within the poll window should be online")
	}

	svc.mu.Lock()
	svc.lastSeen["lock1"] = time.Now().Add(-10 * time.Minute)
	svc.mu.Unlock()
	if svc.Online("lock1") {
		t.Fatal("a cloud_only device past the poll window should be offline")
	}
}

// TestProcessReportsSeedsThenFires checks the report watcher: the first pass
// seeds the cursor and fires nothing; a newer report fires exactly once, keyed
// by DP id with the value coerced (a numeric string becomes a number).
func TestProcessReportsSeedsThenFires(t *testing.T) {
	path := filepath.Join(t.TempDir(), "hub.db")
	addDevice(t, path, db.Device{ID: "lock1", Kind: db.KindTuya, Name: "Fechadura", Category: "jtmspro", CloudOnly: true})

	var mu sync.Mutex
	entries := []any{}

	mux := http.NewServeMux()
	writeJSON := func(w http.ResponseWriter, v any) { _ = json.NewEncoder(w).Encode(v) }
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1.0/token":
			writeJSON(w, map[string]any{"success": true, "result": map[string]any{"access_token": "tok", "expire_time": 3600}})
		case "/v1.0/iot-03/devices/lock1/report-logs":
			mu.Lock()
			list := append([]any(nil), entries...)
			mu.Unlock()
			writeJSON(w, map[string]any{"success": true, "result": map[string]any{"has_more": false, "list": list}})
		default:
			writeJSON(w, map[string]any{"success": false, "code": 404, "msg": "not found"})
		}
	})
	cloud := httptest.NewServer(mux)
	defer cloud.Close()

	svc := newService(t, path, Config{Cloud: tuya.CloudConfig{AccessID: "id", AccessSecret: "secret", Region: "us", BaseURL: cloud.URL}})
	ctx := context.Background()

	// The cached specs map the report code to its DP id.
	if err := svc.store.ReplaceSpecs(ctx, "lock1", []db.Spec{{DeviceID: "lock1", DPID: "1", Code: "unlock_fingerprint"}}); err != nil {
		t.Fatal(err)
	}

	var emu sync.Mutex
	var events []EventRecord
	svc.SetEventHandler(func(_ string, e EventRecord) {
		emu.Lock()
		events = append(events, e)
		emu.Unlock()
	})

	dev, err := svc.store.GetDevice(ctx, "lock1")
	if err != nil {
		t.Fatal(err)
	}

	// First pass: seed the cursor at the newest entry and fire nothing.
	mu.Lock()
	entries = []any{map[string]any{"code": "unlock_fingerprint", "event_time": 1000, "value": "11"}}
	mu.Unlock()
	svc.processReports(ctx, dev)
	if ts, ok, _ := svc.store.ReportCursor(ctx, "lock1"); !ok || ts != 1000 {
		t.Fatalf("cursor not seeded: ts=%d ok=%v", ts, ok)
	}
	emu.Lock()
	n := len(events)
	emu.Unlock()
	if n != 0 {
		t.Fatalf("first pass should not fire, got %d events", n)
	}

	// Second pass: a newer entry fires once; the boundary entry is skipped.
	mu.Lock()
	entries = []any{
		map[string]any{"code": "unlock_fingerprint", "event_time": 1000, "value": "11"},
		map[string]any{"code": "unlock_fingerprint", "event_time": 2000, "value": "12"},
	}
	mu.Unlock()
	svc.processReports(ctx, dev)

	emu.Lock()
	got := append([]EventRecord(nil), events...)
	emu.Unlock()
	if len(got) != 1 {
		t.Fatalf("want 1 event, got %+v", got)
	}
	if got[0].Kind != "report" {
		t.Fatalf("kind = %q, want report", got[0].Kind)
	}
	if got[0].DPs["1"] != float64(12) {
		t.Fatalf("dp value not coerced/keyed by id: %+v", got[0].DPs)
	}
	if ts, _, _ := svc.store.ReportCursor(ctx, "lock1"); ts != 2000 {
		t.Fatalf("cursor not advanced: %d", ts)
	}
}

func TestOpenRecordsWithoutCloud(t *testing.T) {
	path := filepath.Join(t.TempDir(), "hub.db")
	addDevice(t, path, db.Device{ID: "lock1", Kind: db.KindTuya, Name: "Fechadura", Category: "jtmspro", CloudOnly: true})
	svc := newService(t, path, Config{})

	res, err := svc.OpenRecords(context.Background(), "lock1", 0)
	if err != nil {
		t.Fatal(err)
	}
	if res.Available || res.Reason != RecordsReasonCloudNotConfigured {
		t.Fatalf("expected cloud_not_configured, got %+v", res)
	}

	// An unknown device is a hard error.
	if _, err := svc.OpenRecords(context.Background(), "ghost", 0); !errors.Is(err, db.ErrNotFound) {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
}

func TestOpenRecordsFromCloud(t *testing.T) {
	path := filepath.Join(t.TempDir(), "hub.db")
	addDevice(t, path, db.Device{ID: "lock1", Kind: db.KindTuya, Name: "Fechadura", Category: "jtmspro", CloudOnly: true})

	mux := http.NewServeMux()
	writeJSON := func(w http.ResponseWriter, v any) { _ = json.NewEncoder(w).Encode(v) }
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1.0/token":
			writeJSON(w, map[string]any{"success": true, "result": map[string]any{"access_token": "tok", "expire_time": 3600}})
		case "/v1.0/devices/lock1/door-lock/open-logs":
			writeJSON(w, map[string]any{"success": true, "result": map[string]any{
				"total": 1,
				"logs": []any{map[string]any{
					"unlock_name": "Finger andrew",
					"nick_name":   "andre",
					"update_time": 1700086300000,
					"status":      map[string]any{"code": "unlock_fingerprint", "value": "1"},
				}},
			}})
		default:
			writeJSON(w, map[string]any{"success": false, "code": 404, "msg": "not found"})
		}
	})
	cloud := httptest.NewServer(mux)
	defer cloud.Close()

	svc := newService(t, path, Config{Cloud: tuya.CloudConfig{AccessID: "id", AccessSecret: "secret", Region: "us", BaseURL: cloud.URL}})
	res, err := svc.OpenRecords(context.Background(), "lock1", 0)
	if err != nil {
		t.Fatal(err)
	}
	if !res.Available || res.Total != 1 || len(res.Records) != 1 {
		t.Fatalf("unexpected result: %+v", res)
	}
	if res.Records[0].UnlockName != "Finger andrew" || res.Records[0].Code != "unlock_fingerprint" {
		t.Fatalf("unexpected record: %+v", res.Records[0])
	}
}

// TestIngestCloudReport covers the push path: the first report seeds the cursor
// and fires nothing; a newer report fires once, keyed by DP id with the value
// coerced; a repeated report does not fire again; and a device that is not
// cloud_only is ignored.
func TestIngestCloudReport(t *testing.T) {
	path := filepath.Join(t.TempDir(), "hub.db")
	addDevice(t, path, db.Device{ID: "lock1", Kind: db.KindTuya, Name: "Fechadura", Category: "jtmspro", CloudOnly: true})
	addDevice(t, path, db.Device{ID: "plug", Kind: db.KindTuya, Name: "Tomada"})

	svc := newService(t, path, Config{})
	ctx := context.Background()
	if err := svc.store.ReplaceSpecs(ctx, "lock1", []db.Spec{{DeviceID: "lock1", DPID: "1", Code: "unlock_fingerprint"}}); err != nil {
		t.Fatal(err)
	}

	var mu sync.Mutex
	var events []EventRecord
	svc.SetEventHandler(func(_ string, e EventRecord) {
		mu.Lock()
		events = append(events, e)
		mu.Unlock()
	})
	count := func() int {
		mu.Lock()
		defer mu.Unlock()
		return len(events)
	}

	now := time.Now().UnixMilli()

	// 1) First contact seeds the cursor and fires nothing.
	svc.ingestCloudReport(cloudstream.Report{DeviceID: "lock1", DPs: []cloudstream.DP{{Code: "unlock_fingerprint", Value: "11", At: now}}})
	if ts, ok, _ := svc.store.ReportCursor(ctx, "lock1"); !ok || ts != now {
		t.Fatalf("cursor não semeado: ts=%d ok=%v", ts, ok)
	}
	if count() != 0 {
		t.Fatalf("o primeiro relato não deve disparar: %d", count())
	}

	// 2) A newer report fires once, keyed by dp id with a numeric value.
	svc.ingestCloudReport(cloudstream.Report{DeviceID: "lock1", DPs: []cloudstream.DP{{Code: "unlock_fingerprint", Value: "12", At: now + 1000}}})
	if count() != 1 {
		t.Fatalf("want 1 evento, got %d", count())
	}
	if events[0].Kind != "report" || events[0].DPs["1"] != float64(12) {
		t.Fatalf("evento inesperado: %+v", events[0])
	}

	// 3) The same report again does nothing (cursor).
	svc.ingestCloudReport(cloudstream.Report{DeviceID: "lock1", DPs: []cloudstream.DP{{Code: "unlock_fingerprint", Value: "12", At: now + 1000}}})
	if count() != 1 {
		t.Fatalf("relato repetido não deveria disparar: %d", count())
	}

	// 4) A device that is not cloud_only is ignored.
	svc.ingestCloudReport(cloudstream.Report{DeviceID: "plug", DPs: []cloudstream.DP{{Code: "switch_1", Value: true, At: now + 2000}}})
	if count() != 1 {
		t.Fatalf("device não-cloud deveria ser ignorado: %d", count())
	}
	if _, ok, _ := svc.store.ReportCursor(ctx, "plug"); ok {
		t.Fatal("não deveria criar cursor para um device não-cloud")
	}

	// 5) An unknown device is ignored without panicking.
	svc.ingestCloudReport(cloudstream.Report{DeviceID: "ghost", DPs: []cloudstream.DP{{Code: "x", Value: 1, At: now + 3000}}})
}

// TestCloudDevices covers the listing that lets a device which never shows up in
// a LAN scan (a sleeping lock) be registered.
func TestCloudDevices(t *testing.T) {
	path := filepath.Join(t.TempDir(), "hub.db")
	addDevice(t, path, db.Device{ID: "seed", Kind: db.KindTuya, Name: "tomada"})

	mux := http.NewServeMux()
	writeJSON := func(w http.ResponseWriter, v any) { _ = json.NewEncoder(w).Encode(v) }
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1.0/token":
			writeJSON(w, map[string]any{"success": true, "result": map[string]any{"access_token": "tok", "expire_time": 3600}})
		case "/v1.0/devices/seed":
			writeJSON(w, map[string]any{"success": true, "result": map[string]any{"uid": "uid-1"}})
		case "/v1.0/users/uid-1/devices":
			writeJSON(w, map[string]any{"success": true, "result": []any{
				map[string]any{"id": "seed", "name": "Tomada", "local_key": "0123456789abcdef", "category": "cz"},
				map[string]any{"id": "lock1", "name": "Fechadura", "local_key": "fedcba9876543210", "category": "jtmspro"},
			}})
		default:
			writeJSON(w, map[string]any{"success": false, "code": 404, "msg": "not found"})
		}
	})
	cloud := httptest.NewServer(mux)
	defer cloud.Close()

	svc := newService(t, path, Config{Cloud: tuya.CloudConfig{AccessID: "id", AccessSecret: "secret", Region: "us", BaseURL: cloud.URL}})
	list, err := svc.CloudDevices(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 2 {
		t.Fatalf("got %d cloud devices, want 2", len(list))
	}
	byID := map[string]CloudDeviceInfo{}
	for _, d := range list {
		byID[d.ID] = d
	}
	if !byID["seed"].Registered {
		t.Fatalf("the seed device should be marked registered: %+v", byID["seed"])
	}
	if byID["lock1"].Registered || byID["lock1"].Category != "jtmspro" || !byID["lock1"].HasKey {
		t.Fatalf("unexpected lock entry: %+v", byID["lock1"])
	}

	// Without cloud credentials the listing is unavailable.
	plain := newService(t, filepath.Join(t.TempDir(), "outra.db"), Config{})
	if _, err := plain.CloudDevices(context.Background()); !errors.Is(err, ErrCloudNotConfigured) {
		t.Fatalf("expected ErrCloudNotConfigured, got %v", err)
	}
}
