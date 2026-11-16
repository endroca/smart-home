package httpapi

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"
)

// TestOffEndpointTurnsFanOff exercises the generic "desligar tudo" against a
// gateway whose fan advertises a power capability: the hub must derive the off
// action and send exactly that command over the LAN.
func TestOffEndpointTurnsFanOff(t *testing.T) {
	var mu sync.Mutex
	var bodies []map[string]any
	gateway := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.URL.Path == "/api/manifest":
			io.WriteString(w, `{"schemaVersion":1,"manifestVersion":1,"thing":"t1","firmware":{"version":"1"},"devices":[{"id":"fan0","type":"ceiling_fan","name":"Sala","capabilities":{"power":{"kind":"switch","writable":true},"light":{"kind":"switch","writable":true}},"actions":[]}]}`)
		case r.URL.Path == "/api/state" && r.Method == http.MethodPost:
			var body map[string]any
			_ = json.NewDecoder(r.Body).Decode(&body)
			mu.Lock()
			bodies = append(bodies, body)
			mu.Unlock()
			io.WriteString(w, `{"ok":true,"state":{"power":false,"light":false,"speed":0,"direction":"forward"},"transmitted":["power"]}`)
		case r.URL.Path == "/api/state":
			io.WriteString(w, `{"ok":true,"fans":[{"id":"fan0","name":"Sala","state":{"power":false,"light":false,"speed":0,"direction":"forward"},"source":"http","updatedAt":1}]}`)
		default:
			http.NotFound(w, r)
		}
	}))
	defer gateway.Close()

	srv, closeFn := dbServer(t)
	defer closeFn()

	if code := doJSON(t, http.MethodPost, srv.URL+"/api/devices", `{"id":"t1","kind":"thing","url":"`+gateway.URL+`","token":"s3cr3t"}`, nil); code != 200 {
		t.Fatalf("register gateway status=%d", code)
	}

	// The probe discovers the fan asynchronously.
	deadline := time.Now().Add(5 * time.Second)
	for {
		var list struct {
			Devices []struct {
				ID string `json:"id"`
			} `json:"devices"`
		}
		_ = doJSON(t, http.MethodGet, srv.URL+"/api/devices", "", &list)
		found := false
		for _, d := range list.Devices {
			if d.ID == "t1/fan0" {
				found = true
				break
			}
		}
		if found {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("fan0 was not discovered")
		}
		time.Sleep(25 * time.Millisecond)
	}

	// The derived catalog exposes the fan functions, with power marked off.
	var cat struct {
		Catalog struct {
			Functions []struct {
				Key string `json:"key"`
				Off bool   `json:"off"`
			} `json:"functions"`
		} `json:"catalog"`
	}
	if code := doJSON(t, http.MethodGet, srv.URL+"/api/devices/t1/fan0/functions", "", &cat); code != 200 {
		t.Fatalf("functions status=%d", code)
	}
	off := ""
	for _, f := range cat.Catalog.Functions {
		if f.Off {
			off = f.Key
		}
	}
	if off != "power" {
		t.Fatalf("power should be the derived off function: %+v", cat.Catalog.Functions)
	}

	var resp struct {
		Results []struct {
			ID    string `json:"id"`
			OK    bool   `json:"ok"`
			Error string `json:"error"`
		} `json:"results"`
	}
	if code := doJSON(t, http.MethodPost, srv.URL+"/api/off", `{"ids":["t1/fan0"]}`, &resp); code != 200 {
		t.Fatalf("off status=%d", code)
	}
	if len(resp.Results) != 1 || !resp.Results[0].OK {
		t.Fatalf("unexpected off results: %+v", resp.Results)
	}

	mu.Lock()
	defer mu.Unlock()
	if len(bodies) == 0 {
		t.Fatal("the gateway never received a state command")
	}
	state, _ := bodies[len(bodies)-1]["state"].(map[string]any)
	if state["power"] != false {
		t.Fatalf("fan should have been turned off: %+v", bodies)
	}
}

// TestOffEndpointSkipsDeviceWithoutOffFunctions checks a device with nothing
// marked off is reported as skipped instead of failing.
func TestOffEndpointSkipsDeviceWithoutOffFunctions(t *testing.T) {
	srv, closeFn := dbServer(t)
	defer closeFn()

	if code := doJSON(t, http.MethodPost, srv.URL+"/api/devices", `{"id":"plug","kind":"tuya"}`, nil); code != 200 {
		t.Fatalf("create device status=%d", code)
	}
	var resp struct {
		Results []struct {
			ID      string `json:"id"`
			OK      bool   `json:"ok"`
			Skipped bool   `json:"skipped"`
		} `json:"results"`
	}
	if code := doJSON(t, http.MethodPost, srv.URL+"/api/off", `{"ids":["plug"]}`, &resp); code != 200 {
		t.Fatalf("off status=%d", code)
	}
	if len(resp.Results) != 1 || !resp.Results[0].Skipped {
		t.Fatalf("unexpected results: %+v", resp.Results)
	}
}
