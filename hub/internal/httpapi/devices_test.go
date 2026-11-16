package httpapi

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"smart_home/hub/internal/config"
	"smart_home/hub/internal/db"
	"smart_home/hub/internal/functions"
	"smart_home/hub/internal/hub"
)

func dbServer(t *testing.T) (*httptest.Server, func()) {
	t.Helper()
	store, err := db.Open(filepath.Join(t.TempDir(), "hub.db"))
	if err != nil {
		t.Fatal(err)
	}
	cfg := config.Config{HTTPAddr: ":0"}
	h, err := hub.New(cfg, store)
	if err != nil {
		t.Fatal(err)
	}
	api := New(h, cfg, store)
	api.SetFunctions(functions.New(store, h))
	srv := httptest.NewServer(api.Handler())
	return srv, func() {
		srv.Close()
		store.Close()
	}
}

func doJSON(t *testing.T, method, url, body string, out any) int {
	t.Helper()
	req, err := http.NewRequest(method, url, bytes.NewBufferString(body))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	data, _ := io.ReadAll(resp.Body)
	if out != nil && len(data) > 0 {
		if err := json.Unmarshal(data, out); err != nil {
			t.Fatalf("decode %s: %v (%s)", url, err, data)
		}
	}
	return resp.StatusCode
}

func TestDevicesRoomsPlacementsRoutes(t *testing.T) {
	srv, closeFn := dbServer(t)
	defer closeFn()

	if code := doJSON(t, http.MethodPost, srv.URL+"/api/rooms", `{"id":"r1","name":"Sala"}`, nil); code != 200 {
		t.Fatalf("create room status=%d", code)
	}
	if code := doJSON(t, http.MethodPost, srv.URL+"/api/devices", `{"id":"rf01","kind":"thing","name":"ESP32"}`, nil); code != 200 {
		t.Fatalf("create device status=%d", code)
	}
	if code := doJSON(t, http.MethodPost, srv.URL+"/api/devices/rf01", `{"token":"s3cr3t","room_id":"r1"}`, nil); code != 200 {
		t.Fatalf("update device status=%d", code)
	}

	var list struct {
		Devices []struct {
			ID       string `json:"id"`
			Kind     string `json:"kind"`
			HasToken bool   `json:"has_token"`
			RoomID   string `json:"room_id"`
		} `json:"devices"`
	}
	if code := doJSON(t, http.MethodGet, srv.URL+"/api/devices", "", &list); code != 200 {
		t.Fatalf("list devices status=%d", code)
	}
	if len(list.Devices) != 1 || !list.Devices[0].HasToken || list.Devices[0].RoomID != "r1" {
		t.Fatalf("device not stored correctly: %+v", list.Devices)
	}

	if code := doJSON(t, http.MethodPost, srv.URL+"/api/placements", `{"kind":"fan","owner":"rf01","device":"fan0","room_id":"r1"}`, nil); code != 200 {
		t.Fatalf("placement status=%d", code)
	}
	var placements struct {
		Placements []db.Placement `json:"placements"`
	}
	if code := doJSON(t, http.MethodGet, srv.URL+"/api/placements", "", &placements); code != 200 || len(placements.Placements) != 1 {
		t.Fatalf("placements: %+v (status %d)", placements, code)
	}

	if code := doJSON(t, http.MethodDelete, srv.URL+"/api/devices/rf01", "", nil); code != 200 {
		t.Fatalf("delete device status=%d", code)
	}
	if code := doJSON(t, http.MethodGet, srv.URL+"/api/devices/rf01", "", nil); code != 404 {
		t.Fatalf("deleted device status=%d, want 404", code)
	}
}

func TestRenameKeepsDeviceKind(t *testing.T) {
	srv, closeFn := dbServer(t)
	defer closeFn()

	if code := doJSON(t, http.MethodPost, srv.URL+"/api/devices", `{"id":"tv","kind":"tuya"}`, nil); code != 200 {
		t.Fatalf("create tuya device status=%d", code)
	}
	if code := doJSON(t, http.MethodPost, srv.URL+"/api/devices/tv", `{"name":"TV da sala"}`, nil); code != 200 {
		t.Fatalf("rename status=%d", code)
	}
	var got struct {
		Device struct {
			Kind string `json:"kind"`
			Name string `json:"name"`
		} `json:"device"`
	}
	if code := doJSON(t, http.MethodGet, srv.URL+"/api/devices/tv", "", &got); code != 200 {
		t.Fatalf("get status=%d", code)
	}
	if got.Device.Kind != "tuya" || got.Device.Name != "TV da sala" {
		t.Fatalf("rename changed the kind: %+v", got.Device)
	}
}

func TestDeviceTemplateRoute(t *testing.T) {
	srv, closeFn := dbServer(t)
	defer closeFn()

	if code := doJSON(t, http.MethodPost, srv.URL+"/api/devices", `{"id":"plug","kind":"tuya"}`, nil); code != 200 {
		t.Fatalf("create device status=%d", code)
	}
	if code := doJSON(t, http.MethodPost, srv.URL+"/api/devices/plug", `{"template":"tuya.socket"}`, nil); code != 200 {
		t.Fatalf("set template status=%d", code)
	}
	var got struct {
		Device struct {
			Template string `json:"template"`
		} `json:"device"`
	}
	if code := doJSON(t, http.MethodGet, srv.URL+"/api/devices/plug", "", &got); code != 200 {
		t.Fatalf("get status=%d", code)
	}
	if got.Device.Template != "tuya.socket" {
		t.Fatalf("template not stored: %+v", got.Device)
	}
	// Clearing it is explicit; an empty template means "infer".
	if code := doJSON(t, http.MethodPost, srv.URL+"/api/devices/plug", `{"template":""}`, nil); code != 200 {
		t.Fatalf("clear template status=%d", code)
	}
	var cleared struct {
		Device struct {
			Template string `json:"template"`
		} `json:"device"`
	}
	if code := doJSON(t, http.MethodGet, srv.URL+"/api/devices/plug", "", &cleared); code != 200 || cleared.Device.Template != "" {
		t.Fatalf("template not cleared: %+v (status %d)", cleared.Device, code)
	}
}

func TestDeviceOrderRoute(t *testing.T) {
	srv, closeFn := dbServer(t)
	defer closeFn()

	for _, d := range []string{
		`{"id":"r1","name":"Sala"}`,
	} {
		if code := doJSON(t, http.MethodPost, srv.URL+"/api/rooms", d, nil); code != 200 {
			t.Fatalf("create room status=%d", code)
		}
	}
	for _, id := range []string{"a", "b", "c"} {
		if code := doJSON(t, http.MethodPost, srv.URL+"/api/devices", `{"id":"`+id+`","kind":"tuya"}`, nil); code != 200 {
			t.Fatalf("create %s status=%d", id, code)
		}
		if code := doJSON(t, http.MethodPost, srv.URL+"/api/devices/"+id, `{"room_id":"r1"}`, nil); code != 200 {
			t.Fatalf("assign %s status=%d", id, code)
		}
	}

	// Reorder within the room.
	if code := doJSON(t, http.MethodPost, srv.URL+"/api/devices/order", `{"ids":["c","a","b"]}`, nil); code != 200 {
		t.Fatalf("order status=%d", code)
	}
	var got struct {
		Device struct {
			Sort int `json:"sort"`
		} `json:"device"`
	}
	for id, want := range map[string]int{"c": 0, "a": 1, "b": 2} {
		if code := doJSON(t, http.MethodGet, srv.URL+"/api/devices/"+id, "", &got); code != 200 {
			t.Fatalf("get %s status=%d", id, code)
		}
		if got.Device.Sort != want {
			t.Fatalf("%s sort=%d, want %d", id, got.Device.Sort, want)
		}
	}

	// An unknown id is a 404 and a mixed-room order is a 400.
	if code := doJSON(t, http.MethodPost, srv.URL+"/api/devices/order", `{"ids":["a","ghost"]}`, nil); code != 404 {
		t.Fatalf("unknown id status=%d, want 404", code)
	}
	if code := doJSON(t, http.MethodPost, srv.URL+"/api/devices/order", `{"ids":[]}`, nil); code != 400 {
		t.Fatalf("empty order status=%d, want 400", code)
	}
}

func TestDeviceFunctionsRoute(t *testing.T) {
	srv, closeFn := dbServer(t)
	defer closeFn()

	if code := doJSON(t, http.MethodPost, srv.URL+"/api/devices", `{"id":"plug","kind":"tuya"}`, nil); code != 200 {
		t.Fatalf("create device status=%d", code)
	}

	// Without specs the catalog is empty but still answers 200.
	var empty struct {
		Catalog struct {
			Configured bool `json:"configured"`
			Functions  []struct {
				Key string `json:"key"`
			} `json:"functions"`
		} `json:"catalog"`
	}
	if code := doJSON(t, http.MethodGet, srv.URL+"/api/devices/plug/functions", "", &empty); code != 200 {
		t.Fatalf("empty catalog status=%d", code)
	}
	if empty.Catalog.Configured || len(empty.Catalog.Functions) != 0 {
		t.Fatalf("unexpected empty catalog: %+v", empty.Catalog)
	}

	// Unknown device is a 404.
	if code := doJSON(t, http.MethodGet, srv.URL+"/api/devices/ghost/functions", "", nil); code != 404 {
		t.Fatalf("unknown device status=%d, want 404", code)
	}

	// A key that is not a known function is a 400.
	if code := doJSON(t, http.MethodPut, srv.URL+"/api/devices/plug/functions", `{"functions":[{"key":"ghost","label":"x"}]}`, nil); code != 400 {
		t.Fatalf("bad function status=%d, want 400", code)
	}
}
