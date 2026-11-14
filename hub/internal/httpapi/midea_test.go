package httpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"smart_home/hub/internal/config"
	"smart_home/hub/internal/db"
	"smart_home/hub/internal/hub"
	mideasvc "smart_home/hub/internal/midea"
)

func mideaServer(t *testing.T) (*httptest.Server, func()) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "midea.db")
	store, err := db.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.UpsertDevice(context.Background(), db.Device{ID: "1720000001", Kind: db.KindMidea, Name: "sala", IP: "192.168.1.50", Port: 6444}); err != nil {
		t.Fatal(err)
	}
	if err := store.ReplaceMideaSpecs(context.Background(), "1720000001", []db.MideaSpec{
		{DeviceID: "1720000001", ID: "power", Name: "Energia", Type: "boolean", Writable: true},
	}); err != nil {
		t.Fatal(err)
	}

	svc, err := mideasvc.Open(mideasvc.Config{Store: store})
	if err != nil {
		t.Fatal(err)
	}
	cfg := config.Config{HTTPAddr: ":0"}
	h, err := hub.New(cfg, store)
	if err != nil {
		t.Fatal(err)
	}
	server := New(h, cfg, store)
	server.SetMidea(svc)
	srv := httptest.NewServer(server.Handler())
	return srv, func() {
		srv.Close()
		svc.Close()
		store.Close()
	}
}

func TestMideaDevicesListsStateWithoutDialing(t *testing.T) {
	srv, closeFn := mideaServer(t)
	defer closeFn()

	resp, err := http.Get(srv.URL + "/api/midea/devices")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var body struct {
		Devices []struct {
			ID      string `json:"id"`
			Online  bool   `json:"online"`
			HasKey  bool   `json:"has_key"`
			Actions []struct {
				ID string `json:"key"`
			} `json:"actions"`
		} `json:"devices"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatal(err)
	}
	if len(body.Devices) != 1 {
		t.Fatalf("got %d devices", len(body.Devices))
	}
	d := body.Devices[0]
	if d.ID != "1720000001" || d.Online || d.HasKey {
		t.Fatalf("unexpected view: %+v", d)
	}
	if len(d.Actions) != 1 || d.Actions[0].ID != "power" {
		t.Fatalf("capabilities not listed: %+v", d.Actions)
	}
}

func TestMideaSpecsGetIsCacheOnly(t *testing.T) {
	srv, closeFn := mideaServer(t)
	defer closeFn()

	resp, err := http.Get(srv.URL + "/api/midea/specs?id=1720000001")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d", resp.StatusCode)
	}
	var body struct {
		Actions []db.MideaSpec `json:"actions"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatal(err)
	}
	if len(body.Actions) != 1 {
		t.Fatalf("got %d cached specs", len(body.Actions))
	}
}

func TestMideaForgetRemovesState(t *testing.T) {
	srv, closeFn := mideaServer(t)
	defer closeFn()

	resp, err := http.Post(srv.URL+"/api/midea/forget", "application/json", strings.NewReader(`{"id":"1720000001"}`))
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("forget status = %d", resp.StatusCode)
	}

	resp, err = http.Get(srv.URL + "/api/midea/specs?id=1720000001")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var body struct {
		Actions []db.MideaSpec `json:"actions"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatal(err)
	}
	if len(body.Actions) != 0 {
		t.Fatalf("specs survived forget: %+v", body.Actions)
	}

	resp, err = http.Post(srv.URL+"/api/midea/forget", "application/json", strings.NewReader(`{"id":"missing"}`))
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("missing device status = %d", resp.StatusCode)
	}
}
