package tuya

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strconv"
	"time"
)

// noProtocolHeaderCmds are sent without the 15-byte "<version> + zeros" header.
func noProtocolHeader(cmd uint32) bool {
	switch cmd {
	case cmdDPQuery, cmdDPQueryNew, cmdUpdatedps, cmdHeartbeat,
		cmdSessStart, cmdSessResp, cmdSessFinish, cmdLANExt:
		return true
	}
	return false
}

// encodePayload applies the version-specific header and encryption to a JSON
// payload. key is the static local key for 3.1-3.3 and the session key for
// 3.4/3.5 data commands.
func encodePayload(v Version, key []byte, cmd uint32, payload []byte) ([]byte, error) {
	switch {
	case v == V31:
		if cmd == cmdControl {
			return encrypt31Control(key, payload)
		}
		return payload, nil
	case v.atLeast(V32) && !v.atLeast(V34):
		// 3.2/3.3 encrypt every payload with the local key. The 15-byte version
		// header is only prepended for commands that carry one.
		enc, err := aesECBEncrypt(key, payload, true)
		if err != nil {
			return nil, err
		}
		if noProtocolHeader(cmd) {
			return enc, nil
		}
		return append(v.header(), enc...), nil
	case v.atLeast(V34) && !v.atLeast(V35):
		raw := payload
		if !noProtocolHeader(cmd) {
			raw = append(v.header(), payload...)
		}
		return aesECBEncrypt(key, raw, true)
	case v == V35:
		if !noProtocolHeader(cmd) {
			return append(v.header(), payload...), nil
		}
		return payload, nil
	default:
		return nil, fmt.Errorf("tuya: unsupported version %q", v)
	}
}

// decodePayload reverses encodePayload. For 3.5 the frame layer already
// decrypted the payload, so this only strips the optional protocol header.
func decodePayload(v Version, key []byte, cmd uint32, payload []byte, device22 bool) ([]byte, error) {
	switch {
	case v == V31:
		if bytes.HasPrefix(payload, []byte("3.1")) {
			return decrypt31Control(key, payload)
		}
		return payload, nil
	case v.atLeast(V32) && !v.atLeast(V34):
		if bytes.HasPrefix(payload, v.bytes()) {
			if len(payload) < 15 {
				return nil, fmt.Errorf("tuya: short %s header", v)
			}
			payload = payload[15:]
		} else if device22 && len(payload)%16 != 0 && len(payload) >= 15 {
			payload = payload[15:]
		}
		// Some firmwares/clones reply in plaintext; accept it before trying to
		// decrypt so a single device does not break the session.
		if stripped := bytes.TrimSpace(payload); len(stripped) > 0 && stripped[0] == '{' {
			return payload, nil
		}
		return aesECBDecrypt(key, payload, true)
	case v.atLeast(V34) && !v.atLeast(V35):
		raw, err := aesECBDecrypt(key, payload, true)
		if err != nil {
			return nil, err
		}
		if bytes.HasPrefix(raw, v.bytes()) {
			if len(raw) < 15 {
				return nil, fmt.Errorf("tuya: short %s header", v)
			}
			raw = raw[15:]
		}
		return raw, nil
	case v == V35:
		if bytes.HasPrefix(payload, v.bytes()) {
			if len(payload) < 15 {
				return nil, fmt.Errorf("tuya: short %s header", v)
			}
			payload = payload[15:]
		}
		return payload, nil
	default:
		return nil, fmt.Errorf("tuya: unsupported version %q", v)
	}
}

func epochString() string { return strconv.FormatInt(time.Now().Unix(), 10) }

func dpsMap(dps []string) map[string]any {
	if len(dps) == 0 {
		dps = []string{"1"}
	}
	out := make(map[string]any, len(dps))
	for _, dp := range dps {
		out[dp] = nil
	}
	return out
}

// BuildQuery returns the command and JSON payload for a DP status query.
func BuildQuery(id string, v Version, device22 bool, dps []string) (uint32, []byte, error) {
	var m map[string]any
	switch {
	case v.atLeast(V34):
		if device22 {
			m = map[string]any{"dps": dpsMap(dps)}
			js, err := json.Marshal(m)
			return cmdDPQueryNew, js, err
		}
		return cmdDPQueryNew, []byte("{}"), nil
	default:
		if device22 {
			m = map[string]any{
				"devId": id,
				"uid":   id,
				"t":     epochString(),
				"dps":   dpsMap(dps),
			}
			js, err := json.Marshal(m)
			return cmdControlNew, js, err
		}
		m = map[string]any{
			"gwId":  id,
			"devId": id,
			"uid":   id,
			"t":     epochString(),
		}
		js, err := json.Marshal(m)
		return cmdDPQuery, js, err
	}
}

// BuildControl returns the command and JSON payload to set one DP.
func BuildControl(id string, v Version, dp string, value any) (uint32, []byte, error) {
	if v.atLeast(V34) {
		m := map[string]any{
			"protocol": 5,
			"t":        time.Now().Unix(),
			"data": map[string]any{
				"dps": map[string]any{dp: value},
			},
		}
		js, err := json.Marshal(m)
		return cmdControlNew, js, err
	}
	m := map[string]any{
		"devId": id,
		"uid":   id,
		"t":     epochString(),
		"dps":   map[string]any{dp: value},
	}
	js, err := json.Marshal(m)
	return cmdControl, js, err
}

// BuildHeartbeat returns the command and JSON payload for a keep-alive.
func BuildHeartbeat(id string) (uint32, []byte, error) {
	js, err := json.Marshal(map[string]any{"gwId": id, "devId": id})
	return cmdHeartbeat, js, err
}

// ParseDPs extracts the dps map from a decoded JSON payload. It accepts both
// the flat form ({"dps":{...}}) and the 3.4+ nested form
// ({"data":{"dps":{...}}}).
func ParseDPs(payload []byte) (map[string]any, error) {
	var root map[string]any
	if err := json.Unmarshal(payload, &root); err != nil {
		return nil, err
	}
	if raw, ok := root["dps"]; ok {
		if dps, ok := raw.(map[string]any); ok {
			return dps, nil
		}
	}
	if raw, ok := root["data"]; ok {
		if data, ok := raw.(map[string]any); ok {
			if dps, ok := data["dps"].(map[string]any); ok {
				return dps, nil
			}
		}
	}
	return nil, nil
}

// ParseJSON parses a decoded payload into a generic map.
func ParseJSON(payload []byte) (map[string]any, error) {
	var root map[string]any
	if err := json.Unmarshal(payload, &root); err != nil {
		return nil, err
	}
	return root, nil
}
