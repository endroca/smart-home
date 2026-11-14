package httpapi

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"smart_home/hub/internal/config"
	"smart_home/hub/internal/db"
	"smart_home/hub/internal/hub"
	"smart_home/hub/internal/tuya/service"
)

func tuyaServer(t *testing.T, withKey bool) (*httptest.Server, func()) {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "hub.db")
	dev := db.Device{ID: "dev1", Kind: db.KindTuya, Name: "sala", IP: "192.168.1.10", Version: "3.5"}
	if withKey {
		dev.LocalKey = "0123456789abcdef"
		dev.KeySource = "cloud"
	}
	store, err := db.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.UpsertDevice(context.Background(), dev); err != nil {
		t.Fatal(err)
	}

	svc, err := service.Open(service.Config{Store: store})
	if err != nil {
		t.Fatal(err)
	}
	cfg := config.Config{HTTPAddr: ":0"}
	h, err := hub.New(cfg, store)
	if err != nil {
		t.Fatal(err)
	}
	server := New(h, cfg, store)
	server.SetTuya(svc)
	srv := httptest.NewServer(server.Handler())
	return srv, func() {
		srv.Close()
		svc.Close()
		store.Close()
	}
}

func TestTuyaDevicesRoute(t *testing.T) {
	srv, closeFn := tuyaServer(t, true)
	defer closeFn()

	resp, err := http.Get(srv.URL + "/api/tuya/devices")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status=%d body=%s", resp.StatusCode, body)
	}
	if strings.Contains(string(body), "0123456789abcdef") {
		t.Fatalf("local key leaked in response: %s", body)
	}

	var out struct {
		OK      bool `json:"ok"`
		Cloud   bool `json:"cloud"`
		Devices []struct {
			ID     string `json:"id"`
			Name   string `json:"name"`
			HasKey bool   `json:"has_key"`
		} `json:"devices"`
	}
	if err := json.Unmarshal(body, &out); err != nil {
		t.Fatal(err)
	}
	if !out.OK || len(out.Devices) != 1 {
		t.Fatalf("unexpected response: %s", body)
	}
	if !out.Devices[0].HasKey || out.Devices[0].Name != "sala" {
		t.Fatalf("device not reported correctly: %+v", out.Devices[0])
	}
	if out.Cloud {
		t.Fatal("cloud should not be configured in the test")
	}
}

func TestTuyaListenRequiresKey(t *testing.T) {
	srv, closeFn := tuyaServer(t, false)
	defer closeFn()

	resp, err := http.Post(srv.URL+"/api/tuya/listen", "application/json",
		strings.NewReader(`{"id":"dev1","action":"start"}`))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("status=%d, want 400", resp.StatusCode)
	}
}

func TestTuyaEventsRoute(t *testing.T) {
	srv, closeFn := tuyaServer(t, true)
	defer closeFn()

	resp, err := http.Get(srv.URL + "/api/tuya/events?id=dev1")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status=%d", resp.StatusCode)
	}
}

func TestTuyaForgetRoute(t *testing.T) {
	srv, closeFn := tuyaServer(t, true)
	defer closeFn()

	resp, err := http.Post(srv.URL+"/api/tuya/forget", "application/json",
		strings.NewReader(`{"id":"dev1"}`))
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status=%d, want 200", resp.StatusCode)
	}

	list, err := http.Get(srv.URL + "/api/tuya/devices")
	if err != nil {
		t.Fatal(err)
	}
	defer list.Body.Close()
	body, _ := io.ReadAll(list.Body)
	if strings.Contains(string(body), "dev1") {
		t.Fatalf("device still present after forget: %s", body)
	}

	missing, err := http.Post(srv.URL+"/api/tuya/forget", "application/json",
		strings.NewReader(`{"id":"ghost"}`))
	if err != nil {
		t.Fatal(err)
	}
	defer missing.Body.Close()
	if missing.StatusCode != http.StatusNotFound {
		t.Fatalf("forget of unknown device status=%d, want 404", missing.StatusCode)
	}
}

func TestTuyaEnergyRoute(t *testing.T) {
	srv, closeFn := tuyaServer(t, true)
	defer closeFn()

	resp, err := http.Get(srv.URL + "/api/tuya/energy?id=dev1&range=7d")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status=%d body=%s", resp.StatusCode, body)
	}
	var out struct {
		OK        bool   `json:"ok"`
		Available bool   `json:"available"`
		Reason    string `json:"reason"`
		Range     string `json:"range"`
		Unit      string `json:"unit"`
		Points    []any  `json:"points"`
	}
	if err := json.Unmarshal(body, &out); err != nil {
		t.Fatal(err)
	}
	// The test service has no cloud credentials: the endpoint still answers 200
	// with an explanatory reason instead of failing the request.
	if !out.OK || out.Available || out.Reason != "cloud_not_configured" {
		t.Fatalf("unexpected response: %s", body)
	}
	if out.Range != "7d" || out.Unit != "kWh" {
		t.Fatalf("unexpected metadata: %+v", out)
	}

	missing, err := http.Get(srv.URL + "/api/tuya/energy")
	if err != nil {
		t.Fatal(err)
	}
	defer missing.Body.Close()
	if missing.StatusCode != http.StatusBadRequest {
		t.Fatalf("missing id status=%d, want 400", missing.StatusCode)
	}

	unknown, err := http.Get(srv.URL + "/api/tuya/energy?id=ghost")
	if err != nil {
		t.Fatal(err)
	}
	defer unknown.Body.Close()
	if unknown.StatusCode != http.StatusNotFound {
		t.Fatalf("unknown device status=%d, want 404", unknown.StatusCode)
	}
}

func TestTuyaRoutesAbsentWithoutService(t *testing.T) {
	srv, closeFn := testServer(t)
	defer closeFn()
	resp, err := http.Get(srv.URL + "/api/tuya/devices")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("status=%d, want 404 when Tuya is not configured", resp.StatusCode)
	}
}
