package hub

import (
	"context"
	"fmt"
	"log"
	"time"

	"smart_home/hub/internal/db"
)

// ThingSource lists the registered gateways. It is implemented by the hub
// database.
type ThingSource interface {
	ListDevices(ctx context.Context, kind string) ([]db.Device, error)
}

// ProbeThing discovers a gateway over the LAN: it fetches the device's
// discovery document (GET /api/manifest) and applies it. This makes discovery
// generic: the hub learns whatever the device declares, without assuming it is
// a fan.
func (h *Hub) ProbeThing(ctx context.Context, thing string) error {
	m, err := h.fallback.FetchManifest(ctx, thing)
	if err != nil {
		return err
	}
	if m.Thing == "" {
		m.Thing = thing
	}
	if m.Thing != thing {
		return fmt.Errorf("manifest thing %q does not match %q", m.Thing, thing)
	}
	h.onManifest(thing, m)

	// Make sure the device knows where to push its state: every firmware-side
	// change (RF remote, local UI) must reach the hub without manual setup.
	h.syncDeviceHubURL(ctx, thing)

	// Best-effort: read the current state over the LAN so the UI shows it
	// without waiting for the device push.
	for _, d := range m.Devices {
		st, err := h.fallback.FetchState(ctx, thing, d.ID)
		if err != nil || st == nil {
			continue
		}
		h.applyReported(thing, d.ID, st.Reported)
	}
	return nil
}

// StartDiscovery keeps the registered gateways fresh over the LAN (HTTP-first):
// it probes immediately at startup and then retries quickly while a gateway
// still has no manifest, falling back to a slow periodic revalidation. The
// in-memory manifest cache is rebuilt from HTTP whenever it is missing, so the
// hub does not depend on any external service to (re)discover the devices.
func (h *Hub) StartDiscovery(ctx context.Context) {
	go func() {
		fast := time.NewTicker(5 * time.Second)
		slow := time.NewTicker(60 * time.Second)
		defer fast.Stop()
		defer slow.Stop()

		h.probeGateways(ctx, true)
		for {
			select {
			case <-ctx.Done():
				return
			case <-fast.C:
				h.probeGateways(ctx, false)
			case <-slow.C:
				h.probeGateways(ctx, true)
			}
		}
	}()
}

// probeGateways probes the registered gateways over the LAN. With revalidate
// false it only probes the gateways that still have no manifest in memory (the
// fast retry, so a freshly started hub discovers the devices over HTTP right
// away); with true it probes every gateway (startup and the slow periodic
// pass).
func (h *Hub) probeGateways(ctx context.Context, revalidate bool) {
	if h.things == nil {
		return
	}
	list, err := h.things.ListDevices(ctx, db.KindThing)
	if err != nil {
		return
	}
	for _, d := range list {
		if ctx.Err() != nil {
			return
		}
		if !revalidate {
			if t, ok := h.store.Get(d.ID); ok && t.Manifest != nil {
				continue
			}
		}
		cctx, cancel := context.WithTimeout(ctx, 15*time.Second)
		if err := h.ProbeThing(cctx, d.ID); err != nil {
			log.Printf("[hub] LAN probe %s: %v", d.ID, err)
		}
		cancel()
	}
}
