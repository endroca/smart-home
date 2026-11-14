// Package model contains the JSON shapes exchanged with the devices (the
// manifest, the status and the reported state) plus small helpers shared by the
// hub.
package model

import (
	"encoding/json"
	"strconv"
	"strings"
	"time"
)

// Manifest is the retained discovery document published by the ESP32.
type Manifest struct {
	SchemaVersion   int      `json:"schemaVersion"`
	ManifestVersion int      `json:"manifestVersion"`
	Thing           string   `json:"thing"`
	Model           string   `json:"model"`
	Protocol        string   `json:"protocol"`
	Firmware        Firmware `json:"firmware"`
	HTTP            HTTPInfo `json:"http"`
	Actions         []string `json:"actions"`
	Devices         []Device `json:"devices"`
}

// Firmware identifies the running firmware and its build timestamp.
type Firmware struct {
	Version string `json:"version"`
	Build   string `json:"build"`
}

// HTTPInfo describes the LAN endpoint of the device.
type HTTPInfo struct {
	MDNS string `json:"mdns"`
	Port int    `json:"port"`
}

// Device is one logical device exposed by a thing (a sub-device of the
// gateway, for example a fan). ID is its stable identifier inside the gateway.
type Device struct {
	ID           string                `json:"id"`
	Type         string                `json:"type"`
	Name         string                `json:"name"`
	Capabilities map[string]Capability `json:"capabilities"`
	Actions      []string              `json:"actions"`
}

// Capability is intentionally generic; the hub renders only the kinds it
// understands and ignores unknown ones.
type Capability struct {
	Kind     string   `json:"kind"`
	Stateful *bool    `json:"stateful,omitempty"`
	Writable *bool    `json:"writable,omitempty"`
	Values   []string `json:"values,omitempty"`
	Min      *float64 `json:"min,omitempty"`
	Max      *float64 `json:"max,omitempty"`
	Step     *float64 `json:"step,omitempty"`
	Unit     string   `json:"unit,omitempty"`
}

// IsWritable defaults to true when the field is absent.
func (c Capability) IsWritable() bool {
	return c.Writable == nil || *c.Writable
}

// IsStateful defaults to true when the field is absent.
func (c Capability) IsStateful() bool {
	return c.Stateful == nil || *c.Stateful
}

// OTAStatus mirrors the status.ota object.
type OTAStatus struct {
	State   string `json:"state"`
	Version string `json:"version,omitempty"`
}

// Status is the retained liveness/diagnostic document.
type Status struct {
	Online          bool       `json:"online"`
	Thing           string     `json:"thing"`
	Version         string     `json:"version,omitempty"`
	TS              int64      `json:"ts,omitempty"`
	IP              string     `json:"ip,omitempty"`
	RSSI            int        `json:"rssi,omitempty"`
	Heap            int64      `json:"heap,omitempty"`
	Uptime          int64      `json:"uptime,omitempty"`
	PendingSync     bool       `json:"pendingSync,omitempty"`
	ManifestVersion int64      `json:"manifestVersion,omitempty"`
	OTA             *OTAStatus `json:"ota,omitempty"`
}

// DeviceState is the last-known state of one sub-device of a gateway, as the
// device reported it. The hub only mirrors it; it never invents state.
//
// Stale marks a state restored from the durable replica at boot: it is the last
// known value, not a confirmed one, until the device reports again.
type DeviceState struct {
	Thing     string         `json:"thing"`
	DeviceID  string         `json:"id"`
	Reported  map[string]any `json:"reported,omitempty"`
	Stale     bool           `json:"stale,omitempty"`
	UpdatedAt time.Time      `json:"updatedAt"`
}

// Thing aggregates what the hub knows about one gateway: the manifest it
// advertises, its status and the reported state of each sub-device.
type Thing struct {
	ID        string                  `json:"id"`
	Manifest  *Manifest               `json:"manifest,omitempty"`
	Status    *Status                 `json:"status,omitempty"`
	States    map[string]*DeviceState `json:"states"`
	LastEvent map[string]any          `json:"lastEvent,omitempty"`
	LastAck   map[string]any          `json:"lastAck,omitempty"`
	UpdatedAt time.Time               `json:"updatedAt"`
}

// Device returns the manifest device with the given id.
func (t *Thing) Device(id string) (*Device, bool) {
	if t == nil || t.Manifest == nil {
		return nil, false
	}
	for i := range t.Manifest.Devices {
		if t.Manifest.Devices[i].ID == id {
			return &t.Manifest.Devices[i], true
		}
	}
	return nil, false
}

// Online reports the last retained online flag.
func (t *Thing) Online() bool {
	return t != nil && t.Status != nil && t.Status.Online
}

// HasAction reports whether a thing-level action is declared.
func (m *Manifest) HasAction(action string) bool {
	if m == nil {
		return false
	}
	for _, a := range m.Actions {
		if a == action {
			return true
		}
	}
	return false
}

// HasAction reports whether a device-level action is declared.
func (d *Device) HasAction(action string) bool {
	for _, a := range d.Actions {
		if a == action {
			return true
		}
	}
	return false
}

// AsFloat converts JSON numbers produced with UseNumber as well as ordinary
// numeric values to float64.
func AsFloat(v any) (float64, bool) {
	switch n := v.(type) {
	case nil:
		return 0, false
	case json.Number:
		f, err := n.Float64()
		return f, err == nil
	case float64:
		return n, true
	case float32:
		return float64(n), true
	case int:
		return float64(n), true
	case int64:
		return float64(n), true
	case uint64:
		return float64(n), true
	case string:
		f, err := strconv.ParseFloat(strings.TrimSpace(n), 64)
		return f, err == nil
	default:
		return 0, false
	}
}

// AsInt converts a value to int64 without accepting fractional numbers.
func AsInt(v any) (int64, bool) {
	f, ok := AsFloat(v)
	if !ok || f != float64(int64(f)) {
		return 0, false
	}
	return int64(f), true
}

// AsBool converts common JSON representations of a boolean.
func AsBool(v any) (bool, bool) {
	switch b := v.(type) {
	case bool:
		return b, true
	case string:
		switch strings.ToLower(strings.TrimSpace(b)) {
		case "true", "1", "on":
			return true, true
		case "false", "0", "off":
			return false, true
		}
	}
	return false, false
}

// AsString converts a string value.
func AsString(v any) (string, bool) {
	s, ok := v.(string)
	return s, ok
}

// SortCapabilityNames returns capability names in a stable UI-friendly order.
func SortCapabilityNames(caps map[string]Capability) []string {
	order := []string{"power", "light", "speed", "direction"}
	seen := make(map[string]bool, len(caps))
	out := make([]string, 0, len(caps))
	for _, name := range order {
		if _, ok := caps[name]; ok {
			out = append(out, name)
			seen[name] = true
		}
	}
	// Remaining capabilities are sorted alphabetically for determinism.
	var rest []string
	for name := range caps {
		if !seen[name] {
			rest = append(rest, name)
		}
	}
	for i := 0; i < len(rest); i++ {
		for j := i + 1; j < len(rest); j++ {
			if rest[j] < rest[i] {
				rest[i], rest[j] = rest[j], rest[i]
			}
		}
	}
	return append(out, rest...)
}
