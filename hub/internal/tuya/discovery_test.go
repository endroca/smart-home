package tuya

import (
	"net"
	"testing"
)

func TestParseDiscoveryPlaintext(t *testing.T) {
	data := []byte(`{"gwId":"bf123","devId":"bf123","ip":"192.168.1.50","version":"3.3","productKey":"pk1"}`)
	d := parseDiscovery(data, net.ParseIP("192.168.1.99"))
	if d == nil {
		t.Fatal("expected a device")
	}
	if d.ID != "bf123" || d.IP != "192.168.1.50" || d.Version != "3.3" || d.ProductKey != "pk1" {
		t.Fatalf("unexpected device: %+v", d)
	}
}

func TestParseDiscoveryEncrypted(t *testing.T) {
	plain := []byte(`{"gwId":"bf456","ip":"192.168.1.51","version":"3.2"}`)
	enc, err := aesECBEncrypt(discoveryKey, plain, true)
	if err != nil {
		t.Fatal(err)
	}
	d := parseDiscovery(enc, net.ParseIP("192.168.1.52"))
	if d == nil {
		t.Fatal("expected a device")
	}
	if d.ID != "bf456" || d.IP != "192.168.1.51" || d.Version != "3.2" {
		t.Fatalf("unexpected device: %+v", d)
	}
}

func TestParseDiscoveryFrame(t *testing.T) {
	plain := []byte(`{"gwId":"bf789","ip":"192.168.1.53"}`)
	frame, err := packReply(V33, nil, 1, cmdDPQuery, 0, plain)
	if err != nil {
		t.Fatal(err)
	}
	d := parseDiscovery(frame, net.ParseIP("192.168.1.54"))
	if d == nil || d.ID != "bf789" {
		t.Fatalf("unexpected device: %+v", d)
	}
}

func TestParseDiscovery6699(t *testing.T) {
	plain := []byte(`{"gwId":"bfabc","ip":"192.168.1.56","version":"3.5"}` + "\x00\x00")
	raw := append([]byte{0, 0, 0, 0}, plain...)
	frame, err := pack6699(discoveryKey, 1, cmdDevInfo, raw)
	if err != nil {
		t.Fatal(err)
	}
	d := parseDiscovery(frame, net.ParseIP("192.168.1.57"))
	if d == nil || d.ID != "bfabc" || d.Version != "3.5" {
		t.Fatalf("unexpected device: %+v", d)
	}
}

func TestParseDiscoveryIgnoresSolicitation(t *testing.T) {
	data := []byte(`{"from":"app","ip":"192.168.1.10"}`)
	if d := parseDiscovery(data, net.ParseIP("192.168.1.10")); d != nil {
		t.Fatalf("solicitation parsed as device: %+v", d)
	}
}

func TestParseDiscoveryRequiresID(t *testing.T) {
	data := []byte(`{"ip":"192.168.1.55"}`)
	if d := parseDiscovery(data, net.ParseIP("192.168.1.55")); d != nil {
		t.Fatalf("device without id parsed: %+v", d)
	}
}
