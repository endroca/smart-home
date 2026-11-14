package tuya

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

const (
	testAccessID     = "testaccessid123"
	testAccessSecret = "testsecret456"
)

// verifySign recomputes the Tuya signature from the received request and
// compares it with the sign header, so the test does not simply mirror the
// implementation under test.
func verifySign(t *testing.T, r *http.Request, body []byte) {
	t.Helper()
	path := r.URL.Path
	if r.URL.RawQuery != "" {
		path += "?" + r.URL.RawQuery
	}
	sum := sha256.Sum256(body)
	var sb strings.Builder
	sb.WriteString(testAccessID)
	if tok := r.Header.Get("access_token"); tok != "" {
		sb.WriteString(tok)
	}
	sb.WriteString(r.Header.Get("t"))
	sb.WriteString(r.Method)
	sb.WriteByte('\n')
	sb.WriteString(hex.EncodeToString(sum[:]))
	sb.WriteByte('\n')
	if len(body) > 0 {
		sb.WriteString("Content-type:application/json\n")
	}
	sb.WriteByte('\n')
	sb.WriteString(path)

	mac := hmac.New(sha256.New, []byte(testAccessSecret))
	mac.Write([]byte(sb.String()))
	want := strings.ToUpper(hex.EncodeToString(mac.Sum(nil)))
	if got := r.Header.Get("sign"); got != want {
		t.Fatalf("bad signature for %s %s\n got %s\nwant %s", r.Method, path, got, want)
	}
	if r.Header.Get("client_id") != testAccessID {
		t.Fatalf("bad client_id header: %q", r.Header.Get("client_id"))
	}
	if r.Header.Get("sign_method") != "HMAC-SHA256" {
		t.Fatalf("bad sign_method header: %q", r.Header.Get("sign_method"))
	}
}

// TestCloudSignVectors locks the signer against payloads captured from the
// reference implementation (tinytuya), so a regression fails loudly.
func TestCloudSignVectors(t *testing.T) {
	c := &Cloud{cfg: CloudConfig{AccessID: "testid", AccessSecret: "testsecret"}}

	// GET /v1.0/token?grant_type=1 (no body, no token).
	if got, want := c.sign("GET", "/v1.0/token?grant_type=1", nil, "", 1700000000000),
		"18A53EA2AB6725F8B19D5A658B2453789B9DF916B9C4E6BA4D64B6C5A5564653"; got != want {
		t.Fatalf("GET sign\n got %s\nwant %s", got, want)
	}

	// POST with an access token and a JSON body.
	body := []byte(`{"commands": [{"code": "switch_1", "value": true}]}`)
	if got, want := c.sign("POST", "/v1.0/iot-03/devices/dev123/commands", body, "x", 1700000000000),
		"E494F0B1DC6C3C897530EA87C6D4138ED415B50809780EC226EA5C8E800C77F6"; got != want {
		t.Fatalf("POST sign\n got %s\nwant %s", got, want)
	}
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(v)
}

func newCloudServer(t *testing.T) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()

	mux.HandleFunc("/v1.0/token", func(w http.ResponseWriter, r *http.Request) {
		verifySign(t, r, nil)
		if r.URL.Query().Get("grant_type") != "1" {
			t.Fatalf("missing grant_type")
		}
		writeJSON(w, map[string]any{
			"success": true, "t": 1700000000000,
			"result": map[string]any{"access_token": "tok-1", "expire_time": 3600},
		})
	})

	mux.HandleFunc("/v1.0/devices/seed", func(w http.ResponseWriter, r *http.Request) {
		verifySign(t, r, nil)
		if r.Header.Get("access_token") != "tok-1" {
			t.Fatalf("missing access_token")
		}
		writeJSON(w, map[string]any{"success": true, "result": map[string]any{"id": "seed", "uid": "uid-1"}})
	})

	mux.HandleFunc("/v1.0/users/uid-1/devices", func(w http.ResponseWriter, r *http.Request) {
		verifySign(t, r, nil)
		writeJSON(w, map[string]any{
			"success": true,
			"result": []any{
				map[string]any{"id": "seed", "name": "Sala", "local_key": "0123456789abcdef", "uid": "uid-1", "product_id": "pk1", "ip": "192.168.1.50", "online": true},
				map[string]any{"id": "dev2", "name": "Quarto", "local_key": "fedcba9876543210", "uid": "uid-1", "product_id": "pk1"},
			},
		})
	})

	mux.HandleFunc("/v1.1/devices/seed/specifications", func(w http.ResponseWriter, r *http.Request) {
		verifySign(t, r, nil)
		writeJSON(w, map[string]any{
			"success": true,
			"result": map[string]any{
				"category": "cz",
				"functions": []any{
					map[string]any{"code": "switch_1", "type": "Boolean", "values": "{}", "dp_id": 1, "name": "Switch 1", "desc": "Liga/desliga"},
				},
				"status": []any{
					map[string]any{"code": "countdown_1", "type": "Integer", "values": `{"unit":"s","min":0,"max":86400,"scale":0,"step":1}`, "name": "Timer"},
				},
			},
		})
	})

	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv
}

func newTestCloud(t *testing.T, srv *httptest.Server) *Cloud {
	t.Helper()
	c, err := NewCloud(CloudConfig{
		AccessID:     testAccessID,
		AccessSecret: testAccessSecret,
		Region:       "us",
		BaseURL:      srv.URL,
	})
	if err != nil {
		t.Fatal(err)
	}
	return c
}

func TestCloudDevices(t *testing.T) {
	srv := newCloudServer(t)
	c := newTestCloud(t, srv)

	devices, err := c.Devices(context.Background(), "seed")
	if err != nil {
		t.Fatal(err)
	}
	if len(devices) != 2 {
		t.Fatalf("got %d devices, want 2", len(devices))
	}
	if devices[0].ID != "seed" || devices[0].LocalKey != "0123456789abcdef" {
		t.Fatalf("unexpected first device: %+v", devices[0])
	}
	if !devices[0].Online {
		t.Fatalf("expected seed to be online: %+v", devices[0])
	}
	if devices[1].Name != "Quarto" || devices[1].LocalKey != "fedcba9876543210" {
		t.Fatalf("unexpected second device: %+v", devices[1])
	}
}

func TestCloudDevice(t *testing.T) {
	srv := newCloudServer(t)
	c := newTestCloud(t, srv)

	d, err := c.Device(context.Background(), "seed")
	if err != nil {
		t.Fatal(err)
	}
	if d.UID != "uid-1" {
		t.Fatalf("uid = %q", d.UID)
	}
}

func TestCloudSpecifications(t *testing.T) {
	srv := newCloudServer(t)
	c := newTestCloud(t, srv)

	specs, err := c.Specifications(context.Background(), "seed")
	if err != nil {
		t.Fatal(err)
	}
	if len(specs) != 2 {
		t.Fatalf("got %d specs, want 2", len(specs))
	}
	if specs[0].DPID != "1" || specs[0].Code != "switch_1" || specs[0].Type != "Boolean" {
		t.Fatalf("unexpected function spec: %+v", specs[0])
	}
	if specs[0].Name != "Switch 1" || specs[0].Desc != "Liga/desliga" {
		t.Fatalf("function name/desc not parsed: %+v", specs[0])
	}
	if specs[1].DPID != "countdown_1" || specs[1].Type != "Integer" {
		t.Fatalf("unexpected status spec: %+v", specs[1])
	}
	if specs[1].Name != "Timer" {
		t.Fatalf("status name not parsed: %+v", specs[1])
	}
	values, ok := specs[1].Values.(map[string]any)
	if !ok || values["unit"] != "s" {
		t.Fatalf("values not decoded: %#v", specs[1].Values)
	}
}

func TestCloudTokenRefreshOnInvalid(t *testing.T) {
	var tokenCalls, listCalls int
	mux := http.NewServeMux()
	mux.HandleFunc("/v1.0/token", func(w http.ResponseWriter, r *http.Request) {
		tokenCalls++
		writeJSON(w, map[string]any{"success": true, "result": map[string]any{"access_token": fmt.Sprintf("tok-%d", tokenCalls), "expire_time": 3600}})
	})
	mux.HandleFunc("/v1.0/devices/seed", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, map[string]any{"success": true, "result": map[string]any{"uid": "uid-1"}})
	})
	mux.HandleFunc("/v1.0/users/uid-1/devices", func(w http.ResponseWriter, r *http.Request) {
		listCalls++
		if r.Header.Get("access_token") == "tok-1" {
			writeJSON(w, map[string]any{"success": false, "code": 1010, "msg": "token invalid"})
			return
		}
		writeJSON(w, map[string]any{"success": true, "result": []any{map[string]any{"id": "seed", "local_key": "0123456789abcdef"}}})
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	c := newTestCloud(t, srv)
	devices, err := c.Devices(context.Background(), "seed")
	if err != nil {
		t.Fatal(err)
	}
	if len(devices) != 1 {
		t.Fatalf("got %d devices", len(devices))
	}
	if tokenCalls != 2 {
		t.Fatalf("token requested %d times, want 2", tokenCalls)
	}
	if listCalls != 2 {
		t.Fatalf("device list requested %d times, want 2", listCalls)
	}
}

func TestCloudEnergyTrend(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/v1.0/token", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, map[string]any{"success": true, "result": map[string]any{"access_token": "tok-1", "expire_time": 3600}})
	})
	mux.HandleFunc("/v1.0/iot-03/energy/electricity/devices/statistics-trend", func(w http.ResponseWriter, r *http.Request) {
		verifySign(t, r, nil)
		q := r.URL.Query()
		if q.Get("deviceId") != "dev1" {
			t.Fatalf("deviceId = %q", q.Get("deviceId"))
		}
		if q.Get("energyAction") != "consume" {
			t.Fatalf("energyAction = %q", q.Get("energyAction"))
		}
		if q.Get("statisticsType") != "day" {
			t.Fatalf("statisticsType = %q", q.Get("statisticsType"))
		}
		if q.Get("startTime") != "20261105" || q.Get("endTime") != "20261111" {
			t.Fatalf("window = %q..%q", q.Get("startTime"), q.Get("endTime"))
		}
		writeJSON(w, map[string]any{
			"success": true,
			"result": []any{
				map[string]any{"time": "20261105", "value": 1.25},
				// The cloud sometimes serializes the value as a string.
				map[string]any{"time": "20261106", "value": "0.5"},
			},
		})
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	c := newTestCloud(t, srv)
	points, err := c.EnergyTrend(context.Background(), "dev1", "day", "20261105", "20261111")
	if err != nil {
		t.Fatal(err)
	}
	if len(points) != 2 {
		t.Fatalf("got %d points, want 2", len(points))
	}
	if points[0].Time != "20261105" || points[0].Value != 1.25 {
		t.Fatalf("unexpected first point: %+v", points[0])
	}
	if points[1].Value != 0.5 {
		t.Fatalf("string value not parsed: %+v", points[1])
	}
}

func TestCloudEnergyTrendNotSubscribed(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/v1.0/token", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, map[string]any{"success": true, "result": map[string]any{"access_token": "tok-1", "expire_time": 3600}})
	})
	mux.HandleFunc("/v1.0/iot-03/energy/electricity/devices/statistics-trend", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, map[string]any{
			"success": false, "code": 28841101,
			"msg": "No permissions. This API is not subscribed.",
		})
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	c := newTestCloud(t, srv)
	_, err := c.EnergyTrend(context.Background(), "dev1", "day", "20261105", "20261111")
	if !errors.Is(err, ErrNotSubscribed) {
		t.Fatalf("expected ErrNotSubscribed, got %v", err)
	}
}

func TestCloudEnergyLogs(t *testing.T) {
	var pageCalls int
	mux := http.NewServeMux()
	mux.HandleFunc("/v1.0/token", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, map[string]any{"success": true, "result": map[string]any{"access_token": "tok-1", "expire_time": 3600}})
	})
	mux.HandleFunc("/v1.0/iot-03/devices/dev1/report-logs", func(w http.ResponseWriter, r *http.Request) {
		verifySign(t, r, nil)
		q := r.URL.Query()
		if q.Get("codes") != "add_ele,cur_power" {
			t.Fatalf("codes = %q", q.Get("codes"))
		}
		// The cloud signs the query with a literal comma and rejects %2C
		// with "sign invalid", so the raw query must not escape it.
		if strings.Contains(r.URL.RawQuery, "%2C") || !strings.Contains(r.URL.RawQuery, "codes=add_ele,cur_power") {
			t.Fatalf("codes escaped in raw query: %s", r.URL.RawQuery)
		}
		if q.Get("start_time") != "1700000000000" || q.Get("end_time") != "1700086400000" {
			t.Fatalf("window = %q..%q", q.Get("start_time"), q.Get("end_time"))
		}
		if q.Get("size") != "100" {
			t.Fatalf("size = %q", q.Get("size"))
		}
		pageCalls++
		if q.Get("last_row_key") == "" {
			writeJSON(w, map[string]any{
				"success": true,
				"result": map[string]any{
					"has_more":     true,
					"last_row_key": "next-page",
					"list": []any{
						map[string]any{"code": "add_ele", "event_time": 1700086300000, "value": "71"},
					},
				},
			})
			return
		}
		if q.Get("last_row_key") != "next-page" {
			t.Fatalf("last_row_key = %q", q.Get("last_row_key"))
		}
		writeJSON(w, map[string]any{
			"success": true,
			"result": map[string]any{
				"has_more": false,
				"list": []any{
					map[string]any{"code": "add_ele", "event_time": 1700080000000, "value": 83.0},
				},
			},
		})
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	c := newTestCloud(t, srv)
	logs, err := c.EnergyLogs(context.Background(), "dev1", []string{"add_ele", "cur_power"}, 1700000000000, 1700086400000)
	if err != nil {
		t.Fatal(err)
	}
	if pageCalls != 2 {
		t.Fatalf("pages = %d, want 2", pageCalls)
	}
	if len(logs) != 2 {
		t.Fatalf("got %d logs, want 2", len(logs))
	}
	if logs[0].Code != "add_ele" || logs[0].EventTime != 1700086300000 || logs[0].Value != 71 {
		t.Fatalf("unexpected string-valued log: %+v", logs[0])
	}
	if logs[1].EventTime != 1700080000000 || logs[1].Value != 83 {
		t.Fatalf("unexpected numeric log: %+v", logs[1])
	}
}

// TestCloudReportLogs checks the report-log reader keeps the raw value, which
// for a lock is a string ("11") rather than a number.
func TestCloudReportLogs(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/v1.0/token", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, map[string]any{"success": true, "result": map[string]any{"access_token": "tok-1", "expire_time": 3600}})
	})
	mux.HandleFunc("/v1.0/iot-03/devices/lock1/report-logs", func(w http.ResponseWriter, r *http.Request) {
		verifySign(t, r, nil)
		if r.URL.Query().Get("size") != "50" {
			t.Fatalf("size = %q", r.URL.Query().Get("size"))
		}
		writeJSON(w, map[string]any{
			"success": true,
			"result": map[string]any{
				"has_more": false,
				"list": []any{
					map[string]any{"code": "unlock_fingerprint", "event_time": 1700086300000, "value": "11"},
					map[string]any{"code": "reverse_lock", "event_time": 1700086000000, "value": "false"},
				},
			},
		})
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	c := newTestCloud(t, srv)
	logs, err := c.ReportLogs(context.Background(), "lock1", []string{"unlock_fingerprint", "reverse_lock"}, 1, 2, 50)
	if err != nil {
		t.Fatal(err)
	}
	if len(logs) != 2 {
		t.Fatalf("got %d logs, want 2", len(logs))
	}
	if logs[0].Code != "unlock_fingerprint" || logs[0].EventTime != 1700086300000 || logs[0].Value != "11" {
		t.Fatalf("raw string value lost: %+v", logs[0])
	}
	if logs[1].Value != "false" {
		t.Fatalf("raw boolean string lost: %+v", logs[1])
	}
}

// TestCloudOpenLogs checks the Smart Lock open-logs reader: the path is
// /devices/{id}/door-lock/open-logs, and each record carries the method and the
// friendly unlock name.
func TestCloudOpenLogs(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/v1.0/token", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, map[string]any{"success": true, "result": map[string]any{"access_token": "tok-1", "expire_time": 3600}})
	})
	mux.HandleFunc("/v1.0/devices/lock1/door-lock/open-logs", func(w http.ResponseWriter, r *http.Request) {
		verifySign(t, r, nil)
		q := r.URL.Query()
		if q.Get("page_no") != "1" || q.Get("page_size") != "5" {
			t.Fatalf("page = %q/%q", q.Get("page_no"), q.Get("page_size"))
		}
		writeJSON(w, map[string]any{
			"success": true,
			"result": map[string]any{
				"total": 23,
				"logs": []any{
					map[string]any{
						"unlock_name": "Finger11",
						"nick_name":   "",
						"user_id":     "0",
						"update_time": 1700086300000,
						"status":      map[string]any{"code": "unlock_fingerprint", "value": "11"},
					},
				},
			},
		})
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	c := newTestCloud(t, srv)
	recs, total, err := c.OpenLogs(context.Background(), "lock1", 1, 2, 1, 5)
	if err != nil {
		t.Fatal(err)
	}
	if total != 23 || len(recs) != 1 {
		t.Fatalf("total=%d recs=%d", total, len(recs))
	}
	r0 := recs[0]
	if r0.Code != "unlock_fingerprint" || r0.Value != "11" || r0.UnlockName != "Finger11" || r0.At != 1700086300000 {
		t.Fatalf("unexpected record: %+v", r0)
	}
}

func TestCloudUnknownRegion(t *testing.T) {
	if _, err := NewCloud(CloudConfig{AccessID: "a", AccessSecret: "b", Region: "mars"}); err == nil {
		t.Fatal("expected an error for an unknown region")
	}
}

func TestCloudMissingCredentials(t *testing.T) {
	if _, err := NewCloud(CloudConfig{Region: "us"}); err == nil {
		t.Fatal("expected an error without credentials")
	}
}
