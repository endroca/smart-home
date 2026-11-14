package hub

import (
	"context"
	"log"
	"strings"
	"time"
)

// hubURLSync remembers, per gateway, the push URL the hub already configured
// on the device in this process. It avoids an extra round-trip on every probe
// while still reapplying the URL after a hub restart (the map is in memory).
type hubURLSync struct {
	url string
	at  time.Time
}

// syncDeviceHubURL configures the device with the URL it should push state to.
// The device is the source of truth, so this is what lets every firmware-side
// change (RF remote, local UI, correction) reach the hub without any manual
// setup. It is best-effort: a failure is logged and retried on a later probe.
//
// The URL is the hub's LAN IP, never a *.local name: the ESP32 HTTP client
// cannot resolve mDNS.
func (h *Hub) syncDeviceHubURL(ctx context.Context, thing string) {
	url := h.cfg.HubURL()
	if url == "" || !h.fallback.Available(thing) {
		return
	}

	h.hubURLMu.Lock()
	if h.hubURLs == nil {
		h.hubURLs = map[string]*hubURLSync{}
	}
	st := h.hubURLs[thing]
	if st == nil {
		st = &hubURLSync{}
		h.hubURLs[thing] = st
	}
	if st.url == url && time.Since(st.at) < 10*time.Minute {
		h.hubURLMu.Unlock()
		return
	}
	// Throttle failed attempts so a flapping device does not add a request to
	// every probe.
	if !st.at.IsZero() && time.Since(st.at) < 30*time.Second {
		h.hubURLMu.Unlock()
		return
	}
	st.at = time.Now()
	h.hubURLMu.Unlock()

	// Skip the POST when the device already reports the same URL.
	if diag, err := h.fallback.Diag(ctx, thing); err == nil {
		if hub, ok := diag["hub"].(map[string]any); ok {
			if current, _ := hub["url"].(string); strings.TrimRight(current, "/") == url {
				h.markHubURLSynced(thing, url)
				return
			}
		}
	}

	if _, err := h.fallback.SetHubURL(ctx, thing, url); err != nil {
		log.Printf("[hub] configure push url on %s: %v", thing, err)
		return
	}
	h.markHubURLSynced(thing, url)
	log.Printf("[hub] %s pushes state to %s", thing, url)
}

func (h *Hub) markHubURLSynced(thing, url string) {
	h.hubURLMu.Lock()
	defer h.hubURLMu.Unlock()
	if h.hubURLs == nil {
		h.hubURLs = map[string]*hubURLSync{}
	}
	h.hubURLs[thing] = &hubURLSync{url: url, at: time.Now()}
}
