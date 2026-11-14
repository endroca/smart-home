package fallback

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"smart_home/hub/internal/model"
	"smart_home/hub/internal/store"
)

// staticDir is a test directory of LAN credentials.
type staticDir struct {
	tokens map[string]string
	urls   map[string]string
}

func (d staticDir) DeviceToken(thing string) string { return d.tokens[thing] }
func (d staticDir) DeviceURL(thing string) string   { return d.urls[thing] }

func testClient(t *testing.T, handler http.HandlerFunc) (*Client, *httptest.Server) {
	t.Helper()
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)
	dir := staticDir{
		tokens: map[string]string{"t1": "secret"},
		urls:   map[string]string{"t1": srv.URL},
	}
	return New(store.New(), dir), srv
}

func TestBaseURLPrefersKnownIPOverLocal(t *testing.T) {
	st := store.New()
	st.UpsertStatus("t1", &model.Status{Online: true, Thing: "t1", IP: "192.168.1.2"})
	dir := staticDir{tokens: map[string]string{"t1": "tok"}, urls: map[string]string{"t1": "http://t1.local"}}
	c := New(st, dir)
	got, err := c.BaseURL("t1")
	if err != nil || got != "http://192.168.1.2:80" {
		t.Fatalf("BaseURL=%q err=%v", got, err)
	}
}

func TestBaseURLHonoursExplicitNonLocalOverride(t *testing.T) {
	st := store.New()
	st.UpsertStatus("t1", &model.Status{Online: true, Thing: "t1", IP: "192.168.1.2"})
	dir := staticDir{tokens: map[string]string{"t1": "tok"}, urls: map[string]string{"t1": "http://10.0.0.9:81"}}
	c := New(st, dir)
	got, err := c.BaseURL("t1")
	if err != nil || got != "http://10.0.0.9:81" {
		t.Fatalf("BaseURL=%q err=%v", got, err)
	}
}

func TestApplyStateAndFetch(t *testing.T) {
	c, _ := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer secret" {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/api/state":
			w.Header().Set("Content-Type", "application/json")
			io.WriteString(w, `{"ok":true,"state":{"power":true,"light":false,"speed":2,"direction":"forward"},"transmitted":["power"]}`)
		case r.Method == http.MethodGet && r.URL.Path == "/api/state":
			io.WriteString(w, `{"ok":true,"pendingSync":false,"fans":[{"id":"fan0","name":"Sala","state":{"power":true,"light":false,"speed":2,"direction":"forward"},"source":"http","updatedAt":1762286400,"counter":3}]}`)
		default:
			http.NotFound(w, r)
		}
	})
	resp, sh, err := c.ApplyState(context.Background(), "t1", "fan0", map[string]any{"power": true}, false)
	if err != nil {
		t.Fatal(err)
	}
	if sh == nil || sh.Reported["speed"].(json.Number).String() != "2" {
		t.Fatalf("unexpected state: %#v", sh)
	}
	if resp["state"] == nil {
		t.Fatal("missing state response")
	}
}

func TestSetHubURL(t *testing.T) {
	var got string
	c, _ := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/api/hub" {
			http.NotFound(w, r)
			return
		}
		var body map[string]string
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			http.Error(w, "json", http.StatusBadRequest)
			return
		}
		got = body["url"]
		io.WriteString(w, `{"ok":true,"hub":{"configured":true}}`)
	})
	if _, err := c.SetHubURL(context.Background(), "t1", "http://192.168.1.10:8080"); err != nil {
		t.Fatal(err)
	}
	if got != "http://192.168.1.10:8080" {
		t.Fatalf("url=%q", got)
	}
}

func TestCommandAndUpload(t *testing.T) {
	var uploaded bool
	c, _ := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/api/fan/add":
			io.WriteString(w, `{"ok":true,"id":"fan1"}`)
		case r.URL.Path == "/api/update":
			if r.URL.Query().Get("sha256") == "" {
				http.Error(w, "missing sha", http.StatusBadRequest)
				return
			}
			if err := r.ParseMultipartForm(4 << 20); err != nil {
				http.Error(w, "multipart", http.StatusBadRequest)
				return
			}
			if _, _, err := r.FormFile("firmware"); err != nil {
				http.Error(w, "file", http.StatusBadRequest)
				return
			}
			uploaded = true
			io.WriteString(w, `{"ok":true}`)
		default:
			http.NotFound(w, r)
		}
	})
	out, err := c.Command(context.Background(), "t1", "fan.add", map[string]any{})
	if err != nil {
		t.Fatal(err)
	}
	if out["id"] != "fan1" {
		t.Fatalf("unexpected fan.add: %#v", out)
	}
	f, err := os.CreateTemp(t.TempDir(), "fw-*.bin")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.WriteString("firmware"); err != nil {
		t.Fatal(err)
	}
	f.Close()
	if _, err := c.UploadFirmware(context.Background(), "t1", f.Name(), strings.Repeat("a", 64)); err != nil {
		t.Fatal(err)
	}
	if !uploaded {
		t.Fatal("firmware upload did not reach the server")
	}
}

// Without a configured URL and with no known IP, the only candidate is the
// mDNS name, which the Go resolver cannot resolve. The error must say what to
// do about it instead of just naming the failure.
func TestUnreachableGatewayErrorIsActionable(t *testing.T) {
	dir := staticDir{tokens: map[string]string{"sem-url": "tok"}}
	c := New(store.New(), dir)

	_, err := c.FetchState(context.Background(), "sem-url", "fan0")
	if err == nil {
		t.Fatal("expected an error without a LAN address")
	}
	msg := err.Error()
	if !strings.Contains(msg, "Set the gateway URL") || !strings.Contains(msg, "sem-url.local") {
		t.Fatalf("error is not actionable: %q", msg)
	}
	if c.Available("sem-url") {
		t.Fatal("a gateway with no reachable address must not be Available")
	}
}

// A gateway that announced itself over mDNS is reachable with no configuration:
// that is what removes the chicken-and-egg of registering a thing.
func TestBaseURLUsesAnnouncedURL(t *testing.T) {
	st := store.New()
	st.SetAnnouncedURL("t1", "http://192.168.1.7:80")
	dir := staticDir{tokens: map[string]string{"t1": "tok"}}
	c := New(st, dir)

	got, err := c.BaseURL("t1")
	if err != nil || got != "http://192.168.1.7:80" {
		t.Fatalf("BaseURL=%q err=%v", got, err)
	}
	if !c.Available("t1") {
		t.Fatal("an announced gateway must be Available")
	}
}

// The operator's explicit address still wins over what the gateway announced.
func TestBaseURLPreferenceOrder(t *testing.T) {
	st := store.New()
	st.SetAnnouncedURL("t1", "http://192.168.1.7:80")
	dir := staticDir{
		tokens: map[string]string{"t1": "tok"},
		urls:   map[string]string{"t1": "http://10.0.0.9:81"},
	}
	c := New(st, dir)
	if got, _ := c.BaseURL("t1"); got != "http://10.0.0.9:81" {
		t.Fatalf("configured URL must win: %q", got)
	}

	// The IP the device reported is more current than the announcement.
	st.UpsertStatus("t1", &model.Status{Online: true, Thing: "t1", IP: "192.168.1.9"})
	if got, _ := c.BaseURL("t1"); got != "http://10.0.0.9:81" {
		t.Fatalf("configured URL must still win: %q", got)
	}
	c2 := New(st, staticDir{tokens: map[string]string{"t1": "tok"}})
	if got, _ := c2.BaseURL("t1"); got != "http://192.168.1.9:80" {
		t.Fatalf("status IP must win over the announcement: %q", got)
	}
}
