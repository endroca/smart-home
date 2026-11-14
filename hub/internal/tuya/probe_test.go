package tuya

import (
	"context"
	"encoding/json"
	"net/http"
	"os"
	"sort"
	"strconv"
	"strings"
	"testing"
	"time"
)

// TestProbeCloud is an opt-in Phase 0 diagnostic. It asks the real Tuya cloud
// what the account exposes for every device, and then dumps the door lock's
// specifications, current status, recent report logs and the raw shape of the
// Smart Lock endpoints (unlock records, device logs).
//
// The goal is data, not assertions: we want to learn the lock's real DP codes
// and whether the project's subscription includes the Smart Lock APIs.
//
// It is skipped unless TUYA_PROBE=1, so it never runs in CI or `make check`.
// Run it with:
//
//	TUYA_PROBE=1 \
//	TUYA_ACCESS_ID=... TUYA_ACCESS_SECRET=... TUYA_REGION=us \
//	TUYA_SEED_DEVICE_ID=<any device on the account> \
//	go test ./internal/tuya/ -run TestProbeCloud -v
//
// TUYA_SEED_DEVICE_ID is required because the cloud only lists an account's
// devices once we know its uid, and the uid is discovered from a device that is
// already linked to the app account.
func TestProbeCloud(t *testing.T) {
	if os.Getenv("TUYA_PROBE") != "1" {
		t.Skip("cloud probe is opt-in: set TUYA_PROBE=1 (plus TUYA_ACCESS_ID/TUYA_ACCESS_SECRET/TUYA_SEED_DEVICE_ID) to run it")
	}
	accessID := strings.TrimSpace(os.Getenv("TUYA_ACCESS_ID"))
	accessSecret := strings.TrimSpace(os.Getenv("TUYA_ACCESS_SECRET"))
	if accessID == "" || accessSecret == "" {
		t.Fatal("set TUYA_ACCESS_ID and TUYA_ACCESS_SECRET")
	}
	seed := strings.TrimSpace(os.Getenv("TUYA_SEED_DEVICE_ID"))
	if seed == "" {
		t.Fatal("set TUYA_SEED_DEVICE_ID to any device id already linked to the app account")
	}

	cloud, err := NewCloud(CloudConfig{
		AccessID:     accessID,
		AccessSecret: accessSecret,
		Region:       probeEnv("TUYA_REGION", "us"),
	})
	if err != nil {
		t.Fatalf("new cloud client: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()

	devices, err := cloud.Devices(ctx, seed)
	if err != nil {
		t.Fatalf("list cloud devices: %v", err)
	}
	t.Logf("account has %d device(s):", len(devices))
	for _, d := range devices {
		t.Logf("  id=%s name=%q category=%q product=%s version=%s online=%v local_key=%v",
			d.ID, d.Name, d.Category, d.ProductID, d.Version, d.Online, d.LocalKey != "")
	}

	probed := 0
	for _, d := range devices {
		if !looksLikeLock(d) {
			continue
		}
		probed++
		probeLock(ctx, t, cloud, d)
	}
	if probed == 0 {
		t.Logf("no device looked like a lock (category \"ms\" or a lock-ish name); " +
			"re-run pointing the probe at the right device by editing looksLikeLock")
	}
}

// probeLock dumps everything we can learn about one lock from the cloud.
func probeLock(ctx context.Context, t *testing.T, c *Cloud, d CloudDevice) {
	t.Logf("=== probing lock %q (%s) ===", d.Name, d.ID)

	specs, err := c.Specifications(ctx, d.ID)
	if err != nil {
		t.Logf("specifications: %v", err)
	}
	codes := make([]string, 0, len(specs))
	for _, s := range specs {
		values, _ := json.Marshal(s.Values)
		t.Logf("  spec dp=%s code=%s name=%q type=%s writable=%v values=%s",
			s.DPID, s.Code, s.Name, s.Type, s.Writable, values)
		if s.Code != "" {
			codes = append(codes, s.Code)
		}
	}
	sort.Strings(codes)

	// Raw status: shows the underlying result array (code/value) the mapping
	// layer hides, which is what we want to eyeball for lock DPs.
	probeRaw(ctx, t, c, "status", http.MethodGet, "/v1.0/devices/"+d.ID+"/status", nil)

	// Smart Lock Open APIs. The correct prefix is /devices/{id}/door-lock/...
	// (not /smart-lock/...). A "uri path invalid" here means the project did
	// not subscribe to "Smart Lock Open Service" on iot.tuya.com.
	probeRaw(ctx, t, c, "remote-unlocks", http.MethodGet,
		"/v1.0/devices/"+d.ID+"/door-lock/remote-unlocks", nil)
	probeRaw(ctx, t, c, "temp-passwords", http.MethodGet,
		"/v1.0/devices/"+d.ID+"/door-lock/temp-passwords", nil)
	probeRaw(ctx, t, c, "latest media url", http.MethodGet,
		"/v1.0/devices/"+d.ID+"/door-lock/latest/media/url", nil)

	// Report logs: the same /report-logs endpoint the hub already uses for the
	// energy chart. If unlock DPs show up here, this is our event source for
	// automations that must work while the lock sleeps.
	if len(codes) > 0 {
		end := time.Now()
		start := end.Add(-48 * time.Hour)
		logs, err := c.EnergyLogs(ctx, d.ID, codes, start.UnixMilli(), end.UnixMilli())
		if err != nil {
			t.Logf("report-logs (48h): %v", err)
		} else {
			t.Logf("report-logs (48h): %d entr(ies)", len(logs))
			for i, l := range logs {
				if i >= 40 {
					t.Logf("    ... (%d more)", len(logs)-i)
					break
				}
				t.Logf("    %s code=%s value=%v",
					time.UnixMilli(l.EventTime).Format(time.RFC3339), l.Code, l.Value)
			}
		}
	}

	// Raw endpoint probes. The Smart Lock Open APIs need the "Smart Lock Open
	// Service" subscription; an error here is itself a finding, not a failure.
	end := time.Now()
	start := end.Add(-7 * 24 * time.Hour)
	window := map[string]string{
		"page_no":    "1",
		"page_size":  "20",
		"start_time": strconv.FormatInt(start.UnixMilli(), 10),
		"end_time":   strconv.FormatInt(end.UnixMilli(), 10),
	}
	probeRaw(ctx, t, c, "open-logs", http.MethodGet,
		"/v1.0/devices/"+d.ID+"/door-lock/open-logs", window)
	probeRaw(ctx, t, c, "records", http.MethodGet,
		"/v1.0/devices/"+d.ID+"/door-lock/records", window)
	probeRaw(ctx, t, c, "alarm-logs", http.MethodGet,
		"/v1.0/devices/"+d.ID+"/door-lock/alarm-logs", window)
	for _, typ := range []string{"5", "6", "7"} {
		probeRaw(ctx, t, c, "device logs type="+typ, http.MethodGet,
			"/v1.0/devices/"+d.ID+"/logs",
			map[string]string{
				"type":       typ,
				"size":       "20",
				"start_time": strconv.FormatInt(start.UnixMilli(), 10),
				"end_time":   strconv.FormatInt(end.UnixMilli(), 10),
			})
	}
}

// probeRaw calls an endpoint and logs the full raw JSON result (or the error),
// so we can see the response shape without a typed wrapper.
func probeRaw(ctx context.Context, t *testing.T, c *Cloud, label, method, path string, query map[string]string) {
	t.Helper()
	res, err := c.do(ctx, method, path, query, nil, true)
	if err != nil {
		t.Logf("  %s -> error: %v", label, err)
		return
	}
	raw, _ := json.MarshalIndent(res, "", "  ")
	const max = 8000
	if len(raw) > max {
		t.Logf("  %s -> %s\n  ... (%d more bytes)", label, raw[:max], len(raw)-max)
		return
	}
	t.Logf("  %s -> %s", label, raw)
}

// lockCategories are the Tuya door-lock categories, including this lock's
// ("jtmspro", a "smart lock pro").
var lockCategories = map[string]bool{
	"mk": true, "ms": true, "jtmsbh": true, "jtmspro": true,
	"gyms": true, "hotelms": true, "videolock": true, "photolock": true,
}

// looksLikeLock is a loose heuristic: a known lock category, or a lock-ish
// name for anything whose product does not use one.
func looksLikeLock(d CloudDevice) bool {
	if lockCategories[strings.ToLower(strings.TrimSpace(d.Category))] {
		return true
	}
	name := strings.ToLower(d.Name)
	for _, kw := range []string{"lock", "fechadura", "porta", "door"} {
		if strings.Contains(name, kw) {
			return true
		}
	}
	return false
}

func probeEnv(key, fallback string) string {
	if v := strings.TrimSpace(os.Getenv(key)); v != "" {
		return v
	}
	return fallback
}
