package hub

import (
	"testing"

	"github.com/hashicorp/mdns"

	"smart_home/hub/internal/config"
)

// The `thing` TXT record the firmware publishes is authoritative; the host name
// is only the fallback.
func TestMDNSThingResolution(t *testing.T) {
	cases := []struct {
		name string
		e    *mdns.ServiceEntry
		want string
	}{
		{
			name: "txt record",
			e:    &mdns.ServiceEntry{Host: "outro.local.", InfoFields: []string{"thing=smart-control-rf01"}},
			want: "smart-control-rf01",
		},
		{
			name: "txt with padding",
			e:    &mdns.ServiceEntry{Host: "x.local.", InfoFields: []string{"outro=1", "thing= gw-2 "}},
			want: "gw-2",
		},
		{
			name: "falls back to the host",
			e:    &mdns.ServiceEntry{Host: "smart-control-rf02.local."},
			want: "smart-control-rf02",
		},
		{
			name: "ignores a host that is not a single label",
			e:    &mdns.ServiceEntry{Host: "algo.exemplo.local."},
			want: "",
		},
		{
			name: "no host and no txt",
			e:    &mdns.ServiceEntry{},
			want: "",
		},
	}
	for _, c := range cases {
		if got := mdnsThing(c.e); got != c.want {
			t.Errorf("%s: mdnsThing = %q, want %q", c.name, got, c.want)
		}
	}
}

// Anything the gateway announced has to be remembered, and an entry without an
// IPv4 address is useless.
func TestOnMDNSEntryCachesTheAddress(t *testing.T) {
	h, err := New(config.Config{FirmwareDir: t.TempDir()}, nil)
	if err != nil {
		t.Fatal(err)
	}
	entry := &mdns.ServiceEntry{
		Host:       "smart-control-rf01.local.",
		AddrV4:     []byte{192, 168, 1, 7},
		Port:       80,
		InfoFields: []string{"thing=smart-control-rf01"},
	}
	h.onMDNSEntry(entry, map[string]bool{})
	if got := h.store.AnnouncedURL("smart-control-rf01"); got != "http://192.168.1.7:80" {
		t.Fatalf("announced URL = %q", got)
	}

	// No IPv4 address: nothing to remember.
	h.onMDNSEntry(&mdns.ServiceEntry{Host: "x.local.", InfoFields: []string{"thing=outro"}}, map[string]bool{})
	if got := h.store.AnnouncedURL("outro"); got != "" {
		t.Fatalf("entry without an address must be ignored, got %q", got)
	}
}
