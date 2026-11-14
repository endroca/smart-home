package hub

import (
	"context"
	"fmt"
	"io"
	"log"
	"net"
	"strings"
	"time"

	"github.com/hashicorp/mdns"

	"smart_home/hub/internal/db"
)

// StartMDNSDiscovery keeps a LAN browse running for gateways that announce
// themselves over mDNS.
//
// The firmware publishes an `_http._tcp` service named after its host
// (`smart-control-rf01.local`) with a `thing` TXT record. Go's resolver does not
// do mDNS, so without this the hub would only reach a gateway that already has a
// URL or a known IP — which is exactly the chicken-and-egg of registering one.
//
// A hit only teaches the hub an address; it never registers anything. That
// stays an operator decision.
func (h *Hub) StartMDNSDiscovery(ctx context.Context) {
	go func() {
		// Give the HTTP server a moment, then browse on a slow cadence: a
		// gateway announces itself on boot and periodically, so there is no
		// need to flood the network.
		timer := time.NewTimer(2 * time.Second)
		defer timer.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-timer.C:
			}
			h.browseMDNS(ctx)
			timer.Reset(30 * time.Second)
		}
	}()
}

func (h *Hub) browseMDNS(ctx context.Context) {
	registered := map[string]bool{}
	if h.things != nil {
		if list, err := h.things.ListDevices(ctx, db.KindThing); err == nil {
			for _, d := range list {
				registered[d.ID] = true
			}
		}
	}

	entries := make(chan *mdns.ServiceEntry, 64)
	queryCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	err := mdns.QueryContext(queryCtx, &mdns.QueryParam{
		Service:             "_http._tcp",
		Domain:              "local",
		Timeout:             3 * time.Second,
		Entries:             entries,
		WantUnicastResponse: true,
		DisableIPv6:         true,
		// The library logs an INFO line when IPv6 is unavailable, which is the
		// normal case on a Pi; keep the hub log for things that matter.
		Logger: log.New(io.Discard, "", 0),
	})
	// The library does not send after QueryContext returns.
	close(entries)
	for e := range entries {
		h.onMDNSEntry(e, registered)
	}
	if err != nil && ctx.Err() == nil {
		log.Printf("[hub] mDNS browse: %v", err)
	}
}

func (h *Hub) onMDNSEntry(e *mdns.ServiceEntry, registered map[string]bool) {
	if e == nil || e.AddrV4 == nil {
		return
	}
	thing := mdnsThing(e)
	if thing == "" {
		return
	}
	port := e.Port
	if port <= 0 {
		port = 80
	}
	url := fmt.Sprintf("http://%s", net.JoinHostPort(e.AddrV4.String(), fmt.Sprint(port)))
	if h.store.AnnouncedURL(thing) == url {
		return
	}
	h.store.SetAnnouncedURL(thing, url)
	log.Printf("[hub] mDNS: %s anunciou %s", thing, url)

	// A registered gateway that has no manifest yet can be reached now: probe
	// it so registration alone is enough to bring it up.
	if !registered[thing] {
		return
	}
	if t, ok := h.store.Get(thing); ok && t.Manifest != nil {
		return
	}
	go func() {
		cctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		if err := h.ProbeThing(cctx, thing); err != nil {
			log.Printf("[hub] probe after mDNS %s: %v", thing, err)
		}
	}()
}

// mdnsThing resolves the gateway name from an mDNS entry: the `thing` TXT
// record is authoritative, and the host name is the fallback.
func mdnsThing(e *mdns.ServiceEntry) string {
	for _, field := range e.InfoFields {
		if !strings.HasPrefix(field, "thing=") {
			continue
		}
		if v := strings.TrimSpace(strings.TrimPrefix(field, "thing=")); v != "" {
			return v
		}
	}
	host := strings.TrimSuffix(e.Host, ".")
	host = strings.TrimSuffix(host, ".local")
	if host == "" || strings.Contains(host, ".") {
		return ""
	}
	return host
}
