package tuya

import (
	"bytes"
	"context"
	"crypto/md5"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"sync"
	"time"
)

// discoveryKey is the well-known key used by Tuya UDP broadcasts.
var discoveryKey = func() []byte {
	sum := md5.Sum([]byte("yGAdlopoPVldABfn"))
	return sum[:]
}()

// discoverSem serializes discovery inside this process. The UDP discovery ports
// can only be bound once, so concurrent clients (for example several devices
// rediscovering after a network change) must take turns instead of fighting
// over 6666/6667/7000.
var discoverSem = make(chan struct{}, 1)

// Discovered is one device found on the LAN.
type Discovered struct {
	ID         string         `json:"id"`
	IP         string         `json:"ip"`
	Version    string         `json:"version"`
	ProductKey string         `json:"productKey,omitempty"`
	Raw        map[string]any `json:"raw,omitempty"`
}

// Discover listens on the Tuya UDP discovery ports and, for 3.5 devices, sends
// a solicitation on UDP/7000. It returns after timeout.
func Discover(ctx context.Context, timeout time.Duration) ([]Discovered, error) {
	if timeout <= 0 {
		timeout = 5 * time.Second
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	select {
	case discoverSem <- struct{}{}:
		defer func() { <-discoverSem }()
	case <-ctx.Done():
		return nil, ctx.Err()
	}

	results := make(chan Discovered, 32)
	var wg sync.WaitGroup
	var mu sync.Mutex
	bound := 0
	var lastErr error

	for _, port := range []int{6666, 6667, 7000} {
		conn, err := net.ListenUDP("udp4", &net.UDPAddr{IP: net.IPv4zero, Port: port})
		if err != nil {
			mu.Lock()
			lastErr = err
			mu.Unlock()
			continue
		}
		bound++
		wg.Add(1)
		go func(conn *net.UDPConn, port int) {
			defer wg.Done()
			defer conn.Close()
			buf := make([]byte, 4096)
			for {
				_ = conn.SetReadDeadline(time.Now().Add(500 * time.Millisecond))
				n, addr, err := conn.ReadFromUDP(buf)
				if err != nil {
					if ctx.Err() != nil {
						return
					}
					if isTimeout(err) {
						continue
					}
					return
				}
				if d := parseDiscovery(buf[:n], addr.IP); d != nil {
					select {
					case results <- *d:
					case <-ctx.Done():
						return
					}
				}
			}
		}(conn, port)
	}
	if bound == 0 {
		if lastErr != nil {
			return nil, fmt.Errorf("tuya: discovery: %w", lastErr)
		}
		return nil, errors.New("tuya: discovery: no UDP port available")
	}

	// 3.5 devices may stay silent until they receive a solicitation.
	_ = sendSolicitation()

	go func() {
		wg.Wait()
		close(results)
	}()

	seen := map[string]bool{}
	out := []Discovered{}
	for {
		select {
		case <-ctx.Done():
			return dedupe(out), nil
		case d, ok := <-results:
			if !ok {
				return dedupe(out), nil
			}
			key := d.ID
			if key == "" {
				key = d.IP
			}
			if key != "" && seen[key] {
				continue
			}
			if key != "" {
				seen[key] = true
			}
			out = append(out, d)
		}
	}
}

func dedupe(in []Discovered) []Discovered {
	seen := map[string]bool{}
	out := in[:0]
	for _, d := range in {
		key := d.ID
		if key == "" {
			key = d.IP
		}
		if key != "" && seen[key] {
			continue
		}
		if key != "" {
			seen[key] = true
		}
		out = append(out, d)
	}
	return out
}

func sendSolicitation() error {
	local := localIP()
	payload := []byte(fmt.Sprintf(`{"from":"app","ip":"%s"}`, local))
	frame, err := pack6699(discoveryKey, 1, cmdDevInfo, payload)
	if err != nil {
		return err
	}
	conn, err := net.DialUDP("udp4", nil, &net.UDPAddr{IP: net.IPv4bcast, Port: 7000})
	if err != nil {
		return err
	}
	defer conn.Close()
	_, err = conn.Write(frame)
	return err
}

func localIP() string {
	conn, err := net.Dial("udp4", "8.8.8.8:80")
	if err != nil {
		return "255.255.255.255"
	}
	defer conn.Close()
	if addr, ok := conn.LocalAddr().(*net.UDPAddr); ok {
		return addr.IP.String()
	}
	return "255.255.255.255"
}

func parseDiscovery(data []byte, addr net.IP) *Discovered {
	data = trimNul(data)
	// 3.1 devices broadcast plaintext JSON.
	if len(data) > 0 && data[0] == '{' {
		return discoveryFromJSON(data, addr)
	}
	if len(data) >= 18 {
		switch binary.BigEndian.Uint32(data[:4]) {
		case prefix55AA:
			// UDP frames from devices normally carry the 4-byte retcode;
			// some clones do not, so try both interpretations.
			for _, fromDevice := range []bool{true, false} {
				msg, err := parse55AA(data, nil, fromDevice)
				if err != nil {
					continue
				}
				if d := discoveryFromPayload(msg.Payload, addr); d != nil {
					return d
				}
			}
		case prefix6699:
			msg, err := parse6699(data, discoveryKey, false)
			if err != nil {
				return nil
			}
			payload := trimNul(msg.Payload)
			// tinytuya's UDP decoder strips a retcode only when the payload
			// does not start with '{' but the fifth byte does.
			if len(payload) >= 5 && payload[0] != '{' && payload[4] == '{' {
				payload = payload[4:]
			}
			if d := discoveryFromPayload(payload, addr); d != nil {
				return d
			}
		}
	}
	return discoveryFromPayload(data, addr)
}

func trimNul(b []byte) []byte {
	return bytes.TrimSpace(bytes.TrimRight(b, "\x00"))
}

func discoveryFromPayload(payload []byte, addr net.IP) *Discovered {
	payload = trimNul(payload)
	if len(payload) > 0 && payload[0] == '{' {
		return discoveryFromJSON(payload, addr)
	}
	// 3.2/3.3 broadcast: AES-ECB with the discovery key, sometimes behind a
	// 15-byte version header.
	if len(payload) >= 15 && bytes.HasPrefix(payload, []byte("3.")) {
		payload = payload[15:]
	}
	if dec, err := aesECBDecrypt(discoveryKey, payload, true); err == nil {
		if d := discoveryFromJSON(trimNul(dec), addr); d != nil {
			return d
		}
	}
	if dec, err := base64.StdEncoding.DecodeString(string(payload)); err == nil {
		if dec2, err := aesECBDecrypt(discoveryKey, dec, true); err == nil {
			if d := discoveryFromJSON(trimNul(dec2), addr); d != nil {
				return d
			}
		}
	}
	return nil
}

func discoveryFromJSON(data []byte, addr net.IP) *Discovered {
	var raw map[string]any
	if err := json.Unmarshal(data, &raw); err != nil {
		return nil
	}
	// UDP/7000 solicitation replies echo the request; ignore our own broadcast.
	if v, ok := raw["from"].(string); ok && v == "app" {
		return nil
	}
	d := &Discovered{Raw: raw}
	for _, key := range []string{"gwId", "devId", "id", "dev_id"} {
		if v, ok := raw[key].(string); ok && v != "" {
			d.ID = v
			break
		}
	}
	if d.ID == "" {
		// Without a device ID the result cannot be dialed or identified.
		return nil
	}
	if v, ok := raw["ip"].(string); ok && v != "" {
		d.IP = v
	} else if addr != nil {
		d.IP = addr.String()
	}
	if v, ok := raw["version"].(string); ok {
		d.Version = v
	}
	for _, key := range []string{"productKey", "product_key"} {
		if v, ok := raw[key].(string); ok && v != "" {
			d.ProductKey = v
			break
		}
	}
	return d
}
