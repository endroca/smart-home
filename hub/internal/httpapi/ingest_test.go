package httpapi

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"smart_home/hub/internal/config"
	"smart_home/hub/internal/db"
	"smart_home/hub/internal/hub"
)

// newIngestServer builds a server whose database knows one gateway and its
// LAN token, so the device-auth path can be exercised.
func newIngestServer(t *testing.T, token string) (*httptest.Server, *hub.Hub, *db.Store) {
	t.Helper()
	store, err := db.Open(filepath.Join(t.TempDir(), "hub.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })

	ctx := context.Background()
	if err := store.UpsertDevice(ctx, db.Device{ID: "gw", Kind: db.KindThing, Name: "Gateway", Token: token}); err != nil {
		t.Fatal(err)
	}

	cfg := config.Config{HTTPAddr: ":0"}
	h, err := hub.New(cfg, store)
	if err != nil {
		t.Fatal(err)
	}

	server := New(h, cfg, store)
	srv := httptest.NewServer(server.Handler())
	t.Cleanup(srv.Close)
	return srv, h, store
}

func postEvent(t *testing.T, srv *httptest.Server, thing, token string, body any) *http.Response {
	t.Helper()
	raw, err := json.Marshal(body)
	if err != nil {
		t.Fatal(err)
	}
	req, err := http.NewRequest(http.MethodPost, srv.URL+"/api/things/"+thing+"/events", bytes.NewReader(raw))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	return resp
}

func TestIngestRequiresDeviceToken(t *testing.T) {
	srv, _, _ := newIngestServer(t, "s3cr3t-token-value")
	defer srv.Close()

	for _, token := range []string{"", "wrong-token-value", "s3cr3t-token-valu"} {
		resp := postEvent(t, srv, "gw", token, map[string]any{
			"type": "state", "id": "fan0", "reported": map[string]any{"power": true},
		})
		resp.Body.Close()
		if resp.StatusCode != http.StatusUnauthorized {
			t.Fatalf("token %q: status=%d, want 401", token, resp.StatusCode)
		}
	}
}

func TestIngestStateUpdatesCacheAndReplica(t *testing.T) {
	srv, h, store := newIngestServer(t, "s3cr3t-token-value")
	defer srv.Close()

	resp := postEvent(t, srv, "gw", "s3cr3t-token-value", map[string]any{
		"type": "state",
		"id":   "fan0",
		"reported": map[string]any{
			"power": true, "light": false, "speed": 3,
			"direction": "forward", "source": "rf_remote", "updatedAt": 1700,
		},
	})
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status=%d", resp.StatusCode)
	}

	t1, ok := h.Thing("gw")
	if !ok {
		t.Fatal("thing not found after ingest")
	}
	sh := t1.States["fan0"]
	if sh == nil {
		t.Fatal("state not created by ingest")
	}
	if got, _ := sh.Reported["power"].(bool); !got {
		t.Fatalf("reported state not applied: %#v", sh.Reported)
	}
	if sh.Stale {
		t.Fatal("a pushed state must not be stale")
	}

	rows, err := store.ThingStates(context.Background(), "gw")
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 1 || rows[0].Source != "rf_remote" || rows[0].DeviceTS != 1700 {
		t.Fatalf("replica not written: %+v", rows)
	}
}

func TestIngestStatusMarksOnlineAndPersistsOnChange(t *testing.T) {
	srv, h, store := newIngestServer(t, "s3cr3t-token-value")
	defer srv.Close()

	body := map[string]any{
		"type": "status",
		"status": map[string]any{
			"version": "1.2.3", "ip": "192.168.1.50", "rssi": -55, "heap": 123456,
		},
	}
	resp := postEvent(t, srv, "gw", "s3cr3t-token-value", body)
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status=%d", resp.StatusCode)
	}

	t1, _ := h.Thing("gw")
	if t1.Status == nil || !t1.Status.Online {
		t.Fatalf("a status push must mark the gateway online: %+v", t1.Status)
	}
	if t1.Status.IP != "192.168.1.50" {
		t.Fatalf("status fields not applied: %+v", t1.Status)
	}

	stored, err := store.ThingStatusFor(context.Background(), "gw")
	if err != nil {
		t.Fatal(err)
	}
	if got, _ := stored.Status["version"].(string); got != "1.2.3" {
		t.Fatalf("status replica not written: %#v", stored.Status)
	}

	// A repeated heartbeat with nothing new must not write again: the replica
	// keeps the original last_seen timestamp.
	before := stored.LastSeen
	resp = postEvent(t, srv, "gw", "s3cr3t-token-value", body)
	resp.Body.Close()
	again, err := store.ThingStatusFor(context.Background(), "gw")
	if err != nil {
		t.Fatal(err)
	}
	if !again.LastSeen.Equal(before) {
		t.Fatalf("bare heartbeat rewrote the replica: %s -> %s", before, again.LastSeen)
	}
}

func TestIngestEvent(t *testing.T) {
	srv, h, _ := newIngestServer(t, "s3cr3t-token-value")
	defer srv.Close()

	resp := postEvent(t, srv, "gw", "s3cr3t-token-value", map[string]any{
		"type": "event",
		"event": map[string]any{
			"event": "state.failed", "id": "fan0",
			"error": map[string]any{"code": "rf_timeout", "message": "sem frame"},
		},
	})
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status=%d", resp.StatusCode)
	}

	t1, _ := h.Thing("gw")
	if t1.LastEvent == nil || t1.LastEvent["event"] != "state.failed" {
		t.Fatalf("event not recorded: %#v", t1.LastEvent)
	}
}

// The firmware only stores a SHA-256 digest of the token, so it authenticates
// with that digest. The hub must accept it.
func TestIngestAcceptsTokenDigest(t *testing.T) {
	const token = "s3cr3t-token-value"
	srv, h, _ := newIngestServer(t, token)
	defer srv.Close()

	sum := sha256.Sum256([]byte(token))
	digest := hex.EncodeToString(sum[:])

	resp := postEvent(t, srv, "gw", digest, map[string]any{
		"type": "state", "id": "fan0", "reported": map[string]any{"power": true},
	})
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("digest auth status=%d, want 200", resp.StatusCode)
	}
	t1, _ := h.Thing("gw")
	if t1.States["fan0"] == nil {
		t.Fatal("state was not applied with digest auth")
	}

	// A wrong digest is still rejected.
	bad := postEvent(t, srv, "gw", strings.Repeat("00", 32), map[string]any{
		"type": "state", "id": "fan0", "reported": map[string]any{"power": false},
	})
	bad.Body.Close()
	if bad.StatusCode != http.StatusUnauthorized {
		t.Fatalf("bad digest status=%d, want 401", bad.StatusCode)
	}
}

func TestIngestRejectsUnknownType(t *testing.T) {
	srv, _, _ := newIngestServer(t, "s3cr3t-token-value")
	defer srv.Close()

	resp := postEvent(t, srv, "gw", "s3cr3t-token-value", map[string]any{"type": "bogus"})
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("status=%d, want 400", resp.StatusCode)
	}
}

// A gateway the hub does not know, or one without a stored token, must be
// rejected rather than silently accepted.
func TestIngestRejectsUnknownGateway(t *testing.T) {
	srv, _, _ := newIngestServer(t, "s3cr3t-token-value")
	defer srv.Close()

	resp := postEvent(t, srv, "unknown", "s3cr3t-token-value", map[string]any{
		"type": "state", "id": "fan0", "reported": map[string]any{"power": true},
	})
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("status=%d, want 401", resp.StatusCode)
	}
}
