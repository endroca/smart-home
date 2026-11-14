package tuya

import (
	"bytes"
	"context"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"hash/crc32"
	"net"
	"strings"
	"testing"
	"time"
)

// packReply builds a device-to-client frame, which carries a 4-byte retcode.
func packReply(v Version, key []byte, seq, cmd, retcode uint32, payload []byte) ([]byte, error) {
	if v == V35 {
		raw := make([]byte, 4+len(payload))
		binary.BigEndian.PutUint32(raw[:4], retcode)
		copy(raw[4:], payload)
		return pack6699(key, seq, cmd, raw)
	}
	endLen := end55AA
	var hmacKey []byte
	if v.atLeast(V34) {
		endLen = end55AAHMAC
		hmacKey = key
	}
	buf := make([]byte, header55AA+4+len(payload))
	binary.BigEndian.PutUint32(buf[0:], prefix55AA)
	binary.BigEndian.PutUint32(buf[4:], seq)
	binary.BigEndian.PutUint32(buf[8:], cmd)
	binary.BigEndian.PutUint32(buf[12:], uint32(4+len(payload)+endLen))
	binary.BigEndian.PutUint32(buf[16:], retcode)
	copy(buf[20:], payload)
	if hmacKey != nil {
		buf = append(buf, hmacSHA256(hmacKey, buf)...)
	} else {
		sum := crc32.ChecksumIEEE(buf)
		var tmp [4]byte
		binary.BigEndian.PutUint32(tmp[:], sum)
		buf = append(buf, tmp[:]...)
	}
	buf = append(buf, 0x00, 0x00, 0xAA, 0x55)
	return buf, nil
}

type mockDevice struct {
	t       *testing.T
	version Version
	key     []byte
	ln      net.Listener
	port    int
	session []byte
	state   map[string]any
	push    bool
	strict  bool
}

func newMockDevice(t *testing.T, v Version, push bool) *mockDevice {
	return newMockDeviceOpts(t, v, push, false)
}

func newMockDeviceOpts(t *testing.T, v Version, push, strict bool) *mockDevice {
	t.Helper()
	ln, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	m := &mockDevice{
		t:       t,
		version: v,
		key:     []byte("0123456789abcdef"),
		ln:      ln,
		port:    ln.Addr().(*net.TCPAddr).Port,
		state:   map[string]any{"1": true, "2": float64(42)},
		push:    push,
		strict:  strict,
	}
	go m.serve()
	t.Cleanup(func() { _ = ln.Close() })
	return m
}

func (m *mockDevice) serve() {
	conn, err := m.ln.Accept()
	if err != nil {
		return
	}
	defer conn.Close()
	key := m.key
	if m.version.atLeast(V34) {
		if err := m.handshake(conn); err != nil {
			m.t.Logf("mock handshake: %v", err)
			return
		}
		key = m.session
	}
	for {
		msg, err := readMessage(conn, m.version, key, false)
		if err != nil {
			return
		}
		if err := m.handle(conn, key, msg); err != nil {
			m.t.Logf("mock handle: %v", err)
			return
		}
	}
}

func (m *mockDevice) handshake(conn net.Conn) error {
	realKey := m.key
	msg, err := readMessage(conn, m.version, realKey, false)
	if err != nil {
		return err
	}
	if msg.Cmd != cmdSessStart {
		return fmt.Errorf("expected 0x03, got 0x%02x", msg.Cmd)
	}
	clientNonce := msg.Payload
	if m.version == V34 {
		clientNonce, err = aesECBDecrypt(realKey, msg.Payload, true)
		if err != nil {
			return err
		}
	}
	deviceNonce := bytes.Repeat([]byte{0x11}, 16)
	resp := append(append([]byte{}, deviceNonce...), hmacSHA256(realKey, clientNonce)...)
	if m.version == V34 {
		resp, err = aesECBEncrypt(realKey, resp, true)
		if err != nil {
			return err
		}
	}
	reply, err := packReply(m.version, realKey, 900, cmdSessResp, 0, resp)
	if err != nil {
		return err
	}
	if _, err := conn.Write(reply); err != nil {
		return err
	}

	msg, err = readMessage(conn, m.version, realKey, false)
	if err != nil {
		return err
	}
	if msg.Cmd != cmdSessFinish {
		return fmt.Errorf("expected 0x05, got 0x%02x", msg.Cmd)
	}
	finish := msg.Payload
	if m.version == V34 {
		finish, err = aesECBDecrypt(realKey, finish, true)
		if err != nil {
			return err
		}
	}
	if !equalBytes(finish, hmacSHA256(realKey, deviceNonce)) {
		return fmt.Errorf("bad finish HMAC")
	}
	if m.version == V34 {
		m.session, err = deriveSessionKey34(realKey, clientNonce, deviceNonce)
	} else {
		m.session, err = deriveSessionKey35(realKey, clientNonce, deviceNonce)
	}
	return err
}

func (m *mockDevice) handle(conn net.Conn, key []byte, msg *Message) error {
	switch msg.Cmd {
	case cmdDPQuery, cmdDPQueryNew, cmdControl, cmdControlNew:
		js, err := decodePayload(m.version, key, msg.Cmd, msg.Payload, false)
		if err != nil {
			return err
		}
		if m.strict && (msg.Cmd == cmdDPQuery || msg.Cmd == cmdDPQueryNew) {
			var root map[string]any
			_ = json.Unmarshal(js, &root)
			if _, ok := root["dps"]; !ok {
				return m.replyError(conn, key, 1100, "data unvalid")
			}
		}
		if msg.Cmd == cmdControl || msg.Cmd == cmdControlNew {
			m.apply(js)
		}
		if err := m.replyStatus(conn, key, 1000, m.state); err != nil {
			return err
		}
		if m.push && (msg.Cmd == cmdDPQuery || msg.Cmd == cmdDPQueryNew) {
			go func() {
				time.Sleep(80 * time.Millisecond)
				_ = m.replyStatus(conn, key, 1001, map[string]any{"1": false, "2": float64(99)})
			}()
		}
		return nil
	case cmdHeartbeat:
		reply, err := packReply(m.version, key, 1002, cmdHeartbeat, 0, nil)
		if err != nil {
			return err
		}
		_, err = conn.Write(reply)
		return err
	default:
		return nil
	}
}

func (m *mockDevice) apply(js []byte) {
	var root map[string]any
	if err := json.Unmarshal(js, &root); err != nil {
		return
	}
	dps, _ := root["dps"].(map[string]any)
	if dps == nil {
		if data, ok := root["data"].(map[string]any); ok {
			dps, _ = data["dps"].(map[string]any)
		}
	}
	for k, v := range dps {
		// A null DP is a query placeholder, not a state change.
		if v == nil {
			continue
		}
		m.state[k] = v
	}
}

func (m *mockDevice) replyError(conn net.Conn, key []byte, seq uint32, message string) error {
	js, err := json.Marshal(map[string]any{"error": message})
	if err != nil {
		return err
	}
	enc, err := encodePayload(m.version, key, cmdStatus, js)
	if err != nil {
		return err
	}
	reply, err := packReply(m.version, key, seq, cmdStatus, 0, enc)
	if err != nil {
		return err
	}
	_, err = conn.Write(reply)
	return err
}

func (m *mockDevice) replyStatus(conn net.Conn, key []byte, seq uint32, dps map[string]any) error {
	js, err := json.Marshal(map[string]any{"dps": dps})
	if err != nil {
		return err
	}
	enc, err := encodePayload(m.version, key, cmdStatus, js)
	if err != nil {
		return err
	}
	reply, err := packReply(m.version, key, seq, cmdStatus, 0, enc)
	if err != nil {
		return err
	}
	_, err = conn.Write(reply)
	return err
}

func TestClientStatusAndSet(t *testing.T) {
	for _, v := range []Version{V31, V33, V34, V35} {
		t.Run(string(v), func(t *testing.T) {
			m := newMockDevice(t, v, false)
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()

			c, err := Dial(ctx, Config{
				ID:      "dev123",
				IP:      "127.0.0.1",
				Port:    m.port,
				Key:     "0123456789abcdef",
				Version: v,
				Timeout: 2 * time.Second,
			})
			if err != nil {
				t.Fatal(err)
			}
			defer c.Close()

			res, err := c.Status(ctx)
			if err != nil {
				t.Fatal(err)
			}
			if res.DPs["1"] != true {
				t.Fatalf("snapshot dps = %v", res.DPs)
			}

			if _, err := c.Set(ctx, "1", false); err != nil {
				t.Fatal(err)
			}
			res, err = c.Status(ctx)
			if err != nil {
				t.Fatal(err)
			}
			if res.DPs["1"] != false {
				t.Fatalf("after set dps = %v", res.DPs)
			}
		})
	}
}

func TestClientDevice22Fallback(t *testing.T) {
	for _, v := range []Version{V33, V34} {
		t.Run(string(v), func(t *testing.T) {
			m := newMockDeviceOpts(t, v, false, true)
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()

			c, err := Dial(ctx, Config{
				ID:      "dev123",
				IP:      "127.0.0.1",
				Port:    m.port,
				Key:     "0123456789abcdef",
				Version: v,
				Timeout: 2 * time.Second,
			})
			if err != nil {
				t.Fatal(err)
			}
			defer c.Close()

			res, err := c.Status(ctx)
			if err != nil {
				t.Fatal(err)
			}
			if !c.cfg.Device22 {
				t.Fatal("expected device22 fallback to be enabled")
			}
			if res.DPs["1"] != true {
				t.Fatalf("dps = %v", res.DPs)
			}
		})
	}
}

func TestClientRediscoveryOnIPChange(t *testing.T) {
	m := newMockDevice(t, V33, false)
	mockDiscovery(t, func(context.Context, time.Duration) ([]Discovered, error) {
		return []Discovered{{ID: "dev123", IP: "127.0.0.1", Version: "3.3"}}, nil
	})

	var gotOld, gotNew string
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	c, err := Dial(ctx, Config{
		ID:      "dev123",
		IP:      "127.0.0.2", // not the mock's address: forces the fallback
		Port:    m.port,
		Key:     "0123456789abcdef",
		Version: V33,
		Timeout: 500 * time.Millisecond,
		OnIPChange: func(oldIP, newIP string) {
			gotOld, gotNew = oldIP, newIP
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()

	if c.cfg.IP != "127.0.0.1" {
		t.Fatalf("client kept stale IP %q", c.cfg.IP)
	}
	if gotOld != "127.0.0.2" || gotNew != "127.0.0.1" {
		t.Fatalf("OnIPChange got (%q, %q)", gotOld, gotNew)
	}
	res, err := c.Status(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if res.DPs["1"] != true {
		t.Fatalf("dps = %v", res.DPs)
	}
}

func TestClientDisableDiscovery(t *testing.T) {
	m := newMockDevice(t, V33, false)
	called := false
	mockDiscovery(t, func(context.Context, time.Duration) ([]Discovered, error) {
		called = true
		return nil, nil
	})

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	_, err := Dial(ctx, Config{
		ID:               "dev123",
		IP:               "127.0.0.2",
		Port:             m.port,
		Key:              "0123456789abcdef",
		Version:          V33,
		Timeout:          500 * time.Millisecond,
		DisableDiscovery: true,
	})
	if err == nil {
		t.Fatal("expected dial to fail with the stale IP")
	}
	if called {
		t.Fatal("discovery must not run when DisableDiscovery is set")
	}
}

// mockDiscovery swaps the package-level discovery hook for one test.
func mockDiscovery(t *testing.T, fn func(context.Context, time.Duration) ([]Discovered, error)) {
	t.Helper()
	old := discoverFn
	discoverFn = fn
	t.Cleanup(func() { discoverFn = old })
}

func TestClientNoIPResolvesViaDiscovery(t *testing.T) {
	m := newMockDevice(t, V33, false)
	mockDiscovery(t, func(context.Context, time.Duration) ([]Discovered, error) {
		return []Discovered{{ID: "dev123", IP: "127.0.0.1", Version: "3.3"}}, nil
	})

	var gotOld, gotNew string
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	c, err := Dial(ctx, Config{
		ID:      "dev123",
		IP:      "", // cloud-only registration: no address yet
		Port:    m.port,
		Key:     "0123456789abcdef",
		Version: V33,
		Timeout: 500 * time.Millisecond,
		OnIPChange: func(oldIP, newIP string) {
			gotOld, gotNew = oldIP, newIP
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()

	if c.cfg.IP != "127.0.0.1" {
		t.Fatalf("IP not resolved: %q", c.cfg.IP)
	}
	if gotOld != "" || gotNew != "127.0.0.1" {
		t.Fatalf("OnIPChange got (%q, %q)", gotOld, gotNew)
	}
	res, err := c.Status(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if res.DPs["1"] != true {
		t.Fatalf("dps = %v", res.DPs)
	}
}

func TestClientNoIPAndDiscoveryFails(t *testing.T) {
	mockDiscovery(t, func(context.Context, time.Duration) ([]Discovered, error) {
		return nil, nil
	})
	_, err := Dial(context.Background(), Config{
		ID:      "nope",
		IP:      "",
		Port:    6668,
		Key:     "0123456789abcdef",
		Version: V33,
		Timeout: 200 * time.Millisecond,
	})
	if err == nil {
		t.Fatal("expected an error when the address cannot be resolved")
	}
	if !strings.Contains(err.Error(), "no LAN address known") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestClientListen(t *testing.T) {
	m := newMockDevice(t, V33, true)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	c, err := Dial(ctx, Config{
		ID:      "dev123",
		IP:      "127.0.0.1",
		Port:    m.port,
		Key:     "0123456789abcdef",
		Version: V33,
		Timeout: 2 * time.Second,
	})
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()

	events := make(chan Event, 8)
	go func() { _ = c.Listen(ctx, func(e Event) { events <- e }) }()

	for {
		select {
		case e := <-events:
			if e.DPs["1"] == false {
				return
			}
		case <-ctx.Done():
			t.Fatal("timed out waiting for pushed DP update")
		}
	}
}
