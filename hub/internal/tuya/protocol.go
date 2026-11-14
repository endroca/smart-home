package tuya

import (
	"encoding/binary"
	"errors"
	"fmt"
	"hash/crc32"
	"io"
)

// Frame prefixes and suffixes.
const (
	prefix55AA uint32 = 0x000055AA
	suffix55AA uint32 = 0x0000AA55
	prefix6699 uint32 = 0x00006699
	suffix6699 uint32 = 0x00009966

	header55AA = 16
	header6699 = 18

	end55AA      = 8  // CRC32 (4) + footer (4)
	end55AAHMAC  = 36 // HMAC-SHA256 (32) + footer (4)
	end6699      = 20 // GCM tag (16) + footer (4)
	maxFrameSize = 64 * 1024
)

// Command IDs used by the LAN protocol.
const (
	cmdSessStart  uint32 = 0x03
	cmdSessResp   uint32 = 0x04
	cmdSessFinish uint32 = 0x05
	cmdControl    uint32 = 0x07
	cmdStatus     uint32 = 0x08
	cmdHeartbeat  uint32 = 0x09
	cmdDPQuery    uint32 = 0x0A
	cmdControlNew uint32 = 0x0D
	cmdDPQueryNew uint32 = 0x10
	cmdUpdatedps  uint32 = 0x12
	cmdLANExt     uint32 = 0x40
	cmdDevInfo    uint32 = 0x25
)

// Version is a Tuya LAN protocol revision.
type Version string

const (
	V31 Version = "3.1"
	V32 Version = "3.2"
	V33 Version = "3.3"
	V34 Version = "3.4"
	V35 Version = "3.5"
)

func ParseVersion(s string) (Version, error) {
	switch Version(s) {
	case V31, V32, V33, V34, V35:
		return Version(s), nil
	default:
		return "", fmt.Errorf("tuya: unsupported protocol version %q", s)
	}
}

// atLeast compares protocol revisions. Single-digit versions sort
// lexicographically, which is enough for 3.1 through 3.5.
func (v Version) atLeast(other Version) bool { return v >= other }

func (v Version) bytes() []byte { return []byte(string(v)) }

// header returns the 15-byte protocol header: "<version>" plus 12 zero bytes.
func (v Version) header() []byte {
	out := make([]byte, 15)
	copy(out, v.bytes())
	return out
}

// Message is a decoded protocol frame. Payload is the raw frame payload after
// integrity verification: for 3.5 it is already decrypted and has the retcode
// stripped; for older versions the payload still needs decodePayload.
type Message struct {
	Prefix     uint32
	Seq        uint32
	Cmd        uint32
	Retcode    uint32
	HasRetcode bool
	Payload    []byte
}

// packFrame encodes a client message for the given protocol version.
// key is the static local key for 3.1-3.3 (and for 3.4/3.5 handshakes) or the
// negotiated session key after a 3.4/3.5 handshake.
func packFrame(v Version, key []byte, seq, cmd uint32, payload []byte) ([]byte, error) {
	if v == V35 {
		return pack6699(key, seq, cmd, payload)
	}
	var hmacKey []byte
	if v.atLeast(V34) {
		hmacKey = key
	}
	return pack55AA(seq, cmd, payload, hmacKey)
}

func pack55AA(seq, cmd uint32, payload, hmacKey []byte) ([]byte, error) {
	endLen := end55AA
	if hmacKey != nil {
		endLen = end55AAHMAC
	}
	buf := make([]byte, header55AA+len(payload))
	binary.BigEndian.PutUint32(buf[0:], prefix55AA)
	binary.BigEndian.PutUint32(buf[4:], seq)
	binary.BigEndian.PutUint32(buf[8:], cmd)
	binary.BigEndian.PutUint32(buf[12:], uint32(len(payload)+endLen))
	copy(buf[header55AA:], payload)

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

// parse55AA parses a complete 55AA frame. fromDevice selects whether the
// 4-byte retcode is present and modifies the integrity check accordingly.
func parse55AA(data []byte, hmacKey []byte, fromDevice bool) (*Message, error) {
	if len(data) < header55AA {
		return nil, errors.New("tuya: short 55AA frame")
	}
	if binary.BigEndian.Uint32(data[0:]) != prefix55AA {
		return nil, errors.New("tuya: bad 55AA prefix")
	}
	seq := binary.BigEndian.Uint32(data[4:])
	cmd := binary.BigEndian.Uint32(data[8:])
	length := int(binary.BigEndian.Uint32(data[12:]))
	total := header55AA + length
	if length < 0 || total > maxFrameSize || len(data) < total {
		return nil, fmt.Errorf("tuya: bad 55AA length %d", length)
	}
	frame := data[:total]

	endLen := end55AA
	if hmacKey != nil {
		endLen = end55AAHMAC
	}
	verifyLen := total - endLen
	if verifyLen < header55AA {
		return nil, errors.New("tuya: 55AA frame too short for trailer")
	}

	if hmacKey != nil {
		want := hmacSHA256(hmacKey, frame[:verifyLen])
		if !equalBytes(want, frame[verifyLen:verifyLen+32]) {
			return nil, errors.New("tuya: bad HMAC")
		}
	} else {
		want := crc32.ChecksumIEEE(frame[:verifyLen])
		got := binary.BigEndian.Uint32(frame[verifyLen:])
		if want != got {
			return nil, fmt.Errorf("tuya: bad CRC (got %08x want %08x)", got, want)
		}
	}
	if binary.BigEndian.Uint32(frame[total-4:]) != suffix55AA {
		return nil, errors.New("tuya: bad 55AA suffix")
	}

	msg := &Message{Prefix: prefix55AA, Seq: seq, Cmd: cmd}
	if fromDevice {
		if verifyLen < header55AA+4 {
			return nil, errors.New("tuya: 55AA reply without retcode")
		}
		msg.Retcode = binary.BigEndian.Uint32(frame[header55AA:])
		msg.HasRetcode = true
		msg.Payload = frame[header55AA+4 : verifyLen]
	} else {
		msg.Payload = frame[header55AA:verifyLen]
	}
	return msg, nil
}

func pack6699(key []byte, seq, cmd uint32, raw []byte) ([]byte, error) {
	if len(key) != 16 {
		return nil, fmt.Errorf("tuya: 6699 requires a 16-byte key, got %d", len(key))
	}
	iv, err := randomBytes(12)
	if err != nil {
		return nil, err
	}
	header := make([]byte, header6699)
	binary.BigEndian.PutUint32(header[0:], prefix6699)
	binary.BigEndian.PutUint16(header[4:], 0)
	binary.BigEndian.PutUint32(header[6:], seq)
	binary.BigEndian.PutUint32(header[10:], cmd)
	// length counts IV + ciphertext + tag; the footer is excluded.
	binary.BigEndian.PutUint32(header[14:], uint32(12+len(raw)+16))

	sealed, err := gcmSeal(key, iv, raw, header[4:])
	if err != nil {
		return nil, err
	}
	out := make([]byte, 0, len(header)+12+len(sealed)+4)
	out = append(out, header...)
	out = append(out, iv...)
	out = append(out, sealed...)
	out = append(out, 0x00, 0x00, 0x99, 0x66)
	return out, nil
}

func parse6699(data []byte, key []byte, fromDevice bool) (*Message, error) {
	if len(data) < header6699 {
		return nil, errors.New("tuya: short 6699 frame")
	}
	if binary.BigEndian.Uint32(data[0:]) != prefix6699 {
		return nil, errors.New("tuya: bad 6699 prefix")
	}
	seq := binary.BigEndian.Uint32(data[6:])
	cmd := binary.BigEndian.Uint32(data[10:])
	length := int(binary.BigEndian.Uint32(data[14:]))
	total := header6699 + length + 4
	if length < 12+16 || total > maxFrameSize || len(data) < total {
		return nil, fmt.Errorf("tuya: bad 6699 length %d", length)
	}
	frame := data[:total]
	if binary.BigEndian.Uint32(frame[total-4:]) != suffix6699 {
		return nil, errors.New("tuya: bad 6699 suffix")
	}
	body := frame[header6699 : header6699+length]
	iv := body[:12]
	sealed := body[12:]
	raw, err := gcmOpen(key, iv, sealed, frame[4:header6699])
	if err != nil {
		return nil, fmt.Errorf("tuya: 6699 GCM: %w", err)
	}
	msg := &Message{Prefix: prefix6699, Seq: seq, Cmd: cmd}
	if fromDevice {
		if len(raw) < 4 {
			return nil, errors.New("tuya: 6699 reply without retcode")
		}
		msg.Retcode = binary.BigEndian.Uint32(raw[:4])
		msg.HasRetcode = true
		msg.Payload = raw[4:]
	} else {
		msg.Payload = raw
	}
	return msg, nil
}

func equalBytes(a, b []byte) bool {
	if len(a) != len(b) {
		return false
	}
	var v byte
	for i := range a {
		v |= a[i] ^ b[i]
	}
	return v == 0
}

var (
	errPartialFrame = errors.New("tuya: partial frame read")
)

// readFull reads exactly len(buf) bytes, marking a timeout that occurs after
// some bytes were already consumed. Callers must not treat such a timeout as a
// clean idle connection: the stream is desynchronized and needs a reconnect.
func readFull(r io.Reader, buf []byte) error {
	n, err := io.ReadFull(r, buf)
	if err != nil && n > 0 {
		return fmt.Errorf("%w: %w", errPartialFrame, err)
	}
	return err
}

// readMessage reads exactly one frame from r. It first reads the 4-byte prefix
// so the rest of the header can be sized correctly.
func readMessage(r io.Reader, v Version, key []byte, fromDevice bool) (*Message, error) {
	head := make([]byte, 4)
	if err := readFull(r, head); err != nil {
		return nil, err
	}
	prefix := binary.BigEndian.Uint32(head)
	switch prefix {
	case prefix55AA:
		rest := make([]byte, header55AA-4)
		if err := readFull(r, rest); err != nil {
			return nil, err
		}
		frame := append(head, rest...)
		length := int(binary.BigEndian.Uint32(frame[12:]))
		if length < 0 || length > maxFrameSize {
			return nil, fmt.Errorf("tuya: bad 55AA length %d", length)
		}
		body := make([]byte, length)
		if err := readFull(r, body); err != nil {
			return nil, err
		}
		frame = append(frame, body...)
		hmacKey := []byte(nil)
		if v.atLeast(V34) {
			hmacKey = key
		}
		return parse55AA(frame, hmacKey, fromDevice)
	case prefix6699:
		rest := make([]byte, header6699-4)
		if err := readFull(r, rest); err != nil {
			return nil, err
		}
		frame := append(head, rest...)
		length := int(binary.BigEndian.Uint32(frame[14:]))
		if length < 0 || length > maxFrameSize {
			return nil, fmt.Errorf("tuya: bad 6699 length %d", length)
		}
		body := make([]byte, length+4)
		if err := readFull(r, body); err != nil {
			return nil, err
		}
		frame = append(frame, body...)
		return parse6699(frame, key, fromDevice)
	default:
		return nil, fmt.Errorf("tuya: unknown frame prefix %08x", prefix)
	}
}
