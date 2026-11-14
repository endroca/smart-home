package httpapi

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"smart_home/hub/internal/config"
	"smart_home/hub/internal/db"
)

func automationTestServer(t *testing.T) *httptest.Server {
	t.Helper()
	store, err := db.Open(filepath.Join(t.TempDir(), "hub.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	srv := httptest.NewServer(New(nil, config.Config{}, store).Handler())
	t.Cleanup(srv.Close)
	return srv
}

func postJSON(t *testing.T, url string, body string) *http.Response {
	t.Helper()
	resp, err := http.Post(url, "application/json", bytes.NewBufferString(body))
	if err != nil {
		t.Fatal(err)
	}
	return resp
}

// TestAutomationScheduleAndDPSRoundTrip checks the friendly editor's wire
// contract: a grouped source (dps) and a time window survive POST and GET.
func TestAutomationScheduleAndDPSRoundTrip(t *testing.T) {
	srv := automationTestServer(t)

	resp := postJSON(t, srv.URL+"/api/automations", `{
		"name": "Porta à noite",
		"enabled": true,
		"source": {"type": "tuya_dp", "device": "lock1", "dps": ["1", "2"]},
		"schedule": {"start": "18:00", "end": "03:00", "days": [5]},
		"delay": {"enabled": true, "ms": 1500},
		"actions": [{"kind": "tuya", "device": "lamp1", "dp": "1", "value": true}]
	}`)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("POST status = %d", resp.StatusCode)
	}

	var created struct {
		Automation struct {
			ID     string `json:"id"`
			Source struct {
				DPs []string `json:"dps"`
			} `json:"source"`
			Schedule struct {
				Start string `json:"start"`
				End   string `json:"end"`
				Days  []int  `json:"days"`
			} `json:"schedule"`
			Delay struct {
				Enabled bool `json:"enabled"`
				Ms      int  `json:"ms"`
			} `json:"delay"`
		} `json:"automation"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&created); err != nil {
		t.Fatal(err)
	}
	if len(created.Automation.Source.DPs) != 2 {
		t.Fatalf("dps = %v", created.Automation.Source.DPs)
	}
	if created.Automation.Schedule.Start != "18:00" || created.Automation.Schedule.End != "03:00" {
		t.Fatalf("schedule = %+v", created.Automation.Schedule)
	}
	if !created.Automation.Delay.Enabled || created.Automation.Delay.Ms != 1500 {
		t.Fatalf("delay = %+v", created.Automation.Delay)
	}

	listResp, err := http.Get(srv.URL + "/api/automations")
	if err != nil {
		t.Fatal(err)
	}
	defer listResp.Body.Close()
	var list struct {
		Automations []struct {
			ID       string `json:"id"`
			Schedule struct {
				Days []int `json:"days"`
			} `json:"schedule"`
		} `json:"automations"`
	}
	if err := json.NewDecoder(listResp.Body).Decode(&list); err != nil {
		t.Fatal(err)
	}
	if len(list.Automations) != 1 || len(list.Automations[0].Schedule.Days) != 1 || list.Automations[0].Schedule.Days[0] != 5 {
		t.Fatalf("list = %+v", list.Automations)
	}
}

func TestAutomationRejectsInvalidSchedule(t *testing.T) {
	srv := automationTestServer(t)
	resp := postJSON(t, srv.URL+"/api/automations", `{
		"name": "Inválida",
		"enabled": true,
		"source": {"type": "manual"},
		"schedule": {"start": "25:00", "end": "03:00"},
		"actions": [{"kind": "tuya", "device": "lamp1", "dp": "1", "value": true}]
	}`)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", resp.StatusCode)
	}
}

// TestAutomationTimeVolatile checks a volatile timer: a relative delay is
// resolved to an absolute fire_at, the once flag survives, and the API rejects
// a scheduled automation without an instant.
func TestAutomationTimeVolatile(t *testing.T) {
	srv := automationTestServer(t)

	resp := postJSON(t, srv.URL+"/api/automations", `{
		"name": "Desliga em 10 min",
		"enabled": true,
		"once": true,
		"source": {"type": "time", "in_seconds": 600},
		"actions": [{"kind": "tuya", "device": "lamp1", "dp": "1", "value": false}]
	}`)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("POST status = %d", resp.StatusCode)
	}
	var created struct {
		Automation struct {
			Once   bool `json:"once"`
			Source struct {
				FireAt string `json:"fire_at"`
				InSecs int    `json:"in_seconds"`
			} `json:"source"`
		} `json:"automation"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&created); err != nil {
		t.Fatal(err)
	}
	if !created.Automation.Once {
		t.Fatal("once flag was not echoed back")
	}
	if created.Automation.Source.FireAt == "" {
		t.Fatal("fire_at was not resolved in the response")
	}
	if created.Automation.Source.InSecs != 0 {
		t.Fatalf("in_seconds should be cleared, got %d", created.Automation.Source.InSecs)
	}

	bad := postJSON(t, srv.URL+"/api/automations", `{
		"name": "Sem instante",
		"enabled": true,
		"source": {"type": "time"},
		"actions": [{"kind": "tuya", "device": "lamp1", "dp": "1", "value": false}]
	}`)
	defer bad.Body.Close()
	if bad.StatusCode != http.StatusBadRequest {
		t.Fatalf("invalid time source status = %d, want 400", bad.StatusCode)
	}
}
