package hub

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"smart_home/hub/internal/config"
	"smart_home/hub/internal/db"
	"smart_home/hub/internal/eventbus"
	"smart_home/hub/internal/model"
)

func testHub(t *testing.T) *Hub {
	t.Helper()
	h, err := New(config.Config{FirmwareDir: t.TempDir()}, nil)
	if err != nil {
		t.Fatal(err)
	}

	h.onManifest("t1", &model.Manifest{
		SchemaVersion:   1,
		ManifestVersion: 1,
		Thing:           "t1",
		Firmware:        model.Firmware{Version: "1.0.0"},
		Devices: []model.Device{
			{
				ID:   "fan0",
				Name: "Sala",
				Capabilities: map[string]model.Capability{
					"power": {Kind: "switch"},
					"speed": {Kind: "number", Min: fptr(0), Max: fptr(6), Step: fptr(1)},
					"light": {Kind: "switch"},
				},
				Actions: []string{"state.correct", "learn.start", "learn.cancel", "fan.rename"},
			},
		},
		Actions: []string{"fan.add", "fan.remove", "config.get", "config.set", "reboot", "ota.install"},
	})
	h.onStatus("t1", &model.Status{Online: true, Thing: "t1", Version: "1.0.0"})
	h.IngestState("t1", "fan0", map[string]any{
		"power": true, "light": false, "speed": int64(2), "direction": "forward",
		"source": "http", "updatedAt": int64(100), "counter": int64(1),
	})
	return h
}

func fptr(v float64) *float64 { return &v }

// staticDir is a test directory of LAN credentials.
type staticDir struct {
	tokens map[string]string
	urls   map[string]string
}

func (d staticDir) DeviceToken(thing string) string { return d.tokens[thing] }
func (d staticDir) DeviceURL(thing string) string   { return d.urls[thing] }

// fakeFans is an in-memory FanRegistry used to test registration and slot
// reindexing.
type fakeFans struct {
	staticDir
	fans map[string]*db.Device // key: owner/device_id
}

func (f *fakeFans) ListFansByOwner(_ context.Context, owner string) ([]db.Device, error) {
	out := []db.Device{}
	for _, d := range f.fans {
		if d.Owner == owner && d.Kind == db.KindFan {
			out = append(out, *d)
		}
	}
	return out, nil
}

func (f *fakeFans) UpsertFan(_ context.Context, owner, deviceID, firmwareName string) error {
	key := db.FanID(owner, deviceID)
	d := f.fans[key]
	if d == nil {
		d = &db.Device{ID: key, Kind: db.KindFan, Owner: owner, DeviceID: deviceID, Name: firmwareName}
		f.fans[key] = d
	}
	if firmwareName != "" {
		d.FirmwareName = firmwareName
	}
	return nil
}

func (f *fakeFans) RenameFan(_ context.Context, owner, deviceID, name string) error {
	if d := f.fans[db.FanID(owner, deviceID)]; d != nil {
		d.Name = name
	}
	return nil
}

func (f *fakeFans) SetFanFirmwareName(_ context.Context, owner, deviceID, name string) error {
	if d := f.fans[db.FanID(owner, deviceID)]; d != nil {
		d.FirmwareName = name
	}
	return nil
}

func (f *fakeFans) DeleteFan(_ context.Context, owner, deviceID string) error {
	delete(f.fans, db.FanID(owner, deviceID))
	return nil
}

func (f *fakeFans) SetDeviceRoom(_ context.Context, id, roomID string) error {
	if d := f.fans[id]; d != nil {
		d.RoomID = roomID
	}
	return nil
}

func TestProbeThingOverLAN(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/manifest":
			io.WriteString(w, `{"schemaVersion":1,"manifestVersion":3,"thing":"gw","model":"smart-control","protocol":"rf433","firmware":{"version":"1.0.0","build":"x"},"http":{"mdns":"gw.local","port":80},"actions":["fan.add"],"devices":[{"id":"fan0","type":"ceiling_fan","name":"Sala","capabilities":{"power":{"kind":"switch"},"speed":{"kind":"number","min":0,"max":6,"step":1}},"actions":["state.correct"]}]}`)
		case "/api/state":
			io.WriteString(w, `{"ok":true,"fans":[{"id":"fan0","name":"Sala","state":{"power":true,"light":false,"speed":2,"direction":"forward"},"source":"boot","updatedAt":100,"counter":1}]}`)
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	fp := &fakeFans{
		staticDir: staticDir{tokens: map[string]string{"gw": "tok"}, urls: map[string]string{"gw": srv.URL}},
		fans:      map[string]*db.Device{},
	}
	h, err := New(config.Config{FirmwareDir: t.TempDir()}, fp)
	if err != nil {
		t.Fatal(err)
	}

	if err := h.ProbeThing(context.Background(), "gw"); err != nil {
		t.Fatal(err)
	}

	// The device is registered generically from the LAN manifest.
	if len(fp.fans) != 1 || fp.fans[db.FanID("gw", "fan0")] == nil {
		t.Fatalf("device not registered: %v", fp.fans)
	}
	thing, ok := h.Thing("gw")
	if !ok || thing.Manifest == nil || len(thing.Manifest.Devices) != 1 {
		t.Fatalf("manifest not applied: %+v", thing)
	}
	dev := thing.Manifest.Devices[0]
	if dev.ID != "fan0" || dev.Name != "Sala" || dev.Capabilities["speed"].Kind != "number" {
		t.Fatalf("unexpected device: %+v", dev)
	}
	// The current state is reported so the UI shows it.
	sh, ok := h.store.State("gw", "fan0")
	if !ok || sh.Reported["power"] != true {
		t.Fatalf("reported state not set: %+v", sh)
	}
}

// fakeThings is a ThingSource + staticDir used to exercise LAN discovery.
type fakeThings struct {
	staticDir
	things []db.Device
}

func (f fakeThings) ListDevices(_ context.Context, kind string) ([]db.Device, error) {
	out := []db.Device{}
	for _, d := range f.things {
		if kind == "" || d.Kind == kind {
			out = append(out, d)
		}
	}
	return out, nil
}

// TestDiscoveryProbesOverHTTP checks the HTTP-first discovery: a gateway with no
// manifest in memory is probed over the LAN, and the fast pass stops re-probing
// once the manifest is cached.
func TestDiscoveryProbesOverHTTP(t *testing.T) {
	var mu sync.Mutex
	hits := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		hits++
		mu.Unlock()
		if r.URL.Path == "/api/manifest" {
			io.WriteString(w, `{"schemaVersion":1,"manifestVersion":1,"thing":"gw","http":{"mdns":"gw.local","port":80},"devices":[{"id":"fan0","name":"Sala","capabilities":{"power":{"kind":"switch"}}}]}`)
			return
		}
		http.NotFound(w, r)
	}))
	defer srv.Close()

	dir := fakeThings{
		staticDir: staticDir{tokens: map[string]string{"gw": "tok"}, urls: map[string]string{"gw": srv.URL}},
		things:    []db.Device{{ID: "gw", Kind: db.KindThing}},
	}
	h, err := New(config.Config{FirmwareDir: t.TempDir()}, dir)
	if err != nil {
		t.Fatal(err)
	}

	if _, ok := h.Thing("gw"); ok {
		t.Fatal("manifest present before discovery")
	}
	// The fast pass (no manifest) discovers the gateway over HTTP.
	h.probeGateways(context.Background(), false)
	if thing, ok := h.Thing("gw"); !ok || thing.Manifest == nil {
		t.Fatalf("manifest not discovered over HTTP: %+v", thing)
	}

	mu.Lock()
	n := hits
	mu.Unlock()
	// With the manifest cached, the fast pass must not probe again.
	h.probeGateways(context.Background(), false)
	mu.Lock()
	after := hits
	mu.Unlock()
	if after != n {
		t.Fatalf("fast pass re-probed with a cached manifest (%d -> %d)", n, after)
	}
	// The slow pass revalidates every gateway.
	h.probeGateways(context.Background(), true)
	mu.Lock()
	reval := hits
	mu.Unlock()
	if reval == after {
		t.Fatal("revalidation did not probe")
	}
}

func TestManifestRegistersFansAndReindexes(t *testing.T) {
	fp := &fakeFans{fans: map[string]*db.Device{}}
	h, err := New(config.Config{FirmwareDir: t.TempDir()}, fp)
	if err != nil {
		t.Fatal(err)
	}

	manifest := func(ver int, devices ...[2]string) *model.Manifest {
		m := &model.Manifest{SchemaVersion: 1, ManifestVersion: ver, Thing: "t1"}
		for _, d := range devices {
			m.Devices = append(m.Devices, model.Device{ID: d[0], Name: d[1]})
		}
		return m
	}
	h.onManifest("t1", manifest(1, [2]string{"fan0", "A"}, [2]string{"fan1", "B"}, [2]string{"fan2", "C"}))
	if len(fp.fans) != 3 {
		t.Fatalf("fans not registered: %v", fp.fans)
	}
	fp.fans[db.FanID("t1", "fan1")].RoomID = "r1" // room of fan B
	fp.fans[db.FanID("t1", "fan2")].RoomID = "r2" // room of fan C

	// Remove fan1 (B): slots reindex and the physical fan C becomes fan1.
	h.onManifest("t1", manifest(2, [2]string{"fan0", "A"}, [2]string{"fan1", "C"}))

	fan1 := fp.fans[db.FanID("t1", "fan1")]
	// The hub display name chosen for the old slot follows the room; the
	// firmware name is refreshed from the manifest.
	if fan1 == nil || fan1.Name != "B" || fan1.FirmwareName != "C" || fan1.RoomID != "r2" {
		t.Fatalf("room did not follow the fan: %+v", fan1)
	}
	if _, ok := fp.fans[db.FanID("t1", "fan2")]; ok {
		t.Fatal("removed fan row kept")
	}
}

func TestManifestKeepsRowsWhenNamesRepeat(t *testing.T) {
	fp := &fakeFans{fans: map[string]*db.Device{}}
	h, err := New(config.Config{FirmwareDir: t.TempDir()}, fp)
	if err != nil {
		t.Fatal(err)
	}

	manifest := func(ver int, names ...string) *model.Manifest {
		m := &model.Manifest{SchemaVersion: 1, ManifestVersion: ver, Thing: "t1"}
		for i, name := range names {
			m.Devices = append(m.Devices, model.Device{ID: fmt.Sprintf("fan%d", i), Name: name})
		}
		return m
	}

	h.onManifest("t1", manifest(1, "A", "B", "C"))
	fp.fans[db.FanID("t1", "fan0")].RoomID = "r0"
	fp.fans[db.FanID("t1", "fan1")].RoomID = "r1"
	fp.fans[db.FanID("t1", "fan2")].RoomID = "r2"

	// Renaming every fan to the same name on the device must not move the rows:
	// the device id is the identity, and the hub display name is independent.
	h.onManifest("t1", manifest(2, "Ventilador", "Ventilador", "Ventilador"))

	for deviceID, room := range map[string]string{"fan0": "r0", "fan1": "r1", "fan2": "r2"} {
		fan := fp.fans[db.FanID("t1", deviceID)]
		if fan == nil || fan.FirmwareName != "Ventilador" || fan.RoomID != room {
			t.Fatalf("fan %s changed: %+v", deviceID, fan)
		}
	}
	if fp.fans[db.FanID("t1", "fan0")].Name != "A" {
		t.Fatalf("hub name was overwritten by the firmware rename: %+v", fp.fans[db.FanID("t1", "fan0")])
	}
}

func TestRenameIsHubLocal(t *testing.T) {
	fp := &fakeFans{fans: map[string]*db.Device{}}
	h, err := New(config.Config{FirmwareDir: t.TempDir()}, fp)
	if err != nil {
		t.Fatal(err)
	}
	h.onManifest("t1", &model.Manifest{
		SchemaVersion:   1,
		ManifestVersion: 1,
		Thing:           "t1",
		Devices: []model.Device{{
			ID:      "fan0",
			Name:    "Sala",
			Actions: []string{"fan.rename"},
		}},
	})

	// No LAN token/URL is configured, so this only succeeds if the command is
	// handled by the hub instead of being forwarded to the device.
	if _, err := h.Command(context.Background(), "t1", "fan.rename", map[string]any{
		"id": "fan0", "name": "Varanda",
	}); err != nil {
		t.Fatalf("rename: %v", err)
	}
	fan := fp.fans[db.FanID("t1", "fan0")]
	if fan == nil || fan.Name != "Varanda" || fan.FirmwareName != "Sala" {
		t.Fatalf("rename leaked into the firmware name: %+v", fan)
	}
}

func TestLearnStatusTracksSession(t *testing.T) {
	h := testHub(t)

	if s := h.LearnStatus("t1"); s["active"] != false || s["result"] != "idle" {
		t.Fatalf("initial status: %+v", s)
	}
	h.beginLearn("t1", "fan0", "light")
	if s := h.LearnStatus("t1"); s["active"] != true || s["command"] != "light" || s["device"] != "fan0" {
		t.Fatalf("active status: %+v", s)
	}
	h.endLearn("t1", "ok", "00 0A 7B 12", "")
	if s := h.LearnStatus("t1"); s["active"] != false || s["result"] != "ok" || s["frame"] != "00 0A 7B 12" {
		t.Fatalf("finished status: %+v", s)
	}
	// A learn.finished event also ends the session.
	h.beginLearn("t1", "fan0", "power")
	h.onEvent("t1", map[string]any{"event": "learn.finished", "id": "fan0", "command": "power", "ok": true, "frame": "AA"})
	if s := h.LearnStatus("t1"); s["active"] != false || s["result"] != "ok" || s["frame"] != "AA" {
		t.Fatalf("event status: %+v", s)
	}
}

func TestManifestPublishesEvent(t *testing.T) {
	h, err := New(config.Config{FirmwareDir: t.TempDir()}, nil)
	if err != nil {
		t.Fatal(err)
	}

	bus := eventbus.New()
	h.SetEventBus(bus)
	ch, cancel := bus.Subscribe()
	defer cancel()

	h.onManifest("t1", &model.Manifest{
		SchemaVersion:   1,
		ManifestVersion: 1,
		Thing:           "t1",
		Devices:         []model.Device{{ID: "fan0", Name: "Sala"}},
	})

	select {
	case ev := <-ch:
		if ev.Type != "manifest" {
			t.Fatalf("event type = %q, want manifest", ev.Type)
		}
		if ev.Data["thing"] != "t1" {
			t.Fatalf("event data = %+v", ev.Data)
		}
	case <-time.After(time.Second):
		t.Fatal("manifest event was not published")
	}
}

func TestValidateState(t *testing.T) {
	h := testHub(t)
	got, _, _, err := h.validateState("t1", "fan0", map[string]any{"power": false, "speed": 3})
	if err != nil {
		t.Fatal(err)
	}
	if got["speed"] != int64(3) {
		t.Fatalf("normalized speed=%v", got["speed"])
	}
	if _, _, _, err := h.validateState("t1", "fan0", map[string]any{"speed": 9}); err == nil {
		t.Fatal("out-of-range speed accepted")
	}
	if _, _, _, err := h.validateState("t1", "fan0", map[string]any{"bogus": true}); err == nil {
		t.Fatal("unknown capability accepted")
	}
	if _, _, _, err := h.validateState("t1", "fan9", map[string]any{"power": true}); err == nil {
		t.Fatal("unknown device accepted")
	}
}

func TestStateAndHealth(t *testing.T) {
	h := testHub(t)
	sh, ok := h.store.State("t1", "fan0")
	if !ok || sh.Reported["power"] != true {
		t.Fatalf("reported not stored: %#v", sh)
	}
	if _, ok := h.Thing("t1"); !ok {
		t.Fatal("thing missing")
	}
	health := h.Health()
	if health["things"].(int) != 1 {
		t.Fatalf("health=%v", health)
	}
	if _, err := h.SetState(context.Background(), "t1", "fan0", map[string]any{"power": false}, false); err == nil {
		t.Fatal("SetState should fail without a LAN token/URL")
	}
}

func TestNewID(t *testing.T) {
	a := newID()
	b := newID()
	if len(a) != 36 || a == b {
		t.Fatalf("ids %q %q", a, b)
	}
}

func TestFallbackStateAndCommand(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.URL.Path == "/api/state" && r.Method == http.MethodPost:
			io.WriteString(w, `{"ok":true,"state":{"power":false,"light":false,"speed":0,"direction":"forward"},"transmitted":["power"]}`)
		case r.URL.Path == "/api/state" && r.Method == http.MethodGet:
			io.WriteString(w, `{"ok":true,"fans":[{"id":"fan0","name":"Sala","state":{"power":false,"light":false,"speed":0,"direction":"forward"},"source":"http","updatedAt":200,"counter":2}]}`)
		case r.URL.Path == "/api/fan/add":
			io.WriteString(w, `{"ok":true,"id":"fan1"}`)
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	cfg := config.Config{FirmwareDir: t.TempDir()}
	dir := staticDir{
		tokens: map[string]string{"t1": "secret"},
		urls:   map[string]string{"t1": srv.URL},
	}
	h, err := New(cfg, dir)
	if err != nil {
		t.Fatal(err)
	}

	h.onManifest("t1", &model.Manifest{
		SchemaVersion:   1,
		ManifestVersion: 1,
		Thing:           "t1",
		Devices: []model.Device{{
			ID: "fan0", Name: "Sala",
			Capabilities: map[string]model.Capability{
				"power": {Kind: "switch"},
				"light": {Kind: "switch"},
				"speed": {Kind: "number", Min: fptr(0), Max: fptr(6)},
			},
		}},
		Actions: []string{"fan.add"},
	})
	h.onStatus("t1", &model.Status{Online: false, Thing: "t1", Version: "1.0.0"})
	h.IngestState("t1", "fan0", map[string]any{
		"power": true, "light": false, "speed": int64(2), "direction": "forward",
		"source": "http", "updatedAt": int64(100), "counter": int64(1),
	})

	res, err := h.SetState(context.Background(), "t1", "fan0", map[string]any{"power": false}, true)
	if err != nil {
		t.Fatal(err)
	}
	if res["mode"] != "http" {
		t.Fatalf("mode=%v", res["mode"])
	}
	sh, _ := h.store.State("t1", "fan0")
	if sh.Reported["power"] != false {
		t.Fatalf("reported power=%v", sh.Reported["power"])
	}

	ack, err := h.Command(context.Background(), "t1", "fan.add", map[string]any{})
	if err != nil {
		t.Fatal(err)
	}
	if ack["ok"] != true {
		t.Fatalf("ack=%v", ack)
	}
}

func TestSetStateDirectionSendsDirectionAlone(t *testing.T) {
	var mu sync.Mutex
	var got map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.URL.Path == "/api/state" && r.Method == http.MethodPost:
			var body map[string]any
			raw, _ := io.ReadAll(r.Body)
			_ = json.Unmarshal(raw, &body)
			mu.Lock()
			got = body
			mu.Unlock()
			io.WriteString(w, `{"ok":true,"state":{"power":true,"light":false,"speed":1,"direction":"reverse"},"transmitted":["exhaust"]}`)
		case r.URL.Path == "/api/state" && r.Method == http.MethodGet:
			io.WriteString(w, `{"ok":true,"fans":[{"id":"fan0","name":"Sala","state":{"power":true,"light":false,"speed":1,"direction":"reverse"},"source":"http","updatedAt":200,"counter":2}]}`)
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	h, err := New(config.Config{FirmwareDir: t.TempDir()}, staticDir{
		tokens: map[string]string{"t1": "secret"},
		urls:   map[string]string{"t1": srv.URL},
	})
	if err != nil {
		t.Fatal(err)
	}

	h.onManifest("t1", &model.Manifest{
		SchemaVersion:   1,
		ManifestVersion: 1,
		Thing:           "t1",
		Devices: []model.Device{{
			ID: "fan0", Name: "Sala",
			Capabilities: map[string]model.Capability{
				"power":     {Kind: "switch"},
				"light":     {Kind: "switch"},
				"speed":     {Kind: "number", Min: fptr(0), Max: fptr(6)},
				"direction": {Kind: "enum", Values: []string{"forward", "reverse"}},
			},
		}},
	})
	h.onStatus("t1", &model.Status{Online: true, Thing: "t1", Version: "1.0.0"})
	h.IngestState("t1", "fan0", map[string]any{
		"power": false, "light": false, "speed": int64(0), "direction": "forward",
		"source": "http", "updatedAt": int64(100), "counter": int64(1),
	})

	if _, err := h.SetState(context.Background(), "t1", "fan0", map[string]any{"direction": "reverse"}, false); err != nil {
		t.Fatal(err)
	}
	mu.Lock()
	defer mu.Unlock()
	if got == nil {
		t.Fatal("device did not receive the state")
	}
	state, _ := got["state"].(map[string]any)
	if len(state) != 1 || state["direction"] != "reverse" {
		t.Fatalf("direction command must be sent alone, got %v", got)
	}
	if got["force"] == true {
		t.Fatalf("force must stay false for a non-tap state: %v", got)
	}
}

// TestSetStateSendsOnlyNamedField guards the fan rule: the RF remote has one
// command per frame, so automation actions must never merge the old full state.
func TestSetStateSendsOnlyNamedField(t *testing.T) {
	var mu sync.Mutex
	var got map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.URL.Path == "/api/state" && r.Method == http.MethodPost:
			var body map[string]any
			raw, _ := io.ReadAll(r.Body)
			_ = json.Unmarshal(raw, &body)
			mu.Lock()
			got = body
			mu.Unlock()
			io.WriteString(w, `{"ok":true,"state":{"power":true,"light":false,"speed":3,"direction":"forward"},"transmitted":["vel_3"]}`)
		case r.URL.Path == "/api/state" && r.Method == http.MethodGet:
			io.WriteString(w, `{"ok":true,"fans":[{"id":"fan0","name":"Sala","state":{"power":true,"light":false,"speed":3,"direction":"forward"},"source":"http","updatedAt":200,"counter":2}]}`)
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	h, err := New(config.Config{FirmwareDir: t.TempDir()}, staticDir{
		tokens: map[string]string{"t1": "secret"},
		urls:   map[string]string{"t1": srv.URL},
	})
	if err != nil {
		t.Fatal(err)
	}
	h.onManifest("t1", &model.Manifest{
		SchemaVersion:   1,
		ManifestVersion: 1,
		Thing:           "t1",
		Devices: []model.Device{{
			ID: "fan0", Name: "Sala",
			Capabilities: map[string]model.Capability{
				"power": {Kind: "switch"},
				"light": {Kind: "switch"},
				"speed": {Kind: "number", Min: fptr(0), Max: fptr(6)},
			},
		}},
	})
	h.onStatus("t1", &model.Status{Online: true, Thing: "t1", Version: "1.0.0"})
	h.IngestState("t1", "fan0", map[string]any{
		"power": false, "light": false, "speed": int64(0),
		"source": "http", "updatedAt": int64(100), "counter": int64(1),
	})

	if _, err := h.SetState(context.Background(), "t1", "fan0", map[string]any{"speed": int64(3)}, false); err != nil {
		t.Fatal(err)
	}
	if _, err := h.SetState(context.Background(), "t1", "fan0", map[string]any{"speed": int64(3), "light": true}, false); err == nil {
		t.Fatal("two fan fields must be rejected")
	}
	mu.Lock()
	defer mu.Unlock()
	if got == nil {
		t.Fatal("device did not receive the state")
	}
	state, _ := got["state"].(map[string]any)
	if len(state) != 1 || state["speed"] != float64(3) {
		t.Fatalf("state = %v, want only speed 3", state)
	}
}
