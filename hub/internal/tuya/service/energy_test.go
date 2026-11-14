package service

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strconv"
	"sync"
	"testing"
	"time"

	"smart_home/hub/internal/db"
	"smart_home/hub/internal/tuya"
)

// energyCloud is a fake Power Management API that records the requested windows
// and returns a single non-zero bucket at the start of each one, so the test
// also exercises zero-filling and chunk merging.
func energyCloud(t *testing.T, calls *int, windows *[]string) *httptest.Server {
	t.Helper()
	var mu sync.Mutex
	mux := http.NewServeMux()
	writeJSON := func(w http.ResponseWriter, v any) { _ = json.NewEncoder(w).Encode(v) }
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1.0/token":
			writeJSON(w, map[string]any{"success": true, "result": map[string]any{"access_token": "tok", "expire_time": 3600}})
		case "/v1.0/iot-03/energy/electricity/devices/statistics-trend":
			q := r.URL.Query()
			statType, start, end := q.Get("statisticsType"), q.Get("startTime"), q.Get("endTime")
			mu.Lock()
			*calls++
			*windows = append(*windows, statType+" "+start+".."+end)
			mu.Unlock()
			writeJSON(w, map[string]any{
				"success": true,
				"result":  []any{map[string]any{"time": start, "value": 3.0}},
			})
		default:
			writeJSON(w, map[string]any{"success": false, "code": 404, "msg": "not found"})
		}
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv
}

func TestEnergyTrendSeries(t *testing.T) {
	path := filepath.Join(t.TempDir(), "hub.db")
	addDevice(t, path, db.Device{ID: "dev1", Kind: db.KindTuya, Name: "tomada"})

	calls := 0
	var windows []string
	cloud := energyCloud(t, &calls, &windows)
	svc := newService(t, path, Config{Cloud: tuya.CloudConfig{AccessID: "id", AccessSecret: "secret", Region: "us", BaseURL: cloud.URL}})
	fixed := time.Date(2026, 11, 11, 14, 30, 0, 0, time.Local)
	svc.now = func() time.Time { return fixed }

	ctx := context.Background()

	// Today: one bucket per hour from 00h through the current hour.
	today, err := svc.EnergyTrend(ctx, "dev1", "today")
	if err != nil {
		t.Fatal(err)
	}
	if !today.Available || today.Range != "today" || today.Unit != "kWh" {
		t.Fatalf("unexpected series metadata: %+v", today)
	}
	if len(today.Points) != 15 {
		t.Fatalf("today has %d points, want 15", len(today.Points))
	}
	if today.Points[0].Label != "00h" || today.Points[14].Label != "14h" {
		t.Fatalf("unexpected labels: %q..%q", today.Points[0].Label, today.Points[14].Label)
	}
	total := today.Total
	if total != 3 {
		t.Fatalf("total = %v, want 3", total)
	}
	// Missing buckets are zero-filled.
	if today.Points[1].Value != 0 {
		t.Fatalf("gap not zero-filled: %+v", today.Points[1])
	}

	// The same range is served from the cache.
	if _, err := svc.EnergyTrend(ctx, "dev1", "today"); err != nil {
		t.Fatal(err)
	}
	if calls != 1 {
		t.Fatalf("today range hit the cloud %d times, want 1", calls)
	}

	// Seven days: a single day query with seven points.
	week, err := svc.EnergyTrend(ctx, "dev1", "7d")
	if err != nil {
		t.Fatal(err)
	}
	if len(week.Points) != 7 || week.Total != 3 {
		t.Fatalf("unexpected 7d series: total=%v points=%d", week.Total, len(week.Points))
	}
	if week.Points[6].Label != "Hoje" || week.Points[5].Label != "Ontem" {
		t.Fatalf("unexpected 7d labels: %q %q", week.Points[5].Label, week.Points[6].Label)
	}

	// Thirty days: five windows of at most seven days, merged into 30 points.
	month, err := svc.EnergyTrend(ctx, "dev1", "30d")
	if err != nil {
		t.Fatal(err)
	}
	if len(month.Points) != 30 {
		t.Fatalf("30d has %d points, want 30", len(month.Points))
	}
	if month.Total != 15 {
		t.Fatalf("30d total = %v, want 15", month.Total)
	}
	if calls != 7 { // 1 today + 1 week + 5 month windows
		t.Fatalf("cloud calls = %d, want 7: %v", calls, windows)
	}
	if windows[2] != "day 20261013..20261019" {
		t.Fatalf("first 30d window = %q", windows[2])
	}
	if windows[6] != "day 20261110..20261111" {
		t.Fatalf("last 30d window = %q", windows[6])
	}

	// An unknown range falls back to today and is cached separately.
	fallback, err := svc.EnergyTrend(ctx, "dev1", "bogus")
	if err != nil {
		t.Fatal(err)
	}
	if fallback.Range != "today" || len(fallback.Points) != 15 {
		t.Fatalf("unexpected fallback series: %+v", fallback)
	}
}

func TestEnergyFromReportLogs(t *testing.T) {
	path := filepath.Join(t.TempDir(), "hub.db")
	addDevice(t, path, db.Device{ID: "dev1", Kind: db.KindTuya, Name: "tomada"})

	fixed := time.Date(2026, 11, 11, 14, 30, 0, 0, time.Local)
	at := func(day, hour, minute int) int64 {
		return time.Date(2026, 11, day, hour, minute, 0, 0, time.Local).UnixMilli()
	}
	records := []map[string]any{
		{"code": "add_ele", "event_time": at(10, 23, 30), "value": "500"},
		{"code": "add_ele", "event_time": at(11, 0, 10), "value": "100"},
		{"code": "add_ele", "event_time": at(11, 0, 50), "value": 20.0},
		{"code": "add_ele", "event_time": at(11, 13, 20), "value": "300"},
		{"code": "add_ele", "event_time": at(11, 13, 59), "value": "5"},
	}

	var logCalls, trendCalls int
	mux := http.NewServeMux()
	writeJSON := func(w http.ResponseWriter, v any) { _ = json.NewEncoder(w).Encode(v) }
	mux.HandleFunc("/v1.0/token", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, map[string]any{"success": true, "result": map[string]any{"access_token": "tok", "expire_time": 3600}})
	})
	mux.HandleFunc("/v1.0/iot-03/devices/dev1/report-logs", func(w http.ResponseWriter, r *http.Request) {
		logCalls++
		q := r.URL.Query()
		if q.Get("codes") != "add_ele" {
			t.Fatalf("codes = %q", q.Get("codes"))
		}
		start, _ := strconv.ParseInt(q.Get("start_time"), 10, 64)
		end, _ := strconv.ParseInt(q.Get("end_time"), 10, 64)
		list := []any{}
		for _, rec := range records {
			ts := rec["event_time"].(int64)
			if ts >= start && ts <= end {
				list = append(list, rec)
			}
		}
		writeJSON(w, map[string]any{"success": true, "result": map[string]any{"has_more": false, "list": list}})
	})
	mux.HandleFunc("/v1.0/iot-03/energy/electricity/devices/statistics-trend", func(w http.ResponseWriter, r *http.Request) {
		trendCalls++
		q := r.URL.Query()
		writeJSON(w, map[string]any{"success": true, "result": []any{map[string]any{"time": q.Get("startTime"), "value": 9.0}}})
	})
	cloud := httptest.NewServer(mux)
	defer cloud.Close()

	svc := newService(t, path, Config{Cloud: tuya.CloudConfig{AccessID: "id", AccessSecret: "secret", Region: "us", BaseURL: cloud.URL}})
	svc.now = func() time.Time { return fixed }
	ctx := context.Background()
	// add_ele scale 3: raw values are thousandths of a kWh.
	if err := svc.store.ReplaceSpecs(ctx, "dev1", []db.Spec{{
		DeviceID: "dev1", Code: "add_ele", Type: "Integer",
		Values: json.RawMessage(`{"unit":"kwh","scale":3}`),
	}}); err != nil {
		t.Fatal(err)
	}

	today, err := svc.EnergyTrend(ctx, "dev1", "today")
	if err != nil {
		t.Fatal(err)
	}
	if !today.Available || len(today.Points) != 15 {
		t.Fatalf("unexpected today series: %+v", today)
	}
	if today.Total != 0.425 {
		t.Fatalf("today total = %v, want 0.425", today.Total)
	}
	if today.Points[0].Value != 0.12 {
		t.Fatalf("hour 0 = %v, want 0.12", today.Points[0].Value)
	}
	if today.Points[13].Value != 0.305 {
		t.Fatalf("hour 13 = %v, want 0.305", today.Points[13].Value)
	}
	if today.Points[1].Value != 0 {
		t.Fatalf("empty hour not zero-filled: %+v", today.Points[1])
	}
	if trendCalls != 0 {
		t.Fatalf("trend called %d times despite logs", trendCalls)
	}

	// Cached per range.
	if _, err := svc.EnergyTrend(ctx, "dev1", "today"); err != nil {
		t.Fatal(err)
	}
	if logCalls != 1 {
		t.Fatalf("logs fetched %d times, want 1", logCalls)
	}

	week, err := svc.EnergyTrend(ctx, "dev1", "7d")
	if err != nil {
		t.Fatal(err)
	}
	if week.Total != 0.925 || len(week.Points) != 7 {
		t.Fatalf("unexpected 7d series: total=%v points=%d", week.Total, len(week.Points))
	}
	if week.Points[6].Label != "Hoje" || week.Points[6].Value != 0.425 {
		t.Fatalf("unexpected 7d tail: %+v", week.Points[6])
	}

	month, err := svc.EnergyTrend(ctx, "dev1", "30d")
	if err != nil {
		t.Fatal(err)
	}
	if month.Total != 0.925 || len(month.Points) != 30 {
		t.Fatalf("unexpected 30d series: total=%v points=%d", month.Total, len(month.Points))
	}
	if trendCalls != 0 {
		t.Fatalf("trend called %d times, want 0", trendCalls)
	}
}

func TestEnergyFallsBackToTrendWhenLogsEmpty(t *testing.T) {
	path := filepath.Join(t.TempDir(), "hub.db")
	addDevice(t, path, db.Device{ID: "dev1", Kind: db.KindTuya, Name: "tomada"})

	var trendCalls int
	mux := http.NewServeMux()
	writeJSON := func(w http.ResponseWriter, v any) { _ = json.NewEncoder(w).Encode(v) }
	mux.HandleFunc("/v1.0/token", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, map[string]any{"success": true, "result": map[string]any{"access_token": "tok", "expire_time": 3600}})
	})
	mux.HandleFunc("/v1.0/iot-03/devices/dev1/report-logs", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, map[string]any{"success": true, "result": map[string]any{"has_more": false, "list": []any{}}})
	})
	mux.HandleFunc("/v1.0/iot-03/energy/electricity/devices/statistics-trend", func(w http.ResponseWriter, r *http.Request) {
		trendCalls++
		q := r.URL.Query()
		writeJSON(w, map[string]any{"success": true, "result": []any{map[string]any{"time": q.Get("startTime"), "value": 2.5}}})
	})
	cloud := httptest.NewServer(mux)
	defer cloud.Close()

	svc := newService(t, path, Config{Cloud: tuya.CloudConfig{AccessID: "id", AccessSecret: "secret", Region: "us", BaseURL: cloud.URL}})
	svc.now = func() time.Time { return time.Date(2026, 11, 11, 14, 30, 0, 0, time.Local) }

	series, err := svc.EnergyTrend(context.Background(), "dev1", "7d")
	if err != nil {
		t.Fatal(err)
	}
	if !series.Available || series.Total != 2.5 || trendCalls != 1 {
		t.Fatalf("unexpected fallback series: %+v (trendCalls=%d)", series, trendCalls)
	}
}

func TestEnergyTrendNotSubscribed(t *testing.T) {
	path := filepath.Join(t.TempDir(), "hub.db")
	addDevice(t, path, db.Device{ID: "dev1", Kind: db.KindTuya, Name: "tomada"})

	mux := http.NewServeMux()
	writeJSON := func(w http.ResponseWriter, v any) { _ = json.NewEncoder(w).Encode(v) }
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1.0/token":
			writeJSON(w, map[string]any{"success": true, "result": map[string]any{"access_token": "tok", "expire_time": 3600}})
		default:
			writeJSON(w, map[string]any{"success": false, "code": 28841101, "msg": "No permissions. This API is not subscribed."})
		}
	})
	cloud := httptest.NewServer(mux)
	defer cloud.Close()

	svc := newService(t, path, Config{Cloud: tuya.CloudConfig{AccessID: "id", AccessSecret: "secret", Region: "us", BaseURL: cloud.URL}})

	series, err := svc.EnergyTrend(context.Background(), "dev1", "7d")
	if err != nil {
		t.Fatal(err)
	}
	if series.Available || series.Reason != EnergyReasonNotSubscribed {
		t.Fatalf("unexpected series: %+v", series)
	}
	if len(series.Points) != 0 {
		t.Fatalf("unsubscribed series should not have points: %+v", series.Points)
	}
}

func TestEnergyTrendWithoutCloud(t *testing.T) {
	path := filepath.Join(t.TempDir(), "hub.db")
	addDevice(t, path, db.Device{ID: "dev1", Kind: db.KindTuya, Name: "tomada"})
	svc := newService(t, path, Config{})

	series, err := svc.EnergyTrend(context.Background(), "dev1", "today")
	if err != nil {
		t.Fatal(err)
	}
	if series.Available || series.Reason != EnergyReasonCloudNotConfigured {
		t.Fatalf("unexpected series: %+v", series)
	}
}

func TestEnergyTrendUnknownDevice(t *testing.T) {
	path := filepath.Join(t.TempDir(), "hub.db")
	svc := newService(t, path, Config{})

	_, err := svc.EnergyTrend(context.Background(), "ghost", "today")
	if !errors.Is(err, db.ErrNotFound) {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
}

func TestEnergyChunks(t *testing.T) {
	var days []string
	for i := 0; i < 30; i++ {
		days = append(days, time.Date(2026, 10, 13, 0, 0, 0, 0, time.UTC).AddDate(0, 0, i).Format("20060102"))
	}
	chunks := energyChunks(days, "day")
	if len(chunks) != 5 {
		t.Fatalf("got %d chunks, want 5", len(chunks))
	}
	if chunks[0][0] != days[0] || chunks[0][1] != days[6] {
		t.Fatalf("first chunk = %v", chunks[0])
	}
	if chunks[4][0] != days[28] || chunks[4][1] != days[29] {
		t.Fatalf("last chunk = %v", chunks[4])
	}
	if hourly := energyChunks([]string{"2026111100", "2026111101"}, "hour"); len(hourly) != 1 {
		t.Fatalf("hourly chunks = %v", hourly)
	}
}
