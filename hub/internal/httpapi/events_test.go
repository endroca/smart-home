package httpapi

import (
	"bufio"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"smart_home/hub/internal/config"
	"smart_home/hub/internal/db"
	"smart_home/hub/internal/eventbus"
	"smart_home/hub/internal/hub"
)

func TestEventStreamDeliversEvents(t *testing.T) {
	store, err := db.Open(filepath.Join(t.TempDir(), "hub.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	cfg := config.Config{HTTPAddr: ":0"}
	h, err := hub.New(cfg, store)
	if err != nil {
		t.Fatal(err)
	}

	bus := eventbus.New()
	server := New(h, cfg, store)
	server.SetEventBus(bus)
	srv := httptest.NewServer(server.Handler())
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/api/events")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status=%d", resp.StatusCode)
	}
	if ct := resp.Header.Get("Content-Type"); !strings.HasPrefix(ct, "text/event-stream") {
		t.Fatalf("content-type=%q", ct)
	}

	lines := make(chan string, 16)
	go func() {
		sc := bufio.NewScanner(resp.Body)
		for sc.Scan() {
			lines <- sc.Text()
		}
		close(lines)
	}()

	waitFor := func(substr string) string {
		t.Helper()
		deadline := time.After(3 * time.Second)
		for {
			select {
			case l, ok := <-lines:
				if !ok {
					t.Fatalf("stream closed before %q", substr)
				}
				if strings.Contains(l, substr) {
					return l
				}
			case <-deadline:
				t.Fatalf("timeout waiting for %q", substr)
				return ""
			}
		}
	}

	waitFor(": connected")
	bus.PublishData("tuya", map[string]any{"device": "d1", "dps": map[string]any{"1": true}})
	data := waitFor("data:")
	if !strings.Contains(data, `"type":"tuya"`) || !strings.Contains(data, `"device":"d1"`) {
		t.Fatalf("unexpected event payload: %s", data)
	}
}

func TestEventStreamUnavailableWithoutBus(t *testing.T) {
	srv, closeFn := dbServer(t)
	defer closeFn()
	resp, err := http.Get(srv.URL + "/api/events")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusServiceUnavailable {
		t.Fatalf("status=%d, want 503", resp.StatusCode)
	}
}
